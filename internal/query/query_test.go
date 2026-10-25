package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		"create table items (id integer primary key, name text, data blob, created timestamp)",
		"create table pairs (a integer, b integer, primary key (a, b))",
		"create table logs (msg text)",
	}
	for i := 1; i <= 25; i++ {
		stmts = append(stmts,
			fmt.Sprintf("insert into items (id, name) values (%d, 'item %d')", i*10, i),
			fmt.Sprintf("insert into pairs values (%d, 1)", i),
			fmt.Sprintf("insert into logs values ('log %02d')", i))
	}
	stmts = append(stmts, "update items set name = null, data = x'00ff10', created = '2024-03-01 10:20:30' where id = 10")
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatal(s, err)
		}
	}
	raw.Close()

	db, err := drivers.Connect(context.Background(), models.DBConn{Engine: models.EngineSQLite, DB: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func source(t *testing.T, db *sql.DB, table string) TableSource {
	t.Helper()
	d := drivers.SQLite{}
	tb := drivers.Table{Name: table}
	cols, err := d.Columns(context.Background(), db, tb)
	if err != nil {
		t.Fatal(err)
	}
	return TableSource{Driver: d, DB: db, Table: tb, Columns: cols}
}

func firstCol(p Page) []string {
	var out []string
	for _, r := range p.Rows {
		out = append(out, r[0].Text)
	}
	return out
}

func TestTablePaging(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	for _, tc := range []struct {
		table  string
		keyset bool
		pages  []string // first value of each page
	}{
		{"items", true, []string{"10", "110", "210"}},
		{"pairs", false, []string{"1", "11", "21"}}, // composite PK → OFFSET
		{"logs", false, []string{"log 01", "log 11", "log 21"}},
	} {
		t.Run(tc.table, func(t *testing.T) {
			src := source(t, db, tc.table)
			p, err := LoadTablePage(ctx, src, Page{}, First, 10)
			if err != nil {
				t.Fatal(err)
			}
			if p.Keyset != tc.keyset || len(p.Rows) != 10 || !p.HasNext {
				t.Fatalf("first page: keyset=%v rows=%d next=%v", p.Keyset, len(p.Rows), p.HasNext)
			}
			for i, want := range tc.pages {
				if got := firstCol(p)[0]; got != want || p.Index != i {
					t.Fatalf("page %d starts with %q (index %d), want %q", i, got, p.Index, want)
				}
				if i < len(tc.pages)-1 {
					if p, err = LoadTablePage(ctx, src, p, Next, 10); err != nil {
						t.Fatal(err)
					}
				}
			}
			if p.HasNext || len(p.Rows) != 5 {
				t.Fatalf("last page: rows=%d next=%v", len(p.Rows), p.HasNext)
			}

			if p, err = LoadTablePage(ctx, src, p, Prev, 10); err != nil {
				t.Fatal(err)
			}
			if got := firstCol(p); got[0] != tc.pages[1] || len(got) != 10 || p.Index != 1 || !p.HasNext {
				t.Fatalf("prev page = %v index %d", got, p.Index)
			}
			if p, err = LoadTablePage(ctx, src, p, Reload, 10); err != nil || firstCol(p)[0] != tc.pages[1] {
				t.Fatalf("reload = %v, %v", firstCol(p), err)
			}
			if p, err = LoadTablePage(ctx, src, p, Prev, 10); err != nil || firstCol(p)[0] != tc.pages[0] || p.Index != 0 {
				t.Fatalf("back to first = %v, %v", firstCol(p), err)
			}

			n, err := Count(ctx, src)
			if err != nil || n != 25 {
				t.Fatalf("count = %d, %v", n, err)
			}
		})
	}
}

func TestValues(t *testing.T) {
	db := newDB(t)
	p, err := LoadTablePage(context.Background(), source(t, db, "items"), Page{}, First, 1)
	if err != nil {
		t.Fatal(err)
	}
	row := p.Rows[0]
	if !row[1].Null {
		t.Errorf("name should be NULL: %+v", row[1])
	}
	if !row[2].Blob || row[2].Text != "<BLOB 3 B> 00ff10" {
		t.Errorf("blob = %+v", row[2])
	}
	if row[3].Text != "2024-03-01 10:20:30 +00:00" {
		t.Errorf("timestamp = %q", row[3].Text)
	}
	if p.Columns[2].DBType != "BLOB" {
		t.Errorf("column type = %q", p.Columns[2].DBType)
	}
}

func TestPlanWarnings(t *testing.T) {
	d := drivers.Postgres{}
	cases := []struct {
		sql      string
		warnings int
	}{
		{"select 1; select 2", 0},
		{"update t set a = 1 where id = 2", 1},                // no transaction
		{"delete from t", 2},                                  // no WHERE + no transaction
		{"begin; update t set a = 1 where id = 1; commit", 0}, // inside a transaction
		{"begin; delete from t; rollback", 1},                 // only no WHERE
		{"begin; rollback to savepoint s; insert into t values (1); commit", 0},
		{"begin; commit; insert into t values (1)", 1},
	}
	for _, tc := range cases {
		p, err := NewPlan(d, tc.sql, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Warnings) != tc.warnings {
			t.Errorf("%q: warnings = %q", tc.sql, p.Warnings)
		}
	}

	if _, err := NewPlan(d, "select 1; delete from t where id = 1", true); err == nil {
		t.Error("read-only should reject writes")
	}
	if _, err := NewPlan(d, "select 1; begin; select 2; commit", true); err != nil {
		t.Errorf("read-only should allow reads and tx: %v", err)
	}
	if _, err := NewPlan(d, " -- nada\n", false); err == nil {
		t.Error("empty plan should fail")
	}
}

func run(t *testing.T, db *sql.DB, sql string) (RunResult, error) {
	t.Helper()
	p, err := NewPlan(drivers.SQLite{}, sql, false)
	if err != nil {
		t.Fatal(err)
	}
	return Run(context.Background(), db, p, 10)
}

func TestRun(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	// The last result with rows is shown.
	res, err := run(t, db, "select name from items where id = 20; update logs set msg = 'x' where msg = 'log 01'; select msg from logs order by msg")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Statements) != 3 || res.Statements[1].Affected != 1 {
		t.Fatalf("statements = %+v", res.Statements)
	}
	if res.Rows == nil || res.Rows.Columns[0].Name != "msg" || len(res.Rows.Rows) != 10 || !res.Rows.HasNext {
		t.Fatalf("last result = %+v", res.Rows)
	}

	page, err := LoadRowsPage(ctx, db, *res.Rows, 2)
	if err != nil || len(page.Rows) != 5 || page.HasNext || Values(page.Rows[4])[0] != "x" {
		t.Fatalf("page 3 = %+v, %v", page.Rows, err)
	}

	// PRAGMA is not wrapped: it paginates on the client.
	res, err = run(t, db, "pragma table_info(items)")
	if err != nil || len(res.Rows.Rows) != 4 || res.Rows.wrap {
		t.Fatalf("pragma = %+v, %v", res.Rows, err)
	}

	// Statement 2 fails: execution stops and reports it.
	res, err = run(t, db, "insert into logs values ('a'); insert into nope values (1); insert into logs values ('b')")
	var ae *appErrors.AppError
	if !errors.As(err, &ae) || !strings.Contains(ae.Description, "Statement 2 of 3") || !strings.Contains(ae.Description, "previous 1") {
		t.Fatalf("error = %v", err)
	}
	if res.Failed != 1 || len(res.Statements) != 2 {
		t.Fatalf("failed = %d, statements = %d", res.Failed, len(res.Statements))
	}

	// An open transaction is rolled back at the end.
	res, err = run(t, db, "begin; insert into logs values ('tx')")
	if err != nil || !res.RolledBack {
		t.Fatalf("rolled back = %v, %v", res.RolledBack, err)
	}
	var n int
	db.QueryRow("select count(*) from logs where msg = 'tx'").Scan(&n)
	if n != 0 {
		t.Fatal("open transaction was committed")
	}

	// Cancellation.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	p, _ := NewPlan(drivers.SQLite{}, "select 1", false)
	if _, err := Run(cctx, db, p, 10); !errors.As(err, &ae) || ae.Title != "Query cancelled." {
		t.Fatalf("cancel = %v", err)
	}
}

package editing

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

func TestPending(t *testing.T) {
	var p Pending
	orig := Text("Ana")

	p.Set(0, "name", Text("Ann"), orig)
	p.Set(0, "name", Text("Anna"), orig) // overwrites
	if p.Len() != 1 || !p.Modified(0) {
		t.Fatalf("changes = %+v", p.Changes())
	}
	if v, ok := p.Value(0, "name"); !ok || v.Text != "Anna" {
		t.Fatalf("value = %+v", v)
	}
	p.Set(0, "name", orig, orig) // back to the original: no change left
	if p.Len() != 0 {
		t.Fatalf("changes = %+v", p.Changes())
	}

	// Deleting replaces the Update; a deleted row is not edited.
	p.Set(1, "email", Null, Text(""))
	p.Delete(1)
	p.Set(1, "email", Text("x"), Text(""))
	if got := p.Changes(); len(got) != 1 || got[0].Kind != Delete || !p.Deleted(1) {
		t.Fatalf("changes = %+v", got)
	}
	p.Undelete(1)
	if p.Len() != 0 {
		t.Fatal("undelete left changes")
	}

	// Insert + edit + delete of the same new row: nothing to run.
	id := p.AddRow()
	p.SetNew(id, "name", Text("Zoe"))
	if v, ok := p.NewValue(id, "name"); !ok || v.Text != "Zoe" {
		t.Fatal("new value not set")
	}
	p.DeleteNew(id)
	if p.Len() != 0 || len(p.NewRows()) != 0 {
		t.Fatalf("changes = %+v", p.Changes())
	}

	// New row IDs do not collide with page rows.
	p.Set(1, "name", Text("B"), Text("Bruno"))
	id = p.AddRow()
	p.SetNew(id, "name", Text("C"))
	if v, _ := p.Value(1, "name"); v.Text != "B" || p.Len() != 2 {
		t.Fatalf("changes = %+v", p.Changes())
	}
	p.Clear()
	if p.Len() != 0 {
		t.Fatal("clear")
	}
}

func usersTarget(d drivers.Driver, pk bool) Target {
	cols := []drivers.Column{
		{Name: "id", DBType: "integer", PK: pk, PKOrdinal: 1, AutoInc: pk},
		{Name: `na"me`, DBType: "text"},
		{Name: "email", DBType: "text", Nullable: true},
		{Name: "score", DBType: "real", Nullable: true},
	}
	if !pk {
		cols[0].PKOrdinal = 0
	}
	return Target{
		Driver:  d,
		Table:   drivers.Table{Name: "users"},
		Columns: cols,
		Page: query.Result{
			Columns: []query.Column{{Name: "id"}, {Name: `na"me`}, {Name: "email"}, {Name: "score"}},
			Rows: [][]query.Value{
				{{Text: "1", Raw: int64(1)}, {Text: "Ana", Raw: "Ana"}, {Null: true}, {Text: "1.5", Raw: 1.5}},
			},
		},
	}
}

func TestBuild(t *testing.T) {
	changes := []Change{
		{Kind: Update, Row: 0, Values: map[string]Value{"email": Text(""), `na"me`: Text("O'Neil")}},
		{Kind: Delete, Row: 0},
		{Kind: Insert, Row: 1, Values: map[string]Value{"id": {}, `na"me`: Text("Zoe"), "email": Null}},
		{Kind: Insert, Row: 2, Values: map[string]Value{}},
	}
	for _, tc := range []struct {
		name string
		d    drivers.Driver
		pk   bool
		want []string
	}{
		{"postgres", drivers.Postgres{}, true, []string{
			`UPDATE "users" SET "na""me" = $1, "email" = $2 WHERE "id" = $3`,
			`DELETE FROM "users" WHERE "id" = $1`,
			`INSERT INTO "users" ("na""me", "email") VALUES ($1, $2)`,
			`INSERT INTO "users" DEFAULT VALUES`,
		}},
		{"mysql", drivers.MySQL{}, true, []string{
			"UPDATE `users` SET `na\"me` = ?, `email` = ? WHERE `id` = ?",
			"DELETE FROM `users` WHERE `id` = ?",
			"INSERT INTO `users` (`na\"me`, `email`) VALUES (?, ?)",
			"INSERT INTO `users` () VALUES ()",
		}},
		{"sqlite without pk", drivers.SQLite{}, false, []string{
			// NULL compares with IS NULL; the float column is left out.
			`UPDATE "users" SET "na""me" = ?, "email" = ? WHERE "id" = ? AND "na""me" = ? AND "email" IS NULL`,
			`DELETE FROM "users" WHERE "id" = ? AND "na""me" = ? AND "email" IS NULL`,
			// Without AutoInc an empty id is inserted as ''.
			`INSERT INTO "users" ("id", "na""me", "email") VALUES (?, ?, ?)`,
			`INSERT INTO "users" DEFAULT VALUES`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stmts, err := Build(usersTarget(tc.d, tc.pk), changes)
			if err != nil {
				t.Fatal(err)
			}
			for i, st := range stmts {
				if st.SQL != tc.want[i] {
					t.Errorf("statement %d:\n got %s\nwant %s", i, st.SQL, tc.want[i])
				}
			}
			// NULL and '' stay distinct in the arguments.
			if want := []any{"O'Neil", ""}; !reflect.DeepEqual(stmts[0].Args[:2], want) {
				t.Errorf("update args = %#v", stmts[0].Args)
			}
			if last := stmts[2].Args[len(stmts[2].Args)-1]; last != nil {
				t.Errorf("insert NULL arg = %#v", last)
			}
		})
	}
}

func TestPreview(t *testing.T) {
	st := Statement{SQL: `UPDATE "a?b" SET "x" = $1, "y" = $2 WHERE "id" = $3`, Args: []any{"it's", nil, int64(7)}}
	want := `UPDATE "a?b" SET "x" = 'it''s', "y" = NULL WHERE "id" = 7`
	if got := st.Preview(drivers.Postgres{}); got != want {
		t.Fatalf("preview = %s", got)
	}
	st = Statement{SQL: "DELETE FROM `t` WHERE `a` = ? AND `b` = ?", Args: []any{1.5, "x"}}
	if got := st.Preview(drivers.MySQL{}); got != "DELETE FROM `t` WHERE `a` = 1.5 AND `b` = 'x'" {
		t.Fatalf("preview = %s", got)
	}
	st = Statement{SQL: "UPDATE [a@p1]]] SET [x] = @p1 WHERE [id] = @p2", Args: []any{"y", int64(3)}}
	if got := st.Preview(drivers.MSSQL{}); got != "UPDATE [a@p1]]] SET [x] = 'y' WHERE [id] = 3" {
		t.Fatalf("preview = %s", got)
	}
	st = Statement{SQL: `DELETE FROM "t:1" WHERE "id" = :1`, Args: []any{int64(3)}}
	if got := st.Preview(drivers.Oracle{}); got != `DELETE FROM "t:1" WHERE "id" = 3` {
		t.Fatalf("preview = %s", got)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		dbType, text string
		ok           bool
	}{
		{"DATE", "2024-03-01", true},
		{"date", "01/03/2024", false},
		{"timestamp with time zone", "2024-03-01 10:20:30.5 -03:00", true},
		{"TIMESTAMP", "2024-03-01T10:20:30Z", true},
		{"datetime(6)", "2024-03-01 10:20", true},
		{"timestamp", "yesterday", false},
		{"time without time zone", "10:20:30", true},
		{"TIME", "10h", false},
		{"text", "anything", true},
	} {
		err := Validate(drivers.Column{Name: "c", DBType: tc.dbType}, Text(tc.text))
		if (err == nil) != tc.ok {
			t.Errorf("%s %q: err = %v", tc.dbType, tc.text, err)
		}
	}
	if Validate(drivers.Column{Name: "c"}, Null) == nil {
		t.Error("NULL in a NOT NULL column")
	}
	if Validate(drivers.Column{Name: "c", Nullable: true}, Null) != nil {
		t.Error("NULL in a nullable column")
	}
}

// sqliteTarget opens a SQLite database with ddl and reads table's first page.
func sqliteTarget(t *testing.T, ddl, table string) (*sql.DB, Target) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "e.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range (drivers.SQLite{}).SplitStatements(ddl) {
		if _, err := raw.Exec(s.Text); err != nil {
			t.Fatal(s.Text, err)
		}
	}
	raw.Close()
	db, err := drivers.Connect(ctx, models.DBConn{Engine: models.EngineSQLite, DB: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, reload(t, db, table)
}

func reload(t *testing.T, db *sql.DB, table string) Target {
	t.Helper()
	ctx := context.Background()
	d := drivers.SQLite{}
	tb := drivers.Table{Name: table}
	cols, err := d.Columns(ctx, db, tb)
	if err != nil {
		t.Fatal(err)
	}
	src := query.TableSource{Driver: d, DB: db, Table: tb, Columns: cols}
	page, err := query.LoadTablePage(ctx, src, query.Page{}, query.First, 100)
	if err != nil {
		t.Fatal(err)
	}
	return Target{Driver: d, Table: tb, Columns: cols, Page: page.Result}
}

func rows(tg Target) []string {
	var out []string
	for _, r := range tg.Page.Rows {
		var cells []string
		for _, v := range r {
			if v.Null {
				cells = append(cells, "NULL")
			} else {
				cells = append(cells, v.Text)
			}
		}
		out = append(out, strings.Join(cells, "|"))
	}
	return out
}

func TestApply(t *testing.T) {
	ctx := context.Background()
	db, tg := sqliteTarget(t, `
		create table users (id integer primary key, name text not null, email text);
		insert into users (name, email) values ('Ana', 'ana@x'), ('Bruno', null), ('Carla', '');`, "users")

	var p Pending
	p.Set(0, "email", Null, Text("ana@x"))
	p.Set(1, "email", Text(""), Null)
	p.Delete(2)
	id := p.AddRow()
	p.SetNew(id, "id", Value{}) // empty autoincrement: omitted
	p.SetNew(id, "name", Text("Dora"))
	stmts, err := Build(tg, p.Changes())
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, db, stmts); err != nil {
		t.Fatal(err)
	}
	want := []string{"1|Ana|NULL", "2|Bruno|", "3|Dora|NULL"} // without AUTOINCREMENT SQLite reuses id 3
	if got := rows(reload(t, db, "users")); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v", got)
	}

	// A failing statement rolls back the ones before it.
	tg = reload(t, db, "users")
	stmts, _ = Build(tg, []Change{
		{Kind: Update, Row: 0, Values: map[string]Value{"email": Text("changed")}},
		{Kind: Update, Row: 1, Values: map[string]Value{"name": Null}}, // NOT NULL
	})
	err = Apply(ctx, db, stmts)
	var ae *appErrors.AppError
	if !errors.As(err, &ae) || !strings.Contains(ae.Description, "UPDATE of row 2") {
		t.Fatalf("err = %v", err)
	}
	if got := rows(reload(t, db, "users")); !reflect.DeepEqual(got, want) {
		t.Fatalf("not rolled back: %v", got)
	}

	// The row changed since it was read: no row found.
	db.Exec("update users set id = 10 where id = 1")
	stmts, _ = Build(tg, []Change{{Kind: Delete, Row: 0}})
	if err := Apply(ctx, db, stmts); !errors.As(err, &ae) || !strings.Contains(ae.Description, "found no row") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyWithoutPK(t *testing.T) {
	ctx := context.Background()
	db, tg := sqliteTarget(t, `
		create table logs (msg text, level text);
		insert into logs values ('a', 'info'), ('a', 'info'), ('b', null);`, "logs")

	// Two identical rows: the UPDATE would affect both → rollback.
	stmts, err := Build(tg, []Change{
		{Kind: Update, Row: 2, Values: map[string]Value{"level": Text("warn")}},
		{Kind: Update, Row: 0, Values: map[string]Value{"msg": Text("z")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ae *appErrors.AppError
	if err := Apply(ctx, db, stmts); !errors.As(err, &ae) || !strings.Contains(ae.Description, "2 identical rows") {
		t.Fatalf("err = %v", err)
	}
	if got := rows(reload(t, db, "logs")); got[2] != "b|NULL" {
		t.Fatalf("not rolled back: %v", got)
	}

	// The NULL row is found with IS NULL.
	stmts, _ = Build(tg, []Change{{Kind: Delete, Row: 2}})
	if err := Apply(ctx, db, stmts); err != nil {
		t.Fatal(err)
	}
	if got := rows(reload(t, db, "logs")); len(got) != 2 {
		t.Fatalf("rows = %v", got)
	}
}

func TestFormatTemporal(t *testing.T) {
	tm := time.Date(2024, 3, 1, 10, 20, 30, 500_000_000, time.FixedZone("", -3*3600))
	for _, tc := range []struct{ dbType, want string }{
		{"date", "2024-03-01"},
		{"time without time zone", "10:20:30.5"},
		{"time with time zone", "10:20:30.5 -03:00"},
		{"timestamp without time zone", "2024-03-01 10:20:30.5"},
		{"timestamp with time zone", "2024-03-01 10:20:30.5 -03:00"},
		{"DATETIME", "2024-03-01 10:20:30.5"},
	} {
		got := FormatTemporal(tc.dbType, tm)
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.dbType, got, tc.want)
		}
		// What FormatTemporal writes, Validate accepts.
		if err := Validate(drivers.Column{Name: "c", DBType: tc.dbType}, Text(got)); err != nil {
			t.Errorf("%s: %v", tc.dbType, err)
		}
	}
	if TemporalKind("integer") != NotTemporal || TemporalKind("TIMESTAMP") != DateTimeKind {
		t.Error("TemporalKind")
	}
}

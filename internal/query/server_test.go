package query

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
)

// Type conversion against real servers (ANVIL_TEST_PG / ANVIL_TEST_MYSQL /
// ANVIL_TEST_MSSQL / ANVIL_TEST_ORACLE).
// timestamptz arrives in the local time zone.
func TestServerValues(t *testing.T) {
	cases := []struct {
		env, engine, sql string
		want             []string
	}{
		{"ANVIL_TEST_PG", models.EnginePostgres,
			`select 1.50::numeric, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid, '{"a": 1}'::jsonb,
			        '\x00ff'::bytea, '2024-03-01 10:20:30.5+00'::timestamptz, '2024-03-01'::date,
			        true, null::text, ''::text, 42::bigint, array[1,2]`,
			[]string{"1.50", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", `{"a": 1}`, "<BLOB 2 B> 00ff",
				time.Date(2024, 3, 1, 10, 20, 30, 5e8, time.UTC).Local().Format(timeLayout), "2024-03-01", "true", "NULL", "", "42", "{1,2}"}},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL,
			`select cast(1.50 as decimal(5,2)), cast('{"a": 1}' as json), x'00ff',
			        timestamp('2024-03-01 10:20:30'), date('2024-03-01'), null, '', 42`,
			[]string{"1.50", `{"a": 1}`, "<BLOB 2 B> 00ff", "2024-03-01 10:20:30 +00:00", "2024-03-01", "NULL", "", "42"}},
		// Unnamed columns cannot be wrapped in SQL Server: paginated on the client.
		{"ANVIL_TEST_MSSQL", models.EngineMSSQL,
			`select cast(1.50 as decimal(5,2)), cast('A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11' as uniqueidentifier),
			        0x00ff, cast('2024-03-01 10:20:30' as datetime2), cast('2024-03-01' as date), null, N'', 42`,
			[]string{"1.50", "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11", "<BLOB 2 B> 00ff", "2024-03-01 10:20:30 +00:00", "2024-03-01", "NULL", "", "42"}},
		{"ANVIL_TEST_ORACLE", models.EngineOracle,
			`select cast(1.5 as number(5,2)) a, hextoraw('00FF') b, timestamp '2024-03-01 10:20:30' c,
			        date '2024-03-01' d, to_date('2024-03-01 10:20:30', 'YYYY-MM-DD HH24:MI:SS') e, null f, 'x' g, 42 h
			 from dual`,
			[]string{"1.5", "<BLOB 2 B> 00ff", "2024-03-01 10:20:30 +00:00", "2024-03-01", "2024-03-01 10:20:30 +00:00", "NULL", "x", "42"}},
	}
	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			v := strings.Fields(os.Getenv(tc.env))
			if len(v) != 5 {
				t.Skipf("%s not set", tc.env)
			}
			port, _ := strconv.Atoi(v[1])
			ssl := map[string]string{models.EngineMySQL: "preferred", models.EngineMSSQL: "false"}[tc.engine]
			if ssl == "" {
				ssl = "disable"
			}
			conn := models.DBConn{Engine: tc.engine, Host: v[0], Port: port, User: v[2], DB: v[4], SSLMode: ssl}
			ctx := context.Background()
			db, err := drivers.Connect(ctx, conn, v[3])
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			d, _ := drivers.For(tc.engine)

			p, err := NewPlan(d, tc.sql, false)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(ctx, db, p, 10)
			if err != nil {
				t.Fatal(err)
			}
			row := res.Rows.Rows[0]
			for i, want := range tc.want {
				got := row[i].Text
				if row[i].Null {
					got = "NULL"
				}
				if got != want {
					t.Errorf("column %d (%s): got %q, want %q", i, res.Rows.Columns[i].DBType, got, want)
				}
			}
		})
	}
}

// TestServerPagination pages a table by keyset (single PK), by OFFSET (no
// PK) and an editor query wrapped by the engine's pagination clause.
func TestServerPagination(t *testing.T) {
	for _, tc := range []struct{ env, engine, ssl string }{
		{"ANVIL_TEST_PG", models.EnginePostgres, "disable"},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL, "preferred"},
		{"ANVIL_TEST_MSSQL", models.EngineMSSQL, "false"},
		{"ANVIL_TEST_ORACLE", models.EngineOracle, "disable"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			v := strings.Fields(os.Getenv(tc.env))
			if len(v) != 5 {
				t.Skipf("%s not set", tc.env)
			}
			port, _ := strconv.Atoi(v[1])
			conn := models.DBConn{Engine: tc.engine, Host: v[0], Port: port, User: v[2], DB: v[4], SSLMode: tc.ssl}
			ctx := context.Background()
			db, err := drivers.Connect(ctx, conn, v[3])
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			d, _ := drivers.For(tc.engine)
			q := d.QuoteIdent

			for _, table := range []string{"anvil_page_pk", "anvil_page_nopk"} {
				db.Exec("DROP TABLE " + q(table))
				pk := ""
				if table == "anvil_page_pk" {
					pk = " PRIMARY KEY"
				}
				if _, err := db.Exec("CREATE TABLE " + q(table) + " (" + q("n") + " integer NOT NULL" + pk + ")"); err != nil {
					t.Fatal(err)
				}
				defer db.Exec("DROP TABLE " + q(table))
				for i := 1; i <= 5; i++ {
					if _, err := db.Exec("INSERT INTO " + q(table) + " VALUES (" + strconv.Itoa(i) + ")"); err != nil {
						t.Fatal(err)
					}
				}

				tb := drivers.Table{Name: table}
				cols, err := d.Columns(ctx, db, tb)
				if err != nil {
					t.Fatal(err)
				}
				src := TableSource{Driver: d, DB: db, Table: tb, Columns: cols}
				var pages []string
				p, err := LoadTablePage(ctx, src, Page{}, First, 2)
				for ; err == nil; p, err = LoadTablePage(ctx, src, p, Next, 2) {
					pages = append(pages, strings.Join(colValues(p.Result, 0), ","))
					if !p.HasNext {
						break
					}
				}
				if err != nil {
					t.Fatal(table, err)
				}
				if got := strings.Join(pages, " | "); got != "1,2 | 3,4 | 5" {
					t.Errorf("%s pages: %s", table, got)
				}
				if p, err = LoadTablePage(ctx, src, p, Prev, 2); err != nil || strings.Join(colValues(p.Result, 0), ",") != "3,4" {
					t.Errorf("%s prev: %v %v", table, colValues(p.Result, 0), err)
				}
			}

			plan, err := NewPlan(d, "SELECT "+q("n")+" FROM "+q("anvil_page_pk"), false)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(ctx, db, plan, 2)
			if err != nil {
				t.Fatal(err)
			}
			page, err := LoadRowsPage(ctx, db, *res.Rows, 2)
			if err != nil || len(page.Rows) != 1 || page.HasNext || !page.wrap {
				t.Fatalf("editor page: %+v %v", page, err)
			}
		})
	}
}

func colValues(r Result, col int) []string {
	out := make([]string, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = row[col].Text
	}
	return out
}

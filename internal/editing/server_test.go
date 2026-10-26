package editing

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/models"
)

// Tests against real servers, as in internal/drivers:
//
//	ANVIL_TEST_PG="host port user password database"
//	ANVIL_TEST_MYSQL="host port user password database"
func TestServerApply(t *testing.T) {
	for _, tc := range []struct {
		env, engine, ddl string
		table            drivers.Table
	}{
		{"ANVIL_TEST_PG", models.EnginePostgres, `
			drop table if exists anvil_edit;
			create table anvil_edit (id serial primary key, n int, price numeric(10,2), at timestamptz, ok bool, doc jsonb);
			insert into anvil_edit (n, price, at, ok, doc) values (1, 9.99, '2024-03-01 10:00:00+00', true, '{"a":1}');`,
			drivers.Table{Schema: "public", Name: "anvil_edit"}},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL, `
			drop table if exists anvil_edit;
			create table anvil_edit (id int auto_increment primary key, n int, price decimal(10,2), at datetime, ok bool, doc json);
			insert into anvil_edit (n, price, at, ok, doc) values (1, 9.99, '2024-03-01 10:00:00', true, '{"a":1}');`,
			drivers.Table{Name: "anvil_edit"}},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			v := os.Getenv(tc.env)
			if v == "" {
				t.Skipf("%s not set", tc.env)
			}
			f := strings.Fields(v)
			port, _ := strconv.Atoi(f[1])
			conn := models.DBConn{Engine: tc.engine, Host: f[0], Port: port, User: f[2], DB: f[4], SSLMode: "disable"}
			if tc.engine == models.EngineMySQL {
				conn.SSLMode = "preferred"
			}
			ctx := context.Background()
			db, err := drivers.Connect(ctx, conn, f[3])
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			d, _ := drivers.For(tc.engine)
			for _, s := range d.SplitStatements(tc.ddl) {
				if _, err := db.Exec(s.Text); err != nil {
					t.Fatal(s.Text, err)
				}
			}
			defer db.Exec("drop table anvil_edit")

			load := func() Target {
				cols, err := d.Columns(ctx, db, tc.table)
				if err != nil {
					t.Fatal(err)
				}
				src := query.TableSource{Driver: d, DB: db, Table: tc.table, Columns: cols}
				p, err := query.LoadTablePage(ctx, src, query.Page{}, query.First, 10)
				if err != nil {
					t.Fatal(err)
				}
				return Target{Driver: d, Table: tc.table, Columns: cols, Page: p.Result}
			}

			// Text values go into typed columns; an UPDATE that keeps the
			// same value still counts as one row (MySQL ClientFoundRows).
			var p Pending
			p.Set(0, "n", Text("42"), Text("1"))
			p.Set(0, "price", Text("12.50"), Text("9.99"))
			p.Set(0, "at", Text("2024-05-06 07:08:09 +00:00"), Text(""))
			p.Set(0, "ok", Text("0"), Text("1")) // MySQL bool is tinyint; PG takes "0" too
			id := p.AddRow()
			p.SetNew(id, "n", Text("7"))
			p.SetNew(id, "doc", Null)
			stmts, err := Build(load(), p.Changes())
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(ctx, db, stmts); err != nil {
				t.Fatal(err)
			}
			same, _ := Build(load(), []Change{{Kind: Update, Row: 0, Values: map[string]Value{"n": Text("42")}}})
			if err := Apply(ctx, db, same); err != nil {
				t.Fatal("same-value update:", err)
			}

			tg := load()
			if len(tg.Page.Rows) != 2 {
				t.Fatalf("rows = %d", len(tg.Page.Rows))
			}
			got := query.Values(tg.Page.Rows[0])
			if got[1] != "42" || got[2] != "12.50" || !strings.HasPrefix(got[3], "2024-05-06") || got[4] != "false" && got[4] != "0" {
				t.Fatalf("row = %v", got)
			}
			if !tg.Page.Rows[1][5].Null || query.Values(tg.Page.Rows[1])[1] != "7" {
				t.Fatalf("new row = %v", query.Values(tg.Page.Rows[1]))
			}

			// Delete by PK with the raw values read back.
			del, _ := Build(tg, []Change{{Kind: Delete, Row: 1}})
			if err := Apply(ctx, db, del); err != nil {
				t.Fatal(err)
			}
		})
	}
}

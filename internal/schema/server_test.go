package schema

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
)

// Tests against real servers, skipped unless these variables are set:
//
//	ANVIL_TEST_PG="host port user password database"
//	ANVIL_TEST_MYSQL="host port user password database"
func TestServerCreate(t *testing.T) {
	for _, tc := range []struct{ env, engine, ssl string }{
		{"ANVIL_TEST_PG", models.EnginePostgres, "disable"},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL, "preferred"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			f := strings.Fields(os.Getenv(tc.env))
			if len(f) != 5 {
				t.Skipf("%s not set", tc.env)
			}
			port, _ := strconv.Atoi(f[1])
			conn := models.DBConn{Engine: tc.engine, Host: f[0], Port: port, User: f[2], DB: f[4], SSLMode: tc.ssl}
			testServerCreate(t, conn, f[3])
		})
	}
}

func testServerCreate(t *testing.T, conn models.DBConn, password string) {
	ctx := context.Background()
	d, _ := drivers.For(conn.Engine)
	db, err := drivers.Connect(ctx, conn, password)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	table := "anvil_new_" + suffix
	stmt, err := CreateTable(d, Table{Name: table, Columns: []Column{
		{Name: "id", Type: "integer", PK: true, AutoInc: true},
		{Name: "name", Type: "varchar(50)", Default: "'none'"},
		{Name: "price", Type: "numeric(10, 2)", Nullable: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Exec(ctx, db, stmt, "table"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE " + d.QuoteIdent(table))

	if _, err := db.Exec("INSERT INTO " + d.QuoteIdent(table) + " (price) VALUES (1.5)"); err != nil {
		t.Fatal(err)
	}
	var id int
	var name string
	if err := db.QueryRow("SELECT id, name FROM "+d.QuoteIdent(table)).Scan(&id, &name); err != nil || id != 1 || name != "none" {
		t.Fatalf("row: %d %q %v", id, name, err)
	}
	cols, err := d.Columns(ctx, db, drivers.Table{Name: table})
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 3 || !cols[0].PK || !cols[0].AutoInc || cols[1].Nullable || !cols[2].Nullable {
		t.Fatalf("columns: %+v", cols)
	}

	name = "anvil_newdb_" + suffix
	stmt, err = CreateDatabase(d, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := Exec(ctx, db, stmt, "database"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP DATABASE " + d.QuoteIdent(name))
	dbs, err := d.ListDatabases(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(dbs), name) {
		t.Fatalf("databases = %v", dbs)
	}
}

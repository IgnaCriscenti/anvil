package session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Watts-Argentina/anvil/models"
	"gorm.io/gorm"
)

func TestSQLiteSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("create table t (id integer primary key)"); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	m := NewManager()
	defer m.CloseAll()
	ctx := context.Background()
	s, err := m.Connect(ctx, models.DBConn{Model: modelID(1), Engine: models.EngineSQLite, DB: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.AllDatabases() {
		t.Fatal("SQLite never lists databases")
	}
	if db, _ := s.Pool(ctx, "other"); db != s.DB {
		t.Fatal("Pool should return the main pool")
	}
	tables, err := s.Tables(ctx, "")
	if err != nil || len(tables) != 1 {
		t.Fatalf("tables = %v, %v", tables, err)
	}

	again, _ := m.Connect(ctx, s.Conn, "")
	if again != s {
		t.Fatal("Connect should reuse the open session")
	}
	if err := m.Disconnect(1); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(1); ok {
		t.Fatal("session still open")
	}
}

// serverConn reads ANVIL_TEST_PG / ANVIL_TEST_MYSQL, with no database configured.
func serverConn(t *testing.T, env, engine string) (models.DBConn, string, string) {
	v := strings.Fields(os.Getenv(env))
	if len(v) != 5 {
		t.Skipf("%s not set", env)
	}
	port, _ := strconv.Atoi(v[1])
	ssl := "disable"
	if engine == models.EngineMySQL {
		ssl = "preferred"
	}
	return models.DBConn{Model: modelID(2), Engine: engine, Host: v[0], Port: port, User: v[2], SSLMode: ssl}, v[3], v[4]
}

func TestAllDatabasesSession(t *testing.T) {
	for _, tc := range []struct{ env, engine string }{
		{"ANVIL_TEST_PG", models.EnginePostgres},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			conn, pass, dbName := serverConn(t, tc.env, tc.engine)
			m := NewManager()
			defer m.CloseAll()
			ctx := context.Background()

			s, err := m.Connect(ctx, conn, pass)
			if err != nil {
				t.Fatal(err)
			}
			if !s.AllDatabases() {
				t.Fatal("expected all databases")
			}
			dbs, err := s.Databases(ctx)
			if err != nil || !slices.Contains(dbs, dbName) {
				t.Fatalf("databases = %v, %v", dbs, err)
			}

			pool, err := s.Pool(ctx, dbName)
			if err != nil {
				t.Fatal(err)
			}
			if again, _ := s.Pool(ctx, dbName); again != pool {
				t.Fatal("pool not reused")
			}
			var current string
			q := "select current_database()"
			if tc.engine == models.EngineMySQL {
				q = "select database()"
			}
			if err := pool.QueryRow(q).Scan(&current); err != nil || current != dbName {
				t.Fatalf("pool connected to %q, %v", current, err)
			}
			if _, err := s.Tables(ctx, dbName); err != nil {
				t.Fatal(err)
			}

			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := pool.Ping(); err == nil {
				t.Fatal("database pool still open after Close")
			}
		})
	}
}

func modelID(id uint) gorm.Model { return gorm.Model{ID: id} }

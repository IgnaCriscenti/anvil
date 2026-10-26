// Package drivers abstracts the differences between engines (SPEC §2.2):
// connection, dialect and metadata. User databases are queried with
// database/sql; GORM is only used for the local configuration.
package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

type Driver interface {
	// Open opens the pool. If the connection is ReadOnly, every session is
	// read-only on the server side.
	Open(ctx context.Context, c models.DBConn, password string) (*sql.DB, error)
	// ListDatabases returns the server databases the user can connect to
	// (empty for SQLite).
	ListDatabases(ctx context.Context, db *sql.DB) ([]string, error)
	// MaintenanceDB is the database to connect to when the connection has
	// none configured ("" if the engine does not need one).
	MaintenanceDB() string
	// ListTables returns the tables and views of the pool's database.
	ListTables(ctx context.Context, db *sql.DB) ([]Table, error)
	Columns(ctx context.Context, db *sql.DB, table Table) ([]Column, error)
	// ForeignKeys returns the table's foreign keys, with their columns in
	// constraint order.
	ForeignKeys(ctx context.Context, db *sql.DB, table Table) ([]ForeignKey, error)
	QuoteIdent(s string) string
	// Placeholder returns the marker for parameter n (starting at 1).
	Placeholder(n int) string
	SplitStatements(sql string) []Span
	Analyze(stmt string) Statement
}

type Table struct {
	Schema string
	Name   string
	IsView bool
}

type Column struct {
	Name      string
	DBType    string
	Nullable  bool
	PK        bool
	PKOrdinal int // position in the PK, starting at 1; 0 if not part of the PK
	AutoInc   bool
}

// ForeignKey is a foreign key: Columns[i] references RefColumns[i] of
// RefTable.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefTable   Table
	RefColumns []string
}

// scanForeignKeys groups rows of (constraint, ref schema, ref table, column,
// ref column), ordered by constraint and position, into foreign keys.
func scanForeignKeys(rows *sql.Rows, err error) ([]ForeignKey, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fks []ForeignKey
	for rows.Next() {
		var name, schema, table, col, ref string
		if err := rows.Scan(&name, &schema, &table, &col, &ref); err != nil {
			return nil, err
		}
		if n := len(fks); n == 0 || fks[n-1].Name != name {
			fks = append(fks, ForeignKey{Name: name, RefTable: Table{Schema: schema, Name: table}})
		}
		fk := &fks[len(fks)-1]
		fk.Columns = append(fk.Columns, col)
		fk.RefColumns = append(fk.RefColumns, ref)
	}
	return fks, rows.Err()
}

// Span is a statement within the original SQL. Start/End are byte
// offsets; Text == sql[Start:End], without the delimiter.
type Span struct {
	Start int
	End   int
	Text  string
}

// For returns the driver for the engine.
func For(engine string) (Driver, error) {
	switch engine {
	case models.EngineSQLite:
		return SQLite{}, nil
	case models.EnginePostgres:
		return Postgres{}, nil
	case models.EngineMySQL:
		return MySQL{}, nil
	}
	return nil, appErrors.Error(
		"Unsupported database engine.",
		fmt.Sprintf("The engine %q is not supported.", engine),
		fmt.Errorf("unknown engine %q", engine),
	)
}

// AllDatabases reports whether the connection shows every server database:
// server engines with no database configured.
func AllDatabases(c models.DBConn) bool {
	return c.Engine != models.EngineSQLite && c.DB == ""
}

func scanStrings(rows *sql.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// QualifiedName returns the quoted schema.table (just the table if there is no schema).
func QualifiedName(d Driver, t Table) string {
	if t.Schema == "" {
		return d.QuoteIdent(t.Name)
	}
	return d.QuoteIdent(t.Schema) + "." + d.QuoteIdent(t.Name)
}

// TestTimeout is the limit for the Test connection button (SPEC §3.3).
const TestTimeout = 10 * time.Second

// Test opens the connection, pings it and closes it.
func Test(ctx context.Context, c models.DBConn, password string) error {
	db, err := Connect(ctx, c, password)
	if err != nil {
		return err
	}
	return db.Close()
}

// Connect opens the pool for c's engine and checks the connection with Ping.
func Connect(ctx context.Context, c models.DBConn, password string) (*sql.DB, error) {
	d, err := For(c.Engine)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()

	db, err := d.Open(ctx, c, password)
	if err != nil {
		return nil, connectError(err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, connectError(err)
	}
	return db, nil
}

func connectError(err error) error {
	return appErrors.Error(
		"Failed to connect.",
		"Check the connection settings and that the server is reachable: "+err.Error(),
		err,
	)
}

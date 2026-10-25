package drivers

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Watts-Argentina/anvil/models"
	_ "github.com/glebarez/go-sqlite" // registers "sqlite" (pure Go, the same one GORM uses)
)

type SQLite struct{}

// sqliteDSN builds a file: URI with the open mode (ro, rw or rwc). With
// ro/rw the file is not created if it does not exist.
func sqliteDSN(path, mode string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: C:/x.db → /C:/x.db
	}

	q := url.Values{}
	q.Set("mode", mode)
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")

	u := url.URL{Scheme: "file", Path: p, RawQuery: q.Encode()}
	return u.String(), nil
}

func (SQLite) Open(ctx context.Context, c models.DBConn, _ string) (*sql.DB, error) {
	if _, err := os.Stat(c.DB); err != nil {
		return nil, err
	}
	mode := "rw"
	if c.ReadOnly {
		mode = "ro"
	}
	dsn, err := sqliteDSN(c.DB, mode)
	if err != nil {
		return nil, err
	}
	return sql.Open("sqlite", dsn)
}

func (SQLite) ListDatabases(context.Context, *sql.DB) ([]string, error) { return nil, nil }

func (SQLite) MaintenanceDB() string { return "" }

func (SQLite) ListTables(ctx context.Context, db *sql.DB) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, type = 'view'
		FROM sqlite_schema
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.IsView); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (SQLite) Columns(ctx context.Context, db *sql.DB, t Table) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info(?)`, t.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []Column
	pkCount := 0
	for rows.Next() {
		var c Column
		var notNull bool
		if err := rows.Scan(&c.Name, &c.DBType, &notNull, &c.PKOrdinal); err != nil {
			return nil, err
		}
		c.Nullable = !notNull
		c.PK = c.PKOrdinal > 0
		if c.PK {
			pkCount++
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A single INTEGER PK is an alias of the rowid (autoincrement), except in
	// WITHOUT ROWID tables, which have an index with origin 'pk'.
	if pkCount == 1 && !t.IsView {
		var pkIndexes int
		err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM pragma_index_list(?) WHERE origin = 'pk'`, t.Name).Scan(&pkIndexes)
		if err != nil {
			return nil, err
		}
		for i := range cols {
			if cols[i].PK && strings.EqualFold(cols[i].DBType, "INTEGER") && pkIndexes == 0 {
				cols[i].AutoInc = true
				cols[i].Nullable = false
			}
		}
	}
	return cols, nil
}

func (SQLite) QuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (SQLite) Placeholder(int) string { return "?" }

func (SQLite) SplitStatements(sql string) []Span { return sqliteDialect.split(sql) }

func (SQLite) Analyze(stmt string) Statement { return sqliteDialect.analyze(stmt) }

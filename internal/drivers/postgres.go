package drivers

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Postgres struct{}

// pgQuote quotes a value for a libpq connection string.
func pgQuote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return "'" + strings.ReplaceAll(v, `'`, `\'`) + "'"
}

// pgConfig builds the configuration without putting the password in the
// connection string: it is set afterwards on the struct.
func pgConfig(c models.DBConn, password string) (*pgx.ConnConfig, error) {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	port := c.Port
	if port == 0 {
		port = models.DefaultPort(models.EnginePostgres)
	}

	parts := []string{
		"host=" + pgQuote(c.Host),
		"port=" + strconv.Itoa(port),
		"user=" + pgQuote(c.User),
		"sslmode=" + pgQuote(sslMode),
		"application_name=anvil",
	}
	// With no database configured, libpq would use a database named after the
	// user, which may not exist.
	dbname := c.DB
	if dbname == "" {
		dbname = Postgres{}.MaintenanceDB()
	}
	parts = append(parts, "dbname="+pgQuote(dbname))
	if c.SSLCA != "" {
		parts = append(parts, "sslrootcert="+pgQuote(c.SSLCA))
	}
	if c.SSLCert != "" {
		parts = append(parts, "sslcert="+pgQuote(c.SSLCert))
	}
	if c.SSLKey != "" {
		parts = append(parts, "sslkey="+pgQuote(c.SSLKey))
	}

	cfg, err := pgx.ParseConfig(strings.Join(parts, " "))
	if err != nil {
		return nil, err
	}
	cfg.Password = password
	if c.ReadOnly {
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}
	return cfg, nil
}

func (Postgres) Open(ctx context.Context, c models.DBConn, password string) (*sql.DB, error) {
	cfg, err := pgConfig(c, password)
	if err != nil {
		return nil, err
	}
	return stdlib.OpenDB(*cfg), nil
}

func (Postgres) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	return scanStrings(db.QueryContext(ctx, `
		SELECT datname FROM pg_database
		WHERE datallowconn AND NOT datistemplate AND has_database_privilege(datname, 'CONNECT')
		ORDER BY datname`))
}

func (Postgres) MaintenanceDB() string { return "postgres" }

func (Postgres) ListTables(ctx context.Context, db *sql.DB) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT table_schema, table_name, table_type = 'VIEW'
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_schema NOT LIKE 'pg\_toast%' AND table_schema NOT LIKE 'pg\_temp%'
		UNION ALL
		SELECT schemaname, matviewname, true FROM pg_matviews
		ORDER BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.IsView); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (Postgres) Columns(ctx context.Context, db *sql.DB, t Table) ([]Column, error) {
	schema := t.Schema
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT c.column_name,
		       CASE WHEN c.data_type IN ('USER-DEFINED', 'ARRAY') THEN c.udt_name ELSE c.data_type END,
		       c.is_nullable = 'YES',
		       c.is_identity = 'YES' OR coalesce(c.column_default, '') LIKE 'nextval(%',
		       coalesce(k.ordinal_position, 0)
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT kcu.column_name, kcu.ordinal_position
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = tc.constraint_name
			 AND kcu.table_schema = tc.table_schema
			 AND kcu.table_name = tc.table_name
			WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = $1 AND tc.table_name = $2
		) k ON k.column_name = c.column_name
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position`, schema, t.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.DBType, &c.Nullable, &c.AutoInc, &c.PKOrdinal); err != nil {
			return nil, err
		}
		c.PK = c.PKOrdinal > 0
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (Postgres) ForeignKeys(ctx context.Context, db *sql.DB, t Table) ([]ForeignKey, error) {
	schema := t.Schema
	if schema == "" {
		schema = "public"
	}
	return scanForeignKeys(db.QueryContext(ctx, `
		SELECT con.conname, rn.nspname, rc.relname, a.attname, ra.attname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class rc ON rc.oid = con.confrelid
		JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(col, ref, pos)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.col
		JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = k.ref
		WHERE con.contype = 'f' AND n.nspname = $1 AND c.relname = $2
		ORDER BY con.conname, k.pos`, schema, t.Name))
}

func (Postgres) QuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (Postgres) Placeholder(n int) string { return "$" + strconv.Itoa(n) }

func (Postgres) SplitStatements(sql string) []Span { return pgDialect.split(sql) }

func (Postgres) Analyze(stmt string) Statement { return pgDialect.analyze(stmt) }

func (Postgres) AutoIncrement(typ string) (string, bool) {
	return typ + " GENERATED BY DEFAULT AS IDENTITY", false
}

func (d Postgres) CreateDatabase(name string) string { return "CREATE DATABASE " + d.QuoteIdent(name) }

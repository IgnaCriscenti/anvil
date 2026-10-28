package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Watts-Argentina/anvil/models"
	mssql "github.com/microsoft/go-mssqldb"
)

// MSSQL is Microsoft SQL Server (and Azure SQL).
type MSSQL struct{}

// mssqlDSN builds a sqlserver:// URL with url.URL, which escapes the user,
// the password and the parameters. A host such as "srv\SQLEXPRESS" names an
// instance: with the default port it is located through SQL Browser.
func mssqlDSN(c models.DBConn, password string) (string, error) {
	port := c.Port
	if port == 0 {
		port = models.DefaultPort(models.EngineMSSQL)
	}
	host, instance, _ := strings.Cut(c.Host, `\`)

	q := url.Values{}
	q.Set("app name", "anvil")
	db := c.DB
	if db == "" {
		db = MSSQL{}.MaintenanceDB()
	}
	q.Set("database", db)
	switch c.SSLMode {
	case "", "false":
		// Only the login is encrypted, the usual setup of local servers with
		// a self-signed certificate.
		q.Set("encrypt", "false")
		q.Set("trustservercertificate", "true")
	case "true", "strict", "disable":
		q.Set("encrypt", c.SSLMode)
	case "trust":
		q.Set("encrypt", "true")
		q.Set("trustservercertificate", "true")
	default:
		return "", fmt.Errorf("unknown SQL Server encryption mode %q", c.SSLMode)
	}
	if c.ReadOnly {
		// Routes to a readable secondary in availability groups; writes are
		// blocked by anvil itself (SPEC §8).
		q.Set("ApplicationIntent", "ReadOnly")
	}

	u := url.URL{Scheme: "sqlserver", User: url.UserPassword(c.User, password), Host: host, RawQuery: q.Encode()}
	if instance == "" || port != models.DefaultPort(models.EngineMSSQL) {
		u.Host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if instance != "" {
		u.Path = "/" + instance
	}
	return u.String(), nil
}

func (MSSQL) Open(ctx context.Context, c models.DBConn, password string) (*sql.DB, error) {
	dsn, err := mssqlDSN(c, password)
	if err != nil {
		return nil, err
	}
	conn, err := mssql.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(conn), nil
}

// ListDatabases hides the system databases (master, tempdb, model, msdb).
func (MSSQL) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	return scanStrings(db.QueryContext(ctx, `
		SELECT name FROM sys.databases
		WHERE database_id > 4 AND state = 0 AND HAS_DBACCESS(name) = 1
		ORDER BY name`))
}

func (MSSQL) MaintenanceDB() string { return "master" }

func (MSSQL) ListTables(ctx context.Context, db *sql.DB) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.name, o.name, CASE o.type WHEN 'V' THEN 1 ELSE 0 END
		FROM sys.objects o
		JOIN sys.schemas s ON s.schema_id = o.schema_id
		WHERE o.type IN ('U', 'V') AND o.is_ms_shipped = 0
		ORDER BY s.name, o.name`)
	return scanTables(rows, err)
}

// scanTables reads (schema, name, is view as 0/1) rows.
func scanTables(rows *sql.Rows, err error) ([]Table, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var t Table
		var view int
		if err := rows.Scan(&t.Schema, &t.Name, &view); err != nil {
			return nil, err
		}
		t.IsView = view != 0
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (MSSQL) Columns(ctx context.Context, db *sql.DB, t Table) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.name, ty.name, c.is_nullable, c.is_identity, coalesce(ic.key_ordinal, 0)
		FROM sys.columns c
		JOIN sys.types ty ON ty.user_type_id = c.user_type_id
		LEFT JOIN sys.indexes i ON i.object_id = c.object_id AND i.is_primary_key = 1
		LEFT JOIN sys.index_columns ic
		  ON ic.object_id = i.object_id AND ic.index_id = i.index_id AND ic.column_id = c.column_id
		WHERE c.object_id = (
			SELECT o.object_id FROM sys.objects o JOIN sys.schemas s ON s.schema_id = o.schema_id
			WHERE s.name = coalesce(nullif(@p1, ''), SCHEMA_NAME()) AND o.name = @p2)
		ORDER BY c.column_id`, t.Schema, t.Name)
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

func (MSSQL) ForeignKeys(ctx context.Context, db *sql.DB, t Table) ([]ForeignKey, error) {
	return scanForeignKeys(db.QueryContext(ctx, `
		SELECT fk.name, rs.name, rt.name, pc.name, rc.name
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
		JOIN sys.objects pt ON pt.object_id = fk.parent_object_id
		JOIN sys.schemas ps ON ps.schema_id = pt.schema_id
		JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
		JOIN sys.objects rt ON rt.object_id = fk.referenced_object_id
		JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
		JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		WHERE ps.name = coalesce(nullif(@p1, ''), SCHEMA_NAME()) AND pt.name = @p2
		ORDER BY fk.name, fkc.constraint_column_id`, t.Schema, t.Name))
}

func (MSSQL) QuoteIdent(s string) string { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }

func (MSSQL) Placeholder(n int) string { return "@p" + strconv.Itoa(n) }

func (MSSQL) SplitStatements(sql string) []Span { return mssqlDialect.split(sql) }

func (MSSQL) Analyze(stmt string) Statement { return mssqlDialect.analyze(stmt) }

func (MSSQL) AutoIncrement(typ string) (string, bool) { return typ + " IDENTITY(1,1)", false }

func (d MSSQL) CreateDatabase(name string) string { return "CREATE DATABASE " + d.QuoteIdent(name) }

// Limit uses OFFSET … FETCH, which requires an ORDER BY.
func (MSSQL) Limit(orderBy, limit, offset string) string {
	if orderBy == "" {
		orderBy = " ORDER BY (SELECT NULL)"
	}
	if offset == "" {
		offset = "0"
	}
	return orderBy + " OFFSET " + offset + " ROWS FETCH NEXT " + limit + " ROWS ONLY"
}

func (MSSQL) InsertDefaults(table string, _ []Column) string {
	return "INSERT INTO " + table + " DEFAULT VALUES"
}

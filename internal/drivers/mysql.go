package drivers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/go-sql-driver/mysql"
)

type MySQL struct{}

func mysqlConfig(c models.DBConn, password string) (*mysql.Config, error) {
	port := c.Port
	if port == 0 {
		port = models.DefaultPort(models.EngineMySQL)
	}

	cfg := mysql.NewConfig()
	cfg.User = c.User
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(port))
	cfg.DBName = c.DB
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false

	switch c.SSLMode {
	case "", "preferred":
		cfg.TLSConfig = "preferred"
	case "false", "true", "skip-verify":
		cfg.TLSConfig = c.SSLMode
	case "custom":
		t, err := mysqlTLS(c)
		if err != nil {
			return nil, err
		}
		cfg.TLS = t
	default:
		return nil, fmt.Errorf("unknown MySQL SSL mode %q", c.SSLMode)
	}
	return cfg, nil
}

// mysqlTLS builds the TLS configuration with the CA and the client certificate.
func mysqlTLS(c models.DBConn) (*tls.Config, error) {
	t := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if c.SSLCA != "" {
		pem, err := os.ReadFile(c.SSLCA)
		if err != nil {
			return nil, fmt.Errorf("read SSL CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("SSL CA %s has no valid certificates", c.SSLCA)
		}
		t.RootCAs = pool
	}
	if c.SSLCert != "" || c.SSLKey != "" {
		cert, err := tls.LoadX509KeyPair(c.SSLCert, c.SSLKey)
		if err != nil {
			return nil, fmt.Errorf("load SSL client certificate: %w", err)
		}
		t.Certificates = []tls.Certificate{cert}
	}
	return t, nil
}

func (MySQL) Open(ctx context.Context, c models.DBConn, password string) (*sql.DB, error) {
	cfg, err := mysqlConfig(c, password)
	if err != nil {
		return nil, err
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	if c.ReadOnly {
		// Works the same on MySQL and MariaDB (transaction_read_only does not).
		conn = initConnector{Connector: conn, init: "SET SESSION TRANSACTION READ ONLY"}
	}
	return sql.OpenDB(conn), nil
}

// initConnector runs init on every new connection of the pool.
type initConnector struct {
	driver.Connector
	init string
}

func (c initConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	ex, ok := conn.(driver.ExecerContext)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("driver connection does not support ExecContext")
	}
	if _, err := ex.ExecContext(ctx, c.init, nil); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// ListDatabases hides the system schemas.
func (MySQL) ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	return scanStrings(db.QueryContext(ctx, `
		SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
		ORDER BY SCHEMA_NAME`))
}

func (MySQL) MaintenanceDB() string { return "" }

func (MySQL) ListTables(ctx context.Context, db *sql.DB) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE = 'VIEW'
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE()
		ORDER BY TABLE_NAME`)
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

func (MySQL) Columns(ctx context.Context, db *sql.DB, t Table) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE = 'YES',
		       c.EXTRA LIKE '%auto_increment%', coalesce(k.ORDINAL_POSITION, 0)
		FROM information_schema.COLUMNS c
		LEFT JOIN information_schema.KEY_COLUMN_USAGE k
		  ON k.TABLE_SCHEMA = c.TABLE_SCHEMA AND k.TABLE_NAME = c.TABLE_NAME
		 AND k.COLUMN_NAME = c.COLUMN_NAME AND k.CONSTRAINT_NAME = 'PRIMARY'
		WHERE c.TABLE_SCHEMA = coalesce(nullif(?, ''), DATABASE()) AND c.TABLE_NAME = ?
		ORDER BY c.ORDINAL_POSITION`, t.Schema, t.Name)
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

func (MySQL) QuoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

func (MySQL) Placeholder(int) string { return "?" }

func (MySQL) SplitStatements(sql string) []Span { return mysqlDialect.split(sql) }

func (MySQL) Analyze(stmt string) Statement { return mysqlDialect.analyze(stmt) }

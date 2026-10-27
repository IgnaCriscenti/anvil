---
name: sql-drivers
description: Implementing anvil's internal/drivers — Driver interface per engine (SQLite, PostgreSQL, MySQL/MariaDB) on top of database/sql: DSN, Open/Ping, table and column metadata, QuoteIdent, Placeholder, SplitStatements, read-only sessions and cancellation. Use when touching internal/drivers or any code that talks to the user's database.
---

# Engine drivers (SPEC §2.2)

Drivers: `github.com/jackc/pgx/v5/stdlib` (name `"pgx"`), `github.com/go-sql-driver/mysql`
(`"mysql"`), `github.com/glebarez/go-sqlite` (`"sqlite"`). All without CGO. **Never** import
`modernc.org/sqlite` directly: `glebarez/sqlite` (GORM) already registers `"sqlite"` and the double registration panics.

Implemented in `internal/drivers`: `driver.go` (interface, `For`, `Connect`, `Test`), `lexer.go` (lexical
rules per dialect), `split.go` (`SplitStatements`, `StatementAt`), `analyze.go` (`Analyze` → `Statement{Kind,
Keyword, HasWhere}`), `sqlite.go`, `postgres.go`, `mysql.go`. Integration tests against real servers with
`ANVIL_TEST_PG` / `ANVIL_TEST_MYSQL="host port user password database"` (skipped if unset).

```go
type Driver interface {
    Open(ctx context.Context, c models.DBConn, password string) (*sql.DB, error)
    ListDatabases(ctx context.Context, db *sql.DB) ([]string, error) // SPEC §3.4; nil for SQLite
    MaintenanceDB() string // database of the main connection with no DB: "postgres" in PG, "" elsewhere
    ListTables(ctx context.Context, db *sql.DB) ([]Table, error) // tables and views of the pool's database
    Columns(ctx context.Context, db *sql.DB, table Table) ([]Column, error)
    ForeignKeys(ctx context.Context, db *sql.DB, table Table) ([]ForeignKey, error) // SPEC §6.5
    QuoteIdent(s string) string
    Placeholder(n int) string // n starts at 1
    SplitStatements(sql string) []Span
    Analyze(stmt string) Statement
    AutoIncrement(typ string) (def string, inlinePK bool) // DDL of an auto-increment PK column (SPEC §3.5)
    CreateDatabase(name string) string                   // "" for SQLite
}
type Table struct { Schema, Name string; IsView bool }
type Column struct { Name, DBType string; Nullable, PK, AutoInc bool; PKOrdinal int }
type ForeignKey struct { Name string; Columns []string; RefTable Table; RefColumns []string }
type Span struct { Start, End int; Text string } // byte offsets into the original SQL
```

Foreign keys: SQLite `pragma_foreign_key_list` leaves `"to"` NULL for `REFERENCES parent` without
columns (it means the parent's PK, resolved in `ForeignKeys`); PG reads `pg_constraint` with
`unnest(conkey, confkey) WITH ORDINALITY`; MySQL reads `KEY_COLUMN_USAGE` with `REFERENCED_TABLE_NAME`.

A single registry `drivers.For(engine string) (Driver, error)`; no `switch engine` scattered around the code.

## Open

- **Never** build a DSN by concatenating strings with the password: use the config structs.
  - PG: `pgx.ParseConfig("")` and set `Host/Port/User/Password/Database`, TLS according to `SSLMode`;
    `stdlib.OpenDB(*cfg)`. Read-only: `cfg.RuntimeParams["default_transaction_read_only"] = "on"`.
  - MySQL: `mysql.NewConfig()`, `Net="tcp"`, `Addr=net.JoinHostPort(host, port)`, `ParseTime=true`,
    `MultiStatements=false`, `Loc=time.UTC`, `ClientFoundRows=true` (grid editing checks
    `RowsAffected == 1`; without it an UPDATE with the same value counts 0); `mysql.NewConnector(cfg)` +
    `sql.OpenDB`.
    Read-only: `initConnector` runs `SET SESSION TRANSACTION READ ONLY` on every new connection (works for
    MySQL and MariaDB; `transaction_read_only` does not exist in old MariaDB).
  - SQLite: `file:///absolute/path?mode=rw|ro` URI built with `url.URL` (escapes `?`, `#`, spaces; the driver
    cuts at the first `?` if it is not a URI). Enable `_pragma=foreign_keys(1)` and `_pragma=busy_timeout(5000)`.
  - MySQL v1.10: do not put `charset` in `Params` (error 1193); utf8mb4 is already the default.
- After opening: `PingContext` with a 10 s timeout (Test connection button, SPEC §3.3).
- Read-only + several statements in the same session ⇒ run on a dedicated `*sql.Conn` (`db.Conn(ctx)`), not
  on the pool, so `SET`/`BEGIN` apply to the same connection.

## DDL (SPEC §3.5)

`internal/schema` builds `CREATE TABLE` / `CREATE DATABASE`; the per-engine bits live in the driver
(`AutoIncrement`, `CreateDatabase`), never in a `switch engine`. `drivers.CreateSQLiteFile` creates an empty
database file (`mode=rwc` + a write) and never touches an existing one.

## All databases (SPEC §3.4)

`drivers.AllDatabases(conn)` = server engine and empty `DB`. `session.Session.Pool(ctx, database)` opens one
pool per database (lazily) with `conn.DB = database`; `Tables`/`Columns` take the database (`""` = the main one).
Do not qualify tables with the database in MySQL: each database has its own pool.

## Metadata

- **PG**: `information_schema.tables` (exclude `pg_catalog`, `information_schema`; `table_type` = `BASE TABLE`/`VIEW`) and
  `information_schema.columns`; PK through `pg_index`/`information_schema.key_column_usage`;
  autoinc = `is_identity = 'YES'` or `column_default LIKE 'nextval(%'`.
- **MySQL**: `information_schema.TABLES/COLUMNS` with `TABLE_SCHEMA = DATABASE()`; PK = `COLUMN_KEY='PRI'`;
  autoinc = `EXTRA LIKE '%auto_increment%'`.
- **SQLite**: `sqlite_schema` (`type IN ('table','view')`, exclude `sqlite_%`) and `pragma_table_info(?)`;
  `pk>0` gives the ordinal; autoinc = single PK of type `INTEGER` (rowid alias).
- Metadata queries use parameters, never the interpolated table name.

## Quoting and placeholders

- `QuoteIdent`: PG/SQLite `"` + double the `"`; MySQL `` ` `` + double the `` ` ``. For `schema.table`,
  quote each part separately.
- `Placeholder(n)`: PG `$n`; MySQL/SQLite `?`.

## SplitStatements

Hand-written scanner (no regex) that cuts at top-level `;`, ignoring:
- `'…'` strings (with escaped `''`), `"…"` identifiers and `` `…` `` (MySQL),
- `-- …\n` and `/* … */` comments (MySQL also `# …`),
- PG: dollar quoting `$tag$ … $tag$`; `E'…'` strings with `\'`,
- MySQL: `DELIMITER xx` changes the separator (the `DELIMITER` line is not sent to the server),
- SQLite: `BEGIN … END` of `CREATE TRIGGER` is not cut.
Returns spans with offsets to resolve "statement under the cursor" (SPEC §5.2). Ignore empty or
comment-only spans. **Table-driven tests per engine** — it is the piece with the most edge cases in the project.

## Statement classification

`Driver.Analyze(stmt) Statement` (Kind: Read / Write / DDL / Tx / Other) used by read-only (SPEC §8), the
DML-without-transaction warning and `UPDATE/DELETE` without `WHERE`. Based on the first keyword after skipping
comments and parentheses; `WITH` is Write if it contains a top-level `INSERT/UPDATE/DELETE/MERGE`.

## Cancellation

- Every call takes `ctx`. pgx sends the cancel request when the ctx is cancelled; go-sql-driver closes the
  connection (which the pool discards); glebarez/go-sqlite interrupts the query.
- After cancelling, report "Query cancelled", not a generic error.

## Scanning values

Scan into `[]any` with `rows.ColumnTypes()`; normalize: `nil` → NULL, `[]byte` → text if it is
text/JSON and BLOB otherwise (`DatabaseTypeName`), `time.Time` → ISO 8601 (SPEC §6.4). The conversion lives
here or in `internal/query`, never in the UI.

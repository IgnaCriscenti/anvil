# Anvil DBM — Specification v1

## 1. Vision

Anvil is a free (FOSS), cross-platform desktop database manager written in Go with
[go-gui](https://github.com/go-gui-org/go-gui). It is aimed at technical users in general:
connect to a database, explore its tables, run SQL and edit data safely.

| Aspect      | Decision                                   |
|-------------|--------------------------------------------|
| Users       | General FOSS audience (technical users)    |
| Platforms   | Linux (X11/Wayland), Windows, macOS        |
| Engines     | SQLite, PostgreSQL, MySQL / MariaDB        |

### 1.1 v1 scope

Included:
- Connection management (create, edit, delete, test, connect/disconnect).
- Simple list of tables and views per connection.
- SQL editor with highlighting, autocompletion and history.
- Paginated results grid.
- Data editing in the grid, with pending changes and a transaction.
- Export of results to CSV/JSON.
- Light/dark theme.

Out of scope for v1:
- Full schema explorer (indexes, FKs, triggers, DDL).
- SSH tunnel and connection folders/groups.
- Multiple SQL editor tabs per connection (see §5.1).
- Specialized JSON/JSONB editor (shown and edited as text).
- i18n (the UI stays in English) and configurable shortcuts.
- Editing the results of arbitrary queries.

---

## 2. Architecture

```
main.go
ui/                  go-gui views (layout, modals, grid, editor)
ui/components/       reusable components (IconButton, Icon, …)
internal/connections connection CRUD service + keyring
internal/storage     opening and migrating the local database
internal/settings    local preferences (single row)
internal/session     open connections: one *sql.DB per connection
internal/drivers     per-engine abstraction: connection, dialect, metadata, split and analysis
internal/query       (new) async execution, pagination, cancellation, statement splitting
internal/editing     (new) pending changes and DML generation
internal/history     (new) query history
internal/export      (new) CSV / JSON
models/              GORM models for the local configuration
pkg/appErrors        errors with Title/Description to show in the UI
assets/              own embedded SVG icons
```

### 2.1 Local persistence

- SQLite at `os.UserConfigDir()/anvil/anvil.db` (for example `~/.config/anvil/anvil.db`), accessed with GORM.
- **Pure Go** SQLite driver (`glebarez/sqlite` for GORM, which uses `glebarez/go-sqlite`, a fork of
  `modernc.org/sqlite`) to build without CGO on the 3 platforms. The same driver (`glebarez/go-sqlite`, name
  `"sqlite"`) is used for the user's SQLite connections. **Do not** import `modernc.org/sqlite` directly:
  both register `"sqlite"` and the program panics on startup.
- `AutoMigrate` on startup, with the models from §3 and §6.3.

### 2.2 Access to the user's databases

- GORM is used **only** for the local configuration. The user's databases are queried with `database/sql`,
  because columns and types are dynamic.
- Drivers: `jackc/pgx/v5/stdlib`, `go-sql-driver/mysql`, `glebarez/go-sqlite`.
- The user's SQLite databases are opened with a `file:` URI (`mode=rw`, or `mode=ro` if read-only), so
  no empty file is created if the path does not exist.
- `Driver` interface per engine:
  - `Open(ctx, DBConn, password) (*sql.DB, error)`
  - `ListTables(ctx) ([]Table, error)`: tables and views.
  - `Columns(ctx, table) ([]Column, error)`: name, type, nullable, whether it is a PK, whether it is identity/autoincrement.
  - `QuoteIdent(string) string`: `"x"` in PG/SQLite, `` `x` `` in MySQL.
  - `Placeholder(n int) string`: `$n` in PG, `?` in MySQL/SQLite.
  - `SplitStatements(sql string) []Span`: respects strings, comments, `$$` (PG) and `DELIMITER` (MySQL).

---

## 3. Connection management

### 3.1 `DBConn` model

| Field        | Type     | Notes                                                       |
|--------------|----------|-------------------------------------------------------------|
| `gorm.Model` |          | ID, timestamps, soft delete                                 |
| `Name`       | string   | Visible alias, required                                     |
| `Color`      | string   | Hex (`#RRGGBB`) for the indicator in the sidebar            |
| `Engine`     | string   | `sqlite` \| `postgres` \| `mysql`                           |
| `Host`       | string   | Not applicable to SQLite                                    |
| `Port`       | int      | Defaults to 5432 / 3306                                     |
| `User`       | string   | Not applicable to SQLite                                    |
| `DB`         | string   | Database name; for SQLite it is the file path. Empty in PG/MySQL = all databases (§3.4) |
| `ReadOnly`   | bool     | Blocks writes (§7)                                          |
| `SSLMode`    | string   | PG: `disable/require/verify-ca/verify-full`; MySQL: `false/true/skip-verify/custom` |
| `SSLCA`      | string   | Path to the CA                                              |
| `SSLCert`    | string   | Path to the client certificate                              |
| `SSLKey`     | string   | Path to the client key                                      |
| `SavePassword` | bool   | If false, the password is asked for on connect              |

### 3.2 Passwords

- Stored in the OS keyring (service `ANVIL-DBM`, key = `strconv.FormatUint(ID, 10)`).
- If the keyring is not available (for example Linux without Secret Service): **it is not persisted**.
  `SavePassword=false` is set and the password is asked for on every connection. The user is warned once.
- Deleting a connection also deletes its keyring entry.
- The password stays in memory only while the connection is open.

### 3.3 Operations

- **Create and edit**: modal with the fields for the engine (SQLite shows a file picker instead of
  host/port/user). **Test connection** button (Open + Ping with a 10 s timeout).
- **Delete**: confirmation, then deletion of the record and the keyring entry.
- **Connect/disconnect**: from the sidebar. One `*sql.DB` is kept per open connection.

### 3.4 Connection without a database (all databases)

- If the `DB` field is left empty in PostgreSQL or MySQL, the connection shows **every database on the
  server** the user can access, and each database expands to show its tables.
  - PG: `pg_database` without templates, with `datallowconn` and the `CONNECT` privilege. The main connection
    goes to the `postgres` maintenance database (without a database, libpq would use one named after the user).
  - MySQL: `information_schema.SCHEMATA`, hiding `information_schema`, `performance_schema`, `mysql` and `sys`.
- Each database is opened in its own pool the first time it is expanded (`Session.Pool`), with the password
  that stays in memory while the connection is open. Every pool is closed on disconnect.

---

## 4. Layout

```
┌─ [+Add] [x Delete] ─────────────────┐
│ ● prod-pg    │ [users] [SQL] [+]     │
│   users      │──────────────────────│
│   orders     │ id │ name  │ email    │
│ ● local.db   │  1 │ Ana   │ a@x.com  │
│              │  2 │ Bruno │ b@x.com  │
│              │ [Save 2] [Discard] 1/40│
└──────────────┴──────────────────────┘
```

- **Top bar**: global actions (new connection, delete, connect, export, theme).
- **Sidebar**: connections with their color dot. Expanding an open connection shows a flat list of
  tables and views, with a text filter. If the connection has no database (§3.4), the databases are listed
  first and each one expands into its table list and its filter.
- **Central area**: clicking a table opens it with three tabs:
  - **Structure**: columns with type, nullable and PK/autoincrement.
  - **Data**: paginated grid of the table (§6), read-only for now (editing is §7).
  - **SQL**: SQL editor that runs against that table's database (§5). The editor text and its last result
    are per database: they are kept when switching between tables of the same database.
  The selected tab is kept when opening another table.
- **Status bar**: active connection, query status, time, rows, pagination.

---

## 5. SQL editor

### 5.1 Features

- One SQL editor per database (SQL tab of each table, §4). Multiple tabs are left for later.
- Editor: `github.com/go-gui-org/go-edit` (pure Go; selection, undo, line numbers).
- **Syntax highlighting** with `chroma` through go-edit (lexer by engine: `postgresql`, `mysql`, `sql`;
  `github-dark` / `github` style depending on the theme).
- **Autocompletion**: dialect keywords, table and column names of the active connection
  (cached on connect, with manual refresh).
- **History** per connection, persisted locally (§6.3), with search and the option to load a query back
  into the editor.

### 5.2 Execution

- `Ctrl+Enter` runs the **selection**; if there is no selection, it runs the **statement under the cursor**
  (determined with `SplitStatements`).
- **Several statements** in the selection: they run in order, in the same session.
  - If several return rows (for example 2 SELECTs), only the **last** result is shown. The status bar
    keeps the summary of each statement (rows or affected rows, and duration).
  - If any statement is an `UPDATE`/`DELETE` (or `INSERT`) and is not inside an explicit transaction
    (`BEGIN … COMMIT/ROLLBACK`), a **warning** is shown before running ("modifies data without a
    transaction") and the user chooses whether to continue or cancel.
  - If a statement fails, execution stops and the app reports which one failed and which statements had
    already run.
- Execution is **asynchronous**: it runs in a goroutine with a `context.WithCancel`. The UI does not block and
  the **Stop** button cancels the query (cancels the context; in PG it sends the driver's cancel request).
- If the statement returns rows, they are shown in the results grid (**read-only**, §6.4). Otherwise,
  the affected rows and the duration are shown.
- Errors are shown with `appErrors` (title and description, plus the engine message).

---

## 6. Grid and pagination

### 6.1 Page size

- Selectable between **20 and 500** rows (100 by default). Remembered per tab.

### 6.2 Pagination strategy

- **Keyset** if the table has a single PK with identity/autoincrement (or a single sortable PK):
  `SELECT … WHERE pk > :last ORDER BY pk LIMIT :n` (and `<` with `ORDER BY pk DESC` to go back).
- **OFFSET** in every other case: `SELECT … ORDER BY <pk or nothing> LIMIT :n OFFSET :k`.
- SQL editor results are paginated by wrapping the user's query:
  `SELECT * FROM (<query>) AS q LIMIT :n OFFSET :k`. This only applies to SELECT/WITH statements; everything
  else runs as is.
- The total row count (`COUNT(*)`) is computed on demand (button), not on every page.

### 6.3 Additional local models

- `QueryHistory { gorm.Model; ConnID uint; SQL string; Duration int64; RowsAffected int64; Error string }`.
- `Settings { Theme string; DefaultPageSize int }` (single row).

### 6.4 Data types

| Type               | Display                               | Editing                     |
|--------------------|---------------------------------------|-----------------------------|
| Text / numbers     | As is                                 | Text input                  |
| Date / timestamp   | ISO 8601 (`2006-01-02 15:04:05[.000] ±07:00`) | Input with format validation |
| BLOB / bytea       | `<BLOB 1.2 KB>` + first bytes in hex  | **Read-only**               |
| JSON / JSONB       | Text                                  | Text input                  |
| NULL               | Dimmed `NULL`, distinct from `''`     | Can be set to NULL (`Ctrl+0` or context menu) if the column is nullable |

The grids (Structure, Data and SQL results) draw every border, inner and outer, and the header row has a
background different from the data rows. Cells show only the first line of the value, cut at 30 characters
and ending in `…` if something was trimmed. Columns that do not fit in the panel width are reachable with
horizontal scroll (scrollbar or Shift+wheel).

---

## 7. Grid data editing

### 7.1 Where editing is allowed

- Only in **table tabs** opened from the sidebar. SQL editor results are **never** editable.
- If the connection is marked `ReadOnly`, the grid is read-only.
- Views are read-only.

### 7.2 Pending changes

- Every edit, insertion or row deletion stays **pending**. Modified cells are highlighted, new rows are
  marked with `+` and deleted rows are struck through.
- **Save (N)** opens a preview of the SQL that will run and, on confirmation, applies everything in **one
  transaction**. If a statement fails, everything is rolled back and the failing one is reported.
- **Discard** drops every pending change.
- Closing the tab or changing page with pending changes asks for confirmation.

### 7.3 Row identification

- **With a PK**: `UPDATE/DELETE … WHERE pk1 = ? [AND pk2 = ?]`.
- **Without a PK**: the `WHERE` uses **every column** with its original values (`col IS NULL` for
  NULLs; BLOBs and non-comparable types are excluded). A warning is shown before enabling editing.
- **Single-row guarantee**: after each `UPDATE`/`DELETE`, `RowsAffected == 1` is checked. If it is any
  other number, the **whole transaction is rolled back** and the offending row is reported
  (for example "would affect 3 identical rows").

### 7.4 DML generation

- Always with parameters (`Placeholder`) and table and column names with `QuoteIdent`; values are never
  concatenated.
- `INSERT` omits identity/autoincrement columns left empty.

---

## 8. Safeguards

- **Read-only connection**: besides disabling grid editing, the SQL editor rejects statements that are not
  reads (`SELECT`, `WITH` without DML, `SHOW`, `EXPLAIN`, read `PRAGMA`s).
  In PG the session is also opened with `default_transaction_read_only = on`; in MySQL with
  `SET SESSION TRANSACTION READ ONLY`.
- **DELETE/UPDATE without WHERE**: when the SQL editor detects an `UPDATE` or `DELETE` without `WHERE`, it asks
  for explicit confirmation before running it.

---

## 9. Export

- From any grid: **CSV** (RFC 4180, UTF-8, with headers) or **JSON** (array of objects).
- Scope: the current page or the full result (re-run in streaming, without loading everything into
  memory).
- NULL: empty cell in CSV and `null` in JSON. BLOB as base64.

---

## 10. Theme

- Light and dark, switchable from the top bar and persisted in `Settings`.
- Icons are own SVGs with `stroke="currentColor"`; they are painted with the active theme's text color
  (`SvgCfg.Color`), so they must be re-rendered when the theme changes.

---

## 11. Errors

- Every error that reaches the UI is an `*appErrors.AppError` (Title + Description + wrapped Err).
- Add `Unwrap() error` to `AppError` so `errors.Is/As` can be used.
- UI messages stay in English (no i18n in v1).

---

## 12. Current state and detected debt

The 6 findings from the initial review are resolved (milestone 1):

1. ~~`string(conn.ID)` as the keyring key~~ → `strconv.FormatUint` (`connections.keyringKey`).
2. ~~Connection created even if `keyring.Set` fails~~ → `Save` runs in a transaction and, if the keyring fails,
   saves with `SavePassword=false` and returns `SaveResult.Warning`.
3. ~~`delete.go` does not delete the keyring entry or check rows~~ → looks up the row (not found), deletes it and cleans the keyring.
4. ~~Incomplete message in `password.go`~~ → complete messages and wrapped `err` (`ErrPasswordNotFound`).
5. ~~300×300 window with a red border~~ → 1200×760 window with the §4 layout.
6. ~~Empty `internal/interfaces`~~ → removed.

### 12.1 Progress

- **Milestone 1 (done)**: models, local database, connection service, drivers (SQLite/PG/MySQL with metadata,
  `SplitStatements`, `Analyze`), sessions, settings, UI with top bar, sidebar (connections + tables with
  filter), create/edit modal with Test connection, password prompt, delete with confirmation, persisted
  light/dark theme and structure view of the selected table.
- **Milestone 2 (done)**: Structure / Data / SQL tabs per table, read-only paginated grid (keyset or
  OFFSET, on-demand COUNT, dimmed NULL, summarized BLOB), SQL editor with go-edit: Ctrl+Enter (selection or
  statement under the cursor), several statements with the last result, warnings (no WHERE / no transaction),
  read-only, Stop, results pagination and saving to history (`internal/query`, `internal/history`).
- **Milestone 3**: autocompletion, history panel with search, metadata refresh.
- **Milestone 4**: grid editing (`internal/editing`).
- **Milestone 5**: export, packaging and release.

### 12.2 Known dependency limitations

- go-gui v0.92: a vertical `gui.Splitter` inside another `gui.Splitter` doubles the row height of
  `gui.Table`. The SQL tab splits editor/results in a fixed proportion (45 %) instead of using a splitter.
- go-gui v0.92: `datagrid.GridCellFormat` has no public constructor; the grids use `gui.Table`, which supports
  per-cell styling.
- go-gui v0.92: a horizontally scrolling container stores the content width before the table takes its
  final width and clips the scroll to that value. The grid measures its natural width every frame and passes
  it as `MinWidth` (`naturalWidth` in `ui/resultGrid.go`).
- go-gui v0.92: `gui.Table` has no header color; row 0 is painted as selected (`gridStyle`).
- go-edit v0.12: `ActionFunc` takes an unexported type; the run action is declared generic
  (`runAction[S any]`) and reads the cursors through reflection.
- When an execution ends with an open transaction (`BEGIN` without `COMMIT`), a `ROLLBACK` is issued and the
  status bar says so: each execution uses its own session.

---

## 13. Distribution and license

- **License**: MIT (`LICENSE` file at the root).
- **Binaries** on GitHub Releases for Linux (amd64/arm64), Windows (amd64) and macOS (amd64/arm64),
  built in CI (for example with GoReleaser). No CGO thanks to the pure Go SQLite (§2.1).
- **Packages**: AUR (Linux), Homebrew (macOS) and winget (Windows), published from the same release
  pipeline.

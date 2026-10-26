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
- Full schema explorer (indexes, triggers, DDL). Foreign keys are only read to navigate between rows (§6.5).
- SSH tunnel and connection folders/groups.
- Multiple SQL editor tabs per connection (see §5.1).
- Specialized JSON/JSONB editor (shown and edited as text).
- i18n (the UI stays in English) and configurable shortcuts.
- Editing the results of arbitrary queries.

---

## 2. Architecture

```
main.go
ui/                  go-gui views (layout, modals, grid and its editing, SQL editor)
ui/components/       reusable components (IconButton, Icon, …)
internal/connections connection CRUD service + keyring
internal/storage     opening and migrating the local database
internal/settings    local preferences (single row)
internal/session     open connections: one *sql.DB per connection
internal/drivers     per-engine abstraction: connection, dialect, metadata, split and analysis
internal/query       async execution, pagination (with FK filter), cancellation
internal/editing     pending changes, validation, DML generation and the save transaction
internal/history     query history
internal/export      (pending) CSV / JSON
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
  - `ListDatabases(ctx) ([]string, error)` and `MaintenanceDB() string`: for connections without a database (§3.4).
  - `ListTables(ctx) ([]Table, error)`: tables and views.
  - `Columns(ctx, table) ([]Column, error)`: name, type, nullable, whether it is a PK, whether it is identity/autoincrement.
  - `ForeignKeys(ctx, table) ([]ForeignKey, error)`: name, columns, referenced table and columns (in order).
  - `QuoteIdent(string) string`: `"x"` in PG/SQLite, `` `x` `` in MySQL.
  - `Placeholder(n int) string`: `$n` in PG, `?` in MySQL/SQLite.
  - `SplitStatements(sql string) []Span`: respects strings, comments, `$$` (PG) and `DELIMITER` (MySQL).
  - `Analyze(stmt string) Statement`: kind of statement (read/write, DML, `WHERE`, transaction control) for
    the safeguards (§8).
- MySQL connections use `ClientFoundRows` so that `RowsAffected` counts the rows found, not only the changed
  ones (§7.3).

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
┌─ [+Add] [Edit] [Delete] [Disconnect] ────────────────────────────────── [☀] ┐
│ ● prod-pg    │ ‹ users                                                      │
│   users      │ [Structure] [Data] [SQL]                                     │
│   orders     │ ⟳ ‹ Page 1 › [100▾] rows per page     Count rows             │
│ ● local.db   │  # │ id │ name  │ email   │ customer_id                      │
│              │  + │    │ Dora  │ DEFAULT │ DEFAULT                          │
│              │  1 │ 1  │ Ana   │ a@x.com │ 3 [→]                            │
│              │  2 │ 2  │ Bruno │ NULL    │ 1 [→]                            │
│              │                                                              │
│              │ [Save (2)] [Cancel] │ [Add] [Delete] │ [Set NULL] [Set Now]  │
└──────────────┴──────────────────────────────────────────────────────────────┘
 status bar
```

- **Top bar**: global actions (add, edit and delete connections, connect/disconnect, theme).
- **Sidebar**: connections with their color dot. Expanding an open connection shows a flat list of
  tables and views, with a text filter. If the connection has no database (§3.4), the databases are listed
  first and each one expands into its table list and its filter.
- **Central area**: clicking a table opens it with three tabs:
  - **Structure**: columns with type, nullable, PK/autoincrement and FK.
  - **Data**: paginated grid of the table (§6), with foreign key navigation (§6.5) and editing (§7) through
    the bar below the grid.
  - **SQL**: SQL editor that runs against that table's database (§5). The editor text and its last result
    are per database: they are kept when switching between tables of the same database.
  The selected tab is kept when opening another table. A table opened by following a FK has a back button
  (`‹`) next to its name.
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
| Text / numbers     | As is                                 | Inline text input           |
| Date / time / timestamp | ISO 8601 (`2006-01-02 15:04:05[.000] ±07:00`) | Dialog with a calendar (dates) and an `HH:MM:SS` field (times), or **Set Now**; never typed text |
| BLOB / bytea       | `<BLOB 1.2 KB>` + first bytes in hex  | **Read-only**               |
| JSON / JSONB       | Text                                  | Inline text input           |
| NULL               | Dimmed `NULL`, distinct from `''`     | **Set NULL** (`Ctrl+0` in the input) if the column is nullable |

The grids (Structure, Data and SQL results) draw every border, inner and outer, and the header row has a
background different from the data rows. The Data grid starts with a `#` column that numbers the rows shown
from 1. Cells show only the first line of the value, cut at 30 characters
and ending in `…` if something was trimmed. Columns that do not fit in the panel width are reachable with
horizontal scroll (scrollbar or Shift+wheel).

### 6.5 Foreign key navigation

- When a table is opened its foreign keys are read along with its columns (not for views). The Structure tab
  shows them in the Key column (`FK → table.column`).
- In the Data tab, every non-NULL cell of a foreign key column has a link button after the value. It opens the
  referenced table in the Data tab, filtered by `refcol = value` (every column of a composite FK); the
  filter is shown in the toolbar and applies to pagination and `COUNT(*)`.
- A table opened this way has a back button in its header that returns to the previous table with its page
  as it was. Opening a table from the sidebar starts over.
- SQL editor results have no links (their columns are not tied to a table).

---

## 7. Grid data editing

### 7.1 Where editing is allowed

- Only in the **Data tab** of a table, opened from the sidebar or by following a foreign key (§6.5). SQL
  editor results are **never** editable.
- If the connection is marked `ReadOnly`, the grid is read-only.
- Views are read-only. BLOB columns are read-only.

### 7.2 Pending changes

- Every edit, insertion or row deletion stays **pending** until it is saved. Modified cells are highlighted,
  new rows go on top of the grid marked `+` (unset columns show `DEFAULT`) and deleted rows are struck
  through (their `#` number too).
- Pending changes refer to the rows of the current page.
- **Save (N)** opens a preview of the SQL that will run (with the values inlined, only to show them) and, on
  confirmation, applies everything in **one transaction**, in the order the changes were made. If a
  statement fails, everything is rolled back, the error is shown in the preview naming the change
  (for example "The DELETE of row 1 failed: …") and the changes stay pending so they can be fixed.
- **Cancel** discards every pending change.
- Changing page, refreshing, changing the page size, going back, opening another table, disconnecting or
  deleting the connection with pending changes asks first: **Save…**, **Discard** or **Cancel**.
- Closing the window with unsaved changes (including a cell being edited) asks to **Keep editing** or
  **Discard and close**.

### 7.3 Row identification

- **With a PK**: `UPDATE/DELETE … WHERE pk1 = ? [AND pk2 = ?]` with the original values.
- **Without a PK**: the `WHERE` uses **every comparable column** with its original values (`col IS NULL`
  for NULLs; binary, JSON, XML, geometric and floating point columns are excluded). A warning is shown
  before the first change to an existing row.
- **Single-row guarantee**: after each `UPDATE`/`DELETE`, `RowsAffected == 1` is checked. If it is any
  other number, the **whole transaction is rolled back** and the offending row is reported
  (for example "would affect 3 identical rows", or "found no row" if it was changed or deleted meanwhile).

### 7.4 DML generation

- Always with parameters (`Placeholder`) and table and column names with `QuoteIdent`; values are never
  concatenated. Values are sent as text (NULL as `nil`) and the engine converts them to the column type.
- `INSERT` only lists the columns the user set, omitting identity/autoincrement columns left empty; with no
  columns it inserts the defaults.
- MySQL does not accept the offset the grid shows in dates (`… +00:00`): dates are sent in UTC without it.
  MySQL `BOOL` is `TINYINT` (0/1).

### 7.5 Editing in the Data tab

- **Selection**: clicking a cell selects its row and opens the cell's editor. Ctrl+click (Cmd on macOS) adds
  or removes rows and Shift+click selects a range from the last row picked, without editing. Clicking a `#`
  number selects the row without editing. Selected rows have their number filled with the accent color and a
  tinted background; the clicked cell is underlined.
- **Editors**: text and numbers use an inline input: Enter or leaving the cell keeps the value, Esc
  discards it, `Ctrl+0` sets NULL. **Dates and times never ask for text**: a dialog with a calendar (dates)
  and an `HH:MM:SS` field (times) opens on the cell's value, with **Now**, **Set NULL** (if nullable),
  **Cancel** and **OK**. The offset is kept for `with time zone` types.
- **Bar below the grid**:

  | Group | Buttons | Acts on |
  |-------|---------|---------|
  | Changes | **Save (N)**, **Cancel** | Every pending change |
  | Rows | **Add**, **Delete (N)** / **Restore** | Add: a new row on top, editing its first column. Delete: the selected rows (new rows are removed); Restore when all of them are already deleted |
  | Values | **Set NULL**, **Set Now** | The column of the clicked cell in every selected row (or the cell being edited). Set NULL only for nullable columns; Set Now only for date/time columns |

  On the right, the bar shows how many rows are selected and a summary of the pending changes.

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
- **Foreign key navigation** in the Data tab (§6.5): link buttons on FK cells, filtered referenced table,
  back button.
- **Milestone 3**: autocompletion, history panel with search, metadata refresh.
- **Milestone 4 (done)**: grid editing (`internal/editing`, §7): pending changes, SQL preview, single
  transaction with the `RowsAffected == 1` check, row identification with or without a PK, multi-row
  selection with the `#` column, bar below the grid (Save, Cancel | Add, Delete | Set NULL, Set Now),
  calendar/time editor for dates, and confirmation before leaving or closing the window with unsaved
  changes.
- **Milestone 5**: export, packaging and release.

### 12.2 Known dependency limitations

- go-gui v0.92: a vertical `gui.Splitter` inside another `gui.Splitter` doubles the row height of
  `gui.Table`. The SQL tab splits editor/results in a fixed proportion (45 %) instead of using a splitter.
- go-gui v0.92: `datagrid.GridCellFormat` has no public constructor; the grids use `gui.Table`, which supports
  per-cell styling.
- go-gui v0.92: a horizontally scrolling container stores the content width before the table takes its
  final width and clips the scroll to that value. The grid measures its natural width every frame and passes
  it as `MinWidth` (`naturalWidth` in `ui/resultGrid.go`).
- go-gui v0.92: `gui.Table` has no header color; row 0 is painted as selected (`gridStyle`). It has no
  per-row background either: selected rows are shown with the `#` number filled and a text background.
- go-gui v0.92: `gui.Table` measures column widths from `TableCellCfg.Value` even when the cell has
  `Content` (buttons, inputs): the value is padded to make room. `TableCellCfg.ID` is not applied to the cell.
- go-gui v0.92: `gui.DatePicker` only picks days; times use a masked input next to it.
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

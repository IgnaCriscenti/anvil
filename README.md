# Anvil

A free, cross-platform desktop database manager written in Go with
[go-gui](https://github.com/go-gui-org/go-gui). Use it to connect to a database, explore its tables,
run SQL and (soon) edit data safely.

- **Engines**: SQLite, PostgreSQL and MySQL / MariaDB.
- **Platforms**: Linux (X11/Wayland), Windows and macOS.
- **License**: MIT.

> Anvil is under active development. The full specification is in [`SPEC.md`](SPEC.md).

## Features

### Available

- **Connections**: create, edit, delete and test (*Test connection*), with a color per connection to
  identify it in the sidebar.
- **Passwords in the system keyring** (service `ANVIL-DBM`). If no keyring is available, the password is
  not stored and is asked for on connect.
- **SSL/TLS** for PostgreSQL and MySQL (mode, CA, client certificate and key).
- **Connection without a database**: in PostgreSQL and MySQL, if no database is given, every database on
  the server is listed and each one expands to show its tables.
- **Sidebar** with tables and views per connection, with a text filter.
- **Tabs per table**:
  - **Structure**: columns with type, nullable and PK/autoincrement.
  - **Data**: paginated grid (keyset or `OFFSET`), page size from 20 to 500 rows, on-demand `COUNT(*)`,
    dimmed `NULL` and summarized BLOBs.
  - **SQL**: editor with syntax highlighting that runs against the table's database.
- **SQL execution**:
  - `Ctrl+Enter` runs the selection or, if there is no selection, the statement under the cursor.
  - Several statements run in order and the last result is shown.
  - Asynchronous execution with a **Stop** button to cancel.
  - Query history stored locally.
- **Safeguards**:
  - Warning before running `UPDATE`/`DELETE` without `WHERE` or DML outside an explicit transaction.
  - **Read-only** connections: the editor rejects write statements and the session is opened in read-only
    mode on the engine.
- **Light and dark theme**, persisted across sessions.

### Coming up

- Autocompletion of keywords, tables and columns; history panel with search.
- Data editing in the grid with pending changes, SQL preview and saving in a single transaction.
- Export to CSV and JSON.
- Binaries on GitHub Releases and packages for AUR, Homebrew and winget.

## Requirements

- [Go](https://go.dev/dl/) 1.27 or later.
- **Linux / Windows**: no CGO needed (`CGO_ENABLED=0`).
- **macOS**: needs CGO and the Xcode command line tools (go-gui's Metal backend uses Objective-C).
- Optional: a system keyring (Secret Service on Linux, Keychain on macOS, Credential Manager on Windows)
  to store passwords.

## Build and run

```sh
git clone https://github.com/Watts-Argentina/anvil.git
cd anvil
go run .
```

To build a binary:

```sh
go build -o anvil .
```

## Local configuration

Anvil stores its connections, preferences and history in a SQLite database inside the user's
configuration directory:

| System  | Path                                              |
|---------|---------------------------------------------------|
| Linux   | `~/.config/anvil/anvil.db`                        |
| macOS   | `~/Library/Application Support/anvil/anvil.db`    |
| Windows | `%AppData%\anvil\anvil.db`                        |

Passwords are never stored in that database: they go to the system keyring or are asked for on connect.

## Tests

```sh
go test ./...
```

Tests against real PostgreSQL and MySQL servers are skipped unless these variables are set
(five space-separated fields):

```sh
export ANVIL_TEST_PG="localhost 5432 user password database"
export ANVIL_TEST_MYSQL="localhost 3306 user password database"
go test ./...
```

## Project layout

```
main.go
ui/                   go-gui views (layout, sidebar, tabs, grid, SQL editor)
ui/components/        reusable components
internal/connections  connection CRUD + keyring
internal/drivers      per-engine abstraction: connection, dialect, metadata, SQL split and analysis
internal/session      open connections (one pool per database)
internal/query        execution, pagination and cancellation
internal/history      query history
internal/settings     local preferences
internal/storage      opening and migrating the local database
models/               GORM models for the local configuration
pkg/appErrors         errors with a title and description for the UI
assets/               embedded SVG icons
agents/skills/        guides for coding agents working on the repo
```

## Contributing

1. Read [`SPEC.md`](SPEC.md): it defines the scope, the design decisions and the status of each milestone.
2. Open an issue before large changes.
3. Make sure `go vet ./...` and `go test ./...` pass before sending the PR.

## License

Distributed under the [MIT](LICENSE) license.

---
name: query-execution
description: SQL execution in anvil (internal/query, internal/history, internal/export) — Ctrl+Enter with the selection or the current statement, multiple statements, warnings (DML without a transaction, UPDATE/DELETE without WHERE, read-only), cancellable async execution, keyset/OFFSET pagination, on-demand COUNT, history and streaming CSV/JSON export. Use when implementing the SQL editor, table tab loading or export.
---

# Query execution (SPEC §5.2, §6, §8, §9)

Implemented in `internal/query`: `value.go` (Value/Result/scan/convert), `table.go` (TableSource,
LoadTablePage keyset/OFFSET, Count), `plan.go` (NewPlan: split, read-only, warnings), `exec.go` (Run on a
`*sql.Conn`, RowsPage, LoadRowsPage, rollback of an open transaction). History in `internal/history`.

## Editor execution flow

1. Text to run: the selection; if there is none, the `SplitStatements` `Span` that contains the cursor.
2. Split into statements and `Classify` each one (`sql-drivers` skill).
3. **Pre-checks, in this order**, all before sending anything to the server:
   - `ReadOnly` connection and some non-Read statement → reject with `appErrors` (nothing runs).
   - `UPDATE`/`DELETE` without `WHERE` → explicit confirmation.
   - DML (`INSERT`/`UPDATE`/`DELETE`) outside an explicit `BEGIN … COMMIT/ROLLBACK` block →
     "modifies data without a transaction" warning with Continue/Cancel.
   A single dialog listing every warning is better than several in a row.
4. Run in a goroutine with `ctx, cancel := context.WithCancel(...)`; keep `cancel` in the tab state for the
   **Stop** button. Use a dedicated `*sql.Conn` so every statement shares the session.
5. In order: if the statement returns rows → read the first page; otherwise → `RowsAffected` + duration.
   If one fails: stop, report which one failed (index + trimmed text) and which ones already ran.
6. Show only the **last** result with rows; the status bar shows each statement's summary.
7. Come back to the UI thread with `w.QueueCommand` (or `gui.Stream` for progress). Never touch the window
   state directly from the goroutine.
8. Save to `QueryHistory` (ConnID, SQL, Duration, RowsAffected, Error), even if it failed.

One result type per statement, for example:
```go
type StmtResult struct {
    SQL      string
    Columns  []ColumnInfo
    Rows     [][]Value   // Value distinguishes NULL, text, BLOB
    Affected int64
    Duration time.Duration
    Err      error
}
```

## Pagination (SPEC §6.1–6.2)

- Page size 20–500 (default 100, or `Settings.DefaultPageSize`), remembered per tab.
- **Table tabs**:
  - Keyset if there is a single sortable PK: `SELECT … FROM t WHERE pk > $1 ORDER BY pk LIMIT $2`; backwards
    `WHERE pk < $1 ORDER BY pk DESC LIMIT $2` and reverse the rows in memory.
  - Otherwise, `ORDER BY <pk or nothing> LIMIT n OFFSET k`.
  - Fetch `n+1` rows to know whether there is a next page without `COUNT`.
- **Editor results**: only for read `SELECT`/`WITH`, wrap:
  `SELECT * FROM (<query>) AS q LIMIT n OFFSET k`. Strip the trailing `;` and do not wrap if the query already
  has a top-level `LIMIT` that would break it (in that case, read and cut on the client). Other statements
  run as is.
- `TableSource.Filter` (`[]Cond`, column = value) adds a `WHERE` to every page and to `Count`: it is how a
  table opened by following a foreign key shows only the referenced rows (SPEC §6.5). The filter takes the
  first placeholders; keyset conditions go after it.
- Loading a new page discards the grid's pending changes (`grid-editing` skill): ask first with
  `leaveTable`.
- `COUNT(*)` only on demand (button), with its own cancellable context.
- Identifiers always with `QuoteIdent`, values always with `Placeholder`.

## Export (SPEC §9, internal/export)

- CSV with `encoding/csv` (RFC 4180, UTF-8, headers); JSON as an array of objects written with
  `json.Encoder` token by token (`[`, objects separated by `,`, `]`) — **do not** build the whole slice.
- "Full result" scope: run the query again without the pagination wrapper and write row by row from
  `rows.Next()`; cancellable.
- NULL → empty cell in CSV / `null` in JSON. BLOB → base64. Dates → ISO 8601.
- Destination chosen with `w.NativeSaveDialog`; write to a temporary file in the same directory and
  `os.Rename` at the end so no half-written files are left behind.

## Tests

- In-memory SQLite for the full flow (split, warnings, last result, error in statement N).
- Table tests for the pagination wrapper and for detecting DML without a transaction.
- Export: compare against golden files with NULL, quotes, line breaks and BLOBs.

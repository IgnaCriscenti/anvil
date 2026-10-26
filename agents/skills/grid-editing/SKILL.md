---
name: grid-editing
description: Data editing in anvil's Data tab (internal/editing, ui/editing.go, ui/selection.go, ui/dataGrid.go, ui/dateEditor.go) — pending changes (update/insert/delete), multi-row selection and the bottom edit bar, cell editors (input, calendar for dates), SQL preview and Save in a single transaction, row identification with or without a PK, RowsAffected == 1 guarantee, NULL vs empty string, parameterized DML, leave/close guards. Use when changing editing in table tabs.
---

# Grid editing (SPEC §7, §6.4)

## Where things live

| Piece | File | What |
|-------|------|------|
| Pending changes | `internal/editing/pending.go` | `Pending`, `Change`, `Value` (NULL ≠ `''`) |
| DML | `internal/editing/dml.go` | `Target`, `Build`, `Statement.Preview`, `Comparable`, `Target.arg` |
| Validation, dates | `internal/editing/validate.go` | `Validate`, `TemporalKind`, `ParseTemporal`, `FormatTemporal`, `HasTimeZone` |
| Save | `internal/editing/apply.go` | `Apply`: one transaction, `RowsAffected == 1` |
| Cell editing, save, guards | `ui/editing.go` | `startEdit`/`commitEdit`/`setCell`, Set NULL/Now, `reviewSave`, `leaveTable`, `confirmClose` |
| Selection | `ui/selection.go` | `selection`, `selectRow`, `clickCell`, `targets`, `deleteSelected` |
| Grid | `ui/dataGrid.go` | `#` column, styles of pending changes, inline input (`editCell`) |
| Date editor | `ui/dateEditor.go` | Dialog with `gui.DatePicker` + `HH:MM:SS` field |
| Bottom bar | `ui/dataTab.go` (`editBar`) | Save, Cancel \| Add, Delete \| Set NULL, Set Now |

`internal/editing` does not know about the UI; the UI keeps the state in `tableView.Data` (`dataState`:
`Pending`, `Edit`, `Sel`, `Date`, `NoPKOK`, `Saving`, `SaveErr`).

## When editing is allowed (`tableView.editable`)

Table (not view), connection not `ReadOnly`, columns loaded. Per cell (`cellEditable`): not a BLOB column or
value, row not deleted. Tables opened by following a FK are editable too. SQL editor results **never**.
Without a PK, `withPKWarning` asks once per view before the first change to an existing row.

## Pending changes model

```go
type Value struct { Null bool; Text string }
type Change struct {
    Kind   Kind             // Update | Insert | Delete
    Row    int              // page row index (Update/Delete) or new row ID (Insert)
    Values map[string]Value // modified columns (Update) or the ones set by the user (Insert)
}
```

- Changes keep the order they were made in; that is the execution order.
- Pending changes only refer to the **current page**: `loadData` discards them (and the selection) when a
  page arrives. The leave guard asks first.
- Editing a new row changes its `Insert`. Deleting a new row removes the `Insert`.
- Deleting a row with a pending `Update` replaces it with a `Delete`; restoring does **not** bring the edit
  back.
- Setting a cell back to its original value removes the column from the `Update` (and the `Update` if empty).
- `Save (N)`: N = `Pending.Len()`. **Cancel** = `discardChanges`.

## UI behavior

- **Selection** (`dataState.Sel`): rows (`rowKey{Row, New}`), Shift anchor and active column. Click on a cell
  = only that row + its editor; Ctrl/Cmd+click toggles (moves the anchor); Shift+click selects the range from
  the anchor (the anchor stays); click on the `#` number selects without editing. Grid order: new rows on top
  (`+`), then page rows numbered from 1. Read modifiers from `ctx.Event.Modifiers`.
- **Bar below the grid** (`editBar`): Delete (N) / Restore on the selected rows (Restore when all are deleted;
  new rows are dropped); Set NULL / Set Now on the active column of each selected row (`tv.targets()`), or on
  the cell being edited. Buttons that act while a cell is being edited are `FocusDisabled`.
- **Inline input** (text, numbers): Enter or blur keeps the value, Esc discards it, Ctrl+0 sets NULL. The edit
  has a generation (`cellEdit.Gen`): a blur commit that arrives after Esc or after moving to another cell is
  ignored; `startEdit` commits the previous cell first.
- **Dates and times never take typed text**: `openDateEditor` shows a calendar (dates) and a masked
  `HH:MM:SS` input (times) with Now, Set NULL, Cancel, OK. Call `w.DatePickerReset(calendarID)` before showing
  it, or the calendar keeps the last month shown. The value is written with `FormatTemporal` (offset only
  for `with time zone` types; the fraction of a second survives only if the time was not changed).
- **Styles**: modified cells with `BgColor` (warning subtle), new rows in the success color with `DEFAULT`
  for unset columns, deleted rows struck through (also their number), selected rows with an accent number and
  accent-subtle cells, active cell underlined. FK links are hidden on modified cells.
- **Guards**: `leaveTable` (Save… / Discard / Cancel) on page change, refresh, page size, back, opening
  another table from the sidebar (checks the whole FK chain: `pendingView`), disconnect and delete
  connection. Window close: `WindowCfg.OnCloseRequest = confirmClose` (Keep editing / Discard and close);
  the hook must call `w.Close()` to proceed. Use in-window `w.Dialog`, not `NativeSaveDiscardDialog`.

## DML generation (SPEC §7.3–7.4)

- `QuoteIdent` for the table and columns, `Placeholder(n)` for **every** value. Never concatenate values.
- `UPDATE t SET c1 = $1 … WHERE <ident>`; `DELETE FROM t WHERE <ident>`; `INSERT INTO t (cols) VALUES (…)`
  with only the columns the user set, omitting empty autoincrement ones; no columns → `DEFAULT VALUES`
  (MySQL: `() VALUES ()`).
- `<ident>`: with a PK, `pk1 = $k [AND pk2 = …]` with the **original** values; without one, every
  `Comparable` column (no binary, JSON, XML, geometric or floating point), `col IS NULL` for NULL.
- Values go as **text** parameters (`nil` for NULL): pgx, MySQL and SQLite convert them to the column type.
  The `WHERE` uses the scanned `query.Value.Raw`.
- MySQL rejects the ` +00:00` the grid shows in dates: `Target.arg` sends its dates in UTC without offset.
  MySQL `BOOL` is `TINYINT` (the user types 0/1).
- `Statement.Preview` inlines the args **only to show them**; `Label` ("UPDATE of row 3") uses the `#` number.

## Applying (Save)

- `reviewSave` builds the statements, shows the preview and runs `editing.Apply` off the UI thread.
- `Apply`: `BeginTx`, run in order, `RowsAffected == 1` for UPDATE/DELETE (0 → "found no row … reload",
  >1 → "would affect N identical rows"), `Commit`. Any failure → rollback, error **inline in the preview**,
  `Pending` intact. MySQL needs `ClientFoundRows = true` (set in `mysqlConfig`) so an UPDATE with the same
  value counts 1.
- After commit: clear `Pending` and the selection, reload the page (or run the guard's `then`).

## Tests

- `internal/editing`: pending rules, DML per engine (`$n` / `?`, odd identifiers), preview, validation,
  `FormatTemporal`, Apply with rollback (failing statement, rows without a PK with duplicates, changed row).
  `server_test.go` runs against PG/MySQL with `ANVIL_TEST_PG` / `ANVIL_TEST_MYSQL`.
- `ui/ui_test.go`: `TestEditRows`, `TestMultiSelect`, `TestDateEditor`, `TestCloseWithUnsavedChanges`.
  `TableCellCfg.ID` is not applied (go-gui v0.92), so tests call `startEdit`/`selectRow` and click the
  bar and dialog buttons with `TestClick`; inputs use `TestType` / `TestKey` on `editID`.

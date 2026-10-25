---
name: grid-editing
description: Data editing in anvil's grid (internal/editing) — pending changes (update/insert/delete), preview and Save in a single transaction, row identification with or without a PK, RowsAffected == 1 guarantee, NULL vs empty string, parameterized DML generation. Use when implementing or changing editing in table tabs.
---

# Grid editing (SPEC §7, §6.4)

## When editing is allowed

Only in **table** tabs (not views), on a connection that is **not** `ReadOnly`, on non-BLOB columns. SQL
editor results are **never** editable. Without a PK: show a warning before enabling editing.

## Pending changes model

Lives in `internal/editing`, independent of the UI (the UI only reads it to paint and sends it events):

```go
type Value struct { Null bool; Text string } // NULL ≠ ""

type RowKey struct { Original map[string]Value } // original values of the row

type Change struct {
    Kind     ChangeKind            // Update | Insert | Delete
    Key      RowKey                // empty for Insert
    Values   map[string]Value      // modified columns (Update) or all of them (Insert)
}

type Pending struct { /* stable insertion order; editing the same cell twice overwrites the value */ }
```

- Editing a cell of a new row modifies the `Insert`, it does not add an `Update`.
- Deleting a new row removes the `Insert`. Deleting a row with a pending `Update` replaces it with a `Delete`.
- Setting a cell back to its original value removes that column from the `Update` (and the `Update` if it ends up empty).
- `Save (N)`: N = number of `Change`s. `Discard`: clears everything.
- Changing page, closing the tab or disconnecting with pending changes ⇒ `w.NativeSaveDiscardDialog`.
- Dates: validate the ISO 8601 format when editing, before accepting the value into `Pending`.
- NULL: `Ctrl+0` or context menu, only if the column is nullable.

## DML generation (SPEC §7.3–7.4)

- `QuoteIdent` for the table and columns, `Placeholder(n)` for **every** value. Never concatenate values.
- `UPDATE t SET c1 = $1, c2 = $2 WHERE <ident>`; `DELETE FROM t WHERE <ident>`;
  `INSERT INTO t (cols) VALUES (…)` omitting empty autoincrement columns.
- `<ident>`:
  - With a PK: `pk1 = $k [AND pk2 = $k+1]` using the **original** PK values.
  - Without a PK: every comparable column with its original value; `col IS NULL` for NULL; exclude BLOBs and
    non-comparable types (json in PG, etc.).
- One `Statement { SQL string; Args []any; Change *Change }` per change; the preview shows the SQL with the
  formatted args (display only; it always runs parameterized).
- Execution order: the changes in the order they were made.

## Applying (Save)

```go
tx, err := db.BeginTx(ctx, nil)
for _, st := range stmts {
    res, err := tx.ExecContext(ctx, st.SQL, st.Args...)
    // error → tx.Rollback(), report which change failed
    // UPDATE/DELETE: n, _ := res.RowsAffected(); n != 1 → Rollback and report
    //   ("would affect 3 identical rows" / "row no longer exists")
}
tx.Commit()
```

- All or nothing: on any failure, full rollback and `Pending` left intact so the user can fix it.
- After commit: clear `Pending` and reload the current page.
- MySQL: `RowsAffected` counts *changed* rows; an UPDATE that sets the same value returns 0. Open the
  connection with `ClientFoundRows: true` in `mysql.Config` so it counts *found* rows.
- Errors to the UI as `appErrors`, with the engine message.

## Required tests

- Table of cases for the generated DML per engine (PG `$n`, MySQL/SQLite `?`, quoting with odd characters).
- Row without a PK with duplicates ⇒ rollback due to `RowsAffected != 1`.
- NULL vs `''` in `SET` and in `WHERE`.
- Insert + edit + delete of the same new row ⇒ no statement.

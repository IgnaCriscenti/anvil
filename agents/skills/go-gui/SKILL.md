---
name: go-gui
description: How to write anvil's UI with go-gui v0.92 (immediate mode, Cfg structs, State[T], IDs, focus, events, theme, datagrid, dialogs, tabs, splitter, SVG, Stream/QueueCommand). Use before touching any file in ui/ or ui/components/, or when choosing a go-gui widget.
---

# go-gui in anvil

Pinned version: `github.com/go-gui-org/go-gui v0.92.0`. The source code is at
`~/go/pkg/mod/github.com/go-gui-org/go-gui@v0.92.0/` — **when in doubt, read the
source or an example in `examples/` before guessing the API**.

Local references (copied from the go-gui repo, MIT license in `references/GO-GUI-LICENSE`):
- `references/llms.txt` — rules that still compile when you break them but break the behavior.
- `references/dx-cheat-sheet.md` — where the obvious reading of the API is wrong (focus, IDs, `Opt`, events, layers, virtual lists, settings, secrets, streaming).
- `references/style-guide.md` and `references/theme-tokens.md` — which theme role to use for text, color and spacing.

## Mental model

- **Immediate mode**: the view function runs every frame and returns a tree of `gui.View`. There is no virtual DOM or diffing. Do not keep `View`s between frames.
- **State**: one typed slot per window, `gui.State[App](w)`. It **panics** if `App` is not the type passed to the window. All of anvil's state (open connections, tabs, pending changes, query result) hangs off that struct; no globals or stateful closures.
- **UI thread**: never block the view or a callback with I/O (DB, keyring, files). Run the work in a goroutine and come back to the UI thread with `w.QueueCommand(func(w *gui.Window){...})`, or with `gui.Stream(w, ch, fn)` for a stream of values (rows, progress). A channel the view drains on its own **does not repaint** until the next input event.

## Mandatory rules

- Every widget takes an `XxxCfg` with usable zero values. Callbacks: `func(ctx gui.EventCtx)` (or with extra args depending on the widget).
- **Nothing consumes events for you**: a callback that acts on an event calls `ctx.Consume()`.
- **Focus** = `Focusable` **and** a non-empty `ID`. Inputs are focusable by default; to exclude them use `FocusDisabled: true`, never `Focusable: false`.
- **IDs**: empty ID → auto ID (`~input3`). Set an explicit `ID` when code names the widget (`SetFocus`, `FindByID`, tests). Never write a `~` by hand. `SetFocus`/`FindByID` take the **effective** ID: compose it with `gui.ScopeID` / `gui.ScopeIDN`, read it with `w.ResolveID`. In lists (tabs, rows, connections) use `gui.ScopeIDN(owner, part, n)` with `n` = the entity's stable ID (e.g. `DBConn.ID`), not the on-screen index.
- **Colors, padding, sizing, spacing, radius, border**: use constructors/roles (`gui.RGB`, `gui.Hex`, `gui.PadAll`, `gui.FillFill`, `gui.FillFit`, `gui.SpacingMedium`, `gui.RadiusMedium`, `gui.BorderThin`, `gui.NoPadding`, `gui.NoBorder`), never struct literals. `Color{}` means "not set".
- `Opt[T]` (`gui.Some`, `gui.SomeF`) only where the zero value is a real option.
- **Theme**: read styles and colors from the roles (`w.Theme()` outside generation, `gui.CurrentTheme()`); do not hardcode font sizes or dimming alphas. Identify the theme by id, not by `Name`. A structural wrapper takes `SizeBorder: gui.NoBorder`.
- **A11y**: `A11YLabel` / `A11YDescription` through the embedded `A11YCfg`. Icon-only buttons (Add, Delete, Stop) **need** an `A11YLabel`.
- `AmendLayout` runs with the window lock held: do not call `SetFocus`/`SetView` there; use `ctx.Window.QueueCommand`.
- In `OnClick`/`OnHover`, `e.MouseX/Y` are local to the shape.
- Window root: `Sizing: gui.FillFill` so it fills the whole window.
- No debug border (`ColorBorder: gui.Red`) in committed code; to debug layout run with `GOGUI_DEBUG=1`.

## Widgets that map to the spec

| Spec need (SPEC.md)                     | go-gui                                                    | See                                  |
|-----------------------------------------|-----------------------------------------------------------|--------------------------------------|
| Sidebar + central area layout (§4)      | `gui.Splitter` (`view_splitter.go`), `Row`/`Column`       | `examples/dock_layout`, `explorer`   |
| Table / SQL tabs (§4)                   | `gui.TabControl` (`view_tab_control.go`)                  | `examples/showcase`                  |
| Table list with filter (§4)             | `gui.ListBox` or a virtual list                           | `examples/listbox`, `virtual_list`   |
| Data grid (§6, §7)                      | `gui.Table` with per-cell `TextStyle`/`Content`/`OnClick` (`gridTable`, `dataGrid`); **not** `datagrid` (see below) | `ui/dataGrid.go` |
| Connection modal / confirmations        | `w.Dialog(gui.DialogCfg{...})` (`view_dialog.go`), `DialogConfirm` or `DialogCustom`. Anvil uses in-window dialogs (`leaveTable`, `confirmClose`), not `NativeSaveDiscardDialog` (needs a native platform) | `examples/dialogs` |
| Window close with unsaved changes (§7.2) | `WindowCfg.OnCloseRequest`; the hook calls `w.Close()` to proceed | `ui/start.go` |
| Date / time editing (§6.4)              | `gui.DatePicker` (days only) + `gui.Input` with `Mask: "99:99:99"` | `ui/dateEditor.go`, `examples/date_picker_options` |
| SQLite file picker (§3.3)               | `w.NativeOpenDialog(gui.NativeOpenDialogCfg{...})`        | `gui/native_dialog.go`               |
| Export to file (§9)                     | `w.NativeSaveDialog(...)`                                 | `gui/native_dialog.go`               |
| Set NULL (§6.4)                         | Bottom edit bar + `Ctrl+0` in the input (`ui/dataTab.go`); `gui.ContextMenu` exists if a menu is ever needed | `examples/context_menu` |
| Non-blocking notices                    | `w.Toast(gui.ToastCfg{...})` (`view_toast.go`)            | cheat sheet "Toast position"         |
| SQL highlighting (§5.1)                 | `gui/highlight` package (chroma, language `"sql"`); the editor can be `go-edit` (sibling module) or a multiline input | `examples/multiline_input` |
| Local settings (§6.3)                   | Anvil uses GORM (`Settings`), **not** `gui.SaveSettings`  | —                                    |
| Keyring (§3.2)                          | Anvil uses `zalando/go-keyring`; `gui.SaveSecret` exists and returns `gui.ErrSecretsUnsupported` if there is no store | `examples/secrets` |

### Datagrid: why anvil does not use it

Anvil's grids are `gui.Table` (see "Lessons learned"). Notes kept for reference:

- `datagrid.DataGridCfg` requires an `ID`. For anvil use **controlled mode**: pass `Rows []datagrid.GridRow` and `Columns []datagrid.GridColumnCfg`, handle `OnCellEdit`, `OnPageChange`, `OnQueryChange` and keep the changes in the app state (pending changes belong to anvil, see the `grid-editing` skill).
- `DataGridDataSource` has unexported fields in request/result/capabilities: implementing it outside the package is not viable in v0.92. Paginate yourself (`query-execution` skill) and pass the page as `Rows`.
- `GridRow.Cells` is a `map[string]string`: **it does not distinguish NULL from `''`**. Keep a parallel set of NULL cells per row and render them with `CellFormat` (dimmed `NULL` text). Also use `CellFormat` to highlight modified cells, new `+` rows and struck-through deleted rows.
- `Loading: true` while the query runs; `LoadError` for errors; `Editable` per column (false for BLOBs, views, read-only connections, SQL editor results).

## Lessons learned in anvil

- **`gui.Table` borders**: `SizeBorder` defaults to 0 and is applied as is, so `BorderStyle` alone draws
  nothing. There is no header color: anvil paints row 0 as "selected" (`Selected{0: true}` +
  `Colors.Selected`). Use `gridStyle` in `ui/resultGrid.go` for every data table.
- **Clipped horizontal scroll**: a `Scrollable` container caches `contentW` before a Fit `gui.Table` takes its
  final width, and `layoutAdjustScrollOffsets` clips the offset to that stale value (the last columns become
  unreachable). Fix: measure the natural width (`naturalWidth`) in `AmendLayout` and pass it as the table's
  `MinWidth`. In tests, column widths are only measured after `soft.RenderToImage`.
- **Do not nest `gui.Splitter`**: a vertical one inside the sidebar's horizontal one doubles the row height of
  `gui.Table` (v0.92). Split the space with measured heights.
- **Grids**: use `gui.Table` (per-cell style via `TableCellCfg.TextStyle`), not `datagrid` (its
  `GridCellFormat` cannot be built from outside). Give it a bounded height (measured `Height`) so it
  virtualizes; truncate long values because columns are sized from the text.
- **Widgets in `gui.Table` cells**: `TableCellCfg.Content` replaces the text, but column widths are still
  measured from `Value`; pad `Value` to make room for the widget (see `linkCell` in `ui/resultGrid.go`).
- **`TableCellCfg.ID` is not applied** to the cell (v0.92): a test cannot click a cell by ID. Put IDs on
  the widgets inside the cell, or call the handler directly.
- **Cell styles**: `TextStyle.BgColor` highlights the text (modified cells) and `Strikethrough` crosses it
  (deleted rows). `gui.Theme` has no `ColorSuccess`/`ColorError`: use `TextStyleSuccess.Color` /
  `TextStyleError.Color`.
- **Inline editors**: a `FocusDisabled` button keeps the focus in the input being edited (Set NULL). Focus
  the input with `w.QueueCommand(func(w){ w.SetFocus(id) })` right after enabling it; `OnTextCommit` fires
  on Enter and on blur (the blur one runs after the frame pass).
- **Dates**: `gui.DatePicker` is a calendar for days only (no time); its visible month lives in window state
  keyed by ID, so reset it with `w.DatePickerReset(id)`. Day cells are `ScopeIDN(id, "day", d)`. A time field
  is an `Input` with `Mask: "99:99:99"`.
- **Fixed sizes**: `measuredBox` (`ui/resultGrid.go`) measures a panel in `AmendLayout` and re-lays out if it
  changed; the go-edit editor (needs `Width`/`Height`) and the grid use it.
- **go-edit**: custom actions with `EditorCfg.Actions` + `Keymaps`; `ActionFunc` uses an unexported type, so it
  is declared generic (`func runAction[S any](cfg, st *S, buf, w)`) and `Cursors` are read through reflection.
  `TriggerAction` runs on the next layout pass and does **not** run without a text backend (headless tests).
- **Headless tests**: `w.SetSvgParser(svg.New())` avoids the "no SVG parser" log; `soft.RenderToImage(w, 1)`
  installs the text measurer (without it `gui.Table` uses default widths); `w.FrameFn()` drains the
  `QueueCommand` queue (`TestRender` does not).

## Icons

- Own SVGs in `assets/icons/*.svg`, embedded; `assets.GetIconData(name)` reads `icons/<name>.svg`.
- The SVGs use `stroke="currentColor"`. go-gui leaves `currentColor` as an almost transparent sentinel color if `SvgCfg.Color` is not set → **always pass `Color`** with the active theme's text color, and regenerate when the theme changes.
- Reuse `ui/components.Icon` / `IconButton`; do not duplicate SVG loading.

## Verification

- `go build ./... && go vet ./...`.
- UI tests: `gui.NewTestWindow`, `w.TestFindings(mask)` to detect duplicate IDs, focusables without an ID and handlers that do not consume.
- Run the app with `GOGUI_DEBUG=1 go run .` and check the console.

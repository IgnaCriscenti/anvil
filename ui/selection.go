package ui

import (
	"slices"

	"github.com/go-gui-org/go-gui/gui"
)

// rowKey is a grid row: a page row index, or a new row's ID when New is set.
type rowKey struct {
	Row int
	New bool
}

func (r cellRef) key() rowKey { return rowKey{Row: r.Row, New: r.New} }

// selection is the set of selected rows of the Data tab. Column is the
// column of the active cell ("" when the rows were picked from the #
// column): Set NULL and Set Now apply to it on every selected row.
type selection struct {
	Rows   map[rowKey]bool
	Anchor rowKey // where a Shift+click range starts
	Column string
}

func (s *selection) clear() { *s = selection{} }

func (s *selection) has(k rowKey) bool { return s.Rows[k] }

func (s *selection) empty() bool { return len(s.Rows) == 0 }

// keys returns the selected rows in grid order.
func (tv *tableView) selectedRows() []rowKey {
	var out []rowKey
	for _, k := range tv.gridOrder() {
		if tv.Data.Sel.has(k) {
			out = append(out, k)
		}
	}
	return out
}

// gridOrder is the order of the rows in the grid: new rows on top, then the
// page rows.
func (tv *tableView) gridOrder() []rowKey {
	d := &tv.Data
	ids := d.Pending.NewRows()
	out := make([]rowKey, 0, len(ids)+len(d.Page.Rows))
	for _, id := range ids {
		out = append(out, rowKey{Row: id, New: true})
	}
	for r := range d.Page.Rows {
		out = append(out, rowKey{Row: r})
	}
	return out
}

// selectRow changes the selection after a click on row k: alone it selects
// only k, with Ctrl (Cmd on macOS) it toggles k and with Shift it selects
// the range from the anchor to k.
func selectRow(tv *tableView, k rowKey, column string, mods gui.Modifier) {
	s := &tv.Data.Sel
	switch {
	case mods.Has(gui.ModShift) && !s.empty():
		order := tv.gridOrder()
		from, to := slices.Index(order, s.Anchor), slices.Index(order, k)
		if from < 0 {
			from = to
		}
		if from > to {
			from, to = to, from
		}
		s.Rows = map[rowKey]bool{}
		for _, r := range order[from : to+1] {
			s.Rows[r] = true
		}
	case mods.Has(gui.ModCtrl) || mods.Has(gui.ModSuper):
		if s.Rows == nil {
			s.Rows = map[rowKey]bool{}
		}
		if s.Rows[k] {
			delete(s.Rows, k)
		} else {
			s.Rows[k] = true
		}
		s.Anchor = k
	default:
		s.Rows = map[rowKey]bool{k: true}
		s.Anchor = k
	}
	s.Column = column
}

// multiSelect reports whether a click adds to the selection instead of
// editing the cell.
func multiSelect(ctx gui.EventCtx) gui.Modifier {
	if ctx.Event == nil {
		return 0
	}
	m := ctx.Event.Modifiers
	if m.Has(gui.ModShift) || m.Has(gui.ModCtrl) || m.Has(gui.ModSuper) {
		return m
	}
	return 0
}

// clickCell selects a cell's row; without modifiers it also opens the
// cell's editor.
func clickCell(tv *tableView, ref cellRef) func(gui.EventCtx) {
	return func(ctx gui.EventCtx) {
		ctx.Consume()
		if mods := multiSelect(ctx); mods != 0 {
			if commitEdit(ctx.Window, tv) {
				selectRow(tv, ref.key(), ref.Column, mods)
			}
			return
		}
		startEdit(ctx.Window, tv, ref)
	}
}

// clickRowNumber selects a row from the # column, without editing.
func clickRowNumber(tv *tableView, k rowKey) func(gui.EventCtx) {
	return func(ctx gui.EventCtx) {
		ctx.Consume()
		if commitEdit(ctx.Window, tv) {
			mods := gui.Modifier(0)
			if ctx.Event != nil {
				mods = ctx.Event.Modifiers
			}
			selectRow(tv, k, "", mods)
		}
	}
}

// targets are the cells Set NULL and Set Now apply to: the cell being
// edited, or the active column of every selected row.
func (tv *tableView) targets() []cellRef {
	if e := tv.Data.Edit; e.Active {
		return []cellRef{e.Cell}
	}
	s := tv.Data.Sel
	if s.Column == "" {
		return nil
	}
	var out []cellRef
	for _, k := range tv.selectedRows() {
		ref := cellRef{Row: k.Row, New: k.New, Column: s.Column}
		if tv.cellEditable(ref) {
			out = append(out, ref)
		}
	}
	return out
}

// allDeleted reports whether every selected row is an existing row marked
// as deleted: then Delete restores them.
func (tv *tableView) allDeleted() bool {
	rows := tv.selectedRows()
	for _, k := range rows {
		if k.New || !tv.Data.Pending.Deleted(k.Row) {
			return false
		}
	}
	return len(rows) > 0
}

// deleteSelected deletes the selected rows (new rows are dropped), or
// restores them if all of them were already deleted.
func deleteSelected(w *gui.Window, tv *tableView) {
	rows := tv.selectedRows()
	if len(rows) == 0 || !commitEdit(w, tv) {
		return
	}
	endEdit(tv)
	p := &tv.Data.Pending
	if tv.allDeleted() {
		for _, k := range rows {
			p.Undelete(k.Row)
		}
		return
	}
	existing := slices.ContainsFunc(rows, func(k rowKey) bool { return !k.New })
	withPKWarning(w, tv, existing, func(*gui.Window) {
		for _, k := range rows {
			if k.New {
				p.DeleteNew(k.Row)
				delete(tv.Data.Sel.Rows, k)
			} else {
				p.Delete(k.Row)
			}
		}
	})
}

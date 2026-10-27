package ui

import (
	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// cellStyles are the text styles of the grid cells.
type cellStyles struct {
	null, blob gui.TextStyle
}

func newCellStyles(t gui.Theme) cellStyles {
	null := t.TextStyleDisabled
	null.Typeface = t.TextStyleCaption.Typeface
	return cellStyles{null: null, blob: t.TextStyleSecondary}
}

// valueCell shows a read-only value. NULL is dimmed and distinct from ""
// (SPEC §6.4).
func (st *cellStyles) valueCell(v query.Value) gui.TableCellCfg {
	switch {
	case v.Null:
		return gui.TableCellCfg{Value: "NULL", TextStyle: &st.null}
	case v.Blob:
		return gui.TableCellCfg{Value: v.Text, TextStyle: &st.blob}
	}
	return gui.TableCellCfg{Value: components.CellText(v.Text)}
}

// headRow is the header row of a result.
func headRow(res query.Result) gui.TableRowCfg {
	head := make([]gui.TableCellCfg, len(res.Columns))
	for i, c := range res.Columns {
		head[i] = gui.TableCellCfg{Value: c.Name, HeadCell: true}
	}
	return gui.TableRowCfg{Cells: head}
}

// resultGrid shows a read-only result (SQL editor).
func resultGrid(id string, res query.Result, size *components.BoxSize) gui.View {
	t := gui.CurrentTheme()
	if len(res.Columns) == 0 {
		return gui.Text(gui.TextCfg{Text: "No columns.", TextStyle: t.TextStyleSecondary})
	}
	st := newCellStyles(t)
	rows := make([]gui.TableRowCfg, 0, len(res.Rows)+1)
	rows = append(rows, headRow(res))
	for _, r := range res.Rows {
		cells := make([]gui.TableCellCfg, len(r))
		for i, v := range r {
			cells[i] = st.valueCell(v)
		}
		rows = append(rows, gui.TableRowCfg{Cells: cells})
	}
	return components.GridTable(id, rows, size)
}

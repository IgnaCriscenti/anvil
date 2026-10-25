package ui

import (
	"strings"

	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/go-gui-org/go-gui/gui"
)

// maxCellChars truncates long values in the grid: columns are sized by
// measuring the text, and a long value would widen its column so much that
// the others would end up out of view.
const maxCellChars = 30

// cellText keeps the first line of the value and cuts it at maxCellChars,
// ending in "…" when something was trimmed.
func cellText(s string) string {
	cut := false
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s, cut = s[:i], true
	}
	if r := []rune(s); len(r) > maxCellChars {
		s, cut = string(r[:maxCellChars-1]), true
	}
	if cut {
		s = strings.TrimRight(s, " \t") + "…"
	}
	return s
}

// resultGrid shows a read-only result. NULL is dimmed and distinct
// from "" (SPEC §6.4).
func resultGrid(id string, res query.Result, size *boxSize) gui.View {
	t := gui.CurrentTheme()
	if len(res.Columns) == 0 {
		return gui.Text(gui.TextCfg{Text: "No columns.", TextStyle: t.TextStyleSecondary})
	}

	nullStyle := t.TextStyleDisabled
	nullStyle.Typeface = t.TextStyleCaption.Typeface
	blobStyle := t.TextStyleSecondary

	head := make([]gui.TableCellCfg, len(res.Columns))
	for i, c := range res.Columns {
		head[i] = gui.TableCellCfg{Value: c.Name, HeadCell: true}
	}
	rows := make([]gui.TableRowCfg, 0, len(res.Rows)+1)
	rows = append(rows, gui.TableRowCfg{Cells: head})
	for _, r := range res.Rows {
		cells := make([]gui.TableCellCfg, len(r))
		for i, v := range r {
			switch {
			case v.Null:
				cells[i] = gui.TableCellCfg{Value: "NULL", TextStyle: &nullStyle}
			case v.Blob:
				cells[i] = gui.TableCellCfg{Value: v.Text, TextStyle: &blobStyle}
			default:
				cells[i] = gui.TableCellCfg{Value: cellText(v.Text)}
			}
		}
		rows = append(rows, gui.TableRowCfg{Cells: cells})
	}

	// The panel height (measured in the previous frame) bounds the table:
	// that way it virtualizes the rows and scrolls inside the panel. MinWidth is
	// the measured natural width (see measuredBox).
	return measuredBox(id, size, false, gui.Table(gridStyle(gui.TableCfg{
		ID:           gui.ScopeID(id, "table"),
		Data:         rows,
		FreezeHeader: true,
		Sizing:       gui.FitFixed,
		Height:       size.H,
		MinWidth:     size.ContentW,
	})))
}

// naturalWidth is the content width of an unstretched table: that of its
// widest row (a row is a node whose children are all fixed-width cells)
// plus the padding and borders of the containers that wrap it.
func naturalWidth(l *gui.Layout) float32 {
	if isTableRow(l) {
		return l.Shape.Width
	}
	var w float32
	for i := range l.Children {
		w = max(w, naturalWidth(&l.Children[i]))
	}
	return w + l.Shape.Padding.Width() + 2*l.Shape.SizeBorder
}

func isTableRow(l *gui.Layout) bool {
	if len(l.Children) == 0 {
		return false
	}
	for i := range l.Children {
		if l.Children[i].Shape.Sizing != gui.FixedFill {
			return false
		}
	}
	return true
}

// gridStyle draws every table border and gives the header row (row 0) its
// own background. gui.Table has no header color: it is painted as the
// selected row, so these tables cannot use Selected/OnSelect for anything
// else.
func gridStyle(cfg gui.TableCfg) gui.TableCfg {
	t := gui.CurrentTheme()
	cfg.BorderStyle = gui.TableBorderAll
	cfg.SizeBorder = t.SizeBorder
	cfg.Colors.Border = t.ColorBorder
	cfg.Colors.Selected = t.ColorActive
	cfg.Selected = map[int]bool{0: true}
	return cfg
}

// boxSize is the size of a panel measured in the previous frame. ContentW is
// the natural width of the table it contains, if any.
type boxSize struct{ W, H, ContentW float32 }

// measuredBox takes the available space and records its inner size in
// size; if it changed, it requests another layout so the content uses it. It
// is for widgets that need fixed sizes (the go-edit editor, the table).
//
// Without a border, the panel scrolls horizontally. go-gui (v0.92) computes
// the content width before the table takes its final width and clips the
// scroll to that value, so the last columns were unreachable: that is why
// the table's natural width is recorded in size.ContentW and the table uses
// it as MinWidth, which is respected.
func measuredBox(id string, size *boxSize, border bool, content gui.View) gui.View {
	t := gui.CurrentTheme()
	cfg := gui.ContainerCfg{
		ID:         id,
		Sizing:     gui.FillFill,
		Padding:    gui.NoPadding,
		SizeBorder: gui.NoBorder,
		Clip:       true,
		Content:    []gui.View{content},
		AmendLayout: func(ctx gui.EventCtx) {
			b := float32(0)
			if border {
				b = t.SizeBorder
			}
			w := max(ctx.Layout.Shape.Width-2*b, 0)
			h := max(ctx.Layout.Shape.Height-2*b, 0)
			cw := size.ContentW
			if kids := ctx.Layout.Children; !border && len(kids) > 0 && kids[0].Shape.ID == gui.ScopeID(id, "table") {
				cw = naturalWidth(&kids[0])
			}
			if w != size.W || h != size.H || cw != size.ContentW {
				size.W, size.H, size.ContentW = w, h, cw
				ctx.Window.QueueCommand(func(w *gui.Window) { w.InvalidateLayout() })
			}
		},
	}
	if border {
		cfg.SizeBorder, cfg.ColorBorder, cfg.Radius = gui.BorderThin, t.ColorBorder, gui.RadiusSmall
	} else {
		cfg.Scrollable, cfg.ScrollMode = true, gui.ScrollHorizontalOnly
	}
	return gui.Column(cfg)
}

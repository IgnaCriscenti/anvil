package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// Tabs of an open table.
const (
	tabStructure = "structure"
	tabData      = "data"
)

// tableMeta is what a table view loads before its data.
type tableMeta struct {
	cols []drivers.Column
	fks  []drivers.ForeignKey
}

// showTable makes view the current view of tab t and loads its columns
// and foreign keys.
func showTable(w *gui.Window, t *tab, view *tableView) {
	a := appOf(w)
	s, ok := a.Sessions.Get(view.Key.ConnID)
	if !ok {
		return
	}
	a.Selected = view.Key.ConnID
	view.Loading = true
	view.Data = dataState{Size: a.Prefs.DefaultPageSize}
	t.Table = view

	key, tb := view.Key, view.Table
	async(w, func(ctx context.Context) (tableMeta, error) {
		cols, err := s.Columns(ctx, key.Database, tb)
		if err != nil || tb.IsView {
			return tableMeta{cols: cols}, err
		}
		fks, err := s.ForeignKeys(ctx, key.Database, tb)
		return tableMeta{cols, fks}, err
	}, func(w *gui.Window, m tableMeta, err error) {
		view.Loading = false
		if m.cols == nil && err != nil {
			view.Err = errText(err)
			return
		}
		if err != nil {
			// Without foreign keys the table still works.
			showWarning(w, err)
		}
		view.Columns, view.FKs = m.cols, m.fks
		if t.Sub == tabData && t.Table == view {
			loadData(w, view, firstPage)
		}
	})
}

// followFK opens the table that fk references, filtered by the values of
// row (a row of tv's current page).
func followFK(w *gui.Window, tv *tableView, fk drivers.ForeignKey, row []query.Value) {
	res := tv.Data.Page.Result
	filter := make([]query.Cond, len(fk.Columns))
	for i, name := range fk.Columns {
		col := slices.IndexFunc(res.Columns, func(c query.Column) bool { return c.Name == name })
		if col < 0 || row[col].Null {
			return
		}
		filter[i] = query.Cond{Column: fk.RefColumns[i], Value: row[col].Raw}
	}
	t := appOf(w).Active
	if t == nil || t.Table != tv {
		return
	}
	// Following a key pins the tab: its chain is worth keeping.
	t.Sub, t.Preview = tabData, false
	showTable(w, t, &tableView{Key: tv.Key, Table: fk.RefTable, Filter: filter, Back: tv})
}

// goBack returns the active tab to the table its view was opened from.
func goBack(w *gui.Window) {
	t := appOf(w).Active
	if t == nil || t.Table == nil || t.Table.Back == nil {
		return
	}
	leaveTable(w, t.Table, false, func(w *gui.Window) {
		t.Table = t.Table.Back
		t.Sub = tabData
		if !t.Table.Data.loaded() {
			loadData(w, t.Table, query.Reload)
		}
	})
}

// fkOf returns the foreign key that contains column, if any.
func (tv *tableView) fkOf(column string) (drivers.ForeignKey, bool) {
	for _, fk := range tv.FKs {
		if slices.Contains(fk.Columns, column) {
			return fk, true
		}
	}
	return drivers.ForeignKey{}, false
}

// selectTab picks the Structure or Data view of the active table tab.
func selectTab(w *gui.Window, sub string) {
	a := appOf(w)
	t := a.Active
	if t == nil || t.Table == nil {
		return
	}
	a.TabSel, t.Sub = sub, sub
	tv := t.Table
	if sub == tabData && !tv.Loading && tv.Err == "" && !tv.Data.loaded() {
		loadData(w, tv, firstPage)
	}
}

func centralArea(w *gui.Window, a *App) gui.View {
	t := gui.CurrentTheme()
	if a.Active == nil {
		return gui.Column(gui.ContainerCfg{
			Sizing:     gui.FillFill,
			HAlign:     gui.HAlignCenter,
			VAlign:     gui.VAlignMiddle,
			Spacing:    gui.SpacingMedium,
			SizeBorder: gui.NoBorder,
			Content: []gui.View{
				components.IconColor("database", 48, t.TextStyleSecondary.Color),
				gui.Text(gui.TextCfg{Text: "Connect to a database and pick a table", TextStyle: t.TextStyleSecondary}),
			},
		})
	}

	var body gui.View
	if a.Active.SQL != nil {
		body = gui.Column(gui.ContainerCfg{
			ID:         "sql-area",
			Sizing:     gui.FillFill,
			Padding:    gui.PaddingSmall,
			SizeBorder: gui.NoBorder,
			Content:    []gui.View{sqlTab(w, a, a.Active.SQL)},
		})
	} else {
		body = tableArea(w, a, a.Active)
	}
	return gui.Column(gui.ContainerCfg{
		ID:         "central",
		Sizing:     gui.FillFill,
		Padding:    gui.NoPadding,
		Spacing:    gui.NoSpacing,
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{tabBar(a), body},
	})
}

// tableArea is a table tab: its header and the Structure / Data views.
func tableArea(w *gui.Window, a *App, tb *tab) gui.View {
	t := gui.CurrentTheme()
	tv := tb.Table
	title := tv.Table.Name
	if tv.Table.Schema != "" && tv.Table.Schema != tv.Key.Database {
		title = tv.Table.Schema + "." + title
	}
	if tv.Key.Database != "" {
		title = tv.Key.Database + " › " + title
	}
	var header []gui.View
	if tv.Back != nil {
		header = append(header, components.IconButton("chevronLeft", "", gui.ButtonCfg{
			ID:      "table-back",
			Variant: gui.ButtonGhost,
			A11YCfg: gui.A11YCfg{A11YLabel: "Back to " + tv.Back.Table.Name},
			OnClick: func(ctx gui.EventCtx) {
				goBack(ctx.Window)
				ctx.Consume()
			},
		}))
	}
	header = append(header,
		components.Icon(tableIcon(tv.Table), 18),
		gui.Text(gui.TextCfg{Text: title, TextStyle: t.TextStyleTitleSmall}),
	)
	if c, ok := a.conn(tv.Key.ConnID); ok && c.ReadOnly {
		header = append(header, gui.Text(gui.TextCfg{Text: "read-only", TextStyle: t.TextStyleCaption}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         "table-area",
		Sizing:     gui.FillFill,
		Padding:    gui.PaddingSmall,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content: []gui.View{
			gui.Row(gui.ContainerCfg{
				Padding:    gui.PadVH(2, 4),
				Spacing:    gui.SpacingSmall,
				SizeBorder: gui.NoBorder,
				VAlign:     gui.VAlignMiddle,
				Content:    header,
			}),
			gui.TabControl(gui.TabControlCfg{
				ID:       "table-tabs",
				Sizing:   gui.FillFill,
				Selected: tb.Sub,
				OnSelect: func(id string, ctx gui.EventCtx) {
					selectTab(ctx.Window, id)
				},
				Items: []gui.TabItemCfg{
					gui.NewTabItem(tabStructure, "Structure", []gui.View{structureTab(tv)}),
					gui.NewTabItem(tabData, "Data", []gui.View{dataTab(w, a, tv)}),
				},
			}),
		},
	})
}

func tableIcon(tb drivers.Table) string {
	if tb.IsView {
		return "eye"
	}
	return "table"
}

func structureTab(tv *tableView) gui.View {
	t := gui.CurrentTheme()
	var body gui.View
	switch {
	case tv.Loading:
		body = gui.Text(gui.TextCfg{Text: "Loading columns…", TextStyle: t.TextStyleSecondary})
	case tv.Err != "":
		body = components.ErrorText(tv.Err)
	default:
		body = columnsTable(tv)
	}
	return gui.Column(gui.ContainerCfg{
		ID:         "structure",
		Sizing:     gui.FillFill,
		Scrollable: true,
		Padding:    gui.PaddingMedium,
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{body},
	})
}

// columnsTable lists the table's structure.
func columnsTable(tv *tableView) gui.View {
	cols := tv.Columns
	t := gui.CurrentTheme()
	rows := []gui.TableRowCfg{{Cells: []gui.TableCellCfg{
		{Value: "#", HeadCell: true}, {Value: "Column", HeadCell: true}, {Value: "Type", HeadCell: true},
		{Value: "Nullable", HeadCell: true}, {Value: "Key", HeadCell: true},
	}}}
	for i, c := range cols {
		null, key := "", ""
		if c.Nullable {
			null = "yes"
		}
		if c.PK {
			key = "PK"
			if c.AutoInc {
				key += " · auto"
			}
		}
		if fk, ok := tv.fkOf(c.Name); ok {
			ref := fk.RefColumns[slices.Index(fk.Columns, c.Name)]
			key = strings.TrimPrefix(key+" · FK → "+fk.RefTable.Name+"."+ref, " · ")
		}
		rows = append(rows, gui.TableRowCfg{Cells: []gui.TableCellCfg{
			{Value: itoa(i + 1), TextStyle: &t.TextStyleSecondary},
			{Value: c.Name}, {Value: c.DBType}, {Value: null}, {Value: key},
		}})
	}
	return gui.Table(components.GridStyle(gui.TableCfg{
		ID:   "structure-table",
		Data: rows,
	}))
}

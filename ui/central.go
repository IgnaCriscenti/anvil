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
	tabSQL       = "sql"
)

// openTable shows the table in the central area and loads its columns. The
// views of the current table are left, so their pending changes are saved
// or discarded first.
func openTable(w *gui.Window, key dbKey, tb drivers.Table) {
	leaveTable(w, appOf(w).Table, true, func(w *gui.Window) {
		showTable(w, &tableView{Key: key, Table: tb})
	})
}

// tableMeta is what a table view loads before its data.
type tableMeta struct {
	cols []drivers.Column
	fks  []drivers.ForeignKey
}

// showTable makes view the current table and loads its columns and
// foreign keys.
func showTable(w *gui.Window, view *tableView) {
	a := appOf(w)
	s, ok := a.Sessions.Get(view.Key.ConnID)
	if !ok {
		return
	}
	a.Selected = view.Key.ConnID
	view.Loading = true
	view.Data = dataState{Size: a.Prefs.DefaultPageSize}
	a.Table = view

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
		if appOf(w).TabSel == tabData {
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
	a := appOf(w)
	a.TabSel = tabData
	showTable(w, &tableView{Key: tv.Key, Table: fk.RefTable, Filter: filter, Back: tv})
}

// goBack returns to the table the current view was opened from.
func goBack(w *gui.Window) {
	a := appOf(w)
	if a.Table == nil || a.Table.Back == nil {
		return
	}
	leaveTable(w, a.Table, false, func(w *gui.Window) {
		a.Table = a.Table.Back
		a.TabSel = tabData
		if !a.Table.Data.loaded() {
			loadData(w, a.Table, query.Reload)
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

func selectTab(w *gui.Window, tab string) {
	a := appOf(w)
	a.TabSel = tab
	tv := a.Table
	if tab == tabData && tv != nil && !tv.Loading && tv.Err == "" && !tv.Data.loaded() {
		loadData(w, tv, firstPage)
	}
}

func centralArea(w *gui.Window, a *App) gui.View {
	t := gui.CurrentTheme()
	if a.Table == nil {
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

	tv := a.Table
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
		ID:         "central",
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
				Selected: a.TabSel,
				OnSelect: func(id string, ctx gui.EventCtx) {
					selectTab(ctx.Window, id)
				},
				Items: []gui.TabItemCfg{
					gui.NewTabItem(tabStructure, "Structure", []gui.View{structureTab(tv)}),
					gui.NewTabItem(tabData, "Data", []gui.View{dataTab(w, a, tv)}),
					gui.NewTabItem(tabSQL, "SQL", []gui.View{sqlTab(w, a, tv.Key)}),
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
		body = gui.Text(gui.TextCfg{Text: tv.Err, Mode: gui.TextModeWrap, TextStyle: t.TextStyleError})
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
	return gui.Table(gridStyle(gui.TableCfg{
		ID:   "structure-table",
		Data: rows,
	}))
}

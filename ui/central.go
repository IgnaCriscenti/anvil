package ui

import (
	"context"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// Tabs of an open table.
const (
	tabStructure = "structure"
	tabData      = "data"
	tabSQL       = "sql"
)

// openTable shows the table in the central area and loads its columns.
func openTable(w *gui.Window, key dbKey, tb drivers.Table) {
	a := appOf(w)
	s, ok := a.Sessions.Get(key.ConnID)
	if !ok {
		return
	}
	a.Selected = key.ConnID
	view := &tableView{Key: key, Table: tb, Loading: true, Data: dataState{Size: a.Prefs.DefaultPageSize}}
	a.Table = view

	async(w, func(ctx context.Context) ([]drivers.Column, error) {
		return s.Columns(ctx, key.Database, tb)
	}, func(w *gui.Window, cols []drivers.Column, err error) {
		view.Loading = false
		if err != nil {
			view.Err = errText(err)
			return
		}
		view.Columns = cols
		if appOf(w).TabSel == tabData {
			loadData(w, view, firstPage)
		}
	})
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
	header := []gui.View{
		components.Icon(tableIcon(tv.Table), 18),
		gui.Text(gui.TextCfg{Text: title, TextStyle: t.TextStyleTitleSmall}),
	}
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
		body = columnsTable(tv.Columns)
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
func columnsTable(cols []drivers.Column) gui.View {
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

package ui

import (
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

func engineLabel(engine string) string {
	switch engine {
	case models.EngineSQLite:
		return "SQLite"
	case models.EnginePostgres:
		return "PostgreSQL"
	case models.EngineMySQL:
		return "MySQL"
	}
	return engine
}

// Indentation of each level of the sidebar tree.
const indent = 16

func sidebar(w *gui.Window, a *App) gui.View {
	t := gui.CurrentTheme()

	content := make([]gui.View, 0, len(a.Connections))
	for _, c := range a.Connections {
		content = append(content, connectionItem(a, c))
	}
	if len(a.Connections) == 0 {
		content = append(content, gui.Text(gui.TextCfg{
			Text:      "No connections yet. Use Add to create one.",
			Mode:      gui.TextModeWrap,
			TextStyle: t.TextStyleSecondary,
		}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         "sidebar",
		Sizing:     gui.FillFill,
		Scrollable: true,
		Padding:    gui.PaddingSmall,
		Spacing:    gui.SpacingTight,
		SizeBorder: gui.NoBorder,
		Color:      t.ColorPanel,
		Content:    content,
	})
}

func chevronIcon(expanded bool) gui.View {
	if expanded {
		return components.Icon("chevronDown", 12)
	}
	return components.Icon("chevronRight", 12)
}

// treeRow is a clickable sidebar row.
func treeRow(id string, level int, selected bool, onClick func(gui.EventCtx), content ...gui.View) gui.View {
	t := gui.CurrentTheme()
	color := gui.Color{}
	if selected {
		color = t.ColorSelect
	}
	return gui.Row(gui.ContainerCfg{
		ID:         id,
		Sizing:     gui.FillFit,
		Padding:    gui.NewPadding(3, 4, 3, 4+float32(level*indent)),
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Radius:     gui.RadiusSmall,
		Color:      color,
		VAlign:     gui.VAlignMiddle,
		Hover:      gui.HoverStyle{Color: t.ColorHover, Cursor: gui.CursorPointingHand},
		OnClick:    onClick,
		Content:    content,
	})
}

func treeColumn(items []gui.View) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingTight,
		SizeBorder: gui.NoBorder,
		Content:    items,
	})
}

func connectionItem(a *App, c models.DBConn) gui.View {
	t := gui.CurrentTheme()
	id := c.ID
	s, open := a.Sessions.Get(id)
	expanded := open && a.Expanded[id]
	scope := gui.ScopeIDN("conn", "", int(id))

	nameStyle := t.TextStyleDef
	if !open {
		nameStyle = t.TextStyleSecondary
	}

	header := treeRow(gui.ScopeID(scope, "row"), 0, a.Selected == id,
		func(ctx gui.EventCtx) {
			a := appOf(ctx.Window)
			if a.Selected == id {
				if _, open := a.Sessions.Get(id); open {
					a.Expanded[id] = !a.Expanded[id]
				} else {
					connect(ctx.Window, id)
				}
			}
			a.Selected = id
			ctx.Consume()
		},
		chevronIcon(expanded),
		components.ColorDot(c.Color, 10),
		gui.Text(gui.TextCfg{Text: c.Name, TextStyle: nameStyle}),
		gui.Text(gui.TextCfg{Text: engineLabel(c.Engine), TextStyle: t.TextStyleCaption}),
	)

	if !expanded {
		return header
	}

	items := []gui.View{header}
	if !s.AllDatabases() {
		items = append(items, tableList(a, c, dbKey{ConnID: id}, scope, 1)...)
		return treeColumn(items)
	}

	dbs := a.Databases[id]
	if len(dbs) == 0 {
		items = append(items, treeHint("No databases.", 1))
	}
	for n, name := range dbs {
		items = append(items, databaseItem(a, c, name, gui.ScopeIDN(scope, "db", n))...)
	}
	return treeColumn(items)
}

// databaseItem is a server database and, if expanded, its tables.
func databaseItem(a *App, c models.DBConn, name, scope string) []gui.View {
	t := gui.CurrentTheme()
	key := dbKey{ConnID: c.ID, Database: name}
	expanded := a.DBExpanded[key]

	items := []gui.View{treeRow(gui.ScopeID(scope, "row"), 1, false,
		func(ctx gui.EventCtx) {
			appOf(ctx.Window).Selected = key.ConnID
			toggleDatabase(ctx.Window, key)
			ctx.Consume()
		},
		chevronIcon(expanded),
		components.Icon("database", 12),
		gui.Text(gui.TextCfg{Text: name, TextStyle: t.TextStyleBodySmall}),
	)}
	if !expanded {
		return items
	}
	if a.Loading[key] {
		return append(items, treeHint("Loading tables…", 2))
	}
	return append(items, tableList(a, c, key, scope, 2)...)
}

// tableList is the filter and flat table list of a database.
func tableList(a *App, c models.DBConn, key dbKey, scope string, level int) []gui.View {
	items := []gui.View{
		gui.Row(gui.ContainerCfg{
			Sizing:     gui.FillFit,
			Padding:    gui.NewPadding(0, 0, 0, float32(level*indent)),
			SizeBorder: gui.NoBorder,
			Content: []gui.View{gui.Input(gui.InputCfg{
				ID:          gui.ScopeID(scope, "filter"),
				Sizing:      gui.FillFit,
				Text:        a.Filter[key],
				Placeholder: "Filter tables…",
				OnTextChanged: func(text string, ctx gui.EventCtx) {
					appOf(ctx.Window).Filter[key] = text
				},
			})},
		}),
	}

	filter := strings.ToLower(a.Filter[key])
	shown := 0
	for _, tb := range a.Tables[key] {
		if filter != "" && !strings.Contains(strings.ToLower(tb.Name), filter) {
			continue
		}
		items = append(items, tableItem(a, c, key, tb, gui.ScopeIDN(scope, "table", shown), level))
		shown++
	}
	if shown == 0 {
		msg := "No tables."
		if filter != "" {
			msg = "No tables match the filter."
		}
		items = append(items, treeHint(msg, level))
	}
	return items
}

func tableItem(a *App, c models.DBConn, key dbKey, tb drivers.Table, id string, level int) gui.View {
	t := gui.CurrentTheme()
	label := tb.Name
	// In MySQL the schema is the database, so it is only shown in PG outside public.
	if tb.Schema != "" && tb.Schema != "public" && tb.Schema != key.Database && tb.Schema != c.DB {
		label = tb.Schema + "." + tb.Name
	}
	selected := a.Table != nil && a.Table.Key == key && a.Table.Table == tb

	return treeRow(id, level, selected,
		func(ctx gui.EventCtx) {
			openTable(ctx.Window, key, tb)
			ctx.Consume()
		},
		components.Icon(tableIcon(tb), 12),
		gui.Text(gui.TextCfg{Text: label, TextStyle: t.TextStyleBodySmall}),
	)
}

func treeHint(msg string, level int) gui.View {
	t := gui.CurrentTheme()
	return gui.Row(gui.ContainerCfg{
		Padding:    gui.NewPadding(2, 4, 2, 4+float32(level*indent)),
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{gui.Text(gui.TextCfg{Text: msg, TextStyle: t.TextStyleCaption})},
	})
}

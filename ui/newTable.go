package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/schema"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// tableForm is the state of the New table dialog.
type tableForm struct {
	Key     dbKey
	Name    string
	Columns []formColumn
	nextID  int
	Busy    bool
	Error   string
}

// formColumn is a column of the dialog; ID keeps its widget IDs stable when
// columns are removed.
type formColumn struct {
	ID int
	schema.Column
}

func (f *tableForm) addColumn(c schema.Column) {
	f.nextID++
	f.Columns = append(f.Columns, formColumn{ID: f.nextID, Column: c})
}

func (f *tableForm) table() schema.Table {
	t := schema.Table{Name: f.Name, Columns: make([]schema.Column, len(f.Columns))}
	for i, c := range f.Columns {
		t.Columns[i] = c.Column
	}
	return t
}

// column returns the column with that ID (nil if it was removed).
func (f *tableForm) column(id int) *formColumn {
	i := slices.IndexFunc(f.Columns, func(c formColumn) bool { return c.ID == id })
	if i < 0 {
		return nil
	}
	return &f.Columns[i]
}

// openNewTable opens the New table dialog for a database.
func openNewTable(w *gui.Window, key dbKey) {
	a := appOf(w)
	a.NewTable = tableForm{Key: key}
	a.NewTable.addColumn(schema.Column{Name: "id", Type: "integer", PK: true, AutoInc: true})
	a.NewTable.addColumn(schema.Column{Type: "text", Nullable: true})

	w.Dialog(gui.DialogCfg{
		ID:         "new-table",
		DialogType: gui.DialogCustom,
		Title:      "New table",
		Width:      820,
		FocusID:    "nt-name",
		CustomView: newTableView,
	})
}

func newTableView(w *gui.Window) gui.View {
	a := appOf(w)
	f := &a.NewTable
	t := gui.CurrentTheme()

	content := []gui.View{
		components.FormField("Table name", gui.Input(gui.InputCfg{
			ID:          "nt-name",
			Sizing:      gui.FillFit,
			Text:        f.Name,
			Placeholder: "my_table",
			OnTextChanged: func(s string, ctx gui.EventCtx) {
				appOf(ctx.Window).NewTable.Name = s
			},
		})),
		gui.Text(gui.TextCfg{Text: "Columns", TextStyle: t.TextStyleLabel}),
	}
	rows := make([]gui.View, len(f.Columns))
	for i, c := range f.Columns {
		rows[i] = columnRow(c, len(f.Columns) > 1)
	}
	// Many columns scroll instead of growing the dialog past the window.
	content = append(content, gui.Column(gui.ContainerCfg{
		ID:         "nt-columns",
		Sizing:     gui.FillFit,
		MaxHeight:  260,
		Scrollable: true,
		ScrollMode: gui.ScrollVerticalOnly,
		Padding:    gui.NewPadding(0, 12, 0, 0), // room for the scrollbar
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content:    rows,
	}))
	content = append(content, gui.Row(gui.ContainerCfg{
		Padding:    gui.NoPadding,
		SizeBorder: gui.NoBorder,
		Content: []gui.View{components.IconButton("plus", "Add column", gui.ButtonCfg{
			ID:      "nt-add-column",
			Variant: gui.ButtonGhost,
			OnClick: func(ctx gui.EventCtx) {
				appOf(ctx.Window).NewTable.addColumn(schema.Column{Type: "text", Nullable: true})
				ctx.Consume()
			},
		})},
	}))

	var stmt string
	if s, ok := a.Sessions.Get(f.Key.ConnID); ok {
		var err error
		stmt, err = schema.CreateTable(s.Driver, f.table())
		if err != nil {
			stmt = ""
			content = append(content, gui.Text(gui.TextCfg{Text: errDescription(err), TextStyle: t.TextStyleSecondary, Mode: gui.TextModeWrap}))
		}
	}
	if stmt != "" {
		content = append(content, sqlPreview("nt-sql", stmt))
	}
	if f.Error != "" {
		content = append(content, components.ErrorText(f.Error))
	}
	content = append(content, components.DialogButtons(
		components.DismissButton("nt-cancel", "Cancel", gui.ButtonSecondary),
		gui.Button(gui.ButtonCfg{
			ID:       "nt-create",
			Label:    "Create",
			Variant:  gui.ButtonPrimary,
			Disabled: f.Busy || stmt == "",
			OnClick: func(ctx gui.EventCtx) {
				createTable(ctx.Window, stmt)
				ctx.Consume()
			},
		}),
	))
	return components.DialogBody(content...)
}

// columnRow edits one column: name, type, nullable, PK, auto-increment and
// default. removable is false for the last column left.
func columnRow(c formColumn, removable bool) gui.View {
	id := c.ID
	scope := gui.ScopeIDN("nt-col", "", id)
	edit := func(ctx gui.EventCtx, fn func(c *formColumn)) {
		if c := appOf(ctx.Window).NewTable.column(id); c != nil {
			fn(c)
		}
	}
	// Name and type have a fixed width so the rows line up; default takes
	// the rest (width 0). A long value scrolls inside its input.
	input := func(part, text, placeholder string, width float32, disabled bool, set func(c *formColumn, s string)) gui.View {
		sizing := gui.FixedFit
		if width == 0 {
			sizing = gui.FillFit
		}
		return gui.Input(gui.InputCfg{
			ID:              gui.ScopeID(scope, part),
			Width:           width,
			Sizing:          sizing,
			NoMinWidthFloor: true,
			Text:            text,
			Placeholder:     placeholder,
			Disabled:        disabled,
			OnTextChanged: func(s string, ctx gui.EventCtx) {
				edit(ctx, func(c *formColumn) { set(c, s) })
			},
		})
	}
	check := func(part, label string, selected, disabled bool, toggle func(c *formColumn)) gui.View {
		return gui.Checkbox(gui.ToggleCfg{
			ID:       gui.ScopeID(scope, part),
			Label:    label,
			Selected: selected,
			Disabled: disabled,
			OnClick: func(ctx gui.EventCtx) {
				edit(ctx, toggle)
				ctx.Consume()
			},
		})
	}

	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		VAlign:     gui.VAlignMiddle,
		Content: []gui.View{
			input("name", c.Name, "column name", 160, false, func(c *formColumn, s string) { c.Name = s }),
			input("type", c.Type, "type", 130, false, func(c *formColumn, s string) { c.Type = s }),
			check("null", "Nullable", c.Nullable && !c.PK, c.PK, func(c *formColumn) { c.Nullable = !c.Nullable }),
			check("pk", "PK", c.PK, false, func(c *formColumn) {
				c.PK = !c.PK
				if !c.PK {
					c.AutoInc = false
				}
			}),
			check("auto", "Auto inc.", c.AutoInc, !c.PK, func(c *formColumn) {
				c.AutoInc = !c.AutoInc
				if c.AutoInc {
					c.Default = ""
				}
			}),
			input("default", c.Default, "default", 0, c.AutoInc, func(c *formColumn, s string) { c.Default = s }),
			components.IconButton("x", "", gui.ButtonCfg{
				ID:       gui.ScopeID(scope, "remove"),
				Variant:  gui.ButtonGhost,
				Disabled: !removable,
				A11YCfg:  gui.A11YCfg{A11YLabel: "Remove column"},
				OnClick: func(ctx gui.EventCtx) {
					f := &appOf(ctx.Window).NewTable
					f.Columns = slices.DeleteFunc(f.Columns, func(c formColumn) bool { return c.ID == id })
					ctx.Consume()
				},
			}),
		},
	})
}

// createTable runs the CREATE TABLE, reloads the database's tables and
// opens the new one.
func createTable(w *gui.Window, stmt string) {
	a := appOf(w)
	f := &a.NewTable
	s, ok := a.Sessions.Get(f.Key.ConnID)
	if !ok || f.Busy {
		return
	}
	f.Busy, f.Error = true, ""
	key, name := f.Key, strings.TrimSpace(f.Name)

	async(w, func(ctx context.Context) (struct{}, error) {
		db, err := s.Pool(ctx, key.Database)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, schema.Exec(ctx, db, stmt, "table")
	}, func(w *gui.Window, _ struct{}, err error) {
		a := appOf(w)
		a.NewTable.Busy = false
		if err != nil {
			a.NewTable.Error = errText(err)
			return
		}
		w.DialogDismiss()
		a.Status = "Created table " + name
		if key.Database != "" {
			a.DBExpanded[key] = true
		}
		loadTables(w, key, func(w *gui.Window, tables []drivers.Table) {
			if tb, ok := createdTable(tables, name); ok {
				openTable(w, key, tb, true)
			}
		})
	})
}

// createdTable finds a new table by name. In PG it is created in the first
// schema of the search_path, usually public.
func createdTable(tables []drivers.Table, name string) (drivers.Table, bool) {
	var found []drivers.Table
	for _, tb := range tables {
		if tb.Name == name && !tb.IsView {
			found = append(found, tb)
		}
	}
	if len(found) == 0 {
		return drivers.Table{}, false
	}
	for _, tb := range found {
		if tb.Schema == "public" {
			return tb, true
		}
	}
	return found[0], true
}

// sqlPreview shows a statement in a scrollable code block.
func sqlPreview(id, stmt string) gui.View {
	t := gui.CurrentTheme()
	return gui.Column(gui.ContainerCfg{
		ID:          id,
		Sizing:      gui.FillFit,
		MaxHeight:   320,
		Scrollable:  true,
		Padding:     gui.PaddingSmall,
		Color:       t.ColorInterior,
		ColorBorder: t.ColorBorder,
		SizeBorder:  gui.BorderThin,
		Radius:      gui.RadiusSmall,
		Content: []gui.View{gui.Text(gui.TextCfg{
			Text:      stmt,
			TextStyle: t.TextStyleCodeSmall,
			Mode:      gui.TextModeWrap,
		})},
	})
}

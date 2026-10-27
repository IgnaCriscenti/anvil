package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/schema"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// databaseForm is the state of the New database dialog.
type databaseForm struct {
	ConnID uint
	Name   string
	Busy   bool
	Error  string
}

// openNewDatabase opens the New database dialog for a server connection.
func openNewDatabase(w *gui.Window, connID uint) {
	appOf(w).NewDB = databaseForm{ConnID: connID}
	w.Dialog(gui.DialogCfg{
		ID:         "new-database",
		DialogType: gui.DialogCustom,
		Title:      "New database",
		Width:      420,
		FocusID:    "nd-name",
		CustomView: newDatabaseView,
	})
}

func newDatabaseView(w *gui.Window) gui.View {
	a := appOf(w)
	f := a.NewDB

	var stmt string
	if s, ok := a.Sessions.Get(f.ConnID); ok && strings.TrimSpace(f.Name) != "" {
		stmt, _ = schema.CreateDatabase(s.Driver, f.Name)
	}
	submit := func(ctx gui.EventCtx) {
		if stmt != "" {
			createDatabase(ctx.Window, stmt)
		}
		ctx.Consume()
	}

	content := []gui.View{
		components.FormField("Database name", gui.Input(gui.InputCfg{
			ID:          "nd-name",
			Sizing:      gui.FillFit,
			Text:        f.Name,
			Placeholder: "my_database",
			OnTextChanged: func(s string, ctx gui.EventCtx) {
				appOf(ctx.Window).NewDB.Name = s
			},
			OnEnter: submit,
		})),
	}
	if stmt != "" {
		content = append(content, sqlPreview("nd-sql", stmt))
	}
	if f.Error != "" {
		content = append(content, components.ErrorText(f.Error))
	}
	content = append(content, components.DialogButtons(
		components.DismissButton("nd-cancel", "Cancel", gui.ButtonSecondary),
		gui.Button(gui.ButtonCfg{
			ID:       "nd-create",
			Label:    "Create",
			Variant:  gui.ButtonPrimary,
			Disabled: f.Busy || stmt == "",
			OnClick:  submit,
		}),
	))
	return components.DialogBody(content...)
}

// createDatabase runs the CREATE DATABASE on the main pool and reloads the
// server databases. A connection with a database configured does not list
// the others, so it only reports the result.
func createDatabase(w *gui.Window, stmt string) {
	a := appOf(w)
	f := &a.NewDB
	s, ok := a.Sessions.Get(f.ConnID)
	if !ok || f.Busy {
		return
	}
	f.Busy, f.Error = true, ""
	id, name := f.ConnID, strings.TrimSpace(f.Name)

	// created is true once the database exists, even if listing fails.
	type result struct {
		created bool
		dbs     []string
	}
	async(w, func(ctx context.Context) (result, error) {
		if err := schema.Exec(ctx, s.DB, stmt, "database"); err != nil {
			return result{}, err
		}
		if !s.AllDatabases() {
			return result{created: true}, nil
		}
		dbs, err := s.Databases(ctx)
		return result{true, dbs}, err
	}, func(w *gui.Window, res result, err error) {
		a := appOf(w)
		a.NewDB.Busy = false
		if !res.created {
			a.NewDB.Error = errText(err)
			return
		}
		w.DialogDismiss()
		a.Status = "Created database " + name
		if err != nil {
			showError(w, err)
			return
		}
		if !s.AllDatabases() {
			w.Toast(gui.ToastCfg{
				Title: "Database created",
				Body:  "This connection only shows " + s.Conn.DB + ". Leave the connection's database empty to browse every database.",
			})
			return
		}
		a.Databases[id] = res.dbs
		key := dbKey{ConnID: id, Database: name}
		if slices.Contains(res.dbs, name) && !a.DBExpanded[key] {
			toggleDatabase(w, key)
		}
	})
}

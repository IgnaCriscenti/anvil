package ui

import (
	"context"
	"fmt"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/go-gui-org/go-gui/gui"
)

// connect opens connection id. The password comes from the keyring or is
// asked from the user (SPEC §3.2).
func connect(w *gui.Window, id uint) {
	a := appOf(w)
	c, ok := a.conn(id)
	if !ok || a.Connecting[id] {
		return
	}
	if _, open := a.Sessions.Get(id); open {
		a.Expanded[id] = true
		return
	}

	if c.Engine == models.EngineSQLite {
		openSession(w, c, "", false)
		return
	}
	if !c.SavePassword {
		promptPassword(w, c, "")
		return
	}

	a.Connecting[id] = true
	async(w, func(ctx context.Context) (string, error) {
		return a.Conns.GetPassword(ctx, id)
	}, func(w *gui.Window, password string, err error) {
		appOf(w).Connecting[id] = false
		if err != nil {
			promptPassword(w, c, errText(err))
			return
		}
		openSession(w, c, password, false)
	})
}

// sessionInfo is what is loaded on connect: the server databases or the
// tables of the configured database.
type sessionInfo struct {
	all       bool
	databases []string
	tables    []drivers.Table
}

// openSession connects and lists the databases or the tables. fromPrompt
// means the password prompt is open: errors are shown there.
func openSession(w *gui.Window, c models.DBConn, password string, fromPrompt bool) {
	a := appOf(w)
	a.Connecting[c.ID] = true
	a.Status = "Connecting to " + c.Name + "…"

	async(w, func(ctx context.Context) (sessionInfo, error) {
		s, err := a.Sessions.Connect(ctx, c, password)
		if err != nil {
			return sessionInfo{}, err
		}
		if s.AllDatabases() {
			dbs, err := s.Databases(ctx)
			return sessionInfo{all: true, databases: dbs}, err
		}
		tables, err := s.Tables(ctx, "")
		return sessionInfo{tables: tables}, err
	}, func(w *gui.Window, info sessionInfo, err error) {
		a := appOf(w)
		a.Connecting[c.ID] = false
		if err != nil {
			a.Status = ""
			if _, open := a.Sessions.Get(c.ID); open {
				// Connected but could not list the databases or tables.
				a.Expanded[c.ID] = true
			}
			if fromPrompt {
				a.Form.Error = errText(err)
				a.Form.Busy = false
				return
			}
			showError(w, err)
			return
		}
		if fromPrompt {
			w.DialogDismiss()
		}
		a.Expanded[c.ID] = true
		a.Selected = c.ID
		if info.all {
			a.Databases[c.ID] = info.databases
			a.Status = fmt.Sprintf("Connected to %s (%d databases)", c.Name, len(info.databases))
			return
		}
		a.Tables[dbKey{ConnID: c.ID}] = info.tables
		a.Status = fmt.Sprintf("Connected to %s (%d tables)", c.Name, len(info.tables))
	})
}

// toggleDatabase expands or collapses a database and loads its tables the first time.
func toggleDatabase(w *gui.Window, key dbKey) {
	a := appOf(w)
	a.DBExpanded[key] = !a.DBExpanded[key]
	if !a.DBExpanded[key] || a.Loading[key] {
		return
	}
	if _, loaded := a.Tables[key]; loaded {
		return
	}
	loadTables(w, key)
}

func loadTables(w *gui.Window, key dbKey) {
	a := appOf(w)
	s, ok := a.Sessions.Get(key.ConnID)
	if !ok {
		return
	}
	a.Loading[key] = true

	async(w, func(ctx context.Context) ([]drivers.Table, error) {
		return s.Tables(ctx, key.Database)
	}, func(w *gui.Window, tables []drivers.Table, err error) {
		a := appOf(w)
		delete(a.Loading, key)
		if err != nil {
			a.DBExpanded[key] = false
			showError(w, err)
			return
		}
		a.Tables[key] = tables
	})
}

// promptPassword asks for the password to connect. It reuses Form for the
// dialog state (password, error, busy).
func promptPassword(w *gui.Window, c models.DBConn, msg string) {
	a := appOf(w)
	a.Form = connForm{Conn: c, Error: msg}

	submit := func(ctx gui.EventCtx) {
		a := appOf(ctx.Window)
		if a.Form.Busy {
			return
		}
		a.Form.Busy, a.Form.Error = true, ""
		openSession(ctx.Window, a.Form.Conn, a.Form.Password, true)
		ctx.Consume()
	}

	w.Dialog(gui.DialogCfg{
		ID:         "password-prompt",
		DialogType: gui.DialogCustom,
		Title:      "Connect to " + c.Name,
		Width:      380,
		FocusID:    "pp-password",
		CustomView: func(w *gui.Window) gui.View {
			f := appOf(w).Form
			t := gui.CurrentTheme()
			content := []gui.View{
				gui.Text(gui.TextCfg{
					Text:      fmt.Sprintf("Password for %s@%s", f.Conn.User, f.Conn.Host),
					TextStyle: t.TextStyleSecondary,
				}),
				gui.Input(gui.InputCfg{
					ID:         "pp-password",
					Sizing:     gui.FillFit,
					IsPassword: true,
					Text:       f.Password,
					OnTextChanged: func(s string, ctx gui.EventCtx) {
						appOf(ctx.Window).Form.Password = s
					},
					OnEnter: submit,
				}),
			}
			if f.Error != "" {
				content = append(content, gui.Text(gui.TextCfg{Text: f.Error, Mode: gui.TextModeWrap, TextStyle: t.TextStyleError}))
			}
			content = append(content, gui.Row(gui.ContainerCfg{
				Sizing:     gui.FillFit,
				Padding:    gui.NoPadding,
				Spacing:    gui.SpacingSmall,
				SizeBorder: gui.NoBorder,
				HAlign:     gui.HAlignEnd,
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{
						ID:    "pp-cancel",
						Label: "Cancel",
						OnClick: func(ctx gui.EventCtx) {
							ctx.Window.DialogDismiss()
							ctx.Consume()
						},
					}),
					gui.Button(gui.ButtonCfg{
						ID:       "pp-connect",
						Label:    "Connect",
						Variant:  gui.ButtonPrimary,
						Disabled: f.Busy,
						OnClick:  submit,
					}),
				},
			}))
			return gui.Column(gui.ContainerCfg{
				Sizing:     gui.FillFit,
				Padding:    gui.NoPadding,
				Spacing:    gui.SpacingMedium,
				SizeBorder: gui.NoBorder,
				Content:    content,
			})
		},
	})
}

func disconnect(w *gui.Window, id uint) {
	a := appOf(w)
	c, _ := a.conn(id)
	a.forgetConn(id)

	async(w, func(context.Context) (struct{}, error) {
		return struct{}{}, a.Sessions.Disconnect(id)
	}, func(w *gui.Window, _ struct{}, err error) {
		if err != nil {
			showWarning(w, err)
		}
		appOf(w).Status = "Disconnected from " + c.Name
	})
}

func confirmDelete(w *gui.Window, id uint) {
	c, ok := appOf(w).conn(id)
	if !ok {
		return
	}
	w.Dialog(gui.DialogCfg{
		DialogType: gui.DialogConfirm,
		Title:      "Delete connection?",
		Body:       fmt.Sprintf("The connection %q and its saved password will be deleted. The database itself is not modified.", c.Name),
		OnOkYes: func(w *gui.Window) {
			disconnect(w, id)
			a := appOf(w)
			async(w, func(ctx context.Context) (struct{}, error) {
				return struct{}{}, a.Conns.Delete(ctx, id)
			}, func(w *gui.Window, _ struct{}, err error) {
				if err != nil {
					showError(w, err)
				}
				reloadConnections(w)
			})
		},
	})
}

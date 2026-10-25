package ui

import (
	"context"
	"errors"
	"strconv"

	"github.com/Watts-Argentina/anvil/internal/connections"
	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// connForm is the state of the add/edit modal (SPEC §3.3).
type connForm struct {
	Conn     models.DBConn
	Password string
	Port     string
	Busy     bool
	Error    string
	Info     string
}

// connColors are the colors offered for the indicator.
var connColors = []string{"#E5484D", "#F76B15", "#FFC53D", "#46A758", "#12A594", "#0090FF", "#8E4EC6", "#D6409F"}

var engines = []struct{ id, label string }{
	{models.EngineSQLite, "SQLite"},
	{models.EnginePostgres, "PostgreSQL"},
	{models.EngineMySQL, "MySQL / MariaDB"},
}

var sslModes = map[string][]string{
	models.EnginePostgres: {"prefer", "disable", "require", "verify-ca", "verify-full"},
	models.EngineMySQL:    {"preferred", "false", "true", "skip-verify", "custom"},
}

func openConnForm(w *gui.Window, c models.DBConn) {
	a := appOf(w)
	if c.ID == 0 {
		c = models.DBConn{
			Engine:       models.EnginePostgres,
			Host:         "localhost",
			Port:         models.DefaultPort(models.EnginePostgres),
			Color:        connColors[len(a.Connections)%len(connColors)],
			SavePassword: true,
		}
	}
	a.Form = connForm{Conn: c, Port: strconv.Itoa(c.Port)}

	title := "New connection"
	if c.ID != 0 {
		title = "Edit connection"
	}
	w.Dialog(gui.DialogCfg{
		ID:         "conn-form",
		DialogType: gui.DialogCustom,
		Title:      title,
		Width:      480,
		CustomView: connFormView,
	})
}

func formField(label string, field gui.View) gui.View {
	t := gui.CurrentTheme()
	return gui.Column(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingTight,
		SizeBorder: gui.NoBorder,
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: label, TextStyle: t.TextStyleLabel}),
			field,
		},
	})
}

func formInput(id, text, placeholder string, onChange func(*connForm, string)) gui.View {
	return gui.Input(gui.InputCfg{
		ID:          id,
		Sizing:      gui.FillFit,
		Text:        text,
		Placeholder: placeholder,
		OnTextChanged: func(s string, ctx gui.EventCtx) {
			onChange(&appOf(ctx.Window).Form, s)
		},
	})
}

func connFormView(w *gui.Window) gui.View {
	a := appOf(w)
	f := &a.Form
	c := f.Conn
	t := gui.CurrentTheme()

	engineLabels := make([]string, len(engines))
	selectedEngine := ""
	for i, e := range engines {
		engineLabels[i] = e.label
		if e.id == c.Engine {
			selectedEngine = e.label
		}
	}

	fields := []gui.View{
		formField("Name", formInput("cf-name", c.Name, "My database", func(f *connForm, s string) { f.Conn.Name = s })),
		formField("Color", colorPicker(c.Color)),
		formField("Engine", gui.Select(gui.SelectCfg{
			ID:       "cf-engine",
			Sizing:   gui.FillFit,
			Items:    engineLabels,
			Selected: []string{selectedEngine},
			OnSelect: func(sel []string, ctx gui.EventCtx) {
				f := &appOf(ctx.Window).Form
				for _, e := range engines {
					if len(sel) > 0 && e.label == sel[0] && e.id != f.Conn.Engine {
						f.Conn.Engine = e.id
						f.Conn.SSLMode = ""
						if p := models.DefaultPort(e.id); p != 0 {
							f.Conn.Port = p
							f.Port = strconv.Itoa(p)
						}
					}
				}
				ctx.Consume()
			},
		})),
	}

	if c.Engine == models.EngineSQLite {
		fields = append(fields, formField("Database file", gui.Row(gui.ContainerCfg{
			Sizing:     gui.FillFit,
			Padding:    gui.NoPadding,
			Spacing:    gui.SpacingSmall,
			SizeBorder: gui.NoBorder,
			Content: []gui.View{
				formInput("cf-file", c.DB, "/path/to/database.db", func(f *connForm, s string) { f.Conn.DB = s }),
				gui.Button(gui.ButtonCfg{
					ID:    "cf-browse",
					Label: "Browse…",
					OnClick: func(ctx gui.EventCtx) {
						browseSQLite(ctx.Window)
						ctx.Consume()
					},
				}),
			},
		})))
	} else {
		fields = append(fields,
			gui.Row(gui.ContainerCfg{
				Sizing:     gui.FillFit,
				Padding:    gui.NoPadding,
				Spacing:    gui.SpacingMedium,
				SizeBorder: gui.NoBorder,
				Content: []gui.View{
					formField("Host", formInput("cf-host", c.Host, "localhost", func(f *connForm, s string) { f.Conn.Host = s })),
					gui.Column(gui.ContainerCfg{
						Width: 110, Sizing: gui.FixedFit, Padding: gui.NoPadding, SizeBorder: gui.NoBorder,
						Content: []gui.View{formField("Port", gui.Input(gui.InputCfg{
							ID:              "cf-port",
							Sizing:          gui.FillFit,
							NoMinWidthFloor: true,
							Text:            f.Port,
							PreTextChange: func(_, proposed string) (string, bool) {
								for _, r := range proposed {
									if r < '0' || r > '9' {
										return "", false
									}
								}
								return proposed, len(proposed) <= 5
							},
							OnTextChanged: func(s string, ctx gui.EventCtx) {
								f := &appOf(ctx.Window).Form
								f.Port = s
								f.Conn.Port, _ = strconv.Atoi(s)
							},
						}))},
					}),
				},
			}),
			formField("User", formInput("cf-user", c.User, "postgres", func(f *connForm, s string) { f.Conn.User = s })),
			formField("Password", gui.Input(gui.InputCfg{
				ID:          "cf-password",
				Sizing:      gui.FillFit,
				Text:        f.Password,
				IsPassword:  true,
				Placeholder: passwordPlaceholder(c),
				OnTextChanged: func(s string, ctx gui.EventCtx) {
					appOf(ctx.Window).Form.Password = s
				},
			})),
			gui.Checkbox(gui.ToggleCfg{
				ID:       "cf-save-password",
				Label:    "Save password in the system keyring",
				Selected: c.SavePassword,
				OnClick: func(ctx gui.EventCtx) {
					f := &appOf(ctx.Window).Form
					f.Conn.SavePassword = !f.Conn.SavePassword
					ctx.Consume()
				},
			}),
			formField("Database", formInput("cf-db", c.DB, "All databases", func(f *connForm, s string) { f.Conn.DB = s })),
			formField("SSL mode", sslSelect(c)),
		)
		if needsSSLFiles(c) {
			fields = append(fields,
				formField("CA certificate", formInput("cf-ssl-ca", c.SSLCA, "/path/to/ca.pem", func(f *connForm, s string) { f.Conn.SSLCA = s })),
				formField("Client certificate", formInput("cf-ssl-cert", c.SSLCert, "Optional", func(f *connForm, s string) { f.Conn.SSLCert = s })),
				formField("Client key", formInput("cf-ssl-key", c.SSLKey, "Optional", func(f *connForm, s string) { f.Conn.SSLKey = s })),
			)
		}
	}

	fields = append(fields, gui.Checkbox(gui.ToggleCfg{
		ID:       "cf-readonly",
		Label:    "Read-only (block writes)",
		Selected: c.ReadOnly,
		OnClick: func(ctx gui.EventCtx) {
			f := &appOf(ctx.Window).Form
			f.Conn.ReadOnly = !f.Conn.ReadOnly
			ctx.Consume()
		},
	}))

	if f.Error != "" {
		fields = append(fields, gui.Text(gui.TextCfg{Text: f.Error, Mode: gui.TextModeWrap, TextStyle: t.TextStyleError}))
	}
	if f.Info != "" {
		fields = append(fields, gui.Text(gui.TextCfg{Text: f.Info, Mode: gui.TextModeWrap, TextStyle: t.TextStyleSuccess}))
	}

	fields = append(fields, gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.PadVH(6, 0),
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content: []gui.View{
			gui.Button(gui.ButtonCfg{
				ID:       "cf-test",
				Label:    "Test connection",
				Disabled: f.Busy,
				OnClick: func(ctx gui.EventCtx) {
					testConnection(ctx.Window)
					ctx.Consume()
				},
			}),
			gui.Row(gui.ContainerCfg{Sizing: gui.FillFit, Padding: gui.NoPadding, SizeBorder: gui.NoBorder}),
			gui.Button(gui.ButtonCfg{
				ID:    "cf-cancel",
				Label: "Cancel",
				OnClick: func(ctx gui.EventCtx) {
					ctx.Window.DialogDismiss()
					ctx.Consume()
				},
			}),
			gui.Button(gui.ButtonCfg{
				ID:       "cf-save",
				Label:    "Save",
				Variant:  gui.ButtonPrimary,
				Disabled: f.Busy,
				OnClick: func(ctx gui.EventCtx) {
					saveConnection(ctx.Window)
					ctx.Consume()
				},
			}),
		},
	}))

	return gui.Column(gui.ContainerCfg{
		ID:         "conn-form-body",
		Sizing:     gui.FillFit,
		MaxHeight:  620,
		Scrollable: true,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingMedium,
		SizeBorder: gui.NoBorder,
		Content:    fields,
	})
}

func passwordPlaceholder(c models.DBConn) string {
	if c.ID != 0 && c.SavePassword {
		return "Unchanged"
	}
	return ""
}

func needsSSLFiles(c models.DBConn) bool {
	switch c.SSLMode {
	case "require", "verify-ca", "verify-full", "custom":
		return true
	}
	return false
}

func sslSelect(c models.DBConn) gui.View {
	modes := sslModes[c.Engine]
	sel := c.SSLMode
	if sel == "" && len(modes) > 0 {
		sel = modes[0]
	}
	return gui.Select(gui.SelectCfg{
		ID:       "cf-ssl",
		Sizing:   gui.FillFit,
		Items:    modes,
		Selected: []string{sel},
		OnSelect: func(s []string, ctx gui.EventCtx) {
			if len(s) > 0 {
				appOf(ctx.Window).Form.Conn.SSLMode = s[0]
			}
			ctx.Consume()
		},
	})
}

func colorPicker(selected string) gui.View {
	t := gui.CurrentTheme()
	swatches := make([]gui.View, 0, len(connColors))
	for i, hex := range connColors {
		border := gui.Color{}
		if hex == selected {
			border = t.TextStyleDef.Color
		}
		swatches = append(swatches, gui.Row(gui.ContainerCfg{
			ID:          gui.ScopeIDN("cf-color", "", i),
			Padding:     gui.PadAll(2),
			SizeBorder:  gui.BorderThin,
			ColorBorder: border,
			Radius:      gui.RadiusSmall,
			Hover:       gui.HoverStyle{Cursor: gui.CursorPointingHand},
			A11YCfg:     gui.A11YCfg{A11YLabel: "Color " + hex},
			OnClick: func(ctx gui.EventCtx) {
				appOf(ctx.Window).Form.Conn.Color = hex
				ctx.Consume()
			},
			Content: []gui.View{components.ColorDot(hex, 16)},
		}))
	}
	return gui.Row(gui.ContainerCfg{
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content:    swatches,
	})
}

func browseSQLite(w *gui.Window) {
	w.NativeOpenDialog(gui.NativeOpenDialogCfg{
		Title: "Select SQLite database",
		Filters: []gui.NativeFileFilter{
			{Name: "SQLite databases", Extensions: []string{"db", "sqlite", "sqlite3", "db3"}},
			{Name: "All files", Extensions: []string{"*"}},
		},
		OnDone: func(r gui.NativeDialogResult, w *gui.Window) {
			if r.Status == gui.DialogOK {
				if paths := r.PathStrings(); len(paths) > 0 {
					appOf(w).Form.Conn.DB = paths[0]
				}
			}
		},
	})
}

// formPassword returns the password used to test the connection: the one
// typed in or, when editing, the one stored in the keyring.
func formPassword(ctx context.Context, a *App, f connForm) string {
	if f.Password != "" || f.Conn.ID == 0 || !f.Conn.SavePassword {
		return f.Password
	}
	p, _ := a.Conns.GetPassword(ctx, f.Conn.ID)
	return p
}

func testConnection(w *gui.Window) {
	a := appOf(w)
	f := a.Form
	if err := connections.Validate(f.Conn); err != nil {
		a.Form.Error, a.Form.Info = errText(err), ""
		return
	}
	a.Form.Busy, a.Form.Error, a.Form.Info = true, "", "Testing connection…"

	async(w, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, drivers.Test(ctx, f.Conn, formPassword(ctx, a, f))
	}, func(w *gui.Window, _ struct{}, err error) {
		f := &appOf(w).Form
		f.Busy = false
		if err != nil {
			f.Error, f.Info = errText(err), ""
			return
		}
		f.Info = "Connection successful."
	})
}

func saveConnection(w *gui.Window) {
	a := appOf(w)
	f := a.Form
	a.Form.Busy, a.Form.Error, a.Form.Info = true, "", ""

	async(w, func(ctx context.Context) (connections.SaveResult, error) {
		return a.Conns.Save(ctx, f.Conn, f.Password)
	}, func(w *gui.Window, res connections.SaveResult, err error) {
		a := appOf(w)
		a.Form.Busy = false
		if err != nil {
			a.Form.Error = errText(err)
			return
		}
		w.DialogDismiss()
		a.Selected = res.Conn.ID
		reloadConnections(w)

		if res.Warning != nil && !a.KeyringWarned {
			a.KeyringWarned = true
			showWarning(w, res.Warning)
		}
		if _, open := a.Sessions.Get(res.Conn.ID); open {
			w.Toast(gui.ToastCfg{Title: "Connection updated", Body: "Reconnect to apply the changes."})
		}
	})
}

// errText summarizes an error to show it inline.
func errText(err error) string {
	var ae *appErrors.AppError
	if errors.As(err, &ae) {
		return ae.Title + " " + ae.Description
	}
	return err.Error()
}

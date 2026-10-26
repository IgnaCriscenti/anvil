package ui

import (
	"context"
	"log"

	"github.com/Watts-Argentina/anvil/internal/connections"
	"github.com/Watts-Argentina/anvil/internal/history"
	"github.com/Watts-Argentina/anvil/internal/settings"
	"github.com/Watts-Argentina/anvil/internal/storage"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"
)

func Start() {
	path, err := storage.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	db, err := storage.Open(path)
	if err != nil {
		log.Fatal(err)
	}

	app := newApp(&connections.Service{DB: db}, &settings.Service{DB: db}, &history.Service{DB: db})
	if prefs, err := app.Settings.Load(context.Background()); err == nil {
		app.Prefs = prefs
	} else {
		log.Print(err)
	}
	applyTheme(nil, app.Prefs.Theme)

	w := gui.NewWindow(gui.WindowCfg{
		State:  app,
		Title:  "Anvil",
		Width:  1200,
		Height: 760,
		// Asks before losing unsaved grid changes (SPEC §7.2).
		OnCloseRequest: confirmClose,
		OnInit: func(w *gui.Window) {
			w.SetView(mainView)
			reloadConnections(w)
		},
	})

	backend.Run(w)
	app.Sessions.CloseAll()
}

// applyTheme activates the light or dark theme. With w == nil it applies it
// before the window is created.
func applyTheme(w *gui.Window, theme string) {
	t := gui.ThemeDark
	if theme == models.ThemeLight {
		t = gui.ThemeLight
	}
	if w == nil {
		gui.SetTheme(t)
		return
	}
	w.SetTheme(t)
}

func toggleTheme(w *gui.Window) {
	a := appOf(w)
	if a.Prefs.Theme == models.ThemeLight {
		a.Prefs.Theme = models.ThemeDark
	} else {
		a.Prefs.Theme = models.ThemeLight
	}
	applyTheme(w, a.Prefs.Theme)

	prefs := a.Prefs
	async(w, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.Settings.Save(ctx, prefs)
	}, func(w *gui.Window, _ struct{}, err error) {
		if err != nil {
			showWarning(w, err)
		}
	})
}

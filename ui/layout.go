package ui

import (
	"github.com/go-gui-org/go-gui/gui"
)

func mainView(w *gui.Window) gui.View {
	a := appOf(w)
	t := gui.CurrentTheme()

	return gui.Column(gui.ContainerCfg{
		Sizing:     gui.FillFill,
		Padding:    gui.NoPadding,
		Spacing:    gui.NoSpacing,
		SizeBorder: gui.NoBorder,
		Color:      t.ColorBackground,
		Content: []gui.View{
			topMenu(a),
			gui.Splitter(gui.SplitterCfg{
				ID:          "main-splitter",
				Sizing:      gui.FillFill,
				Orientation: gui.SplitterHorizontal,
				Ratio:       gui.SomeF(a.SidebarRatio),
				OnChange: func(ratio float32, _ gui.SplitterCollapsed, ctx gui.EventCtx) {
					appOf(ctx.Window).SidebarRatio = ratio
				},
				First: gui.SplitterPaneCfg{
					MinSize: 180,
					MaxSize: 480,
					Content: []gui.View{sidebar(w, a)},
				},
				Second: gui.SplitterPaneCfg{
					MinSize: 320,
					Content: []gui.View{centralArea(w, a)},
				},
			}),
			statusBar(a),
		},
	})
}

func statusBar(a *App) gui.View {
	t := gui.CurrentTheme()
	active := "No connection selected"
	if c, ok := a.conn(a.Selected); ok {
		state := "disconnected"
		if _, open := a.Sessions.Get(c.ID); open {
			state = "connected"
		}
		if a.Connecting[c.ID] {
			state = "connecting…"
		}
		active = c.Name + " · " + engineLabel(c.Engine) + " · " + state
		if c.ReadOnly {
			active += " · read-only"
		}
	}

	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.PadVH(4, 10),
		Spacing:    gui.SpacingLarge,
		SizeBorder: gui.NoBorder,
		Color:      t.ColorPanel,
		VAlign:     gui.VAlignMiddle,
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: active, TextStyle: t.TextStyleCaption}),
			gui.Row(gui.ContainerCfg{Sizing: gui.FillFit, Padding: gui.NoPadding, SizeBorder: gui.NoBorder}),
			gui.Text(gui.TextCfg{Text: a.Status, TextStyle: t.TextStyleCaption}),
		},
	})
}

package ui

import (
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

func topMenu(a *App) gui.View {
	t := gui.CurrentTheme()
	c, selected := a.conn(a.Selected)
	_, open := a.Sessions.Get(c.ID)

	connectBtn := components.IconButton("plug", "Connect", gui.ButtonCfg{
		ID:       "top-connect",
		Disabled: !selected || a.Connecting[c.ID],
		OnClick: func(ctx gui.EventCtx) {
			connect(ctx.Window, appOf(ctx.Window).Selected)
			ctx.Consume()
		},
	})
	if open {
		connectBtn = components.IconButton("unplug", "Disconnect", gui.ButtonCfg{
			ID: "top-connect",
			OnClick: func(ctx gui.EventCtx) {
				disconnect(ctx.Window, appOf(ctx.Window).Selected)
				ctx.Consume()
			},
		})
	}

	themeIcon, themeLabel := "moon", "Dark theme"
	if a.Prefs.Theme == models.ThemeDark {
		themeIcon, themeLabel = "sun", "Light theme"
	}

	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.PaddingSmall,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Color:      t.ColorPanel,
		VAlign:     gui.VAlignMiddle,
		Content: []gui.View{
			components.IconButton("plus", "Add", gui.ButtonCfg{
				ID: "top-add",
				OnClick: func(ctx gui.EventCtx) {
					openConnForm(ctx.Window, models.DBConn{})
					ctx.Consume()
				},
			}),
			components.IconButton("pencil", "Edit", gui.ButtonCfg{
				ID:       "top-edit",
				Disabled: !selected,
				OnClick: func(ctx gui.EventCtx) {
					if c, ok := appOf(ctx.Window).conn(appOf(ctx.Window).Selected); ok {
						openConnForm(ctx.Window, c)
					}
					ctx.Consume()
				},
			}),
			components.IconButton("trash", "Delete", gui.ButtonCfg{
				ID:       "top-delete",
				Disabled: !selected,
				OnClick: func(ctx gui.EventCtx) {
					confirmDelete(ctx.Window, appOf(ctx.Window).Selected)
					ctx.Consume()
				},
			}),
			connectBtn,
			components.Spacer(),
			components.IconButton(themeIcon, "", gui.ButtonCfg{
				ID:      "top-theme",
				Variant: gui.ButtonGhost,
				A11YCfg: gui.A11YCfg{A11YLabel: themeLabel},
				OnClick: func(ctx gui.EventCtx) {
					toggleTheme(ctx.Window)
					ctx.Consume()
				},
			}),
		},
	})
}

package components

import "github.com/go-gui-org/go-gui/gui"

// FormField is a labeled form field.
func FormField(label string, field gui.View) gui.View {
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

// ColorPicker is a row of "#RRGGBB" swatches; the selected one is outlined.
// Each swatch's ID is gui.ScopeIDN(id, "", index).
func ColorPicker(id string, colors []string, selected string, onSelect func(hex string, ctx gui.EventCtx)) gui.View {
	t := gui.CurrentTheme()
	swatches := make([]gui.View, 0, len(colors))
	for i, hex := range colors {
		border := gui.Color{}
		if hex == selected {
			border = t.TextStyleDef.Color
		}
		swatches = append(swatches, gui.Row(gui.ContainerCfg{
			ID:          gui.ScopeIDN(id, "", i),
			Padding:     gui.PadAll(2),
			SizeBorder:  gui.BorderThin,
			ColorBorder: border,
			Radius:      gui.RadiusSmall,
			Hover:       gui.HoverStyle{Cursor: gui.CursorPointingHand},
			A11YCfg:     gui.A11YCfg{A11YLabel: "Color " + hex},
			OnClick: func(ctx gui.EventCtx) {
				onSelect(hex, ctx)
				ctx.Consume()
			},
			Content: []gui.View{ColorDot(hex, 16)},
		}))
	}
	return gui.Row(gui.ContainerCfg{
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content:    swatches,
	})
}

package components

import "github.com/go-gui-org/go-gui/gui"

// DialogBody is the content column of a custom dialog.
func DialogBody(content ...gui.View) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingMedium,
		SizeBorder: gui.NoBorder,
		Content:    content,
	})
}

// DialogButtons is the button row of a custom dialog, aligned to the end.
func DialogButtons(buttons ...gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		HAlign:     gui.HAlignEnd,
		Content:    buttons,
	})
}

// DismissButton closes the open dialog.
func DismissButton(id, label string, variant gui.ButtonVariant) gui.View {
	return gui.Button(gui.ButtonCfg{
		ID:      id,
		Label:   label,
		Variant: variant,
		OnClick: func(ctx gui.EventCtx) {
			ctx.Window.DialogDismiss()
			ctx.Consume()
		},
	})
}

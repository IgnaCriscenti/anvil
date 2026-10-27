package components

import "github.com/go-gui-org/go-gui/gui"

// Spacer takes the free width of a row, pushing what follows to the end.
func Spacer() gui.View {
	return gui.Row(gui.ContainerCfg{Sizing: gui.FillFit, Padding: gui.NoPadding, SizeBorder: gui.NoBorder})
}

// Toolbar is a full-width row of buttons and captions, vertically centered.
func Toolbar(id string, content ...gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		ID:         id,
		Sizing:     gui.FillFit,
		Padding:    gui.PadVH(4, 4),
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		VAlign:     gui.VAlignMiddle,
		Content:    content,
	})
}

// Separator is a vertical line between groups of buttons.
func Separator() gui.View {
	return gui.Column(gui.ContainerCfg{
		Width:      1,
		Height:     20,
		Sizing:     gui.FixedFixed,
		Padding:    gui.NoPadding,
		SizeBorder: gui.NoBorder,
		Color:      gui.CurrentTheme().ColorBorder,
	})
}

// ErrorText is an error message, wrapped to the available width.
func ErrorText(msg string) gui.View {
	return gui.Text(gui.TextCfg{Text: msg, Mode: gui.TextModeWrap, TextStyle: gui.CurrentTheme().TextStyleError})
}

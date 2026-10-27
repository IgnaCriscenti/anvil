package components

import "github.com/go-gui-org/go-gui/gui"

// TreeIndent is the indentation of each level of a tree.
const TreeIndent = 16

// Chevron is the expand/collapse indicator of a tree node.
func Chevron(expanded bool) gui.View {
	if expanded {
		return Icon("chevronDown", 12)
	}
	return Icon("chevronRight", 12)
}

// TreeRow is a clickable tree row, indented by level.
func TreeRow(id string, level int, selected bool, onClick func(gui.EventCtx), content ...gui.View) gui.View {
	t := gui.CurrentTheme()
	color := gui.Color{}
	if selected {
		color = t.ColorSelect
	}
	return gui.Row(gui.ContainerCfg{
		ID:         id,
		Sizing:     gui.FillFit,
		Padding:    gui.NewPadding(3, 4, 3, 4+float32(level*TreeIndent)),
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Radius:     gui.RadiusSmall,
		Color:      color,
		VAlign:     gui.VAlignMiddle,
		Hover:      gui.HoverStyle{Color: t.ColorHover, Cursor: gui.CursorPointingHand},
		OnClick:    onClick,
		Content:    content,
	})
}

// TreeColumn stacks a node and its children.
func TreeColumn(items []gui.View) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingTight,
		SizeBorder: gui.NoBorder,
		Content:    items,
	})
}

// TreeIndented indents content (an input, for example) to a tree level.
func TreeIndented(level int, content gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NewPadding(0, 0, 0, float32(level*TreeIndent)),
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{content},
	})
}

// TreeHint is a non-clickable message inside a tree ("Loading…", "No tables.").
func TreeHint(msg string, level int) gui.View {
	t := gui.CurrentTheme()
	return gui.Row(gui.ContainerCfg{
		Padding:    gui.NewPadding(2, 4, 2, 4+float32(level*TreeIndent)),
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{gui.Text(gui.TextCfg{Text: msg, TextStyle: t.TextStyleCaption})},
	})
}

// TreeLabel is the text of a tree row. It takes the width left in the row
// and clips what does not fit, so a long name never widens the tree: a
// single-line Text cannot shrink below its measured width, but a Clip
// container drops that floor (as go-gui's Combobox does with its label).
func TreeLabel(text string, style gui.TextStyle) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		SizeBorder: gui.NoBorder,
		Clip:       true,
		Content:    []gui.View{gui.Text(gui.TextCfg{Text: text, TextStyle: style})},
	})
}

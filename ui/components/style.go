package components

import (
	"strconv"

	"github.com/Watts-Argentina/anvil/assets"
	"github.com/go-gui-org/go-gui/gui"
)

// Icon paints the icon with the active theme's text color.
func Icon(name string, size float32) gui.View {
	return IconColor(name, size, gui.CurrentTheme().TextStyleDef.Color)
}

// IconColor paints the icon with color.
func IconColor(name string, size float32, color gui.Color) gui.View {
	return gui.Svg(gui.SvgCfg{
		SvgData: assets.GetIconData(name),
		Width:   size,
		Height:  size,
		Sizing:  gui.FixedFixed,
		// The SVGs use stroke="currentColor"; without Color they are invisible.
		Color: color,
	})
}

// ParseHex converts "#RRGGBB" into a color.
func ParseHex(s string) (gui.Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return gui.Color{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return gui.Color{}, false
	}
	return gui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)), true
}

// ColorDot is a connection's color indicator. Without a valid color it uses
// the theme's border color.
func ColorDot(hex string, size float32) gui.View {
	c, ok := ParseHex(hex)
	if !ok {
		c = gui.CurrentTheme().ColorBorder
	}
	return gui.Circle(gui.ContainerCfg{
		Width:      size,
		Height:     size,
		Sizing:     gui.FixedFixed,
		Color:      c,
		SizeBorder: gui.NoBorder,
		Padding:    gui.NoPadding,
	})
}

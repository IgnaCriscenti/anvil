package components

import "github.com/go-gui-org/go-gui/gui"

// IconButton shows the SVG assets/icons/<icon>.svg followed by label. Without
// a label, it uses A11YLabel (or the icon name) as the accessible text.
func IconButton(icon, label string, config ...gui.ButtonCfg) gui.View {

	var cfg gui.ButtonCfg
	if len(config) > 0 {
		cfg = config[0]
	}

	color := gui.CurrentTheme().TextStyleDef.Color
	if cfg.Disabled {
		color = gui.CurrentTheme().TextStyleDisabled.Color
	}
	if cfg.Variant == gui.ButtonPrimary || cfg.Variant == gui.ButtonDanger {
		color = gui.CurrentTheme().ColorTextOnAccent
	}

	cfg.Content = []gui.View{
		IconColor(icon, 14, color),
	}
	if label != "" {
		cfg.Content = append(cfg.Content, gui.Text(gui.TextCfg{
			Text:      label,
			TextStyle: gui.TextStyle{Color: color},
		}))
		if cfg.A11YLabel == "" {
			cfg.A11YLabel = label
		}
	}
	if cfg.A11YLabel == "" {
		cfg.A11YLabel = icon
	}
	return gui.Button(cfg)
}

package assets

import (
	"embed"
	"log"
)

//go:embed icons/*.svg
var Icons embed.FS

// GetIconData returns the SVG at icons/<name>.svg.
func GetIconData(name string) string {
	data, err := Icons.ReadFile("icons/" + name + ".svg")
	if err != nil {
		log.Printf("icon %q: %v", name, err)
		return ""
	}
	return string(data)
}

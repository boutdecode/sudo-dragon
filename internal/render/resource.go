package render

import (
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// A resource node is drawn with the glyph of the terrain it covers (tree: T, P...; vein: n; plant: ")
// and the colour of its resource, in bold to stand out from the scenery.

// resourceHex gives the colour of each resource in the catalogue.
var resourceHex = map[resource.Kind]string{
	// wood
	"oak": "#c2742b", "pine": "#2dd4bf", "birch": "#f8fafc", "ebony": "#a78bfa", "yew": "#a3e635",
	// ore
	"stone": "#9ca3af", "coal": "#6b7280", "copper": "#f97316", "tin": "#cbd5e1", "iron": "#b8a89a", "silver": "#e2e8f0", "gold": "#facc15",
	// fish (no node on the map: the colour is used for lists)
	"gudgeon": "#7dd3fc", "trout": "#fda4af", "carp": "#d6a35a", "pike": "#4d7c0f", "salmon": "#fb7185", "eel": "#475569", "sturgeon": "#94a3b8",
	// herbs
	"sage": "#8b72be", "nettle": "#3b82f6", "mint": "#5eead4", "lavender": "#d8b4fe", "mushroom": "#f87171", "mandrake": "#fb923c",
}

// resourceStyle is the index, in styles, of the style of each resource.
var resourceStyle = map[resource.Kind]int{}

// addResourceStyles adds to styles one style per resource. Called by init (render.go) before the climate styles.
func addResourceStyles() {
	for _, r := range resource.All() {
		hex, ok := resourceHex[r.Kind]
		if !ok {
			hex = "#ffffff"
		}
		resourceStyle[r.Kind] = len(styles)
		styles = append(styles, lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true))
	}
}

// noded is what Terrain can offer in addition: the resource node of a cell (the world has some, a dungeon does not).
type noded interface {
	NodeAt(x, y int) (resource.Kind, world.Tile, bool)
}

// ResourceHex returns the colour ("#rrggbb") of a resource, for legends.
func ResourceHex(k resource.Kind) string { return resourceHex[k] }

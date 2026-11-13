package render

import (
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Un nœud de ressource se dessine avec le glyphe du terrain qu'il recouvre (arbre: T, P…; filon: n; plante: ")
// et la couleur de sa ressource, en gras pour ressortir du décor.

// resourceHex donne la couleur de chaque ressource du catalogue.
var resourceHex = map[resource.Kind]string{
	// bois
	"oak": "#c2742b", "pine": "#2dd4bf", "birch": "#f8fafc", "ebony": "#a78bfa", "yew": "#a3e635",
	// minerais
	"stone": "#9ca3af", "coal": "#6b7280", "copper": "#f97316", "tin": "#cbd5e1", "iron": "#b8a89a", "silver": "#e2e8f0", "gold": "#facc15",
	// poissons (pas de nœud sur la carte: la couleur sert aux listes)
	"gudgeon": "#7dd3fc", "trout": "#fda4af", "carp": "#d6a35a", "pike": "#4d7c0f", "salmon": "#fb7185", "eel": "#475569", "sturgeon": "#94a3b8",
	// plantes
	"sage": "#8b72be", "nettle": "#3b82f6", "mint": "#5eead4", "lavender": "#d8b4fe", "mushroom": "#f87171", "mandrake": "#fb923c",
}

// resourceStyle est l'indice, dans styles, du style de chaque ressource.
var resourceStyle = map[resource.Kind]int{}

// addResourceStyles ajoute à styles un style par ressource. Appelée par init (render.go) avant les styles de climat.
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

// noded est ce que Terrain peut offrir en plus: le nœud de ressource d'une case (le monde en a, un donjon non).
type noded interface {
	NodeAt(x, y int) (resource.Kind, world.Tile, bool)
}

// ResourceHex renvoie la couleur (« #rrggbb ») d'une ressource, pour les légendes.
func ResourceHex(k resource.Kind) string { return resourceHex[k] }

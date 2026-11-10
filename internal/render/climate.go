package render

import (
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Climats: chaque contrée a le sien (voir world.Climate). Seuls les terrains naturels changent de teinte et de
// glyphes; neige, structures, sols et murs restent identiques. Le tempéré reprend les couleurs d'origine
// (baseStyles), si bien qu'il se rend exactement comme avant.

// climateHex donne, pour chaque climat et chaque terrain naturel, la couleur qui remplace celle de baseStyles.
var climateHex = [world.NumClimates]map[world.Tile]string{
	world.Tropical: {
		world.DeepWater: "#0e7490",
		world.Water:     "#22d3ee",
		world.Sand:      "#fde9a8",
		world.Plain:     "#22e06a",
		world.Forest:    "#00a651",
		world.Hills:     "#7cb518",
		world.Mountain:  "#8b9a8f",
		world.Road:      "#b4905f",
	},
	world.Arid: {
		world.DeepWater: "#1e5a8a",
		world.Water:     "#7ad3c4",
		world.Sand:      "#f4d9a0",
		world.Plain:     "#d9bf3f",
		world.Forest:    "#a3a03c",
		world.Hills:     "#c4693a",
		world.Mountain:  "#b39379",
		world.Road:      "#c9a36b",
	},
	world.Boreal: {
		world.DeepWater: "#1e3a8a",
		world.Water:     "#5b8fb9",
		world.Sand:      "#c9c4b2",
		world.Plain:     "#6f9f7e",
		world.Forest:    "#2f6b5a",
		world.Hills:     "#7f8e94",
		world.Mountain:  "#a3aab5",
		world.Road:      "#8f8670",
	},
}

// climateVariants remplace les glyphes de variants selon le climat (ASCII, hors des lettres des monstres).
var climateVariants = [world.NumClimates]map[world.Tile][]rune{
	world.Tropical: {
		world.Plain:  []rune("\"\"\"\"\"'\"\""),
		world.Forest: []rune("TTPTTPT"),
		world.Sand:   []rune("....,.:."),
	},
	world.Arid: {
		world.Plain:  []rune(".,.:.'..,."),
		world.Forest: []rune("t.i..t.."),
		world.Sand:   []rune("..:.:.,.:"),
		world.Hills:  []rune("nn^n.n"),
	},
	world.Boreal: {
		world.Plain:  []rune("..'.,.'.."),
		world.Forest: []rune("AATAATA"),
		world.Sand:   []rune("..,.:.."),
	},
}

// climateStyleBase[c] est l'indice du premier style du climat c dans styles (le tempéré utilise les tuiles).
var climateStyleBase [world.NumClimates]int

// addClimateStyles ajoute à styles les styles des climats non tempérés. Appelée en dernier par init (render.go):
// les init de plusieurs fichiers s'exécutent dans l'ordre alphabétique de leurs noms, pas celui des dépendances.
func addClimateStyles() {
	for c := world.Climate(1); c < world.NumClimates; c++ {
		climateStyleBase[c] = len(styles)
		for t := world.Tile(0); t < world.NumTiles; t++ {
			st := baseStyles[t]
			if hex, ok := climateHex[c][t]; ok {
				st = st.Foreground(lipgloss.Color(hex))
			}
			styles = append(styles, st)
		}
	}
}

// styleFor est l'indice du style de la tuile t sous le climat c.
func styleFor(c world.Climate, t world.Tile) int {
	if c == world.Temperate || c >= world.NumClimates || t >= world.NumTiles {
		return int(t)
	}
	return climateStyleBase[c] + int(t)
}

// TileHex renvoie la couleur (« #rrggbb ») du terrain t sous le climat c, ou "" s'il garde sa couleur d'origine.
func TileHex(c world.Climate, t world.Tile) string {
	if c >= world.NumClimates {
		return ""
	}
	return climateHex[c][t]
}

// glyphIn choisit le glyphe de la tuile t en (x, y) sous le climat c.
func glyphIn(c world.Climate, t world.Tile, x, y int) rune {
	if c < world.NumClimates {
		if v := climateVariants[c][t]; len(v) > 0 {
			return pick(v, x, y)
		}
	}
	return pick(variants[t], x, y)
}

// climated est ce que Terrain peut offrir en plus: le climat d'une case (le monde en a un, un donjon non).
type climated interface {
	ClimateAt(x, y int) world.Climate
}

// ClimateAt fait suivre le climat du terrain décoré, s'il en a un.
func (e entranceTerrain) ClimateAt(x, y int) world.Climate {
	if c, ok := e.Terrain.(climated); ok {
		return c.ClimateAt(x, y)
	}
	return world.Temperate
}

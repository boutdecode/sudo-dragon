package render

import (
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Climates: each region has its own (see world.Climate). Only natural terrains change hue and
// glyphs; snow, structures, floors and walls stay identical. Temperate keeps the original colours
// (baseStyles), so it renders exactly as before.

// climateHex gives, for each climate and each natural terrain, the colour that replaces the one in baseStyles.
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

// climateVariants replaces the glyphs of variants according to the climate (ASCII, outside the monster letters).
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

// climateStyleBase[c] is the index of the first style of climate c in styles (temperate uses the tiles).
var climateStyleBase [world.NumClimates]int

// addClimateStyles adds to styles the styles of the non-temperate climates. Called last by init (render.go):
// the init functions of several files run in alphabetical order of their names, not in dependency order.
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

// styleFor is the style index of tile t under climate c.
func styleFor(c world.Climate, t world.Tile) int {
	if c == world.Temperate || c >= world.NumClimates || t >= world.NumTiles {
		return int(t)
	}
	return climateStyleBase[c] + int(t)
}

// TileHex returns the colour ("#rrggbb") of terrain t under climate c, or "" if it keeps its original colour.
func TileHex(c world.Climate, t world.Tile) string {
	if c >= world.NumClimates {
		return ""
	}
	return climateHex[c][t]
}

// glyphIn chooses the glyph of tile t at (x, y) under climate c.
func glyphIn(c world.Climate, t world.Tile, x, y int) rune {
	if c < world.NumClimates {
		if v := climateVariants[c][t]; len(v) > 0 {
			return pick(v, x, y)
		}
	}
	return pick(variants[t], x, y)
}

// climated is what Terrain can offer in addition: the climate of a cell (the world has one, a dungeon does not).
type climated interface {
	ClimateAt(x, y int) world.Climate
}

// ClimateAt passes on the climate of the decorated terrain, if it has one.
func (e entranceTerrain) ClimateAt(x, y int) world.Climate {
	if c, ok := e.Terrain.(climated); ok {
		return c.ClimateAt(x, y)
	}
	return world.Temperate
}

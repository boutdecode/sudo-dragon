package world

import "github.com/kevin/ascii-adventure/internal/resource"

// Resource nodes (GenV3): trees, veins and plants placed on natural terrain. They are a pure function
// of (seed, x, y): server and client compute them the same way. A node never covers a structure, a
// point of interest, a road, the area around the starting point nor a dungeon entrance.

// resourceSalt distinguishes the node hash from any other draw.
const resourceSalt = 0x5E50C0DE

// nodeCategory gives, for a terrain and a draw u in [0, 1000), the category of the node placed (or false).
func nodeCategory(t Tile, u int) (resource.Category, bool) {
	switch t {
	case Forest:
		if u < 20 {
			return resource.Wood, true
		}
	case Jungle:
		if u < 18 {
			return resource.Wood, true
		}
	case Plain:
		switch {
		case u < 3:
			return resource.Wood, true
		case u < 15:
			return resource.Herb, true
		}
	case Hills:
		if u < 12 {
			return resource.Ore, true
		}
	case Tundra:
		if u < 4 {
			return resource.Ore, true
		}
	case Desert:
		if u < 4 {
			return resource.Ore, true
		}
	case River:
		if u < 25 {
			return resource.Fish, true
		}
	case Sand:
		if u < 2 {
			return resource.Ore, true
		}
	}
	return 0, false
}

// nodeHash is the hash of tile (x, y), specific to nodes.
func (w *World) nodeHash(x, y int) uint64 {
	h := mix64(uint64(w.Seed) ^ resourceSalt)
	h = mix64(h + uint64(int64(x)))
	return mix64(h + uint64(int64(y)))
}

// nodeAt returns the category, the resource and the tile of the node placed on raw terrain t at (x, y).
// The exclusions (structure, POI, road, spawn, dungeon) are nodeTile's business.
func (w *World) nodeAt(x, y int, t Tile) (resource.Category, resource.Kind, bool) {
	if w.GenVersion < GenV3 {
		return 0, "", false
	}
	h := w.nodeHash(x, y)
	cat, ok := nodeCategory(t, int(h%1000))
	if !ok {
		return 0, "", false
	}
	a := w.spawnAnchor()
	if abs(x-a.X) <= startClearance && abs(y-a.Y) <= startClearance {
		return 0, "", false
	}
	if w.DungeonAt(x, y) != nil {
		return 0, "", false
	}
	kind := resource.Pick(cat, int(w.ClimateAt(x, y)), LevelAt(x, y), h>>16)
	return cat, kind, true
}

// nodeTile is the node tile that replaces terrain t at (x, y), if there is one. The caller has already ruled out the
// structures, the POIs and the roads.
func (w *World) nodeTile(x, y int, t Tile) (Tile, bool) {
	cat, _, ok := w.nodeAt(x, y, t)
	if !ok {
		return 0, false
	}
	return nodeTileOf(cat), true
}

func nodeTileOf(c resource.Category) Tile {
	switch c {
	case resource.Ore:
		return OreNode
	case resource.Herb:
		return HerbNode
	case resource.Fish:
		return FishNode
	}
	return TreeNode
}

// ResourceAt returns the harvestable resource on tile (x, y): that of a visible node (TileAt/At give
// TreeNode, OreNode, HerbNode or FishNode). False if the tile carries none.
func (w *World) ResourceAt(x, y int) (resource.Kind, bool) {
	if w.GenVersion < GenV3 {
		return "", false
	}
	switch w.At(x, y) {
	case TreeNode, OreNode, HerbNode, FishNode:
	default:
		return "", false
	}
	_, kind, ok := w.nodeAt(x, y, w.terrainAt(x, y))
	return kind, ok
}

// NodeAt describes the resource node visible at (x, y): its resource and the natural terrain it covers (whose
// glyph the client reuses, only the colour tells the resource). False if the tile carries no node.
func (w *World) NodeAt(x, y int) (resource.Kind, Tile, bool) {
	switch w.At(x, y) {
	case TreeNode, OreNode, HerbNode, FishNode:
		base := w.terrainAt(x, y)
		if _, kind, ok := w.nodeAt(x, y, base); ok {
			return kind, base, true
		}
	}
	return "", 0, false
}

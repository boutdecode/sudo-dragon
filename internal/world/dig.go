package world

import "slices"

// Dig spots of treasure maps. Like the rest of the world, they are ordinary: nothing on the map shows the
// tile. It is the player's map (see game/treasure.go) that keeps its coordinates.

const (
	digSalt      = 8500
	digMargin    = 6 // minimum distance from the tile to the edge of its region
	digTries     = 32
	digClearance = 3 // minimum distance to a dungeon entrance (a point of interest: its footprint plus one tile)
)

// DigSpot looks for a dig spot in region rp, drawn from nonce: natural walkable terrain over 3x3, outside
// the footprint of a place (safe zone included), of the starting point, of a point of interest and of a dungeon entrance.
// Only the raw terrain counts, as for points of interest: the tile may lie on a road.
func (w *World) DigSpot(rp RegionPos, nonce uint64) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, digSalt+nonce%1_000_003)
	anchor := w.spawnAnchor()
	pois := w.POIsOfRegion(rp)
	var dun *Dungeon
	if mayHideDungeon(w.Seed, rp) {
		dun = w.DungeonOfRegion(rp)
	}
	for range digTries {
		x := rp.X*RegionSize + digMargin + rng.IntN(RegionSize-2*digMargin)
		y := rp.Y*RegionSize + digMargin + rng.IntN(RegionSize-2*digMargin)
		if abs(x-anchor.X) <= startClearance && abs(y-anchor.Y) <= startClearance {
			continue
		}
		if slices.ContainsFunc(pois, func(poi *POI) bool { return poi.Bounds.grow(1).Contains(x, y) }) {
			continue
		}
		if dun != nil && max(abs(x-dun.Entrance.X), abs(y-dun.Entrance.Y)) <= digClearance {
			continue
		}
		if w.digSpotOK(rp, x, y) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

func (w *World) digSpotOK(rp RegionPos, x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if !w.terrainAt(x+dx, y+dy).ordinary() {
				return false
			}
		}
	}
	const clear = safeMargin + 2
	if s := w.structureForRegion(rp); s != nil &&
		x >= s.Origin.X-clear && x < s.Origin.X+s.W+clear && y >= s.Origin.Y-clear && y < s.Origin.Y+s.H+clear {
		return false
	}
	return true
}

package world

import (
	"fmt"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Dungeons: a secret entrance on the main map leads to a separate map, generated
// from the dungeon's seed. Everything is deterministic: the server and the client compute the
// same dungeons and the same plans from the world seed, without exchanging anything.
//
// The draws come from dedicated identity streams (identityRNG with its own salt), never from the
// plan generation stream: adding dungeons changes no existing place.

const (
	dungeonChance     = 0.2 // share of the regions that hide a dungeon
	dungeonSalt       = 7000
	dungeonNameSalt   = 7100
	dungeonSeedSalt   = 7200
	dungeonSpotSalt   = 7300
	dungeonMargin     = 16 // minimum distance from the entrance to the edge of its region
	dungeonTries      = 48 // entrance sites tried before giving up
	dungeonClash      = 3  // radius (in regions) within which two dungeons do not share the same name
	dungeonMaxTier    = 5
	dungeonCacheMax   = 4096
	numDungeonNames   = 8
	numDungeonRerolls = 8
)

// Dungeon is a dungeon hidden in a region.
type Dungeon struct {
	ID       string    // unique, stable identifier, "x,y" of its region
	Name     string    // name in French (see NameIn for the other languages)
	Region   RegionPos // region that hides it
	Entrance Pos       // tile of the main map that leads to it
	Seed     int64     // seed of the plan (see NewDungeonMap)
	Tier     int       // difficulty, from 1 to dungeonMaxTier: grows with distance from the origin

	nameIdx int
	stem    stem
}

// NameIn returns the name of the dungeon in language l.
func (d *Dungeon) NameIn(l i18n.Lang) string {
	return dungeonName(l, d.nameIdx, d.stem)
}

func dungeonName(l i18n.Lang, idx int, s stem) string {
	return fmt.Sprintf(i18n.T(l, fmt.Sprintf("dungeon.name.%d", idx)), s.String())
}

// DungeonOfRegion returns the dungeon hidden in region rp, or nil if there is none.
func (w *World) DungeonOfRegion(rp RegionPos) *Dungeon {
	w.dmu.Lock()
	if d, ok := w.dungeons[rp]; ok {
		w.dmu.Unlock()
		return d
	}
	w.dmu.Unlock()

	d := w.buildDungeon(rp) // outside the lock: touches the structures and the roads
	w.dmu.Lock()
	if len(w.dungeons) >= dungeonCacheMax {
		clear(w.dungeons)
	}
	w.dungeons[rp] = d
	w.dmu.Unlock()
	return d
}

// DungeonByID finds a dungeon from its identifier.
func (w *World) DungeonByID(id string) *Dungeon {
	var rp RegionPos
	if n, err := fmt.Sscanf(id, "%d,%d", &rp.X, &rp.Y); err != nil || n != 2 {
		return nil
	}
	d := w.DungeonOfRegion(rp)
	if d == nil || d.ID != id {
		return nil
	}
	return d
}

// DungeonsNear returns the dungeons of the regions lying at most radius regions from tile (x, y).
func (w *World) DungeonsNear(x, y, radius int) []*Dungeon {
	c := RegionOf(x, y)
	var out []*Dungeon
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if d := w.DungeonOfRegion(RegionPos{c.X + dx, c.Y + dy}); d != nil {
				out = append(out, d)
			}
		}
	}
	return out
}

// DungeonsWithin returns the dungeons whose entrance is at most r tiles (Chebyshev distance) from (x, y). It only
// looks at the regions that this square touches: this is the search to run at every step of a player.
func (w *World) DungeonsWithin(x, y, r int) []*Dungeon {
	lo, hi := RegionOf(x-r, y-r), RegionOf(x+r, y+r)
	var out []*Dungeon
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			if d := w.DungeonOfRegion(RegionPos{rx, ry}); d != nil && max(abs(d.Entrance.X-x), abs(d.Entrance.Y-y)) <= r {
				out = append(out, d)
			}
		}
	}
	return out
}

// DungeonAt returns the dungeon whose entrance is exactly on tile (x, y).
func (w *World) DungeonAt(x, y int) *Dungeon {
	d := w.DungeonOfRegion(RegionOf(x, y))
	if d != nil && d.Entrance.X == x && d.Entrance.Y == y {
		return d
	}
	return nil
}

// mayHideDungeon tells whether the region's draw promises it a dungeon. It depends only on the
// seed (not on the terrain), so that neighbours' names can be compared without generating a plan.
func mayHideDungeon(seed int64, rp RegionPos) bool {
	return identityRNG(seed, rp, dungeonSalt).Float64() < dungeonChance
}

// rawDungeonName is the name drawn for (region, attempt), regardless of the neighbours.
func rawDungeonName(seed int64, rp RegionPos, attempt int) (int, stem) {
	rng := identityRNG(seed, rp, dungeonNameSalt+uint64(attempt))
	return rng.IntN(numDungeonNames), stem{rng.IntN(len(placeStart)), rng.IntN(len(placeEnd))}
}

// uniqueDungeonName chooses the name of region rp: the first attempt that no neighbouring dungeon
// (radius dungeonClash) already named before it, in Y then X order, bears. As for places,
// only the neighbours' raw draws count, which avoids any recursion.
func uniqueDungeonName(seed int64, rp RegionPos) (int, stem) {
	taken := make(map[[3]int]bool, 8)
	for dy := -dungeonClash; dy <= dungeonClash; dy++ {
		for dx := -dungeonClash; dx <= dungeonClash; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			q := RegionPos{rp.X + dx, rp.Y + dy}
			if !mayHideDungeon(seed, q) {
				continue
			}
			i, s := rawDungeonName(seed, q, 0)
			taken[[3]int{i, s.a, s.b}] = true
		}
	}
	idx, s := 0, stem{}
	for attempt := range numDungeonRerolls {
		idx, s = rawDungeonName(seed, rp, attempt)
		if !taken[[3]int{idx, s.a, s.b}] {
			break
		}
	}
	return idx, s
}

// dungeonTier grows with the level of the region (see LevelAt), measured at its centre.
func dungeonTier(rp RegionPos) int {
	return min(1+LevelAt(rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2)/4, dungeonMaxTier)
}

func (w *World) buildDungeon(rp RegionPos) *Dungeon {
	if !mayHideDungeon(w.Seed, rp) {
		return nil
	}
	pos, ok := w.dungeonEntrance(rp)
	if !ok {
		return nil
	}
	idx, st := uniqueDungeonName(w.Seed, rp)
	d := &Dungeon{
		ID:       fmt.Sprintf("%d,%d", rp.X, rp.Y),
		Region:   rp,
		Entrance: pos,
		Seed:     int64(identityRNG(w.Seed, rp, dungeonSeedSalt).Uint64()),
		Tier:     dungeonTier(rp),
		nameIdx:  idx,
		stem:     st,
	}
	d.Name = dungeonName(i18n.FR, idx, st)
	return d
}

// dungeonEntrance looks for an entrance tile: walkable natural terrain, far from places, water and the players'
// starting point.
func (w *World) dungeonEntrance(rp RegionPos) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, dungeonSpotSalt)
	anchor := w.spawnAnchor()
	for range dungeonTries {
		x := rp.X*RegionSize + dungeonMargin + rng.IntN(RegionSize-2*dungeonMargin)
		y := rp.Y*RegionSize + dungeonMargin + rng.IntN(RegionSize-2*dungeonMargin)
		if abs(x-anchor.X) <= startClearance && abs(y-anchor.Y) <= startClearance {
			continue
		}
		if w.dungeonSpotOK(rp, x, y) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

// dungeonSpotOK: the tile and its eight neighbours are walkable natural terrain (no water, no mountain), and far from the
// place of the region. Only the raw terrain and the footprint of the place count: also looking for roads (TileAt) makes
// it trace those of all the neighbouring regions, which cost about 10 ms per dungeon and froze the game loop
// when an NPC looked for the dungeons of 49 regions at once. An entrance may therefore lie on a road.
func (w *World) dungeonSpotOK(rp RegionPos, x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if !w.terrainAt(x+dx, y+dy).ordinary() {
				return false
			}
		}
	}
	// A place never leaves its region (regionMargin): only that of the entrance has to be checked. We keep a margin
	// that exceeds the safe zone.
	const clear = safeMargin + 2
	if s := w.structureForRegion(rp); s != nil &&
		x >= s.Origin.X-clear && x < s.Origin.X+s.W+clear && y >= s.Origin.Y-clear && y < s.Origin.Y+s.H+clear {
		return false
	}
	return true
}

package world

import (
	"fmt"
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Points of interest: small outdoor landmarks (campfire, shrine, lair, watchtower, standing stone),
// one tile each, at most one per region. Like dungeons, everything is drawn from the seed by dedicated identity streams
// (never the plan generation one): server and client compute the same points without exchanging anything, and
// adding some moves no existing place or dungeon.

// POIKind is the type of a point of interest.
type POIKind uint8

const (
	POICampfire POIKind = iota + 1 // campfire: heals and becomes the respawn point
	POIShrine                      // shrine: temporary bonus
	POILair                        // lair: monsters and a chest
	POITower                       // watchtower: reveals the surroundings
	POIStone                       // standing stone: teleportation network
	POISpring                      // spring (oasis in an arid climate): restores HP and mana
	POIRuin                        // ruins: a guardian, a chest, a hint about a dungeon
)

const (
	poiChance = 0.6 // share of the regions that carry one of the first five types
	// poiChanceAll bounds the draw range of the types added later (springs, ruins). This range comes *after*
	// poiChance: the points already placed keep their tile and type.
	poiChanceAll = 0.78
	poiSalt      = 8000
	poiKindSalt  = 8100
	poiSeedSalt  = 8200
	poiSpotSalt  = 8300
	poiNameSalt  = 8400
	poiMargin    = 12 // minimum distance from the point to the edge of its region
	poiTries     = 48
	poiCacheMax  = 4096
	poiClear     = 2 // radius of walkable terrain required around the point
	poiDungeonKm = 4 // minimum distance to a dungeon entrance
)

// poiWeights sets the frequency of each type (sum 100).
var poiWeights = []struct {
	kind POIKind
	w    int
}{{POICampfire, 30}, {POIShrine, 20}, {POILair, 20}, {POITower, 15}, {POIStone, 15}}

// In v2, a region may carry up to poiExtraMax more points than the main one, so that the world does not look
// empty. They have their own identity streams (never those of the main point) and are not standing stones:
// the stone network is indexed by region.
const (
	poiExtraMax  = 2
	poiExtraSalt = 3_000_000 // + 10 per extra point; beyond the range of the dig spots (digSalt + nonce)
	poiExtraGap  = 40        // minimum distance between two points of the same region
)

// poiExtraChance is the probability of each extra point (the first, then the second).
var poiExtraChance = [poiExtraMax]float64{0.55, 0.3}

// poiExtraWeights sets the frequency of the types of an extra point (sum 100).
var poiExtraWeights = []struct {
	kind POIKind
	w    int
}{{POICampfire, 25}, {POIShrine, 20}, {POILair, 20}, {POITower, 15}, {POISpring, 10}, {POIRuin, 10}}

// POIKey identifies a point for the game state (delays, lairs): its region and its index in the region.
type POIKey struct {
	Region RegionPos
	Index  int
}

// POI is a point of interest placed in a region.
type POI struct {
	Kind   POIKind
	Region RegionPos
	// Index is 0 for the main point of the region, 1 or 2 for an extra point (v2).
	Index int
	Pos   Pos
	Seed  int64 // seed specific to the point (name, game draws)
	// Slots is the list of monster slots of a lair or ruins (empty for the other types).
	Slots []Pos

	stem stem
}

// ID is the stable identifier of the point: "x,y" of its region, followed by "#k" for an extra point.
func (p *POI) ID() string {
	if p.Index > 0 {
		return fmt.Sprintf("%d,%d#%d", p.Region.X, p.Region.Y, p.Index)
	}
	return fmt.Sprintf("%d,%d", p.Region.X, p.Region.Y)
}

// Key is the key of the point for the game state.
func (p *POI) Key() POIKey { return POIKey{p.Region, p.Index} }

// Tile is the tile that the point puts on the map.
func (p *POI) Tile() Tile {
	switch p.Kind {
	case POICampfire:
		return Campfire
	case POIShrine:
		return Shrine
	case POILair:
		return LairChest
	case POITower:
		return Tower
	case POISpring:
		return Spring
	case POIRuin:
		return Ruin
	}
	return Stone
}

// NameIn returns the name of the point in language l ("Menhir de Valmor", "Sanctuaire de Valmor"...).
func (p *POI) NameIn(l i18n.Lang) string {
	return fmt.Sprintf(i18n.T(l, poiNameKey(p.Kind)), p.stem.String())
}

func poiNameKey(k POIKind) string {
	switch k {
	case POICampfire:
		return "poi.name.campfire"
	case POIShrine:
		return "poi.name.shrine"
	case POILair:
		return "poi.name.lair"
	case POITower:
		return "poi.name.tower"
	case POISpring:
		return "poi.name.spring"
	case POIRuin:
		return "poi.name.ruin"
	}
	return "poi.name.stone"
}

// POIOfRegion returns the main point of interest of region rp, or nil if there is none.
func (w *World) POIOfRegion(rp RegionPos) *POI {
	for _, p := range w.POIsOfRegion(rp) {
		if p.Index == 0 {
			return p
		}
	}
	return nil
}

// POIsOfRegion returns all the points of region rp: the main one first, then the extra ones (v2).
func (w *World) POIsOfRegion(rp RegionPos) []*POI {
	w.pmu.Lock()
	if l, ok := w.pois[rp]; ok {
		w.pmu.Unlock()
		return l
	}
	w.pmu.Unlock()

	var l []*POI
	if p := w.buildPOI(rp); p != nil { // outside the lock: touches the structures
		l = append(l, p)
	}
	l = append(l, w.buildExtraPOIs(rp, l)...)
	w.pmu.Lock()
	if len(w.pois) >= poiCacheMax {
		clear(w.pois)
	}
	w.pois[rp] = l
	w.pmu.Unlock()
	return l
}

// poiIn returns the point of the list whose tile is (x, y), or nil.
func poiIn(list []*POI, x, y int) *POI {
	for _, p := range list {
		if p.Pos.X == x && p.Pos.Y == y {
			return p
		}
	}
	return nil
}

// POIsWithin returns the points at most r tiles (Chebyshev distance) from (x, y).
func (w *World) POIsWithin(x, y, r int) []*POI {
	lo, hi := RegionOf(x-r, y-r), RegionOf(x+r, y+r)
	var out []*POI
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			for _, p := range w.POIsOfRegion(RegionPos{rx, ry}) {
				if max(abs(p.Pos.X-x), abs(p.Pos.Y-y)) <= r {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// POIAt returns the point whose tile is exactly (x, y).
func (w *World) POIAt(x, y int) *POI {
	return poiIn(w.POIsOfRegion(RegionOf(x, y)), x, y)
}

// buildExtraPOIs draws the extra points of region rp (v2), away from the points already placed.
func (w *World) buildExtraPOIs(rp RegionPos, placed []*POI) []*POI {
	var out []*POI
	for k := 1; k <= poiExtraMax; k++ {
		salt := uint64(poiExtraSalt + 10*k)
		if identityRNG(w.Seed, rp, salt).Float64() >= poiExtraChance[k-1] {
			continue
		}
		var avoid []Pos
		for _, p := range placed {
			avoid = append(avoid, p.Pos)
		}
		for _, p := range out {
			avoid = append(avoid, p.Pos)
		}
		pos, ok := w.poiSpotWith(rp, salt+1, avoid)
		if !ok {
			continue
		}
		kr := identityRNG(w.Seed, rp, salt+2).IntN(100)
		var kind POIKind
		for _, e := range poiExtraWeights {
			if kr < e.w {
				kind = e.kind
				break
			}
			kr -= e.w
		}
		nr := identityRNG(w.Seed, rp, salt+3)
		out = append(out, newPOI(kind, rp, k, pos, int64(identityRNG(w.Seed, rp, salt+4).Uint64()), stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))}))
	}
	return out
}

// newPOI assembles a point and its monster slots.
func newPOI(kind POIKind, rp RegionPos, index int, pos Pos, seed int64, st stem) *POI {
	p := &POI{Kind: kind, Region: rp, Index: index, Pos: pos, Seed: seed, stem: st}
	switch kind {
	case POILair:
		p.Slots = []Pos{{pos.X - 2, pos.Y - 1}, {pos.X + 2, pos.Y - 1}, {pos.X, pos.Y + 2}}
	case POIRuin:
		p.Slots = []Pos{{pos.X, pos.Y + 2}} // a single guardian
	}
	return p
}

func (w *World) buildPOI(rp RegionPos) *POI {
	roll := identityRNG(w.Seed, rp, poiSalt).Float64()
	if roll >= poiChanceAll {
		return nil
	}
	pos, ok := w.poiSpot(rp)
	if !ok {
		return nil
	}
	kr := identityRNG(w.Seed, rp, poiKindSalt).IntN(100)
	var kind POIKind
	if roll >= poiChance { // range of the types added afterwards: 55 % springs, 45 % ruins
		kind = POISpring
		if kr >= 55 {
			kind = POIRuin
		}
	} else {
		for _, e := range poiWeights {
			if kr < e.w {
				kind = e.kind
				break
			}
			kr -= e.w
		}
	}
	nr := identityRNG(w.Seed, rp, poiNameSalt)
	return newPOI(kind, rp, 0, pos, int64(identityRNG(w.Seed, rp, poiSeedSalt).Uint64()), stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))})
}

// poiSpot looks for the tile of the point: walkable natural terrain within poiClear tiles around, far from places, from the
// starting point and from the entrance of any dungeon. As for dungeons, only the raw terrain counts (no
// TileAt, too costly because of the roads): a point may therefore lie on a road.
func (w *World) poiSpot(rp RegionPos) (Pos, bool) { return w.poiSpotWith(rp, poiSpotSalt, nil) }

// poiSpotWith is poiSpot with its own identity stream (salt) and positions to keep away from (avoid).
func (w *World) poiSpotWith(rp RegionPos, salt uint64, avoid []Pos) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, salt)
	anchor := w.spawnAnchor()
	var dun *Dungeon
	if mayHideDungeon(w.Seed, rp) {
		dun = w.DungeonOfRegion(rp)
	}
	for range poiTries {
		x := rp.X*RegionSize + poiMargin + rng.IntN(RegionSize-2*poiMargin)
		y := rp.Y*RegionSize + poiMargin + rng.IntN(RegionSize-2*poiMargin)
		if abs(x-anchor.X) <= startClearance && abs(y-anchor.Y) <= startClearance {
			continue
		}
		if dun != nil && max(abs(x-dun.Entrance.X), abs(y-dun.Entrance.Y)) <= poiDungeonKm {
			continue
		}
		if slices.ContainsFunc(avoid, func(a Pos) bool { return max(abs(x-a.X), abs(y-a.Y)) <= poiExtraGap }) {
			continue
		}
		if w.poiSpotOK(rp, x, y) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

func (w *World) poiSpotOK(rp RegionPos, x, y int) bool {
	for dy := -poiClear; dy <= poiClear; dy++ {
		for dx := -poiClear; dx <= poiClear; dx++ {
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

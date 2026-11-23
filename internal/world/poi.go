package world

import (
	"fmt"
	"slices"
	"sync"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Points of interest: outdoor landmarks (campfire, shrine, lair, watchtower, standing stone, spring, ruins), each one a
// small building (see poi_shapes.go) around its interaction tile, at most three per region. Like dungeons, everything is drawn from the seed by dedicated identity streams
// (never the plan generation one): server and client compute the same points without exchanging anything, and
// adding some moves no existing place or dungeon.

// POIKind is the type of a point of interest.
type POIKind uint8

const (
	POICampfire  POIKind = iota + 1 // campfire: heals and becomes the respawn point
	POIShrine                       // shrine: temporary bonus
	POILair                         // lair: monsters and a chest
	POITower                        // watchtower: reveals the surroundings
	POIStone                        // standing stone: teleportation network
	POISpring                       // spring (oasis in an arid climate): restores HP and mana
	POIRuin                         // ruins: a guardian, a chest, a hint about a dungeon
	POICabinWood                    // lumberjack's cabin: sells axes (see cabin.go)
	POICabinOre                     // miner's shelter: sells pickaxes
	POICabinHerb                    // herbalist's hut: sells sickles
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
	poiBigTries  = 24 // extra attempts per tile of radius beyond poiClear
	poiCacheMax  = 4096
	poiClear     = 2    // radius of walkable terrain required around the point
	poiDungeonKm = 4    // minimum distance to a dungeon entrance
	poiDoorSalt  = 8600 // identity stream of the door side, when there is neither road nor place nearby
	poiDoorRange = 40   // the door turns to a road or a place at most this far
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

// Rect is a rectangle of tiles.
type Rect struct{ X, Y, W, H int }

// Contains tells whether (x, y) is in the rectangle.
func (r Rect) Contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// grow widens the rectangle by n tiles on every side.
func (r Rect) grow(n int) Rect { return Rect{r.X - n, r.Y - n, r.W + 2*n, r.H + 2*n} }

// POI is a point of interest placed in a region.
//
// It is built in two phases. The first one (POIsOfRegion) draws everything that roads depend on: type, climate
// variant, bounds, tiles (doors closed) and monster slots. The second one, lazy and once per point, opens the door on
// the side facing the nearest road: it changes nothing but door tiles, so it cannot move a road.
type POI struct {
	Kind   POIKind
	Region RegionPos
	// Index is 0 for the main point of the region, 1 or 2 for an extra point (v2), cabinPOIIndex for the trade cabin.
	Index int
	// Pos is the interaction tile (altar, stairs, chest, fire, central stone, spring): the centre of the building.
	Pos  Pos
	Seed int64 // seed specific to the point (name, game draws)
	// Slots is the list of monster slots of a lair or ruins (empty for the other types).
	Slots []Pos
	// Bounds is the footprint of the building. Variant is the climate that gave it its shape.
	Bounds  Rect
	Variant Climate

	stem  stem
	tiles []Tile // the building, row by row over Bounds; tileNone outside the walls
	gates [numSides][]gateCell

	doorOnce sync.Once
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

// Tile is the tile of the interaction point itself.
func (p *POI) Tile() Tile { return poiTile(p.Kind) }

// TileAt is the tile that the building puts at (x, y): tileNone outside the walls (natural terrain). The first call
// opens the door (second phase).
func (p *POI) TileAt(w *World, x, y int) Tile {
	if !p.Bounds.Contains(x, y) {
		return tileNone
	}
	p.doorOnce.Do(func() { p.openDoor(w) })
	return p.tiles[(y-p.Bounds.Y)*p.Bounds.W+x-p.Bounds.X]
}

func poiTile(k POIKind) Tile {
	switch k {
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
	case POICabinWood, POICabinOre, POICabinHerb:
		return Floor // the vendor stands on it
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
	case POICabinWood:
		return "poi.name.cabin.wood"
	case POICabinOre:
		return "poi.name.cabin.ore"
	case POICabinHerb:
		return "poi.name.cabin.herb"
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

// POIsOfRegion returns all the points of region rp: the main one first, then the extra ones (v2), then the trade
// cabin if there is one (see cabin.go).
func (w *World) POIsOfRegion(rp RegionPos) []*POI {
	w.pmu.Lock()
	if l, ok := w.pois[rp]; ok {
		w.pmu.Unlock()
		return l
	}
	w.pmu.Unlock()

	l := w.basePOIsOfRegion(rp) // outside the lock: touches the structures
	if c := w.buildCabin(rp, l); c != nil {
		l = append(l, c)
	}
	w.pmu.Lock()
	if len(w.pois) >= poiCacheMax {
		clear(w.pois)
	}
	w.pois[rp] = l
	w.pmu.Unlock()
	return l
}

// basePOIsOfRegion builds the main point of region rp and its extra ones, without the trade cabin: the cabin is
// placed away from them, and the ring of forced cabins around the start (see cabin.go) reads the base points of its
// regions without going through the cache.
func (w *World) basePOIsOfRegion(rp RegionPos) []*POI {
	var l []*POI
	if p := w.buildPOI(rp); p != nil {
		l = append(l, p)
	}
	return append(l, w.buildExtraPOIs(rp, l)...)
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

// poiCovering returns the point of the list whose footprint contains (x, y), or nil.
func poiCovering(list []*POI, x, y int) *POI {
	for _, p := range list {
		if p.Bounds.Contains(x, y) {
			return p
		}
	}
	return nil
}

// poiCovering returns the point whose building covers (x, y), or nil.
func (w *World) poiCovering(x, y int) *POI {
	return poiCovering(w.POIsOfRegion(RegionOf(x, y)), x, y)
}

// POIFootprintAt returns the point whose building footprint (walls, floor and the bare tiles around them) contains
// (x, y), or nil.
func (w *World) POIFootprintAt(x, y int) *POI { return w.poiCovering(x, y) }

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
		kr := identityRNG(w.Seed, rp, salt+2).IntN(100)
		var kind POIKind
		for _, e := range poiExtraWeights {
			if kr < e.w {
				kind = e.kind
				break
			}
			kr -= e.w
		}
		pos, ok := w.poiSpotWith(rp, salt+1, avoid, siteRadius(kind))
		if !ok {
			continue
		}
		nr := identityRNG(w.Seed, rp, salt+3)
		out = append(out, w.newPOI(kind, rp, k, pos, int64(identityRNG(w.Seed, rp, salt+4).Uint64()), stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))}))
	}
	return out
}

// newPOI assembles a point: its building (first phase), centred on pos, and its monster slots.
func (w *World) newPOI(kind POIKind, rp RegionPos, index int, pos Pos, seed int64, st stem) *POI {
	cl := w.ClimateAt(pos.X, pos.Y)
	sh := buildShape(kind, cl, seed)
	n := 2*sh.r + 1
	p := &POI{
		Kind: kind, Region: rp, Index: index, Pos: pos, Seed: seed, stem: st,
		Bounds: Rect{pos.X - sh.r, pos.Y - sh.r, n, n}, Variant: cl, tiles: sh.tiles, gates: sh.gates,
	}
	switch kind {
	case POILair:
		p.Slots = []Pos{{pos.X - 2, pos.Y - 1}, {pos.X + 2, pos.Y - 1}, {pos.X, pos.Y + 2}}
	case POIRuin:
		p.Slots = []Pos{{pos.X, pos.Y + 2}} // a single guardian
	}
	return p
}

// openDoor is the second phase: it opens the door on the side facing the nearest road, else the nearest place, else
// a side drawn from the seed. Only the gate tiles change, never the bounds nor the slots.
func (p *POI) openDoor(w *World) {
	side := p.doorSide(w)
	for _, g := range p.gates[side] {
		x, y := p.Pos.X+g.dx, p.Pos.Y+g.dy
		p.tiles[(y-p.Bounds.Y)*p.Bounds.W+x-p.Bounds.X] = g.t
	}
}

// sideToward gives the side of the building that faces tile (tx, ty).
func sideToward(from Pos, tx, ty int) int {
	dx, dy := tx-from.X, ty-from.Y
	switch {
	case abs(dy) >= abs(dx) && dy > 0:
		return sideSouth
	case abs(dy) >= abs(dx):
		return sideNorth
	case dx < 0:
		return sideWest
	}
	return sideEast
}

func (p *POI) doorSide(w *World) int {
	if len(p.gates[sideSouth]) == 0 && len(p.gates[sideNorth]) == 0 {
		return sideSouth // no door (open building)
	}
	// The nearest road tile (Manhattan distance, then Y, then X: the draw never depends on map order).
	best, bestD, found := Pos{}, 0, false
	for _, l := range w.roadsAround(p.Region) {
		for t := range l.tiles {
			d := abs(t.X-p.Pos.X) + abs(t.Y-p.Pos.Y)
			if d > poiDoorRange {
				continue
			}
			if !found || d < bestD || (d == bestD && (t.Y < best.Y || (t.Y == best.Y && t.X < best.X))) {
				best, bestD, found = t, d, true
			}
		}
	}
	if found {
		return sideToward(p.Pos, best.X, best.Y)
	}
	// Otherwise the nearest edge of a place.
	if near := w.StructuresNear(p.Pos, poiDoorRange); len(near) > 0 {
		s := near[0]
		tx := min(max(p.Pos.X, s.Origin.X), s.Origin.X+s.W-1)
		ty := min(max(p.Pos.Y, s.Origin.Y), s.Origin.Y+s.H-1)
		if tx != p.Pos.X || ty != p.Pos.Y {
			return sideToward(p.Pos, tx, ty)
		}
	}
	return identityRNG(w.Seed, p.Region, poiDoorSalt+uint64(10*p.Index)).IntN(numSides)
}

func (w *World) buildPOI(rp RegionPos) *POI {
	roll := identityRNG(w.Seed, rp, poiSalt).Float64()
	if roll >= poiChanceAll {
		return nil
	}
	// The type comes first: the site depends on the size of its building.
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
	pos, ok := w.poiSpotWith(rp, poiSpotSalt, nil, siteRadius(kind))
	if !ok {
		return nil
	}
	nr := identityRNG(w.Seed, rp, poiNameSalt)
	return w.newPOI(kind, rp, 0, pos, int64(identityRNG(w.Seed, rp, poiSeedSalt).Uint64()), stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))})
}

// poiSpotWith looks for the site of a point whose building has radius r (see siteRadius): natural terrain, walkable
// and flat, over the whole footprint, far from places, from the starting point and from the entrance of any dungeon,
// with its own identity stream (salt) and positions to keep away from (avoid). As for dungeons, only the raw terrain
// counts (no TileAt, too costly because of the roads): roads are the ones that go around the buildings (see roads.go).
// A building of radius poiClear (5x5) gets exactly the site that a one-tile point got in the first versions.
func (w *World) poiSpotWith(rp RegionPos, salt uint64, avoid []Pos, r int) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, salt)
	anchor := w.spawnAnchor()
	var dun *Dungeon
	if mayHideDungeon(w.Seed, rp) {
		dun = w.DungeonOfRegion(rp)
	}
	extra := max(r-poiClear, 0)              // how much the building outgrows the minimal one
	for range poiTries + poiBigTries*extra { // a big building has fewer free sites: more attempts
		x := rp.X*RegionSize + poiMargin + rng.IntN(RegionSize-2*poiMargin)
		y := rp.Y*RegionSize + poiMargin + rng.IntN(RegionSize-2*poiMargin)
		if abs(x-anchor.X) <= startClearance+extra && abs(y-anchor.Y) <= startClearance+extra {
			continue
		}
		if dun != nil && max(abs(x-dun.Entrance.X), abs(y-dun.Entrance.Y)) <= poiDungeonKm+extra {
			continue
		}
		if slices.ContainsFunc(avoid, func(a Pos) bool { return max(abs(x-a.X), abs(y-a.Y)) <= poiExtraGap }) {
			continue
		}
		if w.poiSpotOK(rp, x, y, r) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

func (w *World) poiSpotOK(rp RegionPos, x, y, r int) bool {
	clear := r // terrain to check: the footprint, plus one tile around a big building
	if r > poiClear {
		clear++
	}
	for dy := -clear; dy <= clear; dy++ {
		for dx := -clear; dx <= clear; dx++ {
			if !w.terrainAt(x+dx, y+dy).ordinary() {
				return false
			}
		}
	}
	// The footprint stays away from the place of the region: safe zone and a margin.
	const margin = safeMargin + 2
	grow := margin + max(r-poiClear, 0)
	if s := w.structureForRegion(rp); s != nil &&
		x >= s.Origin.X-grow && x < s.Origin.X+s.W+grow && y >= s.Origin.Y-grow && y < s.Origin.Y+s.H+grow {
		return false
	}
	return true
}

package world

import (
	"math/rand/v2"
	"sort"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// RegionSize is the side of a region, in tiles. Each region contains at
// most one structure. It is a multiple of ChunkSize: a chunk never belongs
// to two regions.
const RegionSize = ChunkSize * 8

// regionMargin separates the footprint of a structure from the edge of its region. It must
// stay greater than the safe zone margin (safeMargin) so that a safe
// zone never spills over onto the neighbouring region.
const regionMargin = 6

// safeMargin widens the footprint of a village or town to form its safe zone.
const safeMargin = 3

// startClearance is the minimum distance between the players' starting point
// and the footprint of any structure.
const startClearance = 16

// RegionPos identifies a region in the region grid.
type RegionPos struct{ X, Y int }

// RegionOf returns the region containing tile (x, y).
func RegionOf(x, y int) RegionPos {
	return RegionPos{floorDiv(x, RegionSize), floorDiv(y, RegionSize)}
}

// Kind is the type of a structure.
type Kind uint8

const (
	KindVillage Kind = iota + 1
	KindTown
	KindFortress
)

// String returns the identifier of the type (used in logs and tools).
func (k Kind) String() string {
	switch k {
	case KindVillage:
		return "village"
	case KindTown:
		return "town"
	case KindFortress:
		return "fortress"
	}
	return "?"
}

// NPCKind is the type of a non-player character.
type NPCKind string

const (
	NPCMerchant NPCKind = "merchant" // buys and sells potions
	NPCHealer   NPCKind = "healer"   // heals for gold
	NPCVillager NPCKind = "villager" // no trade: one can only chat with them
	NPCChief    NPCKind = "chief"    // chief of a village: knows the neighbouring places
	NPCMayor    NPCKind = "mayor"    // mayor of a town: knows the neighbouring places
	NPCSmith    NPCKind = "smith"    // blacksmith: sells weapons
	NPCArmorer  NPCKind = "armorer"  // armourer: sells armour, shields and helmets
)

// NPCKinds lists the NPC types that the generator can place (to check
// that the client can draw them all).
func NPCKinds() []NPCKind {
	return []NPCKind{NPCMerchant, NPCHealer, NPCVillager, NPCChief, NPCMayor, NPCSmith, NPCArmorer}
}

// NPC is a stationary character placed by the generator. It is computed
// identically by the server and the client from the seed.
type NPC struct {
	Kind NPCKind
	Pos  Pos

	// Deterministic identity (see names.go): same value for everyone, at every
	// start. Region and Index form the stable key of the NPC.
	Region  RegionPos
	Index   int
	Name    string  // French name (see NameIn for the other languages)
	Persona Persona // personality in French (see PersonaIn)

	// Race and hatred (in %, 0 to 100) towards each race, indexed by hero.Race.Index: drawn
	// from the identity stream (see races.go). Zero towards its own race.
	Race   hero.Race
	Hatred [hero.NumRaces]int

	// GivesQuests: this villager hands out quests. Drawn from the identity stream (without touching
	// the plans): the first villager of each place always gives some, then about one in two.
	GivesQuests bool

	id ident // identity indices, to translate Name and Persona
}

// SlotRole specifies which monster occupies a fortress slot.
type SlotRole uint8

const (
	RoleGuard SlotRole = iota + 1 // garrison soldier
	RoleElite                     // elite soldier, guardian of the keep
	RoleBoss                      // lord of the fortress
)

// Slot is a spot where a fortress spawns a monster.
type Slot struct {
	Pos  Pos
	Role SlotRole
}

// Structure is a village, a town or a fortress.
type Structure struct {
	Kind   Kind
	Name   string // name of the place in French, deterministic (see NameIn)
	Region RegionPos
	Origin Pos // top-left corner of the footprint
	W, H   int // dimensions of the footprint
	NPCs   []NPC
	Guards []NPC // guards that patrol (village and town): starting position only, see guards.go
	Slots  []Slot

	tiles []Tile // W*H, row by row

	fort int    // prefix of a fortress ("Fort", "Citadelle"...), catalogue index
	base string // name of the place without fortress prefix
}

// NameIn returns the name of the place in language l. Only the fortress prefixes
// ("Citadelle" / "Citadel") change: the rest is a proper name.
func (s *Structure) NameIn(l i18n.Lang) string {
	if s.Kind == KindFortress {
		return fortName(l, s.fort, s.base)
	}
	return s.Name
}

// Contains tells whether tile (x, y) is within the footprint.
func (s *Structure) Contains(x, y int) bool {
	return x >= s.Origin.X && x < s.Origin.X+s.W && y >= s.Origin.Y && y < s.Origin.Y+s.H
}

// TileAt returns the tile of the structure at (x, y), which must be within the footprint.
func (s *Structure) TileAt(x, y int) Tile {
	return s.tiles[(y-s.Origin.Y)*s.W+(x-s.Origin.X)]
}

// Center returns the central tile of the footprint.
func (s *Structure) Center() Pos {
	return Pos{s.Origin.X + s.W/2, s.Origin.Y + s.H/2}
}

// Safe tells whether (x, y) is within the safe zone of the structure (footprint + margin).
// Only villages and towns have a safe zone.
func (s *Structure) Safe(x, y int) bool {
	if s.Kind != KindVillage && s.Kind != KindTown {
		return false
	}
	return x >= s.Origin.X-safeMargin && x < s.Origin.X+s.W+safeMargin &&
		y >= s.Origin.Y-safeMargin && y < s.Origin.Y+s.H+safeMargin
}

// SlotKey identifies a fortress slot in a stable way.
type SlotKey struct {
	Region RegionPos
	Index  int
}

// Key returns the key of slot i of the structure.
func (s *Structure) Key(i int) SlotKey { return SlotKey{s.Region, i} }

// Dimensions of the footprints (margin of clear terrain included).
var kindSize = map[Kind][2]int{
	KindVillage:  {32, 26},
	KindTown:     {58, 46},
	KindFortress: {48, 40},
}

// structureCacheMax bounds the memory of the structure cache; beyond it, it is
// emptied and rebuilt (generation is deterministic).
const structureCacheMax = 4096

// spawnAnchor returns the "raw terrain" starting point: the first plain
// or forest in a spiral around the origin. It depends on no structure,
// which avoids a circular dependency with their placement.
func (w *World) spawnAnchor() Pos {
	w.anchorOnce.Do(func() {
		w.anchor = Pos{}
		for r := 0; r < 500; r++ {
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					if max(abs(dx), abs(dy)) != r {
						continue
					}
					if t := w.terrainAt(dx, dy); t == Plain || t == Forest {
						w.anchor = Pos{dx, dy}
						return
					}
				}
			}
		}
	})
	return w.anchor
}

// structureForRegion returns the structure of the region, or nil. The result is cached.
func (w *World) structureForRegion(rp RegionPos) *Structure {
	w.smu.Lock()
	defer w.smu.Unlock()
	if s, ok := w.structs[rp]; ok {
		return s
	}
	if len(w.structs) >= structureCacheMax {
		clear(w.structs)
	}
	s := w.buildRegion(rp)
	w.structs[rp] = s
	return s
}

func regionRNG(seed int64, rp RegionPos) *rand.Rand {
	a := uint64(rp.X)*0x9E3779B97F4A7C15 ^ uint64(rp.Y)*0xC2B2AE3D27D4EB4F
	return rand.New(rand.NewPCG(uint64(seed)^0x5171C7, a^0xD1B54A32D192ED03))
}

// buildRegion decides the content of a region.
func (w *World) buildRegion(rp RegionPos) *Structure {
	rng := regionRNG(w.Seed, rp)

	// The starting point's region gets a village: a new player must
	// find one near them.
	if anchor := w.spawnAnchor(); RegionOf(anchor.X, anchor.Y) == rp {
		if s := w.buildStartVillage(rp, rng, anchor); s != nil {
			return s
		}
	}

	if rng.Float64() >= w.structureChance() { // no structure here
		return nil
	}
	var kind Kind
	switch r := rng.Float64(); {
	case r < 0.55:
		kind = KindVillage
	case r < 0.85:
		kind = KindFortress
	default:
		kind = KindTown
	}
	x0, y0, ok := w.chooseSite(rp, kind, rng)
	if !ok {
		return nil
	}
	return w.generate(rp, kind, x0, y0, rng)
}

// buildStartVillage places the starting village at the best site of the region
// closest to the starting point. Returns nil if the region offers no
// suitable site (island, mountains...): the ordinary placement then takes over.
func (w *World) buildStartVillage(rp RegionPos, rng *rand.Rand, anchor Pos) *Structure {
	size := kindSize[KindVillage]

	// Candidate sites, from the closest to the starting point to the farthest.
	// We stop at the first suitable one: evaluating the terrain under each site
	// is expensive (nearly 300,000 noise evaluations for all of them), and the
	// first candidates almost always suffice. The result is the same
	// as comparing all the sites: at equal distance, the first in scan
	// order wins (stable sort).
	type cand struct{ x0, y0, dist int }
	var cands []cand
	for y0 := regionMargin; y0 <= RegionSize-size[1]-regionMargin; y0 += 4 {
		for x0 := regionMargin; x0 <= RegionSize-size[0]-regionMargin; x0 += 4 {
			ox, oy := rp.X*RegionSize+x0, rp.Y*RegionSize+y0
			if w.nearAnchor(ox, oy, size[0], size[1], anchor) {
				continue
			}
			d := abs(ox+size[0]/2-anchor.X) + abs(oy+size[1]/2-anchor.Y)
			cands = append(cands, cand{x0, y0, d})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })

	for _, c := range cands {
		ox, oy := rp.X*RegionSize+c.x0, rp.Y*RegionSize+c.y0
		if flat, _ := w.siteQuality(KindVillage, ox, oy, size[0], size[1]); flat >= 0.85 {
			return w.generate(rp, KindVillage, c.x0, c.y0, rng)
		}
	}
	return nil
}

// nearAnchor tells whether the footprint, widened by startClearance, contains the starting point.
func (w *World) nearAnchor(ox, oy, W, H int, anchor Pos) bool {
	return anchor.X >= ox-startClearance && anchor.X < ox+W+startClearance &&
		anchor.Y >= oy-startClearance && anchor.Y < oy+H+startClearance
}

// chooseSite draws candidate sites in the region and keeps the best one.
func (w *World) chooseSite(rp RegionPos, kind Kind, rng *rand.Rand) (x0, y0 int, ok bool) {
	size := kindSize[kind]
	anchor := w.spawnAnchor()
	bestScore := -1.0
	for range 14 {
		cx := regionMargin + rng.IntN(RegionSize-size[0]-2*regionMargin+1)
		cy := regionMargin + rng.IntN(RegionSize-size[1]-2*regionMargin+1)
		ox, oy := rp.X*RegionSize+cx, rp.Y*RegionSize+cy
		if w.nearAnchor(ox, oy, size[0], size[1], anchor) {
			continue
		}
		flat, pref := w.siteQuality(kind, ox, oy, size[0], size[1])
		if flat < 0.9 {
			continue
		}
		score := pref + rng.Float64()*0.05
		if kind == KindTown && w.waterNearby(ox+size[0]/2, oy+size[1]/2) {
			score += 0.3
		}
		if score > bestScore {
			bestScore, x0, y0, ok = score, cx, cy, true
		}
	}
	return x0, y0, ok
}

// siteQuality samples the raw terrain under the footprint (one point in three).
// flat is the share of walkable, flat terrain (no water, mountain or snow);
// pref is the mean suitability for the structure type.
func (w *World) siteQuality(kind Kind, ox, oy, W, H int) (flat, pref float64) {
	var n, nFlat int
	var sumPref float64
	for dy := 0; dy < H; dy += 3 {
		for dx := 0; dx < W; dx += 3 {
			n++
			t := w.terrainAt(ox+dx, oy+dy)
			if t.ordinary() {
				nFlat++
				sumPref += preference(kind, t)
			}
		}
	}
	return float64(nFlat) / float64(n), sumPref / float64(n)
}

// preference rates from 0 to 1 the suitability of a terrain for a structure type.
func preference(kind Kind, t Tile) float64 {
	switch kind {
	case KindFortress:
		switch t {
		case Hills:
			return 1
		case Plain:
			return 0.6
		case Forest:
			return 0.4
		}
		return 0.2
	case KindTown:
		if t == Plain {
			return 1
		}
		return 0.3
	}
	switch t { // village
	case Plain:
		return 1
	case Forest:
		return 0.6
	}
	return 0.3
}

// waterNearby tells whether there is water in any of the eight directions around (x, y).
func (w *World) waterNearby(x, y int) bool {
	const d = 28
	for _, v := range [8][2]int{{d, 0}, {-d, 0}, {0, d}, {0, -d}, {d, d}, {d, -d}, {-d, d}, {-d, -d}} {
		if t := w.terrainAt(x+v[0], y+v[1]); t == Water || t == DeepWater {
			return true
		}
	}
	return false
}

// generate draws the structure and places it at (x0, y0) in the region.
func (w *World) generate(rp RegionPos, kind Kind, x0, y0 int, rng *rand.Rand) *Structure {
	size := kindSize[kind]
	var c *canvas
	switch kind {
	case KindVillage:
		// A village has only one of the two equipment sellers, chosen by the identity stream (not by the plans' one).
		vendor := NPCSmith
		if identityRNG(w.Seed, rp, 7000).IntN(2) == 1 {
			vendor = NPCArmorer
		}
		c = layoutVillage(size[0], size[1], rng, vendor)
	case KindTown:
		c = layoutTown(size[0], size[1], rng)
	default:
		c = layoutFortress(size[0], size[1], rng)
	}
	origin := Pos{rp.X*RegionSize + x0, rp.Y*RegionSize + y0}
	s := &Structure{Kind: kind, Region: rp, Origin: origin, W: c.w, H: c.h, tiles: c.t}
	// Identities: separate streams, so as not to shift the draw of the plans.
	s.Name, s.fort, s.base = placeName(kind, identityRNG(w.Seed, rp, 0), baseFor(w.Seed, rp))
	taken := map[string]bool{}
	for i, n := range c.npcs {
		// Two NPCs of the same place never bear the same name: we redraw with another salt.
		var id ident
		var name string
		for try := uint64(0); try < 16; try++ {
			id = npcIdentity(n.Kind, identityRNG(w.Seed, rp, uint64(i)+1+100*try))
			name = id.name(i18n.FR, n.Kind)
			if !taken[name] {
				break
			}
		}
		taken[name] = true
		s.NPCs = append(s.NPCs, NPC{
			Kind: n.Kind, Pos: Pos{origin.X + n.Pos.X, origin.Y + n.Pos.Y},
			Region: rp, Index: i, Name: name, Persona: id.persona(i18n.FR), id: id,
		})
	}
	w.makeGuards(s, taken)
	w.assignRaces(s)
	firstVillager := true
	for i := range s.NPCs {
		if s.NPCs[i].Kind != NPCVillager {
			continue
		}
		s.NPCs[i].GivesQuests = firstVillager || identityRNG(w.Seed, rp, 5000+uint64(i)).IntN(2) == 0
		firstVillager = false
	}
	for _, sl := range c.slots {
		s.Slots = append(s.Slots, Slot{Pos{origin.X + sl.Pos.X, origin.Y + sl.Pos.Y}, sl.Role})
	}
	return s
}

// StructureOfRegion returns the structure of the region (or nil). Used to name
// distant places after their region.
func (w *World) StructureOfRegion(rp RegionPos) *Structure { return w.structureForRegion(rp) }

// PlaceAt returns the place where (x, y) is: the structure whose footprint or safe zone contains
// the tile, or nil in the wild. Used to display "where one is".
func (w *World) PlaceAt(x, y int) *Structure {
	s := w.structureForRegion(RegionOf(x, y))
	if s != nil && (s.Contains(x, y) || s.Safe(x, y)) {
		return s
	}
	return nil
}

// StructureAt returns the structure whose footprint contains (x, y), or nil.
func (w *World) StructureAt(x, y int) *Structure {
	s := w.structureForRegion(RegionOf(x, y))
	if s != nil && s.Contains(x, y) {
		return s
	}
	return nil
}

// StructuresNear returns the structures whose footprint is within radius
// tiles of p (in Chebyshev distance), sorted from closest to farthest.
func (w *World) StructuresNear(p Pos, radius int) []*Structure {
	lo, hi := RegionOf(p.X-radius, p.Y-radius), RegionOf(p.X+radius, p.Y+radius)
	var out []*Structure
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			s := w.structureForRegion(RegionPos{rx, ry})
			if s == nil {
				continue
			}
			// Distance from p to the footprint rectangle.
			dx := max(s.Origin.X-p.X, 0, p.X-(s.Origin.X+s.W-1))
			dy := max(s.Origin.Y-p.Y, 0, p.Y-(s.Origin.Y+s.H-1))
			if max(dx, dy) <= radius {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Center(), out[j].Center()
		return abs(a.X-p.X)+abs(a.Y-p.Y) < abs(b.X-p.X)+abs(b.Y-p.Y)
	})
	return out
}

// Safe tells whether (x, y) is within the safe zone of a village or town:
// monsters neither spawn nor enter there.
func (w *World) Safe(x, y int) bool {
	s := w.structureForRegion(RegionOf(x, y))
	return s != nil && s.Safe(x, y)
}

// NPCAt returns the posted NPC at (x, y), if there is one.
func (w *World) NPCAt(x, y int) (NPC, bool) {
	s := w.StructureAt(x, y)
	if s == nil {
		return NPC{}, false
	}
	for _, n := range s.NPCs {
		if n.Pos.X == x && n.Pos.Y == y {
			return n, true
		}
	}
	return NPC{}, false
}

// NPCsIn returns the NPCs located within the rectangle [x0, x1] x [y0, y1]. A
// single search per frame suffices for rendering, instead of one per tile.
func (w *World) NPCsIn(x0, y0, x1, y1 int) []NPC {
	lo, hi := RegionOf(x0, y0), RegionOf(x1, y1)
	var out []NPC
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			s := w.structureForRegion(RegionPos{rx, ry})
			if s == nil {
				continue
			}
			for _, n := range s.NPCs {
				if n.Pos.X >= x0 && n.Pos.X <= x1 && n.Pos.Y >= y0 && n.Pos.Y <= y1 {
					out = append(out, n)
				}
			}
		}
	}
	return out
}

// structureChance is the share of the regions that carry a place. The draw consumes the same number of values whatever
// the threshold: in v1 it stays at 55 % (existing worlds do not move), v2 puts more.
func (w *World) structureChance() float64 {
	return 0.7
}

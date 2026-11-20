package world

import "github.com/kevin/ascii-adventure/internal/i18n"

// Trade cabins: isolated buildings, each one with a vendor of gathering tools (a lumberjack for axes, a miner for
// pickaxes, an herbalist for sickles). They are points of interest of their own (index cabinPOIIndex), built after the
// others with their own identity streams: the existing points keep their type, tile and shape. They are never in a place.
//
// The trade follows the land around the site (forest: lumberjack, rock: miner, meadow: herbalist). Since a tool is
// mandatory to gather, the regions around the starting point are guaranteed to hold one cabin of each trade.

// Kinds of the trade cabins, in the order of the trades (wood, ore, herbs).
var cabinKinds = [3]POIKind{POICabinWood, POICabinOre, POICabinHerb}

const (
	// CabinIndexBase is the rank of a cabin's vendor in the numbering of the NPCs of its region (Index): beyond the posted
	// NPCs, the guards, and the caravans (CaravanIndexBase).
	CabinIndexBase = 300

	cabinPOIIndex  = poiExtraMax + 1 // Index of the cabin among the points of its region
	cabinSalt      = 3_500_000       // identity streams: +0 chance, +1 site, +2 trade tie-break, +3 name, +4 seed, +5 vendor
	cabinChance    = 0.3             // share of the regions, beyond the starting ring, that carry a cabin
	cabinSampleR   = 16              // radius (tiles) of the terrain sampled to choose the trade
	cabinSampleGap = 8               // spacing of the samples
	cabinRing      = 2               // radius (regions) of the starting ring where one cabin of each trade is forced
)

// cabinSite is the place and trade of a forced cabin.
type cabinSite struct {
	kind POIKind
	pos  Pos
}

// IsCabin tells whether k is a trade cabin.
func (k POIKind) IsCabin() bool { return k == POICabinWood || k == POICabinOre || k == POICabinHerb }

// VendorKind is the NPC type that keeps a cabin of kind k ("" if k is not a cabin).
func (k POIKind) VendorKind() NPCKind {
	switch k {
	case POICabinWood:
		return NPCLumberjack
	case POICabinOre:
		return NPCMiner
	case POICabinHerb:
		return NPCHerbalist
	}
	return ""
}

// cabinOf returns the trade cabin of region rp, or nil.
func (w *World) cabinOf(rp RegionPos) *POI {
	for _, p := range w.POIsOfRegion(rp) {
		if p.Kind.IsCabin() {
			return p
		}
	}
	return nil
}

// CabinVendor returns the vendor of cabin p. False if p is not a cabin.
func (w *World) CabinVendor(p *POI) (NPC, bool) {
	kind := p.Kind.VendorKind()
	if kind == "" {
		return NPC{}, false
	}
	id := npcIdentity(kind, identityRNG(w.Seed, p.Region, cabinSalt+5))
	return NPC{
		Kind: kind, Pos: p.Pos, Region: p.Region, Index: CabinIndexBase,
		Name: id.name(i18n.FR, kind), Persona: id.persona(i18n.FR), id: id,
	}, true
}

// terrainTrade gives the trade (index in cabinKinds) that a terrain suggests, or false for water and the like.
func terrainTrade(t Tile) (int, bool) {
	switch t {
	case Forest, Jungle:
		return 0, true
	case Hills, Mountain, Tundra, Desert:
		return 1, true
	case Plain, Swamp, Sand:
		return 2, true
	}
	return 0, false
}

// cabinAffinity counts, for each trade, the samples of raw terrain around pos that suit it.
func (w *World) cabinAffinity(pos Pos) (aff [3]int) {
	for dy := -cabinSampleR; dy <= cabinSampleR; dy += cabinSampleGap {
		for dx := -cabinSampleR; dx <= cabinSampleR; dx += cabinSampleGap {
			if i, ok := terrainTrade(w.terrainAt(pos.X+dx, pos.Y+dy)); ok {
				aff[i]++
			}
		}
	}
	return aff
}

// cabinSiteIn looks for the site of a cabin in region rp, away from the points already placed there.
func (w *World) cabinSiteIn(rp RegionPos, base []*POI) (Pos, bool) {
	avoid := make([]Pos, 0, len(base))
	for _, p := range base {
		avoid = append(avoid, p.Pos)
	}
	return w.poiSpotWith(rp, cabinSalt+1, avoid, siteRadius(POICabinWood))
}

// inCabinRing tells whether rp is within the starting ring.
func (w *World) inCabinRing(rp RegionPos) bool {
	a := w.spawnAnchor()
	c := RegionOf(a.X, a.Y)
	return abs(rp.X-c.X) <= cabinRing && abs(rp.Y-c.Y) <= cabinRing
}

// forcedCabins computes, once, the cabins of the starting ring: one per trade, each in the region of the ring where the
// land suits it best (the trades are served in order, a region carries a single cabin). It only reads the raw terrain
// and the base points, so it never depends on the roads or on the cache of points.
func (w *World) forcedCabins() map[RegionPos]cabinSite {
	w.cabinOnce.Do(func() {
		type cand struct {
			rp  RegionPos
			pos Pos
			aff [3]int
		}
		a := w.spawnAnchor()
		c := RegionOf(a.X, a.Y)
		var cands []cand
		for dy := -cabinRing; dy <= cabinRing; dy++ {
			for dx := -cabinRing; dx <= cabinRing; dx++ {
				rp := RegionPos{c.X + dx, c.Y + dy}
				if pos, ok := w.cabinSiteIn(rp, w.basePOIsOfRegion(rp)); ok {
					cands = append(cands, cand{rp, pos, w.cabinAffinity(pos)})
				}
			}
		}
		plan := make(map[RegionPos]cabinSite, len(cabinKinds))
		for i, kind := range cabinKinds {
			best := -1
			for j, cd := range cands {
				if _, taken := plan[cd.rp]; taken {
					continue
				}
				if best < 0 || cd.aff[i] > cands[best].aff[i] {
					best = j
				}
			}
			if best >= 0 {
				plan[cands[best].rp] = cabinSite{kind, cands[best].pos}
			}
		}
		w.cabinForced = plan
	})
	return w.cabinForced
}

// buildCabin builds the cabin of region rp, or nil. base are the other points of the region.
func (w *World) buildCabin(rp RegionPos, base []*POI) *POI {
	var site cabinSite
	if f, ok := w.forcedCabins()[rp]; ok {
		site = f
	} else {
		if w.inCabinRing(rp) || identityRNG(w.Seed, rp, cabinSalt).Float64() >= cabinChance {
			return nil
		}
		pos, ok := w.cabinSiteIn(rp, base)
		if !ok {
			return nil
		}
		site = cabinSite{w.cabinTrade(rp, pos), pos}
	}
	nr := identityRNG(w.Seed, rp, cabinSalt+3)
	return w.newPOI(site.kind, rp, cabinPOIIndex, site.pos, int64(identityRNG(w.Seed, rp, cabinSalt+4).Uint64()),
		stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))})
}

// cabinTrade picks the trade that suits the land around pos: the best affinity, ties (and barren land) drawn.
func (w *World) cabinTrade(rp RegionPos, pos Pos) POIKind {
	aff := w.cabinAffinity(pos)
	rng := identityRNG(w.Seed, rp, cabinSalt+2)
	best := max(aff[0], aff[1], aff[2])
	var tied []int
	for i, n := range aff {
		if n == best {
			tied = append(tied, i)
		}
	}
	return cabinKinds[tied[rng.IntN(len(tied))]]
}

package world

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Districts: an organic partition of the world, laid OVER the region grid (which remains
// the technical unit of places, rumours and reputation). It is a Voronoi diagram
// with offset seeds (Worley): each territorySize x territorySize cell carries a seed, and
// a point belongs to the district of the nearest seed. The borders are irregular.
//
// Everything is computed by arithmetic and hashing, from the seed only: the server and the
// client (which draws the map title) get the same districts without exchanging anything.

// territorySize is the side of the cell carrying a seed, hence the order of magnitude of a district.
const territorySize = 1000

// TerritoryID identifies a district: the cell of its seed.
type TerritoryID struct{ X, Y int }

// terrainTheme classifies the dominant terrain of a district (gives the word of its name).
type terrainTheme uint8

const (
	themeCoast terrainTheme = iota // water, beaches
	themePlain
	themeForest
	themeHills
	themeMountain // mountains, snow
	themeDesert
	themeJungle
	themeSwamp
	themeTundra
	numThemes
)

var themeKeys = [numThemes]string{"coast", "plain", "forest", "hills", "mountain", "desert", "jungle", "swamp", "tundra"}

// Size of the stem family of a district.
const familySize = 8

// Territory is a named district.
type Territory struct {
	ID    TerritoryID
	Site  Pos
	Theme terrainTheme

	// climate is the visual climate (see climate.go): drawn from the seed in v1, from the seed cell's climate in v2.
	climate Climate

	// Family of place names of the district: familySize starts and ends of placeStart/placeEnd.
	starts, ends [familySize]int
	// Stem of the district's name (indices into the family) and variant of the theme word (0 or 1).
	nameStart, nameEnd, variant int
}

// terrHash combines the seed, a cell and a salt into a well-mixed value.
func terrHash(seed int64, x, y int, salt uint64) uint64 {
	h := mix64(uint64(seed) ^ 0x7E221702)
	h = mix64(h + uint64(int64(x)))
	h = mix64(h + uint64(int64(y)))
	return mix64(h + salt)
}

func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// siteOf returns the seed of cell (cx, cy), offset by 15 % to 85 % of the cell.
func siteOf(seed int64, cx, cy int) Pos {
	fx := 0.15 + 0.7*unit(terrHash(seed, cx, cy, 1))
	fy := 0.15 + 0.7*unit(terrHash(seed, cx, cy, 2))
	return Pos{cx*territorySize + int(fx*territorySize), cy*territorySize + int(fy*territorySize)}
}

// territoryIDAt returns the district of point (x, y): the nearest seed among those of the 3 x 3 neighbouring cells.
func territoryIDAt(seed int64, x, y int) TerritoryID {
	cx, cy := floorDiv(x, territorySize), floorDiv(y, territorySize)
	best, bestD := TerritoryID{cx, cy}, int64(-1)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			s := siteOf(seed, cx+dx, cy+dy)
			ddx, ddy := int64(s.X-x), int64(s.Y-y)
			d := ddx*ddx + ddy*ddy
			if bestD < 0 || d < bestD || (d == bestD && (cx+dx < best.X || (cx+dx == best.X && cy+dy < best.Y))) {
				best, bestD = TerritoryID{cx + dx, cy + dy}, d
			}
		}
	}
	return best
}

func territoryRNG(seed int64, id TerritoryID, salt uint64) *rand.Rand {
	h := terrHash(seed, id.X, id.Y, salt)
	return rand.New(rand.NewPCG(h, mix64(h^0x3C6EF372FE94F82B)))
}

// familyOf returns the starts and ends of the district's stems, without sampling the terrain
// (used to name places: it is pure hashing).
func familyOf(seed int64, id TerritoryID) (starts, ends [familySize]int) {
	rng := territoryRNG(seed, id, 10)
	s := rng.Perm(len(placeStart))[:familySize]
	e := rng.Perm(len(placeEnd))[:familySize]
	slices.Sort(s)
	slices.Sort(e)
	copy(starts[:], s)
	copy(ends[:], e)
	return
}

// TerritoryAt returns the district of point (x, y) (cached).
func (w *World) TerritoryAt(x, y int) *Territory {
	id := territoryIDAt(w.Seed, x, y)
	w.tmu.Lock()
	defer w.tmu.Unlock()
	if t, ok := w.territories[id]; ok {
		return t
	}
	if len(w.territories) >= structureCacheMax {
		clear(w.territories)
	}
	t := w.buildTerritory(id)
	w.territories[id] = t
	return t
}

func (w *World) buildTerritory(id TerritoryID) *Territory {
	t := &Territory{ID: id, Site: siteOf(w.Seed, id.X, id.Y)}
	t.climate = climateOf(w.Seed, id)
	var sumTemp, sumWet float64 // real mean climate of the land points (v2)
	var nLand int
	t.starts, t.ends = familyOf(w.Seed, id)
	rng := territoryRNG(w.Seed, id, 20)
	t.nameStart, t.nameEnd, t.variant = rng.IntN(familySize), rng.IntN(familySize), rng.IntN(2)

	// Dominant terrain: a grid of points around the seed, restricted to the district.
	var count [numThemes]int
	const step, half = 80, 4
	for j := -half; j <= half; j++ {
		for i := -half; i <= half; i++ {
			x, y := t.Site.X+i*step, t.Site.Y+j*step
			if territoryIDAt(w.Seed, x, y) != id {
				continue
			}
			if w.GenVersion >= GenV2 {
				if e := w.elev2(x, y); e > 0.04 {
					tp, wt := w.climate2(x, y, e)
					sumTemp, sumWet, nLand = sumTemp+tp, sumWet+wt, nLand+1
				}
			}
			switch w.terrainAt(x, y) {
			case DeepWater:
				// The open sea does not name a district: only its coasts count.
			case Water, Sand:
				count[themeCoast]++ // land counts double: a district is only called "coastal" if water really dominates
			case Plain:
				count[themePlain] += 2
			case Forest:
				count[themeForest] += 2
			case Hills:
				count[themeHills] += 2
			case Desert:
				count[themeDesert] += 2
			case Jungle:
				count[themeJungle] += 2
			case Swamp:
				count[themeSwamp] += 2
			case Tundra:
				count[themeTundra] += 2
			case River:
				count[themeCoast]++
			default: // Mountain, Snow
				count[themeMountain] += 2
			}
		}
	}
	if nLand > 0 {
		t.climate = climateFrom(sumTemp/float64(nLand), sumWet/float64(nLand))
	}
	t.Theme = themePlain // almost entirely marine district: we call it plain, for lack of better
	best := 0
	for th := terrainTheme(0); th < numThemes; th++ {
		if count[th] > best {
			t.Theme, best = th, count[th]
		}
	}
	return t
}

// Stem is the stem that gives the district its name.
func (t *Territory) Stem() string {
	return placeStart[t.starts[t.nameStart]] + placeEnd[t.ends[t.nameEnd]]
}

// NameIn composes the name of the district in language l: "Forêts d'Aube", "Forests of Aube".
func (t *Territory) NameIn(l i18n.Lang) string {
	stem := t.Stem()
	of := i18n.T(l, "region.of")
	if startsWithVowel(stem) {
		of = i18n.T(l, "region.of_elided")
	}
	return i18n.T(l, fmt.Sprintf("region.theme.%s.%d", themeKeys[t.Theme], t.variant)) + " " + of + stem
}

func startsWithVowel(s string) bool {
	for _, r := range s {
		return strings.ContainsRune("aeiouyAEIOUYéèêâîôûÉÈÀ", unicode.ToLower(r)) || strings.ContainsRune("AEIOUYÉÈÀ", r)
	}
	return false
}

// TerritoryOfRegion returns the district of the centre of a grid region (the one that names the place of that region).
func (w *World) TerritoryOfRegion(rp RegionPos) *Territory {
	return w.TerritoryAt(rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2)
}

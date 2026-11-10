package world

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Contrées : un découpage organique du monde, posé PAR-DESSUS la grille de régions (qui reste
// l'unité technique des lieux, des rumeurs et de la réputation). C'est un diagramme de Voronoi
// à germes décalés (Worley) : chaque case de territorySize × territorySize porte un germe, et
// un point appartient à la contrée du germe le plus proche. Les frontières sont irrégulières.
//
// Tout se calcule par arithmétique et hachage, depuis la seed seulement : le serveur et le
// client (qui dessine le titre de la carte) obtiennent les mêmes contrées sans rien échanger.

// territorySize est le côté de la case porteuse d'un germe, donc l'ordre de grandeur d'une contrée.
const territorySize = 1000

// TerritoryID identifie une contrée : la case de son germe.
type TerritoryID struct{ X, Y int }

// terrainTheme classe le terrain dominant d'une contrée (donne le mot de son nom).
type terrainTheme uint8

const (
	themeCoast terrainTheme = iota // eau, plages
	themePlain
	themeForest
	themeHills
	themeMountain // montagnes, neige
	themeDesert
	themeJungle
	themeSwamp
	themeTundra
	numThemes
)

var themeKeys = [numThemes]string{"coast", "plain", "forest", "hills", "mountain", "desert", "jungle", "swamp", "tundra"}

// Taille de la famille de radicaux d'une contrée.
const familySize = 8

// Territory est une contrée nommée.
type Territory struct {
	ID    TerritoryID
	Site  Pos
	Theme terrainTheme

	// climate est le climat visuel (voir climate.go): tiré de la graine en v1, du climat du germe en v2.
	climate Climate

	// Famille de noms de lieux de la contrée : familySize débuts et fins de placeStart/placeEnd.
	starts, ends [familySize]int
	// Radical du nom de la contrée (indices dans la famille) et variante du mot de thème (0 ou 1).
	nameStart, nameEnd, variant int
}

// terrHash combine la seed, une case et un sel en une valeur bien mélangée.
func terrHash(seed int64, x, y int, salt uint64) uint64 {
	h := mix64(uint64(seed) ^ 0x7E221702)
	h = mix64(h + uint64(int64(x)))
	h = mix64(h + uint64(int64(y)))
	return mix64(h + salt)
}

func unit(h uint64) float64 { return float64(h>>11) / (1 << 53) }

// siteOf renvoie le germe de la case (cx, cy), décalé de 15 % à 85 % de la case.
func siteOf(seed int64, cx, cy int) Pos {
	fx := 0.15 + 0.7*unit(terrHash(seed, cx, cy, 1))
	fy := 0.15 + 0.7*unit(terrHash(seed, cx, cy, 2))
	return Pos{cx*territorySize + int(fx*territorySize), cy*territorySize + int(fy*territorySize)}
}

// territoryIDAt renvoie la contrée du point (x, y) : le germe le plus proche parmi ceux des 3 × 3 cases voisines.
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

// familyOf renvoie les débuts et fins de radicaux de la contrée, sans échantillonner le terrain
// (utilisé pour nommer les lieux : c'est du hachage pur).
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

// TerritoryAt renvoie la contrée du point (x, y) (mise en cache).
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
	var sumTemp, sumWet float64 // climat réel moyen des points de terre (v2)
	var nLand int
	t.starts, t.ends = familyOf(w.Seed, id)
	rng := territoryRNG(w.Seed, id, 20)
	t.nameStart, t.nameEnd, t.variant = rng.IntN(familySize), rng.IntN(familySize), rng.IntN(2)

	// Terrain dominant : une grille de points autour du germe, restreinte à la contrée.
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
				// La haute mer ne nomme pas une contrée : seules ses côtes comptent.
			case Water, Sand:
				count[themeCoast]++ // la terre pèse double : une contrée n'est dite « côtière » que si l'eau domine vraiment
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
	t.Theme = themePlain // contrée presque entièrement marine : on la dit de plaine, à défaut
	best := 0
	for th := terrainTheme(0); th < numThemes; th++ {
		if count[th] > best {
			t.Theme, best = th, count[th]
		}
	}
	return t
}

// Stem est le radical qui donne son nom à la contrée.
func (t *Territory) Stem() string {
	return placeStart[t.starts[t.nameStart]] + placeEnd[t.ends[t.nameEnd]]
}

// NameIn compose le nom de la contrée dans la langue l : « Forêts d'Aube », « Forests of Aube ».
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

// TerritoryOfRegion renvoie la contrée du centre d'une région de la grille (celle qui nomme le lieu de cette région).
func (w *World) TerritoryOfRegion(rp RegionPos) *Territory {
	return w.TerritoryAt(rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2)
}

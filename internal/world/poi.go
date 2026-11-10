package world

import (
	"fmt"
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Points d'intérêt : de petits repères en plein air (feu de camp, sanctuaire, repaire, tour de guet, pierre dressée),
// une case chacun, au plus un par région. Comme les donjons, tout est tiré de la seed par des flux d'identité dédiés
// (jamais celui de la génération des plans) : serveur et client calculent les mêmes points sans rien s'échanger, et
// en ajouter ne déplace aucun lieu ni aucun donjon existant.

// POIKind est le type d'un point d'intérêt.
type POIKind uint8

const (
	POICampfire POIKind = iota + 1 // feu de camp : soigne et devient le point de réapparition
	POIShrine                      // sanctuaire : bonus temporaire
	POILair                        // repaire : monstres et coffre
	POITower                       // tour de guet : révèle les alentours
	POIStone                       // pierre dressée : réseau de téléportation
	POISpring                      // source (oasis en climat aride) : rend des PV et du mana
	POIRuin                        // ruines : un gardien, un coffre, un indice sur un donjon
)

const (
	poiChance = 0.6 // part des régions qui portent l'un des cinq premiers types
	// poiChanceAll borne la plage de tirage des types ajoutés ensuite (sources, ruines). Cette plage vient *après*
	// poiChance : les points déjà posés gardent leur case et leur type.
	poiChanceAll = 0.78
	poiSalt      = 8000
	poiKindSalt  = 8100
	poiSeedSalt  = 8200
	poiSpotSalt  = 8300
	poiNameSalt  = 8400
	poiMargin    = 12 // distance minimale du point au bord de sa région
	poiTries     = 48
	poiCacheMax  = 4096
	poiClear     = 2 // rayon de terrain praticable exigé autour du point
	poiDungeonKm = 4 // distance minimale à l'entrée d'un donjon
)

// poiWeights règle la fréquence de chaque type (somme 100).
var poiWeights = []struct {
	kind POIKind
	w    int
}{{POICampfire, 30}, {POIShrine, 20}, {POILair, 20}, {POITower, 15}, {POIStone, 15}}

// En v2, une région peut porter jusqu'à poiExtraMax points de plus que le principal, pour que le monde ne paraisse
// pas vide. Ils ont leurs propres flux d'identité (jamais ceux du point principal) et ne sont pas des pierres dressées :
// le réseau des pierres est indexé par région.
const (
	poiExtraMax  = 2
	poiExtraSalt = 3_000_000 // + 10 par point supplémentaire; au-delà de la plage des cases à creuser (digSalt + nonce)
	poiExtraGap  = 40        // distance minimale entre deux points d'une même région
)

// poiExtraChance est la probabilité de chaque point supplémentaire (le premier, puis le second).
var poiExtraChance = [poiExtraMax]float64{0.55, 0.3}

// poiExtraWeights règle la fréquence des types d'un point supplémentaire (somme 100).
var poiExtraWeights = []struct {
	kind POIKind
	w    int
}{{POICampfire, 25}, {POIShrine, 20}, {POILair, 20}, {POITower, 15}, {POISpring, 10}, {POIRuin, 10}}

// POIKey identifie un point pour l'état du jeu (délais, repaires): sa région et son indice dans la région.
type POIKey struct {
	Region RegionPos
	Index  int
}

// POI est un point d'intérêt posé dans une région.
type POI struct {
	Kind   POIKind
	Region RegionPos
	// Index vaut 0 pour le point principal de la région, 1 ou 2 pour un point supplémentaire (v2).
	Index int
	Pos   Pos
	Seed  int64 // graine propre au point (nom, tirages du jeu)
	// Slots est la liste des emplacements de monstres d'un repaire ou d'une ruine (vide pour les autres types).
	Slots []Pos

	stem stem
}

// ID est l'identifiant stable du point : « x,y » de sa région, suivi de « #k » pour un point supplémentaire.
func (p *POI) ID() string {
	if p.Index > 0 {
		return fmt.Sprintf("%d,%d#%d", p.Region.X, p.Region.Y, p.Index)
	}
	return fmt.Sprintf("%d,%d", p.Region.X, p.Region.Y)
}

// Key est la clé du point pour l'état du jeu.
func (p *POI) Key() POIKey { return POIKey{p.Region, p.Index} }

// Tile est la tuile que le point pose sur la carte.
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

// NameIn renvoie le nom du point dans la langue l (« Menhir de Valmor », « Sanctuaire de Valmor »…).
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

// POIOfRegion renvoie le point d'intérêt principal de la région rp, ou nil s'il n'y en a pas.
func (w *World) POIOfRegion(rp RegionPos) *POI {
	for _, p := range w.POIsOfRegion(rp) {
		if p.Index == 0 {
			return p
		}
	}
	return nil
}

// POIsOfRegion renvoie tous les points de la région rp: le principal d'abord, puis les supplémentaires (v2).
func (w *World) POIsOfRegion(rp RegionPos) []*POI {
	w.pmu.Lock()
	if l, ok := w.pois[rp]; ok {
		w.pmu.Unlock()
		return l
	}
	w.pmu.Unlock()

	var l []*POI
	if p := w.buildPOI(rp); p != nil { // hors verrou : touche aux structures
		l = append(l, p)
	}
	if w.GenVersion >= GenV2 {
		l = append(l, w.buildExtraPOIs(rp, l)...)
	}
	w.pmu.Lock()
	if len(w.pois) >= poiCacheMax {
		clear(w.pois)
	}
	w.pois[rp] = l
	w.pmu.Unlock()
	return l
}

// poiIn renvoie le point de la liste dont la tuile est (x, y), ou nil.
func poiIn(list []*POI, x, y int) *POI {
	for _, p := range list {
		if p.Pos.X == x && p.Pos.Y == y {
			return p
		}
	}
	return nil
}

// POIsWithin renvoie les points à au plus r cases (distance de Chebyshev) de (x, y).
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

// POIAt renvoie le point dont la tuile est exactement (x, y).
func (w *World) POIAt(x, y int) *POI {
	return poiIn(w.POIsOfRegion(RegionOf(x, y)), x, y)
}

// buildExtraPOIs tire les points supplémentaires de la région rp (v2), à distance des points déjà posés.
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

// newPOI assemble un point et ses emplacements de monstres.
func newPOI(kind POIKind, rp RegionPos, index int, pos Pos, seed int64, st stem) *POI {
	p := &POI{Kind: kind, Region: rp, Index: index, Pos: pos, Seed: seed, stem: st}
	switch kind {
	case POILair:
		p.Slots = []Pos{{pos.X - 2, pos.Y - 1}, {pos.X + 2, pos.Y - 1}, {pos.X, pos.Y + 2}}
	case POIRuin:
		p.Slots = []Pos{{pos.X, pos.Y + 2}} // un seul gardien
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
	if roll >= poiChance { // plage des types ajoutés après coup : 55 % de sources, 45 % de ruines
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

// poiSpot cherche la case du point : du terrain naturel praticable sur poiClear cases alentour, loin des lieux, du
// point de départ et de l'entrée d'un éventuel donjon. Comme pour les donjons, seul le terrain brut compte (pas de
// TileAt, trop coûteux à cause des routes) : un point peut donc se trouver sur une route.
func (w *World) poiSpot(rp RegionPos) (Pos, bool) { return w.poiSpotWith(rp, poiSpotSalt, nil) }

// poiSpotWith est poiSpot avec son propre flux d'identité (salt) et des positions à tenir à distance (avoid).
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

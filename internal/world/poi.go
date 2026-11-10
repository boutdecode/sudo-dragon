package world

import (
	"fmt"

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

// POI est un point d'intérêt posé dans une région.
type POI struct {
	Kind   POIKind
	Region RegionPos
	Pos    Pos
	Seed   int64 // graine propre au point (nom, tirages du jeu)
	// Slots est la liste des emplacements de monstres d'un repaire ou d'une ruine (vide pour les autres types).
	Slots []Pos

	stem stem
}

// ID est l'identifiant stable du point : « x,y » de sa région.
func (p *POI) ID() string { return fmt.Sprintf("%d,%d", p.Region.X, p.Region.Y) }

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

// POIOfRegion renvoie le point d'intérêt de la région rp, ou nil s'il n'y en a pas.
func (w *World) POIOfRegion(rp RegionPos) *POI {
	w.pmu.Lock()
	if p, ok := w.pois[rp]; ok {
		w.pmu.Unlock()
		return p
	}
	w.pmu.Unlock()

	p := w.buildPOI(rp) // hors verrou : touche aux structures
	w.pmu.Lock()
	if len(w.pois) >= poiCacheMax {
		clear(w.pois)
	}
	w.pois[rp] = p
	w.pmu.Unlock()
	return p
}

// POIsWithin renvoie les points à au plus r cases (distance de Chebyshev) de (x, y).
func (w *World) POIsWithin(x, y, r int) []*POI {
	lo, hi := RegionOf(x-r, y-r), RegionOf(x+r, y+r)
	var out []*POI
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			if p := w.POIOfRegion(RegionPos{rx, ry}); p != nil && max(abs(p.Pos.X-x), abs(p.Pos.Y-y)) <= r {
				out = append(out, p)
			}
		}
	}
	return out
}

// POIAt renvoie le point dont la tuile est exactement (x, y).
func (w *World) POIAt(x, y int) *POI {
	if p := w.POIOfRegion(RegionOf(x, y)); p != nil && p.Pos.X == x && p.Pos.Y == y {
		return p
	}
	return nil
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
	p := &POI{
		Kind:   kind,
		Region: rp,
		Pos:    pos,
		Seed:   int64(identityRNG(w.Seed, rp, poiSeedSalt).Uint64()),
		stem:   stem{nr.IntN(len(placeStart)), nr.IntN(len(placeEnd))},
	}
	switch kind {
	case POILair:
		p.Slots = []Pos{{pos.X - 2, pos.Y - 1}, {pos.X + 2, pos.Y - 1}, {pos.X, pos.Y + 2}}
	case POIRuin:
		p.Slots = []Pos{{pos.X, pos.Y + 2}} // un seul gardien
	}
	return p
}

// poiSpot cherche la case du point : du terrain naturel praticable sur poiClear cases alentour, loin des lieux, du
// point de départ et de l'entrée d'un éventuel donjon. Comme pour les donjons, seul le terrain brut compte (pas de
// TileAt, trop coûteux à cause des routes) : un point peut donc se trouver sur une route.
func (w *World) poiSpot(rp RegionPos) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, poiSpotSalt)
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
		if w.poiSpotOK(rp, x, y) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

func (w *World) poiSpotOK(rp RegionPos, x, y int) bool {
	for dy := -poiClear; dy <= poiClear; dy++ {
		for dx := -poiClear; dx <= poiClear; dx++ {
			switch w.terrainAt(x+dx, y+dy) {
			case Plain, Forest, Hills, Sand:
			default:
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

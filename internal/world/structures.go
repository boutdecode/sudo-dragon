package world

import (
	"math/rand/v2"
	"sort"
)

// RegionSize est le côté d'une région, en tuiles. Chaque région contient au
// plus une structure. C'est un multiple de ChunkSize: un chunk n'appartient
// jamais à deux régions.
const RegionSize = ChunkSize * 8

// regionMargin sépare l'emprise d'une structure du bord de sa région. Il doit
// rester supérieur à la marge de la zone sûre (safeMargin) pour qu'une zone
// sûre ne déborde jamais sur la région voisine.
const regionMargin = 6

// safeMargin élargit l'emprise d'un village ou d'une ville pour former sa zone sûre.
const safeMargin = 3

// startClearance est la distance minimale entre le point de départ des
// joueurs et l'emprise de n'importe quelle structure.
const startClearance = 16

// RegionPos identifie une région dans la grille de régions.
type RegionPos struct{ X, Y int }

// RegionOf renvoie la région contenant la tuile (x, y).
func RegionOf(x, y int) RegionPos {
	return RegionPos{floorDiv(x, RegionSize), floorDiv(y, RegionSize)}
}

// Kind est le type d'une structure.
type Kind uint8

const (
	KindVillage Kind = iota + 1
	KindTown
	KindFortress
)

// String renvoie l'identifiant du type (utilisé dans les journaux et outils).
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

// NPCKind est le type d'un personnage non joueur.
type NPCKind string

const (
	NPCMerchant NPCKind = "merchant" // achète et vend des potions
	NPCHealer   NPCKind = "healer"   // soigne contre de l'or
)

// NPCKinds liste les types de PNJ que le générateur peut placer (pour vérifier
// que le client sait tous les dessiner).
func NPCKinds() []NPCKind { return []NPCKind{NPCMerchant, NPCHealer} }

// NPC est un personnage immobile placé par le générateur. Il est calculé à
// l'identique par le serveur et par le client à partir de la seed.
type NPC struct {
	Kind NPCKind
	Pos  Pos
}

// SlotRole précise quel monstre occupe un emplacement de forteresse.
type SlotRole uint8

const (
	RoleGuard SlotRole = iota + 1 // soldat de garnison
	RoleElite                     // soldat d'élite, gardien du donjon
	RoleBoss                      // seigneur de la forteresse
)

// Slot est un emplacement où une forteresse fait apparaître un monstre.
type Slot struct {
	Pos  Pos
	Role SlotRole
}

// Structure est un village, une ville ou une forteresse.
type Structure struct {
	Kind   Kind
	Region RegionPos
	Origin Pos // coin haut-gauche de l'emprise
	W, H   int // dimensions de l'emprise
	NPCs   []NPC
	Slots  []Slot

	tiles []Tile // W*H, ligne par ligne
}

// Contains indique si la tuile (x, y) est dans l'emprise.
func (s *Structure) Contains(x, y int) bool {
	return x >= s.Origin.X && x < s.Origin.X+s.W && y >= s.Origin.Y && y < s.Origin.Y+s.H
}

// TileAt renvoie la tuile de la structure en (x, y), qui doit être dans l'emprise.
func (s *Structure) TileAt(x, y int) Tile {
	return s.tiles[(y-s.Origin.Y)*s.W+(x-s.Origin.X)]
}

// Center renvoie la tuile centrale de l'emprise.
func (s *Structure) Center() Pos {
	return Pos{s.Origin.X + s.W/2, s.Origin.Y + s.H/2}
}

// Safe indique si (x, y) est dans la zone sûre de la structure (emprise + marge).
// Seuls les villages et les villes ont une zone sûre.
func (s *Structure) Safe(x, y int) bool {
	if s.Kind != KindVillage && s.Kind != KindTown {
		return false
	}
	return x >= s.Origin.X-safeMargin && x < s.Origin.X+s.W+safeMargin &&
		y >= s.Origin.Y-safeMargin && y < s.Origin.Y+s.H+safeMargin
}

// SlotKey identifie de façon stable un emplacement de forteresse.
type SlotKey struct {
	Region RegionPos
	Index  int
}

// Key renvoie la clé de l'emplacement i de la structure.
func (s *Structure) Key(i int) SlotKey { return SlotKey{s.Region, i} }

// Dimensions des emprises (marge de terrain dégagé comprise).
var kindSize = map[Kind][2]int{
	KindVillage:  {32, 26},
	KindTown:     {58, 46},
	KindFortress: {48, 40},
}

// structureCacheMax borne la mémoire du cache de structures; au-delà, il est
// vidé et se reconstruit (la génération est déterministe).
const structureCacheMax = 4096

// spawnAnchor renvoie le point de départ « terrain brut »: la première plaine
// ou forêt en spirale autour de l'origine. Il ne dépend d'aucune structure,
// ce qui évite une dépendance circulaire avec leur placement.
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

// structureForRegion renvoie la structure de la région, ou nil. Le résultat est mis en cache.
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

// buildRegion décide du contenu d'une région.
func (w *World) buildRegion(rp RegionPos) *Structure {
	rng := regionRNG(w.Seed, rp)

	// La région du point de départ reçoit un village: un nouveau joueur doit
	// en trouver un près de lui.
	if anchor := w.spawnAnchor(); RegionOf(anchor.X, anchor.Y) == rp {
		if s := w.buildStartVillage(rp, rng, anchor); s != nil {
			return s
		}
	}

	if rng.Float64() >= 0.55 { // pas de structure ici
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

// buildStartVillage place le village de départ au meilleur site de la région
// le plus proche du point de départ. Renvoie nil si la région n'offre aucun
// site convenable (île, montagnes…): le placement ordinaire prend alors le relais.
func (w *World) buildStartVillage(rp RegionPos, rng *rand.Rand, anchor Pos) *Structure {
	size := kindSize[KindVillage]

	// Sites candidats, du plus proche du point de départ au plus lointain.
	// On s'arrête au premier convenable: évaluer le terrain sous chaque site
	// coûte cher (près de 300 000 évaluations de bruit pour tous), et les
	// premiers candidats suffisent presque toujours. Le résultat est le même
	// que de comparer tous les sites: à distance égale, le premier dans l'ordre
	// de balayage l'emporte (tri stable).
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

// nearAnchor indique si l'emprise, élargie de startClearance, contient le point de départ.
func (w *World) nearAnchor(ox, oy, W, H int, anchor Pos) bool {
	return anchor.X >= ox-startClearance && anchor.X < ox+W+startClearance &&
		anchor.Y >= oy-startClearance && anchor.Y < oy+H+startClearance
}

// chooseSite tire des sites candidats dans la région et garde le meilleur.
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

// siteQuality échantillonne le terrain brut sous l'emprise (un point sur trois).
// flat est la part de terrain praticable et plat (ni eau, ni montagne, ni neige);
// pref est l'adéquation moyenne au type de structure.
func (w *World) siteQuality(kind Kind, ox, oy, W, H int) (flat, pref float64) {
	var n, nFlat int
	var sumPref float64
	for dy := 0; dy < H; dy += 3 {
		for dx := 0; dx < W; dx += 3 {
			n++
			t := w.terrainAt(ox+dx, oy+dy)
			switch t {
			case Plain, Forest, Sand, Hills:
				nFlat++
				sumPref += preference(kind, t)
			}
		}
	}
	return float64(nFlat) / float64(n), sumPref / float64(n)
}

// preference note de 0 à 1 l'adéquation d'un terrain à un type de structure.
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

// waterNearby indique s'il y a de l'eau dans l'une des huit directions autour de (x, y).
func (w *World) waterNearby(x, y int) bool {
	const d = 28
	for _, v := range [8][2]int{{d, 0}, {-d, 0}, {0, d}, {0, -d}, {d, d}, {d, -d}, {-d, d}, {-d, -d}} {
		if t := w.terrainAt(x+v[0], y+v[1]); t == Water || t == DeepWater {
			return true
		}
	}
	return false
}

// generate dessine la structure et la place à (x0, y0) dans la région.
func (w *World) generate(rp RegionPos, kind Kind, x0, y0 int, rng *rand.Rand) *Structure {
	size := kindSize[kind]
	var c *canvas
	switch kind {
	case KindVillage:
		c = layoutVillage(size[0], size[1], rng)
	case KindTown:
		c = layoutTown(size[0], size[1], rng)
	default:
		c = layoutFortress(size[0], size[1], rng)
	}
	origin := Pos{rp.X*RegionSize + x0, rp.Y*RegionSize + y0}
	s := &Structure{Kind: kind, Region: rp, Origin: origin, W: c.w, H: c.h, tiles: c.t}
	for _, n := range c.npcs {
		s.NPCs = append(s.NPCs, NPC{n.Kind, Pos{origin.X + n.Pos.X, origin.Y + n.Pos.Y}})
	}
	for _, sl := range c.slots {
		s.Slots = append(s.Slots, Slot{Pos{origin.X + sl.Pos.X, origin.Y + sl.Pos.Y}, sl.Role})
	}
	return s
}

// StructureAt renvoie la structure dont l'emprise contient (x, y), ou nil.
func (w *World) StructureAt(x, y int) *Structure {
	s := w.structureForRegion(RegionOf(x, y))
	if s != nil && s.Contains(x, y) {
		return s
	}
	return nil
}

// StructuresNear renvoie les structures dont l'emprise est à moins de radius
// tuiles de p (en distance de Chebyshev), triées de la plus proche à la plus lointaine.
func (w *World) StructuresNear(p Pos, radius int) []*Structure {
	lo, hi := RegionOf(p.X-radius, p.Y-radius), RegionOf(p.X+radius, p.Y+radius)
	var out []*Structure
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			s := w.structureForRegion(RegionPos{rx, ry})
			if s == nil {
				continue
			}
			// Distance de p au rectangle de l'emprise.
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

// Safe indique si (x, y) est dans la zone sûre d'un village ou d'une ville:
// les monstres n'y apparaissent ni n'y entrent.
func (w *World) Safe(x, y int) bool {
	s := w.structureForRegion(RegionOf(x, y))
	return s != nil && s.Safe(x, y)
}

// NPCAt renvoie le PNJ posté en (x, y), s'il y en a un.
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

// NPCsIn renvoie les PNJ situés dans le rectangle [x0, x1] × [y0, y1]. Une
// seule recherche par image suffit au rendu, au lieu d'une par tuile.
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

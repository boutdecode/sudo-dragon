package world

import (
	"fmt"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Donjons : une entrée secrète sur la carte principale mène à une carte à part, générée
// d'après la seed du donjon. Tout est déterministe : le serveur et le client calculent les
// mêmes donjons et les mêmes plans à partir de la seed du monde, sans rien s'échanger.
//
// Les tirages viennent de flux d'identité dédiés (identityRNG avec un sel propre), jamais du
// flux de génération des plans : ajouter des donjons ne modifie aucun lieu existant.

const (
	dungeonChance     = 0.2 // part des régions qui cachent un donjon
	dungeonSalt       = 7000
	dungeonNameSalt   = 7100
	dungeonSeedSalt   = 7200
	dungeonSpotSalt   = 7300
	dungeonMargin     = 16 // distance minimale de l'entrée au bord de sa région
	dungeonTries      = 48 // sites d'entrée essayés avant d'abandonner
	dungeonClash      = 3  // rayon (en régions) dans lequel deux donjons n'ont pas le même nom
	dungeonMaxTier    = 5
	dungeonCacheMax   = 4096
	numDungeonNames   = 8
	numDungeonRerolls = 8
)

// Dungeon est un donjon caché dans une région.
type Dungeon struct {
	ID       string    // identifiant unique et stable, « x,y » de sa région
	Name     string    // nom en français (voir NameIn pour les autres langues)
	Region   RegionPos // région qui le cache
	Entrance Pos       // case de la carte principale qui y mène
	Seed     int64     // seed du plan (voir NewDungeonMap)
	Tier     int       // difficulté, de 1 à dungeonMaxTier : croît avec l'éloignement de l'origine

	nameIdx int
	stem    stem
}

// NameIn renvoie le nom du donjon dans la langue l.
func (d *Dungeon) NameIn(l i18n.Lang) string {
	return dungeonName(l, d.nameIdx, d.stem)
}

func dungeonName(l i18n.Lang, idx int, s stem) string {
	return fmt.Sprintf(i18n.T(l, fmt.Sprintf("dungeon.name.%d", idx)), s.String())
}

// DungeonOfRegion renvoie le donjon caché dans la région rp, ou nil s'il n'y en a pas.
func (w *World) DungeonOfRegion(rp RegionPos) *Dungeon {
	w.dmu.Lock()
	if d, ok := w.dungeons[rp]; ok {
		w.dmu.Unlock()
		return d
	}
	w.dmu.Unlock()

	d := w.buildDungeon(rp) // hors verrou : touche aux structures et aux routes
	w.dmu.Lock()
	if len(w.dungeons) >= dungeonCacheMax {
		clear(w.dungeons)
	}
	w.dungeons[rp] = d
	w.dmu.Unlock()
	return d
}

// DungeonByID retrouve un donjon d'après son identifiant.
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

// DungeonsNear renvoie les donjons des régions situées à au plus radius régions de la tuile (x, y).
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

// DungeonsWithin renvoie les donjons dont l'entrée est à au plus r cases (distance de Chebyshev) de (x, y). Il ne
// regarde que les régions que ce carré touche: c'est la recherche à faire à chaque pas d'un joueur.
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

// DungeonAt renvoie le donjon dont l'entrée est exactement sur la tuile (x, y).
func (w *World) DungeonAt(x, y int) *Dungeon {
	d := w.DungeonOfRegion(RegionOf(x, y))
	if d != nil && d.Entrance.X == x && d.Entrance.Y == y {
		return d
	}
	return nil
}

// mayHideDungeon dit si le tirage de la région lui promet un donjon. Il ne dépend que de la
// seed (pas du terrain), pour pouvoir comparer les noms des voisines sans générer de plan.
func mayHideDungeon(seed int64, rp RegionPos) bool {
	return identityRNG(seed, rp, dungeonSalt).Float64() < dungeonChance
}

// rawDungeonName est le nom tiré pour (région, essai), sans souci des voisines.
func rawDungeonName(seed int64, rp RegionPos, attempt int) (int, stem) {
	rng := identityRNG(seed, rp, dungeonNameSalt+uint64(attempt))
	return rng.IntN(numDungeonNames), stem{rng.IntN(len(placeStart)), rng.IntN(len(placeEnd))}
}

// uniqueDungeonName choisit le nom de la région rp : le premier essai que ne porte aucun donjon
// voisin (rayon dungeonClash) déjà nommé avant elle dans l'ordre Y puis X. Comme pour les lieux,
// seuls les tirages bruts des voisines comptent, ce qui évite toute récursion.
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

// dungeonTier croît avec le niveau de la région (voir LevelAt), mesuré en son centre.
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

// dungeonEntrance cherche une case d'entrée : du terrain naturel praticable, loin des lieux, de l'eau et du point
// de départ des joueurs.
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

// dungeonSpotOK : la case et ses huit voisines sont du terrain naturel praticable (ni eau, ni montagne), et loin du
// lieu de la région. Seuls le terrain brut et l'emprise du lieu comptent: chercher aussi les routes (TileAt) fait
// tracer celles de toutes les régions voisines, ce qui coûtait environ 10 ms par donjon et gelait la boucle de jeu
// quand un PNJ cherchait les donjons de 49 régions d'un coup. Une entrée peut donc se trouver sur une route.
func (w *World) dungeonSpotOK(rp RegionPos, x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			switch w.terrainAt(x+dx, y+dy) {
			case Plain, Forest, Hills, Sand:
			default:
				return false
			}
		}
	}
	// Un lieu ne sort jamais de sa région (regionMargin): seule celle de l'entrée est à vérifier. On garde une marge
	// qui dépasse la zone sûre.
	const clear = safeMargin + 2
	if s := w.structureForRegion(rp); s != nil &&
		x >= s.Origin.X-clear && x < s.Origin.X+s.W+clear && y >= s.Origin.Y-clear && y < s.Origin.Y+s.H+clear {
		return false
	}
	return true
}

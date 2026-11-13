package world

import "github.com/kevin/ascii-adventure/internal/resource"

// Nœuds de ressources (GenV3): arbres, filons et plantes posés sur le terrain naturel. Ils sont une pure fonction
// de (graine, x, y): serveur et client les calculent pareillement. Un nœud ne recouvre jamais une structure, un
// point d'intérêt, une route, l'abord du point de départ ni une entrée de donjon.

// resourceSalt distingue le hachage des nœuds de tout autre tirage.
const resourceSalt = 0x5E50C0DE

// nodeCategory donne, pour un terrain et un tirage u dans [0, 1000), la catégorie du nœud posé (ou false).
func nodeCategory(t Tile, u int) (resource.Category, bool) {
	switch t {
	case Forest:
		if u < 20 {
			return resource.Wood, true
		}
	case Jungle:
		if u < 18 {
			return resource.Wood, true
		}
	case Plain:
		switch {
		case u < 3:
			return resource.Wood, true
		case u < 15:
			return resource.Herb, true
		}
	case Hills:
		if u < 12 {
			return resource.Ore, true
		}
	case Tundra:
		if u < 4 {
			return resource.Ore, true
		}
	case Desert:
		if u < 4 {
			return resource.Ore, true
		}
	case River:
		if u < 25 {
			return resource.Fish, true
		}
	case Sand:
		if u < 2 {
			return resource.Ore, true
		}
	}
	return 0, false
}

// nodeHash est le hachage de la case (x, y), propre aux nœuds.
func (w *World) nodeHash(x, y int) uint64 {
	h := mix64(uint64(w.Seed) ^ resourceSalt)
	h = mix64(h + uint64(int64(x)))
	return mix64(h + uint64(int64(y)))
}

// nodeAt renvoie la catégorie, la ressource et la tuile du nœud posé sur le terrain brut t en (x, y).
// Les exclusions (structure, POI, route, spawn, donjon) sont l'affaire de nodeTile.
func (w *World) nodeAt(x, y int, t Tile) (resource.Category, resource.Kind, bool) {
	if w.GenVersion < GenV3 {
		return 0, "", false
	}
	h := w.nodeHash(x, y)
	cat, ok := nodeCategory(t, int(h%1000))
	if !ok {
		return 0, "", false
	}
	a := w.spawnAnchor()
	if abs(x-a.X) <= startClearance && abs(y-a.Y) <= startClearance {
		return 0, "", false
	}
	if w.DungeonAt(x, y) != nil {
		return 0, "", false
	}
	kind := resource.Pick(cat, int(w.ClimateAt(x, y)), LevelAt(x, y), h>>16)
	return cat, kind, true
}

// nodeTile est la tuile de nœud qui remplace le terrain t en (x, y), s'il y en a un. L'appelant a déjà écarté les
// structures, les POI et les routes.
func (w *World) nodeTile(x, y int, t Tile) (Tile, bool) {
	cat, _, ok := w.nodeAt(x, y, t)
	if !ok {
		return 0, false
	}
	return nodeTileOf(cat), true
}

func nodeTileOf(c resource.Category) Tile {
	switch c {
	case resource.Ore:
		return OreNode
	case resource.Herb:
		return HerbNode
	case resource.Fish:
		return FishNode
	}
	return TreeNode
}

// ResourceAt renvoie la ressource récoltable sur la case (x, y): celle d'un nœud visible (TileAt/At donnent
// TreeNode, OreNode, HerbNode ou FishNode). Faux si la case n'en porte pas.
func (w *World) ResourceAt(x, y int) (resource.Kind, bool) {
	if w.GenVersion < GenV3 {
		return "", false
	}
	switch w.At(x, y) {
	case TreeNode, OreNode, HerbNode, FishNode:
	default:
		return "", false
	}
	_, kind, ok := w.nodeAt(x, y, w.terrainAt(x, y))
	return kind, ok
}

// NodeAt décrit le nœud de ressource visible en (x, y): sa ressource et le terrain naturel qu'il recouvre (dont le
// client reprend le glyphe, seule la couleur dit la ressource). Faux si la case ne porte pas de nœud.
func (w *World) NodeAt(x, y int) (resource.Kind, Tile, bool) {
	switch w.At(x, y) {
	case TreeNode, OreNode, HerbNode, FishNode:
		base := w.terrainAt(x, y)
		if _, kind, ok := w.nodeAt(x, y, base); ok {
			return kind, base, true
		}
	}
	return "", 0, false
}

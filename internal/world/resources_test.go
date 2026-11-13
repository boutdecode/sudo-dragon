package world

import "testing"

func TestNodesOnlyInV3(t *testing.T) {
	for _, gen := range []int{GenV1, GenV2} {
		w := NewVersion(5, gen)
		for y := -60; y < 60; y++ {
			for x := -60; x < 60; x++ {
				switch w.At(x, y) {
				case TreeNode, OreNode, HerbNode:
					t.Fatalf("nœud en v%d à (%d,%d)", gen, x, y)
				}
			}
		}
	}
}

// Les nœuds ne changent pas le terrain brut: la v3 pose des nœuds sur le même terrain que la v2.
func TestV3NodesSitOnV2Terrain(t *testing.T) {
	v2, v3 := NewVersion(9, GenV2), NewVersion(9, GenV3)
	nodes := 0
	for y := -200; y < 200; y++ {
		for x := -200; x < 200; x++ {
			if v2.Terrain(x, y) != v3.Terrain(x, y) {
				t.Fatalf("terrain brut différent en (%d,%d)", x, y)
			}
			switch v3.At(x, y) {
			case TreeNode, OreNode, HerbNode:
				nodes++
				if !v2.At(x, y).Walkable() && v2.At(x, y) != Wall {
					t.Fatalf("nœud sur une case bloquante en (%d,%d)", x, y)
				}
			}
		}
	}
	if nodes < 200 {
		t.Errorf("seulement %d nœuds sur 160 000 cases", nodes)
	}
}

func TestNodesAreCoherentAndExcluded(t *testing.T) {
	w := NewVersion(3, GenV3)
	a := w.FindSpawn()
	for y := -250; y < 250; y++ {
		for x := -250; x < 250; x++ {
			tile := w.TileAt(x, y)
			if tile != w.At(x, y) {
				t.Fatalf("TileAt et At divergent en (%d,%d)", x, y)
			}
			k, ok := w.ResourceAt(x, y)
			isNode := tile == TreeNode || tile == OreNode || tile == HerbNode
			if ok != isNode {
				t.Fatalf("ResourceAt=%v mais tuile %d en (%d,%d)", ok, tile, x, y)
			}
			if !isNode {
				continue
			}
			if k == "" {
				t.Fatalf("nœud sans ressource en (%d,%d)", x, y)
			}
			if abs(x-a.X) <= startClearance && abs(y-a.Y) <= startClearance {
				t.Fatalf("nœud près du spawn en (%d,%d)", x, y)
			}
			if w.DungeonAt(x, y) != nil || w.POIAt(x, y) != nil || w.StructureAt(x, y) != nil {
				t.Fatalf("nœud sur un lieu en (%d,%d)", x, y)
			}
		}
	}
}

func TestNodesAreDeterministic(t *testing.T) {
	a, b := NewVersion(11, GenV3), NewVersion(11, GenV3)
	for y := -80; y < 80; y++ {
		for x := -80; x < 80; x++ {
			ka, oka := a.ResourceAt(x, y)
			kb, okb := b.ResourceAt(x, y)
			if ka != kb || oka != okb {
				t.Fatalf("divergence en (%d,%d)", x, y)
			}
		}
	}
}

// Les plantes ne poussent qu'en plaine; les arbres et les filons ne se cachent pas dans les prairies.
func TestHerbsOnlyOnPlains(t *testing.T) {
	w := NewVersion(3, GenV3)
	herbs := 0
	for y := -250; y < 250; y++ {
		for x := -250; x < 250; x++ {
			if w.At(x, y) != HerbNode {
				continue
			}
			herbs++
			if b := w.Terrain(x, y); b != Plain {
				t.Fatalf("plante sur %d en (%d,%d)", b, x, y)
			}
		}
	}
	if herbs == 0 {
		t.Error("aucune plante sur 250 000 cases")
	}
}

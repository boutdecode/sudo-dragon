package world

import "testing"

func TestNodesOnlyInV3(t *testing.T) {
	for _, gen := range []int{GenV1, GenV2} {
		w := NewVersion(5, gen)
		for y := -60; y < 60; y++ {
			for x := -60; x < 60; x++ {
				switch w.At(x, y) {
				case TreeNode, OreNode, HerbNode, FishNode:
					t.Fatalf("node in v%d at (%d,%d)", gen, x, y)
				}
			}
		}
	}
}

// Nodes do not change the raw terrain: v3 places nodes on the same terrain as v2.
func TestV3NodesSitOnV2Terrain(t *testing.T) {
	v2, v3 := NewVersion(9, GenV2), NewVersion(9, GenV3)
	nodes := 0
	for y := -200; y < 200; y++ {
		for x := -200; x < 200; x++ {
			if v2.Terrain(x, y) != v3.Terrain(x, y) {
				t.Fatalf("different raw terrain at (%d,%d)", x, y)
			}
			switch v3.At(x, y) {
			case TreeNode, OreNode, HerbNode, FishNode:
				nodes++
				if !v2.At(x, y).Walkable() && v2.At(x, y) != Wall {
					t.Fatalf("node on a blocking tile at (%d,%d)", x, y)
				}
			}
		}
	}
	if nodes < 200 {
		t.Errorf("only %d nodes over 160,000 tiles", nodes)
	}
}

func TestNodesAreCoherentAndExcluded(t *testing.T) {
	w := NewVersion(3, GenV3)
	a := w.FindSpawn()
	for y := -250; y < 250; y++ {
		for x := -250; x < 250; x++ {
			tile := w.TileAt(x, y)
			if tile != w.At(x, y) {
				t.Fatalf("TileAt and At diverge at (%d,%d)", x, y)
			}
			k, ok := w.ResourceAt(x, y)
			isNode := tile == TreeNode || tile == OreNode || tile == HerbNode || tile == FishNode
			if ok != isNode {
				t.Fatalf("ResourceAt=%v but tile %d at (%d,%d)", ok, tile, x, y)
			}
			if !isNode {
				continue
			}
			if k == "" {
				t.Fatalf("node without resource at (%d,%d)", x, y)
			}
			if abs(x-a.X) <= startClearance && abs(y-a.Y) <= startClearance {
				t.Fatalf("node near the spawn at (%d,%d)", x, y)
			}
			if w.DungeonAt(x, y) != nil || w.POIAt(x, y) != nil || w.StructureAt(x, y) != nil {
				t.Fatalf("node on a place at (%d,%d)", x, y)
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
				t.Fatalf("divergence at (%d,%d)", x, y)
			}
		}
	}
}

// Plants only grow on plains; trees and veins do not hide in the meadows.
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
				t.Fatalf("plant on %d at (%d,%d)", b, x, y)
			}
		}
	}
	if herbs == 0 {
		t.Error("no plant over 250,000 tiles")
	}
}

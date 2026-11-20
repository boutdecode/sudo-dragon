package world

import "testing"

func TestNodesAreCoherentAndExcluded(t *testing.T) {
	t.Parallel()
	w := New(3)
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
	t.Parallel()
	a, b := New(11), New(11)
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
	t.Parallel()
	w := New(3)
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

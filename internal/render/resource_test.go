package render

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestEveryResourceHasDistinctColour(t *testing.T) {
	seen := map[string]resource.Kind{}
	for _, r := range resource.All() {
		hex, ok := resourceHex[r.Kind]
		if !ok {
			t.Errorf("%s without a colour", r.Kind)
			continue
		}
		if _, ok := resourceStyle[r.Kind]; !ok {
			t.Errorf("%s without a style", r.Kind)
		}
		if other, dup := seen[hex]; dup {
			t.Errorf("%s and %s have the same colour %s", r.Kind, other, hex)
		}
		seen[hex] = r.Kind
	}
}

// A node keeps the glyph of its terrain: neither a, nor 0, nor h.
func TestNodeKeepsTerrainGlyph(t *testing.T) {
	w := testWorld(1)
	for y := -150; y < 150; y++ {
		for x := -150; x < 150; x++ {
			if w.At(x, y) != world.TreeNode {
				continue
			}
			kind, base, _ := w.NodeAt(x, y)
			line := plain(Viewport(w, x, y, 3, 3, nil, nil, 0))[1]
			want := glyphIn(w.ClimateAt(x, y), base, x, y)
			if rune(line[1]) != want {
				t.Fatalf("tree %s at (%d,%d): glyph %q, expected %q", kind, x, y, line[1], want)
			}
			return
		}
	}
	t.Skip("no tree near the origin")
}

// A fish shoal keeps the glyph of the river that carries it.
func TestFishNodeKeepsRiverGlyph(t *testing.T) {
	w := testWorld(1)
	for y := -300; y < 300; y++ {
		for x := -300; x < 300; x++ {
			if w.At(x, y) != world.FishNode {
				continue
			}
			kind, base, _ := w.NodeAt(x, y)
			if base != world.River {
				t.Fatalf("shoal %s on %d", kind, base)
			}
			line := plain(Viewport(w, x, y, 3, 3, nil, nil, 0))[1]
			if want := glyphIn(w.ClimateAt(x, y), world.River, x, y); rune(line[1]) != want {
				t.Fatalf("glyph %q, expected %q", line[1], want)
			}
			return
		}
	}
	t.Skip("no shoal near the origin")
}

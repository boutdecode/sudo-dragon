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
			t.Errorf("%s sans couleur", r.Kind)
			continue
		}
		if _, ok := resourceStyle[r.Kind]; !ok {
			t.Errorf("%s sans style", r.Kind)
		}
		if other, dup := seen[hex]; dup {
			t.Errorf("%s et %s ont la même couleur %s", r.Kind, other, hex)
		}
		seen[hex] = r.Kind
	}
}

// Un nœud garde le glyphe de son terrain: ni a, ni 0, ni h.
func TestNodeKeepsTerrainGlyph(t *testing.T) {
	w := world.NewVersion(1, world.GenV3)
	for y := -150; y < 150; y++ {
		for x := -150; x < 150; x++ {
			if w.At(x, y) != world.TreeNode {
				continue
			}
			kind, base, _ := w.NodeAt(x, y)
			line := plain(Viewport(w, x, y, 3, 3, nil, nil, 0))[1]
			want := glyphIn(w.ClimateAt(x, y), base, x, y)
			if rune(line[1]) != want {
				t.Fatalf("arbre %s en (%d,%d): glyphe %q, attendu %q", kind, x, y, line[1], want)
			}
			return
		}
	}
	t.Skip("aucun arbre près de l'origine")
}

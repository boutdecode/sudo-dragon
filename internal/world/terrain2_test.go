package world

import "testing"

func TestV2IsDeterministicAndDiffersFromV1(t *testing.T) {
	a, b, v1 := NewVersion(42, GenV2), NewVersion(42, GenV2), NewVersion(42, GenV1)
	diff := 0
	for y := -300; y < 300; y += 7 {
		for x := -300; x < 300; x += 7 {
			if a.terrainAt(x, y) != b.terrainAt(x, y) {
				t.Fatalf("terrain v2 non déterministe en (%d,%d)", x, y)
			}
			if a.terrainAt(x, y) != v1.terrainAt(x, y) {
				diff++
			}
		}
	}
	if diff == 0 {
		t.Error("la v2 produit le même terrain que la v1")
	}
}

func TestUnknownGenVersionFallsBackToV1(t *testing.T) {
	for _, g := range []int{0, -1, 99} {
		if w := NewVersion(1, g); w.GenVersion != GenV1 {
			t.Errorf("gen %d: version %d, attendu %d", g, w.GenVersion, GenV1)
		}
	}
}

func TestV2SpawnIsWalkable(t *testing.T) {
	for seed := int64(1); seed <= 30; seed++ {
		w := NewVersion(seed, GenV2)
		if p := w.FindSpawn(); !w.At(p.X, p.Y).Walkable() {
			t.Errorf("seed %d: spawn %v non praticable (%c)", seed, p, w.At(p.X, p.Y).Glyph())
		}
	}
}

// Le monde v2 doit mêler océans, terres, montagnes et rivières, à l'échelle d'un grand territoire.
func TestV2HasContinentsRangesAndRivers(t *testing.T) {
	counts := map[Tile]int{}
	total := 0
	for seed := int64(1); seed <= 6; seed++ {
		w := NewVersion(seed, GenV2)
		for y := -4000; y < 4000; y += 25 {
			for x := -4000; x < 4000; x += 25 {
				counts[w.terrainAt(x, y)]++
				total++
			}
		}
	}
	water := counts[DeepWater] + counts[Water]
	if f := float64(water) / float64(total); f < 0.25 || f > 0.8 {
		t.Errorf("part d'eau %.0f%%, attendu entre 25 et 80%%", 100*f)
	}
	for _, tile := range []Tile{DeepWater, Sand, Plain, Forest, Hills, Mountain, Snow, River, Tundra, Desert, Swamp, Jungle} {
		if counts[tile] == 0 {
			t.Errorf("aucune tuile %c dans l'échantillon", tile.Glyph())
		}
	}
	if !River.Walkable() {
		t.Error("une rivière se traverse à gué")
	}
}

// Le terrain v2 ne doit pas paraître vide: sur un écran (~100×35 cases) pris sur la terre ferme, on doit voir en
// moyenne au moins trois types de terrain différents.
func TestV2ScreensAreVaried(t *testing.T) {
	var windows, kinds int
	for seed := int64(1); seed <= 4; seed++ {
		w := NewVersion(seed, GenV2)
		for cy := -3000; cy <= 3000; cy += 250 {
			for cx := -3000; cx <= 3000; cx += 250 {
				if e := w.elev2(cx, cy); e < 0.2 {
					continue // on veut des écrans à l'intérieur des terres
				}
				seen := map[Tile]bool{}
				for y := cy - 17; y < cy+18; y += 3 {
					for x := cx - 50; x < cx+50; x += 3 {
						seen[w.terrainAt(x, y)] = true
					}
				}
				windows++
				kinds += len(seen)
			}
		}
	}
	if windows < 50 {
		t.Fatalf("seulement %d écrans de terre échantillonnés", windows)
	}
	if avg := float64(kinds) / float64(windows); avg < 3 {
		t.Errorf("%.2f types de terrain par écran en moyenne, attendu au moins 3", avg)
	}
}

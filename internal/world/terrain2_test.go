package world

import "testing"

func TestV2IsDeterministicAndDiffersFromV1(t *testing.T) {
	a, b, v1 := NewVersion(42, GenV2), NewVersion(42, GenV2), NewVersion(42, GenV1)
	diff := 0
	for y := -300; y < 300; y += 7 {
		for x := -300; x < 300; x += 7 {
			if a.terrainAt(x, y) != b.terrainAt(x, y) {
				t.Fatalf("v2 terrain not deterministic at (%d,%d)", x, y)
			}
			if a.terrainAt(x, y) != v1.terrainAt(x, y) {
				diff++
			}
		}
	}
	if diff == 0 {
		t.Error("v2 produces the same terrain as v1")
	}
}

func TestUnknownGenVersionFallsBackToV1(t *testing.T) {
	for _, g := range []int{0, -1, 99} {
		if w := NewVersion(1, g); w.GenVersion != GenV1 {
			t.Errorf("gen %d: version %d, want %d", g, w.GenVersion, GenV1)
		}
	}
}

func TestV2SpawnIsWalkable(t *testing.T) {
	for seed := int64(1); seed <= 30; seed++ {
		w := NewVersion(seed, GenV2)
		if p := w.FindSpawn(); !w.At(p.X, p.Y).Walkable() {
			t.Errorf("seed %d: spawn %v not walkable (%c)", seed, p, w.At(p.X, p.Y).Glyph())
		}
	}
}

// The v2 world must mix oceans, land, mountains and rivers, at the scale of a large territory.
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
		t.Errorf("water share %.0f%%, want between 25 and 80%%", 100*f)
	}
	for _, tile := range []Tile{DeepWater, Sand, Plain, Forest, Hills, Mountain, Snow, River, Tundra, Desert, Swamp, Jungle} {
		if counts[tile] == 0 {
			t.Errorf("no %c tile in the sample", tile.Glyph())
		}
	}
	if !River.Walkable() {
		t.Error("a river can be forded")
	}
}

// The v2 terrain must not look empty: on a screen (~100x35 tiles) taken on dry land, one must see on
// average at least three different terrain types.
func TestV2ScreensAreVaried(t *testing.T) {
	var windows, kinds int
	for seed := int64(1); seed <= 4; seed++ {
		w := NewVersion(seed, GenV2)
		for cy := -3000; cy <= 3000; cy += 250 {
			for cx := -3000; cx <= 3000; cx += 250 {
				if e := w.elev2(cx, cy); e < 0.2 {
					continue // we want screens inland
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
		t.Fatalf("only %d land screens sampled", windows)
	}
	if avg := float64(kinds) / float64(windows); avg < 3 {
		t.Errorf("%.2f terrain types per screen on average, want at least 3", avg)
	}
}

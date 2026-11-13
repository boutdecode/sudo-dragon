package world

import "testing"

func TestClimateIsStablePerTerritoryAndVaried(t *testing.T) {
	seen := map[Climate]bool{}
	for seed := int64(1); seed <= 6; seed++ {
		w := newTestWorld(seed)
		for x := -5000; x <= 5000; x += 250 {
			for y := -5000; y <= 5000; y += 250 {
				c := w.ClimateAt(x, y)
				if c >= NumClimates {
					t.Fatalf("climate %d out of bounds", c)
				}
				seen[c] = true
				id := territoryIDAt(w.Seed, x, y)
				if c != w.TerritoryAt(x, y).climate || c != w.ClimateAt(x, y) ||
					(w.GenVersion < GenV2 && c != climateOf(w.Seed, id)) {
					t.Fatalf("the climate of %d,%d is not that of its district", x, y)
				}
			}
		}
	}
	for c := Climate(0); c < NumClimates; c++ {
		if !seen[c] {
			t.Errorf("climate %q never appears", c.Key())
		}
	}
}

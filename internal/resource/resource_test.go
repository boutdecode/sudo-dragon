package resource

import "testing"

func TestCatalogueIsConsistent(t *testing.T) {
	seen := map[Kind]bool{}
	perCat := map[Category]int{}
	for _, r := range All() {
		if seen[r.Kind] {
			t.Errorf("%s duplicated", r.Kind)
		}
		seen[r.Kind] = true
		perCat[r.Cat]++
		if r.Tier < 1 || r.Tier > MaxTier {
			t.Errorf("%s: tier %d", r.Kind, r.Tier)
		}
	}
	for c := Category(0); c < NumCategories; c++ {
		if perCat[c] == 0 {
			t.Errorf("category %s empty", c.Key())
		}
	}
}

func TestPickIsDeterministicAndValid(t *testing.T) {
	for c := Category(0); c < NumCategories; c++ {
		for climate := Temperate; climate <= Boreal; climate++ {
			for level := 1; level <= 20; level++ {
				for h := uint64(0); h < 50; h++ {
					k := Pick(c, climate, level, h*7919)
					if k != Pick(c, climate, level, h*7919) {
						t.Fatal("non-deterministic pick")
					}
					info, ok := Lookup(k)
					if !ok || info.Cat != c {
						t.Fatalf("Pick(%s) = %q", c.Key(), k)
					}
					if info.Tier > 1+(level-1)/5 {
						t.Fatalf("%s (tier %d) at level %d", k, info.Tier, level)
					}
					if !climateOK(info, climate) {
						t.Fatalf("%s outside its climate %d", k, climate)
					}
				}
			}
		}
	}
}

func TestRareResourcesOnlyFarAway(t *testing.T) {
	found := map[Kind]bool{}
	for h := uint64(0); h < 2000; h++ {
		found[Pick(Ore, Temperate, 20, h)] = true
	}
	if !found["gold"] || !found["iron"] {
		t.Errorf("rare ores missing at level 20: %v", found)
	}
	for h := uint64(0); h < 2000; h++ {
		if Pick(Ore, Temperate, 1, h) == "gold" {
			t.Fatal("gold at level 1")
		}
	}
}

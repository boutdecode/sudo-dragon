package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// placesByKind gathers the places of a square of regions around the origin.
func placesByKind(w *World, r int) map[Kind][]*Structure {
	out := map[Kind][]*Structure{}
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if s := w.StructureOfRegion(RegionPos{X: x, Y: y}); s != nil {
				out[s.Kind] = append(out[s.Kind], s)
			}
		}
	}
	return out
}

func allNPCs(s *Structure) []NPC { return append(append([]NPC(nil), s.NPCs...), s.Guards...) }

func TestNPCRacesAreDeterministicAndHatredBounded(t *testing.T) {
	t.Parallel()
	a, b := New(7), New(7)
	for kind, list := range placesByKind(a, 6) {
		for _, s := range list {
			other := b.StructureOfRegion(s.Region)
			na, nb := allNPCs(s), allNPCs(other)
			for i, n := range na {
				if !n.Race.Valid() {
					t.Fatalf("%v %s: no race", kind, n.Name)
				}
				if n.Race != nb[i].Race || n.Hatred != nb[i].Hatred {
					t.Fatalf("%s: not deterministic", n.Name)
				}
				if n.HatredOf(n.Race) != 0 || n.Hatred[n.Race.Index()] != 0 {
					t.Fatalf("%s hates its own race: %v", n.Name, n.Hatred)
				}
				for _, h := range n.Hatred {
					if h < 0 || h > 100 {
						t.Fatalf("%s: hatred %v", n.Name, n.Hatred)
					}
				}
				if n.RaceIn(i18n.FR) == "" || n.RaceIn(i18n.FR) == "npc.race."+string(n.Race)+".m" {
					t.Fatalf("%s: race label %q", n.Name, n.RaceIn(i18n.FR))
				}
			}
		}
	}
}

// Villages are almost single-race, towns mixed; villages hate more than towns.
func TestVillagesAreSingleRaceAndTownsMixed(t *testing.T) {
	t.Parallel()
	w := New(3)
	places := placesByKind(w, 12)
	if len(places[KindVillage]) < 10 || len(places[KindTown]) < 5 {
		t.Fatalf("too few places: %d villages, %d towns", len(places[KindVillage]), len(places[KindTown]))
	}
	share := func(s *Structure) (float64, int) { // share of the main race, number of races
		count := map[hero.Race]int{}
		ns := allNPCs(s)
		best := 0
		for _, n := range ns {
			count[n.Race]++
			best = max(best, count[n.Race])
		}
		return float64(best) / float64(len(ns)), len(count)
	}
	hate := func(list []*Structure) float64 {
		sum, n := 0, 0
		for _, s := range list {
			for _, npc := range allNPCs(s) {
				for _, r := range hero.Races {
					if r != npc.Race {
						sum += npc.HatredOf(r)
						n++
					}
				}
			}
		}
		return float64(sum) / float64(n)
	}
	var village, town, townRaces float64
	for _, s := range places[KindVillage] {
		sh, _ := share(s)
		village += sh
	}
	for _, s := range places[KindTown] {
		sh, k := share(s)
		town += sh
		townRaces += float64(k)
	}
	village /= float64(len(places[KindVillage]))
	town /= float64(len(places[KindTown]))
	townRaces /= float64(len(places[KindTown]))
	if village < 0.75 || town > 0.6 || townRaces < 3 {
		t.Fatalf("main race share: villages %.2f, towns %.2f (%.1f races per town)", village, town, townRaces)
	}
	if hv, ht := hate(places[KindVillage]), hate(places[KindTown]); hv <= ht {
		t.Fatalf("average hatred: villages %.1f, towns %.1f", hv, ht)
	}
}

func TestHatredOfNPCWithoutRace(t *testing.T) {
	t.Parallel()
	n := NPC{Hatred: [hero.NumRaces]int{90, 90, 90, 90}}
	if n.HatredOf(hero.Elf) != 0 || n.RaceIn(i18n.EN) != "" {
		t.Fatal("an NPC without a race hates no one")
	}
	n.Race = hero.Dwarf
	if n.HatredOf(hero.Elf) != 90 || n.HatredOf(hero.Dwarf) != 0 || n.HatredOf("") != 90 {
		t.Fatal("hatred lookup")
	}
}

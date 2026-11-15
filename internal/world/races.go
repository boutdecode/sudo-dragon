package world

import (
	"math/rand/v2"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Races of the NPCs. A village is almost single-race (a dominant people), a town or a fortress
// mixes the four. Each NPC also bears a hatred (in %) towards each other race: villages, wary of
// strangers, draw higher than towns. Everything comes from the identity stream, on salts of its own:
// neither the plans nor the names move, and the client computes the same thing as the server.

// Hatred tiers (in %), from which an NPC changes its attitude towards a race.
const (
	HateWary    = 20 // cold and suspicious
	HateHostile = 50 // contemptuous, helps as little as possible
	HateNoQuest = 70 // open hatred: hands out no work at all
)

// Salts of the race streams (distinct from the plans, names, guards...).
const (
	raceSaltPlace = 20000
	raceSaltNPC   = 20100
	raceSaltGuard = 20500
)

// villageMainShare is the chance (in %) for a villager to belong to the dominant race of their village.
const villageMainShare = 90

// HatredOf returns the hatred (0 to 100) of the NPC towards race r. An NPC without a race
// (built by hand) hates no one, and nobody hates their own race.
func (n NPC) HatredOf(r hero.Race) int {
	if n.Race == "" || r.OrDefault() == n.Race {
		return 0
	}
	return n.Hatred[r.Index()]
}

// RaceIn returns the race of the NPC in language l, agreeing with its gender ("Naine").
func (n NPC) RaceIn(l i18n.Lang) string {
	if n.Race == "" {
		return ""
	}
	g := "m"
	if n.id.female {
		g = "f"
	}
	return i18n.T(l, "npc.race."+string(n.Race)+"."+g)
}

// assignRaces draws the race and hatreds of the NPCs and guards of a place.
func (w *World) assignRaces(s *Structure) {
	place := identityRNG(w.Seed, s.Region, raceSaltPlace)
	main := pick(place, hero.Races)
	// The starting village welcomes every newcomer: it keeps its main people but has the
	// tolerance of a town.
	tolerant := s.Kind != KindVillage || RegionOf(w.spawnAnchor().X, w.spawnAnchor().Y) == s.Region
	draw := func(n *NPC, rng *rand.Rand) {
		if s.Kind == KindVillage && rng.IntN(100) < villageMainShare {
			n.Race = main
		} else if s.Kind == KindVillage {
			others := make([]hero.Race, 0, hero.NumRaces-1)
			for _, r := range hero.Races {
				if r != main {
					others = append(others, r)
				}
			}
			n.Race = pick(rng, others)
		} else {
			n.Race = pick(rng, hero.Races)
		}
		for _, r := range hero.Races {
			h := hatredRoll(rng, tolerant)
			if r == n.Race {
				h = 0
			}
			n.Hatred[r.Index()] = h
		}
	}
	for i := range s.NPCs {
		draw(&s.NPCs[i], identityRNG(w.Seed, s.Region, raceSaltNPC+uint64(i)))
	}
	for i := range s.Guards {
		draw(&s.Guards[i], identityRNG(w.Seed, s.Region, raceSaltGuard+uint64(i)))
	}
}

// hatredRoll draws a hatred: mostly low, sometimes strong. A tolerant place (town, starting
// village) rarely reaches the high tier.
func hatredRoll(rng *rand.Rand, tolerant bool) int {
	low, mid := 50, 80 // village: 50 % low, 30 % medium, 20 % high
	if tolerant {
		low, mid = 70, 95 // town: 70 % low, 25 % medium, 5 % high
	}
	switch u := rng.IntN(100); {
	case u < low:
		return rng.IntN(HateWary)
	case u < mid:
		return HateWary + rng.IntN(60-HateWary)
	}
	return 60 + rng.IntN(41)
}

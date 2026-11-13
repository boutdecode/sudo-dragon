package world

import "github.com/kevin/ascii-adventure/internal/i18n"

// NPCGuard is the guard of a village or town. Unlike the other NPCs it is not posted: it patrols
// (see game/guards.go). It is therefore not in NPCKinds, nor in Structure.NPCs, and never occupies a tile of
// NPCAt: only its starting position is deterministic.
const NPCGuard NPCKind = "guard"

// GuardIndexBase is the rank of the first guard of a place in the numbering of its NPCs (Index). It is large
// enough never to overlap a posted NPC.
const GuardIndexBase = 100

// guardsPerKind gives the number of guards of a place.
var guardsPerKind = map[Kind]int{KindVillage: 2, KindTown: 5}

// guardSalt is the salt of the guards' identity stream: distinct from those of the plans, the names and the posted NPCs,
// so as not to shift anything that exists.
const guardSalt = 8000

// makeGuards invents the guards of the place: identity (name unique within the place) and starting tile on a road.
// It consumes nothing from the plans' stream.
func (w *World) makeGuards(s *Structure, taken map[string]bool) {
	n := guardsPerKind[s.Kind]
	if n == 0 {
		return
	}
	busy := map[Pos]bool{}
	for _, npc := range s.NPCs {
		busy[npc.Pos] = true
	}
	var roads []Pos
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			p := Pos{s.Origin.X + x, s.Origin.Y + y}
			if s.tiles[y*s.W+x] == Road && !busy[p] {
				roads = append(roads, p)
			}
		}
	}
	if len(roads) == 0 {
		return
	}
	for i := 0; i < n; i++ {
		rng := identityRNG(w.Seed, s.Region, guardSalt+uint64(i))
		var id ident
		var name string
		for try := 0; try < 16; try++ {
			id = npcIdentity(NPCGuard, rng)
			name = id.name(i18n.FR, NPCGuard)
			if !taken[name] {
				break
			}
		}
		taken[name] = true
		start := roads[rng.IntN(len(roads))]
		for try := 0; try < 8 && busy[start]; try++ {
			start = roads[rng.IntN(len(roads))]
		}
		busy[start] = true
		s.Guards = append(s.Guards, NPC{
			Kind: NPCGuard, Pos: start, Region: s.Region, Index: GuardIndexBase + i,
			Name: name, Persona: id.persona(i18n.FR), id: id,
		})
	}
}

// RoadAt tells whether (x, y) is a road of the structure (where the guards patrol).
func (s *Structure) RoadAt(x, y int) bool { return s.Contains(x, y) && s.TileAt(x, y) == Road }

package world

import "github.com/kevin/ascii-adventure/internal/i18n"

// NPCGuard est le garde d'un village ou d'une ville. Contrairement aux autres PNJ il n'est pas posté : il patrouille
// (voir game/guards.go). Il n'est donc pas dans NPCKinds, ni dans Structure.NPCs, et n'occupe jamais une case de
// NPCAt : seule sa position de départ est déterministe.
const NPCGuard NPCKind = "guard"

// GuardIndexBase est le rang du premier garde d'un lieu dans la numérotation de ses PNJ (Index). Il est assez
// grand pour ne jamais recouvrir un PNJ posté.
const GuardIndexBase = 100

// guardsPerKind donne le nombre de gardes d'un lieu.
var guardsPerKind = map[Kind]int{KindVillage: 2, KindTown: 5}

// guardSalt est le sel du flux d'identité des gardes : distinct de ceux des plans, des noms et des PNJ postés,
// pour ne rien décaler de ce qui existe.
const guardSalt = 8000

// makeGuards invente les gardes du lieu : identité (nom unique dans le lieu) et case de départ sur un chemin.
// Il ne consomme rien du flux des plans.
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

// RoadAt indique si (x, y) est un chemin de la structure (là où les gardes patrouillent).
func (s *Structure) RoadAt(x, y int) bool { return s.Contains(x, y) && s.TileAt(x, y) == Road }

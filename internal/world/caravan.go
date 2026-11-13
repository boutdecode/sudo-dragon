package world

import "github.com/kevin/ascii-adventure/internal/i18n"

// Caravans: a travelling merchant goes back and forth on some roads between two places (see game/caravan.go
// for its movement). Its existence and identity are drawn from the seed, like those of the posted NPCs.

const (
	// CaravanIndexBase is the rank of the travelling merchant in the numbering of the NPCs of its region (Index). It is
	// beyond the posted NPCs (fewer than 100) and the guards (GuardIndexBase onwards).
	CaravanIndexBase = 200

	caravanSalt   = 9_000_000 // identity stream: beyond all the salts of the points of interest and of the dig spots
	caravanChance = 0.5       // share of the roads that carry a caravan
)

// HasCaravan tells whether road r carries a caravan, and returns its seed (which sets its offset in time).
func (w *World) HasCaravan(r RoadRoute) (seed uint64, ok bool) {
	rng := identityRNG(w.Seed, r.A, caravanSalt+uint64(r.Orient))
	if rng.Float64() >= caravanChance {
		return 0, false
	}
	return rng.Uint64(), true
}

// CaravanMerchant builds the merchant of the caravan on road r, at tile pos: blacksmith or armourer depending on the
// seed, with its own identity. Region and Index (CaravanIndexBase + Orient) form its key.
func (w *World) CaravanMerchant(r RoadRoute, pos Pos) NPC {
	seed, _ := w.HasCaravan(r)
	kind := NPCSmith
	if seed&1 == 1 {
		kind = NPCArmorer
	}
	id := npcIdentity(kind, identityRNG(w.Seed, r.A, caravanSalt+10+uint64(r.Orient)))
	return NPC{
		Kind: kind, Pos: pos, Region: r.A, Index: CaravanIndexBase + r.Orient,
		Name: id.name(i18n.FR, kind), Persona: id.persona(i18n.FR), id: id,
	}
}

package world

import "github.com/kevin/ascii-adventure/internal/i18n"

// Caravanes : un marchand ambulant fait l'aller-retour sur certaines routes entre deux lieux (voir game/caravan.go
// pour son déplacement). Son existence et son identité sont tirées de la seed, comme celles des PNJ postés.

const (
	// CaravanIndexBase est le rang du marchand ambulant dans la numérotation des PNJ de sa région (Index). Il est au-delà
	// des PNJ postés (moins de 100) et des gardes (GuardIndexBase et suivants).
	CaravanIndexBase = 200

	caravanSalt   = 9_000_000 // flux d'identité : au-delà de tous les sels des points d'intérêt et des cases à creuser
	caravanChance = 0.5       // part des routes qui portent une caravane
)

// HasCaravan dit si la route r porte une caravane, et renvoie sa graine (qui règle son décalage dans le temps).
func (w *World) HasCaravan(r RoadRoute) (seed uint64, ok bool) {
	rng := identityRNG(w.Seed, r.A, caravanSalt+uint64(r.Orient))
	if rng.Float64() >= caravanChance {
		return 0, false
	}
	return rng.Uint64(), true
}

// CaravanMerchant fabrique le marchand de la caravane de la route r, à la case pos : forgeron ou armurier selon la
// graine, avec une identité propre. Region et Index (CaravanIndexBase + Orient) forment sa clé.
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

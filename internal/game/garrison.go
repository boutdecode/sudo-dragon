package game

import (
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	// garrisonActivation: une forteresse à moins de cette distance (Chebyshev) d'un
	// joueur fait apparaître sa garnison. C'est la zone de conservation des
	// monstres (despawnFar) : un monstre créé ici n'est pas supprimé aussitôt.
	garrisonActivation = keepHalfH

	guardRespawn = 3000 // ticks (5 min) avant que le poste d'un soldat soit réoccupé après sa défaite
	bossRespawn  = 9000 // ticks (15 min) pour le seigneur

	// Butin garanti du seigneur de guerre, en plus de l'or ordinaire.
	bossGold    = 100
	bossPotions = 3

	// Or de plus par palier du donjon, pour le boss d'un donjon.
	bossGoldPerTier = 40
)

// speciesForRole renvoie l'espèce qui occupe un emplacement de forteresse.
func speciesForRole(r world.SlotRole) (species, bool) {
	kind := map[world.SlotRole]string{
		world.RoleGuard: "skeleton",
		world.RoleElite: "knight",
		world.RoleBoss:  "warlord",
	}[r]
	for _, s := range speciesList {
		if s.Kind == kind {
			return s, true
		}
	}
	return species{}, false
}

// respawnDelay renvoie le délai de repousse après la défaite d'un monstre de garnison.
func respawnDelay(sp species) uint64 {
	if sp.Boss {
		return bossRespawn
	}
	return guardRespawn
}

// activateGarrisons peuple les forteresses proches d'un joueur. Un emplacement
// reste vide tant que son monstre est en vie, qu'il est en repousse après une
// victoire, ou qu'un joueur ou un monstre s'y tient.
func (g *Game) activateGarrisons() {
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		for _, s := range g.World.StructuresNear(p.pos, garrisonActivation) {
			if s.Kind != world.KindFortress {
				continue
			}
			for i, slot := range s.Slots {
				g.fillSlot(s, i, slot)
			}
		}
	}
}

func (g *Game) fillSlot(s *world.Structure, i int, slot world.Slot) {
	key := s.Key(i)
	if _, alive := g.slotMonster[key]; alive {
		return
	}
	sp, ok := speciesForRole(slot.Role)
	if !ok {
		return
	}
	if at, beaten := g.slotDefeated[key]; beaten {
		if g.tick-at < respawnDelay(sp) {
			return
		}
		delete(g.slotDefeated, key) // délai écoulé
	}
	if g.blocked(slot.Pos) { // quelqu'un se tient sur le poste: on réessaie au prochain passage
		return
	}
	m := g.addMonster(scaledForLevel(sp, world.LevelAt(slot.Pos.X, slot.Pos.Y)), slot.Pos)
	m.slot = &key
	m.bounds = s
	m.leash = garrisonLeash
	g.slotMonster[key] = m.id
	g.garrisonCount++
}

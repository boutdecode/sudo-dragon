package game

import (
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	// garrisonActivation: a fortress within this distance (Chebyshev) of a
	// player spawns its garrison. This is the monster retention zone
	// (despawnFar): a monster created here is not removed right away.
	garrisonActivation = keepHalfH

	guardRespawn = 3000 // ticks (5 min) before a soldier's post is reoccupied after its defeat
	bossRespawn  = 9000 // ticks (15 min) for the lord

	// Guaranteed loot of the warlord, on top of the ordinary gold.
	bossGold    = 100
	bossPotions = 3

	// Extra gold per dungeon tier, for a dungeon's boss.
	bossGoldPerTier = 40
)

// speciesForRole returns the species that occupies a fortress slot.
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

// respawnDelay returns the respawn delay after the defeat of a garrison monster.
func respawnDelay(sp species) uint64 {
	if sp.Boss {
		return bossRespawn
	}
	return guardRespawn
}

// activateGarrisons populates the fortresses near a player. A slot
// stays empty while its monster is alive, while it is respawning after a
// victory, or while a player or a monster stands on it.
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
		delete(g.slotDefeated, key) // delay elapsed
	}
	if g.blocked(slot.Pos) { // someone is standing on the post: retry on the next pass
		return
	}
	m := g.addMonster(scaledForLevel(sp, world.LevelAt(slot.Pos.X, slot.Pos.Y)), slot.Pos)
	m.slot = &key
	m.bounds = s
	m.leash = garrisonLeash
	g.slotMonster[key] = m.id
	g.garrisonCount++
}

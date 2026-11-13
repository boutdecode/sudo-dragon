package game

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// fortress returns a fortress of the test world.
func fortress(t *testing.T, g *Game) *world.Structure {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 400) {
		if s.Kind == world.KindFortress {
			return s
		}
	}
	t.Skip("no fortress near the start")
	return nil
}

func speciesNamed(t *testing.T, kind string) species {
	t.Helper()
	for _, s := range speciesList {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("unknown species %q", kind)
	return species{}
}

// outsideFortress returns a free tile d tiles from the centre, outside the footprint.
func nearFortress(t *testing.T, g *Game, s *world.Structure, d int) world.Pos {
	t.Helper()
	c := s.Center()
	return freeWalkable(t, g, world.Pos{X: c.X + s.W/2 + d, Y: c.Y}, 0)
}

func TestGarrisonSpeciesAreConsistent(t *testing.T) {
	for _, role := range []world.SlotRole{world.RoleGuard, world.RoleElite, world.RoleBoss} {
		sp, ok := speciesForRole(role)
		if !ok {
			t.Fatalf("no species for role %d", role)
		}
		if !sp.Garrison {
			t.Errorf("%s is not marked as garrison", sp.Kind)
		}
		if sp.Boss != (role == world.RoleBoss) {
			t.Errorf("%s: Boss=%v for role %d", sp.Kind, sp.Boss, role)
		}
	}
	// Garrisons never spawn according to biome.
	for tile, list := range speciesByTile {
		for _, sp := range list {
			if sp.Garrison {
				t.Errorf("%s would spawn in a biome (%d)", sp.Kind, tile)
			}
		}
	}
	// Power grows with rank.
	g, e, b := speciesNamed(t, "skeleton"), speciesNamed(t, "knight"), speciesNamed(t, "warlord")
	if !(g.HP < e.HP && e.HP < b.HP && g.Dmg.Avg() < e.Dmg.Avg() && e.Dmg.Avg() < b.Dmg.Avg() && g.fighter().Def < e.fighter().Def && e.fighter().Def < b.fighter().Def && g.XP < e.XP && e.XP < b.XP) {
		t.Fatal("garrison ranks are not strictly increasing")
	}
	// The lord is unique per fortress, the lich per dungeon.
	bosses, dungeonBosses := 0, 0
	for _, s := range speciesList {
		switch {
		case s.Boss && s.Garrison:
			bosses++
		case s.Boss && s.Dungeon:
			dungeonBosses++
		}
	}
	if bosses != 1 || dungeonBosses != 1 {
		t.Fatalf("%d fortress boss species and %d dungeon ones, one of each expected", bosses, dungeonBosses)
	}
}

func TestFortressActivatesWhenPlayerIsNear(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()

	if len(g.monsters) != len(s.Slots) || g.garrisonCount != len(s.Slots) {
		t.Fatalf("%d monsters (garrison %d), %d slots", len(g.monsters), g.garrisonCount, len(s.Slots))
	}
	roles := map[string]int{}
	for i, slot := range s.Slots {
		id, ok := g.slotMonster[s.Key(i)]
		if !ok {
			t.Fatalf("slot %d empty", i)
		}
		m := g.monsters[id]
		want, _ := speciesForRole(slot.Role)
		if m.sp.Kind != want.Kind || m.pos != slot.Pos || m.home != slot.Pos {
			t.Fatalf("slot %d: %s at %v, expected %s at %v", i, m.sp.Kind, m.pos, want.Kind, slot.Pos)
		}
		if m.bounds != s || m.leash != garrisonLeash || m.slot == nil || *m.slot != s.Key(i) {
			t.Fatalf("slot %d: monster badly initialised: %+v", i, m)
		}
		roles[m.sp.Kind]++
	}
	if roles["warlord"] != 1 || roles["knight"] != 2 || roles["skeleton"] != 6 {
		t.Fatalf("garrison %v, expected 1 lord, 2 knights, 6 skeletons", roles)
	}
	if len(g.monsterAt) != len(g.monsters) {
		t.Fatal("inconsistent position index")
	}
}

func TestFortressStaysEmptyWhenNobodyIsNear(t *testing.T) {
	g := newGame()
	fortress(t, g)
	joinRec(g, "a") // at the starting point, far from any fortress
	g.activateGarrisons()
	if len(g.monsters) != 0 {
		t.Fatalf("%d monsters appeared without a player nearby", len(g.monsters))
	}
}

func TestActivationIsIdempotentAndSharedBetweenPlayers(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	place(g, a, nearFortress(t, g, s, 15))
	place(g, b, nearFortress(t, g, s, 25))
	for i := 0; i < 5; i++ {
		g.activateGarrisons()
	}
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("%d monsters for %d slots: duplicates", len(g.monsters), len(s.Slots))
	}
}

func TestGarrisonNeverLeavesTheFortress(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	// Player right in front of the gate, within detection range: it attracts the garrison.
	place(g, p, nearFortress(t, g, s, 3))
	g.activateGarrisons()
	for i := 0; i < 2000; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		for _, m := range g.monsters {
			if !s.Contains(m.pos.X, m.pos.Y) {
				t.Fatalf("%s left the fortress: %v", m.sp.Kind, m.pos)
			}
			if p.fight != nil { // a fight started: free the player to continue observing
				g.endFightQuietly(p)
			}
		}
	}
}

// endFightQuietly abandons the player's fight, for observation tests.
func (g *Game) endFightQuietly(p *player) {
	if f := p.fight; f != nil {
		f.b.Abandon()
		g.endFight(f)
	}
}

func TestGarrisonWanderStaysNearPost(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 30)) // close enough for activation, too far for detection
	g.activateGarrisons()
	for i := 0; i < 1500; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		for _, m := range g.monsters {
			if cheb(m.pos, m.home) > garrisonLeash {
				t.Fatalf("%s moved %d tiles from its post (leash %d)", m.sp.Kind, cheb(m.pos, m.home), garrisonLeash)
			}
		}
	}
}

func TestVictoryStartsRespawnDelay(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()

	// Beat the soldier of the first "soldier" role slot.
	idx := -1
	for i, sl := range s.Slots {
		if sl.Role == world.RoleGuard {
			idx = i
			break
		}
	}
	key := s.Key(idx)
	m := g.monsters[g.slotMonster[key]]
	m.sp.HP = 1 // weakened for a one-hit victory
	dx, dy := freeDir(t, g, p)
	place(g, p, world.Pos{X: m.pos.X - dx, Y: m.pos.Y - dy}) // next to the monster
	if g.blocked(p.pos) && g.byPos[p.pos] != p.id {
		t.Skip("tile next to the post unavailable")
	}
	g.startFight(p, m, true)
	g.act(p, combat.Attack, 0)
	if p.fight != nil {
		t.Fatal("the fight should have been won")
	}
	if _, alive := g.slotMonster[key]; alive {
		t.Fatal("the slot is still marked occupied")
	}
	if g.garrisonCount != len(s.Slots)-1 {
		t.Fatalf("living garrison = %d, expected %d", g.garrisonCount, len(s.Slots)-1)
	}
	if _, beaten := g.slotDefeated[key]; !beaten {
		t.Fatal("the victory did not start the respawn")
	}

	// During the respawn: nothing.
	g.tick += guardRespawn - 1
	g.activateGarrisons()
	if _, alive := g.slotMonster[key]; alive {
		t.Fatal("the post was reoccupied before the delay ended")
	}
	// Delay elapsed: the post is reoccupied.
	g.tick++
	g.activateGarrisons()
	if _, alive := g.slotMonster[key]; !alive {
		t.Fatal("the post was not reoccupied after the delay")
	}
	if _, beaten := g.slotDefeated[key]; beaten {
		t.Fatal("the respawn entry was not cleaned up")
	}
	// The other posts were not touched.
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("%d monsters, %d expected", len(g.monsters), len(s.Slots))
	}
}

func TestBossRespawnIsLongerThanGuards(t *testing.T) {
	boss, guard := speciesNamed(t, "warlord"), speciesNamed(t, "skeleton")
	if respawnDelay(boss) <= respawnDelay(guard) {
		t.Fatalf("lord's delay (%d) <= soldier's (%d)", respawnDelay(boss), respawnDelay(guard))
	}
	if respawnDelay(boss) != bossRespawn || respawnDelay(guard) != guardRespawn {
		t.Fatal("respawn delays inconsistent with the constants")
	}
}

// Moving away does not punish the fortress: the post is freed without delay.
func TestDespawnFreesSlotWithoutDelay(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()
	if len(g.slotMonster) != len(s.Slots) {
		t.Fatal("garrison not created")
	}

	place(g, p, g.World.FindSpawn()) // the player goes very far away
	g.despawnFar()
	if len(g.monsters) != 0 || len(g.slotMonster) != 0 || g.garrisonCount != 0 {
		t.Fatalf("after moving away: %d monsters, %d posts occupied, garrison %d", len(g.monsters), len(g.slotMonster), g.garrisonCount)
	}
	if len(g.slotDefeated) != 0 {
		t.Fatal("merely moving away must not start a respawn")
	}

	place(g, p, nearFortress(t, g, s, 20)) // they come back
	g.activateGarrisons()
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("on return: %d monsters, %d expected", len(g.monsters), len(s.Slots))
	}
}

func TestOccupiedPostWaits(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, s.Slots[0].Pos) // the player stands on a post
	g.activateGarrisons()
	if _, alive := g.slotMonster[s.Key(0)]; alive {
		t.Fatal("a monster appeared on a player's tile")
	}
	if len(g.monsters) != len(s.Slots)-1 {
		t.Fatalf("%d monsters, %d expected (all but the occupied post)", len(g.monsters), len(s.Slots)-1)
	}
	place(g, p, nearFortress(t, g, s, 10))
	g.activateGarrisons()
	if _, alive := g.slotMonster[s.Key(0)]; !alive {
		t.Fatal("the freed post was not reoccupied")
	}
}

func TestGarrisonDoesNotUseBiomeQuota(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 25))
	g.activateGarrisons()
	garrison := g.garrisonCount

	if n := g.monstersAround(p.pos); n != 0 {
		t.Fatalf("the garrison counts toward the biome quota (%d)", n)
	}
	for i := 0; i < 600; i++ {
		g.spawnMonsters()
	}
	biome := len(g.monsters) - garrison
	if biome == 0 {
		t.Fatal("no biome monster appeared near the fortress")
	}
	if biome > maxPerPlayer {
		t.Fatalf("%d biome monsters, quota %d", biome, maxPerPlayer)
	}
}

func TestGarrisonIsVisibleInSnapshots(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, rec := joinRec(g, "a")
	c := s.Center()
	place(g, p, freeWalkable(t, g, world.Pos{X: c.X, Y: c.Y + s.H/2 + 4}, 0))
	g.activateGarrisons()
	g.step()
	kinds := map[string]bool{}
	for _, m := range rec.last().Monsters {
		kinds[m.Kind] = true
	}
	if !kinds["skeleton"] && !kinds["knight"] && !kinds["warlord"] {
		t.Fatalf("no garrison monster in the snapshot: %+v", rec.last().Monsters)
	}
}

// --- Lord's loot ---

func TestBossLootIsGuaranteed(t *testing.T) {
	for i := 0; i < 30; i++ {
		r := newRig(t, func() species { s := speciesNamed(t, "warlord"); s.HP = 1; return s }())
		r.g.rng = rand.New(rand.NewPCG(uint64(i), 3))
		r.p.stats.Potions = 0
		r.p.stats.MaxHP, r.p.hp = 10_000, 10_000 // the lord must not win before the fatal blow
		r.engage()
		for r.p.fight != nil { // a blow can miss
			r.g.act(r.p, combat.Attack, 0)
		}

		if r.p.stats.Potions != bossPotions {
			t.Fatalf("attempt %d: %d potions, expected %d", i, r.p.stats.Potions, bossPotions)
		}
		min := bossGold + speciesNamed(t, "warlord").XP/2
		if r.p.stats.Gold < min {
			t.Fatalf("attempt %d: %d gold, at least %d expected", i, r.p.stats.Gold, min)
		}
		if !strings.Contains(strings.Join(r.last().Log, "|"), "trésor") {
			t.Fatalf("attempt %d: log without mention of the treasure: %v", i, r.last().Log)
		}
	}
}

func TestBossLootRespectsCaps(t *testing.T) {
	boss := speciesNamed(t, "warlord")
	boss.HP = 1
	r := newRig(t, boss)
	r.p.stats.Potions = combat.MaxPotions - 1
	r.p.stats.Gold = combat.MaxGold - 10
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Potions != combat.MaxPotions || r.p.stats.Gold != combat.MaxGold {
		t.Fatalf("caps exceeded: %d potions, %d gold", r.p.stats.Potions, r.p.stats.Gold)
	}
}

func TestOrdinaryMonsterDoesNotDropBossLoot(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.Potions = 0
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Potions > 1 || strings.Contains(strings.Join(r.last().Log, "|"), "trésor") {
		t.Fatalf("boss loot for a rat: %d potions, %v", r.p.stats.Potions, r.last().Log)
	}
}

func TestGarrisonSurvivesSaveAndRestoreRoundTrip(t *testing.T) {
	// The garrison is ephemeral state: it must add nothing to the save.
	g, sv := gameWithSaves()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()
	g.leave(p.id)
	saved, ok := sv.last("a")
	if !ok {
		t.Fatal("no save")
	}
	g2 := newGame()
	q := g2.joinWith("a", func(protocol.Snapshot) {}, &saved)
	if q.pos != saved.Pos {
		t.Fatalf("restored position %v, saved %v", q.pos, saved.Pos)
	}
	if len(g2.monsters) != 0 {
		t.Fatal("monsters appeared on restore")
	}
}

// The real loop (step) must trigger the activation, not just the direct call.
func TestStepActivatesGarrisonPeriodically(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))

	for i := 0; i < spawnEvery-1; i++ {
		g.step()
	}
	if g.garrisonCount != 0 {
		t.Fatalf("garrison created before the deadline (%d)", g.garrisonCount)
	}
	g.step() // tick spawnEvery
	if g.garrisonCount != len(s.Slots) {
		t.Fatalf("garrison = %d after %d ticks, expected %d", g.garrisonCount, spawnEvery, len(s.Slots))
	}
}

package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// village returns the starting village of the test world.
func village(t *testing.T, g *Game) *world.Structure {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 250) {
		if s.Kind == world.KindVillage {
			return s
		}
	}
	t.Skip("no village near the start")
	return nil
}

// insideVillage returns a free tile, inside the safe zone, far from the edges.
func insideVillage(t *testing.T, g *Game, s *world.Structure) world.Pos {
	t.Helper()
	return freeWalkable(t, g, s.Center(), 0)
}

// outsideEdge returns a free tile just outside the safe zone (west of the village).
func outsideEdge(t *testing.T, g *Game, s *world.Structure) world.Pos {
	t.Helper()
	y := s.Center().Y
	for dx := 0; dx < 12; dx++ {
		pos := world.Pos{X: s.Origin.X - 3 - 1 - dx, Y: y} // 3 = safe zone margin
		if !g.World.Safe(pos.X, pos.Y) && !g.blocked(pos) {
			return pos
		}
	}
	t.Skip("no free tile outside the safe zone west of the village")
	return world.Pos{}
}

func TestPlayerInSafeZoneIsFlagged(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	if p.safe {
		t.Fatal("the starting point must not be in a safe zone")
	}
	place(g, p, insideVillage(t, g, s))
	if !p.safe {
		t.Fatal("the safe-zone flag is not set in the village")
	}
	place(g, p, outsideEdge(t, g, s))
	if p.safe {
		t.Fatal("the safe-zone flag did not drop outside the village")
	}
}

func TestMonsterCannotEnterSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	edge := outsideEdge(t, g, s)
	m := g.addMonster(testRat, edge)

	// Inward: refused. Outward: allowed.
	if g.moveMonster(m, 1, 0) && g.World.Safe(m.pos.X, m.pos.Y) {
		t.Fatal("a monster entered the safe zone")
	}
	if !g.World.Safe(edge.X+1, edge.Y) {
		t.Skip("the neighbouring tile is not in a safe zone: the edge is elsewhere")
	}
	m.pos = edge
	g.monsterAt = map[world.Pos]int{edge: m.id}
	if g.moveMonster(m, 1, 0) {
		t.Fatal("a monster crossed the edge of the safe zone")
	}
	if !g.moveMonster(m, -1, 0) {
		t.Fatal("a monster should be able to move away from the safe zone")
	}
}

func TestMonstersNeverWanderIntoSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	m := g.addMonster(testRat, outsideEdge(t, g, s))
	for i := 0; i < 3000; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		if g.World.Safe(m.pos.X, m.pos.Y) {
			t.Fatalf("the monster wanders in the safe zone (tick %d): %v", i, m.pos)
		}
	}
}

// The same monster, at the same distance: it attacks a player outside, not a player inside.
func TestMonsterIgnoresPlayerInSafeZoneButNotOutside(t *testing.T) {
	g := newGame()
	s := village(t, g)
	edge := outsideEdge(t, g, s)
	inside := world.Pos{X: edge.X + 5, Y: edge.Y} // 5 tiles away: in the zone, within the rat's detection range
	if !g.World.Safe(inside.X, inside.Y) || g.blocked(inside) {
		t.Skip("test geometry unavailable")
	}

	p, _ := joinRec(g, "a")
	place(g, p, inside)
	m := g.addMonster(testRat, edge)
	for i := 0; i < 200; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	if p.fight != nil {
		t.Fatal("a monster attacked a player who is in a safe zone")
	}

	// Control: the player leaves the zone, at the same distance. The monster chases them.
	out := world.Pos{X: edge.X - 3, Y: edge.Y}
	if g.blocked(out) || g.World.Safe(out.X, out.Y) {
		t.Skip("control tile unavailable")
	}
	place(g, p, out)
	m.pos = edge
	g.monsterAt = map[world.Pos]int{edge: m.id}
	m.cooldown = 0
	for i := 0; i < 200 && p.fight == nil; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	if p.fight == nil {
		t.Fatal("the monster does not attack a player outside the safe zone (the control test fails)")
	}
}

func TestNoBiomeSpawnInSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, insideVillage(t, g, s))
	for i := 0; i < 800; i++ {
		g.spawnMonsters()
	}
	if len(g.monsters) == 0 {
		t.Fatal("no monster spawned: the test proves nothing")
	}
	for _, m := range g.monsters {
		if g.World.Safe(m.pos.X, m.pos.Y) {
			t.Fatalf("%s spawned in a safe zone: %v", m.sp.Kind, m.pos)
		}
	}
}

func TestSafeZoneHealsFaster(t *testing.T) {
	g := newGame()
	s := village(t, g)
	safe, _ := joinRec(g, "safe")
	wild, _ := joinRec(g, "wild")
	place(g, safe, insideVillage(t, g, s))
	safe.hp, wild.hp = 1, 1

	for i := 0; i < regenEvery; i++ { // 3 seconds
		g.step()
	}
	if got := safe.hp - 1; got != regenEvery/safeRegenEvery {
		t.Fatalf("in a safe zone: +%d HP in %d ticks, expected +%d", got, regenEvery, regenEvery/safeRegenEvery)
	}
	if got := wild.hp - 1; got != 1 {
		t.Fatalf("outside a safe zone: +%d HP in %d ticks, expected +1", got, regenEvery)
	}
	// Never beyond the maximum.
	for i := 0; i < 2000; i++ {
		g.step()
	}
	if safe.hp != safe.stats.MaxHP || wild.hp != wild.stats.MaxHP {
		t.Fatalf("HP after a long wait: %d and %d, maximum %d", safe.hp, wild.hp, safe.stats.MaxHP)
	}
}

func TestNoHealingDuringFightEvenInSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, insideVillage(t, g, s))
	// The player attacks a monster placed next to them, then stops playing.
	dx, dy := freeDir(t, g, p)
	m := g.addMonster(deadly(testRat), world.Pos{X: p.pos.X + dx, Y: p.pos.Y + dy})
	g.startFight(p, m, true)
	p.hp = 3
	for i := 0; i < 100; i++ {
		g.healDue(g.sortedIDs())
		g.regenerate(g.healDue(g.sortedIDs()))
	}
	if p.hp != 3 {
		t.Fatalf("HP = %d: healed during a fight", p.hp)
	}
}

// --- Or ---

func TestGoldRewardRange(t *testing.T) {
	g := newGame()
	for _, sp := range speciesList {
		lo, hi := sp.XP/2, sp.XP
		seen := map[int]bool{}
		for i := 0; i < 500; i++ {
			got := g.goldReward(sp)
			if got < lo || got > hi {
				t.Fatalf("%s: gold %d outside [%d, %d]", sp.Kind, got, lo, hi)
			}
			seen[got] = true
		}
		if len(seen) < 2 && hi > lo {
			t.Fatalf("%s: gold never varies", sp.Kind)
		}
	}
}

func TestWinGivesGoldAndSaysSo(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

	if r.p.stats.Gold < testRat.XP/2 || r.p.stats.Gold > testRat.XP {
		t.Fatalf("gold = %d, expected between %d and %d", r.p.stats.Gold, testRat.XP/2, testRat.XP)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "or.") {
		t.Fatalf("the journal does not mention gold: %v", r.last().Log)
	}
}

func TestLosingOrFleeingGivesNoGold(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.hp = 1
	r.engage()
	r.g.act(r.p, combat.Attack, 0) // lost
	if r.p.stats.Gold != 0 {
		t.Fatalf("gold = %d after a defeat", r.p.stats.Gold)
	}
}

func TestGoldIsCappedOnWin(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.Gold = combat.MaxGold
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Gold != combat.MaxGold {
		t.Fatalf("gold = %d, ceiling %d", r.p.stats.Gold, combat.MaxGold)
	}
	if strings.Contains(strings.Join(r.last().Log, "|"), "or.") {
		t.Fatalf("gold message although nothing was added: %v", r.last().Log)
	}
}

func TestGoldInSnapshotAndPersistence(t *testing.T) {
	g, sv := gameWithSaves()
	p, rec := joinRec(g, "a")
	p.stats.Gold = 321
	g.step()
	if got := rec.last().Stats.Gold; got != 321 {
		t.Fatalf("snapshot gold = %d", got)
	}

	g.leave(p.id)
	saved, ok := sv.last("a")
	if !ok || saved.Stats.Gold != 321 {
		t.Fatalf("saved gold: %+v (ok=%v)", saved.Stats, ok)
	}

	g2 := newGame()
	q := g2.joinWith("a", func(protocol.Snapshot) {}, &saved)
	if q.stats.Gold != 321 {
		t.Fatalf("restored gold = %d", q.stats.Gold)
	}
}

func TestRestoredGoldIsClamped(t *testing.T) {
	g := newGame()
	for in, want := range map[int]int{-9: 0, 5: 5, combat.MaxGold + 10: combat.MaxGold} {
		saved := &Saved{Stats: combat.NewStats().WithGold(0), HP: 10, Pos: g.World.FindSpawn()}
		saved.Stats.Gold = in // raw value, as read from a corrupted database
		p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
		if p.stats.Gold != want {
			t.Errorf("gold restored from %d = %d, expected %d", in, p.stats.Gold, want)
		}
		g.leave(p.id)
	}
}

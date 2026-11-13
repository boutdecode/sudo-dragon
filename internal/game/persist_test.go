package game

import (
	"context"
	"github.com/kevin/ascii-adventure/internal/dice"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// saves collects the calls to Persist.
type saves struct{ calls []savedCall }
type savedCall struct {
	name string
	s    Saved
}

func (s *saves) persist(name string, v Saved) { s.calls = append(s.calls, savedCall{name, v}) }
func (s *saves) last(name string) (Saved, bool) {
	for i := len(s.calls) - 1; i >= 0; i-- {
		if s.calls[i].name == name {
			return s.calls[i].s, true
		}
	}
	return Saved{}, false
}

func gameWithSaves() (*Game, *saves) {
	g := newGame()
	sv := &saves{}
	g.Persist = sv.persist
	return g, sv
}

func TestJoinRestoresSavedState(t *testing.T) {
	g := newGame()
	spawn := g.World.FindSpawn()
	target := freeWalkable(t, g, world.Pos{X: spawn.X + 12, Y: spawn.Y + 3}, 0)

	st := combat.StatsAt(5, 7, 4)
	p := g.joinWith("alice", func(protocol.Snapshot) {}, &Saved{Stats: st, HP: 17, Pos: target})

	if p.stats != st {
		t.Fatalf("stats: %+v, expected %+v", p.stats, st)
	}
	if p.hp != 17 {
		t.Fatalf("HP=%d, expected 17", p.hp)
	}
	if p.pos != target {
		t.Fatalf("position %v, expected %v", p.pos, target)
	}
	if g.byPos[target] != p.id {
		t.Fatal("position not indexed")
	}
}

func TestJoinWithoutSavedStartsFresh(t *testing.T) {
	g := newGame()
	p := g.joinWith("new", func(protocol.Snapshot) {}, nil)
	if p.stats != combat.NewStats() || p.hp != p.stats.MaxHP {
		t.Fatalf("unexpected new player: %+v hp=%d", p.stats, p.hp)
	}
}

func TestJoinClampsCorruptSavedState(t *testing.T) {
	g := newGame()
	pos := g.World.FindSpawn()
	cases := []struct {
		name   string
		saved  Saved
		wantHP func(maxHP int) int
	}{
		{"PV nuls", Saved{Stats: combat.StatsAt(3, 0, 1), HP: 0, Pos: pos}, func(int) int { return 1 }},
		{"negative HP", Saved{Stats: combat.StatsAt(3, 0, 1), HP: -40, Pos: pos}, func(int) int { return 1 }},
		{"PV excessifs", Saved{Stats: combat.StatsAt(3, 0, 1), HP: 99999, Pos: pos}, func(m int) int { return m }},
	}
	for _, c := range cases {
		g := newGame()
		p := g.joinWith(c.name, func(protocol.Snapshot) {}, &c.saved)
		if want := c.wantHP(p.stats.MaxHP); p.hp != want {
			t.Errorf("%s: HP=%d, expected %d", c.name, p.hp, want)
		}
	}

	// An inconsistent sheet (level 0, XP/potions out of bounds) is brought back within bounds.
	bad := Saved{Stats: combat.Stats{Level: 0, XP: -5, Potions: 500, MaxHP: 99999}, HP: 5, Pos: pos}
	p := g.joinWith("bad", func(protocol.Snapshot) {}, &bad)
	if p.stats.Level != 1 || p.stats.MaxHP == 99999 || p.stats.Potions != combat.MaxPotions || p.stats.XP != 0 {
		t.Fatalf("sheet not sanitized (the client must not be able to impose MaxHP): %+v", p.stats)
	}
}

func TestJoinSavedPositionOccupiedFallsBackNearby(t *testing.T) {
	g := newGame()
	first := g.joinWith("a", func(protocol.Snapshot) {}, nil)
	second := g.joinWith("b", func(protocol.Snapshot) {}, &Saved{Stats: combat.NewStats(), HP: 10, Pos: first.pos})

	if second.pos == first.pos {
		t.Fatal("two players on the same tile")
	}
	if cheb(second.pos, first.pos) > 2 {
		t.Fatalf("fallback too far: %v (wanted tile %v)", second.pos, first.pos)
	}
	if !g.World.At(second.pos.X, second.pos.Y).Walkable() {
		t.Fatalf("fallback onto an impassable tile: %v", second.pos)
	}
}

func TestPersistOnLeave(t *testing.T) {
	g, sv := gameWithSaves()
	p, _ := joinRec(g, "alice")
	dx, dy := freeDir(t, g, p)
	g.move(p.id, dx, dy)
	g.step()
	p.hp -= 4
	where := p.pos

	g.leave(p.id)
	got, ok := sv.last("alice")
	if !ok {
		t.Fatal("no save on disconnect")
	}
	if got.Pos != where || got.HP != p.hp || got.Stats != p.stats {
		t.Fatalf("saved state %+v, expected pos=%v hp=%d", got, where, p.hp)
	}
}

func TestPersistOnFightEnd(t *testing.T) {
	r := newRig(t, weak(testRat))
	sv := &saves{}
	r.g.Persist = sv.persist
	r.engage()
	before := len(sv.calls)
	r.g.act(r.p, combat.Attack, 0)

	if len(sv.calls) != before+1 {
		t.Fatalf("%d saves at the end of the fight, expected 1", len(sv.calls)-before)
	}
	got, _ := sv.last("hero")
	if got.Stats.XP != testRat.XP {
		t.Fatalf("the save does not contain the XP gained: %+v", got.Stats)
	}
}

func TestPersistNotCalledDuringFightTurns(t *testing.T) {
	r := newRig(t, deadlyButSlow())
	sv := &saves{}
	r.g.Persist = sv.persist
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 3; i++ {
		r.g.act(r.p, combat.Attack, 0)
	}
	if len(sv.calls) != 0 {
		t.Fatalf("%d saves during an ongoing fight, expected 0", len(sv.calls))
	}
}

// deadlyButSlow: a monster that lasts without killing the test player.
func deadlyButSlow() species {
	s := testRat
	s.HP, s.Hit, s.Dmg = 100000, 0, dice.D(1, 1, 0)
	return s
}

func TestAutosaveEveryInterval(t *testing.T) {
	g, sv := gameWithSaves()
	joinRec(g, "alice")
	joinRec(g, "bob")

	for i := 0; i < autosaveEvery-1; i++ {
		g.step()
	}
	if len(sv.calls) != 0 {
		t.Fatalf("save before the deadline: %d calls", len(sv.calls))
	}
	g.step() // tick autosaveEvery
	if _, ok := sv.last("alice"); !ok {
		t.Fatal("alice not saved")
	}
	if _, ok := sv.last("bob"); !ok {
		t.Fatal("bob not saved")
	}
	if len(sv.calls) != 2 {
		t.Fatalf("%d calls, expected 2 (one per player)", len(sv.calls))
	}
}

func TestPersistOnShutdown(t *testing.T) {
	g, sv := gameWithSaves()
	// Persist is called from the game goroutine: we synchronize via a channel.
	got := make(chan string, 8)
	g.Persist = func(name string, s Saved) { got <- name }

	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{})
	go func() { g.Run(ctx); close(ran) }()

	for _, n := range []string{"alice", "bob"} {
		if _, err := g.Join(n, Handlers{Snapshot: func(protocol.Snapshot) {}}); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop did not stop")
	}

	seen := map[string]bool{}
	for len(got) > 0 {
		seen[<-got] = true
	}
	if !seen["alice"] || !seen["bob"] {
		t.Fatalf("saves at shutdown: %v (expected alice and bob)", seen)
	}
	_ = sv
}

func TestNilPersistIsHarmless(t *testing.T) {
	g := newGame() // Persist == nil
	p, _ := joinRec(g, "a")
	for i := 0; i < autosaveEvery+1; i++ {
		g.step()
	}
	g.leave(p.id) // must not panic
}

// Round trip: what is saved, then restored, gives back the same player.
func TestSaveThenRestoreRoundTrip(t *testing.T) {
	g1, sv := gameWithSaves()
	p, _ := joinRec(g1, "alice")
	p.stats.AddXP(25, nil) // level 3
	p.stats.Potions = 4
	p.hp = 11
	dx, dy := freeDir(t, g1, p)
	g1.move(p.id, dx, dy)
	g1.step()
	g1.leave(p.id)

	saved, _ := sv.last("alice")
	g2 := newGame()
	q := g2.joinWith("alice", func(protocol.Snapshot) {}, &saved)
	if q.stats != p.stats || q.hp != p.hp || q.pos != p.pos {
		t.Fatalf("restored: stats=%+v hp=%d pos=%v; expected stats=%+v hp=%d pos=%v",
			q.stats, q.hp, q.pos, p.stats, p.hp, p.pos)
	}
}

// --- Verified facts and consent ---

func TestWinCountsKillsAndBossKills(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Kills != 1 || r.p.stats.BossKills != 0 || r.p.stats.Deaths != 0 {
		t.Fatalf("after an ordinary victory: %+v", r.p.stats)
	}

	boss := speciesNamed(t, "warlord")
	boss.HP = 1
	r = newRig(t, boss)
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Kills != 1 || r.p.stats.BossKills != 1 {
		t.Fatalf("after the victory over the lord: %+v", r.p.stats)
	}
}

func TestLossAndFleeRecord(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.hp = 1
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.stats.Deaths != 1 || r.p.stats.Kills != 0 {
		t.Fatalf("after a defeat: %+v", r.p.stats)
	}

	r = newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.stats.Deaths != 0 || r.p.stats.Kills != 0 {
		t.Fatalf("a flight must not count for anything: %+v", r.p.stats)
	}
}

func TestRecordAndConsentSurviveSaveAndRestore(t *testing.T) {
	g1, sv := gameWithSaves()
	p, _ := joinRec(g1, "alice")
	p.stats = p.stats.WithRecord(31, 2, 5)
	p.consent = true
	g1.leave(p.id)

	saved, ok := sv.last("alice")
	if !ok || !saved.LLMConsent || saved.Stats.Kills != 31 || saved.Stats.BossKills != 2 || saved.Stats.Deaths != 5 {
		t.Fatalf("save: %+v (ok=%v)", saved, ok)
	}

	g2 := newGame()
	q := g2.joinWith("alice", func(protocol.Snapshot) {}, &saved)
	if !q.consent || q.stats.Kills != 31 || q.stats.BossKills != 2 || q.stats.Deaths != 5 {
		t.Fatalf("restoration: consent=%v stats=%+v", q.consent, q.stats)
	}

	// A new player has accepted nothing and has no history.
	n := g2.joinWith("bob", func(protocol.Snapshot) {}, nil)
	if n.consent || n.stats.Kills != 0 {
		t.Fatalf("new player: consent=%v stats=%+v", n.consent, n.stats)
	}
}

func TestRestoredRecordIsClamped(t *testing.T) {
	g := newGame()
	saved := &Saved{Stats: combat.NewStats(), HP: 10, Pos: g.World.FindSpawn()}
	saved.Stats.Kills, saved.Stats.BossKills, saved.Stats.Deaths = -5, -1, -9
	p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
	if p.stats.Kills != 0 || p.stats.BossKills != 0 || p.stats.Deaths != 0 {
		t.Fatalf("negative counters kept: %+v", p.stats)
	}
}

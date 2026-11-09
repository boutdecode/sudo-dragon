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

// saves collecte les appels à Persist.
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
		t.Fatalf("stats: %+v, attendu %+v", p.stats, st)
	}
	if p.hp != 17 {
		t.Fatalf("PV=%d, attendu 17", p.hp)
	}
	if p.pos != target {
		t.Fatalf("position %v, attendu %v", p.pos, target)
	}
	if g.byPos[target] != p.id {
		t.Fatal("position non indexée")
	}
}

func TestJoinWithoutSavedStartsFresh(t *testing.T) {
	g := newGame()
	p := g.joinWith("new", func(protocol.Snapshot) {}, nil)
	if p.stats != combat.NewStats() || p.hp != p.stats.MaxHP {
		t.Fatalf("nouveau joueur inattendu: %+v hp=%d", p.stats, p.hp)
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
		{"PV négatifs", Saved{Stats: combat.StatsAt(3, 0, 1), HP: -40, Pos: pos}, func(int) int { return 1 }},
		{"PV excessifs", Saved{Stats: combat.StatsAt(3, 0, 1), HP: 99999, Pos: pos}, func(m int) int { return m }},
	}
	for _, c := range cases {
		g := newGame()
		p := g.joinWith(c.name, func(protocol.Snapshot) {}, &c.saved)
		if want := c.wantHP(p.stats.MaxHP); p.hp != want {
			t.Errorf("%s: PV=%d, attendu %d", c.name, p.hp, want)
		}
	}

	// Une fiche incohérente (niveau 0, XP/potions hors bornes) est ramenée dans les bornes.
	bad := Saved{Stats: combat.Stats{Level: 0, XP: -5, Potions: 500, MaxHP: 99999}, HP: 5, Pos: pos}
	p := g.joinWith("bad", func(protocol.Snapshot) {}, &bad)
	if p.stats.Level != 1 || p.stats.MaxHP == 99999 || p.stats.Potions != combat.MaxPotions || p.stats.XP != 0 {
		t.Fatalf("fiche non assainie (le client ne doit pas pouvoir imposer MaxHP): %+v", p.stats)
	}
}

func TestJoinSavedPositionOccupiedFallsBackNearby(t *testing.T) {
	g := newGame()
	first := g.joinWith("a", func(protocol.Snapshot) {}, nil)
	second := g.joinWith("b", func(protocol.Snapshot) {}, &Saved{Stats: combat.NewStats(), HP: 10, Pos: first.pos})

	if second.pos == first.pos {
		t.Fatal("deux joueurs sur la même case")
	}
	if cheb(second.pos, first.pos) > 2 {
		t.Fatalf("repli trop lointain: %v (case voulue %v)", second.pos, first.pos)
	}
	if !g.World.At(second.pos.X, second.pos.Y).Walkable() {
		t.Fatalf("repli sur une case infranchissable: %v", second.pos)
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
		t.Fatal("aucune sauvegarde à la déconnexion")
	}
	if got.Pos != where || got.HP != p.hp || got.Stats != p.stats {
		t.Fatalf("état sauvegardé %+v, attendu pos=%v hp=%d", got, where, p.hp)
	}
}

func TestPersistOnFightEnd(t *testing.T) {
	r := newRig(t, weak(testRat))
	sv := &saves{}
	r.g.Persist = sv.persist
	r.engage()
	before := len(sv.calls)
	r.g.act(r.p, combat.Attack)

	if len(sv.calls) != before+1 {
		t.Fatalf("%d sauvegardes à la fin du combat, attendu 1", len(sv.calls)-before)
	}
	got, _ := sv.last("hero")
	if got.Stats.XP != testRat.XP {
		t.Fatalf("la sauvegarde ne contient pas l'XP gagnée: %+v", got.Stats)
	}
}

func TestPersistNotCalledDuringFightTurns(t *testing.T) {
	r := newRig(t, deadlyButSlow())
	sv := &saves{}
	r.g.Persist = sv.persist
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 3; i++ {
		r.g.act(r.p, combat.Attack)
	}
	if len(sv.calls) != 0 {
		t.Fatalf("%d sauvegardes pendant un combat en cours, attendu 0", len(sv.calls))
	}
}

// deadlyButSlow: un monstre qui dure sans faire mourir le joueur de test.
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
		t.Fatalf("sauvegarde avant l'échéance: %d appels", len(sv.calls))
	}
	g.step() // tick autosaveEvery
	if _, ok := sv.last("alice"); !ok {
		t.Fatal("alice non sauvegardée")
	}
	if _, ok := sv.last("bob"); !ok {
		t.Fatal("bob non sauvegardé")
	}
	if len(sv.calls) != 2 {
		t.Fatalf("%d appels, attendu 2 (un par joueur)", len(sv.calls))
	}
}

func TestPersistOnShutdown(t *testing.T) {
	g, sv := gameWithSaves()
	// Persist est appelé depuis la goroutine de jeu: on synchronise via un canal.
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
		t.Fatal("la boucle ne s'est pas arrêtée")
	}

	seen := map[string]bool{}
	for len(got) > 0 {
		seen[<-got] = true
	}
	if !seen["alice"] || !seen["bob"] {
		t.Fatalf("sauvegardes à l'arrêt: %v (attendu alice et bob)", seen)
	}
	_ = sv
}

func TestNilPersistIsHarmless(t *testing.T) {
	g := newGame() // Persist == nil
	p, _ := joinRec(g, "a")
	for i := 0; i < autosaveEvery+1; i++ {
		g.step()
	}
	g.leave(p.id) // ne doit pas paniquer
}

// Aller-retour: ce qui est sauvegardé, puis restauré, redonne le même joueur.
func TestSaveThenRestoreRoundTrip(t *testing.T) {
	g1, sv := gameWithSaves()
	p, _ := joinRec(g1, "alice")
	p.stats.AddXP(25, nil) // niveau 3
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
		t.Fatalf("restauré: stats=%+v hp=%d pos=%v; attendu stats=%+v hp=%d pos=%v",
			q.stats, q.hp, q.pos, p.stats, p.hp, p.pos)
	}
}

// --- Faits vérifiés et accord ---

func TestWinCountsKillsAndBossKills(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Kills != 1 || r.p.stats.BossKills != 0 || r.p.stats.Deaths != 0 {
		t.Fatalf("après une victoire ordinaire: %+v", r.p.stats)
	}

	boss := speciesNamed(t, "warlord")
	boss.HP = 1
	r = newRig(t, boss)
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Kills != 1 || r.p.stats.BossKills != 1 {
		t.Fatalf("après la victoire sur le seigneur: %+v", r.p.stats)
	}
}

func TestLossAndFleeRecord(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.hp = 1
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Deaths != 1 || r.p.stats.Kills != 0 {
		t.Fatalf("après une défaite: %+v", r.p.stats)
	}

	r = newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee)
	}
	if r.p.stats.Deaths != 0 || r.p.stats.Kills != 0 {
		t.Fatalf("une fuite ne doit rien compter: %+v", r.p.stats)
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
		t.Fatalf("sauvegarde: %+v (ok=%v)", saved, ok)
	}

	g2 := newGame()
	q := g2.joinWith("alice", func(protocol.Snapshot) {}, &saved)
	if !q.consent || q.stats.Kills != 31 || q.stats.BossKills != 2 || q.stats.Deaths != 5 {
		t.Fatalf("restauration: consent=%v stats=%+v", q.consent, q.stats)
	}

	// Un nouveau joueur n'a rien accepté et n'a pas d'histoire.
	n := g2.joinWith("bob", func(protocol.Snapshot) {}, nil)
	if n.consent || n.stats.Kills != 0 {
		t.Fatalf("nouveau joueur: consent=%v stats=%+v", n.consent, n.stats)
	}
}

func TestRestoredRecordIsClamped(t *testing.T) {
	g := newGame()
	saved := &Saved{Stats: combat.NewStats(), HP: 10, Pos: g.World.FindSpawn()}
	saved.Stats.Kills, saved.Stats.BossKills, saved.Stats.Deaths = -5, -1, -9
	p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
	if p.stats.Kills != 0 || p.stats.BossKills != 0 || p.stats.Deaths != 0 {
		t.Fatalf("compteurs négatifs conservés: %+v", p.stats)
	}
}

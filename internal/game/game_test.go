package game

import (
	"context"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// recorder collecte les instantanés reçus par un joueur.
type recorder struct{ snaps []protocol.Snapshot }

func (r *recorder) send(s protocol.Snapshot) { r.snaps = append(r.snaps, s) }
func (r *recorder) last() protocol.Snapshot  { return r.snaps[len(r.snaps)-1] }

func newGame() *Game { return New(world.New(1)) }

func joinRec(g *Game, name string) (*player, *recorder) {
	r := &recorder{}
	return g.join(name, r.send), r
}

// place téléporte un joueur (aide de test).
func place(g *Game, p *player, pos world.Pos) { g.setPos(p, pos) }

// freeDir renvoie une direction cardinale franchissable depuis p.
func freeDir(t *testing.T, g *Game, p *player) (int, int) {
	t.Helper()
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if !g.blocked(world.Pos{X: p.pos.X + d[0], Y: p.pos.Y + d[1]}) {
			return d[0], d[1]
		}
	}
	t.Fatal("aucune direction libre")
	return 0, 0
}

func TestMoveAndCooldown(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	start := p.pos
	dx, dy := freeDir(t, g, p)

	g.move(p.id, dx, dy)
	g.step()
	if p.pos != (world.Pos{X: start.X + dx, Y: start.Y + dy}) {
		t.Fatalf("pas de déplacement: %v -> %v", start, p.pos)
	}
	after := p.pos

	// Une nouvelle intention est retenue tant que le cooldown court.
	g.move(p.id, dx, dy)
	for i := 0; i < MoveCooldown-1; i++ {
		g.step()
		if p.pos != after {
			t.Fatalf("déplacement pendant le cooldown (tick %d)", i)
		}
	}
	g.step()
	if p.pos == after && !g.blocked(world.Pos{X: after.X + dx, Y: after.Y + dy}) {
		t.Fatal("l'intention retenue n'a pas été appliquée après le cooldown")
	}
}

func TestMoveClampedAndZeroIgnored(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	g.move(p.id, 0, 0)
	if p.hasPending {
		t.Fatal("intention nulle acceptée")
	}
	g.move(p.id, 99, -99)
	if p.pendingDX != 1 || p.pendingDY != -1 {
		t.Fatalf("pas borné: %d,%d", p.pendingDX, p.pendingDY)
	}
}

func TestBlockedByTerrain(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	// Cherche une case praticable voisine d'une case infranchissable.
	for x := -200; x < 200; x++ {
		for y := -200; y < 200; y++ {
			// On veut (x,y) praticable et (x+1,y) infranchissable.
			if !g.World.At(x, y).Walkable() || g.World.At(x+1, y).Walkable() {
				continue
			}
			place(g, p, world.Pos{X: x, Y: y})
			g.move(p.id, 1, 0)
			g.step()
			if p.pos != (world.Pos{X: x, Y: y}) {
				t.Fatalf("traversée d'une tuile infranchissable: %v", p.pos)
			}
			return
		}
	}
	t.Skip("aucune bordure trouvée")
}

func TestBlockedByPlayer(t *testing.T) {
	g := newGame()
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	dx, dy := freeDir(t, g, a)
	target := world.Pos{X: a.pos.X + dx, Y: a.pos.Y + dy}
	place(g, b, target)

	before := a.pos
	g.move(a.id, dx, dy)
	g.step()
	if a.pos != before {
		t.Fatalf("a traversé b: %v", a.pos)
	}
	if g.byPos[target] != b.id {
		t.Fatal("index des positions corrompu")
	}
}

func TestSpawnsDoNotOverlap(t *testing.T) {
	g := newGame()
	seen := map[world.Pos]bool{}
	for i := 0; i < 30; i++ {
		p, _ := joinRec(g, "p")
		if seen[p.pos] {
			t.Fatalf("deux joueurs sur %v", p.pos)
		}
		seen[p.pos] = true
		if !g.World.At(p.pos.X, p.pos.Y).Walkable() {
			t.Fatalf("spawn non praticable: %v", p.pos)
		}
	}
}

func TestAreaOfInterest(t *testing.T) {
	g := newGame()
	a, ra := joinRec(g, "a")
	b, _ := joinRec(g, "near")
	c, _ := joinRec(g, "far")
	place(g, b, world.Pos{X: a.pos.X + protocol.ViewHalfW, Y: a.pos.Y})
	place(g, c, world.Pos{X: a.pos.X + protocol.ViewHalfW + 1, Y: a.pos.Y})

	g.step()
	snap := ra.last()
	if len(snap.Others) != 1 || snap.Others[0].ID != b.id {
		t.Fatalf("zone de vision incorrecte: %+v", snap.Others)
	}
	if snap.Self.ID != a.id {
		t.Fatalf("self incorrect: %+v", snap.Self)
	}
}

func TestNearby(t *testing.T) {
	g := newGame()
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "near")
	c, _ := joinRec(g, "far")
	place(g, b, world.Pos{X: a.pos.X, Y: a.pos.Y + protocol.ViewHalfH})
	place(g, c, world.Pos{X: a.pos.X, Y: a.pos.Y + protocol.ViewHalfH + 1})

	got := g.nearby(a.id)
	if len(got) != 2 || got[0] != a.id || got[1] != b.id {
		t.Fatalf("nearby(a) = %v, attendu [%d %d] (soi-même inclus, c exclu)", got, a.id, b.id)
	}
	// c est collé à b (il le voit) mais trop loin de a, et la portée est symétrique.
	if got := g.nearby(c.id); len(got) != 2 || got[0] != b.id || got[1] != c.id {
		t.Fatalf("nearby(c) = %v, attendu [%d %d] (a exclu)", got, b.id, c.id)
	}
	if g.nearby(999) != nil {
		t.Fatal("joueur inconnu: liste vide attendue")
	}
}

func TestSnapshotOnlyOnChange(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	g.step()
	if len(r.snaps) != 1 {
		t.Fatalf("premier instantané attendu, reçu %d", len(r.snaps))
	}
	g.step()
	g.step()
	if len(r.snaps) != 1 {
		t.Fatalf("instantané renvoyé sans changement (%d)", len(r.snaps))
	}
	dx, dy := freeDir(t, g, p)
	g.move(p.id, dx, dy)
	g.step()
	if len(r.snaps) != 2 {
		t.Fatalf("instantané attendu après déplacement, total %d", len(r.snaps))
	}
}

func TestOtherPlayerMoveNotifiesObserver(t *testing.T) {
	g := newGame()
	a, ra := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.step()
	n := len(ra.snaps)
	dx, dy := freeDir(t, g, b)
	g.move(b.id, dx, dy)
	g.step()
	if len(ra.snaps) != n+1 {
		t.Fatal("a n'a pas été notifié du déplacement de b")
	}
	_ = a
}

func TestLeaveFreesTileAndNotifies(t *testing.T) {
	g := newGame()
	a, ra := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.step()
	if len(ra.last().Others) != 1 {
		t.Fatal("b devrait être visible")
	}
	pos := b.pos
	g.leave(b.id)
	if _, ok := g.byPos[pos]; ok {
		t.Fatal("case non libérée")
	}
	g.step()
	if len(ra.last().Others) != 0 {
		t.Fatal("b encore visible après départ")
	}
	_ = a
}

// join() n'envoie aucun instantané: seul step() le fait, après onJoined.
// Le serveur s'appuie là-dessus pour que welcome précède le premier snapshot.
func TestJoinSendsNoSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	if len(r.snaps) != 0 || p.id == 0 {
		t.Fatal("aucun instantané ne doit partir pendant join")
	}
}

func TestMetricsRecordTicksAndGauges(t *testing.T) {
	g := newGame()
	joinRec(g, "a")
	joinRec(g, "b")
	g.addMonster(testRat, g.World.FindSpawn())

	for i := 0; i < 5; i++ {
		g.timedStep()
	}
	m := g.Metrics()
	if m.Ticks != 5 {
		t.Fatalf("Ticks=%d, attendu 5", m.Ticks)
	}
	if m.Players != 2 || m.Monsters < 1 {
		t.Fatalf("jauges: joueurs=%d monstres=%d", m.Players, m.Monsters)
	}
	if m.TickTotal <= 0 || m.TickMax <= 0 || m.TickMax > m.TickTotal {
		t.Fatalf("durées incohérentes: total=%v max=%v", m.TickTotal, m.TickMax)
	}
	if m.SlowTicks != 0 {
		t.Fatalf("%d ticks lents sur une charge triviale", m.SlowTicks)
	}
}

func TestMetricsSlowTickAndMax(t *testing.T) {
	g := newGame()
	g.record(10 * time.Millisecond)
	g.record(TickInterval + time.Millisecond) // trop long
	g.record(30 * time.Millisecond)
	m := g.Metrics()
	if m.SlowTicks != 1 {
		t.Fatalf("SlowTicks=%d, attendu 1", m.SlowTicks)
	}
	if m.TickMax != TickInterval+time.Millisecond {
		t.Fatalf("TickMax=%v, le maximum doit être conservé", m.TickMax)
	}
	if want := 10*time.Millisecond + TickInterval + time.Millisecond + 30*time.Millisecond; m.TickTotal != want {
		t.Fatalf("TickTotal=%v, attendu %v", m.TickTotal, want)
	}
}

// Les métriques sont lisibles pendant que la boucle tourne (go test -race).
func TestMetricsReadableWhileRunning(t *testing.T) {
	g := newGame()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.Run(ctx); close(done) }()
	if _, err := g.Join("a", Handlers{Snapshot: func(protocol.Snapshot) {}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for g.Metrics().Ticks < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if m := g.Metrics(); m.Ticks < 3 || m.Players != 1 {
		t.Fatalf("après exécution: %+v", m)
	}
}

func TestMetricsRecentWindowSummarizes(t *testing.T) {
	g := newGame()
	for i := 1; i <= 200; i++ {
		d := time.Millisecond
		if i == 10 {
			d = 50 * time.Millisecond // une valeur aberrante
		}
		g.record(d)
	}
	m := g.Metrics()
	if m.RecentMax != 50*time.Millisecond {
		t.Fatalf("RecentMax=%v, attendu 50ms", m.RecentMax)
	}
	if m.RecentP99 != time.Millisecond {
		t.Fatalf("RecentP99=%v: une valeur aberrante sur 200 ne doit pas fixer le p99", m.RecentP99)
	}
}

// Une valeur aberrante sort de la fenêtre au bout de 60 s (600 ticks), alors
// que le maximum « depuis le démarrage » la garde.
func TestMetricsRecentWindowForgetsOldSpikes(t *testing.T) {
	g := newGame()
	for i := 1; i <= 1300; i++ {
		d := time.Millisecond
		if i == 5 {
			d = 80 * time.Millisecond
		}
		g.record(d)
	}
	m := g.Metrics()
	if m.RecentMax != time.Millisecond {
		t.Fatalf("RecentMax=%v: la pointe ancienne aurait dû sortir de la fenêtre", m.RecentMax)
	}
	if m.TickMax != 80*time.Millisecond {
		t.Fatalf("TickMax=%v: le maximum global doit conserver la pointe", m.TickMax)
	}
}

func TestSortedIDsIsCachedAndInvalidated(t *testing.T) {
	g := newGame()
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")

	first := g.sortedIDs()
	if len(first) != 2 || first[0] != a.id || first[1] != b.id {
		t.Fatalf("ids = %v", first)
	}
	// Même tranche tant que rien ne change: pas de nouveau tri.
	if second := g.sortedIDs(); &second[0] != &first[0] {
		t.Fatal("la liste triée n'est pas mise en cache")
	}

	c, _ := joinRec(g, "c")
	if got := g.sortedIDs(); len(got) != 3 || got[2] != c.id {
		t.Fatalf("après arrivée: %v", got)
	}
	g.leave(a.id)
	if got := g.sortedIDs(); len(got) != 2 || got[0] != b.id || got[1] != c.id {
		t.Fatalf("après départ: %v", got)
	}
}

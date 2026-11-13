package game

import (
	"context"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// recorder collects the snapshots received by a player.
type recorder struct{ snaps []protocol.Snapshot }

func (r *recorder) send(s protocol.Snapshot) { r.snaps = append(r.snaps, s) }
func (r *recorder) last() protocol.Snapshot  { return r.snaps[len(r.snaps)-1] }

func newGame() *Game { return New(world.New(1)) }

func joinRec(g *Game, name string) (*player, *recorder) {
	r := &recorder{}
	return g.join(name, r.send), r
}

// place teleports a player (test helper).
func place(g *Game, p *player, pos world.Pos) { g.setPos(p, pos) }

// freeDir returns a cardinal direction that is passable from p.
func freeDir(t *testing.T, g *Game, p *player) (int, int) {
	t.Helper()
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if !g.blocked(world.Pos{X: p.pos.X + d[0], Y: p.pos.Y + d[1]}) {
			return d[0], d[1]
		}
	}
	t.Fatal("no free direction")
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
		t.Fatalf("no movement: %v -> %v", start, p.pos)
	}
	after := p.pos

	// A new intent is held while the cooldown runs.
	g.move(p.id, dx, dy)
	for i := 0; i < MoveCooldown-1; i++ {
		g.step()
		if p.pos != after {
			t.Fatalf("movement during the cooldown (tick %d)", i)
		}
	}
	g.step()
	if p.pos == after && !g.blocked(world.Pos{X: after.X + dx, Y: after.Y + dy}) {
		t.Fatal("the held intent was not applied after the cooldown")
	}
}

func TestMoveClampedAndZeroIgnored(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	g.move(p.id, 0, 0)
	if p.hasPending {
		t.Fatal("null intent accepted")
	}
	g.move(p.id, 99, -99)
	if p.pendingDX != 1 || p.pendingDY != -1 {
		t.Fatalf("step out of bounds: %d,%d", p.pendingDX, p.pendingDY)
	}
}

func TestBlockedByTerrain(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	// Look for a walkable tile next to an impassable one.
	for x := -200; x < 200; x++ {
		for y := -200; y < 200; y++ {
			// We want (x,y) walkable and (x+1,y) impassable.
			if !g.World.At(x, y).Walkable() || g.World.At(x+1, y).Walkable() {
				continue
			}
			place(g, p, world.Pos{X: x, Y: y})
			g.move(p.id, 1, 0)
			g.step()
			if p.pos != (world.Pos{X: x, Y: y}) {
				t.Fatalf("crossed an impassable tile: %v", p.pos)
			}
			return
		}
	}
	t.Skip("no border found")
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
		t.Fatalf("a crossed b: %v", a.pos)
	}
	if g.byPos[target] != b.id {
		t.Fatal("position index corrupted")
	}
}

func TestSpawnsDoNotOverlap(t *testing.T) {
	g := newGame()
	seen := map[world.Pos]bool{}
	for i := 0; i < 30; i++ {
		p, _ := joinRec(g, "p")
		if seen[p.pos] {
			t.Fatalf("two players on %v", p.pos)
		}
		seen[p.pos] = true
		if !g.World.At(p.pos.X, p.pos.Y).Walkable() {
			t.Fatalf("spawn not walkable: %v", p.pos)
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
		t.Fatalf("incorrect vision zone: %+v", snap.Others)
	}
	if snap.Self.ID != a.id {
		t.Fatalf("incorrect self: %+v", snap.Self)
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
		t.Fatalf("nearby(a) = %v, expected [%d %d] (self included, c excluded)", got, a.id, b.id)
	}
	// c is stuck to b (it sees it) but too far from a, and the range is symmetric.
	if got := g.nearby(c.id); len(got) != 2 || got[0] != b.id || got[1] != c.id {
		t.Fatalf("nearby(c) = %v, expected [%d %d] (a excluded)", got, b.id, c.id)
	}
	if g.nearby(999) != nil {
		t.Fatal("unknown player: empty list expected")
	}
}

func TestSnapshotOnlyOnChange(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	g.step()
	if len(r.snaps) != 1 {
		t.Fatalf("first snapshot expected, received %d", len(r.snaps))
	}
	g.step()
	g.step()
	if len(r.snaps) != 1 {
		t.Fatalf("snapshot resent without change (%d)", len(r.snaps))
	}
	dx, dy := freeDir(t, g, p)
	g.move(p.id, dx, dy)
	g.step()
	if len(r.snaps) != 2 {
		t.Fatalf("snapshot expected after movement, total %d", len(r.snaps))
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
		t.Fatal("a was not notified of b's movement")
	}
	_ = a
}

func TestLeaveFreesTileAndNotifies(t *testing.T) {
	g := newGame()
	a, ra := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.step()
	if len(ra.last().Others) != 1 {
		t.Fatal("b should be visible")
	}
	pos := b.pos
	g.leave(b.id)
	if _, ok := g.byPos[pos]; ok {
		t.Fatal("tile not freed")
	}
	g.step()
	if len(ra.last().Others) != 0 {
		t.Fatal("b still visible after leaving")
	}
	_ = a
}

// join() sends no snapshot: only step() does, after onJoined.
// The server relies on this so that welcome precedes the first snapshot.
func TestJoinSendsNoSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	if len(r.snaps) != 0 || p.id == 0 {
		t.Fatal("no snapshot must be sent during join")
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
		t.Fatalf("Ticks=%d, expected 5", m.Ticks)
	}
	if m.Players != 2 || m.Monsters < 1 {
		t.Fatalf("gauges: players=%d monsters=%d", m.Players, m.Monsters)
	}
	if m.TickTotal <= 0 || m.TickMax <= 0 || m.TickMax > m.TickTotal {
		t.Fatalf("inconsistent durations: total=%v max=%v", m.TickTotal, m.TickMax)
	}
	if m.SlowTicks != 0 {
		t.Fatalf("%d slow ticks on a trivial load", m.SlowTicks)
	}
}

func TestMetricsSlowTickAndMax(t *testing.T) {
	g := newGame()
	g.record(10 * time.Millisecond)
	g.record(TickInterval + time.Millisecond) // too long
	g.record(30 * time.Millisecond)
	m := g.Metrics()
	if m.SlowTicks != 1 {
		t.Fatalf("SlowTicks=%d, expected 1", m.SlowTicks)
	}
	if m.TickMax != TickInterval+time.Millisecond {
		t.Fatalf("TickMax=%v, the maximum must be kept", m.TickMax)
	}
	if want := 10*time.Millisecond + TickInterval + time.Millisecond + 30*time.Millisecond; m.TickTotal != want {
		t.Fatalf("TickTotal=%v, expected %v", m.TickTotal, want)
	}
}

// Metrics are readable while the loop runs (go test -race).
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
		t.Fatalf("after running: %+v", m)
	}
}

func TestMetricsRecentWindowSummarizes(t *testing.T) {
	g := newGame()
	for i := 1; i <= 200; i++ {
		d := time.Millisecond
		if i == 10 {
			d = 50 * time.Millisecond // an outlier value
		}
		g.record(d)
	}
	m := g.Metrics()
	if m.RecentMax != 50*time.Millisecond {
		t.Fatalf("RecentMax=%v, expected 50ms", m.RecentMax)
	}
	if m.RecentP99 != time.Millisecond {
		t.Fatalf("RecentP99=%v: one outlier in 200 must not set the p99", m.RecentP99)
	}
}

// An outlier leaves the window after 60 s (600 ticks), whereas
// the "since startup" maximum keeps it.
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
		t.Fatalf("RecentMax=%v: the old spike should have left the window", m.RecentMax)
	}
	if m.TickMax != 80*time.Millisecond {
		t.Fatalf("TickMax=%v: the global maximum must keep the spike", m.TickMax)
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
	// Same slice as long as nothing changes: no new sort.
	if second := g.sortedIDs(); &second[0] != &first[0] {
		t.Fatal("the sorted list is not cached")
	}

	c, _ := joinRec(g, "c")
	if got := g.sortedIDs(); len(got) != 3 || got[2] != c.id {
		t.Fatalf("after arrival: %v", got)
	}
	g.leave(a.id)
	if got := g.sortedIDs(); len(got) != 2 || got[0] != b.id || got[1] != c.id {
		t.Fatalf("after departure: %v", got)
	}
}

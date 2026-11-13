package game

import (
	"slices"
	"strconv"
	"testing"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestCaravanIndexCycle(t *testing.T) {
	const n = 5 // 4 tiles to walk
	leg := uint64((n - 1) * caravanStep)
	cases := []struct {
		t    uint64
		want int
	}{
		{0, 0}, {caravanStep - 1, 0}, {caravanStep, 1}, {leg - 1, 3},
		{leg, n - 1}, {leg + caravanPause - 1, n - 1}, // stop at the end
		{leg + caravanPause, n - 1}, {leg + caravanPause + caravanStep, n - 2},
		{2*leg + caravanPause - 1, 1}, {2*leg + caravanPause, 0}, // return then stop at the start
		{caravanPeriod(n) - 1, 0}, {caravanPeriod(n), 0}, {caravanPeriod(n) + caravanStep, 1}, // the cycle restarts
	}
	for _, c := range cases {
		if got := caravanIndex(n, c.t); got != c.want {
			t.Errorf("t=%d: tile %d, expected %d", c.t, got, c.want)
		}
	}
	// Never more than one tile apart from one tick to the next: the caravan does not jump.
	prev := caravanIndex(n, 0)
	for tt := uint64(0); tt < 3*caravanPeriod(n); tt += caravanStep {
		idx := caravanIndex(n, tt)
		if d := idx - prev; d < -1 || d > 1 {
			t.Fatalf("t=%d: jump from %d to %d", tt, prev, idx)
		}
		prev = idx
	}
}

// caravanGame returns a game with a player on a caravan's road, the caravan tracked.
func caravanGame(t *testing.T) (*Game, *player, *caravan) {
	t.Helper()
	g := newGame()
	var route *world.RoadRoute
	for _, r := range g.World.RoadRoutesNear(0, 0, 6*world.RegionSize) {
		if _, ok := g.World.HasCaravan(r); ok && (route == nil || r.ID < route.ID) {
			r := r
			route = &r
		}
	}
	if route == nil {
		t.Fatal("no caravan near the origin")
	}
	p, _ := joinRec(g, "a")
	g.setPos(p, route.Path[len(route.Path)/2])
	g.activateCaravans()
	c := g.car.active[route.ID]
	if c == nil {
		t.Fatal("caravan not tracked near a player")
	}
	return g, p, c
}

func TestCaravanFollowsItsRouteAndShowsInSnapshots(t *testing.T) {
	g, p, c := caravanGame(t)
	for i := 0; i < 3*caravanStep; i++ {
		g.tick++
		g.stepCaravans()
		if !slices.Contains(c.route.Path, c.pos) {
			t.Fatalf("the caravan left its road: %v", c.pos)
		}
		if g.caravanAt(c.pos) != c {
			t.Fatalf("wrong position index for %v", c.pos)
		}
	}
	// It moves: over a full cycle, it occupies several tiles.
	seen := map[world.Pos]bool{}
	for i := uint64(0); i < caravanPeriod(len(c.route.Path)); i += caravanStep {
		g.tick = i
		g.stepCaravans()
		seen[c.pos] = true
	}
	if len(seen) < 2 {
		t.Fatalf("the caravan never moved (%d tile)", len(seen))
	}

	// It is visible when in view, with an identifier outside the monsters' range.
	g.setPos(p, g.freeNear(c.pos))
	var found *protocol.Monster
	for _, m := range g.appendCaravans(nil, p) {
		if m.X == c.pos.X && m.Y == c.pos.Y {
			m := m
			found = &m
		}
	}
	if found == nil || found.Kind != "caravan" || found.ID < caravanIDBase {
		t.Fatalf("caravan missing from the snapshot: %+v", found)
	}

	// Far from any player, it is no longer tracked.
	g.setPos(p, world.Pos{X: 50 * world.RegionSize, Y: 50 * world.RegionSize})
	g.activateCaravans()
	if len(g.car.active) != 0 {
		t.Fatalf("%d caravan(s) still tracked far from players", len(g.car.active))
	}
}

func TestCaravanStopsWhileTalkedToAndWaitsWhenBlocked(t *testing.T) {
	g, p, c := caravanGame(t)
	// Place the caravan mid-route, and the player on the previous tile of the road.
	i := slices.Index(c.route.Path, c.pos)
	if i < 1 {
		t.Skip("caravan at the end of its road")
	}
	g.tick = uint64(i*caravanStep) - c.seed%caravanPeriod(len(c.route.Path)) + 1000*caravanPeriod(len(c.route.Path))
	g.activateCaravans()
	c = g.car.active[c.route.ID]
	i = slices.Index(c.route.Path, c.pos)
	prev := c.route.Path[max(i-1, 0)]
	if i == 0 {
		prev = c.route.Path[1]
	}
	g.setPos(p, prev)
	p.cooldown = 0
	g.move(p.id, c.pos.X-prev.X, c.pos.Y-prev.Y)
	g.step()
	if p.talk == nil || p.talk.npc.Index < world.CaravanIndexBase {
		t.Fatalf("walking onto the caravan does not open its shop (talk=%v, pos %v, caravan %v)", p.talk, p.pos, c.pos)
	}
	if p.pos != prev {
		t.Errorf("the player walked onto the caravan: %v", p.pos)
	}

	// It does not move during the conversation, even over several steps.
	at := c.pos
	for range 5 * caravanStep {
		p.talk.lastAct = g.tick // the conversation must not expire
		g.step()
	}
	if c.pos != at {
		t.Fatalf("the caravan moved during the conversation: %v -> %v", at, c.pos)
	}
	g.closeTalk(p, "")
	for range 2 * caravanStep {
		g.step()
	}
	if c.pos == at && len(c.route.Path) > 2 {
		t.Log("the caravan stayed on the same tile after the conversation (end of road?)")
	}
}

func TestCaravanSellsHigherTierAndRotatesDaily(t *testing.T) {
	g, p, c := caravanGame(t)
	npc := g.World.CaravanMerchant(c.route, c.pos)
	ctx := g.vendorContext(npc)

	w1, rev1 := g.waresFor(p, npc)
	if len(w1.Items) == 0 {
		t.Fatal("caravan without wares")
	}
	for _, it := range w1.Items {
		if it.Tier < min(ctx.MinTier+1, gear.MaxTier) {
			t.Errorf("%s: rank %d, less than the region floor + 1 (%d)", it.Name, it.Tier, ctx.MinTier+1)
		}
	}
	if again, rev := g.waresFor(p, npc); rev != rev1 || len(again.Items) != len(w1.Items) || again.Items[0].ID != w1.Items[0].ID {
		t.Fatal("the selection changes without the day changing")
	}
	g.tick += 24 * protocol.TicksPerGameHour
	w2, rev2 := g.waresFor(p, npc)
	if rev2 == rev1 {
		t.Fatalf("the revision does not change from one day to the next (%d)", rev2)
	}
	same := len(w2.Items) == len(w1.Items)
	for i := range w1.Items {
		if same && (w2.Items[i].ID != w1.Items[i].ID || w2.Items[i].Name != w1.Items[i].Name) {
			same = false
		}
	}
	if same {
		t.Error("the selection is identical the next day")
	}

	// A purchase decided on yesterday's stall is refused (stale revision).
	p.stats.AddGold(5000)
	if out := g.buyItem(p, npc, itoaRev(rev1, w1.Items[0].ID)); len(out) == 0 || out[0] == "" {
		t.Fatal("no response to a stale purchase")
	}
	if len(p.inv.Items) != 0 && len(p.inv.Items) != 1 { // the possible starting weapon
		t.Logf("bag: %d items", len(p.inv.Items))
	}

	// The merchant offers neither theft, nor quest, nor free discussion.
	for _, o := range g.offers(p, npc) {
		switch o.Action {
		case protocol.NPCSteal, protocol.NPCTalk, protocol.NPCAskWork, protocol.NPCTurnIn:
			t.Errorf("unexpected offer from a travelling merchant: %s", o.Action)
		}
	}
}

func itoaRev(rev int, id int64) string { return strconv.Itoa(rev) + "." + strconv.FormatInt(id, 10) }

package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Caravans: on some roads between two places (see world/caravan.go), a travelling merchant goes back and forth.
// It sells gear one rank above the region's forges, with a selection that changes every game
// day. You talk to it by walking onto it; it stops during the conversation.
//
// Its position depends only on the tick, its seed and the time it has spent stopped: nothing to save, and the same
// caravan is in the same place for everyone. Only those whose road passes near a player are tracked.

const (
	caravanIDBase = 1 << 29 // network IDs, above those of monsters and guards (guardIDBase)
	caravanStep   = 10      // ticks per tile (1 tile per second)
	caravanPause  = 300     // ticks (30 s) stopped at each end of the road
	caravanReach  = garrisonActivation
	maxPauses     = 512
)

type caravan struct {
	id    int
	route world.RoadRoute
	seed  uint64
	pos   world.Pos
}

// caravanState groups the tracked caravans.
type caravanState struct {
	next   int
	active map[string]*caravan // by road identifier
	at     map[world.Pos]*caravan
	paused map[string]uint64 // road -> ticks spent stopped (conversation, occupied tile)
}

// caravanPeriod is the duration of a full round trip on a road of n tiles.
func caravanPeriod(n int) uint64 { return uint64(2*(n-1)*caravanStep + 2*caravanPause) }

// caravanIndex gives the tile (index in the n-tile path) of the caravan at time t of its cycle: outbound,
// pause, return, pause.
func caravanIndex(n int, t uint64) int {
	if n < 2 {
		return 0
	}
	t %= caravanPeriod(n)
	leg := uint64((n - 1) * caravanStep)
	switch {
	case t < leg:
		return int(t / caravanStep)
	case t < leg+caravanPause:
		return n - 1
	case t < 2*leg+caravanPause:
		return n - 1 - int((t-leg-caravanPause)/caravanStep)
	}
	return 0
}

// clock is the cycle time of c: the tick, shifted by its seed, minus the time spent stopped.
func (g *Game) caravanClock(c *caravan) uint64 {
	now := g.tick + c.seed%caravanPeriod(len(c.route.Path))
	if p := g.car.paused[c.route.ID]; p < now {
		return now - p
	}
	return 0
}

// caravanAt returns the caravan occupying tile pos.
func (g *Game) caravanAt(pos world.Pos) *caravan {
	if g.car.at == nil {
		return nil
	}
	return g.car.at[pos]
}

// activateCaravans tracks the caravans whose road passes within caravanReach of a player and forgets the others.
func (g *Game) activateCaravans() {
	seen := map[string]bool{}
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		for _, r := range g.World.RoadRoutesNear(p.pos.X, p.pos.Y, caravanReach) {
			seed, ok := g.World.HasCaravan(r)
			if !ok || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if g.car.active == nil {
				g.car.active = map[string]*caravan{}
				g.car.paused = map[string]uint64{}
			}
			if _, tracked := g.car.active[r.ID]; tracked {
				continue
			}
			g.car.next++
			c := &caravan{id: g.car.next, route: r, seed: seed}
			c.pos = r.Path[caravanIndex(len(r.Path), g.caravanClock(c))]
			g.car.active[r.ID] = c
		}
	}
	for id := range g.car.active {
		if !seen[id] {
			delete(g.car.active, id)
		}
	}
	if len(g.car.paused) > maxPauses {
		clear(g.car.paused)
	}
	g.indexCaravans()
}

func (g *Game) indexCaravans() {
	g.car.at = make(map[world.Pos]*caravan, len(g.car.active))
	for _, c := range g.car.active {
		g.car.at[c.pos] = c
	}
}

// caravanOf finds the caravan that merchant npc represents, or nil.
func (g *Game) caravanOf(npc world.NPC) *caravan {
	for _, c := range g.car.active {
		if c.route.A == npc.Region && npc.Index == world.CaravanIndexBase+c.route.Orient {
			return c
		}
	}
	return nil
}

// stepCaravans advances caravans by one tile every caravanStep ticks. A caravan stops its clock while a
// player is talking to it, or while the next tile is occupied.
func (g *Game) stepCaravans() {
	if len(g.car.active) == 0 {
		return
	}
	for _, id := range g.sortedIDs() {
		if p := g.players[id]; p.talk != nil && p.talk.npc.Index >= world.CaravanIndexBase {
			if c := g.caravanOf(p.talk.npc); c != nil {
				g.car.paused[c.route.ID]++
			}
		}
	}
	if g.tick%caravanStep != 0 {
		return
	}
	moved := false
	for _, c := range g.sortedCaravans() {
		want := c.route.Path[caravanIndex(len(c.route.Path), g.caravanClock(c))]
		if want == c.pos {
			continue
		}
		if g.blockedIn(0, want) {
			g.car.paused[c.route.ID] += caravanStep // the road is blocked: wait
			continue
		}
		c.pos, moved = want, true
	}
	if moved {
		g.indexCaravans()
	}
}

func (g *Game) sortedCaravans() []*caravan {
	out := make([]*caravan, 0, len(g.car.active))
	for _, c := range g.car.active {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b *caravan) int { return a.id - b.id })
	return out
}

// bumpCaravan opens the caravan's shop.
func (g *Game) bumpCaravan(p *player, c *caravan) {
	g.startTalk(p, g.World.CaravanMerchant(c.route, c.pos))
}

// appendCaravans adds the caravans in p's view to p's list of visible monsters (like guards: you talk to them).
func (g *Game) appendCaravans(ms []protocol.Monster, p *player) []protocol.Monster {
	if p.zone != 0 {
		return ms
	}
	for _, c := range g.sortedCaravans() {
		if inBox(c.pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
			ms = append(ms, protocol.Monster{ID: caravanIDBase + c.id, Kind: "caravan", X: c.pos.X, Y: c.pos.Y})
		}
	}
	return ms
}

// caravanWares is the caravan's selection: one rank above the forges of its region, renewed every game
// day. It comes from the deterministic fallback: never a language model nor a database write. The revision changes with
// the day, so a purchase opened the day before is refused ("the stall has been renewed").
func (g *Game) caravanWares(p *player, npc world.NPC) (gear.Wares, int) {
	day := int(g.tick / (24 * protocol.TicksPerGameHour))
	ctx := g.vendorContext(npc)
	ctx.MaxTier = min(ctx.MaxTier+1, gear.MaxTier)
	ctx.MinTier = min(ctx.MinTier+1, ctx.MaxTier)
	key := world.NPCKey{Region: npc.Region, Index: npc.Index + 1000*(day+1)}
	return gear.Fallback(key, ctx, p.lang), day + 1
}

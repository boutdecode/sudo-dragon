package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Caravanes : sur certaines routes entre deux lieux (voir world/caravan.go), un marchand ambulant fait l'aller-retour.
// Il vend de l'équipement d'un rang de plus que les forges de la région, avec un assortiment qui change chaque jour de
// jeu. On lui parle en marchant sur lui ; il s'arrête pendant la conversation.
//
// Sa position ne dépend que du tick, de sa graine et du temps qu'il a passé arrêté : rien à sauvegarder, et la même
// caravane est au même endroit pour tous. Seules celles dont la route passe près d'un joueur sont suivies.

const (
	caravanIDBase = 1 << 29 // identifiants réseau, au-delà de ceux des monstres et des gardes (guardIDBase)
	caravanStep   = 10      // ticks par case (1 case par seconde)
	caravanPause  = 300     // ticks (30 s) d'arrêt à chaque bout de la route
	caravanReach  = garrisonActivation
	maxPauses     = 512
)

type caravan struct {
	id    int
	route world.RoadRoute
	seed  uint64
	pos   world.Pos
}

// caravanState regroupe les caravanes suivies.
type caravanState struct {
	next   int
	active map[string]*caravan // par identifiant de route
	at     map[world.Pos]*caravan
	paused map[string]uint64 // route -> ticks passés à l'arrêt (conversation, case occupée)
}

// caravanPeriod est la durée d'un aller-retour complet sur une route de n cases.
func caravanPeriod(n int) uint64 { return uint64(2*(n-1)*caravanStep + 2*caravanPause) }

// caravanIndex donne la case (indice dans le tracé de n cases) de la caravane à l'instant t de son cycle : aller,
// pause, retour, pause.
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

// clock est l'heure du cycle de c : le tick, décalé par sa graine, moins le temps passé à l'arrêt.
func (g *Game) caravanClock(c *caravan) uint64 {
	now := g.tick + c.seed%caravanPeriod(len(c.route.Path))
	if p := g.car.paused[c.route.ID]; p < now {
		return now - p
	}
	return 0
}

// caravanAt renvoie la caravane qui occupe la case pos.
func (g *Game) caravanAt(pos world.Pos) *caravan {
	if g.car.at == nil {
		return nil
	}
	return g.car.at[pos]
}

// activateCaravans suit les caravanes dont la route passe à moins de caravanReach d'un joueur et oublie les autres.
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

// caravanOf retrouve la caravane que le marchand npc représente, ou nil.
func (g *Game) caravanOf(npc world.NPC) *caravan {
	for _, c := range g.car.active {
		if c.route.A == npc.Region && npc.Index == world.CaravanIndexBase+c.route.Orient {
			return c
		}
	}
	return nil
}

// stepCaravans avance les caravanes d'une case à chaque caravanStep ticks. Une caravane arrête sa montre tant qu'un
// joueur lui parle, ou que la case suivante est occupée.
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
			g.car.paused[c.route.ID] += caravanStep // la route est barrée : on attend
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

// bumpCaravan ouvre la boutique de la caravane.
func (g *Game) bumpCaravan(p *player, c *caravan) {
	g.startTalk(p, g.World.CaravanMerchant(c.route, c.pos))
}

// appendCaravans ajoute à la liste des monstres visibles de p les caravanes de sa vue (comme les gardes : on leur parle).
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

// caravanWares est l'assortiment de la caravane : un rang de plus que les forges de sa région, renouvelé chaque jour
// de jeu. Il vient du repli déterministe : jamais de modèle de langage ni d'écriture en base. La révision change avec
// le jour, si bien qu'un achat ouvert la veille est refusé (« l'étalage a été renouvelé »).
func (g *Game) caravanWares(p *player, npc world.NPC) (gear.Wares, int) {
	day := int(g.tick / (24 * protocol.TicksPerGameHour))
	ctx := g.vendorContext(npc)
	ctx.MaxTier = min(ctx.MaxTier+1, gear.MaxTier)
	ctx.MinTier = min(ctx.MinTier+1, ctx.MaxTier)
	key := world.NPCKey{Region: npc.Region, Index: npc.Index + 1000*(day+1)}
	return gear.Fallback(key, ctx, p.lang), day + 1
}

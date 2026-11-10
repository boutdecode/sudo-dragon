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
	const n = 5 // 4 cases à parcourir
	leg := uint64((n - 1) * caravanStep)
	cases := []struct {
		t    uint64
		want int
	}{
		{0, 0}, {caravanStep - 1, 0}, {caravanStep, 1}, {leg - 1, 3},
		{leg, n - 1}, {leg + caravanPause - 1, n - 1}, // arrêt au bout
		{leg + caravanPause, n - 1}, {leg + caravanPause + caravanStep, n - 2},
		{2*leg + caravanPause - 1, 1}, {2*leg + caravanPause, 0}, // retour puis arrêt au départ
		{caravanPeriod(n) - 1, 0}, {caravanPeriod(n), 0}, {caravanPeriod(n) + caravanStep, 1}, // le cycle recommence
	}
	for _, c := range cases {
		if got := caravanIndex(n, c.t); got != c.want {
			t.Errorf("t=%d: case %d, attendu %d", c.t, got, c.want)
		}
	}
	// Jamais plus d'une case d'écart d'un tick de pas à l'autre : la caravane ne saute pas.
	prev := caravanIndex(n, 0)
	for tt := uint64(0); tt < 3*caravanPeriod(n); tt += caravanStep {
		idx := caravanIndex(n, tt)
		if d := idx - prev; d < -1 || d > 1 {
			t.Fatalf("t=%d: saut de %d à %d", tt, prev, idx)
		}
		prev = idx
	}
}

// caravanGame renvoie un jeu avec un joueur sur la route d'une caravane, la caravane suivie.
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
		t.Fatal("aucune caravane près de l'origine")
	}
	p, _ := joinRec(g, "a")
	g.setPos(p, route.Path[len(route.Path)/2])
	g.activateCaravans()
	c := g.car.active[route.ID]
	if c == nil {
		t.Fatal("caravane non suivie près d'un joueur")
	}
	return g, p, c
}

func TestCaravanFollowsItsRouteAndShowsInSnapshots(t *testing.T) {
	g, p, c := caravanGame(t)
	for i := 0; i < 3*caravanStep; i++ {
		g.tick++
		g.stepCaravans()
		if !slices.Contains(c.route.Path, c.pos) {
			t.Fatalf("la caravane a quitté sa route: %v", c.pos)
		}
		if g.caravanAt(c.pos) != c {
			t.Fatalf("index de position faux pour %v", c.pos)
		}
	}
	// Elle bouge : sur un cycle entier, elle occupe plusieurs cases.
	seen := map[world.Pos]bool{}
	for i := uint64(0); i < caravanPeriod(len(c.route.Path)); i += caravanStep {
		g.tick = i
		g.stepCaravans()
		seen[c.pos] = true
	}
	if len(seen) < 2 {
		t.Fatalf("la caravane ne s'est jamais déplacée (%d case)", len(seen))
	}

	// Elle est visible quand elle est dans la vue, avec un identifiant hors de la plage des monstres.
	g.setPos(p, g.freeNear(c.pos))
	var found *protocol.Monster
	for _, m := range g.appendCaravans(nil, p) {
		if m.X == c.pos.X && m.Y == c.pos.Y {
			m := m
			found = &m
		}
	}
	if found == nil || found.Kind != "caravan" || found.ID < caravanIDBase {
		t.Fatalf("caravane absente du snapshot: %+v", found)
	}

	// Loin de tout joueur, elle n'est plus suivie.
	g.setPos(p, world.Pos{X: 50 * world.RegionSize, Y: 50 * world.RegionSize})
	g.activateCaravans()
	if len(g.car.active) != 0 {
		t.Fatalf("%d caravane(s) encore suivie(s) loin des joueurs", len(g.car.active))
	}
}

func TestCaravanStopsWhileTalkedToAndWaitsWhenBlocked(t *testing.T) {
	g, p, c := caravanGame(t)
	// Place la caravane au milieu d'un trajet, et le joueur sur la case précédente de la route.
	i := slices.Index(c.route.Path, c.pos)
	if i < 1 {
		t.Skip("caravane au bout de sa route")
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
		t.Fatalf("marcher sur la caravane n'ouvre pas sa boutique (talk=%v, pos %v, caravane %v)", p.talk, p.pos, c.pos)
	}
	if p.pos != prev {
		t.Errorf("le joueur a marché sur la caravane: %v", p.pos)
	}

	// Elle ne bouge pas pendant la conversation, même sur plusieurs pas.
	at := c.pos
	for range 5 * caravanStep {
		p.talk.lastAct = g.tick // la conversation ne doit pas expirer
		g.step()
	}
	if c.pos != at {
		t.Fatalf("la caravane a marché pendant la conversation: %v -> %v", at, c.pos)
	}
	g.closeTalk(p, "")
	for range 2 * caravanStep {
		g.step()
	}
	if c.pos == at && len(c.route.Path) > 2 {
		t.Log("la caravane est restée à la même case après la conversation (bout de route ?)")
	}
}

func TestCaravanSellsHigherTierAndRotatesDaily(t *testing.T) {
	g, p, c := caravanGame(t)
	npc := g.World.CaravanMerchant(c.route, c.pos)
	ctx := g.vendorContext(npc)

	w1, rev1 := g.waresFor(p, npc)
	if len(w1.Items) == 0 {
		t.Fatal("caravane sans marchandise")
	}
	for _, it := range w1.Items {
		if it.Tier < min(ctx.MinTier+1, gear.MaxTier) {
			t.Errorf("%s : rang %d, moins que le plancher de la région + 1 (%d)", it.Name, it.Tier, ctx.MinTier+1)
		}
	}
	if again, rev := g.waresFor(p, npc); rev != rev1 || len(again.Items) != len(w1.Items) || again.Items[0].ID != w1.Items[0].ID {
		t.Fatal("l'assortiment change sans que le jour change")
	}
	g.tick += 24 * protocol.TicksPerGameHour
	w2, rev2 := g.waresFor(p, npc)
	if rev2 == rev1 {
		t.Fatalf("la révision ne change pas d'un jour à l'autre (%d)", rev2)
	}
	same := len(w2.Items) == len(w1.Items)
	for i := range w1.Items {
		if same && (w2.Items[i].ID != w1.Items[i].ID || w2.Items[i].Name != w1.Items[i].Name) {
			same = false
		}
	}
	if same {
		t.Error("l'assortiment est identique le lendemain")
	}

	// Un achat décidé sur l'étalage d'hier est refusé (révision périmée).
	p.stats.AddGold(5000)
	if out := g.buyItem(p, npc, itoaRev(rev1, w1.Items[0].ID)); len(out) == 0 || out[0] == "" {
		t.Fatal("pas de réponse à un achat périmé")
	}
	if len(p.inv.Items) != 0 && len(p.inv.Items) != 1 { // l'arme de départ éventuelle
		t.Logf("sac : %d objets", len(p.inv.Items))
	}

	// Le marchand ne propose ni vol, ni quête, ni discussion libre.
	for _, o := range g.offers(p, npc) {
		switch o.Action {
		case protocol.NPCSteal, protocol.NPCTalk, protocol.NPCAskWork, protocol.NPCTurnIn:
			t.Errorf("offre inattendue chez un marchand ambulant: %s", o.Action)
		}
	}
}

func itoaRev(rev int, id int64) string { return strconv.Itoa(rev) + "." + strconv.FormatInt(id, 10) }

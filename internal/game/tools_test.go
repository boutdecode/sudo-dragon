package game

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// newCabinRig puts a player next to the vendor of a trade cabin of the starting ring (one of each trade is guaranteed).
func newCabinRig(t *testing.T, kind world.NPCKind) *npcRig {
	t.Helper()
	g := newGame()
	a := g.World.FindSpawn()
	c := world.RegionOf(a.X, a.Y)
	var npc world.NPC
	found := false
	for dy := -2; dy <= 2 && !found; dy++ {
		for dx := -2; dx <= 2 && !found; dx++ {
			for _, poi := range g.World.POIsOfRegion(world.RegionPos{X: c.X + dx, Y: c.Y + dy}) {
				if v, ok := g.World.CabinVendor(poi); ok && v.Kind == kind {
					npc, found = v, true
				}
			}
		}
	}
	if !found {
		t.Fatalf("no %s cabin around the start", kind)
	}
	p, rec := joinRec(g, "hero")
	p.lang = i18n.FR
	p.stats.Gold = 5000
	r := &npcRig{g: g, p: p, npc: npc, rec: rec, tt: t}
	p.sendNPC = func(d protocol.NPCDialog) { r.msgs = append(r.msgs, d) }
	for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
		pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
		if !g.blocked(pos) {
			place(g, p, pos)
			r.dx, r.dy = d[0], d[1]
			return r
		}
	}
	t.Fatalf("no free tile next to the vendor at %v", npc.Pos)
	return nil
}

func toolOffersOf(d protocol.NPCDialog) (out []protocol.NPCOffer) {
	for _, o := range d.Offers {
		if o.Action == protocol.NPCBuyTool {
			out = append(out, o)
		}
	}
	return
}

func TestToolVendorSellsOnlyItsTools(t *testing.T) {
	for kind, cat := range map[world.NPCKind]resource.Category{world.NPCLumberjack: resource.Wood, world.NPCMiner: resource.Ore, world.NPCHerbalist: resource.Herb} {
		r := newCabinRig(t, kind)
		r.talk()
		d := r.last()
		if d.Kind != string(kind) || d.Place == "" || d.Name == "" {
			t.Fatalf("%s: dialog %+v", kind, d)
		}
		if len(d.Offers) != resource.NumToolTiers || len(toolOffersOf(d)) != resource.NumToolTiers {
			t.Fatalf("%s: %d offers, want %d tool offers only", kind, len(d.Offers), resource.NumToolTiers)
		}
		for i, o := range d.Offers {
			tier := i + 1
			if o.Arg != string(rune('0'+tier)) || o.Label != toolName(r.p, cat, tier) || o.Detail == "" {
				t.Errorf("%s: offer %d: %+v", kind, i, o)
			}
			tl, _ := resource.ToolTier(tier)
			if want := r.g.priceFor(r.p, r.npc, tl.Price); o.Price != want {
				t.Errorf("%s: tier %d price %d, want %d (charisma and reputation apply)", kind, tier, o.Price, want)
			}
			// Level 1: only the basic tool can be bought.
			if o.Available != (tier == 1) {
				t.Errorf("%s: tier %d availability %v", kind, tier, o.Available)
			}
		}
	}
}

func TestBuyToolSucceeds(t *testing.T) {
	r := newCabinRig(t, world.NPCLumberjack)
	var saved []Saved
	r.g.Persist = func(_ string, s Saved) { saved = append(saved, s) }
	var bags []protocol.Bag
	r.p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	r.talk()
	o := toolOffersOf(r.last())[0]
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyTool, o.Arg)
	if r.p.tools[resource.Wood] != 1 || r.p.stats.Gold != gold-o.Price {
		t.Fatalf("tool %d, gold %d -> %d (price %d)", r.p.tools[resource.Wood], gold, r.p.stats.Gold, o.Price)
	}
	if n := len(saved); n == 0 || saved[n-1].Tools["wood"] != 1 {
		t.Fatalf("the purchase must be saved right away: %+v", saved)
	}
	if n := len(bags); n == 0 || bags[n-1].Jobs[resource.Wood].Tool != 1 || bags[n-1].Jobs[resource.Wood].Yield != 1 {
		t.Fatalf("bag not updated: %+v", bags)
	}
	// The owned tier is no longer offered.
	if offers := toolOffersOf(r.last()); len(offers) != resource.NumToolTiers-1 || offers[0].Arg != "2" {
		t.Fatalf("offers after the purchase: %+v", offers)
	}
	// Other trades are untouched.
	if r.p.tools[resource.Ore] != 0 || r.p.tools[resource.Herb] != 0 {
		t.Fatalf("a lumberjack sold another trade's tool: %v", r.p.tools)
	}
}

func TestBuyToolRefusals(t *testing.T) {
	r := newCabinRig(t, world.NPCMiner)
	r.talk()
	buy := func(arg string) {
		r.msgs = nil
		r.g.talkAct(r.p, protocol.NPCBuyTool, arg)
	}
	state := func() (int, int) { return r.p.stats.Gold, r.p.tools[resource.Ore] }

	// Job level too low for tier 2.
	g0, _ := state()
	buy("2")
	if g, tl := state(); g != g0 || tl != 0 {
		t.Fatalf("tier 2 bought at job level 1: gold %d, tool %d", g, tl)
	}
	// Invalid arguments.
	for _, arg := range []string{"", "x", "0", "-1", "5", "99", "1.5", "1;2"} {
		buy(arg)
		if g, tl := state(); g != g0 || tl != 0 {
			t.Fatalf("argument %q changed the state: gold %d, tool %d", arg, g, tl)
		}
	}
	// Not enough gold.
	r.p.stats.Gold = 1
	buy("1")
	if r.p.tools[resource.Ore] != 0 || r.p.stats.Gold != 1 {
		t.Fatalf("bought without gold: %v, %d", r.p.tools, r.p.stats.Gold)
	}
	// Already owned, or lower than the owned tier.
	r.p.stats.Gold = 5000
	r.p.skills = map[resource.Category]int{resource.Ore: resource.XPToNext(1) + resource.XPToNext(2) + resource.XPToNext(3)} // level 4
	buy("2")
	if r.p.tools[resource.Ore] != 2 {
		t.Fatalf("tier 2 not bought at level 4: %v", r.p.tools)
	}
	gold := r.p.stats.Gold
	for _, arg := range []string{"1", "2"} {
		buy(arg)
		if r.p.stats.Gold != gold || r.p.tools[resource.Ore] != 2 {
			t.Fatalf("tier %s bought again: gold %d, tool %v", arg, r.p.stats.Gold, r.p.tools)
		}
	}
}

// Only a trade vendor sells tools: the action is refused anywhere else.
func TestBuyToolRefusedByOtherNPCs(t *testing.T) {
	r := smithRig(t)
	r.talk()
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyTool, "1")
	if r.p.stats.Gold != gold || len(r.p.tools) != 0 {
		t.Fatalf("a smith sold a tool: %v", r.p.tools)
	}
}

// A trade vendor only buys and sells tools: no quest, no work, no discussion, never the language model.
func TestToolVendorNeverCallsTheModel(t *testing.T) {
	r := newCabinRig(t, world.NPCHerbalist)
	ft := newFakeTalker()
	r.g.Talker = ft
	r.p.consent = true
	r.talk()
	for _, a := range []string{protocol.NPCTalk, protocol.NPCSay, protocol.NPCConsent, protocol.NPCAskWork, protocol.NPCWhere,
		protocol.NPCPlaces, protocol.NPCSteal, protocol.NPCTurnIn, protocol.NPCBuy, protocol.NPCHeal, protocol.NPCSellMenu} {
		r.g.talkAct(r.p, a, "hello")
	}
	if ft.count() != 0 {
		t.Fatalf("the language model was called %d times", ft.count())
	}
	if r.p.talk == nil || r.p.talk.mode != protocol.NPCModeMenu {
		t.Fatalf("the conversation left the menu: %+v", r.p.talk)
	}
	for _, o := range r.last().Offers {
		if o.Action != protocol.NPCBuyTool {
			t.Errorf("unexpected offer %+v", o)
		}
	}
	r.act(protocol.NPCLeave)
	if r.p.talk != nil {
		t.Fatal("the conversation did not close")
	}
}

// The price goes through the player's charisma like any other purchase.
func TestToolPriceFollowsCharisma(t *testing.T) {
	r := newCabinRig(t, world.NPCLumberjack)
	tl, _ := resource.ToolTier(1)
	r.p.stats.Scores[hero.CHA] = 3
	low := r.g.toolPrice(r.p, r.npc, tl)
	r.p.stats.Scores[hero.CHA] = 18
	high := r.g.toolPrice(r.p, r.npc, tl)
	if high >= low {
		t.Errorf("a charming player must pay less: %d vs %d", high, low)
	}
}

func TestWalkingIntoAVendorOpensTheShop(t *testing.T) {
	r := newCabinRig(t, world.NPCMiner)
	if !r.g.blocked(r.npc.Pos) {
		t.Fatal("the vendor's tile must be blocked")
	}
	r.talk()
	if r.p.talk == nil || r.p.talk.npc.Kind != world.NPCMiner {
		t.Fatal("walking onto the vendor must open the shop")
	}
}

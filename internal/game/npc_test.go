package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// npcRig sets up a player next to an NPC of the wanted type and captures the dialogues.
type npcRig struct {
	g      *Game
	p      *player
	npc    world.NPC
	dx, dy int // direction from the player to the NPC
	msgs   []protocol.NPCDialog
	rec    *recorder
	tt     *testing.T
}

func (r *npcRig) last() protocol.NPCDialog { return r.msgs[len(r.msgs)-1] }

// findNPC looks for an NPC of the given type near the start.
func findNPC(t *testing.T, g *Game, kind world.NPCKind) world.NPC {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 700) {
		for _, n := range s.NPCs {
			if n.Kind == kind {
				return n
			}
		}
	}
	t.Skipf("no %s NPC near the start", kind)
	return world.NPC{}
}

func newNPCRig(t *testing.T, kind world.NPCKind) *npcRig {
	t.Helper()
	g := newGame()
	npc := findNPC(t, g, kind)
	p, rec := joinRec(g, "hero")
	p.lang = i18n.FR // assertions are on French catalogue text
	r := &npcRig{g: g, p: p, npc: npc, rec: rec, tt: t}
	p.sendNPC = func(d protocol.NPCDialog) { r.msgs = append(r.msgs, d) }

	// A free tile next to the NPC, with the direction leading to it.
	for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
		pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
		if !g.blocked(pos) {
			place(g, p, pos)
			r.dx, r.dy = d[0], d[1]
			return r
		}
	}
	t.Fatalf("no free tile next to the NPC at %v", npc.Pos)
	return nil
}

// talk walks toward the NPC to speak to it.
func (r *npcRig) talk() {
	r.g.move(r.p.id, r.dx, r.dy)
	r.g.step()
}

func (r *npcRig) act(action string) { r.g.talkAct(r.p, action, "") }

func (r *npcRig) offer(action string) (protocol.NPCOffer, bool) {
	for _, o := range r.last().Offers {
		if o.Action == action {
			return o, true
		}
	}
	return protocol.NPCOffer{}, false
}

func TestNPCOccupiesItsTile(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	if !g.blocked(npc.Pos) {
		t.Fatal("an NPC's tile must be blocked")
	}
	// Neither a player restored there nor a monster can land on it.
	saved := &Saved{Stats: combat.NewStats(), HP: 10, Pos: npc.Pos}
	p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
	if p.pos == npc.Pos {
		t.Fatal("a player was placed on an NPC")
	}
}

func TestWalkingIntoNPCOpensDialog(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	start := r.p.pos
	r.talk()

	if r.p.talk == nil || r.p.talk.npc != r.npc {
		t.Fatal("the conversation did not open")
	}
	if r.p.pos != start {
		t.Fatalf("the player moved: %v", r.p.pos)
	}
	if len(r.msgs) != 1 {
		t.Fatalf("%d messages, expected 1", len(r.msgs))
	}
	d := r.last()
	if d.Kind != "healer" || d.Over || len(d.Log) != 1 {
		t.Fatalf("opening: %+v", d)
	}
	if _, ok := r.offer(protocol.NPCHeal); !ok {
		t.Fatalf("the healer offers no healing: %+v", d.Offers)
	}
}

func TestPlayerIsFrozenWhileTalking(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	start := r.p.pos
	for i := 0; i < 30; i++ {
		r.g.move(r.p.id, -r.dx, -r.dy)
		r.g.step()
	}
	if r.p.pos != start || r.p.hasPending {
		t.Fatalf("the player moved during the conversation: %v", r.p.pos)
	}
}

func TestLeavingEndsTheDialogAndFreesThePlayer(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.act(protocol.NPCLeave)

	if r.p.talk != nil {
		t.Fatal("the conversation is still open")
	}
	d := r.last()
	if !d.Over || d.Kind != "healer" || len(d.Offers) != 0 {
		t.Fatalf("closing message: %+v", d)
	}
	// Free to leave: a step is executed.
	before := r.p.pos
	r.g.move(r.p.id, -r.dx, -r.dy)
	r.g.step()
	if r.p.pos == before && !r.g.blocked(world.Pos{X: before.X - r.dx, Y: before.Y - r.dy}) {
		t.Fatal("the player stays frozen after the conversation")
	}
}

func TestActionsWithoutDialogAreIgnored(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.hp = 1
	r.p.stats.Gold = 500
	r.act(protocol.NPCHeal)
	if r.p.hp != 1 || r.p.stats.Gold != 500 || len(r.msgs) != 0 {
		t.Fatal("an out-of-dialogue action had an effect")
	}
}

func TestCannotTalkWhileFighting(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	dx, dy := freeDir(t, r.g, r.p)
	m := r.g.addMonster(testRat, world.Pos{X: r.p.pos.X + dx, Y: r.p.pos.Y + dy})
	r.g.startFight(r.p, m, true)
	r.g.startTalk(r.p, r.npc)
	if r.p.talk != nil {
		t.Fatal("a conversation opened during a fight")
	}
}

// --- Soigneur ---

func TestHealerIsFreeAtLevelOne(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.hp = 3
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); !o.Available || o.Price != 0 {
		t.Fatalf("offer: %+v, expected free and available", o)
	}
	r.act(protocol.NPCHeal)
	if r.p.hp != r.p.stats.MaxHP || r.p.stats.Gold != 0 {
		t.Fatalf("HP %d/%d, or %d", r.p.hp, r.p.stats.MaxHP, r.p.stats.Gold)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "offert") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

func TestHealerChargesPerLevel(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(5, 0, 2)
	r.p.hp = 10
	price := healPrice(5)
	if price != 5*healPricePerLevel {
		t.Fatalf("price = %d", price)
	}
	r.p.stats.Gold = price + 7
	r.talk()
	r.act(protocol.NPCHeal)

	if r.p.hp != r.p.stats.MaxHP || r.p.stats.Gold != 7 {
		t.Fatalf("HP %d/%d, or %d (expected 7)", r.p.hp, r.p.stats.MaxHP, r.p.stats.Gold)
	}
}

func TestHealerRefusesWhenPoor(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(4, 0, 2)
	r.p.hp = 5
	r.p.stats.Gold = healPrice(4) - 3
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); o.Available {
		t.Fatal("the offer should be marked unavailable")
	}
	r.act(protocol.NPCHeal)
	if r.p.hp != 5 || r.p.stats.Gold != healPrice(4)-3 {
		t.Fatalf("state modified despite the refusal: HP %d, gold %d", r.p.hp, r.p.stats.Gold)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Il vous manque 3 or") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

func TestHealerDoesNotChargeWhenHealthy(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(6, 0, 2)
	r.p.hp, r.p.mana = r.p.stats.MaxHP, r.p.stats.MaxMana
	r.p.stats.Gold = 100
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); o.Available {
		t.Fatal("healing a player at full health should not be offered")
	}
	r.act(protocol.NPCHeal)
	if r.p.stats.Gold != 100 {
		t.Fatalf("gold debited (%d) for a player at full health", r.p.stats.Gold)
	}
}

func TestHealerDoesNotSellPotions(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats.Gold = 200
	before := r.p.stats.Potions
	r.talk()
	r.act(protocol.NPCBuy)
	r.act(protocol.NPCSell)
	if r.p.stats.Gold != 200 || r.p.stats.Potions != before {
		t.Fatalf("the healer traded: gold %d, potions %d", r.p.stats.Gold, r.p.stats.Potions)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "pas ce que je propose") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

// --- Marchand ---

func TestMerchantBuy(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Gold = potionBuyPrice + 5
	before := r.p.stats.Potions
	r.talk()
	r.act(protocol.NPCBuy)

	if r.p.stats.Potions != before+1 || r.p.stats.Gold != 5 {
		t.Fatalf("potions %d (expected %d), or %d (expected 5)", r.p.stats.Potions, before+1, r.p.stats.Gold)
	}
	if d := r.last(); d.Gold != 5 || d.Potions != before+1 {
		t.Fatalf("the dialogue is not up to date: %+v", d)
	}
}

func TestMerchantBuyRefusals(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()

	r.p.stats.Gold = potionBuyPrice - 1 // too poor
	before := r.p.stats.Potions
	r.act(protocol.NPCBuy)
	if r.p.stats.Potions != before || r.p.stats.Gold != potionBuyPrice-1 {
		t.Fatal("purchase accepted without the necessary gold")
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Il vous manque 1 or") {
		t.Fatalf("journal: %v", r.last().Log)
	}

	r.p.stats.Gold = 500 // full bag: the gold is NOT debited
	r.p.stats.Potions = combat.MaxPotions
	r.act(protocol.NPCBuy)
	if r.p.stats.Gold != 500 || r.p.stats.Potions != combat.MaxPotions {
		t.Fatalf("purchase with a full bag: gold %d, potions %d", r.p.stats.Gold, r.p.stats.Potions)
	}
	if o, _ := r.offer(protocol.NPCBuy); o.Available {
		t.Fatal("the purchase should not be offered with a full bag")
	}
}

func TestMerchantSell(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Potions = 3
	r.p.stats.Gold = 10
	r.talk()
	r.act(protocol.NPCSell)
	if r.p.stats.Potions != 2 || r.p.stats.Gold != 10+potionSellPrice {
		t.Fatalf("potions %d, or %d", r.p.stats.Potions, r.p.stats.Gold)
	}
	if o, ok := r.offer(protocol.NPCSell); !ok || !o.Gain || o.Price != potionSellPrice {
		t.Fatalf("sale offer: %+v", o)
	}
}

func TestMerchantSellRefusals(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()

	r.p.stats.Potions = 0
	r.p.stats.Gold = 40
	r.act(protocol.NPCSell)
	if r.p.stats.Gold != 40 || r.p.stats.Potions != 0 {
		t.Fatal("sale without a potion accepted")
	}

	// Full purse: we do not lose a potion for nothing.
	r.p.stats.Potions = 4
	r.p.stats.Gold = combat.MaxGold - 2
	r.act(protocol.NPCSell)
	if r.p.stats.Potions != 4 || r.p.stats.Gold != combat.MaxGold-2 {
		t.Fatalf("potion sold with a full purse: potions %d, gold %d", r.p.stats.Potions, r.p.stats.Gold)
	}
}

func TestMerchantBuyThenSellNeverCreatesGold(t *testing.T) {
	if potionSellPrice >= potionBuyPrice {
		t.Fatalf("reselling (%d) must yield less than buying (%d), otherwise gold is manufactured", potionSellPrice, potionBuyPrice)
	}
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Gold = 1000
	r.p.stats.Potions = 0
	r.talk()
	start := r.p.stats.Gold
	for i := 0; i < 20; i++ {
		r.act(protocol.NPCBuy)
		r.act(protocol.NPCSell)
	}
	if r.p.stats.Gold > start {
		t.Fatalf("gold created by round trips: %d -> %d", start, r.p.stats.Gold)
	}
}

func TestMerchantDoesNotHeal(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.hp = 2
	r.p.stats.Gold = 500
	r.talk()
	r.act(protocol.NPCHeal)
	if r.p.hp != 2 || r.p.stats.Gold != 500 {
		t.Fatalf("the merchant healed: HP %d, gold %d", r.p.hp, r.p.stats.Gold)
	}
}

// --- Persistence, expiration, real loop ---

func TestTransactionIsSavedImmediately(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.p.stats.Gold = 100
	r.talk()
	if len(sv.calls) != 0 {
		t.Fatal("save on opening a conversation")
	}
	r.act(protocol.NPCBuy)
	got, ok := sv.last("hero")
	if !ok || got.Stats.Gold != 100-potionBuyPrice {
		t.Fatalf("save after purchase: %+v (ok=%v)", got.Stats, ok)
	}
	n := len(sv.calls)
	r.p.stats.Gold = 0
	r.act(protocol.NPCBuy) // refused
	if len(sv.calls) != n {
		t.Fatal("a refused transaction triggered a save")
	}
}

func TestIdleDialogExpires(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.g.tick += fightIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("expired too early")
	}
	r.g.tick++
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk != nil {
		t.Fatal("the idle conversation should have expired")
	}
	if d := r.last(); !d.Over || d.Kind != "healer" {
		t.Fatalf("expiration message: %+v", d)
	}
}

func TestActionResetsIdleTimerForDialogs(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()
	r.g.tick += fightIdleTicks
	r.act(protocol.NPCSell) // the player acts
	r.g.tick += fightIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("an active player must not be kicked out")
	}
}

func TestLeavingGameClosesDialogSilently(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.g.leave(r.p.id)
	if _, ok := r.g.players[r.p.id]; ok {
		t.Fatal("player still present")
	}
}

func TestValidNPCActions(t *testing.T) {
	for _, a := range []string{"buy", "sell", "heal", "leave"} {
		if !ValidNPCAction(a) {
			t.Errorf("%q should be valid", a)
		}
	}
	for _, a := range []string{"", "Buy", "pillage", "attack", "buy "} {
		if ValidNPCAction(a) {
			t.Errorf("%q should not be valid", a)
		}
	}
}

// Full walkthrough through the real loop (goroutine, command queue, callbacks).
func TestDialogThroughRunningGame(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	msgs := make(chan protocol.NPCDialog, 16)
	id, err := g.Join("hero", Handlers{
		Snapshot: func(protocol.Snapshot) {},
		NPC:      func(d protocol.NPCDialog) { msgs <- d },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Place the player next to the NPC then make them take a step toward it.
	ready := make(chan [2]int, 1)
	g.do(func() {
		p := g.players[id]
		for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
			pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
			if !g.blocked(pos) {
				g.setPos(p, pos)
				p.hp = 1
				ready <- d
				return
			}
		}
		ready <- [2]int{}
	})
	d := <-ready
	if d == ([2]int{}) {
		t.Skip("no free tile next to the NPC")
	}
	g.Move(id, d[0], d[1])

	recv := func() protocol.NPCDialog {
		select {
		case m := <-msgs:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no dialogue message")
			return protocol.NPCDialog{}
		}
	}
	if m := recv(); m.Over || m.Kind != "healer" {
		t.Fatalf("opening: %+v", m)
	}
	g.TalkAct(id, protocol.NPCHeal, "")
	if m := recv(); m.HP != m.MaxHP {
		t.Fatalf("healing not applied: %+v", m)
	}
	g.TalkAct(id, "action-inconnue", "") // ignored
	g.TalkAct(id, protocol.NPCLeave, "")
	if m := recv(); !m.Over {
		t.Fatalf("closing: %+v", m)
	}
}

func TestVillagerOffersOnlyConversation(t *testing.T) {
	r := newNPCRig(t, world.NPCVillager)
	r.g.Talker = newFakeTalker()
	r.talk()
	d := r.last()
	if d.Kind != string(world.NPCVillager) || len(d.Offers) != 1 || d.Offers[0].Action != protocol.NPCTalk {
		t.Fatalf("a villager only offers Chat: %+v", d)
	}
	// Neither trade nor healing with them, and nothing is debited.
	r.p.stats.Gold = 100
	r.act(protocol.NPCBuy)
	r.act(protocol.NPCHeal)
	if r.p.stats.Gold != 100 || !strings.Contains(strings.Join(r.last().Log, " "), "pas ce que je propose") {
		t.Fatalf("gold=%d last=%+v", r.p.stats.Gold, r.last())
	}
}

func TestVillagerWithoutAIGivesAStableAmbientLine(t *testing.T) {
	r := newNPCRig(t, world.NPCVillager) // no Talker
	r.talk()
	d := r.last()
	if len(d.Offers) != 0 || len(d.Log) != 1 || d.Log[0] != ambientLine(i18n.FR, r.npc) || d.Log[0] == "" {
		t.Fatalf("ambient sentence expected with no empty menu or AI : %+v", d)
	}
	if ambientLine(i18n.FR, r.npc) != ambientLine(i18n.FR, r.npc) {
		t.Fatal("the sentence must be stable for a given NPC")
	}
}

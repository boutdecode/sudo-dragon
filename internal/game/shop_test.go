package game

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func smithRig(t *testing.T) *npcRig {
	t.Helper()
	r := newNPCRig(t, world.NPCSmith)
	r.p.stats.Level, r.p.stats.Gold = 8, 5000
	return r
}

func buyOffers(d protocol.NPCDialog) (out []protocol.NPCOffer) {
	for _, o := range d.Offers {
		if o.Action == protocol.NPCBuyItem {
			out = append(out, o)
		}
	}
	return
}

func TestVendorMenuShowsFallbackWaresWithoutAModel(t *testing.T) {
	r := smithRig(t)
	r.talk()
	d := r.last()
	offers := buyOffers(d)
	if len(offers) < 6 || d.Specialty == "" {
		t.Fatalf("fallback stock: %d offers, specialty %q", len(offers), d.Specialty)
	}
	for _, o := range offers {
		if o.Price <= 0 || o.Arg == "" || !strings.Contains(o.Detail, "Dégâts") || !o.Available {
			t.Errorf("incomplete offer: %+v", o)
		}
		if !strings.HasPrefix(o.Arg, "0.") {
			t.Errorf("the fallback carries revision 0: %q", o.Arg)
		}
	}
	if _, has := r.offer(protocol.NPCSellMenu); has {
		t.Fatal("nothing to resell with an empty bag")
	}
}

func TestBuyEquipAndFightBonus(t *testing.T) {
	r := smithRig(t)
	var saved []Saved
	r.g.Persist = func(_ string, s Saved) { saved = append(saved, s) }
	var bags []protocol.Bag
	r.p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	r.talk()
	o := buyOffers(r.last())[3]
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if len(r.p.inv.Items) != 1 || r.p.stats.Gold != gold-o.Price {
		t.Fatalf("bag %d items, gold %d -> %d (price %d)", len(r.p.inv.Items), gold, r.p.stats.Gold, o.Price)
	}
	item := r.p.inv.Items[0]
	if item.ID != 1 || item.Origin != r.npc.Key() || item.Slot != gear.Weapon {
		t.Fatalf("%+v", item)
	}
	if n := len(saved); n == 0 || !saved[n-1].GearDirty || len(saved[n-1].Gear.Items) != 1 {
		t.Fatalf("the purchase must be saved right away: %+v", saved)
	}
	if len(bags) == 0 || len(bags[len(bags)-1].Items) != 1 {
		t.Fatal("bag not sent")
	}

	// No bonus as long as the item is not equipped.
	if statsOf(r.p).Dmg != "1d4" || r.p.fighter().Dmg != combat.BareHands {
		t.Fatalf("bare-handed: %s", statsOf(r.p).Dmg)
	}
	r.g.GearActNow(r.p, protocol.GearEquip, item.ID, "")
	if f := r.p.fighter(); f.Dmg != item.Damage() || statsOf(r.p).Dmg != item.Damage().WithBonus(f.DmgMod).String() || f.Dmg.IsZero() {
		t.Fatalf("damage %s, expected %s", statsOf(r.p).Dmg, item.Damage())
	}
	r.g.GearActNow(r.p, protocol.GearUnequip, 0, string(gear.Weapon))
	if statsOf(r.p).Dmg != "1d4" {
		t.Fatal("removal without effect")
	}
}

func TestBuyRules(t *testing.T) {
	r := smithRig(t)
	r.talk()
	o := buyOffers(r.last())[0]

	r.p.stats.Gold = o.Price - 1
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != o.Price-1 {
		t.Fatal("purchase without enough gold")
	}
	r.p.stats.Gold = 100000
	for _, bad := range []string{"", "x", "0.99", "7.1", "0.", ".1", "0.-1"} {
		r.g.talkAct(r.p, protocol.NPCBuyItem, bad)
	}
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != 100000 {
		t.Fatal("an invalid argument had an effect")
	}
	for i := 0; i < gear.BagCapacity; i++ {
		r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	}
	if len(r.p.inv.Items) != gear.BagCapacity {
		t.Fatalf("%d items", len(r.p.inv.Items))
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if len(r.p.inv.Items) != gear.BagCapacity || r.p.stats.Gold != gold {
		t.Fatal("full bag: purchase accepted")
	}
	// A merchant does not sell equipment.
	m := newNPCRig(t, world.NPCMerchant)
	m.p.stats.Gold = 1000
	m.talk()
	m.g.talkAct(m.p, protocol.NPCBuyItem, "0.1")
	if len(m.p.inv.Items) != 0 || m.p.stats.Gold != 1000 {
		t.Fatal("a merchant sold equipment")
	}
}

func TestReputationDiscountsEquipmentAndMenuAgrees(t *testing.T) {
	r := smithRig(t)
	r.p.rep = map[world.RegionPos]int{r.npc.Region: 15}
	r.talk()
	o := buyOffers(r.last())[0]
	w, _ := r.g.waresFor(r.p, r.npc)
	if o.Price >= w.Items[0].Price {
		t.Fatalf("no discount: %d for %d", o.Price, w.Items[0].Price)
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if gold-r.p.stats.Gold != o.Price {
		t.Fatalf("debited %d, announced %d", gold-r.p.stats.Gold, o.Price)
	}
}

func TestSellFlow(t *testing.T) {
	r := smithRig(t)
	r.talk()
	r.g.talkAct(r.p, protocol.NPCBuyItem, buyOffers(r.last())[0].Arg)
	r.g.talkAct(r.p, protocol.NPCBuyItem, buyOffers(r.last())[1].Arg)
	first := r.p.inv.Items[0]
	r.g.GearActNow(r.p, protocol.GearEquip, first.ID, "")

	r.act(protocol.NPCSellMenu)
	d := r.last()
	if d.Mode != protocol.NPCModeSell {
		t.Fatalf("mode %q", d.Mode)
	}
	var sells []protocol.NPCOffer
	for _, o := range d.Offers {
		if o.Action == protocol.NPCSellItem {
			sells = append(sells, o)
		}
	}
	if len(sells) != 1 || sells[0].Arg == "1" {
		t.Fatalf("only the unworn item resells: %+v", sells)
	}
	// The worn item cannot be sold, even with its identifier.
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCSellItem, "1")
	if len(r.p.inv.Items) != 2 || r.p.stats.Gold != gold {
		t.Fatal("worn item resold")
	}
	r.g.talkAct(r.p, protocol.NPCSellItem, sells[0].Arg)
	if len(r.p.inv.Items) != 1 || r.p.stats.Gold != gold+sells[0].Price {
		t.Fatalf("resale: %d items, or %d (+%d expected)", len(r.p.inv.Items), r.p.stats.Gold-gold, sells[0].Price)
	}
	if r.last().Mode != protocol.NPCModeMenu {
		t.Fatalf("nothing left to resell: back to the menu, mode %q", r.last().Mode)
	}
}

func TestEquipRules(t *testing.T) {
	r := smithRig(t)
	r.p.inv.Add(gear.Item{Name: "Grande lame", Slot: gear.Weapon, Tier: 5, Family: "longsword", Quality: 5, Price: 500})
	r.p.inv.Add(gear.Item{Name: "Petite lame", Slot: gear.Weapon, Tier: 1, Family: "dagger", Price: 50})
	r.p.stats.Level = 5
	var bags []protocol.Bag
	r.p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }

	r.g.GearActNow(r.p, protocol.GearEquip, 1, "") // tier 5: level 20 required
	if r.p.inv.IsWorn(1) || len(bags) == 0 || len(bags[len(bags)-1].Notice) == 0 {
		t.Fatalf("insufficient level ignored: %+v", bags)
	}
	r.g.GearActNow(r.p, protocol.GearEquip, 2, "")
	r.p.stats.Level = 20
	r.g.GearActNow(r.p, protocol.GearEquip, 1, "") // replaces the small blade
	if r.p.inv.IsWorn(2) || !r.p.inv.IsWorn(1) {
		t.Fatal("one item per slot")
	}
	// No equipment change in the middle of a fight.
	r.p.fight = &fight{}
	r.g.GearActNow(r.p, protocol.GearEquip, 2, "")
	if !r.p.inv.IsWorn(1) {
		t.Fatal("equipment changed in a fight")
	}
	r.p.fight = nil
	r.g.GearActNow(r.p, protocol.GearEquip, 999, "") // unknown: no effect
	r.g.GearActNow(r.p, protocol.GearUnequip, 0, "ring")
}

func TestGearSurvivesReconnectionAndCorruptionIsCleaned(t *testing.T) {
	r := smithRig(t)
	var saved Saved
	r.g.Persist = func(_ string, s Saved) { saved = s }
	r.talk()
	r.g.talkAct(r.p, protocol.NPCBuyItem, buyOffers(r.last())[0].Arg)
	r.g.GearActNow(r.p, protocol.GearEquip, r.p.inv.Items[0].ID, "")
	r.g.save(r.p)

	p2 := r.g.joinWith("hero2", func(protocol.Snapshot) {}, &saved)
	if len(p2.inv.Items) != 1 || !p2.inv.IsWorn(p2.inv.Items[0].ID) {
		t.Fatalf("after reconnection: %+v", p2.inv)
	}
	// A tampered save is brought back within bounds.
	bad := Saved{Stats: r.p.stats, Gear: gear.Inventory{
		Items: []gear.Item{{ID: 1, Name: "Dieu", Slot: gear.Weapon, Tier: 99, Family: "greataxe", Quality: 1 << 30, Price: 1 << 40}, {ID: 1, Name: "Doublon", Slot: gear.Helm}},
		Worn:  map[gear.Slot]int64{gear.Weapon: 1, gear.Armor: 1},
	}}
	p3 := r.g.joinWith("hero3", func(protocol.Snapshot) {}, &bad)
	if w := p3.inv.Equipped().Weapon; len(p3.inv.Items) != 1 || w == nil || w.Tier != gear.MaxTier || w.Quality != gear.MaxTier || p3.inv.Equipped().Defense() != 0 {
		t.Fatalf("item badly brought back: %+v", p3.inv)
	}
}

// fakeStocker is a Talker that also supplies the stocks.
type fakeStocker struct {
	*fakeTalker
	reqs  []npcai.StockRequest
	dones []func(npcai.StockReply)
}

func (f *fakeStocker) Stock(req npcai.StockRequest, done func(npcai.StockReply)) {
	f.reqs = append(f.reqs, req)
	f.dones = append(f.dones, done)
}

func TestGeneratedWaresReplaceTheFallbackOnce(t *testing.T) {
	r := smithRig(t)
	st := &fakeStocker{fakeTalker: newFakeTalker()}
	r.g.Talker = st
	r.talk()
	r.act(protocol.NPCLeave)
	r.talk() // only one request until the first one has come back
	if len(st.reqs) != 1 {
		t.Fatalf("%d stock requests", len(st.reqs))
	}
	req := st.reqs[0]
	if len(req.Context.Slots) != 1 || req.Context.Slots[0] != gear.Weapon || req.Context.MaxTier < 1 || req.Region == "" {
		t.Fatalf("request: %+v", req)
	}
	nb := len(r.msgs)
	items := []gear.Item{}
	for i := 1; i <= 5; i++ {
		items = append(items, gear.Item{ID: int64(i), Name: fmt.Sprintf("Lame %d", i), Slot: gear.Weapon, Tier: 1, Family: "axe", Price: 40})
	}
	st.dones[0](npcai.StockReply{Wares: gear.Wares{NPC: r.npc.Key(), Specialty: "lames courtes", Items: items}})
	r.pump()
	if len(r.msgs) <= nb {
		t.Fatal("the player looking at the stock was not updated")
	}
	d := r.last()
	offers := buyOffers(d)
	if d.Specialty != "lames courtes" || len(offers) != 5 || offers[0].Label != "Lame 1" || !strings.HasPrefix(offers[0].Arg, "1.") {
		t.Fatalf("generated stock: %+v", d)
	}
	// A fallback offer (revision 0) buys nothing on the renewed stock.
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, "0.1")
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != gold {
		t.Fatal("purchase with a stale revision")
	}
	r.g.talkAct(r.p, protocol.NPCBuyItem, offers[0].Arg)
	if len(r.p.inv.Items) != 1 || r.p.inv.Items[0].Name != "Lame 1" {
		t.Fatal("purchase from the generated stock")
	}
	r.act(protocol.NPCLeave)
	r.talk()
	if len(st.reqs) != 1 {
		t.Fatal("stock requested again although it is known")
	}
}

func TestFailedStockRetriesLaterNotImmediately(t *testing.T) {
	r := smithRig(t)
	st := &fakeStocker{fakeTalker: newFakeTalker()}
	r.g.Talker = st
	r.talk()
	st.dones[0](npcai.StockReply{Failure: npcai.Unavailable})
	r.pump()
	r.act(protocol.NPCLeave)
	r.talk()
	if len(st.reqs) != 1 {
		t.Fatalf("requested again right after a failure: %d", len(st.reqs))
	}
	r.g.tick += stockRetryTicks + 1
	r.act(protocol.NPCLeave)
	r.talk()
	if len(st.reqs) != 2 {
		t.Fatalf("the request should have been made again: %d", len(st.reqs))
	}
}

// The assortment rises in rank with the level bracket of the vendor's region.
func TestVendorTiersFollowRegionLevelBand(t *testing.T) {
	g := newGame()
	cases := []struct {
		rp       world.RegionPos
		min, max int
	}{
		{world.RegionPos{X: 0, Y: 0}, 1, 3},
		{world.RegionPos{X: 4, Y: 0}, 2, 4},
		{world.RegionPos{X: -7, Y: 3}, 3, 5},
		{world.RegionPos{X: 12, Y: 12}, 4, 5},
	}
	for _, c := range cases {
		npc := world.NPC{Kind: world.NPCSmith, Region: c.rp}
		if g.World.StructureOfRegion(c.rp) != nil && g.World.StructureOfRegion(c.rp).Kind == world.KindTown {
			continue // a town always sells up to rank 5
		}
		ctx := g.vendorContext(npc)
		if ctx.MinTier != c.min || ctx.MaxTier != c.max {
			t.Errorf("region %v: ranks %d-%d, expected %d-%d", c.rp, ctx.MinTier, ctx.MaxTier, c.min, c.max)
		}
		for _, it := range gear.Fallback(npc.Key(), ctx, i18n.FR).Items {
			if it.Tier < ctx.MinTier || it.Tier > ctx.MaxTier {
				t.Errorf("region %v: item of rank %d outside %d-%d", c.rp, it.Tier, ctx.MinTier, ctx.MaxTier)
			}
		}
	}
}

package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

// vendorSpots returns the blacksmith or armorer of the starting village and two free access tiles.
func vendorSpots(t *testing.T) (world.NPC, []spot) {
	t.Helper()
	w := world.New(1)
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCSmith && n.Kind != world.NPCArmorer {
				continue
			}
			var spots []spot
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if _, isNPC := w.NPCAt(p.X, p.Y); w.At(p.X, p.Y).Walkable() && !isNPC {
					spots = append(spots, spot{p, d[0], d[1]})
				}
			}
			if len(spots) >= 1 {
				return n, spots
			}
		}
	}
	t.Skip("no equipment vendor with a free access tile near the start")
	return world.NPC{}, nil
}

func (p *player) bag(what string, pred func(protocol.Bag) bool) protocol.Bag {
	p.t.Helper()
	for range 80 {
		env := recv(p.t, p.c)
		if env.T != protocol.TBag {
			continue
		}
		var b protocol.Bag
		if err := env.Payload(&b); err != nil {
			p.t.Fatal(err)
		}
		if pred(b) {
			return b
		}
	}
	p.t.Fatalf("%s %s: expected bag not received", p.name, what)
	return protocol.Bag{}
}

// untilStat reads snapshots until the requested statistic takes the wanted value.
func (p *player) untilStat(what string, pick func(protocol.Stats) int, want int) {
	p.t.Helper()
	for range 300 {
		env := recv(p.t, p.c)
		if env.T != protocol.TSnapshot {
			continue
		}
		var s protocol.Snapshot
		if err := env.Payload(&s); err != nil {
			p.t.Fatal(err)
		}
		if pick(s.Stats) == want {
			return
		}
	}
	p.t.Fatalf("%s %s: value %d never reached", p.name, what, want)
}

func TestGeneratedShopBuyEquipAndPersistEndToEnd(t *testing.T) {
	e := startTalkServer(t)
	npc, spots := vendorSpots(t)
	slot, field := gearSlotAndField(npc.Kind)
	calls := 0
	e.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		calls++
		mk := func(name string, tier int) map[string]any {
			return map[string]any{"slot": slot, "name": name, "desc": "Solide.", "tier": tier, field: 1000000, "price": 1}
		}
		b, _ := json.Marshal(map[string]any{"specialty": "pièces robustes", "items": []any{
			mk("Pièce un", 1), mk("Pièce deux", 1), mk("Pièce trois", 2), mk("Pièce quatre", 2), mk("Pièce cinq", 3),
		}})
		return llm.Response{Content: string(b), Usage: llm.Usage{Total: 200}}, nil
	}

	p := e.joinChar(t, "aldric", spots[0], store.Character{Level: 5, HP: 30, Gold: 5000})
	first := p.approach() // the fallback is displayed first
	if first.Specialty == "" || len(buyArgs(first)) < 6 || !strings.HasPrefix(buyArgs(first)[0], "0.") {
		t.Fatalf("fallback wares: %+v", first)
	}
	menu := p.until("generated wares", func(d protocol.NPCDialog) bool { return d.Specialty == "pièces robustes" })
	offers := buyOffers(menu)
	if len(offers) != 5 || !strings.HasPrefix(offers[0].Arg, "1.") || calls != 1 {
		t.Fatalf("generated wares: %d offers, %d calls", len(offers), calls)
	}
	for _, o := range offers {
		if o.Price <= 1 {
			t.Errorf("the price chosen by the model (1) was not raised to the minimum: %+v", o)
		}
	}

	// A starting character already wears their class's weapon: it is in the bag before any purchase.
	const starter = 1
	worn := 2 // starting weapon and armor
	if slot == "weapon" {
		worn = 1 // the bought weapon replaces the starting one
	}
	p.act(protocol.NPCBuyItem, offers[2].Arg)
	b := p.bag("achat", func(b protocol.Bag) bool { return len(b.Items) == starter+1 })
	it := b.Items[starter]
	if it.Name != "Pièce trois" || it.Slot != slot || it.Tier != 2 {
		t.Fatalf("bought item: %+v", it)
	}
	if slot == "weapon" {
		// Rank 2: two dice, default family (short sword, d6), quality capped at the tier despite the million offered.
		if it.Dice != "2d6+2" || it.Family != "shortsword" {
			t.Fatalf("bought weapon: %+v", it)
		}
	} else if it.Def < 1 || it.Def > 2 || it.Reduction != "2d6" || it.Family != "medium" {
		t.Fatalf("bought armor: %+v", it)
	}
	if b.DefBonus != 0 {
		t.Fatal("bonus before equipping")
	}

	send(t, p.c, protocol.TGearAction, protocol.GearAction{Action: protocol.GearEquip, ID: it.ID})
	b = p.bag("equipment", func(b protocol.Bag) bool { return len(b.Items) == starter+1 && b.Items[starter].Worn })
	if slot == "weapon" {
		p.untilStat("damage", func(s protocol.Stats) int {
			if s.Dmg == "2d6+2" {
				return 1
			}
			return 0
		}, 1)
	} else {
		if b.DefBonus != it.Def || b.Reduction != "2d6" {
			t.Fatalf("worn armor: bonus %d, reduction %s", b.DefBonus, b.Reduction)
		}
		p.untilStat("defense", func(s protocol.Stats) int { return s.Def }, 10+it.Def)
	}

	// Bag and equipment survive the disconnection and the restart of the session.
	waitUntil(t, "bag saved", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && len(ch.Gear.Items) == starter+1 && len(ch.Gear.Worn) == worn
	})
	p.c.Close(websocket.StatusNormalClosure, "")
	ch, _ := e.be.LoadCharacter("aldric")
	p2 := e.joinChar(t, "aldric", spots[0], *ch)
	b = p2.bag("reconnexion", func(b protocol.Bag) bool { return len(b.Items) == starter+1 })
	if !b.Items[starter].Worn || b.DefBonus != it.Def {
		t.Fatalf("after reconnection: %+v", b)
	}
	// The generated wares are saved: a second player sees them without a new model call.
	w, ok, _ := e.be.Store.GetWares(context.Background(), npc.Key())
	if !ok || w.Specialty != "pièces robustes" || len(w.Items) != 5 {
		t.Fatalf("saved wares: %+v %v", w, ok)
	}
}

func gearSlotAndField(k world.NPCKind) (string, string) {
	if k == world.NPCArmorer {
		return "armor", "def"
	}
	return "weapon", "quality"
}

func buyArgs(d protocol.NPCDialog) (out []string) {
	for _, o := range buyOffers(d) {
		out = append(out, o.Arg)
	}
	return
}

func buyOffers(d protocol.NPCDialog) (out []protocol.NPCOffer) {
	for _, o := range d.Offers {
		if o.Action == protocol.NPCBuyItem {
			out = append(out, o)
		}
	}
	return
}

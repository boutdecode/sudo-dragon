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

// vendorSpots renvoie le forgeron ou l'armurier du village de départ et deux accès libres.
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
	t.Skip("pas de vendeur d'équipement avec un accès libre près du départ")
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
	p.t.Fatalf("%s %s : sac attendu non reçu", p.name, what)
	return protocol.Bag{}
}

// untilStat lit les instantanés jusqu'à ce que la statistique demandée prenne la valeur voulue.
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
	p.t.Fatalf("%s %s : valeur %d jamais atteinte", p.name, what, want)
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
	first := p.approach() // le repli s'affiche d'abord
	if first.Specialty == "" || len(buyArgs(first)) < 6 || !strings.HasPrefix(buyArgs(first)[0], "0.") {
		t.Fatalf("étalage de repli: %+v", first)
	}
	menu := p.until("étalage généré", func(d protocol.NPCDialog) bool { return d.Specialty == "pièces robustes" })
	offers := buyOffers(menu)
	if len(offers) != 5 || !strings.HasPrefix(offers[0].Arg, "1.") || calls != 1 {
		t.Fatalf("étalage généré: %d offres, %d appels", len(offers), calls)
	}
	for _, o := range offers {
		if o.Price <= 1 {
			t.Errorf("le prix choisi par le modèle (1) n'a pas été relevé au minimum: %+v", o)
		}
	}

	p.act(protocol.NPCBuyItem, offers[2].Arg)
	b := p.bag("achat", func(b protocol.Bag) bool { return len(b.Items) == 1 })
	it := b.Items[0]
	bonus := it.Atk + it.Def
	if it.Name != "Pièce trois" || bonus <= 0 || bonus > 12 || it.Slot != slot || it.Tier != 2 {
		t.Fatalf("objet acheté: %+v", it)
	}
	if b.AtkBonus+b.DefBonus != 0 {
		t.Fatal("bonus avant d'équiper")
	}

	send(t, p.c, protocol.TGearAction, protocol.GearAction{Action: protocol.GearEquip, ID: it.ID})
	b = p.bag("équipement", func(b protocol.Bag) bool { return len(b.Items) == 1 && b.Items[0].Worn })
	if b.AtkBonus+b.DefBonus != bonus {
		t.Fatalf("bonus %d/%d, attendu %d", b.AtkBonus, b.DefBonus, bonus)
	}
	if field == "atk" {
		p.untilStat("attaque", func(s protocol.Stats) int { return s.Atk }, 14+bonus)
	} else {
		p.untilStat("défense", func(s protocol.Stats) int { return s.Def }, 6+bonus)
	}

	// Sac et équipement survivent à la déconnexion et au redémarrage de la session.
	waitUntil(t, "sac enregistré", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && len(ch.Gear.Items) == 1 && len(ch.Gear.Worn) == 1
	})
	p.c.Close(websocket.StatusNormalClosure, "")
	ch, _ := e.be.LoadCharacter("aldric")
	p2 := e.joinChar(t, "aldric", spots[0], *ch)
	b = p2.bag("reconnexion", func(b protocol.Bag) bool { return len(b.Items) == 1 })
	if !b.Items[0].Worn || b.AtkBonus+b.DefBonus != bonus {
		t.Fatalf("après reconnexion: %+v", b)
	}
	// L'étalage généré est enregistré : un second joueur le voit sans nouvel appel au modèle.
	w, ok, _ := e.be.Store.GetWares(context.Background(), npc.Key())
	if !ok || w.Specialty != "pièces robustes" || len(w.Items) != 5 {
		t.Fatalf("assortiment enregistré: %+v %v", w, ok)
	}
}

func gearSlotAndField(k world.NPCKind) (string, string) {
	if k == world.NPCArmorer {
		return "armor", "def"
	}
	return "weapon", "atk"
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

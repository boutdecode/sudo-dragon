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
		t.Fatalf("étalage de repli: %d offres, spécialité %q", len(offers), d.Specialty)
	}
	for _, o := range offers {
		if o.Price <= 0 || o.Arg == "" || !strings.Contains(o.Detail, "ATQ") || !o.Available {
			t.Errorf("offre incomplète: %+v", o)
		}
		if !strings.HasPrefix(o.Arg, "0.") {
			t.Errorf("le repli porte la révision 0: %q", o.Arg)
		}
	}
	if _, has := r.offer(protocol.NPCSellMenu); has {
		t.Fatal("rien à revendre avec un sac vide")
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
		t.Fatalf("sac %d objets, or %d → %d (prix %d)", len(r.p.inv.Items), gold, r.p.stats.Gold, o.Price)
	}
	item := r.p.inv.Items[0]
	if item.ID != 1 || item.Origin != r.npc.Key() || item.Slot != gear.Weapon {
		t.Fatalf("%+v", item)
	}
	if n := len(saved); n == 0 || !saved[n-1].GearDirty || len(saved[n-1].Gear.Items) != 1 {
		t.Fatalf("l'achat doit être sauvegardé tout de suite: %+v", saved)
	}
	if len(bags) == 0 || len(bags[len(bags)-1].Items) != 1 {
		t.Fatal("sac non envoyé")
	}

	// Pas de bonus tant que l'objet n'est pas équipé.
	baseAtk := r.p.stats.Atk
	if statsOf(r.p).Atk != baseAtk {
		t.Fatal("bonus sans équipement")
	}
	r.g.GearActNow(r.p, protocol.GearEquip, item.ID, "")
	if statsOf(r.p).Atk != baseAtk+item.Atk || r.p.fighter().Atk != baseAtk+item.Atk {
		t.Fatalf("attaque %d, attendue %d", statsOf(r.p).Atk, baseAtk+item.Atk)
	}
	r.g.GearActNow(r.p, protocol.GearUnequip, 0, string(gear.Weapon))
	if statsOf(r.p).Atk != baseAtk {
		t.Fatal("retrait sans effet")
	}
}

func TestBuyRules(t *testing.T) {
	r := smithRig(t)
	r.talk()
	o := buyOffers(r.last())[0]

	r.p.stats.Gold = o.Price - 1
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != o.Price-1 {
		t.Fatal("achat sans or suffisant")
	}
	r.p.stats.Gold = 100000
	for _, bad := range []string{"", "x", "0.99", "7.1", "0.", ".1", "0.-1"} {
		r.g.talkAct(r.p, protocol.NPCBuyItem, bad)
	}
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != 100000 {
		t.Fatal("un argument invalide a eu un effet")
	}
	for i := 0; i < gear.BagCapacity; i++ {
		r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	}
	if len(r.p.inv.Items) != gear.BagCapacity {
		t.Fatalf("%d objets", len(r.p.inv.Items))
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if len(r.p.inv.Items) != gear.BagCapacity || r.p.stats.Gold != gold {
		t.Fatal("sac plein : achat accepté")
	}
	// Un marchand ne vend pas d'équipement.
	m := newNPCRig(t, world.NPCMerchant)
	m.p.stats.Gold = 1000
	m.talk()
	m.g.talkAct(m.p, protocol.NPCBuyItem, "0.1")
	if len(m.p.inv.Items) != 0 || m.p.stats.Gold != 1000 {
		t.Fatal("un marchand a vendu de l'équipement")
	}
}

func TestReputationDiscountsEquipmentAndMenuAgrees(t *testing.T) {
	r := smithRig(t)
	r.p.rep = map[world.RegionPos]int{r.npc.Region: 15}
	r.talk()
	o := buyOffers(r.last())[0]
	w, _ := r.g.waresFor(r.p, r.npc)
	if o.Price >= w.Items[0].Price {
		t.Fatalf("pas de remise: %d pour %d", o.Price, w.Items[0].Price)
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, o.Arg)
	if gold-r.p.stats.Gold != o.Price {
		t.Fatalf("débité %d, annoncé %d", gold-r.p.stats.Gold, o.Price)
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
		t.Fatalf("seul l'objet non porté se revend: %+v", sells)
	}
	// L'objet porté ne se vend pas, même avec son identifiant.
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCSellItem, "1")
	if len(r.p.inv.Items) != 2 || r.p.stats.Gold != gold {
		t.Fatal("objet porté revendu")
	}
	r.g.talkAct(r.p, protocol.NPCSellItem, sells[0].Arg)
	if len(r.p.inv.Items) != 1 || r.p.stats.Gold != gold+sells[0].Price {
		t.Fatalf("revente: %d objets, or %d (+%d attendu)", len(r.p.inv.Items), r.p.stats.Gold-gold, sells[0].Price)
	}
	if r.last().Mode != protocol.NPCModeMenu {
		t.Fatalf("plus rien à revendre: retour au menu, mode %q", r.last().Mode)
	}
}

func TestEquipRules(t *testing.T) {
	r := smithRig(t)
	r.p.inv.Add(gear.Item{Name: "Grande lame", Slot: gear.Weapon, Tier: 5, Atk: 10, Price: 500})
	r.p.inv.Add(gear.Item{Name: "Petite lame", Slot: gear.Weapon, Tier: 1, Atk: 2, Price: 50})
	r.p.stats.Level = 5
	var bags []protocol.Bag
	r.p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }

	r.g.GearActNow(r.p, protocol.GearEquip, 1, "") // palier 5 : niveau 19 requis
	if r.p.inv.IsWorn(1) || len(bags) == 0 || len(bags[len(bags)-1].Notice) == 0 {
		t.Fatalf("niveau insuffisant ignoré: %+v", bags)
	}
	r.g.GearActNow(r.p, protocol.GearEquip, 2, "")
	r.p.stats.Level = 19
	r.g.GearActNow(r.p, protocol.GearEquip, 1, "") // remplace la petite lame
	if r.p.inv.IsWorn(2) || !r.p.inv.IsWorn(1) {
		t.Fatal("un objet par emplacement")
	}
	// Pas de changement d'équipement en plein combat.
	r.p.fight = &fight{}
	r.g.GearActNow(r.p, protocol.GearEquip, 2, "")
	if !r.p.inv.IsWorn(1) {
		t.Fatal("équipement changé en combat")
	}
	r.p.fight = nil
	r.g.GearActNow(r.p, protocol.GearEquip, 999, "") // inconnu : sans effet
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
		t.Fatalf("après reconnexion: %+v", p2.inv)
	}
	// Une sauvegarde trafiquée est ramenée dans les bornes.
	bad := Saved{Stats: r.p.stats, Gear: gear.Inventory{
		Items: []gear.Item{{ID: 1, Name: "Dieu", Slot: gear.Weapon, Tier: 99, Atk: 1 << 30, Price: 1 << 40}, {ID: 1, Name: "Doublon", Slot: gear.Helm}},
		Worn:  map[gear.Slot]int64{gear.Weapon: 1, gear.Armor: 1},
	}}
	p3 := r.g.joinWith("hero3", func(protocol.Snapshot) {}, &bad)
	atk, def := p3.gearBonus()
	if len(p3.inv.Items) != 1 || atk > 12 || def != 0 {
		t.Fatalf("bonus %d/%d, %d objets", atk, def, len(p3.inv.Items))
	}
}

// fakeStocker est un Talker qui fournit aussi les étalages.
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
	r.talk() // une seule demande tant que la première n'est pas revenue
	if len(st.reqs) != 1 {
		t.Fatalf("%d demandes d'étalage", len(st.reqs))
	}
	req := st.reqs[0]
	if len(req.Context.Slots) != 1 || req.Context.Slots[0] != gear.Weapon || req.Context.MaxTier < 1 || req.Region == "" {
		t.Fatalf("demande: %+v", req)
	}
	nb := len(r.msgs)
	items := []gear.Item{}
	for i := 1; i <= 5; i++ {
		items = append(items, gear.Item{ID: int64(i), Name: fmt.Sprintf("Lame %d", i), Slot: gear.Weapon, Tier: 1, Atk: 2, Price: 40})
	}
	st.dones[0](npcai.StockReply{Wares: gear.Wares{NPC: r.npc.Key(), Specialty: "lames courtes", Items: items}})
	r.pump()
	if len(r.msgs) <= nb {
		t.Fatal("le joueur qui regarde l'étalage n'a pas été mis à jour")
	}
	d := r.last()
	offers := buyOffers(d)
	if d.Specialty != "lames courtes" || len(offers) != 5 || offers[0].Label != "Lame 1" || !strings.HasPrefix(offers[0].Arg, "1.") {
		t.Fatalf("étalage généré: %+v", d)
	}
	// Une offre du repli (révision 0) n'achète rien sur l'étalage renouvelé.
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCBuyItem, "0.1")
	if len(r.p.inv.Items) != 0 || r.p.stats.Gold != gold {
		t.Fatal("achat avec une révision périmée")
	}
	r.g.talkAct(r.p, protocol.NPCBuyItem, offers[0].Arg)
	if len(r.p.inv.Items) != 1 || r.p.inv.Items[0].Name != "Lame 1" {
		t.Fatal("achat de l'étalage généré")
	}
	r.act(protocol.NPCLeave)
	r.talk()
	if len(st.reqs) != 1 {
		t.Fatal("étalage redemandé alors qu'il est connu")
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
		t.Fatalf("redemandé tout de suite après un échec: %d", len(st.reqs))
	}
	r.g.tick += stockRetryTicks + 1
	r.act(protocol.NPCLeave)
	r.talk()
	if len(st.reqs) != 2 {
		t.Fatalf("la demande aurait dû être refaite: %d", len(st.reqs))
	}
}

// Équilibrage : le meilleur équipement d'un palier, porté au niveau requis, aide mais ne rend ni invulnérable
// ni capable d'abattre le seigneur de guerre sans niveau. Les dégâts valent max(1, atk − def ± 1).
func TestBalanceBestGearStaysFair(t *testing.T) {
	best := func(tier int) (atk, def int) {
		for _, s := range gear.Slots {
			_, hi := gear.Bounds(s, tier)
			if s == gear.Weapon {
				atk += hi
			} else {
				def += hi
			}
		}
		return
	}
	var warlord species
	for _, sp := range speciesList {
		if sp.Boss {
			warlord = sp
		}
	}
	for tier := 1; tier <= gear.MaxTier; tier++ {
		level := gear.MinLevelOf[tier]
		base := combat.StatsAt(level, 0, 0)
		atk, def := best(tier)
		if atk > base.Atk*40/100 {
			t.Errorf("palier %d : +%d attaque pour %d de base (>40 %%)", tier, atk, base.Atk)
		}
		if def > base.Def*60/100+2 {
			t.Errorf("palier %d : +%d défense pour %d de base", tier, def, base.Def)
		}
		// Face au monstre le plus fort que ce palier affronte, le joueur équipé encaisse encore des coups.
		for _, sp := range speciesList {
			if sp.Garrison || sp.XP > speciesXPBase+speciesXPPerLvl*level {
				continue
			}
			plain := max(1, sp.Atk-base.Def)
			geared := max(1, sp.Atk-base.Def-def)
			if plain > 1 && geared*100 < plain*40 {
				t.Errorf("palier %d, %s : dégâts %d → %d, l'équipement les divise par plus de 2,5", tier, sp.Kind, plain, geared)
			}
		}
	}
	// Le meilleur équipement disponible à un niveau avance d'au plus 3 niveaux le moment où l'on peut
	// vaincre le seigneur de guerre en un combat (les dégâts sont déterministes à ±1 près).
	wins := func(level int, geared bool) bool {
		base := combat.StatsAt(level, 0, 0)
		atk, def := 0, 0
		if geared {
			for tier := gear.MaxTier; tier >= 1; tier-- {
				if gear.MinLevelOf[tier] <= level {
					atk, def = best(tier)
					break
				}
			}
		}
		perHit := max(1, base.Atk+atk-warlord.Def)
		taken := max(1, warlord.Atk-base.Def-def)
		turns := (warlord.HP + perHit - 1) / perHit
		return turns*taken < base.MaxHP
	}
	firstPlain, firstGeared := 0, 0
	for level := 1; level <= 40 && (firstPlain == 0 || firstGeared == 0); level++ {
		if firstPlain == 0 && wins(level, false) {
			firstPlain = level
		}
		if firstGeared == 0 && wins(level, true) {
			firstGeared = level
		}
	}
	if firstPlain == 0 || firstGeared < firstPlain-3 {
		t.Errorf("le seigneur de guerre tombe dès le niveau %d équipé contre %d sans équipement (plus de 3 niveaux d'avance)", firstGeared, firstPlain)
	}
	_ = i18n.FR
}

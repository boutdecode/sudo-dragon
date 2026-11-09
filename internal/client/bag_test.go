package client

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

func bagMsg(t *testing.T, b protocol.Bag) eventMsg {
	t.Helper()
	data, err := protocol.Encode(protocol.TBag, b)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := protocol.Decode(data)
	return eventMsg{Event{Env: env}}
}

func sampleBag() protocol.Bag {
	return protocol.Bag{
		Capacity: 24, DefBonus: 1,
		Items: []protocol.ItemInfo{
			{ID: 1, Name: "Épée longue en acier", Desc: "Une lame bien équilibrée.", Slot: "weapon", Tier: 2, Family: "longsword", Dice: "2d6+2", Kind: "melee", MinLevel: 5, Sell: 40, Worn: true, Origin: "Forgeron Hubert"},
			{ID: 2, Name: "Hache en fer", Slot: "weapon", Tier: 1, Family: "axe", Dice: "1d6", Kind: "melee", MinLevel: 1, Sell: 20},
			{ID: 3, Name: "Casque en cuivre", Slot: "helm", Tier: 1, Def: 1, MinLevel: 1, Sell: 12, Worn: true},
		},
	}
}

func withGear(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(bagMsg(t, sampleBag()))
	return tm.(Model)
}

func sentGearActions(out chan []byte) (acts []protocol.GearAction) {
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TGearAction {
				var a protocol.GearAction
				_ = env.Payload(&a)
				acts = append(acts, a)
			}
		default:
			return
		}
	}
}

func TestBagScreenShowsWornItemsAndDetail(t *testing.T) {
	m := withGear(t, playingModel(t, 120, 36))
	m = press(m, "i")
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Équipé", "Arme:", "Épée longue en acier", "Casque:", "Casque en cuivre", "Sac (3/24)", "Hache en fer", "Dégâts 2d6+2 (tranchant)", "Niveau requis : 5", "Forgeron Hubert"} {
		if !strings.Contains(view, want) {
			t.Errorf("sac sans %q:\n%s", want, view)
		}
	}
	m = press(m, "down")
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Par rapport à l'objet porté : -6") {
		t.Errorf("comparaison avec l'objet porté absente:\n%s", view)
	}
}

func TestBagKeysEquipAndUnequip(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 36))
	m = withGear(t, m)
	m = press(m, "i")
	m = press(m, "e") // première ligne : épée portée → retrait
	acts := sentGearActions(out)
	if len(acts) != 1 || acts[0].Action != protocol.GearUnequip || acts[0].Slot != "weapon" {
		t.Fatalf("%v", acts)
	}
	m = press(m, "down")
	m = press(m, "e") // hache non portée → équipement
	acts = sentGearActions(out)
	if len(acts) != 1 || acts[0].Action != protocol.GearEquip || acts[0].ID != 2 {
		t.Fatalf("%v", acts)
	}
	m = press(m, "esc")
	if m.inventory {
		t.Fatal("Échap devrait fermer le sac")
	}
}

func TestBagIsSanitizedAndFitsAllSizes(t *testing.T) {
	hostile := sampleBag()
	hostile.Items[0].Name = "Épée\x1b[2J\x1b]0;pwn\x07\u202e rouge"
	hostile.Items[0].Desc = strings.Repeat("très long ", 60)
	hostile.Items[1].Slot = "ring"
	hostile.Notice = []string{"Vous équipez \x1b[2J : Épée"}
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {200, 60}} {
		tm, _ := playingModel(t, sz[0], sz[1]).Update(bagMsg(t, hostile))
		m := press(tm.(Model), "i")
		view := m.View().Content
		if strings.Contains(view, "\x1b]0;") || strings.ContainsAny(ansi.Strip(view), "\u202e") {
			t.Fatalf("%v : texte non neutralisé", sz)
		}
		lines := strings.Split(ansi.Strip(view), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v : %d lignes > hauteur", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v ligne %d : largeur %d > %d", sz, i, w, sz[0])
			}
		}
	}
	tm, _ := playingModel(t, 120, 36).Update(bagMsg(t, hostile))
	last := tm.(Model).chat[len(tm.(Model).chat)-1]
	if strings.Contains(last.Text, "\x1b") {
		t.Fatalf("notification: %q", last.Text)
	}
}

func shopDialog() protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "smith", Name: "Forgeron Hubert", Place: "Brumeford", Mode: protocol.NPCModeMenu, Gold: 500, Specialty: "lames courtes et dagues",
		Offers: []protocol.NPCOffer{
			{Action: protocol.NPCBuyItem, Label: "Dague en fer", Detail: "ATQ 2 (+2) · niv 1", Price: 40, Arg: "1.1", Available: true,
				Info: &protocol.ItemInfo{Name: "Dague en fer", Desc: "Une lame courte et fiable.", Slot: "weapon", Tier: 1, Dice: "1d4", Family: "dagger", MinLevel: 1, Sell: 20}},
			{Action: protocol.NPCBuyItem, Label: "Épée en acier", Detail: "ATQ 4 (+4) · niv 4", Price: 120, Arg: "1.2", Available: true,
				Info: &protocol.ItemInfo{Name: "Épée en acier", Desc: "Une épée lourde forgée à Brumeford.", Slot: "weapon", Tier: 2, Dice: "1d8", Family: "sword", MinLevel: 4, Sell: 60}},
			{Action: protocol.NPCSellMenu, Label: "Revendre un objet", Available: true},
		},
	}
}

func TestShopListsWaresAndBuysByNumber(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 36))
	m = openNPC(t, m, shopDialog())
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"[1] Dague en fer", "ATQ 2 (+2) · niv 1", "40 or", "[2] Épée en acier", "120 or", "[V] Revendre un objet", "Spécialité : lames courtes"} {
		if !strings.Contains(view, want) {
			t.Errorf("boutique sans %q:\n%s", want, view)
		}
	}
	m = press(m, "2")
	data := <-out
	env, _ := protocol.Decode(data)
	var a protocol.NPCAction
	_ = env.Payload(&a)
	if a.Action != protocol.NPCBuyItem || a.Text != "1.2" {
		t.Fatalf("%+v", a)
	}
	m = press(m, "v")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCSellMenu {
		t.Fatalf("la touche V devrait ouvrir la revente: %v", got)
	}
}

func TestSellModeUsesNumbersAndEscapeGoesBack(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 36))
	d := shopDialog()
	d.Mode = protocol.NPCModeSell
	d.Offers = []protocol.NPCOffer{
		{Action: protocol.NPCSellItem, Label: "Hache en fer", Detail: "ATQ 2 (+0) · niv 1", Price: 20, Gain: true, Arg: "2", Available: true},
		{Action: protocol.NPCBack, Label: "Retour", Available: true},
	}
	m = openNPC(t, m, d)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "[1] Hache en fer") || !strings.Contains(view, "+20 or") {
		t.Fatalf("revente:\n%s", view)
	}
	m = press(m, "1")
	data := <-out
	env, _ := protocol.Decode(data)
	var a protocol.NPCAction
	_ = env.Payload(&a)
	if a.Action != protocol.NPCSellItem || a.Text != "2" {
		t.Fatalf("%+v", a)
	}
	m = press(m, "esc")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCBack {
		t.Fatalf("Échap doit revenir à la boutique, pas quitter: %v", got)
	}
}

func TestShopFitsAllSizes(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {200, 60}} {
		m := openNPC(t, playingModel(t, sz[0], sz[1]), shopDialog())
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v : %d lignes > hauteur", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v ligne %d : largeur %d > %d", sz, i, w, sz[0])
			}
		}
	}
}

func TestShopTabInspectsAndEnterBuys(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 36))
	m = openNPC(t, m, shopDialog())
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Détails") || !strings.Contains(view, "Tab") {
		t.Fatalf("panneau de détails absent:\n%s", view)
	}
	tab := func(m Model, k tea.KeyPressMsg) Model { tm, _ := m.Update(k); return tm.(Model) }
	m = tab(m, tea.KeyPressMsg{Code: tea.KeyTab})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Une lame courte et fiable.") || strings.Contains(view, "Une épée lourde") {
		t.Fatalf("détails du 1er objet:\n%s", view)
	}
	m = tab(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Une épée lourde forgée") {
		t.Fatalf("détails du 2e objet:\n%s", view)
	}
	m = tab(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.npcSel != 0 {
		t.Fatalf("shift+tab: sel=%d", m.npcSel)
	}
	m = tab(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = press(m, "enter")
	env, _ := protocol.Decode(<-out)
	var a protocol.NPCAction
	_ = env.Payload(&a)
	if a.Action != protocol.NPCBuyItem || a.Text != "1.2" {
		t.Fatalf("Entrée devrait acheter l'objet examiné: %+v", a)
	}
}

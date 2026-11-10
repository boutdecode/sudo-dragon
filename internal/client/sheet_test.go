package client

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func sheetModel(lang i18n.Lang, w, h int) Model {
	m := New("ws://x/ws", "alice")
	m.lang, m.width, m.height = lang, w, h
	m.state, m.hasSnap = statePlaying, true
	m.world = world.New(1)
	m.snap = protocol.Snapshot{Self: protocol.Entity{Name: "alice", X: 12, Y: -3},
		Stats: protocol.Stats{Level: 4, XP: 7, XPNext: 40, HP: 20, MaxHP: 30, Def: 13, Hit: 3, Dmg: "2d6+3", Potions: 2, Gold: 120, Mana: 3, MaxMana: 8}}
	m.bag = protocol.Bag{Class: "mage", Scores: []int{8, 12, 17, 10, 14, 6}, Style: "magic", DefBonus: 2, Reduction: "2d8", Kills: 31, BossKills: 1, Deaths: 3,
		Items: []protocol.ItemInfo{{ID: 1, Name: "Bâton de chêne", Slot: "weapon", Kind: "magic", Worn: true, Dice: "1d6+1", Family: "staff"}}}
	m.quests = protocol.Quests{Standing: []protocol.Standing{{Place: "Valdurne", Tier: "Connu", Points: 4}}}
	return m
}

func TestSheetShowsEverythingAndInventoryNoLongerShowsAbilities(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		m := sheetModel(lang, 120, 40)
		m.sheet = true
		out := ansi.Strip(m.sheetView(116, 30))
		for _, want := range []string{"alice", "d6", "20/30", "3/8", "17 (+3)", "6 (-2)", "12, -3", "Valdurne", "Bâton de chêne", "31", "65 %", "+4 %"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: fiche sans %q", lang, want)
			}
		}
		inv := ansi.Strip(m.inventoryView(116, 30))
		for _, bad := range []string{"FOR", "STR", "(+3)", "Palmarès", "Record"} {
			if strings.Contains(inv, bad) {
				t.Errorf("%s: l'inventaire montre encore %q", lang, bad)
			}
		}
	}
}

func TestSheetKeyOpensAndClosesAndFitsEverySize(t *testing.T) {
	m := sheetModel(i18n.FR, 80, 24)
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = tm.(Model)
	if !m.sheet {
		t.Fatal("la touche c devrait ouvrir la fiche")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tm.(Model).sheet {
		t.Fatal("Échap devrait fermer la fiche")
	}
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		for _, sz := range [][2]int{{80, 24}, {110, 34}, {200, 60}} {
			m := sheetModel(lang, sz[0], sz[1])
			m.sheet = true
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s %v: %d lignes", lang, sz, len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > sz[0] {
					t.Errorf("%s %v: ligne trop large", lang, sz)
					break
				}
			}
		}
	}
}

func TestSheetShowsSanctuaryBlessings(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		m := sheetModel(lang, 120, 40)
		m.sheet = true
		if out := ansi.Strip(m.sheetView(116, 30)); strings.Contains(out, m.t("sheet.blessings")) {
			t.Errorf("%s: section des bénédictions affichée sans bénédiction", lang)
		}
		m.bagAt = time.Now()
		m.bag.Blessings = []protocol.Blessing{{Kind: protocol.BlessDef, Bonus: 2, Secs: 125}, {Kind: protocol.BlessXP, Bonus: 50, Secs: 30}}
		out := ansi.Strip(m.sheetView(116, 30))
		for _, want := range []string{m.t("sheet.blessings"), "+2 DEF", "(2:05)", "+50 %", "(0:30)"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: fiche sans %q\n%s", lang, want, out)
			}
		}
		// La DEF de la bénédiction ne doit pas être comptée dans la base du détail « base + AGI + équipement ».
		if got, want := m.blessingDef(), 2; got != want {
			t.Errorf("%s: DEF de bénédiction %d, attendu %d", lang, got, want)
		}
		// Un bonus échu disparaît de la fiche sans nouveau message du serveur.
		m.bagAt = time.Now().Add(-time.Minute)
		if out := ansi.Strip(m.sheetView(116, 30)); strings.Contains(out, "+50 %") || !strings.Contains(out, "+2 DEF") {
			t.Errorf("%s: les bonus échus ne disparaissent pas:\n%s", lang, out)
		}
	}
}

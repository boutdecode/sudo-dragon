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
	m := NewLang("ws://x/ws", "alice", i18n.FR)
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
				t.Errorf("%s: sheet without %q", lang, want)
			}
		}
		inv := ansi.Strip(m.inventoryView(116, 30))
		for _, bad := range []string{"FOR", "STR", "(+3)", "Palmarès", "Record"} {
			if strings.Contains(inv, bad) {
				t.Errorf("%s: the inventory still shows %q", lang, bad)
			}
		}
	}
}

func TestSheetKeyOpensAndClosesAndFitsEverySize(t *testing.T) {
	m := sheetModel(i18n.FR, 80, 24)
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = tm.(Model)
	if !m.sheet {
		t.Fatal("the p key should open the sheet")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tm.(Model).sheet {
		t.Fatal("Escape should close the sheet")
	}
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		for _, sz := range [][2]int{{80, 24}, {110, 34}, {200, 60}} {
			m := sheetModel(lang, sz[0], sz[1])
			m.sheet = true
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s %v: %d lines", lang, sz, len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > sz[0] {
					t.Errorf("%s %v: line too wide", lang, sz)
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
			t.Errorf("%s: blessings section shown without a blessing", lang)
		}
		m.bagAt = time.Now()
		m.bag.Blessings = []protocol.Blessing{{Kind: protocol.BlessDef, Bonus: 2, Secs: 125}, {Kind: protocol.BlessXP, Bonus: 50, Secs: 30}}
		out := ansi.Strip(m.sheetView(116, 30))
		for _, want := range []string{m.t("sheet.blessings"), "+2 DEF", "(2:05)", "+50 %", "(0:30)"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: sheet without %q\n%s", lang, want, out)
			}
		}
		// The blessing's DEF must not be counted in the base of the "base + AGI + equipment" detail.
		if got, want := m.blessingDef(), 2; got != want {
			t.Errorf("%s: blessing DEF %d, expected %d", lang, got, want)
		}
		// An expired bonus disappears from the sheet without a new server message.
		m.bagAt = time.Now().Add(-time.Minute)
		if out := ansi.Strip(m.sheetView(116, 30)); strings.Contains(out, "+50 %") || !strings.Contains(out, "+2 DEF") {
			t.Errorf("%s: expired bonuses do not disappear:\n%s", lang, out)
		}
	}
}

func TestSheetShowsTreasureMaps(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		m := sheetModel(lang, 120, 40)
		m.sheet = true
		if out := ansi.Strip(m.sheetView(116, 30)); strings.Contains(out, m.t("sheet.maps")) {
			t.Errorf("%s: maps section shown without a map", lang)
		}
		m.bag.Maps = []protocol.TreasureMap{{X: 112, Y: -3}}
		out := ansi.Strip(m.sheetView(116, 30))
		for _, want := range []string{m.t("sheet.maps"), "(112, -3)", "100"} { // the sheet is at (12, -3)
			if !strings.Contains(out, want) {
				t.Errorf("%s: sheet without %q\n%s", lang, want, out)
			}
		}
	}
}

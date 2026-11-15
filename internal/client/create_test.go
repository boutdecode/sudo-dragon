package client

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
)

// nextEvent reads the next server message and passes it to the model.
func nextEvent(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(waitEvent(m.conn)())
	return tm.(Model)
}

// finishCreation confirms the creation of a model on the creation screen and waits for entry into the game.
func finishCreation(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = key(m, keyEnter)
	for range 5 {
		if m = nextEvent(t, m); m.state == statePlaying {
			return m
		}
	}
	t.Fatalf("did not enter the game: state=%v err=%q", m.state, m.errMsg)
	return m
}

func creatingModel(t *testing.T) Model {
	t.Helper()
	url := startServer(t)
	m := NewLang(url, "alice", i18n.FR)
	m, _ = key(m, keyCtrlR)
	m = typeText(m, "un-bon-mot-de-passe")
	m, cmd := key(m, keyEnter)
	tm, _ := m.Update(cmd())
	m = tm.(Model)
	if m.state != stateCreating {
		t.Fatalf("state=%v err=%q", m.state, m.errMsg)
	}
	t.Cleanup(func() {
		if m.conn != nil {
			m.conn.Close()
		}
	})
	return m
}

func TestCreationRerollIsLimitedAndScoresStayInRange(t *testing.T) {
	m := creatingModel(t)
	if len(m.create.data.Scores) != 6 || m.create.data.Left != 3 {
		t.Fatalf("initial roll: %+v", m.create.data)
	}
	for _, v := range m.create.data.Scores {
		if v < 3 || v > 18 {
			t.Fatalf("score outside 3-18: %v", m.create.data.Scores)
		}
	}
	for want := 2; want >= 0; want-- {
		m, _ = key(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
		m = nextEvent(t, m)
		if m.create.data.Left != want {
			t.Fatalf("rerolls left: %d, expected %d", m.create.data.Left, want)
		}
		m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	before := append([]int(nil), m.create.data.Scores...)
	m, _ = key(m, tea.KeyPressMsg{Code: 'r', Text: "r"}) // no more rerolls: nothing is sent
	if got := m.create.data.Scores; len(got) != len(before) {
		t.Fatal("roll modified")
	}
	for i, v := range before {
		if m.create.data.Scores[i] != v {
			t.Fatal("roll modified without a reroll")
		}
	}
}

func TestCreationRerollsOnlyTheChosenAbility(t *testing.T) {
	m := creatingModel(t)
	before := append([]int(nil), m.create.data.Scores...)
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyDown}) // INT
	m, _ = key(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = nextEvent(t, m)
	for i, v := range before {
		if i != 2 && m.create.data.Scores[i] != v {
			t.Fatalf("ability %d changed", i)
		}
	}
}

// pressN sends the same key n times.
func pressN(m Model, k tea.KeyPressMsg, n int) Model {
	for range n {
		m, _ = key(m, k)
	}
	return m
}

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	keyLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyRight = tea.KeyPressMsg{Code: tea.KeyRight}
)

func TestCreationClassAndRaceRowsAndEscape(t *testing.T) {
	m := creatingModel(t)
	// On an ability, ←→ change nothing.
	m = pressN(m, keyRight, 1)
	if m.create.classID() != hero.Warrior || m.create.raceID() != hero.Human {
		t.Fatalf("←→ on an ability: %v %v", m.create.classID(), m.create.raceID())
	}
	// ↓ past the six abilities reaches the class row, then the race row.
	m = pressN(m, keyDown, 6)
	if m.create.sel != rowClass {
		t.Fatalf("row: %d", m.create.sel)
	}
	m = pressN(m, keyRight, 1)
	if m.create.classID() != hero.Rogue {
		t.Fatalf("class: %v", m.create.classID())
	}
	m = pressN(m, keyLeft, 2)
	if m.create.classID() != hero.Mage || m.create.raceID() != hero.Human {
		t.Fatalf("class %v race %v", m.create.classID(), m.create.raceID())
	}
	// No reroll from the class row.
	left := m.create.data.Left
	m = pressN(m, tea.KeyPressMsg{Code: 'r', Text: "r"}, 1)
	if m.create.data.Left != left {
		t.Fatal("reroll from the class row")
	}

	raw := m.create.rolled()
	m = pressN(m, keyDown, 1)
	m = pressN(m, keyRight, 2)
	if m.create.sel != rowRace || m.create.raceID() != hero.Dwarf || m.create.classID() != hero.Mage {
		t.Fatalf("row %d race %v class %v", m.create.sel, m.create.raceID(), m.create.classID())
	}
	if sc := m.create.scores(); sc[hero.CON] != raw[hero.CON]+2 || sc[hero.STR] != raw[hero.STR] {
		t.Fatalf("dwarf bonus: %v from %v", sc, raw)
	}
	m = pressN(m, keyLeft, 3)
	if m.create.raceID() != hero.Halfling {
		t.Fatalf("race: %v", m.create.raceID())
	}
	// ↓ wraps back to the first ability, ↑ to the race row.
	m = pressN(m, keyDown, 1)
	if m.create.sel != 0 {
		t.Fatalf("wrap down: %d", m.create.sel)
	}
	m = pressN(m, keyUp, 1)
	if m.create.sel != rowRace {
		t.Fatalf("wrap up: %d", m.create.sel)
	}
	m, _ = key(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.state != stateLogin || m.conn != nil {
		t.Fatalf("return to login expected: %v", m.state)
	}
}

func TestCreatedCharacterGetsChosenClassAndSheet(t *testing.T) {
	m := creatingModel(t)
	m = pressN(m, keyUp, 2)    // class row
	m = pressN(m, keyRight, 2) // mage
	m = pressN(m, keyDown, 1)  // race row
	m = pressN(m, keyRight, 1) // elf
	want := m.create.scores()
	m = finishCreation(t, m)
	for range 40 { // the sheet arrives with the bag
		if m.bag.Class != "" {
			break
		}
		m = nextEvent(t, m)
	}
	if m.bag.Class != "mage" || m.bag.Race != "elf" || len(m.bag.Scores) != 6 || m.bag.Scores[hero.INT] != want[hero.INT] {
		t.Fatalf("sheet: %+v (expected %v)", m.bag, want)
	}
	if head := ansi.Strip(strings.Join(m.sheetLeft(40), "\n")); !strings.Contains(head, "Elfe Magicien") {
		t.Fatalf("sheet header: %q", head)
	}
	if line := m.scoresLine(); !strings.Contains(line, "FOR") || !strings.Contains(line, "CHA") {
		t.Fatalf("abilities line: %q", line)
	}
}

func TestCreationScreenFitsEverySizeInBothLanguages(t *testing.T) {
	m := NewLang("ws://x/ws", "alice", i18n.FR)
	m.state = stateCreating
	m.create = newCreateState(protocol.Create{Scores: []int{18, 3, 10, 12, 8, 15}, Left: 2, Level: 1})
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		m.lang = lang
		for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
			m.width, m.height = sz[0], sz[1]
			out := ansi.Strip(m.View().Content)
			lines := strings.Split(out, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s %v: %d lines", lang, sz, len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > sz[0] {
					t.Errorf("%s %v: line too wide (%d)", lang, sz, ansi.StringWidth(l))
					break
				}
			}
			// Human by default: +2 STR on the 18.
			for _, want := range []string{"20 (+5) +2", "3 (-4)", "d12", "d6"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s %v: %q missing", lang, sz, want)
				}
			}
		}
	}
}

func TestCleanCreateBoundsHostileInput(t *testing.T) {
	c := cleanCreate(protocol.Create{Scores: []int{-5, 1000}, Left: 99, Level: -3, Classes: []protocol.ClassInfo{{ID: "hacker", Die: 1000}},
		Races: []protocol.RaceInfo{{ID: "orc", Ability: 0, Bonus: 50}, {ID: "elf", Ability: 0, Bonus: 50}}})
	if len(c.Scores) != 6 || c.Scores[0] != 0 || c.Scores[1] != 99 || c.Left != 3 || c.Level != 1 || len(c.Classes) != 3 ||
		len(c.Races) != 1 || c.Races[0] != (protocol.RaceInfo{ID: "elf", Ability: int(hero.INT), Bonus: hero.RaceBonus}) {
		t.Fatalf("%+v", c)
	}
}

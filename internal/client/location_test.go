package client

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func moveTo(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	data, err := protocol.Encode(protocol.TSnapshot, protocol.Snapshot{Tick: 2, Self: protocol.Entity{ID: 1, Name: "bob", X: x, Y: y}})
	if err != nil {
		t.Fatal(err)
	}
	env, _ := protocol.Decode(data)
	tm, _ := m.Update(eventMsg{Event{Env: env}})
	return tm.(Model)
}

func lastSystemLines(m Model, n int) []string {
	var out []string
	for _, l := range m.chat {
		if l.Channel == protocol.ChanSystem {
			out = append(out, l.Text)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func villagePos(t *testing.T) (world.Pos, *world.Structure) {
	t.Helper()
	w := world.New(1)
	sp := w.FindSpawn()
	s := w.StructuresNear(sp, 300)[0]
	return world.Pos{X: s.Origin.X + s.W/2, Y: s.Origin.Y + s.H/2}, s
}

func TestMapTitleShowsTerritoryAndPlace(t *testing.T) {
	pos, s := villagePos(t)
	m := moveTo(t, playingModel(t, 140, 36), pos.X, pos.Y)
	view := ansi.Strip(m.View().Content)
	region := m.world.TerritoryAt(pos.X, pos.Y).NameIn(i18n.FR)
	for _, want := range []string{region, "village " + s.Name} {
		if !strings.Contains(view, want) {
			t.Errorf("map title without %q:\n%s", want, strings.Join(strings.Split(view, "\n")[:3], "\n"))
		}
	}
	// In the wild: the region only, no place.
	far := moveTo(t, m, pos.X+3000, pos.Y+3000)
	title := far.mapTitle(pos.X+3000, pos.Y+3000, 100)
	if strings.Contains(title, "village") || strings.Contains(title, "ville") || strings.Contains(title, "forteresse") {
		t.Errorf("place shown in the wild: %q", title)
	}
	if !strings.Contains(title, far.world.TerritoryAt(pos.X+3000, pos.Y+3000).NameIn(i18n.FR)) {
		t.Errorf("region missing: %q", title)
	}
}

func TestMapTitleAlwaysFits(t *testing.T) {
	pos, _ := villagePos(t)
	m := moveTo(t, playingModel(t, 140, 36), pos.X, pos.Y)
	for w := 5; w <= 120; w++ {
		if got := ansi.StringWidth(m.mapTitle(pos.X, pos.Y, w)); got > w {
			t.Fatalf("width %d: title of %d columns", w, got)
		}
	}
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {120, 36}} {
		mm := moveTo(t, playingModel(t, sz[0], sz[1]), pos.X, pos.Y)
		for i, l := range strings.Split(ansi.Strip(mm.View().Content), "\n") {
			if ansi.StringWidth(l) > sz[0] {
				t.Errorf("%v line %d too wide", sz, i)
			}
		}
	}
}

func TestArrivalMessagesAreAnnouncedOnceAndInTheRightLanguage(t *testing.T) {
	pos, s := villagePos(t)
	m := playingModel(t, 140, 36) // first snapshot at (5, 5): silent
	if got := lastSystemLines(m, 5); len(got) != 0 {
		t.Fatalf("announcement on the first position: %v", got)
	}
	m = moveTo(t, m, pos.X, pos.Y)
	got := lastSystemLines(m, 5)
	if len(got) == 0 || !strings.Contains(strings.Join(got, " "), s.Name) {
		t.Fatalf("arrival at the place not announced: %v", got)
	}
	n := len(m.chat)
	m = moveTo(t, m, pos.X+1, pos.Y) // same place: nothing more
	if len(m.chat) != n {
		t.Fatalf("announcement repeated in the same place: %v", lastSystemLines(m, 3))
	}
	m = moveTo(t, m, pos.X+4000, pos.Y) // new region
	if got := lastSystemLines(m, 2); !strings.Contains(strings.Join(got, " "), "Vous entrez") || !strings.Contains(strings.Join(got, " "), "climat") {
		t.Fatalf("region change not announced: %v", got)
	}

	en := NewLang("ws://x/ws", "bob", i18n.EN)
	var tm tea.Model = en
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 140, Height: 36})
	tm, _ = tm.Update(connectedMsg{welcome: protocol.Welcome{ID: 1, Name: "bob", Seed: 1}})
	em := moveTo(t, tm.(Model), 5, 5)
	em = moveTo(t, em, pos.X, pos.Y)
	if got := strings.Join(lastSystemLines(em, 3), " "); !strings.Contains(got, "You arrive at the") {
		t.Fatalf("English announcement: %q", got)
	}
}

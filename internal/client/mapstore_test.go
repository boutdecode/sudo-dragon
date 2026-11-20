package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// By default, tests never write to the real configuration directory.
func init() { mapStorePath = func() string { return "" } }

func tempStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "map.json")
	old := mapStorePath
	mapStorePath = func() string { return path }
	t.Cleanup(func() { mapStorePath = old })
	return path
}

func TestMarkerNamingFlow(t *testing.T) {
	path := tempStore(t)
	m := mapModel(i18n.FR, 120, 40)
	m, _ = m.openMap()
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	// During input, w and + are text.
	m = typeText(m, "Mon w+camp")
	if !m.wmap.naming || m.wmap.z() != 8 {
		t.Fatalf("input must absorb the keys: %+v", m.wmap)
	}
	if !strings.Contains(ansi.Strip(m.helpLine()), "Mon w+camp") {
		t.Error("the bottom bar should show the entered text")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.wmap.naming || m.wmap.markers[0].Name != "Mon w+camp" {
		t.Fatalf("name = %+v", m.wmap.markers)
	}
	if _, err := os.Stat(path); err != nil {
		// Saving is an asynchronous command; we force it here.
		m.saveMapNow()
	}

	// Rename: w on the marker prefills the name; a duplicate (case) gets a suffix.
	m.wmap.markers = append(m.wmap.markers, marker{world.Pos{X: 500, Y: 500}, "Puits"})
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	if m.markName.Value() != "Mon w+camp" {
		t.Fatalf("rename not prefilled: %q", m.markName.Value())
	}
	m.markName.SetValue("puits")
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.wmap.markers[0].Name != "puits 2" {
		t.Errorf("duplicate: %q", m.wmap.markers[0].Name)
	}

	// Enter with an empty name keeps the current name.
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	m.markName.SetValue("  ")
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.wmap.markers[0].Name != "puits 2" {
		t.Errorf("empty name: %q", m.wmap.markers[0].Name)
	}
}

func TestDefaultMarkerNameIsFirstFree(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	m.wmap.markers = []marker{{world.Pos{}, "Repère 1"}, {world.Pos{X: 9}, "Repère 3"}}
	if got := m.defaultMarkerName(); got != "Repère 2" {
		t.Errorf("got %q", got)
	}
}

func TestFindMarker(t *testing.T) {
	s := mapState{markers: []marker{{Name: "Camp Nord"}, {Name: "Camp Sud"}, {Name: "Puits"}}}
	for _, c := range []struct {
		in   string
		n    int
		name string
	}{
		{"puits", 1, "Puits"}, {"PUI", 1, "Puits"}, {"camp nord", 1, "Camp Nord"},
		{"camp", 2, ""}, {"mine", 0, ""}, {"", 0, ""},
	} {
		mk, n := s.findMarker(c.in)
		if n != c.n || (n == 1 && mk.Name != c.name) {
			t.Errorf("%q: %v %d", c.in, mk.Name, n)
		}
	}
}

func TestParseGotoName(t *testing.T) {
	for _, c := range []struct {
		in   string
		name string
		ok   bool
	}{
		{"/goto Puits", "Puits", true},
		{"/GOTO mon  camp", "mon camp", true},
		{"/goto 3 -4", "", false},
		{"/goto 3", "3", true},
		{"/goto", "", false},
		{"/g puits", "", false},
	} {
		name, ok := parseGotoName(c.in)
		if name != c.name || ok != c.ok {
			t.Errorf("%q: %q %v", c.in, name, ok)
		}
	}
}

func TestGotoMarker(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	// A line of five walkable tiles: the player at the start, the marker at the end.
	var start world.Pos
search:
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			ok := true
			for k := 0; k < 5; k++ {
				ok = ok && m.world.At(x+k, y).Walkable()
				if _, npc := m.world.NPCAt(x+k, y); npc {
					ok = false
				}
			}
			if ok {
				start = world.Pos{X: x, Y: y}
				break search
			}
		}
	}
	target := world.Pos{X: start.X + 4, Y: start.Y}
	m.snap.Self.X, m.snap.Self.Y = start.X, start.Y
	m.snap.Others = nil
	m.wmap.markers = []marker{{target, "Puits"}, {world.Pos{X: 300, Y: 300}, "Pic"}}
	m2, _ := m.gotoName("pui")
	if !m2.walking || m2.walkTarget != target {
		t.Errorf("walk not started: %v %v", m2.walking, m2.walkTarget)
	}
	m3, _ := m.gotoName("nope")
	if m3.walking || len(m3.chat) == 0 || !strings.Contains(m3.chat[len(m3.chat)-1].Text, "nope") {
		t.Error("unknown marker: message expected")
	}
	m.wmap.markers[1].Name = "Puits 2"
	m4, _ := m.gotoName("pui")
	if m4.walking || len(m4.chat) == 0 {
		t.Error("ambiguous prefix: no walk")
	}
}

func TestMapStoreRoundTripKeepsOtherSeeds(t *testing.T) {
	path := tempStore(t)
	m := mapModel(i18n.FR, 120, 40) // graine 1
	m.wmap.markers = []marker{{world.Pos{X: 5, Y: -6}, "Camp"}}
	m.saveMapNow()

	other := mapModel(i18n.FR, 120, 40)
	other.world = world.New(2)
	other.wmap = mapState{explored: map[[2]int]struct{}{{7, 8}: {}}}
	other.saveMapNow()

	got := loadMapState(1)
	if len(got.markers) != 1 || got.markers[0].Name != "Camp" || got.markers[0].Pos != (world.Pos{X: 5, Y: -6}) {
		t.Errorf("markers: %+v", got.markers)
	}
	if len(got.explored) != len(m.wmap.explored) || len(got.explored) == 0 {
		t.Errorf("exploration: %d/%d", len(got.explored), len(m.wmap.explored))
	}
	if g2 := loadMapState(2); len(g2.explored) != 1 {
		t.Errorf("seed 2 was overwritten: %+v", g2.explored)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file: %v %v", info, err)
	}
}

func TestMapStoreMissingOrCorrupt(t *testing.T) {
	path := tempStore(t)
	if st := loadMapState(1); len(st.markers) != 0 || st.explored == nil {
		t.Error("missing file: non-nil empty state expected")
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{pas du json"), 0o600)
	if st := loadMapState(1); len(st.markers) != 0 {
		t.Error("corrupt file: empty state expected")
	}
	m := mapModel(i18n.FR, 120, 40)
	m.saveMapNow() // must rewrite a valid file over it
	if st := loadMapState(1); len(st.explored) == 0 {
		t.Error("saving should replace the corrupt file")
	}
}

func TestExploreSaveIsThrottled(t *testing.T) {
	tempStore(t)
	m := mapModel(i18n.FR, 120, 40)
	if !m.wmap.dirty {
		t.Fatal("exploration should be marked modified")
	}
	if cmd := m.maybeSaveExplored(); cmd == nil {
		t.Fatal("first save expected")
	}
	m.snap.Self.X += 200
	m.markExplored()
	if cmd := m.maybeSaveExplored(); cmd != nil {
		t.Error("no new write before exploreSaveEvery")
	}
	m.wmap.lastSave = m.wmap.lastSave.Add(-2 * exploreSaveEvery)
	if cmd := m.maybeSaveExplored(); cmd == nil {
		t.Error("writing should resume after the delay")
	}
}

func TestNamingBarFitsEverySize(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		for _, sz := range [][2]int{{80, 24}, {110, 34}, {200, 60}} {
			m := mapModel(lang, sz[0], sz[1])
			m, _ = m.openMap()
			m = pressKey(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
			m = typeText(m, "un nom de repère assez long pour dépasser")
			for _, l := range strings.Split(m.View().Content, "\n") {
				if ansi.StringWidth(l) > sz[0] {
					t.Fatalf("%s %v: line too wide (%d)", lang, sz, ansi.StringWidth(l))
				}
			}
		}
	}
}

// placeModel puts the player near a place of seed 1; explored says whether the place is discovered.
func placeModel(t *testing.T, explored bool) (Model, *world.Structure) {
	t.Helper()
	m := mapModel(i18n.FR, 120, 40)
	ss := m.world.StructuresNear(world.Pos{}, 800)
	if len(ss) == 0 {
		t.Skip("no nearby place")
	}
	s := ss[0]
	c := s.Center()
	m.snap.Others, m.snap.Monsters = nil, nil
	m.wmap.explored = map[[2]int]struct{}{}
	m.snap.Self.X, m.snap.Self.Y = c.X, c.Y
	m.snap.Self = entityAt(m.walkableNear(world.Pos{X: c.X + 3, Y: c.Y}, 12), m.snap.Self)
	if explored {
		m.markExplored()
	}
	m.snap.Self.X, m.snap.Self.Y = c.X+s.W/2+2, c.Y
	m.snap.Self = entityAt(m.walkableNear(world.Pos{X: c.X + s.W/2 + 2, Y: c.Y}, 12), m.snap.Self)
	return m, s
}

func entityAt(p world.Pos, e protocol.Entity) protocol.Entity {
	e.X, e.Y = p.X, p.Y
	return e
}

func TestGotoDiscoveredPlace(t *testing.T) {
	m, s := placeModel(t, true)
	name := s.NameIn(m.lang)
	got, n := m.findTarget(strings.ToUpper(name))
	if n != 1 || !got.place || got.Pos != s.Center() {
		t.Fatalf("findTarget(%q) = %+v %d", name, got, n)
	}
	if _, n := m.findTarget(m.placeLabel(s)); n != 1 {
		t.Errorf("the full label %q should be recognized", m.placeLabel(s))
	}
	if _, n := m.findTarget(name[:3]); n < 1 {
		t.Errorf("prefix %q not recognized", name[:3])
	}
	m2, _ := m.gotoName(name)
	if !m2.walking {
		t.Fatalf("walk not started: %v", m2.chat)
	}
	if near := m.walkableNear(s.Center(), 8); dist2(near, s.Center()) > 8*8*2 || m2.walkTarget != near {
		t.Errorf("target %v, expected a free tile near the center %v", m2.walkTarget, near)
	}
}

func TestGotoUndiscoveredPlaceIsUnknown(t *testing.T) {
	m, s := placeModel(t, false)
	if _, n := m.findTarget(s.NameIn(m.lang)); n != 0 {
		t.Fatalf("an undiscovered place must not be found (n=%d)", n)
	}
	m2, _ := m.gotoName(s.NameIn(m.lang))
	if m2.walking || len(m2.chat) == 0 {
		t.Error("\"unknown\" message expected")
	}
}

func TestMarkerBeatsPlaceWithSameName(t *testing.T) {
	m, s := placeModel(t, true)
	m.wmap.markers = []marker{{world.Pos{X: 1, Y: 1}, s.NameIn(m.lang)}}
	got, n := m.findTarget(s.NameIn(m.lang))
	if n != 1 || got.place || got.Pos != (world.Pos{X: 1, Y: 1}) {
		t.Errorf("the marker should win: %+v", got)
	}
}

func TestWalkableNearPrefersRoadAndSkipsWalls(t *testing.T) {
	m, s := placeModel(t, true)
	p := m.walkableNear(s.Center(), 8)
	if !m.freeFn()(p) {
		t.Errorf("%v should be a free tile", p)
	}
}

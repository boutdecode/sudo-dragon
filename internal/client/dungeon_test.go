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

func sendEvent(t *testing.T, m Model, typ string, payload any) Model {
	t.Helper()
	data, err := protocol.Encode(typ, payload)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := protocol.Decode(data)
	tm, _ := m.Update(eventMsg{Event{Env: env}})
	return tm.(Model)
}

// intoDungeon moves the model into a dungeon, placed on its arrival point.
func intoDungeon(t *testing.T) (Model, *world.DungeonMap) {
	t.Helper()
	d := &world.Dungeon{Seed: 987654321, Tier: 3}
	dm := world.NewDungeonMap(d)
	m := playingModel(t, 140, 40)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: d.Seed, Tier: d.Tier})
	m = sendEvent(t, m, protocol.TSnapshot, protocol.Snapshot{Tick: 9, Self: protocol.Entity{ID: 1, Name: "bob", X: dm.Spawn.X, Y: dm.Spawn.Y}})
	return m, dm
}

func TestEnteringADungeonSwitchesTheMap(t *testing.T) {
	m, dm := intoDungeon(t)
	if !m.inDungeon() || m.dmap.Exit != dm.Exit {
		t.Fatal("dungeon layout was not rebuilt")
	}
	if got := lastSystemLines(m, 1); len(got) == 0 || !strings.Contains(got[0], "Crypte de Test") {
		t.Errorf("entrance announcement: %v", got)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Crypte de Test") || !strings.Contains(view, "@") {
		t.Errorf("view shows neither the dungeon nor the player:\n%s", view)
	}
	// The layout is in the fog: chests far from the arrival are not visible yet.
	far := 0
	for _, c := range dm.Chests {
		if dist2(c, dm.Spawn) > dungeonSight*dungeonSight && m.terrain().At(c.X, c.Y) == world.Chest {
			far++
		}
	}
	if far != 0 {
		t.Errorf("%d far chests already visible", far)
	}
}

func TestDungeonSightRevealsAsYouWalk(t *testing.T) {
	m, dm := intoDungeon(t)
	if m.terrain().At(dm.Spawn.X+1, dm.Spawn.Y) != dm.TileAt(dm.Spawn.X+1, dm.Spawn.Y) {
		t.Error("the neighboring tile should be visible")
	}
	boss := dm.Boss
	if m.dseen[boss] {
		t.Skip("the boss is within sight of the arrival")
	}
	m = sendEvent(t, m, protocol.TSnapshot, protocol.Snapshot{Tick: 10, Self: protocol.Entity{ID: 1, X: boss.X, Y: boss.Y}})
	if !m.dseen[boss] || m.terrain().At(boss.X, boss.Y) != world.Floor {
		t.Error("the boss room should be seen once on site")
	}
}

func TestOpenedChestsAreShownEmpty(t *testing.T) {
	m, dm := intoDungeon(t)
	c := dm.Chests[0]
	m.dseen[c] = true
	if m.terrain().At(c.X, c.Y) != world.Chest {
		t.Fatal("the chest should be visible")
	}
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 987654321, Tier: 3, Opened: [][2]int{{c.X, c.Y}}})
	if !m.inDungeon() || m.terrain().At(c.X, c.Y) != world.Floor {
		t.Error("the opened chest should appear empty")
	}
	if m.hasSnap == false {
		t.Error("a simple zone resend must not wait for a new snapshot")
	}
}

func TestLeavingTheDungeonRestoresTheWorld(t *testing.T) {
	m, _ := intoDungeon(t)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{})
	if m.inDungeon() || m.dseen != nil {
		t.Fatal("the dungeon is still there")
	}
	if m.hasSnap {
		t.Error("the main map's snapshot is awaited")
	}
	m = moveTo(t, m, 10, 10)
	if m.terrain() != any(m.world) {
		t.Error("the terrain should be the world")
	}
	if got := lastSystemLines(m, 1); len(got) == 0 || !strings.Contains(got[len(got)-1], "surface") {
		t.Errorf("exit announcement: %v", got)
	}
}

func TestMapKeyShowsTheDungeonPlan(t *testing.T) {
	m, _ := intoDungeon(t)
	m, _ = m.openMap()
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Plan du donjon") || !strings.Contains(view, "palier 3") {
		t.Errorf("layout missing:\n%s", view)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if tm.(Model).worldMap {
		t.Error("m should close the layout")
	}
}

func TestGotoInsideADungeon(t *testing.T) {
	m, dm := intoDungeon(t)
	// The stairs are found by name, not the markers or the surface places.
	tg, n := m.findTarget("escal")
	if n != 1 || tg.Pos != dm.Exit {
		t.Fatalf("target %+v (%d), expected the stairs %v", tg, n, dm.Exit)
	}
	if _, n := m.findTarget("village"); n != 0 {
		t.Error("a surface place must not be found underground")
	}
	// A path to a tile of the layout exists, and does not go through the stairs.
	var goal world.Pos
	for p := range dm.Reachable(dm.Spawn) {
		if dist2(p, dm.Spawn) > dist2(goal, dm.Spawn) && dm.TileAt(p.X, p.Y) == world.Floor {
			goal = p
		}
	}
	m.hasSnap = true
	free := m.freeFnTo(goal)
	if free(dm.Exit) {
		t.Error("the stairs must not be crossed by /goto")
	}
	path := m.findWalkPath(m.selfPos(), goal, free)
	if path == nil {
		t.Fatalf("no path from %v to %v", m.selfPos(), goal)
	}
	for _, p := range path {
		if dm.TileAt(p.X, p.Y) == world.Stairs {
			t.Fatal("the path crosses the stairs")
		}
	}
	// But it can be targeted.
	if !m.freeFnTo(dm.Exit)(dm.Exit) {
		t.Error("the stairs must remain a possible destination")
	}
}

func TestGotoNeverCrossesADungeonEntrance(t *testing.T) {
	w := world.New(1)
	var d *world.Dungeon
	for _, c := range w.DungeonsNear(0, 0, 4) {
		d = c
		break
	}
	if d == nil {
		t.Skip("no dungeon")
	}
	m := playingModel(t, 140, 36)
	free := m.freeFnTo(world.Pos{})
	if free(d.Entrance) {
		t.Error("a dungeon entrance must not be crossed")
	}
	if !m.freeFnTo(d.Entrance)(d.Entrance) {
		t.Error("but it remains a destination")
	}
}

func TestMonstersBehindRockStayHidden(t *testing.T) {
	m, dm := intoDungeon(t)
	var far world.Pos
	for _, s := range dm.Slots {
		if dist2(s.Pos, dm.Spawn) > 4*dungeonSight*dungeonSight {
			far = s.Pos
			break
		}
	}
	if far == (world.Pos{}) {
		t.Skip("all monsters are near the arrival")
	}
	near := world.Pos{X: dm.Spawn.X + 1, Y: dm.Spawn.Y}
	m = sendEvent(t, m, protocol.TSnapshot, protocol.Snapshot{
		Tick:     11,
		Self:     protocol.Entity{ID: 1, Name: "bob", X: dm.Spawn.X, Y: dm.Spawn.Y},
		Monsters: []protocol.Monster{{ID: 7, Kind: "skeleton", X: far.X, Y: far.Y}, {ID: 8, Kind: "wraith", X: near.X, Y: near.Y}},
		Others:   []protocol.Entity{{ID: 2, Name: "eve", X: far.X, Y: far.Y}},
	})
	if len(m.snap.Monsters) != 1 || m.snap.Monsters[0].ID != 8 || len(m.snap.Others) != 0 {
		t.Errorf("monsters %v, players %v: only the near one should remain", m.snap.Monsters, m.snap.Others)
	}
}

// discovered gives a model on the main map that knows a world dungeon's entrance.
func discovered(t *testing.T) (Model, protocol.DungeonInfo) {
	t.Helper()
	w := world.New(1)
	var d *world.Dungeon
	for _, c := range w.DungeonsNear(0, 0, 4) {
		d = c
		break
	}
	if d == nil {
		t.Fatal("no dungeon near the origin")
	}
	info := protocol.DungeonInfo{ID: d.ID, Name: d.Name, X: d.Entrance.X, Y: d.Entrance.Y, Tier: d.Tier}
	m := playingModel(t, 140, 40)
	m = sendEvent(t, m, protocol.TDungeons, protocol.Dungeons{List: []protocol.DungeonInfo{info}, New: d.ID})
	m = moveTo(t, m, info.X-3, info.Y)
	return m, info
}

func TestDiscoveringAnEntranceIsAnnouncedAndDrawn(t *testing.T) {
	m, info := discovered(t)
	if got := strings.Join(lastSystemLines(m, 5), "\n"); !strings.Contains(got, info.Name) {
		t.Errorf("announcement missing: %v", got)
	}
	if m.terrain().At(info.X, info.Y) != world.Entrance {
		t.Fatal("the discovered entrance should be drawn")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, ">") {
		t.Error("the entrance glyph is missing from the view")
	}
	// A list sent again with nothing new does not replay the announcement.
	before := len(m.chat)
	m = sendEvent(t, m, protocol.TDungeons, protocol.Dungeons{List: []protocol.DungeonInfo{info}})
	if len(m.chat) != before {
		t.Error("announcement replayed")
	}
	// An undiscovered entrance remains an ordinary tile.
	w := world.New(1)
	for _, d := range w.DungeonsNear(0, 0, 3) {
		if d.ID != info.ID && m.terrain().At(d.Entrance.X, d.Entrance.Y) == world.Entrance {
			t.Fatalf("%s is not discovered but is displayed", d.ID)
		}
	}
}

func TestDiscoveredDungeonShowsOnTheWorldMapAndLegend(t *testing.T) {
	m, info := discovered(t)
	m, _ = m.openMap()
	if lm := m.landmarks(); len(lm) == 0 || lm[0].name != info.Name || lm[0].glyph != ">" {
		t.Fatalf("landmarks: %+v", lm)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Donjon") || !strings.Contains(view, info.Name) {
		t.Errorf("legend or list without the dungeon:\n%s", view)
	}
}

func TestGotoADiscoveredDungeonStopsInFrontOfIt(t *testing.T) {
	m, info := discovered(t)
	prefix := strings.ToLower(strings.Fields(info.Name)[0])
	tg, n := m.findTarget(prefix)
	if n != 1 || tg.Pos != (world.Pos{X: info.X, Y: info.Y}) || !tg.place {
		t.Fatalf("cible %+v (%d)", tg, n)
	}
	stop := m.walkableNear(tg.Pos, 8)
	if stop == tg.Pos || chebyshev(stop, tg.Pos) != 1 {
		t.Errorf("stop at %v: should be next to the entrance %v, not on it", stop, tg.Pos)
	}
	// An unknown dungeon is not found by name.
	m.dungeons = nil
	if _, n := m.findTarget(prefix); n != 0 {
		t.Error("an undiscovered dungeon must not be targetable")
	}
}

func TestAskForWorkHasItsOwnKey(t *testing.T) {
	m := playingModel(t, 140, 40)
	for _, lang := range []string{"fr", "en"} {
		l := i18n.FR
		if lang == "en" {
			l = i18n.EN
		}
		m.lang = l
		keys := m.npcKeys()
		work := strings.ToLower(m.t("npc.key.work"))
		if keys[work] != protocol.NPCAskWork {
			t.Errorf("%s: key %q does not request work", lang, work)
		}
		if m.npcKeyLabel(protocol.NPCAskWork) != m.t("npc.key.work") {
			t.Errorf("%s: inconsistent offer label", lang)
		}
		// No other action shares this key.
		seen := map[string]string{}
		for k, a := range keys {
			if prev, dup := seen[k]; dup {
				t.Errorf("%s: key %q for %s and %s", lang, k, prev, a)
			}
			seen[k] = a
		}
		if len(keys) != 7 {
			t.Errorf("%s: %d keys, expected 7 (one per action without argument)", lang, len(keys))
		}
	}
}

func TestEnteringACleanedDungeonTellsWhenItComesBack(t *testing.T) {
	m := playingModel(t, 140, 40)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1, ResetIn: 17})
	if got := strings.Join(lastSystemLines(m, 3), "\n"); !strings.Contains(got, "17 h de jeu") {
		t.Errorf("delay missing from announcements: %q", got)
	}
	// An intact dungeon says nothing about it, and a zone resend (chest opened) does not repeat the announcement.
	fresh := playingModel(t, 140, 40)
	fresh = sendEvent(t, fresh, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1})
	if got := strings.Join(lastSystemLines(fresh, 3), "\n"); strings.Contains(got, "h de jeu") {
		t.Errorf("delay announcement in an intact dungeon: %q", got)
	}
	n := len(m.chat)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1, ResetIn: 16, Opened: [][2]int{{1, 1}}})
	if len(m.chat) != n {
		t.Error("the announcement is replayed on every zone resend")
	}
}

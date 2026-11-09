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

// intoDungeon fait entrer le modèle dans un donjon, posé sur son point d'arrivée.
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
		t.Fatal("le plan du donjon n'a pas été reconstruit")
	}
	if got := lastSystemLines(m, 1); len(got) == 0 || !strings.Contains(got[0], "Crypte de Test") {
		t.Errorf("annonce d'entrée: %v", got)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Crypte de Test") || !strings.Contains(view, "@") {
		t.Errorf("la vue ne montre ni le donjon ni le joueur:\n%s", view)
	}
	// Le plan est dans le brouillard: les coffres loin de l'arrivée ne se voient pas encore.
	far := 0
	for _, c := range dm.Chests {
		if dist2(c, dm.Spawn) > dungeonSight*dungeonSight && m.terrain().At(c.X, c.Y) == world.Chest {
			far++
		}
	}
	if far != 0 {
		t.Errorf("%d coffres lointains déjà visibles", far)
	}
}

func TestDungeonSightRevealsAsYouWalk(t *testing.T) {
	m, dm := intoDungeon(t)
	if m.terrain().At(dm.Spawn.X+1, dm.Spawn.Y) != dm.TileAt(dm.Spawn.X+1, dm.Spawn.Y) {
		t.Error("la case voisine devrait être visible")
	}
	boss := dm.Boss
	if m.dseen[boss] {
		t.Skip("le boss est dans le rayon de vue de l'arrivée")
	}
	m = sendEvent(t, m, protocol.TSnapshot, protocol.Snapshot{Tick: 10, Self: protocol.Entity{ID: 1, X: boss.X, Y: boss.Y}})
	if !m.dseen[boss] || m.terrain().At(boss.X, boss.Y) != world.Floor {
		t.Error("la salle du boss devrait être vue une fois sur place")
	}
}

func TestOpenedChestsAreShownEmpty(t *testing.T) {
	m, dm := intoDungeon(t)
	c := dm.Chests[0]
	m.dseen[c] = true
	if m.terrain().At(c.X, c.Y) != world.Chest {
		t.Fatal("le coffre devrait être visible")
	}
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 987654321, Tier: 3, Opened: [][2]int{{c.X, c.Y}}})
	if !m.inDungeon() || m.terrain().At(c.X, c.Y) != world.Floor {
		t.Error("le coffre ouvert devrait apparaître vide")
	}
	if m.hasSnap == false {
		t.Error("un simple renvoi de zone ne doit pas faire attendre un nouvel instantané")
	}
}

func TestLeavingTheDungeonRestoresTheWorld(t *testing.T) {
	m, _ := intoDungeon(t)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{})
	if m.inDungeon() || m.dseen != nil {
		t.Fatal("le donjon est resté")
	}
	if m.hasSnap {
		t.Error("on attend l'instantané de la carte principale")
	}
	m = moveTo(t, m, 10, 10)
	if m.terrain() != any(m.world) {
		t.Error("le terrain devrait être le monde")
	}
	if got := lastSystemLines(m, 1); len(got) == 0 || !strings.Contains(got[len(got)-1], "surface") {
		t.Errorf("annonce de sortie: %v", got)
	}
}

func TestMapKeyShowsTheDungeonPlan(t *testing.T) {
	m, _ := intoDungeon(t)
	m, _ = m.openMap()
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Plan du donjon") || !strings.Contains(view, "palier 3") {
		t.Errorf("plan absent:\n%s", view)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if tm.(Model).worldMap {
		t.Error("m devrait refermer le plan")
	}
}

func TestMovetoInsideADungeon(t *testing.T) {
	m, dm := intoDungeon(t)
	// L'escalier se trouve par son nom, pas les repères ni les lieux de la surface.
	tg, n := m.findTarget("escal")
	if n != 1 || tg.Pos != dm.Exit {
		t.Fatalf("cible %+v (%d), attendu l'escalier %v", tg, n, dm.Exit)
	}
	if _, n := m.findTarget("village"); n != 0 {
		t.Error("un lieu de la surface ne doit pas se trouver sous terre")
	}
	// Un chemin vers une case du plan existe, et ne passe pas par l'escalier.
	var goal world.Pos
	for p := range dm.Reachable(dm.Spawn) {
		if dist2(p, dm.Spawn) > dist2(goal, dm.Spawn) && dm.TileAt(p.X, p.Y) == world.Floor {
			goal = p
		}
	}
	m.hasSnap = true
	free := m.freeFnTo(goal)
	if free(dm.Exit) {
		t.Error("l'escalier ne doit pas être traversé par /moveto")
	}
	path := m.findWalkPath(m.selfPos(), goal, free)
	if path == nil {
		t.Fatalf("aucun chemin de %v à %v", m.selfPos(), goal)
	}
	for _, p := range path {
		if dm.TileAt(p.X, p.Y) == world.Stairs {
			t.Fatal("le chemin traverse l'escalier")
		}
	}
	// Mais on peut le viser.
	if !m.freeFnTo(dm.Exit)(dm.Exit) {
		t.Error("l'escalier doit rester une destination possible")
	}
}

func TestMovetoNeverCrossesADungeonEntrance(t *testing.T) {
	w := world.New(1)
	var d *world.Dungeon
	for _, c := range w.DungeonsNear(0, 0, 4) {
		d = c
		break
	}
	if d == nil {
		t.Skip("pas de donjon")
	}
	m := playingModel(t, 140, 36)
	free := m.freeFnTo(world.Pos{})
	if free(d.Entrance) {
		t.Error("l'entrée d'un donjon ne doit pas être traversée")
	}
	if !m.freeFnTo(d.Entrance)(d.Entrance) {
		t.Error("mais elle reste une destination")
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
		t.Skip("tous les monstres sont près de l'arrivée")
	}
	near := world.Pos{X: dm.Spawn.X + 1, Y: dm.Spawn.Y}
	m = sendEvent(t, m, protocol.TSnapshot, protocol.Snapshot{
		Tick:     11,
		Self:     protocol.Entity{ID: 1, Name: "bob", X: dm.Spawn.X, Y: dm.Spawn.Y},
		Monsters: []protocol.Monster{{ID: 7, Kind: "skeleton", X: far.X, Y: far.Y}, {ID: 8, Kind: "wraith", X: near.X, Y: near.Y}},
		Others:   []protocol.Entity{{ID: 2, Name: "eve", X: far.X, Y: far.Y}},
	})
	if len(m.snap.Monsters) != 1 || m.snap.Monsters[0].ID != 8 || len(m.snap.Others) != 0 {
		t.Errorf("monstres %v, joueurs %v: seul le proche devrait rester", m.snap.Monsters, m.snap.Others)
	}
}

// discovered donne un modèle sur la carte principale qui connaît l'entrée d'un donjon du monde.
func discovered(t *testing.T) (Model, protocol.DungeonInfo) {
	t.Helper()
	w := world.New(1)
	var d *world.Dungeon
	for _, c := range w.DungeonsNear(0, 0, 4) {
		d = c
		break
	}
	if d == nil {
		t.Fatal("aucun donjon près de l'origine")
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
		t.Errorf("annonce absente: %v", got)
	}
	if m.terrain().At(info.X, info.Y) != world.Entrance {
		t.Fatal("l'entrée découverte devrait être dessinée")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, ">") {
		t.Error("le glyphe de l'entrée est absent de la vue")
	}
	// Une liste renvoyée sans nouveauté ne rejoue pas l'annonce.
	before := len(m.chat)
	m = sendEvent(t, m, protocol.TDungeons, protocol.Dungeons{List: []protocol.DungeonInfo{info}})
	if len(m.chat) != before {
		t.Error("annonce rejouée")
	}
	// Une entrée non découverte reste une case ordinaire.
	w := world.New(1)
	for _, d := range w.DungeonsNear(0, 0, 3) {
		if d.ID != info.ID && m.terrain().At(d.Entrance.X, d.Entrance.Y) == world.Entrance {
			t.Fatalf("%s n'est pas découvert mais s'affiche", d.ID)
		}
	}
}

func TestDiscoveredDungeonShowsOnTheWorldMapAndLegend(t *testing.T) {
	m, info := discovered(t)
	m, _ = m.openMap()
	if lm := m.landmarks(); len(lm) == 0 || lm[0].name != info.Name || lm[0].glyph != ">" {
		t.Fatalf("repères: %+v", lm)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Donjon") || !strings.Contains(view, info.Name) {
		t.Errorf("légende ou liste sans le donjon:\n%s", view)
	}
}

func TestMovetoADiscoveredDungeonStopsInFrontOfIt(t *testing.T) {
	m, info := discovered(t)
	prefix := strings.ToLower(strings.Fields(info.Name)[0])
	tg, n := m.findTarget(prefix)
	if n != 1 || tg.Pos != (world.Pos{X: info.X, Y: info.Y}) || !tg.place {
		t.Fatalf("cible %+v (%d)", tg, n)
	}
	stop := m.walkableNear(tg.Pos, 8)
	if stop == tg.Pos || chebyshev(stop, tg.Pos) != 1 {
		t.Errorf("arrêt en %v: devrait être à côté de l'entrée %v, pas dessus", stop, tg.Pos)
	}
	// Un donjon inconnu ne se trouve pas par son nom.
	m.dungeons = nil
	if _, n := m.findTarget(prefix); n != 0 {
		t.Error("un donjon non découvert ne doit pas être ciblable")
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
			t.Errorf("%s: la touche %q ne demande pas de travail", lang, work)
		}
		if m.npcKeyLabel(protocol.NPCAskWork) != m.t("npc.key.work") {
			t.Errorf("%s: étiquette de l'offre incohérente", lang)
		}
		// Aucune autre action ne partage cette touche.
		seen := map[string]string{}
		for k, a := range keys {
			if prev, dup := seen[k]; dup {
				t.Errorf("%s: touche %q pour %s et %s", lang, k, prev, a)
			}
			seen[k] = a
		}
		if len(keys) != 5 {
			t.Errorf("%s: %d touches, attendu 5 (une par action sans argument)", lang, len(keys))
		}
	}
}

func TestEnteringACleanedDungeonTellsWhenItComesBack(t *testing.T) {
	m := playingModel(t, 140, 40)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1, ResetIn: 17})
	if got := strings.Join(lastSystemLines(m, 3), "\n"); !strings.Contains(got, "17 h de jeu") {
		t.Errorf("délai absent des annonces: %q", got)
	}
	// Un donjon intact n'en dit rien, et un renvoi de zone (coffre ouvert) ne répète pas l'annonce.
	fresh := playingModel(t, 140, 40)
	fresh = sendEvent(t, fresh, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1})
	if got := strings.Join(lastSystemLines(fresh, 3), "\n"); strings.Contains(got, "h de jeu") {
		t.Errorf("annonce de délai dans un donjon intact: %q", got)
	}
	n := len(m.chat)
	m = sendEvent(t, m, protocol.TZone, protocol.Zone{Dungeon: "1,1", Name: "Crypte de Test", Seed: 5, Tier: 1, ResetIn: 16, Opened: [][2]int{{1, 1}}})
	if len(m.chat) != n {
		t.Error("l'annonce est rejouée à chaque renvoi de la zone")
	}
}

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

func mapModel(lang i18n.Lang, w, h int) Model {
	m := sheetModel(lang, w, h)
	m.snap.Others = []protocol.Entity{{ID: 2, Name: "bob", X: 14, Y: -3}}
	m.markExplored()
	return m
}

func pressKey(t *testing.T, m Model, k tea.KeyPressMsg) Model {
	t.Helper()
	tm, _ := m.Update(k)
	return tm.(Model)
}

func TestMapGridShowsSelfExploredAndUnexplored(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	m, _ = m.openMap()
	m.wmap.zoom = 1
	lines, explored, total := mapGrid(m.world, &m.wmap, m.snap.Self, m.snap.Others, 101, 41)
	if len(lines) != 41 {
		t.Fatalf("lignes = %d", len(lines))
	}
	out := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(out, "@") || !strings.Contains(out, "░") {
		t.Error("la carte devrait montrer le joueur et de l'inexploré")
	}
	if strings.Count(out, "@") != 2 {
		t.Errorf("attendu 2 « @ » (joueur + autre), obtenu %d", strings.Count(out, "@"))
	}
	if explored == 0 || explored >= total {
		t.Errorf("exploré = %d/%d", explored, total)
	}
	// Au centre de la grille, la cellule est celle du joueur.
	if r := []rune(ansi.Strip(lines[20])); r[50] != '@' {
		t.Errorf("le joueur devrait être au centre, trouvé %q", string(r[50]))
	}
}

func TestMapStructuresAppearWhenExplored(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	w := m.world
	var s *world.Structure
	for _, c := range w.StructuresNear(world.Pos{}, 800) {
		s = c
		break
	}
	if s == nil {
		t.Skip("aucune structure proche")
	}
	ctr := s.Center()
	m.snap.Self.X, m.snap.Self.Y = ctr.X+3, ctr.Y
	m.markExplored()
	m.wmap.curX, m.wmap.curY, m.wmap.zoom = ctr.X, ctr.Y, 1
	lines, _, _ := mapGrid(w, &m.wmap, m.snap.Self, nil, 41, 21)
	out := ansi.Strip(strings.Join(lines, "\n"))
	want := "H"
	if s.Kind == world.KindFortress {
		want = "X"
	}
	if !strings.Contains(out, want) {
		t.Errorf("structure %v sans %q", s.Kind, want)
	}
}

func TestMapKeysZoomCursorMarkers(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !m.worldMap || m.wmap.curX != 12 || m.wmap.z() != 8 {
		t.Fatalf("ouverture: %+v", m.wmap)
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: '+', Text: "+"})
	if m.wmap.z() != 4 {
		t.Errorf("zoom = %d", m.wmap.z())
	}
	for i := 0; i < 10; i++ {
		m = pressKey(t, m, tea.KeyPressMsg{Code: '-', Text: "-"})
	}
	if m.wmap.z() != 16 {
		t.Errorf("zoom borné à 16, obtenu %d", m.wmap.z())
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.wmap.curX != 12+16 {
		t.Errorf("curseur x = %d", m.wmap.curX)
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	if len(m.wmap.markers) != 1 || !m.wmap.naming || m.wmap.markers[0].Name != "Repère 1" {
		t.Fatalf("w devrait poser un repère et ouvrir la saisie: %+v", m.wmap)
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.wmap.naming || !m.worldMap || len(m.wmap.markers) != 1 {
		t.Fatal("Échap annule la saisie sans fermer la carte ni perdre le repère")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if len(m.wmap.markers) != 0 {
		t.Fatal("x devrait retirer le repère de la cellule")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: '0', Text: "0"})
	if m.wmap.curX != 12 {
		t.Error("0 devrait recentrer")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.worldMap {
		t.Error("Échap devrait fermer la carte")
	}
}

func TestMapViewFitsEverySizeAndLanguage(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.FR, i18n.EN} {
		for _, sz := range [][2]int{{80, 24}, {110, 34}, {200, 60}} {
			m := mapModel(lang, sz[0], sz[1])
			m, _ = m.openMap()
			m.wmap.markers = []marker{{world.Pos{X: 20, Y: 5}, "Camp"}}
			v := m.View().Content
			lines := strings.Split(v, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s %v: %d lignes > %d", lang, sz, len(lines), sz[1])
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > sz[0] {
					t.Fatalf("%s %v: ligne trop large (%d)", lang, sz, ansi.StringWidth(l))
				}
			}
			plain := ansi.Strip(v)
			for _, want := range []string{m.t("map.title"), m.t("map.zoom", 8), seedLabel(1)} {
				if !strings.Contains(plain, want) {
					t.Errorf("%s %v: carte sans %q", lang, sz, want)
				}
			}
			if sz[0] >= legendMinWidth && !strings.Contains(plain, m.t("map.legend")) {
				t.Errorf("%s %v: légende absente", lang, sz)
			}
		}
	}
}

func TestArrowAndSeedLabel(t *testing.T) {
	for _, c := range []struct {
		dx, dy int
		want   string
	}{{5, 0, "→"}, {5, 5, "↘"}, {0, 5, "↓"}, {-5, 5, "↙"}, {-5, 0, "←"}, {-5, -5, "↖"}, {0, -5, "↑"}, {5, -5, "↗"}} {
		if got := arrowTo(c.dx, c.dy); got != c.want {
			t.Errorf("arrowTo(%d,%d) = %s, attendu %s", c.dx, c.dy, got, c.want)
		}
	}
	if got := seedLabel(0x4F2A91C0); got != "4F2A-91C0" {
		t.Errorf("seedLabel = %s", got)
	}
}

func TestMapCursorBlinks(t *testing.T) {
	m := mapModel(i18n.FR, 120, 40)
	m = pressKey(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	gen, was := m.wmap.gen, m.wmap.blink
	tm, cmd := m.Update(mapBlinkMsg{gen})
	m = tm.(Model)
	if m.wmap.blink == was || cmd == nil {
		t.Fatal("le curseur devrait basculer et relancer le minuteur")
	}
	m = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, cmd = m.Update(mapBlinkMsg{gen})
	if cmd != nil || tm.(Model).wmap.blink != m.wmap.blink {
		t.Error("carte fermée : le clignotement doit s'arrêter")
	}
}

// Les couleurs de la carte suivent le climat de la contrée, sans changer les glyphes.
func TestMapStylesFollowClimate(t *testing.T) {
	if len(mapStyles) != int(world.NumClimates)*numMapStyles {
		t.Fatalf("%d styles, attendu %d", len(mapStyles), int(world.NumClimates)*numMapStyles)
	}
	temperate := mapStyles[msForest+climatShift(world.Temperate)].Render("^")
	for c := world.Climate(1); c < world.NumClimates; c++ {
		if got := mapStyles[msForest+climatShift(c)].Render("^"); got == temperate {
			t.Errorf("la forêt du climat %s a la couleur du tempéré", c.Key())
		}
		if mapStyles[msVillage+climatShift(c)].Render("H") != mapStyles[msVillage].Render("H") {
			t.Errorf("les villages ne doivent pas changer de couleur (%s)", c.Key())
		}
	}
	for c := world.Climate(0); c < world.NumClimates; c++ {
		if i18n.T(i18n.FR, "climate."+c.Key()) == "climate."+c.Key() || i18n.T(i18n.EN, "climate."+c.Key()) == "climate."+c.Key() {
			t.Errorf("climat %s sans traduction", c.Key())
		}
	}
}

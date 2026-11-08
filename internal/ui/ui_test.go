package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPanelExactSize(t *testing.T) {
	for _, c := range []struct{ w, h int }{{4, 2}, {10, 3}, {30, 6}, {80, 24}} {
		out := Panel("Titre très long qui déborde", "a\nb\nune ligne beaucoup trop longue pour tenir\nd\ne\nf\ng", c.w, c.h)
		lines := strings.Split(out, "\n")
		if len(lines) != c.h {
			t.Errorf("%dx%d: %d lignes", c.w, c.h, len(lines))
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != c.w {
				t.Errorf("%dx%d ligne %d: largeur %d", c.w, c.h, i, got)
			}
		}
	}
	if Panel("x", "y", 3, 5) != "" || Panel("x", "y", 10, 1) != "" {
		t.Error("panneau trop petit: chaîne vide attendue")
	}
}

func TestPanelShowsTitle(t *testing.T) {
	out := ansi.Strip(Panel("Joueurs", "", 20, 3))
	if !strings.HasPrefix(out, "╭─ Joueurs ") {
		t.Errorf("titre absent: %q", out)
	}
}

func TestKeyBar(t *testing.T) {
	out := ansi.Strip(KeyBar([2]string{"e", "interagir"}, [2]string{"esc", "retour"}))
	if !strings.Contains(out, "interagir") || !strings.Contains(out, "esc") {
		t.Errorf("barre incomplète: %q", out)
	}
}

func TestBarWidthAndFill(t *testing.T) {
	cases := []struct{ cur, max, width, filled int }{
		{10, 10, 10, 10}, {5, 10, 10, 5}, {0, 10, 10, 0},
		{1, 100, 10, 1},  // jamais vide tant qu'il reste des PV
		{99, 100, 10, 9}, // jamais plein tant qu'il en manque
		{-5, 10, 10, 0}, {50, 10, 10, 10}, {3, 0, 10, 10},
	}
	for _, c := range cases {
		out := ansi.Strip(Bar(c.cur, c.max, c.width))
		if w := ansi.StringWidth(out); w != c.width {
			t.Errorf("Bar(%d,%d,%d) largeur %d", c.cur, c.max, c.width, w)
		}
		if got := strings.Count(out, "█"); got != c.filled {
			t.Errorf("Bar(%d,%d,%d) = %q, %d pleines, attendu %d", c.cur, c.max, c.width, out, got, c.filled)
		}
	}
	if Bar(5, 10, 0) != "" || FlatBar(5, 10, 0, "#fff") != "" {
		t.Error("largeur nulle: chaîne vide attendue")
	}
}

func TestBarColorChangesWithHealth(t *testing.T) {
	full, mid, low := Bar(10, 10, 10), Bar(5, 10, 10), Bar(2, 10, 10)
	if full == mid || mid == low || full == low {
		t.Fatal("la couleur de la jauge devrait changer selon les PV")
	}
}

func TestLogoLinesHaveEqualWidth(t *testing.T) {
	logo := Logo()
	if len(logo) != 5 {
		t.Fatalf("%d lignes", len(logo))
	}
	for i, l := range logo {
		if w := ansi.StringWidth(l); w != LogoWidth() {
			t.Errorf("ligne %d: largeur %d, attendu %d", i, w, LogoWidth())
		}
	}
}

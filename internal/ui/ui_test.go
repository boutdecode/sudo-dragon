package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPanelExactSize(t *testing.T) {
	for _, c := range []struct{ w, h int }{{4, 2}, {10, 3}, {30, 6}, {80, 24}} {
		out := Panel("Very long title that overflows", "a\nb\na line far too long to fit\nd\ne\nf\ng", c.w, c.h)
		lines := strings.Split(out, "\n")
		if len(lines) != c.h {
			t.Errorf("%dx%d: %d lines", c.w, c.h, len(lines))
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != c.w {
				t.Errorf("%dx%d line %d: width %d", c.w, c.h, i, got)
			}
		}
	}
	if Panel("x", "y", 3, 5) != "" || Panel("x", "y", 10, 1) != "" {
		t.Error("panel too small: empty string expected")
	}
}

func TestPanelShowsTitle(t *testing.T) {
	out := ansi.Strip(Panel("Joueurs", "", 20, 3))
	if !strings.HasPrefix(out, "╭─ Joueurs ") {
		t.Errorf("title missing: %q", out)
	}
}

func TestKeyBar(t *testing.T) {
	out := ansi.Strip(KeyBar([2]string{"e", "interagir"}, [2]string{"esc", "retour"}))
	if !strings.Contains(out, "interagir") || !strings.Contains(out, "esc") {
		t.Errorf("incomplete bar: %q", out)
	}
}

func TestBarWidthAndFill(t *testing.T) {
	cases := []struct{ cur, max, width, filled int }{
		{10, 10, 10, 10}, {5, 10, 10, 5}, {0, 10, 10, 0},
		{1, 100, 10, 1},  // never empty while HP remain
		{99, 100, 10, 9}, // never full while some are missing
		{-5, 10, 10, 0}, {50, 10, 10, 10}, {3, 0, 10, 10},
	}
	for _, c := range cases {
		out := ansi.Strip(Bar(c.cur, c.max, c.width))
		if w := ansi.StringWidth(out); w != c.width {
			t.Errorf("Bar(%d,%d,%d) width %d", c.cur, c.max, c.width, w)
		}
		if got := strings.Count(out, "█"); got != c.filled {
			t.Errorf("Bar(%d,%d,%d) = %q, %d filled, expected %d", c.cur, c.max, c.width, out, got, c.filled)
		}
	}
	if Bar(5, 10, 0) != "" || FlatBar(5, 10, 0, "#fff") != "" {
		t.Error("zero width: empty string expected")
	}
}

func TestBarColorChangesWithHealth(t *testing.T) {
	full, mid, low := Bar(10, 10, 10), Bar(5, 10, 10), Bar(2, 10, 10)
	if full == mid || mid == low || full == low {
		t.Fatal("the gauge colour should change with HP")
	}
}

func TestLogoLinesHaveEqualWidth(t *testing.T) {
	logo := Logo()
	if len(logo) != 5 {
		t.Fatalf("%d lines", len(logo))
	}
	for i, l := range logo {
		if w := ansi.StringWidth(l); w != LogoWidth() {
			t.Errorf("line %d: width %d, expected %d", i, w, LogoWidth())
		}
	}
}

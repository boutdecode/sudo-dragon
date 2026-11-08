package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/game"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestEverySpeciesHasArt(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		raw, ok := monsterArt[kind]
		if !ok {
			t.Errorf("%s: pas de sprite", kind)
			continue
		}
		if len(raw) < 3 || len(raw) > 6 {
			t.Errorf("%s: %d lignes (3 à 6 attendues)", kind, len(raw))
		}
		for _, l := range raw {
			for _, r := range l {
				if r > 126 || (r < 32) {
					t.Errorf("%s: caractère non ASCII imprimable %q", kind, r)
				}
			}
			if len(l) > 14 {
				t.Errorf("%s: ligne trop large (%d): %q", kind, len(l), l)
			}
		}
	}
}

func TestMonsterArtLinesHaveEqualWidth(t *testing.T) {
	for kind := range monsterArt {
		art := MonsterArt(kind)
		for _, l := range art {
			if len(l) != len(art[0]) {
				t.Errorf("%s: largeurs inégales (%d vs %d)", kind, len(l), len(art[0]))
			}
		}
	}
	// Une espèce inconnue a un sprite de secours, jamais vide.
	if art := MonsterArt("licorne"); len(art) == 0 || strings.TrimSpace(strings.Join(art, "")) == "" {
		t.Error("pas de sprite de secours")
	}
}

func TestBarWidthAndFill(t *testing.T) {
	cases := []struct {
		cur, max, width int
		filled          int
	}{
		{10, 10, 10, 10},
		{5, 10, 10, 5},
		{0, 10, 10, 0},
		{1, 100, 10, 1},  // jamais vide tant qu'il reste des PV
		{99, 100, 10, 9}, // jamais plein tant qu'il en manque
		{-5, 10, 10, 0},  // valeurs hors bornes
		{50, 10, 10, 10},
		{3, 0, 10, 10}, // max invalide: traité comme 1
	}
	for _, c := range cases {
		out := ansi.Strip(Bar(c.cur, c.max, c.width))
		if len([]rune(out)) != c.width {
			t.Errorf("Bar(%d,%d,%d) largeur %d", c.cur, c.max, c.width, len([]rune(out)))
		}
		if got := strings.Count(out, "#"); got != c.filled {
			t.Errorf("Bar(%d,%d,%d) = %q, %d cases pleines, attendu %d", c.cur, c.max, c.width, out, got, c.filled)
		}
	}
	if Bar(5, 10, 0) != "" {
		t.Error("largeur nulle: chaîne vide attendue")
	}
}

func TestBarColorChangesWithHealth(t *testing.T) {
	full, mid, low := Bar(10, 10, 10), Bar(5, 10, 10), Bar(2, 10, 10)
	if full == mid || mid == low || full == low {
		t.Fatal("la couleur de la jauge devrait changer selon les PV")
	}
}

func TestEveryNPCKindHasLookAndPortrait(t *testing.T) {
	for _, kind := range world.NPCKinds() {
		l, ok := npcLooks[kind]
		if !ok {
			t.Errorf("PNJ %q sans apparence", kind)
			continue
		}
		if l.glyph > 127 || l.glyph == '@' || l.glyph == ' ' {
			t.Errorf("%s: glyphe %q invalide", kind, l.glyph)
		}
		if _, ok := npcStyle[kind]; !ok {
			t.Errorf("%s: pas de style", kind)
		}
		raw, ok := npcArt[kind]
		if !ok {
			t.Errorf("PNJ %q sans portrait", kind)
			continue
		}
		if len(raw) < 3 || len(raw) > 6 {
			t.Errorf("%s: %d lignes (3 à 6 attendues)", kind, len(raw))
		}
		for _, line := range raw {
			for _, r := range line {
				if r < 32 || r > 126 {
					t.Errorf("%s: caractère non ASCII imprimable %q", kind, r)
				}
			}
			if len(line) > 14 {
				t.Errorf("%s: ligne trop large (%d): %q", kind, len(line), line)
			}
		}
	}
	// Réciproque: aucune apparence pour un PNJ que le générateur ne place pas.
	known := map[world.NPCKind]bool{}
	for _, k := range world.NPCKinds() {
		known[k] = true
	}
	for kind := range npcLooks {
		if !known[kind] {
			t.Errorf("apparence pour un PNJ inexistant: %q", kind)
		}
	}
}

func TestNPCArtLinesHaveEqualWidthWithFallback(t *testing.T) {
	for _, kind := range append(world.NPCKinds(), "inconnu") {
		art := NPCArt(kind)
		if len(art) == 0 {
			t.Fatalf("%s: portrait vide", kind)
		}
		for _, l := range art {
			if len(l) != len(art[0]) {
				t.Errorf("%s: largeurs inégales", kind)
			}
		}
	}
}

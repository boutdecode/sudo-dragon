package render

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/game"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestEverySpeciesHasArt(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		raw, ok := monsterArt[kind]
		if !ok {
			t.Errorf("%s: no sprite", kind)
			continue
		}
		if len(raw) < 3 || len(raw) > 6 {
			t.Errorf("%s: %d lines (3 to 6 expected)", kind, len(raw))
		}
		for _, l := range raw {
			for _, r := range l {
				if r > 126 || (r < 32) {
					t.Errorf("%s: printable non-ASCII character %q", kind, r)
				}
			}
			if len(l) > 14 {
				t.Errorf("%s: line too wide (%d): %q", kind, len(l), l)
			}
		}
	}
}

func TestMonsterArtLinesHaveEqualWidth(t *testing.T) {
	for kind := range monsterArt {
		art := MonsterArt(kind)
		for _, l := range art {
			if len(l) != len(art[0]) {
				t.Errorf("%s: unequal widths (%d vs %d)", kind, len(l), len(art[0]))
			}
		}
	}
	// An unknown species has a fallback sprite, never empty.
	if art := MonsterArt("licorne"); len(art) == 0 || strings.TrimSpace(strings.Join(art, "")) == "" {
		t.Error("no fallback sprite")
	}
}

func TestEveryNPCKindHasLookAndPortrait(t *testing.T) {
	for _, kind := range world.NPCKinds() {
		l, ok := npcLooks[kind]
		if !ok {
			t.Errorf("NPC %q without a look", kind)
			continue
		}
		if l.glyph > 127 || l.glyph == '@' || l.glyph == ' ' {
			t.Errorf("%s: invalid glyph %q", kind, l.glyph)
		}
		if _, ok := npcStyle[kind]; !ok {
			t.Errorf("%s: no style", kind)
		}
		raw, ok := npcArt[kind]
		if !ok {
			t.Errorf("NPC %q without a portrait", kind)
			continue
		}
		if len(raw) < 3 || len(raw) > 6 {
			t.Errorf("%s: %d lines (3 to 6 expected)", kind, len(raw))
		}
		for _, line := range raw {
			for _, r := range line {
				if r < 32 || r > 126 {
					t.Errorf("%s: printable non-ASCII character %q", kind, r)
				}
			}
			if len(line) > 14 {
				t.Errorf("%s: line too wide (%d): %q", kind, len(line), line)
			}
		}
	}
	// Converse: no look for an NPC that the generator does not place.
	known := map[world.NPCKind]bool{}
	for _, k := range world.NPCKinds() {
		known[k] = true
	}
	for kind := range npcLooks {
		if !known[kind] && kind != questGiverKind && kind != questTurnInKind && kind != world.NPCGuard { // map look only
			t.Errorf("look for a nonexistent NPC: %q", kind)
		}
	}
}

func TestNPCArtLinesHaveEqualWidthWithFallback(t *testing.T) {
	for _, kind := range append(world.NPCKinds(), "inconnu") {
		art := NPCArt(kind)
		if len(art) == 0 {
			t.Fatalf("%s: empty portrait", kind)
		}
		for _, l := range art {
			if len(l) != len(art[0]) {
				t.Errorf("%s: unequal widths", kind)
			}
		}
	}
}

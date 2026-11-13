package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// townsAndVillages returns the villages and towns of the test world around the start.
func townsAndVillages(w *World) []*Structure {
	var out []*Structure
	for _, s := range w.StructuresNear(w.FindSpawn(), 1500) {
		if s.Kind == KindVillage || s.Kind == KindTown {
			out = append(out, s)
		}
	}
	return out
}

func TestVillagesAndTownsHaveGuards(t *testing.T) {
	w := newTestWorld(1)
	seenVillage, seenTown := false, false
	for _, s := range townsAndVillages(w) {
		want := guardsPerKind[s.Kind]
		if len(s.Guards) != want {
			t.Errorf("%s (%v): %d guards, want %d", s.Name, s.Kind, len(s.Guards), want)
		}
		names := map[string]bool{}
		for _, n := range s.NPCs {
			names[n.Name] = true
		}
		pos := map[Pos]bool{}
		for i, gd := range s.Guards {
			if gd.Kind != NPCGuard || gd.Index != GuardIndexBase+i || gd.Region != s.Region {
				t.Errorf("%s: guard %d malformed: %+v", s.Name, i, gd)
			}
			if !s.RoadAt(gd.Pos.X, gd.Pos.Y) {
				t.Errorf("%s: guard %d off a road (%v)", s.Name, i, gd.Pos)
			}
			if pos[gd.Pos] {
				t.Errorf("%s: two guards on %v", s.Name, gd.Pos)
			}
			pos[gd.Pos] = true
			if _, onNPC := w.NPCAt(gd.Pos.X, gd.Pos.Y); onNPC {
				t.Errorf("%s: guard on an NPC", s.Name)
			}
			if names[gd.Name] {
				t.Errorf("%s: duplicate guard name: %s", s.Name, gd.Name)
			}
			names[gd.Name] = true
			if gd.NameIn(i18n.EN) == "" {
				t.Errorf("%s: no English name", s.Name)
			}
		}
		seenVillage = seenVillage || s.Kind == KindVillage
		seenTown = seenTown || s.Kind == KindTown
	}
	if !seenVillage {
		t.Skip("no village near the start")
	}
	_ = seenTown
}

func TestGuardsAreDeterministicAndNeverPlacedInFortresses(t *testing.T) {
	a, b := newTestWorld(7), newTestWorld(7)
	for _, s := range a.StructuresNear(a.FindSpawn(), 1500) {
		o := b.StructureOfRegion(s.Region)
		if len(o.Guards) != len(s.Guards) {
			t.Fatalf("%s: different number of guards", s.Name)
		}
		for i := range s.Guards {
			if s.Guards[i].Pos != o.Guards[i].Pos || s.Guards[i].Name != o.Guards[i].Name {
				t.Fatalf("%s: guard %d not deterministic", s.Name, i)
			}
		}
		if s.Kind == KindFortress && len(s.Guards) != 0 {
			t.Fatalf("%s: a fortress has no guards", s.Name)
		}
	}
}

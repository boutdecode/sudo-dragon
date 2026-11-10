package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// townsAndVillages renvoie les villages et villes du monde de test autour du départ.
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
	w := New(1)
	seenVillage, seenTown := false, false
	for _, s := range townsAndVillages(w) {
		want := guardsPerKind[s.Kind]
		if len(s.Guards) != want {
			t.Errorf("%s (%v): %d gardes, attendu %d", s.Name, s.Kind, len(s.Guards), want)
		}
		names := map[string]bool{}
		for _, n := range s.NPCs {
			names[n.Name] = true
		}
		pos := map[Pos]bool{}
		for i, gd := range s.Guards {
			if gd.Kind != NPCGuard || gd.Index != GuardIndexBase+i || gd.Region != s.Region {
				t.Errorf("%s: garde %d mal formé: %+v", s.Name, i, gd)
			}
			if !s.RoadAt(gd.Pos.X, gd.Pos.Y) {
				t.Errorf("%s: garde %d hors d'un chemin (%v)", s.Name, i, gd.Pos)
			}
			if pos[gd.Pos] {
				t.Errorf("%s: deux gardes sur %v", s.Name, gd.Pos)
			}
			pos[gd.Pos] = true
			if _, onNPC := w.NPCAt(gd.Pos.X, gd.Pos.Y); onNPC {
				t.Errorf("%s: garde sur un PNJ", s.Name)
			}
			if names[gd.Name] {
				t.Errorf("%s: nom de garde en double: %s", s.Name, gd.Name)
			}
			names[gd.Name] = true
			if gd.NameIn(i18n.EN) == "" {
				t.Errorf("%s: pas de nom anglais", s.Name)
			}
		}
		seenVillage = seenVillage || s.Kind == KindVillage
		seenTown = seenTown || s.Kind == KindTown
	}
	if !seenVillage {
		t.Skip("pas de village près du départ")
	}
	_ = seenTown
}

func TestGuardsAreDeterministicAndNeverPlacedInFortresses(t *testing.T) {
	a, b := New(7), New(7)
	for _, s := range a.StructuresNear(a.FindSpawn(), 1500) {
		o := b.StructureOfRegion(s.Region)
		if len(o.Guards) != len(s.Guards) {
			t.Fatalf("%s: nombre de gardes différent", s.Name)
		}
		for i := range s.Guards {
			if s.Guards[i].Pos != o.Guards[i].Pos || s.Guards[i].Name != o.Guards[i].Name {
				t.Fatalf("%s: garde %d non déterministe", s.Name, i)
			}
		}
		if s.Kind == KindFortress && len(s.Guards) != 0 {
			t.Fatalf("%s: une forteresse n'a pas de gardes", s.Name)
		}
	}
}

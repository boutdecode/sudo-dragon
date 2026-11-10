package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

func TestPOIsAreDeterministicAndPlaced(t *testing.T) {
	a, b := newTestWorld(7), newTestWorld(7)
	counts := map[POIKind]int{}
	regions := 0
	for ry := -6; ry <= 6; ry++ {
		for rx := -6; rx <= 6; rx++ {
			rp := RegionPos{rx, ry}
			regions++
			p, q := a.POIOfRegion(rp), b.POIOfRegion(rp)
			if (p == nil) != (q == nil) {
				t.Fatalf("%v: présence différente entre deux mondes de même seed", rp)
			}
			if p == nil {
				continue
			}
			if p.Pos != q.Pos || p.Kind != q.Kind || p.Seed != q.Seed || p.NameIn(i18n.FR) != q.NameIn(i18n.FR) {
				t.Fatalf("%v: points différents", rp)
			}
			counts[p.Kind]++
			if RegionOf(p.Pos.X, p.Pos.Y) != rp {
				t.Errorf("%v: le point %v sort de sa région", rp, p.Pos)
			}
			if got := a.TileAt(p.Pos.X, p.Pos.Y); got != p.Tile() {
				t.Errorf("%v: tuile %d à %v, attendu %d", rp, got, p.Pos, p.Tile())
			}
			if got := a.At(p.Pos.X, p.Pos.Y); got != p.Tile() {
				t.Errorf("%v: chunk %d à %v, attendu %d", rp, got, p.Pos, p.Tile())
			}
			if a.POIAt(p.Pos.X, p.Pos.Y) != p {
				t.Errorf("%v: POIAt ne retrouve pas le point", rp)
			}
			if s := a.StructureAt(p.Pos.X, p.Pos.Y); s != nil {
				t.Errorf("%v: point dans un lieu", rp)
			}
			if d := a.DungeonOfRegion(rp); d != nil && d.Entrance == p.Pos {
				t.Errorf("%v: point sur l'entrée d'un donjon", rp)
			}
			for _, s := range p.Slots {
				if !a.TileAt(s.X, s.Y).Walkable() {
					t.Errorf("%v: emplacement %v de repaire infranchissable", rp, s)
				}
			}
			if p.Kind == POILair && len(p.Slots) != 3 {
				t.Errorf("%v: repaire à %d emplacements", rp, len(p.Slots))
			}
			if p.Kind == POIRuin && len(p.Slots) != 1 {
				t.Errorf("%v: ruine à %d gardiens", rp, len(p.Slots))
			}
		}
	}
	for _, k := range []POIKind{POICampfire, POIShrine, POILair, POITower, POIStone, POISpring, POIRuin} {
		if counts[k] == 0 {
			t.Errorf("aucun point de type %d sur %d régions", k, regions)
		}
	}
}

// Ajouter des types de points ne doit déplacer ni changer aucun point déjà posé : les nouveaux types n'occupent que
// des régions jusque-là vides.
func TestNewPOIKindsOnlyFillEmptyRegions(t *testing.T) {
	w := newTestWorld(7)
	for ry := -8; ry <= 8; ry++ {
		for rx := -8; rx <= 8; rx++ {
			rp := RegionPos{rx, ry}
			p := w.POIOfRegion(rp)
			if p == nil {
				continue
			}
			roll := identityRNG(w.Seed, rp, poiSalt).Float64()
			if (roll < poiChance) == (p.Kind == POISpring || p.Kind == POIRuin) {
				t.Fatalf("%v: tirage %.2f, type %d : une source ou une ruine ne doit sortir que de la plage ajoutée", rp, roll, p.Kind)
			}
		}
	}
}

func TestDigSpotIsOrdinaryAndDeterministic(t *testing.T) {
	a, b := newTestWorld(7), newTestWorld(7)
	found := 0
	for ry := -3; ry <= 3; ry++ {
		for rx := -3; rx <= 3; rx++ {
			rp := RegionPos{rx, ry}
			for nonce := uint64(0); nonce < 3; nonce++ {
				p, ok := a.DigSpot(rp, nonce)
				q, ok2 := b.DigSpot(rp, nonce)
				if ok != ok2 || p != q {
					t.Fatalf("%v/%d: deux mondes de même seed donnent %v et %v", rp, nonce, p, q)
				}
				if !ok {
					continue
				}
				found++
				if RegionOf(p.X, p.Y) != rp {
					t.Errorf("%v: case %v hors de la région", rp, p)
				}
				if a.StructureAt(p.X, p.Y) != nil || a.POIAt(p.X, p.Y) != nil || a.DungeonAt(p.X, p.Y) != nil {
					t.Errorf("%v: case %v occupée par un lieu, un point ou une entrée", rp, p)
				}
				if !a.TileAt(p.X, p.Y).Walkable() {
					t.Errorf("%v: case %v infranchissable", rp, p)
				}
			}
		}
	}
	if found < 100 {
		t.Errorf("seulement %d cases trouvées sur 147 essais", found)
	}
}

// En v1, une région porte au plus un point (le principal); en v2, des points supplémentaires s'y ajoutent: ils sont
// déterministes, distincts, à distance les uns des autres, jamais des pierres dressées, et le principal ne bouge pas.
func TestExtraPOIsOnlyInV2AndStayApart(t *testing.T) {
	v1, v2 := NewVersion(7, GenV1), NewVersion(7, GenV2)
	again := NewVersion(7, GenV2)
	var n1, n2, extras int
	for ry := -6; ry <= 6; ry++ {
		for rx := -6; rx <= 6; rx++ {
			rp := RegionPos{rx, ry}
			l1, l2, l2b := v1.POIsOfRegion(rp), v2.POIsOfRegion(rp), again.POIsOfRegion(rp)
			n1, n2 = n1+len(l1), n2+len(l2)
			if len(l1) > 1 {
				t.Fatalf("v1: %d points en %v", len(l1), rp)
			}
			if len(l2) != len(l2b) {
				t.Fatalf("v2 non déterministe en %v", rp)
			}
			seen := map[string]bool{}
			for i, p := range l2 {
				if q := l2b[i]; p.Pos != q.Pos || p.Kind != q.Kind || p.Seed != q.Seed || p.ID() != q.ID() {
					t.Fatalf("v2 non déterministe en %v: %+v / %+v", rp, p, q)
				}
				if seen[p.ID()] {
					t.Fatalf("identifiant %q en double dans %v", p.ID(), rp)
				}
				seen[p.ID()] = true
				if RegionOf(p.Pos.X, p.Pos.Y) != rp {
					t.Fatalf("le point %v sort de sa région %v", p.Pos, rp)
				}
				if p.Index > 0 {
					extras++
					if p.Kind == POIStone {
						t.Fatalf("pierre dressée supplémentaire en %v", rp)
					}
				}
				for _, o := range l2[:i] {
					if max(abs(o.Pos.X-p.Pos.X), abs(o.Pos.Y-p.Pos.Y)) <= poiExtraGap {
						t.Fatalf("points trop proches en %v: %v et %v", rp, o.Pos, p.Pos)
					}
				}
			}
			// Le point principal est le même qu'en v1 pour une même région et un même terrain: on vérifie
			// seulement qu'il garde l'indice 0 et vient en tête.
			if len(l2) > 0 && l2[0].Index != 0 && v2.POIOfRegion(rp) != nil {
				t.Fatalf("le point principal doit venir en tête en %v", rp)
			}
			for _, p := range l2 {
				if got := v2.POIAt(p.Pos.X, p.Pos.Y); got != p {
					t.Fatalf("POIAt(%v) ne retrouve pas %s", p.Pos, p.ID())
				}
			}
		}
	}
	if extras == 0 || n2 <= n1 {
		t.Errorf("la v2 doit avoir plus de points que la v1: %d contre %d (%d supplémentaires)", n2, n1, extras)
	}
}

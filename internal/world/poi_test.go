package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

func TestPOIsAreDeterministicAndPlaced(t *testing.T) {
	a, b := New(7), New(7)
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
	w := New(7)
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
	a, b := New(7), New(7)
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

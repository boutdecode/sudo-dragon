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
		}
	}
	for _, k := range []POIKind{POICampfire, POIShrine, POILair, POITower, POIStone} {
		if counts[k] == 0 {
			t.Errorf("aucun point de type %d sur %d régions", k, regions)
		}
	}
}

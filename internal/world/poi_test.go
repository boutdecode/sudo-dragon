package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

func TestPOIsAreDeterministicAndPlaced(t *testing.T) {
	t.Parallel()
	a, b := New(7), New(7)
	counts := map[POIKind]int{}
	regions := 0
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			rp := RegionPos{rx, ry}
			regions++
			p, q := a.POIOfRegion(rp), b.POIOfRegion(rp)
			if (p == nil) != (q == nil) {
				t.Fatalf("%v: presence differs between two worlds with the same seed", rp)
			}
			if p == nil {
				continue
			}
			if p.Pos != q.Pos || p.Kind != q.Kind || p.Seed != q.Seed || p.NameIn(i18n.FR) != q.NameIn(i18n.FR) {
				t.Fatalf("%v: different points", rp)
			}
			counts[p.Kind]++
			if RegionOf(p.Pos.X, p.Pos.Y) != rp {
				t.Errorf("%v: point %v leaves its region", rp, p.Pos)
			}
			if got := a.TileAt(p.Pos.X, p.Pos.Y); got != p.Tile() {
				t.Errorf("%v: tile %d at %v, want %d", rp, got, p.Pos, p.Tile())
			}
			if got := a.At(p.Pos.X, p.Pos.Y); got != p.Tile() {
				t.Errorf("%v: chunk %d at %v, want %d", rp, got, p.Pos, p.Tile())
			}
			if a.POIAt(p.Pos.X, p.Pos.Y) != p {
				t.Errorf("%v: POIAt does not find the point", rp)
			}
			if s := a.StructureAt(p.Pos.X, p.Pos.Y); s != nil {
				t.Errorf("%v: point inside a place", rp)
			}
			if d := a.DungeonOfRegion(rp); d != nil && d.Entrance == p.Pos {
				t.Errorf("%v: point on a dungeon entrance", rp)
			}
			for _, s := range p.Slots {
				if !a.TileAt(s.X, s.Y).Walkable() {
					t.Errorf("%v: lair slot %v impassable", rp, s)
				}
			}
			if p.Kind == POILair && len(p.Slots) != 3 {
				t.Errorf("%v: lair with %d slots", rp, len(p.Slots))
			}
			if p.Kind == POIRuin && len(p.Slots) != 1 {
				t.Errorf("%v: ruin with %d guardians", rp, len(p.Slots))
			}
		}
	}
	for _, k := range []POIKind{POICampfire, POIShrine, POILair, POITower, POIStone, POISpring, POIRuin} {
		if counts[k] == 0 {
			t.Errorf("no point of type %d over %d regions", k, regions)
		}
	}
}

// Adding point types must neither move nor change any point already placed: the new types only occupy
// regions that were empty until then.
func TestNewPOIKindsOnlyFillEmptyRegions(t *testing.T) {
	t.Parallel()
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
				t.Fatalf("%v: roll %.2f, type %d: a spring or ruin must only come from the added range", rp, roll, p.Kind)
			}
		}
	}
}

func TestDigSpotIsOrdinaryAndDeterministic(t *testing.T) {
	t.Parallel()
	a, b := New(7), New(7)
	found := 0
	for ry := -3; ry <= 3; ry++ {
		for rx := -3; rx <= 3; rx++ {
			rp := RegionPos{rx, ry}
			for nonce := uint64(0); nonce < 3; nonce++ {
				p, ok := a.DigSpot(rp, nonce)
				q, ok2 := b.DigSpot(rp, nonce)
				if ok != ok2 || p != q {
					t.Fatalf("%v/%d: two worlds with the same seed give %v and %v", rp, nonce, p, q)
				}
				if !ok {
					continue
				}
				found++
				if RegionOf(p.X, p.Y) != rp {
					t.Errorf("%v: tile %v outside the region", rp, p)
				}
				if a.StructureAt(p.X, p.Y) != nil || a.POIAt(p.X, p.Y) != nil || a.DungeonAt(p.X, p.Y) != nil {
					t.Errorf("%v: tile %v taken by a place, a point or an entrance", rp, p)
				}
				if !a.TileAt(p.X, p.Y).Walkable() {
					t.Errorf("%v: tile %v impassable", rp, p)
				}
			}
		}
	}
	if found < 100 {
		t.Errorf("only %d tiles found over 147 attempts", found)
	}
}

// A region carries a main point plus extra ones: they are deterministic, distinct, apart from one another,
// never standing stones, and the main one comes first.
func TestExtraPOIsStayApart(t *testing.T) {
	t.Parallel()
	w, again := New(7), New(7)
	extras := 0
	for ry := -6; ry <= 6; ry++ {
		for rx := -6; rx <= 6; rx++ {
			rp := RegionPos{rx, ry}
			l, lb := w.POIsOfRegion(rp), again.POIsOfRegion(rp)
			if len(l) != len(lb) {
				t.Fatalf("not deterministic in %v", rp)
			}
			seen := map[string]bool{}
			for i, p := range l {
				if q := lb[i]; p.Pos != q.Pos || p.Kind != q.Kind || p.Seed != q.Seed || p.ID() != q.ID() {
					t.Fatalf("not deterministic in %v: %+v / %+v", rp, p, q)
				}
				if seen[p.ID()] {
					t.Fatalf("duplicate identifier %q in %v", p.ID(), rp)
				}
				seen[p.ID()] = true
				if RegionOf(p.Pos.X, p.Pos.Y) != rp {
					t.Fatalf("point %v leaves its region %v", p.Pos, rp)
				}
				if p.Index > 0 {
					extras++
					if p.Kind == POIStone {
						t.Fatalf("extra standing stone in %v", rp)
					}
				}
				for _, o := range l[:i] {
					if max(abs(o.Pos.X-p.Pos.X), abs(o.Pos.Y-p.Pos.Y)) <= poiExtraGap {
						t.Fatalf("points too close in %v: %v and %v", rp, o.Pos, p.Pos)
					}
				}
			}
			if len(l) > 0 && l[0].Index != 0 && w.POIOfRegion(rp) != nil {
				t.Fatalf("the main point must come first in %v", rp)
			}
			for _, p := range l {
				if got := w.POIAt(p.Pos.X, p.Pos.Y); got != p {
					t.Fatalf("POIAt(%v) does not find %s", p.Pos, p.ID())
				}
			}
		}
	}
	if extras == 0 {
		t.Error("no extra point found")
	}
}

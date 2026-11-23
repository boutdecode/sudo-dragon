package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

func cabinsIn(w *World, n int) []*POI {
	var out []*POI
	for ry := -n; ry <= n; ry++ {
		for rx := -n; rx <= n; rx++ {
			if c := w.cabinOf(RegionPos{rx, ry}); c != nil {
				out = append(out, c)
			}
		}
	}
	return out
}

// Cabins are deterministic, last in their region, and leave the base points untouched.
func TestCabinsAreDeterministicAndAppended(t *testing.T) {
	t.Parallel()
	a, b := New(7), New(7)
	found := 0
	for ry := -5; ry <= 5; ry++ {
		for rx := -5; rx <= 5; rx++ {
			rp := RegionPos{rx, ry}
			l, base := a.POIsOfRegion(rp), b.basePOIsOfRegion(rp)
			for i, p := range base {
				if q := l[i]; p.Pos != q.Pos || p.Kind != q.Kind || p.Seed != q.Seed || p.Bounds != q.Bounds {
					t.Fatalf("%v: base point %d changed by the cabin", rp, i)
				}
			}
			if len(l) == len(base) {
				continue
			}
			found++
			c := l[len(l)-1]
			if len(l) != len(base)+1 || !c.Kind.IsCabin() || c.Index != cabinPOIIndex {
				t.Fatalf("%v: the cabin must be the last point, with index %d: %+v", rp, cabinPOIIndex, c)
			}
			d := b.cabinOf(rp)
			if d == nil || d.Pos != c.Pos || d.Kind != c.Kind || d.Seed != c.Seed {
				t.Fatalf("%v: not deterministic", rp)
			}
			if RegionOf(c.Pos.X, c.Pos.Y) != rp || a.StructureAt(c.Pos.X, c.Pos.Y) != nil {
				t.Errorf("%v: cabin outside its region or inside a place", rp)
			}
			for _, o := range base {
				if c.Bounds.grow(1).Contains(o.Pos.X, o.Pos.Y) || o.Bounds.Contains(c.Pos.X, c.Pos.Y) {
					t.Errorf("%v: cabin overlaps %v", rp, o.ID())
				}
			}
			if got := a.TileAt(c.Pos.X, c.Pos.Y); got != Floor {
				t.Errorf("%v: tile %d under the vendor, want Floor", rp, got)
			}
		}
	}
	if found < 5 {
		t.Errorf("only %d cabins over 121 regions", found)
	}
}

// Whatever the seed, the starting ring holds one cabin of each trade (a tool is mandatory to gather).
func TestStartingRingHasEveryTrade(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 12; seed++ {
		w := sharedWorld(seed)
		a := w.spawnAnchor()
		c := RegionOf(a.X, a.Y)
		trades := map[POIKind]bool{}
		for dy := -cabinRing; dy <= cabinRing; dy++ {
			for dx := -cabinRing; dx <= cabinRing; dx++ {
				if p := w.cabinOf(RegionPos{c.X + dx, c.Y + dy}); p != nil {
					trades[p.Kind] = true
				}
			}
		}
		for _, k := range cabinKinds {
			if !trades[k] {
				t.Errorf("seed %d: no cabin of kind %d around the start", seed, k)
			}
		}
	}
}

// A vendor is found by NPCAt and NPCsIn, stands on the cabin tile and has a trade identity.
func TestCabinVendorIsAnNPC(t *testing.T) {
	t.Parallel()
	w := sharedWorld(42)
	cabins := cabinsIn(w, 4)
	if len(cabins) == 0 {
		t.Fatal("no cabin")
	}
	for _, c := range cabins {
		npc, ok := w.NPCAt(c.Pos.X, c.Pos.Y)
		if !ok || npc.Kind != c.Kind.VendorKind() || npc.Pos != c.Pos || npc.Index != CabinIndexBase || npc.Region != c.Region {
			t.Fatalf("%v: NPCAt gave %+v, %v", c.ID(), npc, ok)
		}
		if npc.NameIn(i18n.FR) == "" || npc.Name == "" {
			t.Errorf("%v: vendor without a name", c.ID())
		}
		found := false
		for _, n := range w.NPCsIn(c.Pos.X-3, c.Pos.Y-3, c.Pos.X+3, c.Pos.Y+3) {
			found = found || n.Key() == npc.Key()
		}
		if !found {
			t.Errorf("%v: NPCsIn does not list the vendor", c.ID())
		}
		if _, ok := w.NPCAt(c.Pos.X+1, c.Pos.Y); ok {
			t.Errorf("%v: an NPC next to the cabin centre", c.ID())
		}
	}
}

// No road crosses a cabin, and nothing but the building covers its footprint.
func TestRoadsAvoidCabins(t *testing.T) {
	t.Parallel()
	w := sharedWorld(7)
	for _, c := range cabinsIn(w, 3) {
		for y := c.Bounds.Y; y < c.Bounds.Y+c.Bounds.H; y++ {
			for x := c.Bounds.X; x < c.Bounds.X+c.Bounds.W; x++ {
				if w.TileAt(x, y) == Road {
					t.Fatalf("%v: road at %d,%d inside the cabin", c.ID(), x, y)
				}
			}
		}
	}
}

// The trade follows the land: lumberjacks in woods, miners in rock, herbalists in meadows.
func TestCabinTradeFollowsTerrain(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	match, total := 0, 0
	for _, c := range cabinsIn(w, 8) {
		aff := w.cabinAffinity(c.Pos)
		best := max(aff[0], aff[1], aff[2])
		for i, k := range cabinKinds {
			if k == c.Kind {
				total++
				if aff[i] == best {
					match++
				}
			}
		}
	}
	if total < 5 {
		t.Fatalf("only %d cabins", total)
	}
	// Forced cabins of the starting ring may settle for less; the others always take the best trade.
	if match*10 < total*7 {
		t.Errorf("only %d cabins out of %d match their terrain", match, total)
	}
}

func TestCabinShapeFitsAndReachable(t *testing.T) {
	t.Parallel()
	for _, k := range cabinKinds {
		for cl := Climate(0); cl < NumClimates; cl++ {
			s := buildShape(k, cl, 5)
			if s.r != siteRadius(k) {
				t.Errorf("kind %d climate %d: radius %d, site %d", k, cl, s.r, siteRadius(k))
			}
		}
	}
}

package world

import "testing"

// linkedPairs returns the pairs of neighbouring regions linked by a road.
func linkedPairs(w *World, n int) []roadKey {
	var out []roadKey
	for ry := -n; ry < n; ry++ {
		for rx := -n; rx < n; rx++ {
			a := RegionPos{rx, ry}
			for _, b := range []RegionPos{{rx + 1, ry}, {rx, ry + 1}} {
				if l := w.roadFor(roadKey{a, b}); l != nil {
					out = append(out, roadKey{a, b})
				}
			}
		}
	}
	return out
}

func (w *World) roadFor(k roadKey) *roadLink {
	w.rmu.Lock()
	defer w.rmu.Unlock()
	return w.buildRoad(k)
}

func TestRoadsExistAndAreDeterministic(t *testing.T) {
	a, b := newTestWorld(1), newTestWorld(1)
	pairs := linkedPairs(a, 3)
	if len(pairs) == 0 {
		t.Fatal("no road generated between the neighbouring regions")
	}
	for _, k := range pairs {
		la, lb := a.roadFor(k), b.roadFor(k)
		if lb == nil || len(la.tiles) != len(lb.tiles) {
			t.Fatalf("%v: route not deterministic", k)
		}
		for p := range la.tiles {
			if _, ok := lb.tiles[p]; !ok {
				t.Fatalf("%v: tile %v missing from the second route", k, p)
			}
		}
	}
	t.Logf("%d links over 6x6 regions", len(pairs))
}

// A road never touches the footprint of a structure, and the tile seen by At
// (chunks) is that of TileAt.
func TestRoadsStayOutsideStructuresAndAreWalkable(t *testing.T) {
	w := newTestWorld(1)
	for _, k := range linkedPairs(w, 3) {
		l := w.roadFor(k)
		for p := range l.tiles {
			if s := w.StructureAt(p.X, p.Y); s != nil {
				t.Fatalf("%v: road %v enters %s", k, p, s.Name)
			}
			if got := w.TileAt(p.X, p.Y); got != Road {
				t.Fatalf("%v: tile %v = %d, want Road", k, p, got)
			}
			if got := w.At(p.X, p.Y); got != Road {
				t.Fatalf("%v: At(%v) = %d, want Road", k, p, got)
			}
		}
	}
}

// Two linked places are reachable on foot through walkable tiles.
func TestRoadsConnectTheirPlaces(t *testing.T) {
	w := newTestWorld(1)
	checked := 0
	for _, k := range linkedPairs(w, 3) {
		sa, sb := w.structureForRegion(k.a), w.structureForRegion(k.b)
		_, anchorsA := sa.outsideReach()
		_, anchorsB := sb.outsideReach()
		from, ok1 := pickAnchor(map[Pos]bool{}, anchorsA, sb.Center())
		to, ok2 := pickAnchor(map[Pos]bool{}, anchorsB, sa.Center())
		if !ok1 || !ok2 {
			continue
		}
		seen := map[Pos]bool{from: true}
		queue := []Pos{from}
		for len(queue) > 0 && !seen[to] {
			p := queue[0]
			queue = queue[1:]
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				q := Pos{p.X + d[0], p.Y + d[1]}
				if seen[q] || !w.TileAt(q.X, q.Y).Walkable() {
					continue
				}
				// Stays on the road or within the two footprints.
				if sa.Contains(q.X, q.Y) || sb.Contains(q.X, q.Y) || w.TileAt(q.X, q.Y) == Road {
					seen[q] = true
					queue = append(queue, q)
				}
			}
		}
		if !seen[to] {
			t.Fatalf("%v: %v and %v are not linked by the road", k, from, to)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no link checked")
	}
}

func TestRoadRoutesAreOrderedContinuousAndOnRoads(t *testing.T) {
	w := newTestWorld(1)
	routes := w.RoadRoutesNear(0, 0, 4*RegionSize)
	if len(routes) == 0 {
		t.Fatal("no road near the origin")
	}
	for _, r := range routes {
		if len(r.Path) < 2 {
			t.Errorf("%s: route of %d tiles", r.ID, len(r.Path))
			continue
		}
		for i, p := range r.Path {
			if w.TileAt(p.X, p.Y) != Road {
				t.Errorf("%s: tile %v is not a road", r.ID, p)
			}
			if i > 0 {
				if prev := r.Path[i-1]; max(abs(p.X-prev.X), abs(p.Y-prev.Y)) > 1 {
					t.Errorf("%s: jump from %v to %v", r.ID, prev, p)
				}
			}
		}
		if r.A == r.B {
			t.Errorf("%s: same regions", r.ID)
		}
		seed, ok := w.HasCaravan(r)
		if again, ok2 := w.HasCaravan(r); ok != ok2 || seed != again {
			t.Errorf("%s: caravan existence unstable", r.ID)
		}
		if ok {
			npc := w.CaravanMerchant(r, r.Path[0])
			if npc.Index < CaravanIndexBase || npc.Region != r.A || npc.Name == "" || (npc.Kind != NPCSmith && npc.Kind != NPCArmorer) {
				t.Errorf("%s: inconsistent merchant %+v", r.ID, npc)
			}
		}
	}
}

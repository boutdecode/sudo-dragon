package world

import "testing"

// linkedPairs renvoie les couples de régions voisines reliées par une route.
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
	a, b := New(1), New(1)
	pairs := linkedPairs(a, 3)
	if len(pairs) == 0 {
		t.Fatal("aucune route générée entre les régions voisines")
	}
	for _, k := range pairs {
		la, lb := a.roadFor(k), b.roadFor(k)
		if lb == nil || len(la.tiles) != len(lb.tiles) {
			t.Fatalf("%v: tracé non déterministe", k)
		}
		for p := range la.tiles {
			if _, ok := lb.tiles[p]; !ok {
				t.Fatalf("%v: tuile %v absente du second tracé", k, p)
			}
		}
	}
	t.Logf("%d liens sur 6x6 régions", len(pairs))
}

// Une route ne touche jamais l'emprise d'une structure, et la tuile vue par At
// (chunks) est celle de TileAt.
func TestRoadsStayOutsideStructuresAndAreWalkable(t *testing.T) {
	w := New(1)
	for _, k := range linkedPairs(w, 3) {
		l := w.roadFor(k)
		for p := range l.tiles {
			if s := w.StructureAt(p.X, p.Y); s != nil {
				t.Fatalf("%v: la route %v entre dans %s", k, p, s.Name)
			}
			if got := w.TileAt(p.X, p.Y); got != Road {
				t.Fatalf("%v: tuile %v = %d, attendu Road", k, p, got)
			}
			if got := w.At(p.X, p.Y); got != Road {
				t.Fatalf("%v: At(%v) = %d, attendu Road", k, p, got)
			}
		}
	}
}

// Deux lieux reliés sont joignables à pied par les tuiles praticables.
func TestRoadsConnectTheirPlaces(t *testing.T) {
	w := New(1)
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
				// Reste sur la route ou dans les deux emprises.
				if sa.Contains(q.X, q.Y) || sb.Contains(q.X, q.Y) || w.TileAt(q.X, q.Y) == Road {
					seen[q] = true
					queue = append(queue, q)
				}
			}
		}
		if !seen[to] {
			t.Fatalf("%v: %v et %v ne sont pas reliés par la route", k, from, to)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("aucun lien vérifié")
	}
}

func TestRoadRoutesAreOrderedContinuousAndOnRoads(t *testing.T) {
	w := New(1)
	routes := w.RoadRoutesNear(0, 0, 4*RegionSize)
	if len(routes) == 0 {
		t.Fatal("aucune route près de l'origine")
	}
	for _, r := range routes {
		if len(r.Path) < 2 {
			t.Errorf("%s: tracé de %d cases", r.ID, len(r.Path))
			continue
		}
		for i, p := range r.Path {
			if w.TileAt(p.X, p.Y) != Road {
				t.Errorf("%s: la case %v n'est pas une route", r.ID, p)
			}
			if i > 0 {
				if prev := r.Path[i-1]; max(abs(p.X-prev.X), abs(p.Y-prev.Y)) > 1 {
					t.Errorf("%s: saut de %v à %v", r.ID, prev, p)
				}
			}
		}
		if r.A == r.B {
			t.Errorf("%s: mêmes régions", r.ID)
		}
		seed, ok := w.HasCaravan(r)
		if again, ok2 := w.HasCaravan(r); ok != ok2 || seed != again {
			t.Errorf("%s: existence de la caravane instable", r.ID)
		}
		if ok {
			npc := w.CaravanMerchant(r, r.Path[0])
			if npc.Index < CaravanIndexBase || npc.Region != r.A || npc.Name == "" || (npc.Kind != NPCSmith && npc.Kind != NPCArmorer) {
				t.Errorf("%s: marchand incohérent %+v", r.ID, npc)
			}
		}
	}
}

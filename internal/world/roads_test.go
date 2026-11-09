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

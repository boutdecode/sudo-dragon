package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// dungeonsAround liste les donjons des régions de -n à n.
func dungeonsAround(w *World, n int) []*Dungeon {
	var out []*Dungeon
	for ry := -n; ry <= n; ry++ {
		for rx := -n; rx <= n; rx++ {
			if d := w.DungeonOfRegion(RegionPos{rx, ry}); d != nil {
				out = append(out, d)
			}
		}
	}
	return out
}

func TestDungeonsExistAndAreDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	da, db := dungeonsAround(a, 8), dungeonsAround(b, 8)
	if len(da) < 10 {
		t.Fatalf("seulement %d donjons dans 289 régions", len(da))
	}
	if len(da) != len(db) {
		t.Fatalf("%d donjons contre %d pour la même seed", len(da), len(db))
	}
	for i := range da {
		x, y := *da[i], *db[i]
		if x != y {
			t.Errorf("donjon %d différent: %+v / %+v", i, x, y)
		}
	}
}

func TestDungeonEntrancesAreHiddenOnOpenGround(t *testing.T) {
	w := New(7)
	for _, d := range dungeonsAround(w, 6) {
		if RegionOf(d.Entrance.X, d.Entrance.Y) != d.Region {
			t.Errorf("%s: entrée hors de sa région", d.ID)
		}
		if w.StructureAt(d.Entrance.X, d.Entrance.Y) != nil || w.Safe(d.Entrance.X, d.Entrance.Y) {
			t.Errorf("%s: entrée dans un lieu", d.ID)
		}
		// Une route peut la croiser (les routes ne sont pas calculées pour placer un donjon: voir dungeonSpotOK).
		switch w.TileAt(d.Entrance.X, d.Entrance.Y) {
		case Plain, Forest, Hills, Sand, Road:
		default:
			t.Errorf("%s: entrée sur %v", d.ID, w.TileAt(d.Entrance.X, d.Entrance.Y))
		}
		if got := w.DungeonAt(d.Entrance.X, d.Entrance.Y); got != d {
			t.Errorf("%s: DungeonAt ne le retrouve pas", d.ID)
		}
		if got := w.DungeonByID(d.ID); got != d {
			t.Errorf("%s: DungeonByID ne le retrouve pas", d.ID)
		}
	}
}

func TestDungeonNamesAreUniqueNearby(t *testing.T) {
	for _, seed := range []int64{1, 42} {
		w := New(seed)
		all := dungeonsAround(w, 9)
		for i, a := range all {
			for _, b := range all[i+1:] {
				near := abs(a.Region.X-b.Region.X) <= dungeonClash && abs(a.Region.Y-b.Region.Y) <= dungeonClash
				if near && a.Name == b.Name {
					t.Errorf("seed %d: %s et %s s'appellent tous deux %q", seed, a.ID, b.ID, a.Name)
				}
			}
			if a.Name == "" || a.NameIn(i18n.EN) == "" || a.NameIn(i18n.EN) == a.Name {
				t.Errorf("seed %d: nom %q / %q", seed, a.Name, a.NameIn(i18n.EN))
			}
		}
	}
}

func TestDungeonTierGrowsWithDistance(t *testing.T) {
	if dungeonTier(RegionPos{0, 0}) != 1 || dungeonTier(RegionPos{100, 0}) != dungeonMaxTier {
		t.Errorf("paliers: %d, %d", dungeonTier(RegionPos{0, 0}), dungeonTier(RegionPos{100, 0}))
	}
}

func TestDungeonMapIsConnectedAndPopulated(t *testing.T) {
	for tier := 1; tier <= dungeonMaxTier; tier++ {
		for seed := int64(1); seed <= 60; seed++ {
			d := &Dungeon{ID: "t", Seed: seed * 7919, Tier: tier}
			m := NewDungeonMap(d)
			reach := m.Reachable(m.Spawn)
			if !reach[m.Exit] || !reach[m.Boss] {
				t.Fatalf("palier %d seed %d: sortie ou boss inaccessible", tier, d.Seed)
			}
			if m.TileAt(m.Exit.X, m.Exit.Y) != Stairs {
				t.Errorf("seed %d: pas d'escalier à la sortie", d.Seed)
			}
			bosses, elites := 0, 0
			seen := map[Pos]bool{m.Spawn: true, m.Exit: true}
			for _, s := range m.Slots {
				if !reach[s.Pos] {
					t.Errorf("seed %d: monstre inaccessible en %v", d.Seed, s.Pos)
				}
				if seen[s.Pos] {
					t.Errorf("seed %d: deux occupants en %v", d.Seed, s.Pos)
				}
				seen[s.Pos] = true
				switch s.Role {
				case RoleBoss:
					bosses++
					if s.Pos != m.Boss {
						t.Errorf("seed %d: boss hors de sa salle", d.Seed)
					}
				case RoleElite:
					elites++
				}
			}
			if bosses != 1 || elites < 1 {
				t.Errorf("seed %d: %d boss, %d élites", d.Seed, bosses, elites)
			}
			if len(m.Chests) < 1 {
				t.Errorf("seed %d: aucun coffre", d.Seed)
			}
			for _, c := range m.Chests {
				if !reach[c] || m.TileAt(c.X, c.Y) != Chest {
					t.Errorf("seed %d: coffre invalide en %v", d.Seed, c)
				}
				if seen[c] {
					t.Errorf("seed %d: coffre sur un occupant en %v", d.Seed, c)
				}
			}
			// Le spawn est dans la première salle, loin du boss, et sans monstre autour.
			for _, s := range m.Slots {
				if abs(s.Pos.X-m.Spawn.X)+abs(s.Pos.Y-m.Spawn.Y) < 3 {
					t.Errorf("seed %d: monstre collé au point d'arrivée", d.Seed)
				}
			}
		}
	}
}

func TestDungeonMapIsStable(t *testing.T) {
	d := &Dungeon{Seed: 123456789, Tier: 3}
	a, b := NewDungeonMap(d), NewDungeonMap(d)
	for y := range a.H {
		for x := range a.W {
			if a.TileAt(x, y) != b.TileAt(x, y) {
				t.Fatalf("tuile différente en %d,%d", x, y)
			}
		}
	}
	if a.TileAt(-1, 0) != Rock || a.TileAt(0, a.H) != Rock {
		t.Error("hors de la carte, c'est de la roche")
	}
}

// Ajouter les donjons ne doit pas changer les plans existants (voir TestExistingLayoutsAreFrozen):
// l'existence d'un donjon ne consomme aucun tirage du flux des lieux.
func TestDungeonsDoNotMoveStructures(t *testing.T) {
	w := New(42)
	before := layoutFingerprint(42)
	dungeonsAround(w, 6)
	if layoutFingerprint(42) != before {
		t.Error("les donjons ont modifié les plans")
	}
}

func TestDungeonsWithinFindsOnlyCloseEntrances(t *testing.T) {
	w := New(42)
	for _, d := range dungeonsAround(w, 5) {
		for _, r := range []int{0, 3, 20} {
			found := false
			for _, f := range w.DungeonsWithin(d.Entrance.X-r, d.Entrance.Y, r) {
				found = found || f == d
			}
			if !found {
				t.Fatalf("%s: l'entrée à %d cases n'est pas trouvée", d.ID, r)
			}
		}
		for _, f := range w.DungeonsWithin(d.Entrance.X-4, d.Entrance.Y, 3) {
			if f == d {
				t.Fatalf("%s: trouvé à 4 cases avec un rayon de 3", d.ID)
			}
		}
	}
}

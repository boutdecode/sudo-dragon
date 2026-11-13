package world

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// dungeonsAround lists the dungeons of the regions from -n to n.
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
	a, b := newTestWorld(42), newTestWorld(42)
	da, db := dungeonsAround(a, 8), dungeonsAround(b, 8)
	if len(da) < 10 {
		t.Fatalf("only %d dungeons in 289 regions", len(da))
	}
	if len(da) != len(db) {
		t.Fatalf("%d dungeons against %d for the same seed", len(da), len(db))
	}
	for i := range da {
		x, y := *da[i], *db[i]
		if x != y {
			t.Errorf("dungeon %d differs: %+v / %+v", i, x, y)
		}
	}
}

func TestDungeonEntrancesAreHiddenOnOpenGround(t *testing.T) {
	w := newTestWorld(7)
	for _, d := range dungeonsAround(w, 6) {
		if RegionOf(d.Entrance.X, d.Entrance.Y) != d.Region {
			t.Errorf("%s: entrance outside its region", d.ID)
		}
		if w.StructureAt(d.Entrance.X, d.Entrance.Y) != nil || w.Safe(d.Entrance.X, d.Entrance.Y) {
			t.Errorf("%s: entrance inside a place", d.ID)
		}
		// A road may cross it (roads are not computed to place a dungeon: see dungeonSpotOK).
		if t0 := w.TileAt(d.Entrance.X, d.Entrance.Y); !t0.ordinary() && t0 != Road {
			t.Errorf("%s: entrance on %v", d.ID, t0)
		}
		if got := w.DungeonAt(d.Entrance.X, d.Entrance.Y); got != d {
			t.Errorf("%s: DungeonAt does not find it", d.ID)
		}
		if got := w.DungeonByID(d.ID); got != d {
			t.Errorf("%s: DungeonByID does not find it", d.ID)
		}
	}
}

func TestDungeonNamesAreUniqueNearby(t *testing.T) {
	for _, seed := range []int64{1, 42} {
		w := newTestWorld(seed)
		all := dungeonsAround(w, 9)
		for i, a := range all {
			for _, b := range all[i+1:] {
				near := abs(a.Region.X-b.Region.X) <= dungeonClash && abs(a.Region.Y-b.Region.Y) <= dungeonClash
				if near && a.Name == b.Name {
					t.Errorf("seed %d: %s and %s are both called %q", seed, a.ID, b.ID, a.Name)
				}
			}
			if a.Name == "" || a.NameIn(i18n.EN) == "" || a.NameIn(i18n.EN) == a.Name {
				t.Errorf("seed %d: name %q / %q", seed, a.Name, a.NameIn(i18n.EN))
			}
		}
	}
}

func TestDungeonTierGrowsWithDistance(t *testing.T) {
	if dungeonTier(RegionPos{0, 0}) != 1 || dungeonTier(RegionPos{100, 0}) != dungeonMaxTier {
		t.Errorf("tiers: %d, %d", dungeonTier(RegionPos{0, 0}), dungeonTier(RegionPos{100, 0}))
	}
}

func TestDungeonMapIsConnectedAndPopulated(t *testing.T) {
	for tier := 1; tier <= dungeonMaxTier; tier++ {
		for seed := int64(1); seed <= 60; seed++ {
			d := &Dungeon{ID: "t", Seed: seed * 7919, Tier: tier}
			m := NewDungeonMap(d)
			reach := m.Reachable(m.Spawn)
			if !reach[m.Exit] || !reach[m.Boss] {
				t.Fatalf("tier %d seed %d: exit or boss unreachable", tier, d.Seed)
			}
			if m.TileAt(m.Exit.X, m.Exit.Y) != Stairs {
				t.Errorf("seed %d: no staircase at the exit", d.Seed)
			}
			bosses, elites := 0, 0
			seen := map[Pos]bool{m.Spawn: true, m.Exit: true}
			for _, s := range m.Slots {
				if !reach[s.Pos] {
					t.Errorf("seed %d: unreachable monster at %v", d.Seed, s.Pos)
				}
				if seen[s.Pos] {
					t.Errorf("seed %d: two occupants at %v", d.Seed, s.Pos)
				}
				seen[s.Pos] = true
				switch s.Role {
				case RoleBoss:
					bosses++
					if s.Pos != m.Boss {
						t.Errorf("seed %d: boss outside its room", d.Seed)
					}
				case RoleElite:
					elites++
				}
			}
			if bosses != 1 || elites < 1 {
				t.Errorf("seed %d: %d bosses, %d elites", d.Seed, bosses, elites)
			}
			if len(m.Chests) < 1 {
				t.Errorf("seed %d: no chest", d.Seed)
			}
			for _, c := range m.Chests {
				if !reach[c] || m.TileAt(c.X, c.Y) != Chest {
					t.Errorf("seed %d: invalid chest at %v", d.Seed, c)
				}
				if seen[c] {
					t.Errorf("seed %d: chest on an occupant at %v", d.Seed, c)
				}
			}
			// The spawn is in the first room, far from the boss, and with no monster around.
			for _, s := range m.Slots {
				if abs(s.Pos.X-m.Spawn.X)+abs(s.Pos.Y-m.Spawn.Y) < 3 {
					t.Errorf("seed %d: monster stuck to the arrival point", d.Seed)
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
				t.Fatalf("different tile at %d,%d", x, y)
			}
		}
	}
	if a.TileAt(-1, 0) != Rock || a.TileAt(0, a.H) != Rock {
		t.Error("off the map, it is rock")
	}
}

// Adding dungeons must not change the existing plans (see TestExistingLayoutsAreFrozen):
// the existence of a dungeon consumes no draw from the places' stream.
func TestDungeonsDoNotMoveStructures(t *testing.T) {
	w := newTestWorld(42)
	before := layoutFingerprint(42, testGen)
	dungeonsAround(w, 6)
	if layoutFingerprint(42, testGen) != before {
		t.Error("the dungeons changed the plans")
	}
}

func TestDungeonsWithinFindsOnlyCloseEntrances(t *testing.T) {
	w := newTestWorld(42)
	for _, d := range dungeonsAround(w, 5) {
		for _, r := range []int{0, 3, 20} {
			found := false
			for _, f := range w.DungeonsWithin(d.Entrance.X-r, d.Entrance.Y, r) {
				found = found || f == d
			}
			if !found {
				t.Fatalf("%s: the entrance at %d tiles is not found", d.ID, r)
			}
		}
		for _, f := range w.DungeonsWithin(d.Entrance.X-4, d.Entrance.Y, 3) {
			if f == d {
				t.Fatalf("%s: found at 4 tiles with a radius of 3", d.ID)
			}
		}
	}
}

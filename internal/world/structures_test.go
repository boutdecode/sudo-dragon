package world

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

var testSeeds = []int64{1, 2, 7, 42, 1234}

// sharedWorld returns a world shared between the read-only tests: the
// regions already generated are not generated a second time.
var (
	sharedMu     sync.Mutex
	sharedWorlds = map[int64]*World{}
)

func sharedWorld(seed int64) *World {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if w, ok := sharedWorlds[seed]; ok {
		return w
	}
	w := New(seed)
	sharedWorlds[seed] = w
	return w
}

// allStructures returns the structures of the regions [-n, n]² of a world.
func allStructures(w *World, n int) []*Structure {
	var out []*Structure
	for ry := -n; ry <= n; ry++ {
		for rx := -n; rx <= n; rx++ {
			if s := w.structureForRegion(RegionPos{rx, ry}); s != nil {
				out = append(out, s)
			}
		}
	}
	return out
}

func sameStructure(a, b *Structure) bool {
	return a.Kind == b.Kind && a.Origin == b.Origin && a.W == b.W && a.H == b.H &&
		reflect.DeepEqual(a.tiles, b.tiles) && reflect.DeepEqual(a.NPCs, b.NPCs) && reflect.DeepEqual(a.Slots, b.Slots)
}

func TestStructuresAreDeterministic(t *testing.T) {
	t.Parallel()
	for _, seed := range testSeeds {
		a, b := New(seed), New(seed)
		for ry := -3; ry <= 3; ry++ {
			for rx := -3; rx <= 3; rx++ {
				sa, sb := a.structureForRegion(RegionPos{rx, ry}), b.structureForRegion(RegionPos{rx, ry})
				if (sa == nil) != (sb == nil) || (sa != nil && !sameStructure(sa, sb)) {
					t.Fatalf("seed %d region (%d,%d): two identical worlds differ", seed, rx, ry)
				}
			}
		}
	}
	// Another seed gives another world.
	a, b := allStructures(New(1), 4), allStructures(New(2), 4)
	if len(a) == len(b) {
		same := true
		for i := range a {
			same = same && sameStructure(a[i], b[i])
		}
		if same {
			t.Fatal("two different seeds give the same structures")
		}
	}
}

// The cache must not change the result: emptied, it rebuilds identically.
func TestStructureCacheRebuildsIdentically(t *testing.T) {
	t.Parallel()
	w := New(7)
	before := allStructures(w, 3)
	w.smu.Lock()
	clear(w.structs)
	w.smu.Unlock()
	after := allStructures(w, 3)
	if len(before) != len(after) {
		t.Fatalf("%d structures then %d after emptying the cache", len(before), len(after))
	}
	for i := range before {
		if !sameStructure(before[i], after[i]) {
			t.Fatalf("structure %d different after rebuilding", i)
		}
	}
}

func TestStructuresStayInsideTheirRegion(t *testing.T) {
	t.Parallel()
	for _, seed := range testSeeds {
		w := sharedWorld(seed)
		for _, s := range allStructures(w, 5) {
			lx, ly := s.Origin.X-s.Region.X*RegionSize, s.Origin.Y-s.Region.Y*RegionSize
			if lx < regionMargin || ly < regionMargin || lx+s.W > RegionSize-regionMargin || ly+s.H > RegionSize-regionMargin {
				t.Fatalf("seed %d: %v at (%d,%d) %dx%d leaves its region or its margin", seed, s.Kind, lx, ly, s.W, s.H)
			}
			if RegionOf(s.Origin.X, s.Origin.Y) != s.Region || RegionOf(s.Origin.X+s.W-1, s.Origin.Y+s.H-1) != s.Region {
				t.Fatalf("seed %d: the footprint straddles two regions", seed)
			}
		}
	}
}

func TestSafeMarginStaysInsideRegion(t *testing.T) {
	t.Parallel()
	if safeMargin >= regionMargin {
		t.Fatalf("safeMargin (%d) must stay below regionMargin (%d)", safeMargin, regionMargin)
	}
}

func TestStructureSizesMatchLayouts(t *testing.T) {
	t.Parallel()
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			want := kindSize[s.Kind]
			if s.W != want[0] || s.H != want[1] || len(s.tiles) != s.W*s.H {
				t.Fatalf("seed %d: %v of %dx%d (%d tiles), want %v", seed, s.Kind, s.W, s.H, len(s.tiles), want)
			}
		}
	}
}

// reachable computes, by 4-neighbour breadth-first traversal from the corner of
// the footprint (always clear terrain), the reachable walkable tiles.
// NPCs count as obstacles: they occupy their tile.
func reachable(s *Structure) map[Pos]bool {
	blocked := map[Pos]bool{}
	for _, n := range s.NPCs {
		blocked[n.Pos] = true
	}
	start := s.Origin
	seen := map[Pos]bool{start: true}
	queue := []Pos{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := Pos{p.X + d[0], p.Y + d[1]}
			if seen[q] || !s.Contains(q.X, q.Y) || blocked[q] || !s.TileAt(q.X, q.Y).Walkable() {
				continue
			}
			seen[q] = true
			queue = append(queue, q)
		}
	}
	return seen
}

func TestEveryOpenTileIsReachableFromOutside(t *testing.T) {
	t.Parallel()
	total := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			total++
			seen := reachable(s)
			if !s.TileAt(s.Origin.X, s.Origin.Y).Walkable() {
				t.Fatalf("seed %d %v: the corner of the footprint is not clear", seed, s.Kind)
			}
			blocked := map[Pos]bool{}
			for _, n := range s.NPCs {
				blocked[n.Pos] = true
			}
			for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
				for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
					p := Pos{x, y}
					tile := s.TileAt(x, y)
					if !tile.Walkable() || blocked[p] {
						continue
					}
					if !seen[p] {
						t.Fatalf("seed %d %v: %v (tile %d) unreachable from outside (at %v)", seed, s.Kind, p, tile, s.Origin)
					}
				}
			}
		}
	}
	if total < 20 {
		t.Fatalf("only %d structures tested: the sample is too poor", total)
	}
}

func TestNPCsStandOnFloorAndNeverBlockEntrance(t *testing.T) {
	t.Parallel()
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			seen := reachable(s) // NPCs are obstacles in this traversal
			for _, n := range s.NPCs {
				// Villagers may stand on the square (road); the other NPCs stay inside.
				if tile := s.TileAt(n.Pos.X, n.Pos.Y); tile != Floor && !(n.Kind != NPCMerchant && n.Kind != NPCHealer && tile == Road) {
					t.Fatalf("seed %d %v: NPC %s off an interior floor (tile %d)", seed, s.Kind, n.Kind, tile)
				}
				// At least one reachable neighbour, otherwise the player cannot talk to it.
				ok := false
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if seen[Pos{n.Pos.X + d[0], n.Pos.Y + d[1]}] {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("seed %d %v: nobody can reach NPC %s at %v", seed, s.Kind, n.Kind, n.Pos)
				}
			}
		}
	}
}

func TestNPCsPerKind(t *testing.T) {
	t.Parallel()
	kinds := map[Kind]int{}
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			kinds[s.Kind]++
			counts := map[NPCKind]int{}
			for _, n := range s.NPCs {
				counts[n.Kind]++
			}
			switch s.Kind {
			case KindVillage:
				if counts[NPCHealer] != 1 || counts[NPCVillager] != 3 || counts[NPCChief] != 1 || counts[NPCSmith]+counts[NPCArmorer] != 1 || len(s.NPCs) != 6 {
					t.Fatalf("village: NPCs %v, want a healer, 3 villagers, a chief and a single equipment seller", counts)
				}
				if k := s.NPCs[len(s.NPCs)-1].Kind; k != NPCSmith && k != NPCArmorer || s.NPCs[len(s.NPCs)-2].Kind != NPCChief {
					t.Fatalf("village: the equipment seller must follow the chief, last: %v", s.NPCs)
				}
			case KindTown:
				if counts[NPCHealer] != 1 || counts[NPCMerchant] != 1 || counts[NPCVillager] != 5 || counts[NPCMayor] != 1 || counts[NPCSmith] != 1 || counts[NPCArmorer] != 1 || len(s.NPCs) != 10 {
					t.Fatalf("town: NPCs %v, want merchant, healer, 5 villagers, mayor, blacksmith and armourer", counts)
				}
				if s.NPCs[len(s.NPCs)-3].Kind != NPCMayor || s.NPCs[len(s.NPCs)-2].Kind != NPCSmith || s.NPCs[len(s.NPCs)-1].Kind != NPCArmorer {
					t.Fatalf("town: order of the last NPCs: %v", s.NPCs)
				}
				if s.NPCs[0].Kind != NPCMerchant || s.NPCs[1].Kind != NPCHealer {
					t.Fatalf("town: the indices of the original NPCs moved: %v", s.NPCs)
				}
			case KindFortress:
				if len(s.NPCs) != 0 {
					t.Fatalf("fortress with NPCs: %v", counts)
				}
			}
		}
	}
	for _, k := range []Kind{KindVillage, KindTown, KindFortress} {
		if kinds[k] == 0 {
			t.Errorf("no structure of type %v in the sample", k)
		}
	}
}

// The original NPCs keep their index (the memories in the database are attached to it); the villagers follow.
func TestVillagersComeAfterOriginalNPCsAndAreDistinct(t *testing.T) {
	t.Parallel()
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			seenPos, seenName := map[Pos]bool{}, map[string]bool{}
			sawVillager := false
			for i, n := range s.NPCs {
				if n.Index != i {
					t.Fatalf("index %d at rank %d", n.Index, i)
				}
				if n.Kind == NPCVillager {
					sawVillager = true
				} else if sawVillager && n.Kind != NPCChief && n.Kind != NPCMayor && n.Kind != NPCSmith && n.Kind != NPCArmorer {
					t.Fatalf("NPC %s after a villager", n.Kind)
				}
				if seenPos[n.Pos] || seenName[n.Name] {
					t.Fatalf("duplicate position or name: %v %q", n.Pos, n.Name)
				}
				seenPos[n.Pos], seenName[n.Name] = true, true
				if !s.Contains(n.Pos.X, n.Pos.Y) {
					t.Fatalf("NPC outside the footprint: %v", n.Pos)
				}
			}
		}
	}
}

func TestFortressGarrison(t *testing.T) {
	t.Parallel()
	found := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			if s.Kind != KindFortress {
				continue
			}
			found++
			roles := map[SlotRole]int{}
			seenPos := map[Pos]bool{}
			reach := reachable(s)
			for _, sl := range s.Slots {
				roles[sl.Role]++
				if seenPos[sl.Pos] {
					t.Fatalf("two slots on %v", sl.Pos)
				}
				seenPos[sl.Pos] = true
				if s.TileAt(sl.Pos.X, sl.Pos.Y) != Floor {
					t.Fatalf("slot %v off an interior floor", sl.Pos)
				}
				if !reach[sl.Pos] {
					t.Fatalf("slot %v (role %d) unreachable: this monster could not be fought", sl.Pos, sl.Role)
				}
			}
			if roles[RoleBoss] != 1 || roles[RoleElite] != 2 || roles[RoleGuard] != 6 {
				t.Fatalf("garrison %v, want 1 lord, 2 elites, 6 soldiers", roles)
			}
		}
	}
	if found == 0 {
		t.Fatal("no fortress found in the sample")
	}
}

func TestOnlyFortressesHaveSlots(t *testing.T) {
	t.Parallel()
	for _, s := range allStructures(sharedWorld(3), 5) {
		if s.Kind != KindFortress && len(s.Slots) != 0 {
			t.Fatalf("%v with garrison slots", s.Kind)
		}
	}
}

func TestBuildingsHaveDoors(t *testing.T) {
	t.Parallel()
	min := map[Kind]int{KindVillage: 4, KindTown: 12 + 8, KindFortress: 12}
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			doors := 0
			for _, tile := range s.tiles {
				if tile == Door {
					doors++
				}
			}
			if doors < min[s.Kind] {
				t.Fatalf("seed %d %v: %d doors, at least %d expected", seed, s.Kind, doors, min[s.Kind])
			}
		}
	}
}

// The starting point must never be covered or cluttered by a structure.
func TestNoStructureNearSpawn(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 20; seed++ {
		w := sharedWorld(seed)
		sp := w.FindSpawn()
		if near := w.StructuresNear(sp, startClearance-1); len(near) != 0 {
			t.Fatalf("seed %d: %v within %d tiles of the starting point %v", seed, near[0].Kind, startClearance, sp)
		}
		// A road may pass over the starting point: it remains walkable.
		if tile := w.At(sp.X, sp.Y); tile != Plain && tile != Forest && tile != Road {
			t.Fatalf("seed %d: the starting point is on %d", seed, tile)
		}
	}
}

// A new player must find a village within a reasonable walk.
func TestStartVillageIsNear(t *testing.T) {
	t.Parallel()
	const seeds = 20
	near, villages := 0, 0
	for seed := int64(1); seed <= seeds; seed++ {
		w := sharedWorld(seed)
		sp := w.FindSpawn()
		list := w.StructuresNear(sp, 160)
		for _, s := range list {
			if s.Kind == KindVillage {
				villages++
				near++
				break
			}
		}
	}
	// Some worlds may start on an island or between mountains: we tolerate 10 %.
	if near < seeds*9/10 {
		t.Fatalf("only %d/%d worlds have a village within 160 tiles of the start", near, seeds)
	}
	_ = villages
}

func TestSafeZones(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	var village, town, fortress *Structure
	for _, s := range allStructures(w, 8) {
		switch {
		case s.Kind == KindVillage && village == nil:
			village = s
		case s.Kind == KindTown && town == nil:
			town = s
		case s.Kind == KindFortress && fortress == nil:
			fortress = s
		}
	}
	if village == nil || fortress == nil {
		t.Skip("sample without a village or without a fortress")
	}

	for _, s := range []*Structure{village, town} {
		if s == nil {
			continue
		}
		c := s.Center()
		if !w.Safe(c.X, c.Y) {
			t.Errorf("%v: the centre is not in the safe zone", s.Kind)
		}
		// Exact limit: safeMargin tiles around the footprint.
		if !w.Safe(s.Origin.X-safeMargin, c.Y) || w.Safe(s.Origin.X-safeMargin-1, c.Y) {
			t.Errorf("%v: the west edge of the safe zone is misplaced", s.Kind)
		}
		if !w.Safe(c.X, s.Origin.Y+s.H-1+safeMargin) || w.Safe(c.X, s.Origin.Y+s.H+safeMargin) {
			t.Errorf("%v: the south edge of the safe zone is misplaced", s.Kind)
		}
	}
	fc := fortress.Center()
	if w.Safe(fc.X, fc.Y) {
		t.Error("a fortress must not be a safe zone")
	}
	sp := w.FindSpawn()
	if w.Safe(sp.X, sp.Y) {
		t.Error("the starting point must not be in a safe zone")
	}
}

func TestStructuresNearSemantics(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	all := allStructures(w, 4)
	if len(all) == 0 {
		t.Fatal("no structure")
	}
	s := all[0]
	c := s.Center()

	if got := w.StructuresNear(c, 0); len(got) == 0 || got[0] != s {
		t.Fatalf("from its centre, the structure must be first (radius 0): %v", got)
	}
	// Just outside the footprint, radius too short: absent; long enough: present.
	out := Pos{s.Origin.X - 10, c.Y}
	for _, o := range w.StructuresNear(out, 9) {
		if o == s {
			t.Fatal("found at 10 tiles with a radius of 9")
		}
	}
	found := false
	for _, o := range w.StructuresNear(out, 10) {
		found = found || o == s
	}
	if !found {
		t.Fatal("not found at 10 tiles with a radius of 10")
	}
	// Sorting by proximity.
	list := w.StructuresNear(Pos{}, 600)
	for i := 1; i < len(list); i++ {
		a, b := list[i-1].Center(), list[i].Center()
		if abs(a.X)+abs(a.Y) > abs(b.X)+abs(b.Y) {
			t.Fatal("StructuresNear is not sorted from closest to farthest")
		}
	}
}

func TestNPCLookups(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	checked := 0
	for _, s := range allStructures(w, 5) {
		for _, n := range s.NPCs {
			checked++
			got, ok := w.NPCAt(n.Pos.X, n.Pos.Y)
			if !ok || got != n {
				t.Fatalf("NPCAt(%v) = %v, %v; want %v", n.Pos, got, ok, n)
			}
			if _, ok := w.NPCAt(n.Pos.X+1, n.Pos.Y); ok {
				t.Fatalf("NPCAt finds an NPC next to %v", n.Pos)
			}
			in := w.NPCsIn(n.Pos.X-3, n.Pos.Y-3, n.Pos.X+3, n.Pos.Y+3)
			has := false
			for _, o := range in {
				has = has || o == n
			}
			if !has {
				t.Fatalf("NPCsIn does not return the NPC at %v", n.Pos)
			}
			if far := w.NPCsIn(n.Pos.X+1, n.Pos.Y+1, n.Pos.X+5, n.Pos.Y+5); len(far) != 0 {
				for _, o := range far {
					if o == n {
						t.Fatalf("NPCsIn returns an NPC outside the rectangle")
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no NPC checked")
	}
	if _, ok := w.NPCAt(1_000_003, -777); ok {
		t.Fatal("NPC found in the wild")
	}
}

// Chunks (fast path) and TileAt (direct path) must give the same tiles.
func TestChunksAgreeWithTileAt(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	for _, s := range allStructures(w, 2) {
		for _, p := range []Pos{s.Origin, s.Center(), {s.Origin.X + s.W - 1, s.Origin.Y + s.H - 1}, {s.Origin.X - 2, s.Origin.Y - 2}} {
			if got, want := w.At(p.X, p.Y), w.TileAt(p.X, p.Y); got != want {
				t.Fatalf("%v: At=%d, TileAt=%d", p, got, want)
			}
		}
		// The whole footprint, tile by tile.
		for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
			for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
				if w.At(x, y) != s.TileAt(x, y) {
					t.Fatalf("(%d,%d): the chunk does not reflect the structure", x, y)
				}
			}
		}
	}
}

// Outside structures, the terrain is unchanged.
func TestTerrainOutsideStructuresIsUnchanged(t *testing.T) {
	t.Parallel()
	w := sharedWorld(1)
	for _, p := range []Pos{{-500, 300}, {0, 0}, {900, -40}, {123, 456}} {
		if w.StructureAt(p.X, p.Y) != nil {
			continue
		}
		if got := w.TileAt(p.X, p.Y); got != w.terrainAt(p.X, p.Y) && got != Road {
			t.Fatalf("%v: TileAt differs from the raw terrain outside structures (and is not a road)", p)
		}
	}
}

func TestStructureTilesAreStructureTiles(t *testing.T) {
	t.Parallel()
	for _, s := range allStructures(sharedWorld(1), 5) {
		for _, tile := range s.tiles {
			if tile >= NumTiles {
				t.Fatalf("tile %d outside the enumeration", tile)
			}
		}
	}
}

func TestStructureDistribution(t *testing.T) {
	t.Parallel()
	counts := map[Kind]int{}
	regions, withStruct := 0, 0
	for _, seed := range testSeeds {
		w := sharedWorld(seed)
		for ry := -8; ry < 8; ry++ {
			for rx := -8; rx < 8; rx++ {
				regions++
				if s := w.structureForRegion(RegionPos{rx, ry}); s != nil {
					withStruct++
					counts[s.Kind]++
				}
			}
		}
	}
	frac := float64(withStruct) / float64(regions)
	if frac < 0.20 || frac > 0.60 {
		t.Fatalf("%.0f%% of the regions have a structure (want between 20 and 60 %%)", frac*100)
	}
	if counts[KindVillage] <= counts[KindTown] || counts[KindVillage] <= counts[KindFortress]/2 {
		t.Fatalf("unexpected distribution: %v (villages should dominate, towns be rare)", counts)
	}
	t.Logf("%d regions, %.0f%% occupied: %v", regions, frac*100, counts)
}

func TestFirstChunkGenerationIsFast(t *testing.T) {
	if raceEnabled {
		t.Skip("timing assertion: meaningless under the race detector")
	}
	w := New(1)
	start := time.Now()
	for cy := -5; cy < 5; cy++ {
		for cx := -5; cx < 5; cx++ {
			w.Chunk(ChunkPos{cx, cy})
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("100 chunks in %v: the structure lookup slows down generation", d)
	}
}

func TestWallsBlockAndDoorsOpen(t *testing.T) {
	t.Parallel()
	if Wall.Walkable() {
		t.Fatal("a wall must block")
	}
	for _, tile := range []Tile{Door, Floor, Road} {
		if !tile.Walkable() {
			t.Fatalf("tile %d should be walkable", tile)
		}
	}
}

func BenchmarkChunkWithStructures(b *testing.B) {
	for i := 0; i < b.N; i++ {
		w := New(int64(i % 5))
		for cy := 0; cy < 8; cy++ {
			for cx := 0; cx < 8; cx++ {
				w.Chunk(ChunkPos{cx, cy})
			}
		}
	}
}

func TestEveryPlaceHasAQuestGiverAndOthersAreVillagers(t *testing.T) {
	t.Parallel()
	someOther := false
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			givers, villagers := 0, 0
			for _, n := range s.NPCs {
				if n.GivesQuests && n.Kind != NPCVillager {
					t.Fatalf("%s should not give quests", n.Kind)
				}
				if n.Kind == NPCVillager {
					villagers++
					if n.GivesQuests {
						givers++
					} else {
						someOther = true
					}
				}
			}
			if villagers > 0 && givers == 0 {
				t.Fatalf("seed %d %v : no quest giver among %d villagers", seed, s.Kind, villagers)
			}
		}
	}
	if !someOther {
		t.Error("all villagers give quests: the draw does not vary")
	}
}

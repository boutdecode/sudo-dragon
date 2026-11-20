package world

import (
	"testing"
)

var allPOIKinds = []POIKind{POICampfire, POIShrine, POILair, POITower, POIStone, POISpring, POIRuin}

// slotOffsets are the monster slots of a type, relative to its centre (see newPOI).
func slotOffsets(k POIKind) [][2]int {
	switch k {
	case POILair:
		return [][2]int{{-2, -1}, {2, -1}, {0, 2}}
	case POIRuin:
		return [][2]int{{0, 2}}
	}
	return nil
}

// reachableFromOutside floods the shape from its border (4 directions, the natural terrain around is walkable).
func reachableFromOutside(s *shape, tiles []Tile) map[[2]int]bool {
	n := 2*s.r + 1
	at := func(dx, dy int) Tile { return tiles[(dy+s.r)*n+dx+s.r] }
	seen := map[[2]int]bool{}
	var queue [][2]int
	for dy := -s.r; dy <= s.r; dy++ {
		for dx := -s.r; dx <= s.r; dx++ {
			if max(abs(dx), abs(dy)) == s.r && at(dx, dy).Walkable() {
				seen[[2]int{dx, dy}] = true
				queue = append(queue, [2]int{dx, dy})
			}
		}
	}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := [2]int{c[0] + d[0], c[1] + d[1]}
			if abs(q[0]) > s.r || abs(q[1]) > s.r || seen[q] || !at(q[0], q[1]).Walkable() {
				continue
			}
			seen[q] = true
			queue = append(queue, q)
		}
	}
	return seen
}

// Whatever the climate, the seed and the side of the door, the interaction tile and the monster slots are walkable
// and reachable from outside.
func TestShapesAreAccessibleOnEverySide(t *testing.T) {
	t.Parallel()
	for _, k := range allPOIKinds {
		for cl := Climate(0); cl < NumClimates; cl++ {
			for seed := int64(0); seed < 40; seed++ {
				base := buildShape(k, cl, seed*7919+13)
				if base.r > siteRadius(k) {
					t.Fatalf("kind %d climate %d: radius %d exceeds the site radius %d", k, cl, base.r, siteRadius(k))
				}
				if got := base.at(0, 0); got != poiTile(k) {
					t.Fatalf("kind %d climate %d: centre %d, want %d", k, cl, got, poiTile(k))
				}
				for side := range numSides {
					tiles := append([]Tile(nil), base.tiles...)
					for _, g := range base.gates[side] {
						tiles[base.idx(g.dx, g.dy)] = g.t
					}
					reach := reachableFromOutside(base, tiles)
					if !reach[[2]int{0, 0}] {
						t.Fatalf("kind %d climate %d seed %d side %d: centre unreachable", k, cl, seed, side)
					}
					for _, o := range slotOffsets(k) {
						if !reach[o] {
							t.Fatalf("kind %d climate %d seed %d side %d: slot %v unreachable", k, cl, seed, side, o)
						}
					}
				}
			}
		}
	}
}

func TestShapesAreDeterministic(t *testing.T) {
	t.Parallel()
	for _, k := range allPOIKinds {
		for cl := Climate(0); cl < NumClimates; cl++ {
			a, b := buildShape(k, cl, 99), buildShape(k, cl, 99)
			if a.r != b.r || string(tilesBytes(a.tiles)) != string(tilesBytes(b.tiles)) {
				t.Fatalf("kind %d climate %d: two builds differ", k, cl)
			}
		}
	}
}

func tilesBytes(l []Tile) []byte {
	out := make([]byte, len(l))
	for i, t := range l {
		out[i] = byte(t)
	}
	return out
}

func count(s *shape, t Tile) int {
	n := 0
	for _, v := range s.tiles {
		if v == t {
			n++
		}
	}
	return n
}

// The shapes look like what they are: a cross, a round or square tower, a half collapsed ruin, a stone circle.
func TestShapesLookLikeTheirBuilding(t *testing.T) {
	t.Parallel()

	// Church: Greek cross, the four arms alike, the corners of the square outside the walls.
	cross := buildShape(POIShrine, Temperate, 1)
	if cross.r != 5 {
		t.Errorf("church radius %d, want 5", cross.r)
	}
	for _, c := range [][2]int{{4, 4}, {-4, 4}, {4, -4}, {-4, -4}} {
		if cross.at(c[0], c[1]) != tileNone {
			t.Errorf("cross corner %v is built: %d", c, cross.at(c[0], c[1]))
		}
	}
	for _, c := range [][2]int{{4, 0}, {-4, 0}, {0, 4}, {0, -4}, {1, 1}, {0, 0}} {
		if !cross.at(c[0], c[1]).Walkable() {
			t.Errorf("cross: %v should be a floor", c)
		}
	}
	if cross.at(2, 2) != Wall {
		t.Errorf("the inner corner of the cross must be a wall, got %d", cross.at(2, 2))
	}
	for side := range numSides {
		if len(cross.gates[side]) != 1 || cross.gates[side][0].t != Door {
			t.Errorf("cross: side %d has no door", side)
		}
	}
	// Temple of the tropics: columns around an open platform.
	if got := count(buildShape(POIShrine, Tropical, 1), Pillar); got != 16 {
		t.Errorf("open temple with %d columns, want 16", got)
	}

	// Towers: square (temperate), disc (arid), on stilts (tropical).
	sq := buildShape(POITower, Temperate, 1)
	if sq.r != 2 || count(sq, Wall) != 16 || sq.at(2, 2) != Wall {
		t.Errorf("square tower: r=%d walls=%d", sq.r, count(sq, Wall))
	}
	round := buildShape(POITower, Arid, 1)
	if round.at(3, 3) != tileNone || round.at(2, 2) != Wall || round.at(0, 3) != Wall || round.at(2, 1) != Floor {
		t.Error("round stone tower: not a disc")
	}
	if got := count(buildShape(POITower, Tropical, 1), Pillar); got != 8 {
		t.Errorf("tower on stilts with %d pillars, want 8", got)
	}

	// Ruins: standing walls and breaches, whatever the seed.
	for seed := int64(0); seed < 30; seed++ {
		r := buildShape(POIRuin, Temperate, seed)
		walls, rubble := count(r, Wall), count(r, Rubble)
		if walls < 4 || walls >= 24 || rubble < 5 {
			t.Fatalf("seed %d: ruin with %d walls and %d rubble tiles is not half collapsed", seed, walls, rubble)
		}
	}
	if count(buildShape(POIRuin, Tropical, 5), Jungle) == 0 || count(buildShape(POIRuin, Arid, 5), Sand) == 0 || count(buildShape(POIRuin, Boreal, 5), Snow) == 0 {
		t.Error("ruins must be taken over by the climate's vegetation")
	}

	// Standing stones: cromlech (ring with four openings) or dolmen.
	if got := count(buildShape(POIStone, Temperate, 1), Pillar); got != 12 {
		t.Errorf("cromlech with %d stones, want 12", got)
	}
	if got := count(buildShape(POIStone, Arid, 1), Pillar); got != 3 {
		t.Errorf("dolmen with %d stones, want 3", got)
	}

	// Lairs: palisade or cave of rock.
	if count(buildShape(POILair, Temperate, 1), Wall) == 0 || count(buildShape(POILair, Temperate, 1), Mountain) != 0 {
		t.Error("lair: palisade expected in the temperate climate")
	}
	if count(buildShape(POILair, Boreal, 1), Mountain) == 0 || count(buildShape(POILair, Boreal, 1), Snow) == 0 {
		t.Error("lair: snowy cave expected in the cold climate")
	}
	// Spring: a pool; oasis with trees in the arid climate.
	if count(buildShape(POISpring, Temperate, 1), Water) != 4 || count(buildShape(POISpring, Arid, 1), Forest) == 0 {
		t.Error("spring: pool and oasis expected")
	}
	// Campfire: a tent whose door opens on the fire.
	for seed := int64(0); seed < 20; seed++ {
		c := buildShape(POICampfire, Temperate, seed)
		doors := 0
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				if c.at(dx, dy) == Door {
					doors++
					if abs(dx)+abs(dy) != 1 {
						t.Fatalf("seed %d: the tent door %v is not next to the fire", seed, [2]int{dx, dy})
					}
				}
			}
		}
		if doors != 1 {
			t.Fatalf("seed %d: %d tent doors", seed, doors)
		}
	}
}

// The footprint of every point stays in its region, away from places and dungeon entrances, on natural terrain; the
// map agrees with the building tile by tile; no road touches it.
func TestPOIBuildingsInTheWorld(t *testing.T) {
	t.Parallel()
	for _, seed := range []int64{7, 42} {
		w, again := New(seed), New(seed)
		kinds := map[POIKind]bool{}
		for ry := -4; ry <= 4; ry++ {
			for rx := -4; rx <= 4; rx++ {
				rp := RegionPos{rx, ry}
				for _, p := range w.POIsOfRegion(rp) {
					kinds[p.Kind] = true
					b := p.Bounds
					if RegionOf(b.X, b.Y) != rp || RegionOf(b.X+b.W-1, b.Y+b.H-1) != rp {
						t.Fatalf("seed %d %s: footprint %v leaves region %v", seed, p.ID(), b, rp)
					}
					if b.W != b.H || !b.Contains(p.Pos.X, p.Pos.Y) || b.X+b.W/2 != p.Pos.X || b.Y+b.H/2 != p.Pos.Y {
						t.Fatalf("seed %d %s: %v is not centred on %v", seed, p.ID(), b, p.Pos)
					}
					q := again.POIsOfRegion(rp)
					var twin *POI
					for _, o := range q {
						if o.ID() == p.ID() {
							twin = o
						}
					}
					if twin == nil || twin.Bounds != p.Bounds || twin.Variant != p.Variant {
						t.Fatalf("seed %d %s: not deterministic", seed, p.ID())
					}
					for y := b.Y - 1; y < b.Y+b.H+1; y++ {
						for x := b.X - 1; x < b.X+b.W+1; x++ {
							if w.StructureAt(x, y) != nil {
								t.Fatalf("seed %d %s: footprint on a place at (%d,%d)", seed, p.ID(), x, y)
							}
							if w.TileAt(x, y) == Road || w.At(x, y) == Road {
								t.Fatalf("seed %d %s: road at (%d,%d) next to the building", seed, p.ID(), x, y)
							}
							if b.Contains(x, y) {
								tt := p.TileAt(w, x, y)
								want := tt
								if tt == tileNone {
									want = w.Terrain(x, y)
								}
								if got := w.TileAt(x, y); got != want {
									t.Fatalf("seed %d %s: TileAt(%d,%d)=%d, want %d", seed, p.ID(), x, y, got, want)
								}
								if got := w.At(x, y); got != want {
									t.Fatalf("seed %d %s: At(%d,%d)=%d, want %d", seed, p.ID(), x, y, got, want)
								}
								if again.TileAt(x, y) != w.TileAt(x, y) {
									t.Fatalf("seed %d %s: door differs between two worlds at (%d,%d)", seed, p.ID(), x, y)
								}
								if !w.Terrain(x, y).ordinary() {
									t.Fatalf("seed %d %s: building on non-ordinary terrain at (%d,%d)", seed, p.ID(), x, y)
								}
								if (x != p.Pos.X || y != p.Pos.Y) && w.POIAt(x, y) != nil {
									t.Fatalf("seed %d %s: POIAt finds the point away from its tile", seed, p.ID())
								}
							}
						}
					}
					if d := w.DungeonOfRegion(rp); d != nil && b.Contains(d.Entrance.X, d.Entrance.Y) {
						t.Fatalf("seed %d %s: building on a dungeon entrance", seed, p.ID())
					}
					if got := w.TileAt(p.Pos.X, p.Pos.Y); got != p.Tile() {
						t.Fatalf("seed %d %s: centre %d, want %d", seed, p.ID(), got, p.Tile())
					}
					for _, s := range p.Slots {
						if !w.TileAt(s.X, s.Y).Walkable() || !b.Contains(s.X, s.Y) {
							t.Fatalf("seed %d %s: slot %v impassable or outside", seed, p.ID(), s)
						}
					}
				}
			}
		}
		for _, k := range allPOIKinds {
			if !kinds[k] {
				t.Errorf("seed %d: no point of type %d", seed, k)
			}
		}
	}
}

// With the real map, the points stay accessible: flood fill from the edge of the footprint.
func TestPOIsAreAccessibleInTheWorld(t *testing.T) {
	t.Parallel()
	w := New(7)
	checked := 0
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			for _, p := range w.POIsOfRegion(RegionPos{rx, ry}) {
				b := p.Bounds
				seen := map[Pos]bool{}
				var queue []Pos
				for y := b.Y; y < b.Y+b.H; y++ {
					for x := b.X; x < b.X+b.W; x++ {
						if (x == b.X || y == b.Y || x == b.X+b.W-1 || y == b.Y+b.H-1) && w.TileAt(x, y).Walkable() {
							seen[Pos{x, y}] = true
							queue = append(queue, Pos{x, y})
						}
					}
				}
				for len(queue) > 0 {
					c := queue[0]
					queue = queue[1:]
					for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						q := Pos{c.X + d[0], c.Y + d[1]}
						if !b.Contains(q.X, q.Y) || seen[q] || !w.TileAt(q.X, q.Y).Walkable() {
							continue
						}
						seen[q] = true
						queue = append(queue, q)
					}
				}
				if !seen[p.Pos] {
					t.Errorf("%s: interaction tile %v unreachable", p.ID(), p.Pos)
				}
				for _, s := range p.Slots {
					if !seen[s] {
						t.Errorf("%s: slot %v unreachable", p.ID(), s)
					}
				}
				checked++
			}
		}
	}
	if checked < 80 {
		t.Errorf("only %d points checked", checked)
	}
}

// The door faces the nearest road when there is one within range.
func TestPOIDoorFacesTheNearestRoad(t *testing.T) {
	t.Parallel()
	w := New(7)
	checked := 0
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			for _, p := range w.POIsOfRegion(RegionPos{rx, ry}) {
				if len(p.gates[sideSouth]) == 0 || p.gates[sideSouth][0].t == Floor && p.Kind != POILair {
					continue
				}
				// Independent search of the nearest road tile.
				best, bestD := Pos{}, poiDoorRange+1
				for _, l := range w.roadsAround(p.Region) {
					for tl := range l.tiles {
						d := abs(tl.X-p.Pos.X) + abs(tl.Y-p.Pos.Y)
						if d < bestD || (d == bestD && (tl.Y < best.Y || (tl.Y == best.Y && tl.X < best.X))) {
							best, bestD = tl, d
						}
					}
				}
				if bestD > poiDoorRange {
					continue
				}
				want := sideToward(p.Pos, best.X, best.Y)
				for side := range numSides {
					g := p.gates[side][0]
					got := w.TileAt(p.Pos.X+g.dx, p.Pos.Y+g.dy)
					if side == want && got != g.t {
						t.Errorf("%s: the door should face the road (side %d), tile %d", p.ID(), want, got)
					}
					if side != want && got == g.t && g.t == Door {
						t.Errorf("%s: a second door on side %d (the road is on side %d)", p.ID(), side, want)
					}
				}
				checked++
			}
		}
	}
	if checked < 5 {
		t.Errorf("only %d doors checked", checked)
	}
}

// frozenSmallPOIs are the main points of seed 7 (regions -4..4) that fit in 5x5, as placed before the buildings
// existed.
var frozenSmallPOIs = map[string]Pos{
	"0,-4":  {83, -1011},
	"1,-4":  {287, -986},
	"3,-4":  {891, -965},
	"4,-4":  {1036, -872},
	"-3,-3": {-553, -679},
	"-1,-3": {-224, -551},
	"2,-3":  {676, -564},
	"4,-3":  {1169, -699},
	"-1,-2": {-220, -346},
	"3,-2":  {963, -490},
	"4,-2":  {1146, -383},
	"-3,-1": {-599, -75},
	"-1,-1": {-109, -197},
	"0,-1":  {125, -101},
	"2,-1":  {581, -48},
	"0,0":   {156, 59},
	"1,0":   {482, 206},
	"-3,1":  {-653, 272},
	"-2,1":  {-497, 352},
	"-4,2":  {-920, 553},
	"-2,2":  {-479, 600},
	"-1,2":  {-28, 671},
	"-3,3":  {-732, 837},
	"-2,3":  {-283, 969},
	"1,3":   {462, 856},
	"4,3":   {1037, 923},
	"-4,4":  {-916, 1107},
	"-2,4":  {-372, 1128},
	"0,4":   {125, 1158},
	"1,4":   {429, 1088},
	"2,4":   {526, 1199},
	"4,4":   {1162, 1092},
}

// The points that fit in 5x5 (campfire, standing stone, spring) keep the site they had when they were one tile: saved
// respawn points and activated stones stay valid.
func TestSmallPOISitesAreFrozen(t *testing.T) {
	t.Parallel()
	w := New(7)
	seen := 0
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			p := w.POIOfRegion(RegionPos{rx, ry})
			if p == nil || siteRadius(p.Kind) != poiClear {
				continue
			}
			seen++
			if want, ok := frozenSmallPOIs[p.ID()]; !ok || want != p.Pos {
				t.Errorf("point %s: %v, want %v", p.ID(), p.Pos, want)
			}
		}
	}
	if seen != len(frozenSmallPOIs) {
		t.Errorf("%d small points, %d frozen", seen, len(frozenSmallPOIs))
	}
}

// Over a map of 81 regions, the number of points does not collapse (a big building needs a bigger free area) and the
// roads keep their links (a building must not cut a pass between two places).
func TestPOIDensityAndRoadLinks(t *testing.T) {
	t.Parallel()
	w := New(7)
	n, links := 0, 0
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			rp := RegionPos{rx, ry}
			n += len(w.POIsOfRegion(rp))
			for _, l := range w.roadsAround(rp) {
				if l.key.a == rp {
					links++
				}
			}
		}
	}
	if n < 110 { // 129 before the buildings
		t.Errorf("%d points over 81 regions, expected at least 110", n)
	}
	if links != 21 {
		t.Errorf("%d road links over 81 regions, want 21 (the buildings must not cut any)", links)
	}
}

package world

import "math/rand/v2"

// Buildings of the points of interest: every point is drawn on the map as a small building (church, temple, tower,
// palisade, cave, stone circle...), whose shape depends on its type and on the climate of its district.
//
// A shape is a square grid of tiles centred on the interaction tile of the point (Pos). It is computed in the first
// phase (see poi.go), from the seed and the climate only: the bounds, the tiles and the monster slots therefore never
// depend on the roads. Only the door is chosen afterwards, to face the nearest road: each shape lists, for each of the
// four sides, the tiles to open (gates).

// tileNone marks a tile of a shape that is not part of the building: the natural terrain shows through.
const tileNone Tile = 255

// Sides of a building, in the order of the gate lists: the door faces south, west, north or east.
const (
	sideSouth = iota
	sideWest
	sideNorth
	sideEast
	numSides
)

// gateCell is a tile that a door opens when it faces a given side: dx, dy are relative to the centre of the building.
type gateCell struct {
	dx, dy int
	t      Tile
}

// shape is the building of a point: tiles is a (2r+1)² grid, row by row, whose centre is the point itself.
type shape struct {
	r     int
	tiles []Tile
	gates [numSides][]gateCell
}

func newShape(r int) *shape {
	n := 2*r + 1
	s := &shape{r: r, tiles: make([]Tile, n*n)}
	for i := range s.tiles {
		s.tiles[i] = tileNone
	}
	return s
}

func (s *shape) idx(dx, dy int) int { return (dy+s.r)*(2*s.r+1) + dx + s.r }

func (s *shape) in(dx, dy int) bool { return abs(dx) <= s.r && abs(dy) <= s.r }

func (s *shape) set(dx, dy int, t Tile) {
	if s.in(dx, dy) {
		s.tiles[s.idx(dx, dy)] = t
	}
}

func (s *shape) at(dx, dy int) Tile {
	if !s.in(dx, dy) {
		return tileNone
	}
	return s.tiles[s.idx(dx, dy)]
}

// box fills the square of Chebyshev radius rad.
func (s *shape) box(rad int, t Tile) {
	for dy := -rad; dy <= rad; dy++ {
		for dx := -rad; dx <= rad; dx++ {
			s.set(dx, dy, t)
		}
	}
}

// ring draws the square outline of Chebyshev radius rad.
func (s *shape) ring(rad int, t Tile) {
	for dy := -rad; dy <= rad; dy++ {
		for dx := -rad; dx <= rad; dx++ {
			if max(abs(dx), abs(dy)) == rad {
				s.set(dx, dy, t)
			}
		}
	}
}

// walls surrounds the non-empty tiles with t: every empty tile touching one (diagonals included) becomes t, so that
// the wall is tight even for a player moving diagonally.
func (s *shape) walls(t Tile) {
	var add []int
	for dy := -s.r; dy <= s.r; dy++ {
		for dx := -s.r; dx <= s.r; dx++ {
			if s.at(dx, dy) != tileNone {
				continue
			}
			for ny := dy - 1; ny <= dy+1; ny++ {
				for nx := dx - 1; nx <= dx+1; nx++ {
					if v := s.at(nx, ny); v != tileNone && v != t {
						add = append(add, s.idx(dx, dy))
						ny, nx = dy+2, dx+2 // leaves both loops
					}
				}
			}
		}
	}
	for _, i := range add {
		s.tiles[i] = t
	}
}

// gate registers cell (dx, dy) as an opening of the given side.
func (s *shape) gate(side, dx, dy int, t Tile) {
	s.gates[side] = append(s.gates[side], gateCell{dx, dy, t})
}

// gates4 registers, for each side, the opening in the middle of the square of radius rad.
func (s *shape) gates4(rad int, t Tile) {
	s.gate(sideSouth, 0, rad, t)
	s.gate(sideWest, -rad, 0, t)
	s.gate(sideNorth, 0, -rad, t)
	s.gate(sideEast, rad, 0, t)
}

// siteRadius is the radius of the area a point needs at its site: the largest building of its type. The site is
// chosen before the climate (hence the shape) is known, and every shape of the type fits in it.
func siteRadius(k POIKind) int {
	switch k {
	case POIShrine:
		return 5
	case POIRuin, POILair:
		return 4
	case POITower:
		return 3
	}
	return poiClear // campfire, standing stone, spring: 5x5
}

// buildShape draws the building of a point of type k in climate cl. seed gives the irregular details (breaches of a
// ruin, side of a tent).
func buildShape(k POIKind, cl Climate, seed int64) *shape {
	rng := rand.New(rand.NewPCG(uint64(seed), 0x5EED5A9E))
	var s *shape
	switch k {
	case POIShrine:
		s = shrineShape(cl)
	case POIRuin:
		s = ruinShape(cl, rng)
	case POITower:
		s = towerShape(cl)
	case POILair:
		s = lairShape(cl)
	case POIStone:
		s = stoneShape(cl)
	case POISpring:
		s = springShape(cl)
	default:
		s = campfireShape(cl, rng)
	}
	s.set(0, 0, poiTile(k))
	return s
}

// shrineShape: a church whose nave and transept form a (Greek) cross, an open colonnaded temple, or a walled temple
// with an inner sanctuary. The altar is at the centre.
func shrineShape(cl Climate) *shape {
	switch cl {
	case Tropical: // open temple: a platform surrounded by columns
		s := newShape(4)
		s.box(3, Floor)
		for dy := -4; dy <= 4; dy += 2 {
			for dx := -4; dx <= 4; dx += 2 {
				if max(abs(dx), abs(dy)) == 4 {
					s.set(dx, dy, Pillar)
				}
			}
		}
		s.gates4(4, Floor) // the door is a gap in the colonnade
		return s
	case Arid: // temple: a walled court and a 3x3 inner sanctuary
		s := newShape(4)
		s.ring(4, Wall)
		s.box(3, Floor)
		s.ring(2, Wall)
		s.box(1, Floor)
		s.gates4(4, Door)
		s.gate(sideSouth, 0, 2, Door)
		s.gate(sideWest, -2, 0, Door)
		s.gate(sideNorth, 0, -2, Door)
		s.gate(sideEast, 2, 0, Door)
		return s
	}
	arm := 4 // church with a cross-shaped plan; compact chapel in the cold
	if cl == Boreal {
		arm = 3
	}
	s := newShape(arm + 1)
	for dy := -arm; dy <= arm; dy++ {
		for dx := -arm; dx <= arm; dx++ {
			if abs(dx) <= 1 || abs(dy) <= 1 {
				s.set(dx, dy, Floor)
			}
		}
	}
	s.walls(Wall)
	s.gates4(arm+1, Door) // the doors close the ends of the four equal arms
	return s
}

// ruinShape: a rectangular building (9x7) half collapsed: about 40 % of the walls are down, a whole stretch of one
// wall has fallen and rubble litters the floor. Nature takes over according to the climate.
func ruinShape(cl Climate, rng *rand.Rand) *shape {
	s := newShape(4)
	for dy := -3; dy <= 3; dy++ {
		for dx := -4; dx <= 4; dx++ {
			s.set(dx, dy, Floor)
		}
	}
	var wall [][2]int
	for dy := -3; dy <= 3; dy++ {
		for dx := -4; dx <= 4; dx++ {
			if abs(dx) == 4 || abs(dy) == 3 {
				s.set(dx, dy, Wall)
				if abs(dx) != 4 || abs(dy) != 3 { // the corners stand
					wall = append(wall, [2]int{dx, dy})
				}
			}
		}
	}
	for _, c := range wall {
		if rng.IntN(100) < 40 {
			s.set(c[0], c[1], Rubble)
		}
	}
	// A whole stretch of one wall has fallen.
	side := rng.IntN(numSides)
	for o := -2; o <= 2; o++ {
		switch side {
		case sideSouth:
			s.set(o, 3, Rubble)
		case sideNorth:
			s.set(o, -3, Rubble)
		case sideWest:
			s.set(-4, o/2, Rubble)
		default:
			s.set(4, o/2, Rubble)
		}
	}
	nature := Rubble
	switch cl {
	case Tropical:
		nature = Jungle
	case Arid:
		nature = Sand
	case Boreal:
		nature = Snow
	}
	for dy := -2; dy <= 2; dy++ {
		for dx := -3; dx <= 3; dx++ {
			// The centre (chest) and the guardian's post (0, 2) stay clear.
			if (dx == 0 && (dy == 0 || dy == 2)) || s.at(dx, dy) != Floor {
				continue
			}
			switch r := rng.IntN(100); {
			case r < 18:
				s.set(dx, dy, Rubble)
			case r < 36:
				s.set(dx, dy, nature)
			}
		}
	}
	s.gate(sideSouth, 0, 3, Door)
	s.gate(sideWest, -4, 0, Door)
	s.gate(sideNorth, 0, -3, Door)
	s.gate(sideEast, 4, 0, Door)
	return s
}

// towerShape: a square tower, a round tower on stilts, a round stone tower or a walled square tower. The stairs are at
// the centre.
func towerShape(cl Climate) *shape {
	switch cl {
	case Tropical: // round platform on stilts
		s := newShape(2)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				switch d := dx*dx + dy*dy; {
				case d <= 4:
					s.set(dx, dy, Floor)
				case d <= 5:
					s.set(dx, dy, Pillar)
				}
			}
		}
		return s
	case Arid: // round stone tower: a disc of radius ~2 wrapped in a tight wall
		s := newShape(3)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				if dx*dx+dy*dy <= 5 {
					s.set(dx, dy, Floor)
				}
			}
		}
		s.walls(Wall)
		s.gates4(3, Door)
		return s
	case Boreal: // square tower inside a low wall
		s := newShape(3)
		s.ring(3, Wall)
		s.box(2, Floor)
		s.ring(1, Wall)
		s.gates4(3, Floor)
		s.gate(sideSouth, 0, 1, Door)
		s.gate(sideWest, -1, 0, Door)
		s.gate(sideNorth, 0, -1, Door)
		s.gate(sideEast, 1, 0, Door)
		return s
	}
	s := newShape(2) // square tower 5x5
	s.ring(2, Wall)
	s.box(1, Floor)
	s.gates4(2, Door)
	return s
}

// lairShape: a camp inside a palisade (one opening), or a cave of rock in the dry and cold climates. The three
// guardians' posts (see newPOI) are on the floor, whatever the side of the opening.
func lairShape(cl Climate) *shape {
	s := newShape(4)
	switch cl {
	case Arid, Boreal:
		floor := Sand
		if cl == Boreal {
			floor = Snow
		}
		s.ring(4, Mountain)
		s.box(3, floor)
		for _, c := range [][2]int{{-3, -3}, {3, -3}, {-3, 3}, {3, 3}} {
			s.set(c[0], c[1], Mountain) // rounded cave
		}
		s.gates4(4, floor)
	default:
		s.ring(4, Wall)
		s.box(3, Floor)
		s.gates4(4, Floor)
	}
	return s
}

// stoneShape: a cromlech (ring of standing stones with four openings) or a dolmen (three stones).
func stoneShape(cl Climate) *shape {
	s := newShape(2)
	if cl == Tropical || cl == Arid {
		s.set(-1, -1, Pillar)
		s.set(0, -1, Pillar)
		s.set(1, -1, Pillar)
		return s
	}
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			if max(abs(dx), abs(dy)) == 2 && dx != 0 && dy != 0 {
				s.set(dx, dy, Pillar)
			}
		}
	}
	return s
}

// springShape: a pool at the centre, with a kerb; an oasis ringed with trees in the arid climate.
func springShape(cl Climate) *shape {
	s := newShape(2)
	pool, kerb := Water, Floor
	switch cl {
	case Boreal:
		pool = Snow // frozen pool
	case Arid:
		kerb = Sand
	}
	for _, c := range [][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		s.set(c[0], c[1], pool)
	}
	for _, c := range [][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}} {
		s.set(c[0], c[1], kerb)
	}
	switch cl {
	case Arid:
		s.ring(2, Forest)
	case Tropical:
		for _, c := range [][2]int{{-2, -2}, {2, -2}, {-2, 2}, {2, 2}} {
			s.set(c[0], c[1], Jungle)
		}
	}
	return s
}

// campfireShape: the fire and a small tent (3x2) next to it, its door facing the fire. The side of the tent is drawn
// from the seed.
func campfireShape(cl Climate, rng *rand.Rand) *shape {
	s := newShape(2)
	k := rng.IntN(4)
	put := func(dx, dy int, t Tile) {
		for range k { // quarter turns around the fire
			dx, dy = -dy, dx
		}
		s.set(dx, dy, t)
	}
	switch cl {
	case Tropical: // hut on stilts
		put(-1, -2, Pillar)
		put(1, -2, Pillar)
		put(-1, -1, Pillar)
		put(1, -1, Pillar)
		put(0, -2, Wall)
	case Boreal: // yurt: rounded roof
		put(0, -2, Wall)
		put(-1, -1, Wall)
		put(1, -1, Wall)
	default: // tent
		put(-1, -2, Wall)
		put(0, -2, Wall)
		put(1, -2, Wall)
		put(-1, -1, Wall)
		put(1, -1, Wall)
	}
	put(0, -1, Door)
	return s
}

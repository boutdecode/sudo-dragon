// Package world generates an infinite, deterministic procedural world, split into chunks.
package world

import (
	"math"
	"sync"

	opensimplex "github.com/ojrac/opensimplex-go"
)

// ChunkSize is the width/height of a chunk in tiles.
const ChunkSize = 32

// Tile is the terrain type of a tile.
type Tile uint8

const (
	DeepWater Tile = iota
	Water
	Sand
	Plain
	Forest
	Hills
	Mountain
	Snow

	// Structure tiles (villages, towns, fortresses). Added after
	// Snow so as not to change the terrain tile values.
	Road  // road, square
	Floor // interior floor of a building
	Wall  // wall: impassable
	Door  // door or gate: passable

	// Dungeon tiles (see dungeon.go).
	Chest  // chest: passable, opens by walking onto it
	Stairs // staircase: entrance and exit of the dungeon

	// Entrance marks, on the client screen only, an already discovered dungeon entrance. The world never
	// generates it: the secret entrance is an ordinary terrain tile (see dungeon.go).
	Entrance

	// Rock is the solid rock of a dungeon: impassable, drawn muted (see dungeonmap.go).
	Rock

	// Outdoor points of interest (see poi.go): all passable, one interacts by walking onto them.
	Campfire  // campfire
	Shrine    // shrine
	Tower     // watchtower
	Stone     // standing stone
	LairChest // lair chest
	Spring    // spring or oasis
	Ruin      // guarded ruins

	// River is a river: shallow water, fordable (see terrain2.go). A bridge is a road laid over it.
	River

	// v2 biomes (see terrain2.go): all passable.
	Tundra // frozen ground of the high latitudes
	Desert // sand and rock of the hot, dry zones
	Swamp  // marsh, low and wet
	Jungle // dense forest of the hot, humid zones

	// v3 resource nodes (see resources.go): passable, harvested with the action key.
	TreeNode // tree to fell (wood)
	OreNode  // ore vein or rock outcrop
	HerbNode // tuft of plants
	FishNode // school of fish, in a river

	// Building tiles of the points of interest (see poi_shapes.go).
	Rubble // rubble of a collapsed wall: passable
	Pillar // column or standing stone: impassable

	// NumTiles is the number of tile types: the values range from 0 to NumTiles-1.
	NumTiles
)

// Glyph returns an ASCII character representing the tile (schematic view, without
// colour; the client uses render, which adds variants and colours). Pure
// ASCII: characters like ♣ or ≈ have an ambiguous width depending on the terminal.
func (t Tile) Glyph() rune {
	switch t {
	case DeepWater:
		return '='
	case Water:
		return '~'
	case Sand:
		return '.'
	case Plain:
		return '"'
	case Forest:
		return 'T'
	case Hills:
		return 'n'
	case Mountain:
		return '^'
	case Snow:
		return '*'
	case Road:
		return ':'
	case Floor:
		return '_'
	case Wall:
		return '#'
	case Door:
		return '+'
	case Chest:
		return '$'
	case Stairs:
		return '<'
	case Entrance:
		return '>'
	case Rock:
		return '%'
	case Campfire:
		return '&'
	case Shrine:
		return 'o'
	case Tower:
		return 'I'
	case Stone:
		return '|'
	case LairChest:
		return 'x'
	case Spring:
		return 'v'
	case Ruin:
		return '/'
	case River:
		return '-'
	case Tundra:
		return ','
	case Desert:
		return ';'
	case Swamp:
		return 'm'
	case Jungle:
		return 'J'
	case TreeNode:
		return 'a'
	case OreNode:
		return '0'
	case HerbNode:
		return 'h'
	case FishNode:
		return 'f'
	case Rubble:
		return '`'
	case Pillar:
		return 'i'
	}
	return '?'
}

// ordinary indicates ordinary natural terrain: flat and dry enough for a hidden dungeon, a point of interest,
// a buried treasure or a structure. Excludes water, mountains, rivers and marshes.
func (t Tile) ordinary() bool {
	switch t {
	case Plain, Forest, Hills, Sand, Tundra, Desert, Jungle:
		return true
	}
	return false
}

// Walkable tells whether a player can walk on the tile.
func (t Tile) Walkable() bool {
	switch t {
	case DeepWater, Water, Mountain, Wall, Rock, Pillar:
		return false
	}
	return true
}

// ChunkPos identifies a chunk in the chunk grid.
type ChunkPos struct{ X, Y int }

// Pos is a position in tile coordinates.
type Pos struct{ X, Y int }

// Chunk contains the tiles of a ChunkSize x ChunkSize area.
type Chunk struct {
	Pos   ChunkPos
	Tiles [ChunkSize][ChunkSize]Tile // [y][x]
}

// World generates and caches chunks from a seed.
type World struct {
	Seed   int64
	noise2 noise2 // noises of the terrain generator (see terrain2.go)

	mu    sync.Mutex
	cache map[ChunkPos]*Chunk

	// Structures (see structures.go). smu is always taken after mu, never before.
	smu        sync.Mutex
	structs    map[RegionPos]*Structure
	anchorOnce sync.Once
	anchor     Pos

	// Districts (see territory.go). tmu is independent of mu and smu.
	tmu         sync.Mutex
	territories map[TerritoryID]*Territory

	// Dungeons (see dungeon.go). dmu is independent of the other locks: it is never held
	// during a computation.
	dmu      sync.Mutex
	dungeons map[RegionPos]*Dungeon

	// Points of interest (see poi.go). pmu is independent of the other locks: it is never held during a computation.
	pmu  sync.Mutex
	pois map[RegionPos][]*POI

	// Roads between places (see roads.go). rmu is taken before smu, never after.
	rmu   sync.Mutex
	roads map[roadKey]*roadLink
}

// New creates a deterministic world for the given seed.
func New(seed int64) *World {
	return &World{
		Seed:    seed,
		noise2:  newNoise2(seed),
		cache:   make(map[ChunkPos]*Chunk),
		structs: make(map[RegionPos]*Structure),

		dungeons:    make(map[RegionPos]*Dungeon),
		pois:        make(map[RegionPos][]*POI),
		territories: make(map[TerritoryID]*Territory),
		roads:       make(map[roadKey]*roadLink),
	}
}

// fbm sums several octaves of noise, result in [-1, 1].
func fbm(n opensimplex.Noise, x, y float64) float64 {
	sum, amp, freq, norm := 0.0, 1.0, 1.0, 0.0
	for range 4 {
		sum += amp * n.Eval2(x*freq, y*freq)
		norm += amp
		amp *= 0.5
		freq *= 2
	}
	return sum / norm
}

// TileAt computes the tile at an absolute position (without chunk cache), taking
// structures into account.
func (w *World) TileAt(x, y int) Tile {
	if s := w.StructureAt(x, y); s != nil {
		return s.TileAt(x, y)
	}
	t := w.terrainAt(x, y)
	if p := w.poiCovering(x, y); p != nil {
		if pt := p.TileAt(w, x, y); pt != tileNone {
			return pt
		}
		return t // inside the footprint, off the building: raw terrain (no road, no node)
	}
	if w.roadCostOf(t) > 0 && onRoad(w.roadsAround(RegionOf(x, y)), x, y) {
		return Road
	}
	if n, ok := w.nodeTile(x, y, t); ok {
		return n
	}
	return t
}

// terrainAt computes the raw terrain, without structures. Structure placement
// relies on it (never on TileAt, to avoid any circularity).
func (w *World) terrainAt(x, y int) Tile {
	return w.terrainAtV2(x, y)
}

// Terrain is the raw terrain at (x, y), without structure, POI or road. For the visualisation tools.
func (w *World) Terrain(x, y int) Tile { return w.terrainAt(x, y) }

// floorDiv is integer division rounded towards -infinity (correct for negatives).
func floorDiv(a, b int) int {
	return int(math.Floor(float64(a) / float64(b)))
}

// ChunkOf returns the chunk containing the given tile position.
func ChunkOf(x, y int) ChunkPos {
	return ChunkPos{floorDiv(x, ChunkSize), floorDiv(y, ChunkSize)}
}

// Chunk returns (generating it if needed) the requested chunk.
func (w *World) Chunk(cp ChunkPos) *Chunk {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.cache[cp]; ok {
		return c
	}
	c := &Chunk{Pos: cp}
	// A whole region contains the chunk: a single structure lookup,
	// and not one per tile.
	rp := RegionOf(cp.X*ChunkSize, cp.Y*ChunkSize)
	s := w.structureForRegion(rp)
	roads := w.roadsAround(rp)
	pois := w.POIsOfRegion(rp)
	for ty := range ChunkSize {
		for tx := range ChunkSize {
			x, y := cp.X*ChunkSize+tx, cp.Y*ChunkSize+ty
			if s != nil && s.Contains(x, y) {
				c.Tiles[ty][tx] = s.TileAt(x, y)
			} else if poi := poiCovering(pois, x, y); poi != nil {
				if pt := poi.TileAt(w, x, y); pt != tileNone {
					c.Tiles[ty][tx] = pt
				} else {
					c.Tiles[ty][tx] = w.terrainAt(x, y)
				}
			} else if t := w.terrainAt(x, y); w.roadCostOf(t) > 0 && onRoad(roads, x, y) {
				c.Tiles[ty][tx] = Road
			} else if n, ok := w.nodeTile(x, y, t); ok {
				c.Tiles[ty][tx] = n
			} else {
				c.Tiles[ty][tx] = t
			}
		}
	}
	w.cache[cp] = c
	return c
}

// At returns the tile at the absolute position (via the chunk cache).
func (w *World) At(x, y int) Tile {
	cp := ChunkOf(x, y)
	c := w.Chunk(cp)
	return c.Tiles[y-cp.Y*ChunkSize][x-cp.X*ChunkSize]
}

// Unload removes from memory the chunks not belonging to keep.
func (w *World) Unload(keep map[ChunkPos]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for cp := range w.cache {
		if !keep[cp] {
			delete(w.cache, cp)
		}
	}
}

// FindSpawn returns the players' starting point: the first plain or forest
// in a spiral around the origin, computed on the raw terrain. No
// structure covers its surroundings (see startClearance).
func (w *World) FindSpawn() Pos { return w.spawnAnchor() }

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

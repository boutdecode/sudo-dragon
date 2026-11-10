// Package world génère un monde procédural infini, déterministe, découpé en chunks.
package world

import (
	"math"
	"sync"

	opensimplex "github.com/ojrac/opensimplex-go"
)

// ChunkSize est la largeur/hauteur d'un chunk en tuiles.
const ChunkSize = 32

// Tile est le type de terrain d'une case.
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

	// Tuiles des structures (villages, villes, forteresses). Ajoutées après
	// Snow pour ne pas changer les valeurs des tuiles de terrain.
	Road  // chemin, place
	Floor // sol intérieur d'un bâtiment
	Wall  // mur: infranchissable
	Door  // porte ou portail: franchissable

	// Tuiles des donjons (voir dungeon.go).
	Chest  // coffre: franchissable, s'ouvre en y marchant
	Stairs // escalier: entrée et sortie du donjon

	// Entrance marque, à l'écran du client seulement, l'entrée d'un donjon déjà découverte. Le monde ne la
	// génère jamais : l'entrée secrète est une case de terrain ordinaire (voir dungeon.go).
	Entrance

	// Rock est la roche pleine d'un donjon: infranchissable, dessinée en sourdine (voir dungeonmap.go).
	Rock

	// Points d'intérêt en plein air (voir poi.go): tous franchissables, on interagit en y marchant.
	Campfire  // feu de camp
	Shrine    // sanctuaire
	Tower     // tour de guet
	Stone     // pierre dressée
	LairChest // coffre d'un repaire
	Spring    // source ou oasis
	Ruin      // ruines gardées

	// River est une rivière: eau peu profonde, franchissable à gué (voir terrain2.go). Un pont est une route posée dessus.
	River

	// Biomes de la v2 (voir terrain2.go): tous franchissables.
	Tundra // sol gelé des hautes latitudes
	Desert // sable et roche des zones chaudes et sèches
	Swamp  // marais, bas et humide
	Jungle // forêt dense des zones chaudes et humides

	// NumTiles est le nombre de types de tuiles: les valeurs vont de 0 à NumTiles-1.
	NumTiles
)

// Glyph renvoie un caractère ASCII représentant la tuile (vue schématique, sans
// couleur; le client utilise render, qui ajoute variantes et couleurs). ASCII
// pur: les caractères comme ♣ ou ≈ ont une largeur ambiguë selon les terminaux.
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
	}
	return '?'
}

// ordinary indique un terrain naturel ordinaire: assez plat et sec pour un donjon caché, un point d'intérêt,
// un trésor enterré ou une structure. Exclut l'eau, les montagnes, les rivières et les marais.
func (t Tile) ordinary() bool {
	switch t {
	case Plain, Forest, Hills, Sand, Tundra, Desert, Jungle:
		return true
	}
	return false
}

// Walkable indique si un joueur peut marcher sur la tuile.
func (t Tile) Walkable() bool {
	switch t {
	case DeepWater, Water, Mountain, Wall, Rock:
		return false
	}
	return true
}

// ChunkPos identifie un chunk dans la grille de chunks.
type ChunkPos struct{ X, Y int }

// Pos est une position en coordonnées de tuiles.
type Pos struct{ X, Y int }

// Chunk contient les tuiles d'une zone ChunkSize x ChunkSize.
type Chunk struct {
	Pos   ChunkPos
	Tiles [ChunkSize][ChunkSize]Tile // [y][x]
}

// World génère et met en cache des chunks à partir d'une seed.
type World struct {
	Seed int64
	// GenVersion est la version du générateur de terrain (voir GenLatest). Un monde déjà
	// joué garde la sienne: changer le terrain d'une base existante rendrait incohérentes
	// les positions sauvegardées.
	GenVersion int

	elevation opensimplex.Noise
	moisture  opensimplex.Noise
	noise2    noise2 // bruits du générateur v2

	mu    sync.Mutex
	cache map[ChunkPos]*Chunk

	// Structures (voir structures.go). smu est toujours pris après mu, jamais avant.
	smu        sync.Mutex
	structs    map[RegionPos]*Structure
	anchorOnce sync.Once
	anchor     Pos

	// Contrées (voir territory.go). tmu est indépendant de mu et smu.
	tmu         sync.Mutex
	territories map[TerritoryID]*Territory

	// Donjons (voir dungeon.go). dmu est indépendant des autres verrous : il n'est jamais tenu
	// pendant un calcul.
	dmu      sync.Mutex
	dungeons map[RegionPos]*Dungeon

	// Points d'intérêt (voir poi.go). pmu est indépendant des autres verrous : il n'est jamais tenu pendant un calcul.
	pmu  sync.Mutex
	pois map[RegionPos][]*POI

	// Routes entre lieux (voir roads.go). rmu est pris avant smu, jamais après.
	rmu   sync.Mutex
	roads map[roadKey]*roadLink
}

// Versions du générateur de terrain.
const (
	// GenV1 est le générateur d'origine (élévation + humidité, sans continents).
	GenV1 = 1
	// GenV2 ajoute continents, océans, îles, chaînes de montagnes, rivières et biomes (voir terrain2.go).
	GenV2 = 2
	// GenLatest est la version utilisée pour les nouveaux mondes.
	GenLatest = GenV2
)

// New crée un monde déterministe pour la seed donnée, avec le générateur d'origine (v1).
func New(seed int64) *World { return NewVersion(seed, GenV1) }

// NewVersion crée un monde déterministe pour la seed et la version de générateur données.
// Une version inconnue ou nulle retombe sur v1 (anciens clients et bases sans version).
func NewVersion(seed int64, gen int) *World {
	if gen < GenV1 || gen > GenV2 {
		gen = GenV1
	}
	return &World{
		Seed:       seed,
		GenVersion: gen,
		noise2:     newNoise2(seed),
		elevation:  opensimplex.New(seed),
		moisture:   opensimplex.New(seed ^ 0x5DEECE66D),
		cache:      make(map[ChunkPos]*Chunk),
		structs:    make(map[RegionPos]*Structure),

		dungeons:    make(map[RegionPos]*Dungeon),
		pois:        make(map[RegionPos][]*POI),
		territories: make(map[TerritoryID]*Territory),
		roads:       make(map[roadKey]*roadLink),
	}
}

// fbm somme plusieurs octaves de bruit, résultat dans [-1, 1].
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

// TileAt calcule la tuile à une position absolue (sans cache de chunk), en
// tenant compte des structures.
func (w *World) TileAt(x, y int) Tile {
	if s := w.StructureAt(x, y); s != nil {
		return s.TileAt(x, y)
	}
	if p := w.POIAt(x, y); p != nil {
		return p.Tile()
	}
	t := w.terrainAt(x, y)
	if w.roadCostOf(t) > 0 && onRoad(w.roadsAround(RegionOf(x, y)), x, y) {
		return Road
	}
	return t
}

// terrainAt calcule le terrain brut, sans structure. Le placement des
// structures s'appuie sur lui (jamais sur TileAt, pour éviter toute circularité).
func (w *World) terrainAt(x, y int) Tile {
	if w.GenVersion >= GenV2 {
		return w.terrainAtV2(x, y)
	}
	return w.terrainAtV1(x, y)
}

// Terrain est le terrain brut en (x, y), sans structure, POI ni route. Pour les outils de visualisation.
func (w *World) Terrain(x, y int) Tile { return w.terrainAt(x, y) }

// terrainAtV1 est le générateur d'origine, figé: ne pas le modifier (layout_golden_test).
func (w *World) terrainAtV1(x, y int) Tile {
	const scale = 0.02
	fx, fy := float64(x)*scale, float64(y)*scale
	e := fbm(w.elevation, fx, fy)
	m := fbm(w.moisture, fx+1000, fy+1000)

	switch {
	case e < -0.35:
		return DeepWater
	case e < -0.15:
		return Water
	case e < -0.08:
		return Sand
	case e < 0.35:
		if m > 0.05 {
			return Forest
		}
		return Plain
	case e < 0.5:
		return Hills
	case e < 0.65:
		return Mountain
	}
	return Snow
}

// floorDiv est la division entière arrondie vers -∞ (correcte pour les négatifs).
func floorDiv(a, b int) int {
	return int(math.Floor(float64(a) / float64(b)))
}

// ChunkOf renvoie le chunk contenant la position de tuile donnée.
func ChunkOf(x, y int) ChunkPos {
	return ChunkPos{floorDiv(x, ChunkSize), floorDiv(y, ChunkSize)}
}

// Chunk renvoie (en le générant au besoin) le chunk demandé.
func (w *World) Chunk(cp ChunkPos) *Chunk {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c, ok := w.cache[cp]; ok {
		return c
	}
	c := &Chunk{Pos: cp}
	// Une région entière contient le chunk: une seule recherche de structure,
	// et non une par tuile.
	rp := RegionOf(cp.X*ChunkSize, cp.Y*ChunkSize)
	s := w.structureForRegion(rp)
	roads := w.roadsAround(rp)
	pois := w.POIsOfRegion(rp)
	for ty := range ChunkSize {
		for tx := range ChunkSize {
			x, y := cp.X*ChunkSize+tx, cp.Y*ChunkSize+ty
			if s != nil && s.Contains(x, y) {
				c.Tiles[ty][tx] = s.TileAt(x, y)
			} else if poi := poiIn(pois, x, y); poi != nil {
				c.Tiles[ty][tx] = poi.Tile()
			} else if t := w.terrainAt(x, y); w.roadCostOf(t) > 0 && onRoad(roads, x, y) {
				c.Tiles[ty][tx] = Road
			} else {
				c.Tiles[ty][tx] = t
			}
		}
	}
	w.cache[cp] = c
	return c
}

// At renvoie la tuile à la position absolue (via le cache de chunks).
func (w *World) At(x, y int) Tile {
	cp := ChunkOf(x, y)
	c := w.Chunk(cp)
	return c.Tiles[y-cp.Y*ChunkSize][x-cp.X*ChunkSize]
}

// Unload retire de la mémoire les chunks n'appartenant pas à keep.
func (w *World) Unload(keep map[ChunkPos]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for cp := range w.cache {
		if !keep[cp] {
			delete(w.cache, cp)
		}
	}
}

// FindSpawn renvoie le point de départ des joueurs: la première plaine ou forêt
// en spirale autour de l'origine, calculée sur le terrain brut. Aucune
// structure ne recouvre ses abords (voir startClearance).
func (w *World) FindSpawn() Pos { return w.spawnAnchor() }

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

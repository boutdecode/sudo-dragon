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
	}
	return '?'
}

// Walkable indique si un joueur peut marcher sur la tuile.
func (t Tile) Walkable() bool {
	switch t {
	case DeepWater, Water, Mountain, Wall:
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

	elevation opensimplex.Noise
	moisture  opensimplex.Noise

	mu    sync.Mutex
	cache map[ChunkPos]*Chunk

	// Structures (voir structures.go). smu est toujours pris après mu, jamais avant.
	smu        sync.Mutex
	structs    map[RegionPos]*Structure
	anchorOnce sync.Once
	anchor     Pos
}

// New crée un monde déterministe pour la seed donnée.
func New(seed int64) *World {
	return &World{
		Seed:      seed,
		elevation: opensimplex.New(seed),
		moisture:  opensimplex.New(seed ^ 0x5DEECE66D),
		cache:     make(map[ChunkPos]*Chunk),
		structs:   make(map[RegionPos]*Structure),
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
	return w.terrainAt(x, y)
}

// terrainAt calcule le terrain brut, sans structure. Le placement des
// structures s'appuie sur lui (jamais sur TileAt, pour éviter toute circularité).
func (w *World) terrainAt(x, y int) Tile {
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
	s := w.structureForRegion(RegionOf(cp.X*ChunkSize, cp.Y*ChunkSize))
	for ty := range ChunkSize {
		for tx := range ChunkSize {
			x, y := cp.X*ChunkSize+tx, cp.Y*ChunkSize+ty
			if s != nil && s.Contains(x, y) {
				c.Tiles[ty][tx] = s.TileAt(x, y)
			} else {
				c.Tiles[ty][tx] = w.terrainAt(x, y)
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

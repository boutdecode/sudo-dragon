package world

import (
	"math"

	opensimplex "github.com/ojrac/opensimplex-go"
)

// Terrain generator v2: continents, oceans and islands.
//
// The terrain remains a pure function (seed, x, y) -> Tile, computable tile by tile: the server and
// the client recompute it identically, and the world stays infinite. Everything starts from an "elevation"
// elev2, sea at 0.

// Scales of generator v2, in tiles.
const (
	continentScale = 600.0 // size of a continent or an ocean
	islandScale    = 100.0 // size of an archipelago
	warpScale      = 150.0 // undulation of the coasts
	warpAmount     = 50.0  // maximum distortion of the coasts, in tiles
	detailScale    = 35.0  // local relief
	originRadius   = 350.0 // radius of the continent guaranteed around (0, 0)
	grainScale     = 6.0   // size of the groves and clearings
	patchScale     = 18.0  // size of the groves, clearings, dunes and ponds inside a biome
)

// noise2 groups the noises specific to v2, all derived from the seed.
type noise2 struct {
	continent, island, warpX, warpY, detail, hills, temp, wet, ridge, ridgeMask, river, riverWarp, riverMask, lake opensimplex.Noise
}

func newNoise2(seed int64) noise2 {
	return noise2{
		continent: opensimplex.New(seed ^ 0x2C1B3C6D),
		island:    opensimplex.New(seed ^ 0x297A2D39),
		warpX:     opensimplex.New(seed ^ 0x1B873593),
		warpY:     opensimplex.New(seed ^ 0x6A09E667),
		detail:    opensimplex.New(seed ^ 0x3C6EF372),
		hills:     opensimplex.New(seed ^ 0x510E527F),
		temp:      opensimplex.New(seed ^ 0x3956C25B),
		wet:       opensimplex.New(seed ^ 0x59F111F1),
		ridge:     opensimplex.New(seed ^ 0x9B05688C),
		ridgeMask: opensimplex.New(seed ^ 0x1F83D9AB),
		river:     opensimplex.New(seed ^ 0x5BE0CD19),
		riverWarp: opensimplex.New(seed ^ 0x428A2F98),
		riverMask: opensimplex.New(seed ^ 0x71374491),
		lake:      opensimplex.New(seed ^ 0xB5C0FBCF),
	}
}

func smoothstep(a, b, v float64) float64 {
	t := math.Max(0, math.Min(1, (v-a)/(b-a)))
	return t * t * (3 - 2*t)
}

// elev2 is the v2 elevation, roughly in [-1, 1]; the sea is below 0.
func (w *World) elev2(x, y int) float64 {
	n := &w.noise2
	fx, fy := float64(x), float64(y)

	// Irregular coasts: the coordinates are distorted before reading the continent.
	wx := fx + warpAmount*fbm(n.warpX, fx/warpScale, fy/warpScale)
	wy := fy + warpAmount*fbm(n.warpY, fx/warpScale+50, fy/warpScale+50)

	// Continents: very large-scale noise. A curve stretches it into land masses
	// well separated by wide oceans rather than a checkerboard.
	c := fbm(n.continent, wx/continentScale, wy/continentScale)
	c = c*1.6 - 0.05

	// A guaranteed continent around the origin: the player never spawns in the open sea.
	d := math.Hypot(fx, fy)
	c += 0.9 * (1 - smoothstep(0, originRadius, d))

	// Islands: where the ocean is shallow, a second noise makes archipelagos emerge.
	isl := fbm(n.island, wx/islandScale, wy/islandScale)
	shallow := 1 - smoothstep(-0.5, -0.05, c) // 1 in shallow sea, 0 offshore
	shallow *= smoothstep(-0.95, -0.55, c)    // no islands in the middle of a deep ocean
	c += shallow * math.Max(0, isl-0.05) * 2.2

	// Local relief, which cuts up the shores and plains a little.
	return c + 0.12*fbm(n.detail, fx/detailScale, fy/detailScale)
}

// Mountain ranges, rivers and lakes.
const (
	ridgeScale       = 200.0 // distance between two ranges
	ridgeWarpScale   = 90.0  // distortion of the ranges
	ridgeWarpAmount  = 45.0
	riverScale       = 160.0 // sinuosity of the rivers
	riverWarpScale   = 110.0
	riverWarpAmount  = 75.0
	riverWarpScale2  = 55.0 // small undulations
	riverWarpAmount2 = 34.0
	riverHalfBase    = 1.3 // half-width of a river, in tiles
	riverHalfCoast   = 1.6 // half-width added towards the coasts
	lakeScale        = 45.0
	hillScale        = 70.0 // size of isolated hills
)

// mountain2 is the height of the mountains at (x, y), in [0, 1]: linear ridges (where a noise
// crosses zero), interrupted by a second noise to form ranges rather than a grid, and
// which only appear inland.
func (w *World) mountain2(x, y int, e float64) float64 {
	n := &w.noise2
	fx, fy := float64(x), float64(y)
	// The ridges are distorted like the rivers: without this, a range is a band with almost straight edges.
	wx := fx + ridgeWarpAmount*n.riverWarp.Eval2(fx/ridgeWarpScale+800, fy/ridgeWarpScale+800) +
		ridgeWarpAmount/3*n.riverWarp.Eval2(fx/(ridgeWarpScale/3)+900, fy/(ridgeWarpScale/3)+900)
	wy := fy + ridgeWarpAmount*n.riverWarp.Eval2(fx/ridgeWarpScale+1100, fy/ridgeWarpScale+1100) +
		ridgeWarpAmount/3*n.riverWarp.Eval2(fx/(ridgeWarpScale/3)+1200, fy/(ridgeWarpScale/3)+1200)
	r := 1 - math.Abs(n.ridge.Eval2(wx/ridgeScale, wy/ridgeScale)+0.3*n.ridge.Eval2(wx/ridgeScale*2.3+9, wy/ridgeScale*2.3+9))
	r = math.Max(0, (r-0.6)/0.4) // narrow ridge: 0 outside a range, 1 on the ridge line
	mask := smoothstep(-0.12, 0.05, fbm(n.ridgeMask, fx/(ridgeScale*1.7)+300, fy/(ridgeScale*1.7)+300))
	inland := smoothstep(0.08, 0.4, e)
	return r * mask * inland
}

// riverField is the field whose zero level line is the rivers. The point is first distorted at two
// scales (large bends, then small undulations) and the field sums several octaves (meanders and small
// sinuosities): the line is therefore not a long straight stroke.
func (w *World) riverField(fx, fy float64) float64 {
	n := &w.noise2
	wx := fx + riverWarpAmount*n.riverWarp.Eval2(fx/riverWarpScale, fy/riverWarpScale) +
		riverWarpAmount2*n.riverWarp.Eval2(fx/riverWarpScale2+70, fy/riverWarpScale2+70)
	wy := fy + riverWarpAmount*n.riverWarp.Eval2(fx/riverWarpScale+300, fy/riverWarpScale+300) +
		riverWarpAmount2*n.riverWarp.Eval2(fx/riverWarpScale2+370, fy/riverWarpScale2+370)
	return n.river.Eval2(wx/riverScale, wy/riverScale) +
		0.4*n.river.Eval2(wx/(riverScale/2.5)+30, wy/(riverScale/2.5)+30) +
		0.15*n.river.Eval2(wx/(riverScale/6)+60, wy/(riverScale/6)+60)
}

// riverAt tells whether (x, y) is a river: a level line of riverField, without simulating the flow (the
// world is infinite). The width is given in tiles: we measure the distance to the level line using the gradient of the
// field (without which the band widens into broad strokes where the field is almost flat), and it varies along the
// course. A mask noise makes the river appear and fade; it widens towards the coasts and does not climb the
// high peaks.
func (w *World) riverAt(x, y int, e, mount float64) bool {
	if e < 0.06 || mount > 0.35 {
		return false
	}
	fx, fy := float64(x), float64(y)
	v := w.riverField(fx, fy)
	if math.Abs(v) > 0.25 {
		return false // too far from a level line: avoids computing the gradient
	}
	n := &w.noise2
	life := smoothstep(-0.2, 0.2, fbm(n.riverMask, fx/(riverScale*2.2)+500, fy/(riverScale*2.2)+500))
	// Variable width along the course: sometimes a trickle, sometimes a wider bed.
	vary := 0.65 + 0.7*smoothstep(-0.5, 0.5, n.detail.Eval2(fx/55+1900, fy/55+1900))
	half := (riverHalfBase + riverHalfCoast*(1-smoothstep(0.1, 0.8, e))) * life * vary
	if half < 0.5 {
		return false
	}
	const h = 1.5
	gx := (w.riverField(fx+h, fy) - v) / h
	gy := (w.riverField(fx, fy+h) - v) / h
	g := math.Hypot(gx, gy)
	return g > 1e-6 && math.Abs(v)/g < half
}

// Climate: temperature and humidity, roughly in [-1, 1].
const (
	tempScale     = 800.0  // size of the climate zones
	latitudeSpan  = 3200.0 // period of the latitude bands (north-south)
	midWetScale   = 140.0  // size of the local humidity patches
	temperateZone = 400.0  // radius around (0, 0) where the climate is pulled towards temperate
)

// climate2 returns the temperature and humidity of (x, y). The temperature follows a latitude (cold
// and hot bands along y, with a phase specific to the seed) perturbed by a noise; humidity comes from a
// noise, stronger near the sea. The neighbourhood of the origin is pulled towards temperate: one always spawns there
// in a land of plains and forests.
func (w *World) climate2(x, y int, e float64) (temp, wet float64) {
	n := &w.noise2
	fx, fy := float64(x), float64(y)
	phase := float64(uint64(w.Seed)&0xFFFF) / 0xFFFF * 2 * math.Pi
	temp = 0.7*math.Sin(2*math.Pi*fy/latitudeSpan+phase) + 0.5*fbm(n.temp, fx/tempScale, fy/tempScale)
	// Humidity at two scales: the climate zones, and patches of a few hundred tiles that mix
	// forests and plains within the same zone.
	wet = 1.4*fbm(n.wet, fx/tempScale+200, fy/tempScale+200) + 1.0*fbm(n.wet, fx/midWetScale+900, fy/midWetScale+900) +
		0.3*(1-smoothstep(0.05, 0.6, e))
	k := smoothstep(0, temperateZone, math.Hypot(fx, fy)) // 0 at the origin, 1 far away
	// Towards temperate and mid-humid (forests and plains mixed), without erasing all variation.
	return temp * (0.3 + 0.7*k), 0.12 + (wet-0.12)*(0.6+0.4*k)
}

// terrainAtV2 is the v2 terrain generator.
func (w *World) terrainAtV2(x, y int) Tile {
	e := w.elev2(x, y)
	switch {
	case e < -0.38:
		return DeepWater
	case e < -0.02:
		return Water
	case e < 0.04:
		return Sand
	}
	temp, wet := w.climate2(x, y, e)
	mount := w.mountain2(x, y, e)
	cold := -temp // the larger, the colder
	if mount > 0.55 {
		// Snow on the peaks of cold regions, and on the highest ones everywhere.
		if mount > 0.95 || cold+0.5*mount > 0.8 {
			return Snow
		}
		return Mountain
	}
	if w.riverAt(x, y, e, mount) {
		return River
	}
	// Lakes: isolated basins inland.
	if e > 0.25 && mount < 0.1 && fbm(w.noise2.lake, float64(x)/lakeScale, float64(y)/lakeScale) > 0.5 {
		return Water
	}
	if mount > 0.3 || fbm(w.noise2.hills, float64(x)/hillScale, float64(y)/hillScale) > 0.25 {
		return Hills
	}
	// Local patterns inside a biome: groves and clearings, dunes, ponds, snow patches.
	patch := fbm(w.noise2.detail, float64(x)/patchScale+700, float64(y)/patchScale+700)
	// Fine grain: isolated groves in a plain, clearings in a forest, ragged edges.
	grain := fbm(w.noise2.hills, float64(x)/grainScale+2300, float64(y)/grainScale+2300)
	switch {
	case cold > 0.95:
		return Snow
	case cold > 0.62:
		switch {
		case patch > 0.28:
			return Hills
		case patch < -0.28:
			return Snow
		}
		return Tundra
	case temp > 0.4 && wet < -0.1:
		switch {
		case patch > 0.25:
			return Hills // dune
		case patch < -0.25:
			return Sand
		}
		return Desert
	case wet > 0.35 && e < 0.3 && temp > -0.1:
		if patch > 0.3 {
			return Water // pond
		}
		return Swamp
	case temp > 0.4 && wet > 0.15:
		if patch < -0.3 {
			return Forest // clearing
		}
		return Jungle
	case 0.35*wet+1.4*patch+3.2*grain > 0.05:
		return Forest
	}
	return Plain
}

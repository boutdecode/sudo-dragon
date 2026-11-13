package world

// Climate: the mood of a district. It is purely visual (client colours and glyphs): it changes neither the
// terrain, nor the monsters, nor the generation. It is derived from the seed and the district by a pure hash, so the
// server and the client compute it the same way, without exchanging anything.

// Climate is the climate of a district.
type Climate uint8

const (
	Temperate Climate = iota // the original colours
	Tropical                 // vivid greens, turquoise seas
	Arid                     // yellow plains, red hills
	Boreal                   // cold, bluish greens
	NumClimates
)

// climateSalt distinguishes the climate draw from those of the seeds (1, 2) and of the names (10, 20) of a district.
const climateSalt = 30

// Key is the identifier of the climate (translation key "climate.<key>").
func (c Climate) Key() string {
	switch c {
	case Tropical:
		return "tropical"
	case Arid:
		return "arid"
	case Boreal:
		return "boreal"
	}
	return "temperate"
}

// climateOf draws the climate of district id: 40 % temperate, 20 % each of the others.
func climateOf(seed int64, id TerritoryID) Climate {
	switch r := terrHash(seed, id.X, id.Y, climateSalt) % 5; r {
	case 0, 1:
		return Temperate
	case 2:
		return Tropical
	case 3:
		return Arid
	}
	return Boreal
}

// climateFrom derives the climate of a district from its mean temperature and humidity (generator v2): hot
// and humid gives tropical, hot and dry arid, cold boreal. Colours thus follow the terrain biomes.
func climateFrom(temp, wet float64) Climate {
	switch {
	case temp > 0.35 && wet > 0.1:
		return Tropical
	case temp > 0.35:
		return Arid
	case temp < -0.35:
		return Boreal
	}
	return Temperate
}

// ClimateAt returns the climate of point (x, y), that of its district.
func (w *World) ClimateAt(x, y int) Climate {
	return w.TerritoryAt(x, y).climate
}

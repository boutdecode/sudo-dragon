package world

// Climat: l'ambiance d'une contrée. Il est purement visuel (couleurs et glyphes du client) : il ne change ni le
// terrain, ni les monstres, ni la génération. Il se déduit de la graine et de la contrée par un hachage pur, donc le
// serveur et le client le calculent pareillement, sans rien échanger.

// Climate est le climat d'une contrée.
type Climate uint8

const (
	Temperate Climate = iota // les couleurs d'origine
	Tropical                 // verts vifs, mers turquoise
	Arid                     // plaines jaunes, collines rouges
	Boreal                   // verts froids et bleutés
	NumClimates
)

// climateSalt distingue le tirage du climat de ceux des germes (1, 2) et des noms (10, 20) d'une contrée.
const climateSalt = 30

// Key est l'identifiant du climat (clé de traduction "climate.<key>").
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

// climateOf tire le climat de la contrée id: 40 % tempéré, 20 % chacun des autres.
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

// ClimateAt renvoie le climat du point (x, y), celui de sa contrée.
func (w *World) ClimateAt(x, y int) Climate {
	return climateOf(w.Seed, territoryIDAt(w.Seed, x, y))
}

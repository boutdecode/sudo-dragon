package world

// Niveau de région: il croît avec l'éloignement du point (0, 0) et ne dépend d'aucune graine, si bien que le
// serveur et le client le calculent pareillement. Quatre tranches de cinq niveaux (1-5, 6-10, 11-15, 16-20),
// chacune large de trois régions (LevelBandSize tuiles, en distance de Chebyshev).
const (
	MaxLevel      = 20
	LevelsPerBand = 5
	LevelBands    = MaxLevel / LevelsPerBand
	LevelBandSize = 3 * RegionSize // distance qui sépare deux tranches
)

// LevelAt renvoie le niveau (1 à MaxLevel) de la tuile (x, y).
func LevelAt(x, y int) int {
	d := max(abs(x), abs(y))
	return min(1+d*LevelsPerBand/LevelBandSize, MaxLevel)
}

// LevelBand renvoie la tranche (0 à LevelBands-1) d'un niveau.
func LevelBand(level int) int {
	return min(max(level-1, 0)/LevelsPerBand, LevelBands-1)
}

// LevelRange renvoie les bornes (basse, haute) de la tranche du niveau.
func LevelRange(level int) (lo, hi int) {
	b := LevelBand(level)
	return b*LevelsPerBand + 1, (b + 1) * LevelsPerBand
}

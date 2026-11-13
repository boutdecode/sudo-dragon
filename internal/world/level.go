package world

// Region level: it grows with distance from point (0, 0) and depends on no seed, so the
// server and the client compute it the same way. Four bands of five levels (1-5, 6-10, 11-15, 16-20),
// each three regions wide (LevelBandSize tiles, in Chebyshev distance).
const (
	MaxLevel      = 20
	LevelsPerBand = 5
	LevelBands    = MaxLevel / LevelsPerBand
	LevelBandSize = 3 * RegionSize // distance between two bands
)

// LevelAt returns the level (1 to MaxLevel) of tile (x, y).
func LevelAt(x, y int) int {
	d := max(abs(x), abs(y))
	return min(1+d*LevelsPerBand/LevelBandSize, MaxLevel)
}

// LevelBand returns the band (0 to LevelBands-1) of a level.
func LevelBand(level int) int {
	return min(max(level-1, 0)/LevelsPerBand, LevelBands-1)
}

// LevelRange returns the bounds (low, high) of the band of the level.
func LevelRange(level int) (lo, hi int) {
	b := LevelBand(level)
	return b*LevelsPerBand + 1, (b + 1) * LevelsPerBand
}

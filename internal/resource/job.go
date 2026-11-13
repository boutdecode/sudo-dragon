package resource

// Métiers de récolte: un par catégorie de ressource (bois: bûcheron, minerais: mineur, plantes: herboriste,
// poissons: pêcheur). Le métier fixe la taille du sac de la catégorie et la chance de réussir une récolte, et
// progresse à chaque réussite. L'expérience est stockée cumulée: le niveau s'en déduit.

// Réglages d'équilibrage.
const (
	MaxJobLevel  = 10 // niveau maximal d'un métier
	baseCapacity = 5  // unités portées par catégorie au niveau 1
	capPerLevel  = 3  // unités gagnées par niveau
	baseChance   = 40 // pourcentage de réussite au niveau 1
	chancePerLvl = 6  // pourcentage gagné par niveau
	maxChance    = 95
	xpPerTier    = 5  // XP par palier de la ressource récoltée
	xpCurve      = 25 // XPToNext = xpCurve × niveau²
)

// XPToNext est l'expérience à accumuler dans le niveau donné pour passer au suivant.
func XPToNext(level int) int { return xpCurve * level * level }

// LevelOf renvoie le niveau (1 à MaxJobLevel) d'un métier qui a cumulé xp points.
func LevelOf(xp int) int {
	level := 1
	for level < MaxJobLevel && xp >= XPToNext(level) {
		xp -= XPToNext(level)
		level++
	}
	return level
}

// Progress renvoie le niveau, l'expérience acquise dans ce niveau et celle qu'il faut pour le suivant (0 au
// niveau maximal).
func Progress(xp int) (level, inLevel, next int) {
	level = 1
	for level < MaxJobLevel && xp >= XPToNext(level) {
		xp -= XPToNext(level)
		level++
	}
	if level >= MaxJobLevel {
		return level, 0, 0
	}
	return level, xp, XPToNext(level)
}

// Capacity est le nombre d'unités que le sac peut contenir pour une catégorie, au niveau donné.
func Capacity(level int) int { return baseCapacity + capPerLevel*(clampLevel(level)-1) }

// Chance est la chance (en pourcentage) de réussir une récolte au niveau donné.
func Chance(level int) int { return min(baseChance+chancePerLvl*(clampLevel(level)-1), maxChance) }

// GainXP est l'expérience d'une récolte réussie sur une ressource du palier donné.
func GainXP(tier int) int { return xpPerTier * max(tier, 1) }

func clampLevel(l int) int { return max(1, min(l, MaxJobLevel)) }

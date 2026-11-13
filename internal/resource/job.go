package resource

// Gathering jobs: one per resource category (wood: lumberjack, ore: miner, herbs: herbalist,
// fish: fisherman). The job sets the bag size of the category and the chance of a successful gather, and
// progresses with each success. Experience is stored cumulatively: the level is derived from it.

// Balancing settings.
const (
	MaxJobLevel  = 10 // maximum level of a job
	baseCapacity = 5  // units carried per category at level 1
	capPerLevel  = 3  // units gained per level
	baseChance   = 40 // success percentage at level 1
	chancePerLvl = 6  // percentage gained per level
	maxChance    = 95
	xpPerTier    = 5  // XP per tier of the gathered resource
	xpCurve      = 25 // XPToNext = xpCurve × level²
)

// XPToNext is the experience to accumulate within the given level to reach the next one.
func XPToNext(level int) int { return xpCurve * level * level }

// LevelOf returns the level (1 to MaxJobLevel) of a job that has accumulated xp points.
func LevelOf(xp int) int {
	level := 1
	for level < MaxJobLevel && xp >= XPToNext(level) {
		xp -= XPToNext(level)
		level++
	}
	return level
}

// Progress returns the level, the experience gained within that level and the amount needed for the next (0 at
// max level).
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

// Capacity is the number of units the bag can hold for a category, at the given level.
func Capacity(level int) int { return baseCapacity + capPerLevel*(clampLevel(level)-1) }

// Chance is the chance (as a percentage) of a successful gather at the given level.
func Chance(level int) int { return min(baseChance+chancePerLvl*(clampLevel(level)-1), maxChance) }

// GainXP is the experience of a successful gather on a resource of the given tier.
func GainXP(tier int) int { return xpPerTier * max(tier, 1) }

func clampLevel(l int) int { return max(1, min(l, MaxJobLevel)) }

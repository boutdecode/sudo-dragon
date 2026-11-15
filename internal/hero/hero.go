// Package hero contains the rules for a character's ability scores: dice rolls,
// modifiers, classes, hit points and mana. It depends on no other game package and
// receives its randomness as a parameter.
package hero

import (
	"math/rand/v2"
)

// Ability is one of the six abilities.
type Ability int

const (
	STR Ability = iota // strength: melee attacks
	AGI                // agility: ranged attacks
	INT                // intelligence: magic attacks
	CON                // constitution: hit points
	WIS                // wisdom: mana
	CHA                // charisma: how NPCs perceive the character
	NumAbilities
)

// Key is the identifier (translation key) of an ability.
func (a Ability) Key() string {
	return [...]string{"str", "agi", "int", "con", "wis", "cha"}[a]
}

// Scores holds the value of the six abilities (3 to 20 and beyond).
type Scores [NumAbilities]int

const (
	MinScore = 3
	MaxScore = 30 // upper bound when reading a save file
	MaxRolls = 3  // rerolls allowed at creation, in total
)

// Set reports whether abilities have been rolled (an all-zero sheet has not been created).
func (s Scores) Set() bool {
	for _, v := range s {
		if v != 0 {
			return true
		}
	}
	return false
}

// Default is the neutral sheet (10 everywhere), for a character without creation.
func Default() Scores { return Scores{10, 10, 10, 10, 10, 10} }

// Mod is the modifier of a value: 3 → −4, 4-5 → −3, 6-7 → −2, 8-9 → −1, 10-11 → 0,
// 12-13 → +1, etc.
func Mod(v int) int {
	d := v - 10
	if d >= 0 {
		return d / 2
	}
	return -((-d + 1) / 2)
}

// Mod returns the modifier of ability a.
func (s Scores) Mod(a Ability) int { return Mod(s[a]) }

// Sanitize clamps each value into [MinScore, MaxScore]; a sheet not created stays at zero.
func (s Scores) Sanitize() Scores {
	if !s.Set() {
		return Scores{}
	}
	for i, v := range s {
		s[i] = max(MinScore, min(v, MaxScore))
	}
	return s
}

// Roll3d6 rolls three six-sided dice and adds them (3 to 18).
func Roll3d6(rng *rand.Rand) int {
	return rng.IntN(6) + rng.IntN(6) + rng.IntN(6) + 3
}

// RollScores rolls the six abilities.
func RollScores(rng *rand.Rand) Scores {
	var s Scores
	for i := range s {
		s[i] = Roll3d6(rng)
	}
	return s
}

// Creation is a roll pending validation.
type Creation struct {
	Scores Scores
	Rolls  int // rerolls used
}

// NewCreation rolls a sheet.
func NewCreation(rng *rand.Rand) Creation { return Creation{Scores: RollScores(rng)} }

// Left is the number of rerolls remaining.
func (c Creation) Left() int { return MaxRolls - c.Rolls }

// Reroll rerolls one ability. It returns false, changing nothing, if the index is
// invalid or if rerolls are exhausted.
func (c *Creation) Reroll(a Ability, rng *rand.Rand) bool {
	if a < 0 || a >= NumAbilities || c.Rolls >= MaxRolls {
		return false
	}
	c.Scores[a] = Roll3d6(rng)
	c.Rolls++
	return true
}

// Class is the base of a character.
type Class string

const (
	Warrior Class = "warrior"
	Rogue   Class = "rogue"
	Mage    Class = "mage"
)

// Classes in display order.
var Classes = []Class{Warrior, Rogue, Mage}

// Valid reports whether c is a known class.
func (c Class) Valid() bool { return c == Warrior || c == Rogue || c == Mage }

// OrDefault returns c, or warrior if it is unknown.
func (c Class) OrDefault() Class {
	if c.Valid() {
		return c
	}
	return Warrior
}

// Race is the people a character belongs to: it adds RaceBonus to one ability.
type Race string

const (
	Human    Race = "human"
	Elf      Race = "elf"
	Dwarf    Race = "dwarf"
	Halfling Race = "halfling"
)

// Races in display order.
var Races = []Race{Human, Elf, Dwarf, Halfling}

// NumRaces is the number of races (len(Races)), to size tables indexed by Race.Index.
const NumRaces = 4

// Index is the position of the race in Races (an unknown race is human).
func (r Race) Index() int {
	switch r {
	case Elf:
		return 1
	case Dwarf:
		return 2
	case Halfling:
		return 3
	}
	return 0
}

// RaceBonus is the ability bonus granted by a race at creation.
const RaceBonus = 2

// Valid reports whether r is a known race.
func (r Race) Valid() bool { return r == Human || r == Elf || r == Dwarf || r == Halfling }

// OrDefault returns r, or human if it is unknown.
func (r Race) OrDefault() Race {
	if r.Valid() {
		return r
	}
	return Human
}

// Ability is the ability raised by the race (an unknown race is human).
func (r Race) Ability() Ability {
	switch r {
	case Elf:
		return INT
	case Dwarf:
		return CON
	case Halfling:
		return AGI
	}
	return STR
}

// Apply returns the scores with the racial bonus added.
func (r Race) Apply(s Scores) Scores {
	s[r.Ability()] += RaceBonus
	return s
}

// HitDie is the hit die of the class.
func (c Class) HitDie() int {
	switch c {
	case Rogue:
		return 8
	case Mage:
		return 6
	}
	return 12
}

// ManaBase is the starting mana of the class.
func (c Class) ManaBase() int {
	switch c {
	case Rogue:
		return 4
	case Mage:
		return 8
	}
	return 2
}

// FirstHP: HP at level 1 (hit die at maximum + CON modifier, at least 1).
func FirstHP(c Class, s Scores) int { return max(1, c.OrDefault().HitDie()+s.Mod(CON)) }

// LevelHP: HP gained at a level (hit die rolled + CON modifier, at least 1).
func LevelHP(c Class, s Scores, rng *rand.Rand) int {
	return max(1, rng.IntN(c.OrDefault().HitDie())+1+s.Mod(CON))
}

// RollHP computes the HP of a character of level level, one die per level beyond the first.
func RollHP(c Class, s Scores, level int, rng *rand.Rand) int {
	hp := FirstHP(c, s)
	for l := 2; l <= level; l++ {
		hp += LevelHP(c, s, rng)
	}
	return hp
}

// SanitizeHP clamps max HP read from a save file into the possible bounds.
func SanitizeHP(hp int, c Class, s Scores, level int) int {
	level = max(1, level)
	hi := max(level, FirstHP(c, s)+(level-1)*max(1, c.OrDefault().HitDie()+s.Mod(CON)))
	return max(level, min(hp, hi))
}

// MaxMana is the mana reserve (WIS makes it vary).
func MaxMana(c Class, s Scores, level int) int {
	level = max(1, level)
	return max(0, c.OrDefault().ManaBase()+2*(level-1)+level*s.Mod(WIS))
}

// Kind is the attack style of a weapon.
type Kind string

const (
	Melee  Kind = "melee"
	Ranged Kind = "ranged"
	Magic  Kind = "magic"
)

// Valid reports whether k is a known style.
func (k Kind) Valid() bool { return k == Melee || k == Ranged || k == Magic }

// OrMelee returns k, or melee if it is unknown (or empty: no weapon).
func (k Kind) OrMelee() Kind {
	if k.Valid() {
		return k
	}
	return Melee
}

// Stat is the ability that drives this style.
func (k Kind) Stat() Ability {
	switch k {
	case Ranged:
		return AGI
	case Magic:
		return INT
	}
	return STR
}

// SpellCost is the mana spent by a magic attack.
const SpellCost = 2

// Size is the size of a creature: it sets its base DEF.
type Size string

const (
	Small  Size = "small"  // small creatures
	Medium Size = "medium" // humanoids and medium creatures (the players)
	Large  Size = "large"  // large creatures
)

// DefBase is the base DEF of a size (8, 10 or 12); an unknown size is medium.
func DefBase(s Size) int {
	switch s {
	case Small:
		return 8
	case Large:
		return 12
	}
	return 10
}

// DefenseBonus is the DEF bonus from agility (the AGI modifier).
func (s Scores) DefenseBonus() int { return s.Mod(AGI) }

// Hits reports whether a d20 roll hits: a natural 20 always hits, a natural 1 always misses,
// otherwise it must be strictly greater than the DEF with the bonus.
func Hits(roll, bonus, def int) bool {
	switch roll {
	case 20:
		return true
	case 1:
		return false
	}
	return roll+bonus > def
}

// HitChance is the probability (0 to 1) of hitting a DEF with a bonus, per Hits.
func HitChance(bonus, def int) float64 {
	n := 0
	for r := 1; r <= 20; r++ {
		if Hits(r, bonus, def) {
			n++
		}
	}
	return float64(n) / 20
}

// PriceAdjust is the price adjustment (in %) from the CHA modifier: −2% per positive
// point, +2% per negative point, capped at ±10%.
func PriceAdjust(mod int) int { return max(-10, min(10, -2*mod)) }

// AdjustPrice applies charisma to a price (never less than 1 for a positive price).
func AdjustPrice(price, chaMod int) int {
	if price <= 0 {
		return price
	}
	return max(1, price*(100+PriceAdjust(chaMod))/100)
}

// Impression is the translation key of the impression tier the character makes.
func Impression(mod int) string {
	switch {
	case mod <= -3:
		return "repellent"
	case mod <= -1:
		return "awkward"
	case mod == 0:
		return "ordinary"
	case mod <= 2:
		return "pleasant"
	}
	return "charismatic"
}

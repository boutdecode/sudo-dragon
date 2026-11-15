package combat

import (
	"math/rand/v2"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
)

// Stats is the progression of a character.
type Stats struct {
	Level, XP int
	MaxHP     int // sum of the hit dice rolled (see hero), stored: it is not recomputed
	MaxMana   int
	Potions   int
	Gold      int

	// Class and abilities rolled at creation. STR, AGI and INT give the hit and damage
	// bonus (depending on the weapon style), AGI the DEF; CON and WIS have already been used to set MaxHP and MaxMana.
	Class  hero.Class
	Race   hero.Race // racial bonus already included in Scores
	Scores hero.Scores

	// Facts established by the server about the character's history. They are not used
	// in combat: NPCs may cite them as certain, unlike
	// what the player says about himself.
	Kills     int // monsters defeated
	BossKills int // warlords defeated
	Deaths    int // defeats
}

// MaxGold caps a character's gold (prevents any integer overflow).
const MaxGold = 999_999

// Starting values and per-level gains.
const (
	startPotions = 2
)

// BareHands is the damage when bare-handed (before the STR modifier).
var BareHands = dice.D(1, 4, 0)

// NewStats returns a level 1 character: a warrior with neutral abilities (10).
func NewStats() Stats { return NewStatsFor(hero.Warrior, hero.Default()) }

// NewStatsFor returns a level 1 character of the given class and abilities.
func NewStatsFor(class hero.Class, scores hero.Scores) Stats {
	class = class.OrDefault()
	if !scores.Set() {
		scores = hero.Default()
	}
	scores = scores.Sanitize()
	return Stats{
		Level: 1, MaxHP: hero.FirstHP(class, scores), MaxMana: hero.MaxMana(class, scores, 1),
		Potions: startPotions, Class: class, Race: hero.Human, Scores: scores,
	}
}

// Mod returns the attack modifier of style k (STR, AGI or INT).
func (s Stats) Mod(k hero.Kind) int { return s.Scores.Mod(k.OrMelee().Stat()) }

// Defense is the DEF of a character without equipment: 10 (medium size) plus the AGI modifier.
func (s Stats) Defense() int { return hero.DefBase(hero.Medium) + s.Scores.DefenseBonus() }

// averageHP is the "average" HP gain of a level (half the die, plus one, plus the CON mod).
func (s Stats) averageHP() int {
	return max(1, s.Class.OrDefault().HitDie()/2+1+s.Scores.Mod(hero.CON))
}

// xpCurve sets the experience curve: XPToNext is xpCurve × level². Level 20 requires ~300,000 XP, i.e.
// ~60 h of active play (30 days at ~2 h a day); changing this coefficient is enough to slow down or speed up.
const xpCurve = 120

// XPToNext is the experience to accumulate (within the current level) to reach the next one.
func (s Stats) XPToNext() int { return xpCurve * s.Level * s.Level }

// AddXP adds experience and applies level-ups: each level rolls the hit die
// of the class (plus the CON mod). With a nil rng, the gain is the average value of the die
// (deterministic, for tests). It returns the number of levels gained.
func (s *Stats) AddXP(n int, rng *rand.Rand) (levels int) {
	s.XP += max(0, n)
	for s.XP >= s.XPToNext() {
		s.XP -= s.XPToNext()
		s.Level++
		if rng != nil {
			s.MaxHP += hero.LevelHP(s.Class, s.Scores, rng)
		} else {
			s.MaxHP += s.averageHP()
		}
		s.MaxMana = hero.MaxMana(s.Class, s.Scores, s.Level)
		levels++
	}
	return levels
}

// AddGold adds gold up to the cap and returns the amount actually added.
func (s *Stats) AddGold(n int) int {
	added := max(0, min(n, MaxGold-s.Gold))
	s.Gold += added
	return added
}

// SpendGold removes n gold pieces. It returns false, removing nothing, if gold is short
// or if n is negative.
func (s *Stats) SpendGold(n int) bool {
	if n < 0 || n > s.Gold {
		return false
	}
	s.Gold -= n
	return true
}

// WithGold returns the sheet with the given gold, clamped into [0, MaxGold]. Used to
// restore a save without trusting the value read.
func (s Stats) WithGold(gold int) Stats {
	s.Gold = max(0, min(gold, MaxGold))
	return s
}

// WithRace returns the sheet with the given race (human if unknown). The bonus is not
// applied: it is already part of the saved scores.
func (s Stats) WithRace(r hero.Race) Stats {
	s.Race = r.OrDefault()
	return s
}

// WithRecord returns the sheet with the given history, each counter clamped to 0 or more.
func (s Stats) WithRecord(kills, bossKills, deaths int) Stats {
	s.Kills, s.BossKills, s.Deaths = max(0, kills), max(0, bossKills), max(0, deaths)
	return s
}

// AddPotions adds potions up to the cap and returns the number actually added.
func (s *Stats) AddPotions(n int) int {
	added := max(0, min(n, MaxPotions-s.Potions))
	s.Potions += added
	return added
}

// Fighter converts the sheet into a combatant with hp current hit points.
func (s Stats) Fighter(hp int) Fighter {
	m := s.Mod(hero.Melee)
	return Fighter{HP: hp, MaxHP: s.MaxHP, Def: s.Defense(), Hit: m, Dmg: BareHands, DmgMod: m, Kind: hero.Melee}
}

// StatsAt rebuilds the sheet of a warrior with neutral abilities from his
// level, his experience within that level and his potions, with "average" HP.
func StatsAt(level, xp, potions int) Stats {
	return StatsFor(level, xp, potions, hero.Warrior, hero.Default(), 0)
}

// StatsFor rebuilds the sheet of a saved character. Out-of-range values are
// clamped into the valid bounds (corrupted save); maxHP <= 0 gives average HP.
func StatsFor(level, xp, potions int, class hero.Class, scores hero.Scores, maxHP int) Stats {
	level = max(1, level)
	s := NewStatsFor(class, scores)
	if maxHP <= 0 {
		maxHP = s.MaxHP + (level-1)*s.averageHP()
	}
	s.Level = level
	s.MaxHP = hero.SanitizeHP(maxHP, s.Class, s.Scores, level)
	s.MaxMana = hero.MaxMana(s.Class, s.Scores, level)
	s.Potions = max(0, min(potions, MaxPotions))
	s.XP = max(0, min(xp, s.XPToNext()-1))
	return s
}

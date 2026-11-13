// Package combat contains the rules of turn-based combat and the
// progression of a character. It depends on no other game package and
// receives its randomness as a parameter, which makes it fully deterministic in tests.
package combat

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Action is a player's choice for a turn.
type Action int

const (
	Attack Action = iota + 1
	Item
	Flee
)

// ParseAction converts the name of an action received from the network.
func ParseAction(s string) (Action, bool) {
	switch s {
	case "attack":
		return Attack, true
	case "item":
		return Item, true
	case "flee":
		return Flee, true
	}
	return 0, false
}

// Result is the outcome of a battle.
type Result int

const (
	Ongoing Result = iota
	Won
	Lost
	Fled
)

// String returns the outcome identifier used in the protocol.
func (r Result) String() string {
	switch r {
	case Won:
		return "won"
	case Lost:
		return "lost"
	case Fled:
		return "fled"
	}
	return ""
}

// Balancing settings.
const (
	HealAmount   = 15  // HP restored by a potion
	MaxPotions   = 9   // inventory cap
	FleeChance   = 0.5 // chance of a successful flee
	critRoll     = 20  // a natural 20 always hits and doubles the dice
	fumbleRoll   = 1   // a natural 1 always misses
	minimumDamge = 1   // a hit deals at least this much, reduction included
)

// Fighter is a combatant. To hit: d20 + Hit must exceed the opponent's DEF. Damage: Dmg (dice and
// weapon quality) + DmgMod, minus the opponent's Reduction dice.
type Fighter struct {
	HP, MaxHP int
	Init      int       // initiative roll modifier (AGI for a player)
	Def       int       // total DEF (size, agility, equipment)
	Hit       int       // bonus to the hit roll
	Dmg       dice.Dice // damage dice
	DmgMod    int       // ability modifier added to damage
	Reduction dice.Dice // dice subtracted from damage taken (armour)

	// Weapon style (empty: melee) and mana (only players have any).
	Kind          hero.Kind
	Mana, MaxMana int

	// Without mana, a magic weapon is used as a club: the wielder's hit and STR modifier.
	MeleeHit, MeleeMod int
}

// Foe is a monster engaged in a battle. A slain monster stays in the list with HP == 0, so that
// indices remain stable.
type Foe struct {
	Fighter
	Name string // for messages, e.g. "Rat 2" (already in the player's language)
	Kind string // species
	XP   int    // used to designate the strongest
}

// Alive reports whether the monster is still standing.
func (f Foe) Alive() bool { return f.HP > 0 }

// Strike is a resolved attack, for the client's visual effects.
type Strike struct {
	ByPlayer bool // the player strikes (otherwise a monster)
	Foe      int  // index of the monster concerned (target or attacker)
	Dmg      int  // damage dealt (0 if missed)
	Hit      bool
	Crit     bool
}

// Battle is a fight between a player and one or more monsters.
type Battle struct {
	Player  Fighter
	Foes    []Foe
	Lang    i18n.Lang // language of the log; the zero value is English
	Potions int
	Round   int
	Result  Result

	// Events lists the attacks of the last call to Start or Act.
	Events []Strike

	rng *rand.Rand
}

// New prepares a duel against a single monster. rng must not be nil.
func New(player Fighter, potions int, monster Fighter, monsterName string, rng *rand.Rand) *Battle {
	return NewGroup(player, potions, []Foe{{Fighter: monster, Name: monsterName}}, rng)
}

// NewGroup prepares a battle against several monsters.
func NewGroup(player Fighter, potions int, foes []Foe, rng *rand.Rand) *Battle {
	return &Battle{Player: player, Foes: foes, Potions: potions, rng: rng}
}

// FirstAlive returns the index of the first living monster (-1 if none remain).
func (b *Battle) FirstAlive() int {
	for i, f := range b.Foes {
		if f.Alive() {
			return i
		}
	}
	return -1
}

// Strongest returns the index of the strongest living monster: most XP, then most max HP, then the first
// (-1 if none remain).
func (b *Battle) Strongest() int {
	best := -1
	for i, f := range b.Foes {
		if !f.Alive() {
			continue
		}
		if best < 0 || f.XP > b.Foes[best].XP || (f.XP == b.Foes[best].XP && f.MaxHP > b.Foes[best].MaxHP) {
			best = i
		}
	}
	return best
}

func (b *Battle) t(key string, args ...any) string { return i18n.T(b.Lang, key, args...) }

// blow is the result of an attack.
type blow struct {
	roll      int // natural d20
	total     int // d20 + bonus
	hit, crit bool
	damage    int // damage dealt (0 if missed)
	reduced   int // part removed by armour
	targetDef int
	rollText  string
}

// strike resolves an attack by att against a target of DEF def and reduction red.
func (b *Battle) strike(att Fighter, def int, red dice.Dice) blow {
	r := b.rng.IntN(20) + 1
	bl := blow{roll: r, total: r + att.Hit, targetDef: def}
	bl.hit = hero.Hits(r, att.Hit, def)
	bl.crit = bl.hit && r == critRoll
	bl.rollText = b.t("combat.roll", r, att.Hit, bl.total, def)
	if !bl.hit {
		return bl
	}
	var raw int
	if bl.crit {
		raw = att.Dmg.RollCrit(b.rng)
	} else {
		raw = att.Dmg.Roll(b.rng)
	}
	raw += att.DmgMod
	bl.damage = max(minimumDamge, raw-red.Roll(b.rng))
	bl.reduced = max(0, raw-bl.damage)
	return bl
}

// with adds to the message the part removed by armour.
func (b *Battle) reducedNote(bl blow) string {
	if bl.reduced <= 0 {
		return ""
	}
	return " " + b.t("combat.reduced", bl.reduced)
}

// Act resolves a full turn: the player's action (target designates the targeted monster; if invalid or slain, the
// first living one) then the retaliation of each living monster. It returns the log lines of the turn. Using a potion
// without having one does not consume the turn. No effect if the battle is over.
func (b *Battle) Act(a Action, target int) []string {
	if b.Result != Ongoing {
		return nil
	}
	b.Events = b.Events[:0]
	var log []string
	if target < 0 || target >= len(b.Foes) || !b.Foes[target].Alive() {
		target = b.FirstAlive()
	}

	switch a {
	case Attack:
		foe := &b.Foes[target]
		kind := b.Player.Kind.OrMelee()
		att, clubbed := b.Player, false
		if kind == hero.Magic {
			if att.Mana < hero.SpellCost { // no mana: strike with the weapon held as a club
				att.Hit, att.Dmg, att.DmgMod, clubbed = att.MeleeHit, BareHands, att.MeleeMod, true
			} else {
				b.Player.Mana -= hero.SpellCost
			}
		}
		bl := b.strike(att, foe.Def, foe.Reduction)
		b.Events = append(b.Events, Strike{ByPlayer: true, Foe: target, Dmg: bl.damage, Hit: bl.hit, Crit: bl.crit})
		switch {
		case !bl.hit:
			log = append(log, bl.rollText+" "+b.t("combat.player_miss", foe.Name))
		default:
			foe.HP = max(0, foe.HP-bl.damage)
			var msg string
			switch {
			case bl.crit:
				msg = b.t("combat.player_crit", bl.damage, foe.Name)
			case clubbed:
				msg = b.t("combat.player_club", foe.Name, bl.damage)
			case kind == hero.Ranged:
				msg = b.t("combat.player_shot", foe.Name, bl.damage)
			case kind == hero.Magic:
				msg = b.t("combat.player_spell", foe.Name, bl.damage)
			default:
				msg = b.t("combat.player_hit", foe.Name, bl.damage)
			}
			log = append(log, bl.rollText+" "+msg+b.reducedNote(bl))
			if !foe.Alive() {
				log = append(log, b.t("combat.monster_dead", foe.Name))
			}
		}
	case Item:
		if b.Potions <= 0 {
			return []string{b.t("combat.no_potion")}
		}
		b.Potions--
		healed := min(HealAmount, b.Player.MaxHP-b.Player.HP)
		b.Player.HP += healed
		log = append(log, b.t("combat.drink", healed))
	case Flee:
		if b.rng.Float64() < FleeChance {
			b.Result = Fled
			return append(log, b.t("combat.flee_ok"))
		}
		log = append(log, b.t("combat.flee_fail"))
	default:
		return []string{b.t("combat.unknown_action")}
	}

	if b.FirstAlive() < 0 {
		b.Result = Won
		return log
	}

	// Retaliation of each living monster.
	for i, foe := range b.Foes {
		if !foe.Alive() {
			continue
		}
		var died bool
		if log, died = b.foeStrikes(i, log); died {
			return log
		}
	}
	b.Round++
	return log
}

// foeStrikes resolves a monster's attack on the player. It returns the completed log and whether the player died
// (the battle is then lost).
func (b *Battle) foeStrikes(i int, log []string) ([]string, bool) {
	foe := b.Foes[i]
	bl := b.strike(foe.Fighter, b.Player.Def, b.Player.Reduction)
	b.Events = append(b.Events, Strike{Foe: i, Dmg: bl.damage, Hit: bl.hit, Crit: bl.crit})
	switch {
	case !bl.hit:
		log = append(log, bl.rollText+" "+b.t("combat.monster_miss", foe.Name))
	default:
		b.Player.HP = max(0, b.Player.HP-bl.damage)
		msg := b.t("combat.monster_hit", foe.Name, bl.damage)
		if bl.crit {
			msg = b.t("combat.monster_crit", foe.Name, bl.damage)
		}
		log = append(log, bl.rollText+" "+msg+b.reducedNote(bl))
	}
	if b.Player.HP <= 0 {
		b.Result = Lost
		return append(log, b.t("combat.you_died")), true
	}
	return log, false
}

// Start rolls initiative (d20 + modifier) for the player and each monster. Monsters that beat the player
// (strictly: on a tie, the player goes first) strike before his first turn, from fastest to slowest. To be
// called once, at the start of the battle. It returns the log lines.
func (b *Battle) Start() []string {
	type roll struct{ idx, total int }
	b.Events = b.Events[:0]
	pr := b.rng.IntN(20) + 1
	playerTotal := pr + b.Player.Init
	var foes []string
	var faster []roll
	for i, f := range b.Foes {
		r := b.rng.IntN(20) + 1 + f.Init
		foes = append(foes, fmt.Sprintf("%s %d", f.Name, r))
		if r > playerTotal {
			faster = append(faster, roll{i, r})
		}
	}
	log := []string{b.t("combat.init", playerTotal, pr, b.Player.Init, strings.Join(foes, ", "))}
	if len(faster) == 0 {
		return append(log, b.t("combat.init_you"))
	}
	slices.SortStableFunc(faster, func(x, y roll) int { return y.total - x.total })
	log = append(log, b.t("combat.init_foes"))
	for _, f := range faster {
		var died bool
		if log, died = b.foeStrikes(f.idx, log); died {
			break
		}
	}
	return log
}

// Abandon ends the battle as a flee (player inactivity).
func (b *Battle) Abandon() {
	if b.Result == Ongoing {
		b.Result = Fled
	}
}

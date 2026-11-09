// Package combat contient les règles du combat tour par tour et la
// progression d'un personnage. Il ne dépend d'aucun autre paquet du jeu et
// reçoit son aléa en paramètre, ce qui le rend entièrement déterministe en test.
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

// Action est le choix d'un joueur pour un tour.
type Action int

const (
	Attack Action = iota + 1
	Item
	Flee
)

// ParseAction convertit le nom d'une action reçu du réseau.
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

// Result est l'issue d'un combat.
type Result int

const (
	Ongoing Result = iota
	Won
	Lost
	Fled
)

// String renvoie l'identifiant d'issue utilisé dans le protocole.
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

// Réglages d'équilibrage.
const (
	HealAmount   = 15  // PV rendus par une potion
	MaxPotions   = 9   // plafond de l'inventaire
	FleeChance   = 0.5 // chance de réussir une fuite
	critRoll     = 20  // le 20 naturel touche toujours et double les dés
	fumbleRoll   = 1   // le 1 naturel rate toujours
	minimumDamge = 1   // un coup qui touche inflige au moins ceci, réduction comprise
)

// Fighter est un combattant. Toucher: d20 + Hit doit dépasser la DEF de l'adversaire. Dégâts: Dmg (dés et
// qualité de l'arme) + DmgMod, moins les dés de Reduction de l'adversaire.
type Fighter struct {
	HP, MaxHP int
	Init      int       // modificateur du jet d'initiative (AGI pour un joueur)
	Def       int       // DEF totale (taille, agilité, équipement)
	Hit       int       // bonus au jet de toucher
	Dmg       dice.Dice // dés de dégâts
	DmgMod    int       // modificateur de caractéristique ajouté aux dégâts
	Reduction dice.Dice // dés retirés aux dégâts reçus (armure)

	// Style de l'arme (vide: mêlée) et mana (seuls les joueurs en ont).
	Kind          hero.Kind
	Mana, MaxMana int

	// Sans mana, une arme magique sert de gourdin: toucher et modificateur de FOR du porteur.
	MeleeHit, MeleeMod int
}

// Foe est un monstre engagé dans un combat. Un monstre abattu reste dans la liste avec HP == 0, pour que les
// indices restent stables.
type Foe struct {
	Fighter
	Name string // pour les messages, ex. "Rat 2" (déjà dans la langue du joueur)
	Kind string // espèce
	XP   int    // sert à désigner le plus fort
}

// Alive dit si le monstre est encore debout.
func (f Foe) Alive() bool { return f.HP > 0 }

// Battle est un combat entre un joueur et un ou plusieurs monstres.
type Battle struct {
	Player  Fighter
	Foes    []Foe
	Lang    i18n.Lang // langue du journal; le zéro vaut le français
	Potions int
	Round   int
	Result  Result

	rng *rand.Rand
}

// New prépare un duel contre un seul monstre. rng ne doit pas être nil.
func New(player Fighter, potions int, monster Fighter, monsterName string, rng *rand.Rand) *Battle {
	return NewGroup(player, potions, []Foe{{Fighter: monster, Name: monsterName}}, rng)
}

// NewGroup prépare un combat contre plusieurs monstres.
func NewGroup(player Fighter, potions int, foes []Foe, rng *rand.Rand) *Battle {
	return &Battle{Player: player, Foes: foes, Potions: potions, rng: rng}
}

// FirstAlive renvoie l'indice du premier monstre vivant (-1 s'il n'en reste aucun).
func (b *Battle) FirstAlive() int {
	for i, f := range b.Foes {
		if f.Alive() {
			return i
		}
	}
	return -1
}

// Strongest renvoie l'indice du monstre vivant le plus fort: le plus d'XP, puis le plus de PV max, puis le premier
// (-1 s'il n'en reste aucun).
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

// blow est le résultat d'une attaque.
type blow struct {
	roll      int // d20 naturel
	total     int // d20 + bonus
	hit, crit bool
	damage    int // dégâts infligés (0 si raté)
	reduced   int // part retirée par l'armure
	targetDef int
	rollText  string
}

// strike résout une attaque de att contre une cible de DEF def et de réduction red.
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

// with ajoute au message la part retirée par l'armure.
func (b *Battle) reducedNote(bl blow) string {
	if bl.reduced <= 0 {
		return ""
	}
	return " " + b.t("combat.reduced", bl.reduced)
}

// Act résout un tour complet: l'action du joueur (target désigne le monstre visé; s'il est invalide ou abattu, le
// premier vivant) puis la riposte de chaque monstre vivant. Renvoie les lignes de journal du tour. Utiliser une potion
// sans en avoir ne consomme pas le tour. Sans effet si le combat est terminé.
func (b *Battle) Act(a Action, target int) []string {
	if b.Result != Ongoing {
		return nil
	}
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
			if att.Mana < hero.SpellCost { // sans mana: coup de l'arme tenue comme un gourdin
				att.Hit, att.Dmg, att.DmgMod, clubbed = att.MeleeHit, BareHands, att.MeleeMod, true
			} else {
				b.Player.Mana -= hero.SpellCost
			}
		}
		bl := b.strike(att, foe.Def, foe.Reduction)
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

	// Riposte de chaque monstre vivant.
	for _, foe := range b.Foes {
		if !foe.Alive() {
			continue
		}
		var died bool
		if log, died = b.foeStrikes(foe, log); died {
			return log
		}
	}
	b.Round++
	return log
}

// foeStrikes résout l'attaque d'un monstre contre le joueur. Renvoie le journal complété et si le joueur est mort
// (le combat est alors perdu).
func (b *Battle) foeStrikes(foe Foe, log []string) ([]string, bool) {
	bl := b.strike(foe.Fighter, b.Player.Def, b.Player.Reduction)
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

// Start tire l'initiative (d20 + modificateur) du joueur et de chaque monstre. Les monstres qui devancent le joueur
// (strictement: à égalité, le joueur commence) frappent avant son premier tour, du plus rapide au plus lent. À
// appeler une fois, au début du combat. Renvoie les lignes de journal.
func (b *Battle) Start() []string {
	type roll struct{ idx, total int }
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
		if log, died = b.foeStrikes(b.Foes[f.idx], log); died {
			break
		}
	}
	return log
}

// Abandon termine le combat comme une fuite (inactivité du joueur).
func (b *Battle) Abandon() {
	if b.Result == Ongoing {
		b.Result = Fled
	}
}

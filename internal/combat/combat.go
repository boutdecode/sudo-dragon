// Package combat contient les règles du combat tour par tour et la
// progression d'un personnage. Il ne dépend d'aucun autre paquet du jeu et
// reçoit son aléa en paramètre, ce qui le rend entièrement déterministe en test.
package combat

import (
	"math/rand/v2"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Action est le choix d'un joueur pour un tour.
type Action int

const (
	Attack Action = iota + 1
	Defend
	Item
	Flee
)

// ParseAction convertit le nom d'une action reçu du réseau.
func ParseAction(s string) (Action, bool) {
	switch s {
	case "attack":
		return Attack, true
	case "defend":
		return Defend, true
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
	fleeChance   = 0.5 // chance de réussir une fuite
	DefendBonus  = 4   // bonus de DEF de l'action Défendre, jusqu'à la riposte
	critRoll     = 20  // le 20 naturel touche toujours et double les dés
	fumbleRoll   = 1   // le 1 naturel rate toujours
	minimumDamge = 1   // un coup qui touche inflige au moins ceci, réduction comprise
)

// Fighter est un combattant. Toucher: d20 + Hit doit dépasser la DEF de l'adversaire. Dégâts: Dmg (dés et
// qualité de l'arme) + DmgMod, moins les dés de Reduction de l'adversaire.
type Fighter struct {
	HP, MaxHP int
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

// Battle est un duel entre un joueur et un monstre.
type Battle struct {
	Player      Fighter
	Monster     Fighter
	MonsterName string    // pour les messages, ex. "Rat" (déjà dans la langue du joueur)
	Lang        i18n.Lang // langue du journal; le zéro vaut le français
	Potions     int
	Round       int
	Result      Result

	rng *rand.Rand
}

// New prépare un combat. rng ne doit pas être nil.
func New(player Fighter, potions int, monster Fighter, monsterName string, rng *rand.Rand) *Battle {
	return &Battle{Player: player, Monster: monster, MonsterName: monsterName, Potions: potions, rng: rng}
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

// Act résout un tour complet: l'action du joueur puis la riposte du monstre.
// Renvoie les lignes de journal du tour. Utiliser une potion sans en avoir ne
// consomme pas le tour. Sans effet si le combat est terminé.
func (b *Battle) Act(a Action) []string {
	if b.Result != Ongoing {
		return nil
	}
	var log []string
	defending := false

	switch a {
	case Attack:
		kind := b.Player.Kind.OrMelee()
		att, clubbed := b.Player, false
		if kind == hero.Magic {
			if att.Mana < hero.SpellCost { // sans mana: coup de l'arme tenue comme un gourdin
				att.Hit, att.Dmg, att.DmgMod, clubbed = att.MeleeHit, BareHands, att.MeleeMod, true
			} else {
				b.Player.Mana -= hero.SpellCost
			}
		}
		bl := b.strike(att, b.Monster.Def, b.Monster.Reduction)
		switch {
		case !bl.hit:
			log = append(log, bl.rollText+" "+b.t("combat.player_miss", b.MonsterName))
		default:
			b.Monster.HP = max(0, b.Monster.HP-bl.damage)
			var msg string
			switch {
			case bl.crit:
				msg = b.t("combat.player_crit", bl.damage, b.MonsterName)
			case clubbed:
				msg = b.t("combat.player_club", b.MonsterName, bl.damage)
			case kind == hero.Ranged:
				msg = b.t("combat.player_shot", b.MonsterName, bl.damage)
			case kind == hero.Magic:
				msg = b.t("combat.player_spell", b.MonsterName, bl.damage)
			default:
				msg = b.t("combat.player_hit", b.MonsterName, bl.damage)
			}
			log = append(log, bl.rollText+" "+msg+b.reducedNote(bl))
		}
	case Defend:
		defending = true
		log = append(log, b.t("combat.defend", DefendBonus))
	case Item:
		if b.Potions <= 0 {
			return []string{b.t("combat.no_potion")}
		}
		b.Potions--
		healed := min(HealAmount, b.Player.MaxHP-b.Player.HP)
		b.Player.HP += healed
		log = append(log, b.t("combat.drink", healed))
	case Flee:
		if b.rng.Float64() < fleeChance {
			b.Result = Fled
			return append(log, b.t("combat.flee_ok"))
		}
		log = append(log, b.t("combat.flee_fail"))
	default:
		return []string{b.t("combat.unknown_action")}
	}

	if b.Monster.HP <= 0 {
		b.Result = Won
		return append(log, b.t("combat.monster_dead", b.MonsterName))
	}

	// Riposte du monstre.
	def := b.Player.Def
	if defending {
		def += DefendBonus
	}
	bl := b.strike(b.Monster, def, b.Player.Reduction)
	switch {
	case !bl.hit:
		log = append(log, bl.rollText+" "+b.t("combat.monster_miss", b.MonsterName))
	default:
		b.Player.HP = max(0, b.Player.HP-bl.damage)
		msg := b.t("combat.monster_hit", b.MonsterName, bl.damage)
		if bl.crit {
			msg = b.t("combat.monster_crit", b.MonsterName, bl.damage)
		}
		log = append(log, bl.rollText+" "+msg+b.reducedNote(bl))
	}
	if b.Player.HP <= 0 {
		b.Result = Lost
		log = append(log, b.t("combat.you_died"))
		return log
	}
	b.Round++
	return log
}

// Abandon termine le combat comme une fuite (inactivité du joueur).
func (b *Battle) Abandon() {
	if b.Result == Ongoing {
		b.Result = Fled
	}
}

// Package combat contient les règles du combat tour par tour et la
// progression d'un personnage. Il ne dépend d'aucun autre paquet du jeu et
// reçoit son aléa en paramètre, ce qui le rend entièrement déterministe en test.
package combat

import (
	"fmt"
	"math/rand/v2"
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
	HealAmount      = 15   // PV rendus par une potion
	MaxPotions      = 9    // plafond de l'inventaire
	fleeChance      = 0.5  // chance de réussir une fuite
	playerCritRate  = 0.10 // coups critiques (dégâts x2)
	monsterCritRate = 0.05
)

// Fighter est un combattant.
type Fighter struct {
	HP, MaxHP int
	Atk, Def  int
}

// Battle est un duel entre un joueur et un monstre.
type Battle struct {
	Player      Fighter
	Monster     Fighter
	MonsterName string // pour les messages, ex. "Rat"
	Potions     int
	Round       int
	Result      Result

	rng *rand.Rand
}

// New prépare un combat. rng ne doit pas être nil.
func New(player Fighter, potions int, monster Fighter, monsterName string, rng *rand.Rand) *Battle {
	return &Battle{Player: player, Monster: monster, MonsterName: monsterName, Potions: potions, rng: rng}
}

// damage calcule les dégâts d'une attaque: attaque moins défense, ±1, minimum 1.
func (b *Battle) damage(atk, def int, critRate float64) (dmg int, crit bool) {
	dmg = max(1, atk-def+b.rng.IntN(3)-1)
	if b.rng.Float64() < critRate {
		return dmg * 2, true
	}
	return dmg, false
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
		dmg, crit := b.damage(b.Player.Atk, b.Monster.Def, playerCritRate)
		b.Monster.HP = max(0, b.Monster.HP-dmg)
		if crit {
			log = append(log, fmt.Sprintf("Coup critique ! Vous infligez %d dégâts au %s.", dmg, b.MonsterName))
		} else {
			log = append(log, fmt.Sprintf("Vous frappez le %s: %d dégâts.", b.MonsterName, dmg))
		}
	case Defend:
		defending = true
		log = append(log, "Vous vous mettez en garde.")
	case Item:
		if b.Potions <= 0 {
			return []string{"Vous n'avez plus de potion."}
		}
		b.Potions--
		healed := min(HealAmount, b.Player.MaxHP-b.Player.HP)
		b.Player.HP += healed
		log = append(log, fmt.Sprintf("Vous buvez une potion: +%d PV.", healed))
	case Flee:
		if b.rng.Float64() < fleeChance {
			b.Result = Fled
			return append(log, "Vous prenez la fuite !")
		}
		log = append(log, "Vous n'arrivez pas à fuir !")
	default:
		return []string{"Action inconnue."}
	}

	if b.Monster.HP <= 0 {
		b.Result = Won
		return append(log, fmt.Sprintf("Le %s est vaincu !", b.MonsterName))
	}

	// Riposte du monstre.
	dmg, crit := b.damage(b.Monster.Atk, b.Player.Def, monsterCritRate)
	if defending {
		dmg = max(1, dmg/2)
	}
	b.Player.HP = max(0, b.Player.HP-dmg)
	switch {
	case crit:
		log = append(log, fmt.Sprintf("Le %s vous porte un coup terrible: %d dégâts !", b.MonsterName, dmg))
	case defending:
		log = append(log, fmt.Sprintf("Le %s attaque, vous parez en partie: %d dégâts.", b.MonsterName, dmg))
	default:
		log = append(log, fmt.Sprintf("Le %s vous attaque: %d dégâts.", b.MonsterName, dmg))
	}
	if b.Player.HP <= 0 {
		b.Result = Lost
		log = append(log, "Vous êtes terrassé…")
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

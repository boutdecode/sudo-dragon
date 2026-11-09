// Package hero contient les règles des caractéristiques d'un personnage : tirage des dés,
// modificateurs, classes, points de vie et mana. Il ne dépend d'aucun autre paquet du jeu et
// reçoit son aléa en paramètre.
package hero

import (
	"math/rand/v2"
	"sort"
)

// Ability est l'une des six caractéristiques.
type Ability int

const (
	STR Ability = iota // force : attaques de mêlée
	AGI                // agilité : attaques à distance
	INT                // intelligence : attaques magiques
	CON                // constitution : points de vie
	WIS                // sagesse : mana
	CHA                // charisme : perception par les PNJ
	NumAbilities
)

// Key est l'identifiant (clé de traduction) d'une caractéristique.
func (a Ability) Key() string {
	return [...]string{"str", "agi", "int", "con", "wis", "cha"}[a]
}

// Scores est la valeur des six caractéristiques (3 à 20 et plus).
type Scores [NumAbilities]int

const (
	MinScore = 3
	MaxScore = 30 // borne de lecture d'une sauvegarde
	MaxRolls = 3  // relances autorisées à la création, au total
)

// Set indique si des caractéristiques ont été tirées (une fiche à zéro n'a pas été créée).
func (s Scores) Set() bool {
	for _, v := range s {
		if v != 0 {
			return true
		}
	}
	return false
}

// Default est la fiche neutre (10 partout), pour un personnage sans création.
func Default() Scores { return Scores{10, 10, 10, 10, 10, 10} }

// Mod est le modificateur d'une valeur : 3 → −4, 4-5 → −3, 6-7 → −2, 8-9 → −1, 10-11 → 0,
// 12-13 → +1, etc.
func Mod(v int) int {
	d := v - 10
	if d >= 0 {
		return d / 2
	}
	return -((-d + 1) / 2)
}

// Mod renvoie le modificateur de la caractéristique a.
func (s Scores) Mod(a Ability) int { return Mod(s[a]) }

// Sanitize ramène chaque valeur dans [MinScore, MaxScore] ; une fiche non créée reste à zéro.
func (s Scores) Sanitize() Scores {
	if !s.Set() {
		return Scores{}
	}
	for i, v := range s {
		s[i] = max(MinScore, min(v, MaxScore))
	}
	return s
}

// Roll4d6 lance 4d6 et additionne les trois meilleurs dés.
func Roll4d6(rng *rand.Rand) int {
	d := [4]int{}
	for i := range d {
		d[i] = rng.IntN(6) + 1
	}
	sort.Ints(d[:])
	return d[1] + d[2] + d[3]
}

// RollScores tire les six caractéristiques.
func RollScores(rng *rand.Rand) Scores {
	var s Scores
	for i := range s {
		s[i] = Roll4d6(rng)
	}
	return s
}

// Creation est un tirage en cours de validation.
type Creation struct {
	Scores Scores
	Rolls  int // relances utilisées
}

// NewCreation tire une fiche.
func NewCreation(rng *rand.Rand) Creation { return Creation{Scores: RollScores(rng)} }

// Left est le nombre de relances restantes.
func (c Creation) Left() int { return MaxRolls - c.Rolls }

// Reroll relance une caractéristique. Renvoie false, sans rien changer, si l'indice est
// invalide ou si les relances sont épuisées.
func (c *Creation) Reroll(a Ability, rng *rand.Rand) bool {
	if a < 0 || a >= NumAbilities || c.Rolls >= MaxRolls {
		return false
	}
	c.Scores[a] = Roll4d6(rng)
	c.Rolls++
	return true
}

// Class est la base d'un personnage.
type Class string

const (
	Warrior Class = "warrior"
	Rogue   Class = "rogue"
	Mage    Class = "mage"
)

// Classes dans l'ordre d'affichage.
var Classes = []Class{Warrior, Rogue, Mage}

// Valid indique si c est une classe connue.
func (c Class) Valid() bool { return c == Warrior || c == Rogue || c == Mage }

// OrDefault renvoie c, ou guerrier si elle est inconnue.
func (c Class) OrDefault() Class {
	if c.Valid() {
		return c
	}
	return Warrior
}

// HitDie est le dé de vie de la classe.
func (c Class) HitDie() int {
	switch c {
	case Rogue:
		return 8
	case Mage:
		return 6
	}
	return 12
}

// ManaBase est la mana de départ de la classe.
func (c Class) ManaBase() int {
	switch c {
	case Rogue:
		return 4
	case Mage:
		return 8
	}
	return 2
}

// FirstHP : PV au niveau 1 (dé de vie au maximum + modificateur de CON, au moins 1).
func FirstHP(c Class, s Scores) int { return max(1, c.OrDefault().HitDie()+s.Mod(CON)) }

// LevelHP : PV gagnés à un niveau (dé de vie lancé + modificateur de CON, au moins 1).
func LevelHP(c Class, s Scores, rng *rand.Rand) int {
	return max(1, rng.IntN(c.OrDefault().HitDie())+1+s.Mod(CON))
}

// RollHP calcule les PV d'un personnage de niveau level, un dé par niveau au-delà du premier.
func RollHP(c Class, s Scores, level int, rng *rand.Rand) int {
	hp := FirstHP(c, s)
	for l := 2; l <= level; l++ {
		hp += LevelHP(c, s, rng)
	}
	return hp
}

// SanitizeHP ramène des PV maximaux lus d'une sauvegarde dans les bornes possibles.
func SanitizeHP(hp int, c Class, s Scores, level int) int {
	level = max(1, level)
	hi := max(level, FirstHP(c, s)+(level-1)*max(1, c.OrDefault().HitDie()+s.Mod(CON)))
	return max(level, min(hp, hi))
}

// MaxMana est la réserve de mana (SAG la fait varier).
func MaxMana(c Class, s Scores, level int) int {
	level = max(1, level)
	return max(0, c.OrDefault().ManaBase()+2*(level-1)+level*s.Mod(WIS))
}

// Kind est le style d'attaque d'une arme.
type Kind string

const (
	Melee  Kind = "melee"
	Ranged Kind = "ranged"
	Magic  Kind = "magic"
)

// Valid indique si k est un style connu.
func (k Kind) Valid() bool { return k == Melee || k == Ranged || k == Magic }

// OrMelee renvoie k, ou mêlée s'il est inconnu (ou vide : sans arme).
func (k Kind) OrMelee() Kind {
	if k.Valid() {
		return k
	}
	return Melee
}

// Stat est la caractéristique qui dirige ce style.
func (k Kind) Stat() Ability {
	switch k {
	case Ranged:
		return AGI
	case Magic:
		return INT
	}
	return STR
}

// SpellCost est la mana dépensée par une attaque magique.
const SpellCost = 2

// Size est la taille d'une créature : elle fixe sa DEF de base.
type Size string

const (
	Small  Size = "small"  // petites créatures
	Medium Size = "medium" // humanoïdes et créatures moyennes (les joueurs)
	Large  Size = "large"  // grandes créatures
)

// DefBase est la DEF de base d'une taille (8, 10 ou 12) ; une taille inconnue vaut moyenne.
func DefBase(s Size) int {
	switch s {
	case Small:
		return 8
	case Large:
		return 12
	}
	return 10
}

// DefenseBonus est le bonus de DEF de l'agilité (le modificateur d'AGI).
func (s Scores) DefenseBonus() int { return s.Mod(AGI) }

// Hits indique si un jet de d20 touche : le 20 naturel touche toujours, le 1 naturel échoue toujours,
// sinon il faut faire strictement plus que la DEF avec le bonus.
func Hits(roll, bonus, def int) bool {
	switch roll {
	case 20:
		return true
	case 1:
		return false
	}
	return roll+bonus > def
}

// HitChance est la probabilité (0 à 1) de toucher une DEF avec un bonus, selon Hits.
func HitChance(bonus, def int) float64 {
	n := 0
	for r := 1; r <= 20; r++ {
		if Hits(r, bonus, def) {
			n++
		}
	}
	return float64(n) / 20
}

// PriceAdjust est l'ajustement des prix (en %) selon le modificateur de CHA : −2 % par point
// positif, +2 % par point négatif, borné à ±10 %.
func PriceAdjust(mod int) int { return max(-10, min(10, -2*mod)) }

// AdjustPrice applique le charisme à un prix (jamais moins de 1 pour un prix positif).
func AdjustPrice(price, chaMod int) int {
	if price <= 0 {
		return price
	}
	return max(1, price*(100+PriceAdjust(chaMod))/100)
}

// Impression est la clé de traduction du palier d'impression que fait le personnage.
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

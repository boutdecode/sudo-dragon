// Package dice contient la notation « x d y + z » des dégâts : x dés à y faces, plus un bonus.
// Il ne dépend d'aucun autre paquet et reçoit son aléa en paramètre.
package dice

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// Bornes de lecture d'une notation (sauvegarde, modèle de langage).
const (
	MaxCount = 20
	MaxSides = 100
	MaxBonus = 100
)

// Dice est « N d Sides + Bonus ». Le zéro ne fait rien (N = 0 : aucun dé, bonus seul).
type Dice struct {
	N, Sides int
	Bonus    int
}

// D construit n dés à sides faces plus bonus.
func D(n, sides, bonus int) Dice { return Dice{N: n, Sides: sides, Bonus: bonus} }

// IsZero indique qu'il n'y a ni dé ni bonus.
func (d Dice) IsZero() bool { return d.N == 0 && d.Bonus == 0 }

func (d Dice) rolls(rng *rand.Rand, times int) int {
	if d.Sides < 1 {
		return 0
	}
	sum := 0
	for range max(0, d.N) * times {
		sum += rng.IntN(d.Sides) + 1
	}
	return sum
}

// Roll lance les dés et ajoute le bonus.
func (d Dice) Roll(rng *rand.Rand) int { return d.rolls(rng, 1) + d.Bonus }

// RollCrit lance deux fois plus de dés (coup critique); le bonus ne double pas.
func (d Dice) RollCrit(rng *rand.Rand) int { return d.rolls(rng, 2) + d.Bonus }

// Min et Max sont les résultats extrêmes ; Avg la moyenne (arrondie à l'inférieur).
func (d Dice) Min() int { return max(0, d.N) + d.Bonus }
func (d Dice) Max() int { return max(0, d.N)*max(0, d.Sides) + d.Bonus }
func (d Dice) Avg() int {
	if d.Sides < 1 {
		return d.Bonus
	}
	return max(0, d.N)*(d.Sides+1)/2 + d.Bonus
}

// String donne « 2d6+2 », « 3d4 », « 1d8-1 » (ou « 0 » pour le zéro).
func (d Dice) String() string {
	if d.N <= 0 || d.Sides < 1 {
		return strconv.Itoa(d.Bonus)
	}
	s := strconv.Itoa(d.N) + "d" + strconv.Itoa(d.Sides)
	switch {
	case d.Bonus > 0:
		s += "+" + strconv.Itoa(d.Bonus)
	case d.Bonus < 0:
		s += strconv.Itoa(d.Bonus)
	}
	return s
}

// WithBonus renvoie les mêmes dés avec le bonus augmenté de b.
func (d Dice) WithBonus(b int) Dice { d.Bonus += b; return d }

// Parse lit « 2d6+2 », « d8 », « 3d4-1 » ou « 5 », de façon tolérante (espaces, majuscules) et bornée.
// Renvoie false si le texte n'est pas une notation.
func Parse(s string) (Dice, bool) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if s == "" {
		return Dice{}, false
	}
	clamp := func(v, hi int) int { return max(-hi, min(v, hi)) }
	head, tail := s, ""
	if i := strings.IndexAny(s[1:], "+-"); i >= 0 {
		head, tail = s[:i+1], s[i+1:]
	}
	var d Dice
	if n, sides, ok := strings.Cut(head, "d"); ok {
		cnt := 1
		if n != "" {
			v, err := strconv.Atoi(n)
			if err != nil || v < 0 {
				return Dice{}, false
			}
			cnt = v
		}
		sd, err := strconv.Atoi(sides)
		if err != nil || sd < 1 {
			return Dice{}, false
		}
		d.N, d.Sides = min(cnt, MaxCount), min(sd, MaxSides)
	} else {
		v, err := strconv.Atoi(head)
		if err != nil {
			return Dice{}, false
		}
		d.Bonus = clamp(v, MaxBonus)
		return d, tail == ""
	}
	if tail != "" {
		v, err := strconv.Atoi(tail)
		if err != nil {
			return Dice{}, false
		}
		d.Bonus = clamp(v, MaxBonus)
	}
	return d, true
}

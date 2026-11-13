// Package dice provides the "x d y + z" damage notation: x dice with y sides, plus a bonus.
// It depends on no other package and receives its randomness as a parameter.
package dice

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// Bounds when reading a notation (save file, language model).
const (
	MaxCount = 20
	MaxSides = 100
	MaxBonus = 100
)

// Dice is "N d Sides + Bonus". The zero value does nothing (N = 0: no dice, bonus only).
type Dice struct {
	N, Sides int
	Bonus    int
}

// D builds n dice with sides faces plus bonus.
func D(n, sides, bonus int) Dice { return Dice{N: n, Sides: sides, Bonus: bonus} }

// IsZero reports that there is neither a die nor a bonus.
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

// Roll rolls the dice and adds the bonus.
func (d Dice) Roll(rng *rand.Rand) int { return d.rolls(rng, 1) + d.Bonus }

// RollCrit rolls twice as many dice (critical hit); the bonus is not doubled.
func (d Dice) RollCrit(rng *rand.Rand) int { return d.rolls(rng, 2) + d.Bonus }

// Min and Max are the extreme results; Avg is the average (rounded down).
func (d Dice) Min() int { return max(0, d.N) + d.Bonus }
func (d Dice) Max() int { return max(0, d.N)*max(0, d.Sides) + d.Bonus }
func (d Dice) Avg() int {
	if d.Sides < 1 {
		return d.Bonus
	}
	return max(0, d.N)*(d.Sides+1)/2 + d.Bonus
}

// String gives "2d6+2", "3d4", "1d8-1" (or "0" for the zero value).
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

// WithBonus returns the same dice with the bonus increased by b.
func (d Dice) WithBonus(b int) Dice { d.Bonus += b; return d }

// Parse reads "2d6+2", "d8", "3d4-1" or "5", tolerantly (spaces, uppercase) and bounded.
// It returns false if the text is not a notation.
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

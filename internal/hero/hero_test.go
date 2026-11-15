package hero

import (
	"math/rand/v2"
	"testing"
)

func TestModTable(t *testing.T) {
	want := map[int]int{3: -4, 4: -3, 5: -3, 6: -2, 7: -2, 8: -1, 9: -1, 10: 0, 11: 0, 12: 1, 13: 1, 14: 2, 15: 2, 16: 3, 17: 3, 18: 4, 19: 4, 20: 5, 21: 5, 22: 6}
	for v, m := range want {
		if got := Mod(v); got != m {
			t.Errorf("Mod(%d) = %d, expected %d", v, got, m)
		}
	}
	for v := 3; v < 30; v++ {
		if Mod(v+1) < Mod(v) {
			t.Fatalf("Mod not monotonic at %d", v)
		}
	}
}

func TestRoll3d6StaysInRangeAndCoversEveryValue(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	sum, seen := 0, map[int]bool{}
	const n = 20000
	for range n {
		v := Roll3d6(rng)
		if v < 3 || v > 18 {
			t.Fatalf("out of bounds: %d", v)
		}
		sum += v
		seen[v] = true
	}
	if avg := float64(sum) / n; avg < 10.3 || avg > 10.7 { // theoretical mean 10.5
		t.Fatalf("average %.2f", avg)
	}
	if len(seen) != 16 {
		t.Fatalf("values seen: %d of 16", len(seen))
	}
}

func TestRerollLimitedToThreeInTotal(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	c := NewCreation(rng)
	for i := range 3 {
		if !c.Reroll(Ability(i), rng) {
			t.Fatalf("reroll %d refused", i)
		}
	}
	if c.Reroll(STR, rng) || c.Left() != 0 {
		t.Fatalf("4th reroll accepted: %+v", c)
	}
	d := NewCreation(rng)
	if d.Reroll(-1, rng) || d.Reroll(NumAbilities, rng) || d.Rolls != 0 {
		t.Fatal("invalid index accepted")
	}
}

func TestHitPointsFollowDieAndConstitution(t *testing.T) {
	s := Default()
	s[CON] = 14 // +2
	if got := FirstHP(Warrior, s); got != 14 {
		t.Fatalf("warrior lvl 1: %d", got)
	}
	if got := FirstHP(Mage, s); got != 8 {
		t.Fatalf("mage lvl 1: %d", got)
	}
	s[CON] = 3 // −4
	if got := FirstHP(Mage, s); got != 2 {
		t.Fatalf("mage CON 3: %d", got)
	}
	rng := rand.New(rand.NewPCG(5, 6))
	for range 200 {
		if LevelHP(Mage, s, rng) < 1 {
			t.Fatal("HP gain below 1")
		}
		hp := RollHP(Rogue, Default(), 10, rng)
		if hp < 10+8 || hp > 8+9*8 {
			t.Fatalf("HP level 10: %d", hp)
		}
	}
}

func TestSanitizeHPAndScores(t *testing.T) {
	s := Default()
	if got := SanitizeHP(10_000, Warrior, s, 5); got != 12+4*12 {
		t.Fatalf("ceiling: %d", got)
	}
	if got := SanitizeHP(-5, Warrior, s, 5); got != 5 {
		t.Fatalf("floor: %d", got)
	}
	if (Scores{}).Sanitize().Set() {
		t.Fatal("a sheet not created must stay that way")
	}
	if got := (Scores{1, 99, 10, 10, 10, 10}).Sanitize(); got[0] != MinScore || got[1] != MaxScore {
		t.Fatalf("bounds: %v", got)
	}
}

func TestManaAndCombatKnobs(t *testing.T) {
	s := Default()
	s[WIS] = 16 // +3
	if got := MaxMana(Mage, s, 1); got != 11 {
		t.Fatalf("mage mana lvl1: %d", got)
	}
	s[WIS] = 3
	if got := MaxMana(Warrior, s, 1); got != 0 {
		t.Fatalf("mana floor: %d", got)
	}
	if Ranged.Stat() != AGI || Magic.Stat() != INT || Kind("").Stat() != STR || Kind("x").OrMelee() != Melee {
		t.Fatal("styles")
	}
	if AdjustPrice(100, 3) != 94 || AdjustPrice(100, -5) != 110 || AdjustPrice(1, 5) != 1 || AdjustPrice(0, 5) != 0 {
		t.Fatal("price")
	}
}

func FuzzSanitize(f *testing.F) {
	f.Add(1, 2, 3, 4, 5, 6, 7, 99)
	f.Fuzz(func(t *testing.T, a, b, c, d, e, g, level, hp int) {
		s := Scores{a, b, c, d, e, g}.Sanitize()
		if s.Set() {
			for _, v := range s {
				if v < MinScore || v > MaxScore {
					t.Fatalf("out of bounds: %v", s)
				}
			}
		}
		level = max(1, min(level, 1000))
		if got := SanitizeHP(hp, Mage, s, level); got < level {
			t.Fatalf("HP %d < level %d", got, level)
		}
	})
}

func TestDefenseAndHitRules(t *testing.T) {
	if DefBase(Small) != 8 || DefBase(Medium) != 10 || DefBase(Large) != 12 || DefBase("") != 10 {
		t.Fatal("base DEF")
	}
	s := Default()
	s[AGI] = 14
	if s.DefenseBonus() != 2 {
		t.Fatal("AGI bonus")
	}
	// It must be strictly greater than the DEF.
	if Hits(10, 0, 10) || !Hits(11, 0, 10) || !Hits(8, 3, 10) || Hits(7, 3, 10) {
		t.Fatal("hit threshold")
	}
	if !Hits(20, -10, 30) || Hits(1, 30, 5) {
		t.Fatal("natural 20 and 1")
	}
	// DEF 10, bonus 0: 11 to 20 hit => 50%; bonus +5: 6 to 20 => 75%.
	if HitChance(0, 10) != 0.5 || HitChance(5, 10) != 0.75 || HitChance(-20, 10) != 0.05 || HitChance(30, 10) != 0.95 {
		t.Fatalf("probabilities: %v %v %v %v", HitChance(0, 10), HitChance(5, 10), HitChance(-20, 10), HitChance(30, 10))
	}
}

func TestRaceBonus(t *testing.T) {
	want := map[Race]Ability{Human: STR, Elf: INT, Dwarf: CON, Halfling: AGI}
	for _, r := range Races {
		s := r.Apply(Default())
		for a := Ability(0); a < NumAbilities; a++ {
			exp := 10
			if a == want[r] {
				exp += RaceBonus
			}
			if s[a] != exp {
				t.Fatalf("%s: %s = %d, expected %d", r, a.Key(), s[a], exp)
			}
		}
	}
	if Race("orc").Valid() || Race("orc").OrDefault() != Human || Race("").Ability() != STR {
		t.Fatal("unknown race must be human")
	}
}

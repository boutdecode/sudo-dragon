package combat

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/hero"
)

func TestNewStats(t *testing.T) {
	s := NewStats()
	if s.Level != 1 || s.XP != 0 || s.MaxHP <= 0 || s.Defense() != 10 || s.Potions != startPotions {
		t.Fatalf("unexpected starting sheet: %+v", s)
	}
	if s.XPToNext() != 120 {
		t.Fatalf("XPToNext level 1 = %d", s.XPToNext())
	}
}

func TestAddXPLevelsUpAndCarriesOver(t *testing.T) {
	s := NewStats()
	if n := s.AddXP(40, nil); n != 0 || s.Level != 1 || s.XP != 40 {
		t.Fatalf("no level expected: %+v (levels gained %d)", s, n)
	}
	hp := s.MaxHP
	if n := s.AddXP(80, nil); n != 1 || s.Level != 2 || s.XP != 0 {
		t.Fatalf("level 2 with 2 leftover expected: %+v (levels gained %d)", s, n)
	}
	if s.MaxHP != hp+7 {
		t.Fatalf("incorrect level gains: %+v", s)
	}
}

func TestAddXPSeveralLevelsAtOnce(t *testing.T) {
	s := NewStats()
	// 120 (lvl 1) + 480 (lvl 2) = 600 to reach level 3, plus 5 leftover.
	if n := s.AddXP(605, nil); n != 2 || s.Level != 3 || s.XP != 5 {
		t.Fatalf("expected level 3 / XP 5, got %+v (levels gained %d)", s, n)
	}
}

// Level 20 should take ~60 h of play: ~300,000 cumulative XP (see xpCurve).
func TestXPCurveTotalToMaxLevel(t *testing.T) {
	s := NewStats()
	total := 0
	for s.Level < 20 {
		total += s.XPToNext()
		s.Level++
	}
	if total < 280000 || total > 320000 {
		t.Fatalf("cumulative XP level 1→20 = %d, expected ~300,000", total)
	}
}

func TestAddXPNegativeIgnored(t *testing.T) {
	s := NewStats()
	s.AddXP(-50, nil)
	if s.XP != 0 || s.Level != 1 {
		t.Fatalf("negative XP applied: %+v", s)
	}
}

func TestAddPotionsCap(t *testing.T) {
	s := NewStats()
	if got := s.AddPotions(3); got != 3 || s.Potions != startPotions+3 {
		t.Fatalf("simple add: %d, %+v", got, s)
	}
	if got := s.AddPotions(100); s.Potions != MaxPotions || got != MaxPotions-startPotions-3 {
		t.Fatalf("cap: added %d, potions %d", got, s.Potions)
	}
	if got := s.AddPotions(1); got != 0 {
		t.Fatalf("add at cap: %d", got)
	}
	if got := s.AddPotions(-4); got != 0 || s.Potions != MaxPotions {
		t.Fatalf("negative add: %d, %d", got, s.Potions)
	}
}

func TestFighterFromStats(t *testing.T) {
	s := NewStats()
	f := s.Fighter(7)
	if f.HP != 7 || f.MaxHP != s.MaxHP || f.Def != s.Defense() || f.Dmg != BareHands {
		t.Fatalf("incorrect conversion: %+v", f)
	}
}

// StatsAt must give back exactly what normal progression produces.
func TestStatsAtMatchesProgression(t *testing.T) {
	s := NewStats()
	for level := 1; level <= 30; level++ {
		got := StatsAt(s.Level, s.XP, s.Potions)
		if got != s {
			t.Fatalf("level %d: StatsAt=%+v, progression=%+v", level, got, s)
		}
		s.AddXP(s.XPToNext()-s.XP+3, nil) // moves to the next level with 3 leftover
	}
}

func TestStatsAtClampsCorruptValues(t *testing.T) {
	s := StatsAt(-5, -1, 99)
	if s.Level != 1 || s.XP != 0 || s.Potions != MaxPotions {
		t.Fatalf("negative/excessive values badly clamped: %+v", s)
	}
	s = StatsAt(2, 10_000, -3)
	if s.XP != s.XPToNext()-1 || s.Potions != 0 {
		t.Fatalf("excessive XP badly clamped: %+v", s)
	}
}

func TestGoldAddSpendAndCap(t *testing.T) {
	s := NewStats()
	if s.Gold != 0 {
		t.Fatalf("starting gold = %d", s.Gold)
	}
	if got := s.AddGold(30); got != 30 || s.Gold != 30 {
		t.Fatalf("add: %d, gold=%d", got, s.Gold)
	}
	if got := s.AddGold(-5); got != 0 || s.Gold != 30 {
		t.Fatalf("a negative add changed the gold: %d, gold=%d", got, s.Gold)
	}
	if !s.SpendGold(12) || s.Gold != 18 {
		t.Fatalf("spend of 12: gold=%d", s.Gold)
	}
	if s.SpendGold(19) || s.Gold != 18 {
		t.Fatalf("spend above gold accepted: gold=%d", s.Gold)
	}
	if s.SpendGold(-4) || s.Gold != 18 {
		t.Fatalf("negative spend accepted (it would add gold): gold=%d", s.Gold)
	}
	if !s.SpendGold(18) || s.Gold != 0 {
		t.Fatalf("exact spend: gold=%d", s.Gold)
	}
	s.Gold = MaxGold - 5
	if got := s.AddGold(100); got != 5 || s.Gold != MaxGold {
		t.Fatalf("cap: added %d, gold=%d", got, s.Gold)
	}
}

func TestWithGoldClamps(t *testing.T) {
	base := NewStats()
	for in, want := range map[int]int{-50: 0, 0: 0, 123: 123, MaxGold: MaxGold, MaxGold + 1: MaxGold, 1 << 60: MaxGold} {
		if got := base.WithGold(in).Gold; got != want {
			t.Errorf("WithGold(%d) = %d, expected %d", in, got, want)
		}
	}
	if base.Gold != 0 {
		t.Fatal("WithGold modified the original")
	}
}

func TestLevelUpKeepsGold(t *testing.T) {
	s := NewStats()
	s.AddGold(50)
	s.AddXP(100, nil)
	if s.Gold != 50 {
		t.Fatalf("gold changed on level-up: %d", s.Gold)
	}
}

func TestWithRecordClamps(t *testing.T) {
	s := NewStats().WithRecord(12, 2, 5)
	if s.Kills != 12 || s.BossKills != 2 || s.Deaths != 5 {
		t.Fatalf("%+v", s)
	}
	s = NewStats().WithRecord(-1, -2, -3)
	if s.Kills != 0 || s.BossKills != 0 || s.Deaths != 0 {
		t.Fatalf("negative values kept: %+v", s)
	}
}

func TestLevelUpKeepsRecord(t *testing.T) {
	s := NewStats().WithRecord(10, 1, 2)
	s.AddXP(500, nil)
	if s.Kills != 10 || s.BossKills != 1 || s.Deaths != 2 {
		t.Fatalf("history altered on level-up: %+v", s)
	}
}

func TestNewStatsForUsesClassDieAndConstitution(t *testing.T) {
	sc := hero.Default()
	sc[hero.CON], sc[hero.WIS] = 14, 16 // +2, +3
	mage := NewStatsFor(hero.Mage, sc)
	if mage.MaxHP != 8 || mage.MaxMana != 8+3 || mage.Class != hero.Mage {
		t.Fatalf("mage: %+v", mage)
	}
	if w := NewStatsFor(hero.Warrior, sc); w.MaxHP != 14 {
		t.Fatalf("warrior: %+v", w)
	}
	if d := NewStatsFor("", hero.Scores{}); d.Class != hero.Warrior || d.Scores != hero.Default() {
		t.Fatalf("defaults: %+v", d)
	}
}

func TestAddXPRollsHitDice(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	s := NewStatsFor(hero.Rogue, hero.Default())
	seen := map[int]bool{}
	for range 200 {
		before := s.MaxHP
		s.AddXP(s.XPToNext(), rng)
		gain := s.MaxHP - before
		if gain < 1 || gain > 8 {
			t.Fatalf("gain %d outside d8", gain)
		}
		seen[gain] = true
	}
	if len(seen) < 5 {
		t.Fatalf("dice do not vary: %v", seen)
	}
}

func TestStatsForClampsHitPointsAndMana(t *testing.T) {
	s := StatsFor(3, 0, 0, hero.Mage, hero.Default(), 10_000)
	if s.MaxHP != 6+2*6 {
		t.Fatalf("excessive HP: %d", s.MaxHP)
	}
	if s := StatsFor(3, 0, 0, hero.Mage, hero.Default(), 1); s.MaxHP != 3 {
		t.Fatalf("HP floor: %d", s.MaxHP)
	}
}

func TestDefenseIsTenPlusAgilityModifier(t *testing.T) {
	sc := hero.Default()
	sc[hero.AGI] = 16 // +3
	if d := NewStatsFor(hero.Rogue, sc).Defense(); d != 13 {
		t.Fatalf("DEF = %d", d)
	}
	sc[hero.AGI] = 6 // −2
	f := NewStatsFor(hero.Rogue, sc).Fighter(5)
	if f.Def != 8 || f.Dmg != BareHands {
		t.Fatalf("combatant: %+v", f)
	}
}

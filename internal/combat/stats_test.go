package combat

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/hero"
)

func TestNewStats(t *testing.T) {
	s := NewStats()
	if s.Level != 1 || s.XP != 0 || s.MaxHP <= 0 || s.Defense() != 10 || s.Potions != startPotions {
		t.Fatalf("fiche de départ inattendue: %+v", s)
	}
	if s.XPToNext() != 120 {
		t.Fatalf("XPToNext niveau 1 = %d", s.XPToNext())
	}
}

func TestAddXPLevelsUpAndCarriesOver(t *testing.T) {
	s := NewStats()
	if n := s.AddXP(40, nil); n != 0 || s.Level != 1 || s.XP != 40 {
		t.Fatalf("pas de niveau attendu: %+v (niveaux gagnés %d)", s, n)
	}
	hp := s.MaxHP
	if n := s.AddXP(80, nil); n != 1 || s.Level != 2 || s.XP != 0 {
		t.Fatalf("niveau 2 avec reliquat 2 attendu: %+v (niveaux gagnés %d)", s, n)
	}
	if s.MaxHP != hp+7 {
		t.Fatalf("gains de niveau incorrects: %+v", s)
	}
}

func TestAddXPSeveralLevelsAtOnce(t *testing.T) {
	s := NewStats()
	// 120 (niv. 1) + 480 (niv. 2) = 600 pour atteindre le niveau 3, plus 5 de reliquat.
	if n := s.AddXP(605, nil); n != 2 || s.Level != 3 || s.XP != 5 {
		t.Fatalf("attendu niveau 3 / XP 5, obtenu %+v (niveaux gagnés %d)", s, n)
	}
}

// Le niveau 20 doit demander ~60 h de jeu: ~300 000 XP cumulés (voir xpCurve).
func TestXPCurveTotalToMaxLevel(t *testing.T) {
	s := NewStats()
	total := 0
	for s.Level < 20 {
		total += s.XPToNext()
		s.Level++
	}
	if total < 280000 || total > 320000 {
		t.Fatalf("XP cumulé niveau 1→20 = %d, attendu ~300 000", total)
	}
}

func TestAddXPNegativeIgnored(t *testing.T) {
	s := NewStats()
	s.AddXP(-50, nil)
	if s.XP != 0 || s.Level != 1 {
		t.Fatalf("XP négative appliquée: %+v", s)
	}
}

func TestAddPotionsCap(t *testing.T) {
	s := NewStats()
	if got := s.AddPotions(3); got != 3 || s.Potions != startPotions+3 {
		t.Fatalf("ajout simple: %d, %+v", got, s)
	}
	if got := s.AddPotions(100); s.Potions != MaxPotions || got != MaxPotions-startPotions-3 {
		t.Fatalf("plafond: ajouté %d, potions %d", got, s.Potions)
	}
	if got := s.AddPotions(1); got != 0 {
		t.Fatalf("ajout au plafond: %d", got)
	}
	if got := s.AddPotions(-4); got != 0 || s.Potions != MaxPotions {
		t.Fatalf("ajout négatif: %d, %d", got, s.Potions)
	}
}

func TestFighterFromStats(t *testing.T) {
	s := NewStats()
	f := s.Fighter(7)
	if f.HP != 7 || f.MaxHP != s.MaxHP || f.Def != s.Defense() || f.Dmg != BareHands {
		t.Fatalf("conversion incorrecte: %+v", f)
	}
}

// StatsAt doit redonner exactement ce que la progression normale produit.
func TestStatsAtMatchesProgression(t *testing.T) {
	s := NewStats()
	for level := 1; level <= 30; level++ {
		got := StatsAt(s.Level, s.XP, s.Potions)
		if got != s {
			t.Fatalf("niveau %d: StatsAt=%+v, progression=%+v", level, got, s)
		}
		s.AddXP(s.XPToNext()-s.XP+3, nil) // passe au niveau suivant avec un reliquat de 3
	}
}

func TestStatsAtClampsCorruptValues(t *testing.T) {
	s := StatsAt(-5, -1, 99)
	if s.Level != 1 || s.XP != 0 || s.Potions != MaxPotions {
		t.Fatalf("valeurs négatives/excessives mal ramenées: %+v", s)
	}
	s = StatsAt(2, 10_000, -3)
	if s.XP != s.XPToNext()-1 || s.Potions != 0 {
		t.Fatalf("XP excessive mal ramenée: %+v", s)
	}
}

func TestGoldAddSpendAndCap(t *testing.T) {
	s := NewStats()
	if s.Gold != 0 {
		t.Fatalf("or de départ = %d", s.Gold)
	}
	if got := s.AddGold(30); got != 30 || s.Gold != 30 {
		t.Fatalf("ajout: %d, or=%d", got, s.Gold)
	}
	if got := s.AddGold(-5); got != 0 || s.Gold != 30 {
		t.Fatalf("un ajout négatif a modifié l'or: %d, or=%d", got, s.Gold)
	}
	if !s.SpendGold(12) || s.Gold != 18 {
		t.Fatalf("dépense de 12: or=%d", s.Gold)
	}
	if s.SpendGold(19) || s.Gold != 18 {
		t.Fatalf("dépense supérieure à l'or acceptée: or=%d", s.Gold)
	}
	if s.SpendGold(-4) || s.Gold != 18 {
		t.Fatalf("dépense négative acceptée (elle ajouterait de l'or): or=%d", s.Gold)
	}
	if !s.SpendGold(18) || s.Gold != 0 {
		t.Fatalf("dépense exacte: or=%d", s.Gold)
	}
	s.Gold = MaxGold - 5
	if got := s.AddGold(100); got != 5 || s.Gold != MaxGold {
		t.Fatalf("plafond: ajouté %d, or=%d", got, s.Gold)
	}
}

func TestWithGoldClamps(t *testing.T) {
	base := NewStats()
	for in, want := range map[int]int{-50: 0, 0: 0, 123: 123, MaxGold: MaxGold, MaxGold + 1: MaxGold, 1 << 60: MaxGold} {
		if got := base.WithGold(in).Gold; got != want {
			t.Errorf("WithGold(%d) = %d, attendu %d", in, got, want)
		}
	}
	if base.Gold != 0 {
		t.Fatal("WithGold a modifié l'original")
	}
}

func TestLevelUpKeepsGold(t *testing.T) {
	s := NewStats()
	s.AddGold(50)
	s.AddXP(100, nil)
	if s.Gold != 50 {
		t.Fatalf("l'or a changé à la montée de niveau: %d", s.Gold)
	}
}

func TestWithRecordClamps(t *testing.T) {
	s := NewStats().WithRecord(12, 2, 5)
	if s.Kills != 12 || s.BossKills != 2 || s.Deaths != 5 {
		t.Fatalf("%+v", s)
	}
	s = NewStats().WithRecord(-1, -2, -3)
	if s.Kills != 0 || s.BossKills != 0 || s.Deaths != 0 {
		t.Fatalf("valeurs négatives conservées: %+v", s)
	}
}

func TestLevelUpKeepsRecord(t *testing.T) {
	s := NewStats().WithRecord(10, 1, 2)
	s.AddXP(500, nil)
	if s.Kills != 10 || s.BossKills != 1 || s.Deaths != 2 {
		t.Fatalf("historique altéré à la montée de niveau: %+v", s)
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
		t.Fatalf("guerrier: %+v", w)
	}
	if d := NewStatsFor("", hero.Scores{}); d.Class != hero.Warrior || d.Scores != hero.Default() {
		t.Fatalf("défauts: %+v", d)
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
			t.Fatalf("gain %d hors d8", gain)
		}
		seen[gain] = true
	}
	if len(seen) < 5 {
		t.Fatalf("les dés ne varient pas: %v", seen)
	}
}

func TestStatsForClampsHitPointsAndMana(t *testing.T) {
	s := StatsFor(3, 0, 0, hero.Mage, hero.Default(), 10_000)
	if s.MaxHP != 6+2*6 {
		t.Fatalf("PV excessifs: %d", s.MaxHP)
	}
	if s := StatsFor(3, 0, 0, hero.Mage, hero.Default(), 1); s.MaxHP != 3 {
		t.Fatalf("PV plancher: %d", s.MaxHP)
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
		t.Fatalf("combattant: %+v", f)
	}
}

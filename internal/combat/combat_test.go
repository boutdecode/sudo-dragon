package combat

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
)

func rngSeed(n uint64) *rand.Rand { return rand.New(rand.NewPCG(n, n*7+1)) }

func newBattle(player, monster Fighter, potions int, seed uint64) *Battle {
	return New(player, potions, monster, "Rat", rngSeed(seed))
}

var (
	// Un guerrier de niveau 1 (DEF 10, 1d6 + 2) et un rat (DEF 8, 1d4, +1 pour toucher).
	player = Fighter{HP: 30, MaxHP: 30, Def: 10, Hit: 2, Dmg: dice.D(1, 6, 0), DmgMod: 2}
	rat    = Fighter{HP: 8, MaxHP: 8, Def: 8, Hit: 1, Dmg: dice.D(1, 4, 0)}
)

// finish joue des attaques jusqu'à la fin du combat (une attaque peut manquer).
func finish(t *testing.T, b *Battle) {
	t.Helper()
	for i := 0; b.Result == Ongoing; i++ {
		if i > 500 {
			t.Fatal("combat sans fin")
		}
		b.Act(Attack)
	}
}

func TestParseAction(t *testing.T) {
	for s, want := range map[string]Action{"attack": Attack, "item": Item, "flee": Flee} {
		if got, ok := ParseAction(s); !ok || got != want {
			t.Errorf("ParseAction(%q) = %v,%v", s, got, ok)
		}
	}
	if _, ok := ParseAction("dance"); ok {
		t.Error("action inconnue acceptée")
	}
}

func TestResultStrings(t *testing.T) {
	for r, want := range map[Result]string{Ongoing: "", Won: "won", Lost: "lost", Fled: "fled"} {
		if r.String() != want {
			t.Errorf("Result(%d) = %q, attendu %q", r, r.String(), want)
		}
	}
}

// hitRate mesure la fréquence à laquelle l'attaquant touche une cible de DEF donnée.
func hitRate(att Fighter, def int) float64 {
	hits := 0
	const n = 6000
	for seed := uint64(0); seed < n; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: def}, 0, seed)
		b.Act(Attack)
		if b.Monster.HP < 1000 {
			hits++
		}
	}
	return float64(hits) / n
}

func TestAttackNeedsMoreThanTheDefenseOnAD20(t *testing.T) {
	att := func(hit int) Fighter {
		return Fighter{HP: 1e6, MaxHP: 1e6, Def: 10, Hit: hit, Dmg: dice.D(1, 4, 0)}
	}
	// Contre DEF 10 : bonus 0 => 11 à 20 (50 %) ; +5 => 6 à 20 (75 %) ; le 20 naturel touche toujours (5 %) ; le 1 jamais (95 %).
	for _, c := range []struct {
		hit  int
		want float64
	}{{0, 0.50}, {5, 0.75}, {-30, 0.05}, {30, 0.95}} {
		if got := hitRate(att(c.hit), 10); got < c.want-0.03 || got > c.want+0.03 {
			t.Errorf("bonus %+d: touche %.2f, attendu %.2f", c.hit, got, c.want)
		}
	}
	// Une cible plus agile (DEF 14) est plus dure à toucher qu'une petite créature (DEF 8).
	if hitRate(att(0), 14) >= hitRate(att(0), 8) {
		t.Error("la DEF n'a pas d'effet")
	}
}

func TestCriticalDoublesDiceButNotTheBonus(t *testing.T) {
	// 2d1+10 vaut toujours 12 ; en critique 4d1+10 = 14. Plus 3 de modificateur.
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(2, 1, 10), DmgMod: 3}
	seen := map[int]int{}
	for seed := uint64(0); seed < 3000; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10}, 0, seed)
		b.Act(Attack)
		if d := 1000 - b.Monster.HP; d != 0 {
			seen[d]++
		}
	}
	if len(seen) != 2 || seen[15] == 0 || seen[17] == 0 {
		t.Fatalf("dégâts observés: %v (attendu 15 normal et 17 critique)", seen)
	}
	// Le critique est le 20 naturel : environ 1 coup sur 20 parmi ceux qui touchent (95 % ici).
	if rate := float64(seen[17]) / float64(seen[15]+seen[17]); rate < 0.03 || rate > 0.09 {
		t.Fatalf("taux de critique %.3f", rate)
	}
}

func TestArmourDiceReduceDamageButNeverBelowOne(t *testing.T) {
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 5)} // 6 dégâts, 12 en critique
	for _, c := range []struct {
		red  dice.Dice
		want []int
	}{{dice.Dice{}, []int{6, 7}}, {dice.D(2, 1, 0), []int{4, 5}}, {dice.D(1, 1, 50), []int{1}}} {
		seen := map[int]bool{}
		for seed := uint64(0); seed < 400; seed++ {
			b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Reduction: c.red}, 0, seed)
			b.Act(Attack)
			if d := 1000 - b.Monster.HP; d != 0 {
				seen[d] = true
			}
		}
		for d := range seen {
			ok := false
			for _, w := range c.want {
				ok = ok || d == w || (c.red.IsZero() && d == 7)
			}
			if !ok && d != 12-c.red.Min() && d != 12 {
				t.Errorf("réduction %v: dégâts %d inattendus (vus %v)", c.red, d, seen)
			}
		}
		if len(seen) == 0 {
			t.Errorf("réduction %v: aucun coup ne touche", c.red)
		}
		if c.red == dice.D(1, 1, 50) && (seen[1] != true || len(seen) != 1 && !seen[1]) {
			t.Errorf("minimum 1 non respecté: %v", seen)
		}
	}
}

func TestReductionIsReportedInTheLog(t *testing.T) {
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 9)}
	for seed := uint64(0); seed < 50; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Reduction: dice.D(1, 1, 2)}, 0, seed)
		log := b.Act(Attack)
		if b.Monster.HP < 1000 && !strings.Contains(log[0], "armure") {
			t.Fatalf("la réduction n'apparaît pas dans le journal: %v", log)
		}
	}
}

func TestPlayerWinsAgainstWeakMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 30, Hit: 10, Dmg: dice.D(1, 1, 50)}, rat, 0, 1)
	finish(t, b)
	if b.Result != Won || b.Monster.HP != 0 {
		t.Fatalf("victoire attendue: %v, PV monstre %d", b.Result, b.Monster.HP)
	}
	hp := b.Player.HP
	if log := b.Act(Attack); log != nil || b.Player.HP != hp {
		t.Fatalf("action acceptée après la fin: %v", log)
	}
}

func TestPlayerLosesAgainstStrongMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 5, MaxHP: 5, Def: 0, Hit: -30, Dmg: dice.D(1, 1, 0)}, Fighter{HP: 999, MaxHP: 999, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, 0, 1)
	finish(t, b)
	if b.Result != Lost || b.Player.HP != 0 {
		t.Fatalf("défaite attendue: %v, PV %d", b.Result, b.Player.HP)
	}
}

func TestDeadMonsterDoesNotStrikeBack(t *testing.T) {
	for seed := uint64(0); seed < 50; seed++ {
		b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, Fighter{HP: 5, MaxHP: 5, Def: 0, Hit: 50, Dmg: dice.D(1, 1, 9)}, 0, seed)
		before := b.Player.HP
		b.Act(Attack)
		if b.Result == Won && b.Player.HP != before {
			t.Fatalf("seed %d: un monstre mort a riposté", seed)
		}
	}
}

func TestRoundCountsOnlyCompletedRounds(t *testing.T) {
	b := newBattle(player, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Dmg: dice.D(1, 1, 0)}, 0, 3)
	b.Act(Attack)
	b.Act(Attack)
	if b.Round != 2 {
		t.Fatalf("Round=%d, attendu 2", b.Round)
	}
}

func TestItemHealsAndIsCapped(t *testing.T) {
	weak := Fighter{HP: 1000, MaxHP: 1000, Def: 10, Dmg: dice.D(1, 1, 0)}

	// Soin plafonné aux PV max.
	b := newBattle(Fighter{HP: 25, MaxHP: 30, Def: 100}, weak, 2, 1)
	log := b.Act(Item)
	if b.Potions != 1 {
		t.Fatalf("potions=%d, attendu 1", b.Potions)
	}
	if !strings.Contains(log[0], "+5 PV") {
		t.Fatalf("soin plafonné attendu (+5), journal: %v", log)
	}

	// Soin complet quand il y a de la marge.
	b = newBattle(Fighter{HP: 5, MaxHP: 100, Def: 100}, weak, 1, 1)
	log = b.Act(Item)
	if want := "+15 PV"; !strings.Contains(log[0], want) {
		t.Fatalf("soin de %d attendu, journal: %v", HealAmount, log)
	}
}

func TestItemWithoutPotionDoesNotConsumeTurn(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Item)
	if len(log) != 1 || !strings.Contains(log[0], "plus de potion") {
		t.Fatalf("journal: %v", log)
	}
	if b.Round != 0 || b.Player.HP != player.HP || b.Monster.HP != rat.HP {
		t.Fatalf("le tour a été consommé: round=%d, PV joueur=%d", b.Round, b.Player.HP)
	}
}

func TestFleeRateAndEffects(t *testing.T) {
	fled, failed := 0, 0
	const n = 2000
	for seed := uint64(0); seed < n; seed++ {
		b := newBattle(Fighter{HP: 1000, MaxHP: 1000, Def: 0}, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 9)}, 0, seed)
		b.Act(Flee)
		switch b.Result {
		case Fled:
			fled++
			if b.Player.HP != 1000 {
				t.Fatalf("seed %d: le monstre a frappé malgré la fuite réussie", seed)
			}
		case Ongoing:
			failed++
		default:
			t.Fatalf("seed %d: issue inattendue %v", seed, b.Result)
		}
	}
	if rate := float64(fled) / n; rate < 0.42 || rate > 0.58 {
		t.Fatalf("taux de fuite %.2f, attendu environ 0.5", rate)
	}
}

func TestAbandon(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	b.Abandon()
	if b.Result != Fled {
		t.Fatalf("Result=%v", b.Result)
	}
	// Ne remplace pas une issue déjà acquise.
	w := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, rat, 0, 1)
	finish(t, w)
	w.Abandon()
	if w.Result != Won {
		t.Fatalf("Abandon a écrasé la victoire: %v", w.Result)
	}
}

func TestUnknownAction(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Action(99))
	if len(log) != 1 || b.Round != 0 || b.Player.HP != player.HP {
		t.Fatalf("action inconnue mal gérée: %v", log)
	}
}

func TestFullFightsTerminate(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		b := newBattle(player, Fighter{HP: 20, MaxHP: 20, Def: 10, Hit: 2, Dmg: dice.D(1, 6, 0)}, 2, seed)
		finish(t, b)
	}
}

func TestModifierAddsToDamage(t *testing.T) {
	// 1d1 + 5 de mod : au moins 6 à chaque coup (12 en critique), jamais moins.
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 0), DmgMod: 5}, Fighter{HP: 100000, MaxHP: 100000, Def: 10}, 0, 3)
	for range 100 {
		before := b.Monster.HP
		b.Act(Attack)
		if d := before - b.Monster.HP; d != 0 && d != 6 && d != 7 {
			t.Fatalf("dégâts %d pour 1d1 + 5", d)
		}
	}
}

func TestWithoutManaAMagicWeaponIsAClub(t *testing.T) {
	mage := Fighter{HP: 30, MaxHP: 30, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 40), Kind: hero.Magic, Mana: 1, MaxMana: 10, MeleeHit: 50, MeleeMod: 1}
	for seed := uint64(0); seed < 40; seed++ {
		b := newBattle(mage, Fighter{HP: 1000, MaxHP: 1000, Def: 10}, 0, seed)
		b.Act(Attack)
		// Mana intacte, tour joué, dégâts de gourdin (1d4 + FOR) et non du sort (1d1+40).
		if d := 1000 - b.Monster.HP; b.Player.Mana != 1 || b.Round != 1 || d > 2*4+1 {
			t.Fatalf("seed %d: mana %d, round %d, dégâts %d", seed, b.Player.Mana, b.Round, d)
		}
	}
	b := newBattle(mage, rat, 0, 1)
	b.Player.Mana = 5
	b.Act(Attack)
	if b.Player.Mana != 5-hero.SpellCost {
		t.Fatalf("mana après un sort: %d", b.Player.Mana)
	}
}

func TestOtherStylesCostNoMana(t *testing.T) {
	for _, k := range []hero.Kind{hero.Melee, hero.Ranged, "", "inconnu"} {
		b := newBattle(Fighter{HP: 30, MaxHP: 30, Def: 10, Dmg: dice.D(1, 6, 0), Kind: k, Mana: 0}, rat, 0, 1)
		b.Act(Attack)
		if b.Player.Mana != 0 || b.Round == 0 && b.Result == Ongoing {
			t.Fatalf("style %q: tour non joué", k)
		}
	}
}

func TestLogShowsTheRollAgainstTheDefense(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Attack)
	if !strings.Contains(log[0], "DEF 8") || !strings.Contains(log[0], "d20") {
		t.Fatalf("journal sans jet ni DEF: %v", log)
	}
}

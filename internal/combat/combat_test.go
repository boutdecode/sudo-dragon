package combat

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func rngSeed(n uint64) *rand.Rand { return rand.New(rand.NewPCG(n, n*7+1)) }

func newBattle(player, monster Fighter, potions int, seed uint64) *Battle {
	return New(player, potions, monster, "Rat", rngSeed(seed))
}

var (
	hero = Fighter{HP: 30, MaxHP: 30, Atk: 6, Def: 2}
	rat  = Fighter{HP: 8, MaxHP: 8, Atk: 3, Def: 0}
)

func TestParseAction(t *testing.T) {
	for s, want := range map[string]Action{"attack": Attack, "defend": Defend, "item": Item, "flee": Flee} {
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

func TestAttackAlwaysDealsAtLeastOneAndNeverNegativeHP(t *testing.T) {
	for seed := uint64(0); seed < 200; seed++ {
		// Attaque très inférieure à la défense: dégâts planchers à 1.
		b := newBattle(Fighter{HP: 100, MaxHP: 100, Atk: 1, Def: 50}, Fighter{HP: 50, MaxHP: 50, Atk: 1, Def: 40}, 0, seed)
		before := b.Monster.HP
		b.Act(Attack)
		if b.Monster.HP >= before {
			t.Fatalf("seed %d: aucun dégât infligé", seed)
		}
		if b.Monster.HP < 0 || b.Player.HP < 0 {
			t.Fatalf("seed %d: PV négatifs", seed)
		}
	}
}

func TestPlayerWinsAgainstWeakMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Atk: 50, Def: 10}, rat, 0, 1)
	log := b.Act(Attack)
	if b.Result != Won || b.Monster.HP != 0 {
		t.Fatalf("victoire attendue: %v, PV monstre %d", b.Result, b.Monster.HP)
	}
	if !strings.Contains(log[len(log)-1], "vaincu") {
		t.Fatalf("journal: %v", log)
	}
	// Le monstre vaincu ne riposte pas.
	if b.Player.HP != 100 {
		t.Fatalf("un monstre mort a riposté (PV joueur %d)", b.Player.HP)
	}
}

func TestPlayerLosesAgainstStrongMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 5, MaxHP: 5, Atk: 1, Def: 0}, Fighter{HP: 999, MaxHP: 999, Atk: 50, Def: 0}, 0, 1)
	b.Act(Attack)
	if b.Result != Lost || b.Player.HP != 0 {
		t.Fatalf("défaite attendue: %v, PV %d", b.Result, b.Player.HP)
	}
}

func TestRoundCountsOnlyCompletedRounds(t *testing.T) {
	b := newBattle(hero, Fighter{HP: 1000, MaxHP: 1000, Atk: 1, Def: 0}, 0, 3)
	b.Act(Attack)
	b.Act(Defend)
	if b.Round != 2 {
		t.Fatalf("Round=%d, attendu 2", b.Round)
	}
}

func TestNoActionsAfterEnd(t *testing.T) {
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Atk: 50}, rat, 0, 1)
	b.Act(Attack)
	hp := b.Player.HP
	if log := b.Act(Attack); log != nil || b.Player.HP != hp || b.Result != Won {
		t.Fatalf("action acceptée après la fin: %v", log)
	}
}

func TestDefendHalvesIncomingDamage(t *testing.T) {
	avgDamage := func(a Action) float64 {
		total := 0
		const n = 400
		for seed := uint64(0); seed < n; seed++ {
			// PV pleins: la potion ne soigne rien, elle consomme juste le tour sans parer.
			b := newBattle(Fighter{HP: 1000, MaxHP: 1000, Atk: 1, Def: 0}, Fighter{HP: 1000, MaxHP: 1000, Atk: 20, Def: 0}, 1, seed)
			b.Act(a)
			total += 1000 - b.Player.HP
		}
		return float64(total) / n
	}
	plain, guarded := avgDamage(Item), avgDamage(Defend)
	if guarded > plain*0.65 || guarded < plain*0.35 {
		t.Fatalf("parade: %.1f dégâts contre %.1f sans parade (attendu environ la moitié)", guarded, plain)
	}
}

func TestItemHealsAndIsCapped(t *testing.T) {
	weak := Fighter{HP: 1000, MaxHP: 1000, Atk: 1, Def: 0}

	// Soin plafonné aux PV max.
	b := newBattle(Fighter{HP: 25, MaxHP: 30, Atk: 1, Def: 100}, weak, 2, 1)
	log := b.Act(Item)
	if b.Potions != 1 {
		t.Fatalf("potions=%d, attendu 1", b.Potions)
	}
	if !strings.Contains(log[0], "+5 PV") {
		t.Fatalf("soin plafonné attendu (+5), journal: %v", log)
	}

	// Soin complet quand il y a de la marge.
	b = newBattle(Fighter{HP: 5, MaxHP: 100, Atk: 1, Def: 100}, weak, 1, 1)
	log = b.Act(Item)
	if want := "+15 PV"; !strings.Contains(log[0], want) {
		t.Fatalf("soin de %d attendu, journal: %v", HealAmount, log)
	}
}

func TestItemWithoutPotionDoesNotConsumeTurn(t *testing.T) {
	b := newBattle(hero, rat, 0, 1)
	log := b.Act(Item)
	if len(log) != 1 || !strings.Contains(log[0], "plus de potion") {
		t.Fatalf("journal: %v", log)
	}
	if b.Round != 0 || b.Player.HP != hero.HP || b.Monster.HP != rat.HP {
		t.Fatalf("le tour a été consommé: round=%d, PV joueur=%d", b.Round, b.Player.HP)
	}
}

func TestFleeRateAndEffects(t *testing.T) {
	fled, failed := 0, 0
	const n = 2000
	for seed := uint64(0); seed < n; seed++ {
		b := newBattle(Fighter{HP: 1000, MaxHP: 1000, Atk: 1, Def: 0}, Fighter{HP: 1000, MaxHP: 1000, Atk: 10, Def: 0}, 0, seed)
		b.Act(Flee)
		switch b.Result {
		case Fled:
			fled++
			if b.Player.HP != 1000 {
				t.Fatalf("seed %d: le monstre a frappé malgré la fuite réussie", seed)
			}
		case Ongoing:
			failed++
			if b.Player.HP >= 1000 {
				t.Fatalf("seed %d: fuite ratée sans riposte", seed)
			}
		default:
			t.Fatalf("seed %d: issue inattendue %v", seed, b.Result)
		}
	}
	if rate := float64(fled) / n; rate < 0.42 || rate > 0.58 {
		t.Fatalf("taux de fuite %.2f, attendu environ 0.5", rate)
	}
}

func TestAbandon(t *testing.T) {
	b := newBattle(hero, rat, 0, 1)
	b.Abandon()
	if b.Result != Fled {
		t.Fatalf("Result=%v", b.Result)
	}
	// Ne remplace pas une issue déjà acquise.
	w := newBattle(Fighter{HP: 100, MaxHP: 100, Atk: 50}, rat, 0, 1)
	w.Act(Attack)
	w.Abandon()
	if w.Result != Won {
		t.Fatalf("Abandon a écrasé la victoire: %v", w.Result)
	}
}

func TestUnknownAction(t *testing.T) {
	b := newBattle(hero, rat, 0, 1)
	log := b.Act(Action(99))
	if len(log) != 1 || b.Round != 0 || b.Player.HP != hero.HP {
		t.Fatalf("action inconnue mal gérée: %v", log)
	}
}

func TestFullFightsTerminate(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		b := newBattle(hero, Fighter{HP: 20, MaxHP: 20, Atk: 5, Def: 1}, 2, seed)
		for i := 0; b.Result == Ongoing; i++ {
			if i > 200 {
				t.Fatalf("seed %d: combat sans fin", seed)
			}
			b.Act(Attack)
		}
	}
}

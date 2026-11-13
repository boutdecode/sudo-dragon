package combat

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

func rngSeed(n uint64) *rand.Rand { return rand.New(rand.NewPCG(n, n*7+1)) }

func newBattle(player, monster Fighter, potions int, seed uint64) *Battle {
	b := New(player, potions, monster, "Rat", rngSeed(seed))
	b.Lang = i18n.FR
	return b
}

var (
	// A level 1 warrior (DEF 10, 1d6 + 2) and a rat (DEF 8, 1d4, +1 to hit).
	player = Fighter{HP: 30, MaxHP: 30, Def: 10, Hit: 2, Dmg: dice.D(1, 6, 0), DmgMod: 2}
	rat    = Fighter{HP: 8, MaxHP: 8, Def: 8, Hit: 1, Dmg: dice.D(1, 4, 0)}
)

// finish plays attacks until the end of the battle (an attack may miss).
func finish(t *testing.T, b *Battle) {
	t.Helper()
	for i := 0; b.Result == Ongoing; i++ {
		if i > 500 {
			t.Fatal("endless battle")
		}
		b.Act(Attack, 0)
	}
}

func TestParseAction(t *testing.T) {
	for s, want := range map[string]Action{"attack": Attack, "item": Item, "flee": Flee} {
		if got, ok := ParseAction(s); !ok || got != want {
			t.Errorf("ParseAction(%q) = %v,%v", s, got, ok)
		}
	}
	if _, ok := ParseAction("dance"); ok {
		t.Error("unknown action accepted")
	}
}

func TestResultStrings(t *testing.T) {
	for r, want := range map[Result]string{Ongoing: "", Won: "won", Lost: "lost", Fled: "fled"} {
		if r.String() != want {
			t.Errorf("Result(%d) = %q, expected %q", r, r.String(), want)
		}
	}
}

// hitRate measures how often the attacker hits a target of the given DEF.
func hitRate(att Fighter, def int) float64 {
	hits := 0
	const n = 6000
	for seed := uint64(0); seed < n; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: def}, 0, seed)
		b.Act(Attack, 0)
		if b.Foes[0].HP < 1000 {
			hits++
		}
	}
	return float64(hits) / n
}

func TestAttackNeedsMoreThanTheDefenseOnAD20(t *testing.T) {
	att := func(hit int) Fighter {
		return Fighter{HP: 1e6, MaxHP: 1e6, Def: 10, Hit: hit, Dmg: dice.D(1, 4, 0)}
	}
	// Against DEF 10: bonus 0 => 11 to 20 (50%); +5 => 6 to 20 (75%); a natural 20 always hits (5%); a 1 never does (95%).
	for _, c := range []struct {
		hit  int
		want float64
	}{{0, 0.50}, {5, 0.75}, {-30, 0.05}, {30, 0.95}} {
		if got := hitRate(att(c.hit), 10); got < c.want-0.03 || got > c.want+0.03 {
			t.Errorf("bonus %+d: hit %.2f, expected %.2f", c.hit, got, c.want)
		}
	}
	// A more agile target (DEF 14) is harder to hit than a small creature (DEF 8).
	if hitRate(att(0), 14) >= hitRate(att(0), 8) {
		t.Error("DEF has no effect")
	}
}

func TestCriticalDoublesDiceButNotTheBonus(t *testing.T) {
	// 2d1+10 is always 12; on a critical 4d1+10 = 14. Plus 3 of modifier.
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(2, 1, 10), DmgMod: 3}
	seen := map[int]int{}
	for seed := uint64(0); seed < 3000; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10}, 0, seed)
		b.Act(Attack, 0)
		if d := 1000 - b.Foes[0].HP; d != 0 {
			seen[d]++
		}
	}
	if len(seen) != 2 || seen[15] == 0 || seen[17] == 0 {
		t.Fatalf("observed damage: %v (expected 15 normal and 17 critical)", seen)
	}
	// A critical is the natural 20: about 1 hit in 20 among those that land (95% here).
	if rate := float64(seen[17]) / float64(seen[15]+seen[17]); rate < 0.03 || rate > 0.09 {
		t.Fatalf("critical rate %.3f", rate)
	}
}

func TestArmourDiceReduceDamageButNeverBelowOne(t *testing.T) {
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 5)} // 6 damage, 12 on a critical
	for _, c := range []struct {
		red  dice.Dice
		want []int
	}{{dice.Dice{}, []int{6, 7}}, {dice.D(2, 1, 0), []int{4, 5}}, {dice.D(1, 1, 50), []int{1}}} {
		seen := map[int]bool{}
		for seed := uint64(0); seed < 400; seed++ {
			b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Reduction: c.red}, 0, seed)
			b.Act(Attack, 0)
			if d := 1000 - b.Foes[0].HP; d != 0 {
				seen[d] = true
			}
		}
		for d := range seen {
			ok := false
			for _, w := range c.want {
				ok = ok || d == w || (c.red.IsZero() && d == 7)
			}
			if !ok && d != 12-c.red.Min() && d != 12 {
				t.Errorf("reduction %v: unexpected damage %d (seen %v)", c.red, d, seen)
			}
		}
		if len(seen) == 0 {
			t.Errorf("reduction %v: no hit lands", c.red)
		}
		if c.red == dice.D(1, 1, 50) && (seen[1] != true || len(seen) != 1 && !seen[1]) {
			t.Errorf("minimum 1 not respected: %v", seen)
		}
	}
}

func TestReductionIsReportedInTheLog(t *testing.T) {
	att := Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 9)}
	for seed := uint64(0); seed < 50; seed++ {
		b := newBattle(att, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Reduction: dice.D(1, 1, 2)}, 0, seed)
		log := b.Act(Attack, 0)
		if b.Foes[0].HP < 1000 && !strings.Contains(log[0], "armure") {
			t.Fatalf("the reduction does not appear in the log: %v", log)
		}
	}
}

func TestPlayerWinsAgainstWeakMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 30, Hit: 10, Dmg: dice.D(1, 1, 50)}, rat, 0, 1)
	finish(t, b)
	if b.Result != Won || b.Foes[0].HP != 0 {
		t.Fatalf("victory expected: %v, monster HP %d", b.Result, b.Foes[0].HP)
	}
	hp := b.Player.HP
	if log := b.Act(Attack, 0); log != nil || b.Player.HP != hp {
		t.Fatalf("action accepted after the end: %v", log)
	}
}

func TestPlayerLosesAgainstStrongMonster(t *testing.T) {
	b := newBattle(Fighter{HP: 5, MaxHP: 5, Def: 0, Hit: -30, Dmg: dice.D(1, 1, 0)}, Fighter{HP: 999, MaxHP: 999, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, 0, 1)
	finish(t, b)
	if b.Result != Lost || b.Player.HP != 0 {
		t.Fatalf("defeat expected: %v, HP %d", b.Result, b.Player.HP)
	}
}

func TestDeadMonsterDoesNotStrikeBack(t *testing.T) {
	for seed := uint64(0); seed < 50; seed++ {
		b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, Fighter{HP: 5, MaxHP: 5, Def: 0, Hit: 50, Dmg: dice.D(1, 1, 9)}, 0, seed)
		before := b.Player.HP
		b.Act(Attack, 0)
		if b.Result == Won && b.Player.HP != before {
			t.Fatalf("seed %d: a dead monster struck back", seed)
		}
	}
}

func TestRoundCountsOnlyCompletedRounds(t *testing.T) {
	b := newBattle(player, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Dmg: dice.D(1, 1, 0)}, 0, 3)
	b.Act(Attack, 0)
	b.Act(Attack, 0)
	if b.Round != 2 {
		t.Fatalf("Round=%d, expected 2", b.Round)
	}
}

func TestItemHealsAndIsCapped(t *testing.T) {
	weak := Fighter{HP: 1000, MaxHP: 1000, Def: 10, Dmg: dice.D(1, 1, 0)}

	// Healing capped at max HP.
	b := newBattle(Fighter{HP: 25, MaxHP: 30, Def: 100}, weak, 2, 1)
	log := b.Act(Item, 0)
	if b.Potions != 1 {
		t.Fatalf("potions=%d, expected 1", b.Potions)
	}
	if !strings.Contains(log[0], "+5 PV") {
		t.Fatalf("capped healing expected (+5), log: %v", log)
	}

	// Full healing when there is room.
	b = newBattle(Fighter{HP: 5, MaxHP: 100, Def: 100}, weak, 1, 1)
	log = b.Act(Item, 0)
	if want := "+15 PV"; !strings.Contains(log[0], want) {
		t.Fatalf("healing of %d expected, log: %v", HealAmount, log)
	}
}

func TestItemWithoutPotionDoesNotConsumeTurn(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Item, 0)
	if len(log) != 1 || !strings.Contains(log[0], "plus de potion") {
		t.Fatalf("log: %v", log)
	}
	if b.Round != 0 || b.Player.HP != player.HP || b.Foes[0].HP != rat.HP {
		t.Fatalf("the turn was consumed: round=%d, player HP=%d", b.Round, b.Player.HP)
	}
}

func TestFleeRateAndEffects(t *testing.T) {
	fled, failed := 0, 0
	const n = 2000
	for seed := uint64(0); seed < n; seed++ {
		b := newBattle(Fighter{HP: 1000, MaxHP: 1000, Def: 0}, Fighter{HP: 1000, MaxHP: 1000, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 9)}, 0, seed)
		b.Act(Flee, 0)
		switch b.Result {
		case Fled:
			fled++
			if b.Player.HP != 1000 {
				t.Fatalf("seed %d: the monster struck despite the successful flee", seed)
			}
		case Ongoing:
			failed++
		default:
			t.Fatalf("seed %d: unexpected outcome %v", seed, b.Result)
		}
	}
	if rate := float64(fled) / n; rate < 0.42 || rate > 0.58 {
		t.Fatalf("flee rate %.2f, expected about 0.5", rate)
	}
}

func TestAbandon(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	b.Abandon()
	if b.Result != Fled {
		t.Fatalf("Result=%v", b.Result)
	}
	// Does not replace an already settled outcome.
	w := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 50)}, rat, 0, 1)
	finish(t, w)
	w.Abandon()
	if w.Result != Won {
		t.Fatalf("Abandon overwrote the victory: %v", w.Result)
	}
}

func TestUnknownAction(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Action(99), 0)
	if len(log) != 1 || b.Round != 0 || b.Player.HP != player.HP {
		t.Fatalf("unknown action badly handled: %v", log)
	}
}

func TestFullFightsTerminate(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		b := newBattle(player, Fighter{HP: 20, MaxHP: 20, Def: 10, Hit: 2, Dmg: dice.D(1, 6, 0)}, 2, seed)
		finish(t, b)
	}
}

func TestModifierAddsToDamage(t *testing.T) {
	// 1d1 + 5 of mod: at least 6 per hit (12 on a critical), never less.
	b := newBattle(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 0), DmgMod: 5}, Fighter{HP: 100000, MaxHP: 100000, Def: 10}, 0, 3)
	for range 100 {
		before := b.Foes[0].HP
		b.Act(Attack, 0)
		if d := before - b.Foes[0].HP; d != 0 && d != 6 && d != 7 {
			t.Fatalf("damage %d for 1d1 + 5", d)
		}
	}
}

func TestWithoutManaAMagicWeaponIsAClub(t *testing.T) {
	mage := Fighter{HP: 30, MaxHP: 30, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 40), Kind: hero.Magic, Mana: 1, MaxMana: 10, MeleeHit: 50, MeleeMod: 1}
	for seed := uint64(0); seed < 40; seed++ {
		b := newBattle(mage, Fighter{HP: 1000, MaxHP: 1000, Def: 10}, 0, seed)
		b.Act(Attack, 0)
		// Mana intact, turn played, club damage (1d4 + STR) and not the spell's (1d1+40).
		if d := 1000 - b.Foes[0].HP; b.Player.Mana != 1 || b.Round != 1 || d > 2*4+1 {
			t.Fatalf("seed %d: mana %d, round %d, damage %d", seed, b.Player.Mana, b.Round, d)
		}
	}
	b := newBattle(mage, rat, 0, 1)
	b.Player.Mana = 5
	b.Act(Attack, 0)
	if b.Player.Mana != 5-hero.SpellCost {
		t.Fatalf("mana after a spell: %d", b.Player.Mana)
	}
}

func TestOtherStylesCostNoMana(t *testing.T) {
	for _, k := range []hero.Kind{hero.Melee, hero.Ranged, "", "unknown"} {
		b := newBattle(Fighter{HP: 30, MaxHP: 30, Def: 10, Dmg: dice.D(1, 6, 0), Kind: k, Mana: 0}, rat, 0, 1)
		b.Act(Attack, 0)
		if b.Player.Mana != 0 || b.Round == 0 && b.Result == Ongoing {
			t.Fatalf("style %q: turn not played", k)
		}
	}
}

func TestLogShowsTheRollAgainstTheDefense(t *testing.T) {
	b := newBattle(player, rat, 0, 1)
	log := b.Act(Attack, 0)
	if !strings.Contains(log[0], "DEF 8") || !strings.Contains(log[0], "d20") {
		t.Fatalf("log without roll or DEF: %v", log)
	}
}

func group(foes ...Fighter) []Foe {
	out := make([]Foe, len(foes))
	for i, f := range foes {
		out[i] = Foe{Fighter: f, Name: "Rat " + string(rune('1'+i)), Kind: "rat", XP: 4 * (i + 1)}
	}
	return out
}

func TestEveryLivingFoeStrikesBack(t *testing.T) {
	foe := Fighter{HP: 1000, MaxHP: 1000, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 0)} // always hits: 1 HP per blow
	b := NewGroup(Fighter{HP: 100, MaxHP: 100, Def: 10}, 0, group(foe, foe, foe), rngSeed(1))
	log := b.Act(Attack, 0)
	if b.Player.HP > 100-3+1 || b.Player.HP < 100-3 { // 3 retaliations (a natural 1 may miss)
		t.Fatalf("player HP %d, 3 retaliations expected: %v", b.Player.HP, log)
	}
	if b.Round != 1 {
		t.Fatalf("Round=%d", b.Round)
	}
}

func TestAttackHitsTheChosenTargetAndFallsBack(t *testing.T) {
	weak := Fighter{HP: 1, MaxHP: 1, Def: 0}
	sturdy := Fighter{HP: 1000, MaxHP: 1000, Def: 0}
	for seed := uint64(0); seed < 200; seed++ {
		b := NewGroup(Fighter{HP: 1000, MaxHP: 1000, Def: 100, Hit: 50, Dmg: dice.D(1, 4, 50)}, 0, group(sturdy, weak, sturdy), rngSeed(seed))
		for i := 0; i < 40 && b.Foes[1].Alive(); i++ {
			b.Act(Attack, 1)
		}
		if b.Foes[1].Alive() || !b.Foes[0].Alive() || !b.Foes[2].Alive() {
			t.Fatalf("seed %d: only target 1 should have fallen (%v)", seed, b.Foes)
		}
		for i := 0; i < 40 && b.Foes[0].HP == 1000; i++ { // target slain or invalid: fallback (a natural 1 always misses)
			b.Act(Attack, 1+98*(i%2))
		}
		if b.Foes[0].HP == 1000 {
			t.Fatalf("seed %d: the fallback on the first living one did not strike", seed)
		}
	}
}

func TestWonOnlyWhenEveryFoeIsDead(t *testing.T) {
	weak := Fighter{HP: 1, MaxHP: 1, Def: 0}
	b := NewGroup(Fighter{HP: 1000, MaxHP: 1000, Def: 100, Hit: 50, Dmg: dice.D(1, 4, 50)}, 0, group(weak, weak), rngSeed(3))
	b.Act(Attack, 0)
	if b.Result != Ongoing {
		t.Fatalf("a monster is still standing, result %v", b.Result)
	}
	b.Act(Attack, 1)
	if b.Result != Won {
		t.Fatalf("victory expected, result %v", b.Result)
	}
}

func TestStrongestIgnoresTheDead(t *testing.T) {
	f := Fighter{HP: 10, MaxHP: 10}
	b := NewGroup(player, 0, group(f, f, f), rngSeed(1)) // XP 4, 8, 12
	if got := b.Strongest(); got != 2 {
		t.Fatalf("Strongest=%d, expected 2", got)
	}
	b.Foes[2].HP = 0
	if got := b.Strongest(); got != 1 {
		t.Fatalf("Strongest=%d after the strongest died, expected 1", got)
	}
	b.Foes[1].HP, b.Foes[0].HP = 0, 0
	if got := b.Strongest(); got != -1 {
		t.Fatalf("Strongest=%d with no survivor, expected -1", got)
	}
	// XP tie: the most max HP.
	b = NewGroup(player, 0, []Foe{{Fighter: Fighter{HP: 5, MaxHP: 5}, XP: 3}, {Fighter: Fighter{HP: 9, MaxHP: 9}, XP: 3}}, rngSeed(1))
	if got := b.Strongest(); got != 1 {
		t.Fatalf("XP tie: Strongest=%d, expected 1", got)
	}
}

func TestInitiativeDecidesWhoOpens(t *testing.T) {
	foe := Fighter{HP: 10, MaxHP: 10, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 0)} // always hits (except a natural 1)
	hero := Fighter{HP: 100, MaxHP: 100, Def: 10}
	for seed := uint64(0); seed < 100; seed++ {
		// Overwhelming player initiative: the monster never strikes first.
		h := hero
		h.Init = 100
		b := NewGroup(h, 0, group(foe), rngSeed(seed))
		b.Lang = i18n.FR
		log := b.Start()
		if b.Player.HP != 100 || !strings.Contains(log[0], "Initiative") || len(log) != 2 {
			t.Fatalf("seed %d: the player should have started: HP=%d %v", seed, b.Player.HP, log)
		}
		// Overwhelming monster initiative: it strikes before the first turn.
		f := foe
		f.Init = 100
		b = NewGroup(hero, 0, group(f), rngSeed(seed))
		log = b.Start()
		if len(log) != 3 || b.Player.HP > 100 {
			t.Fatalf("seed %d: the monster should have opened: %v", seed, log)
		}
	}
}

func TestInitiativeTiesGoToThePlayerAndOpeningCanKill(t *testing.T) {
	foe := Fighter{HP: 10, MaxHP: 10, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 99), Init: 100}
	b := NewGroup(Fighter{HP: 5, MaxHP: 5, Def: 10}, 0, group(foe, foe), rngSeed(1))
	b.Start()
	if b.Result != Lost && b.Player.HP == 5 { // a natural 1 may miss; try again
		b = NewGroup(Fighter{HP: 5, MaxHP: 5, Def: 10}, 0, group(foe, foe), rngSeed(2))
		b.Start()
	}
	if b.Result != Lost {
		t.Fatalf("a deadly opening blow must lose the battle: %v", b.Result)
	}
}

func TestEventsDescribeEveryAttack(t *testing.T) {
	foe := Fighter{HP: 1000, MaxHP: 1000, Def: 10, Hit: 50, Dmg: dice.D(1, 1, 0)}
	b := NewGroup(Fighter{HP: 1000, MaxHP: 1000, Def: 10, Hit: 50, Dmg: dice.D(1, 4, 0)}, 0, group(foe, foe), rngSeed(4))
	b.Act(Attack, 1)
	if len(b.Events) != 3 { // 1 player blow + 2 retaliations
		t.Fatalf("%d events, expected 3: %+v", len(b.Events), b.Events)
	}
	if e := b.Events[0]; !e.ByPlayer || e.Foe != 1 || (e.Hit && e.Dmg == 0) {
		t.Fatalf("player blow: %+v", e)
	}
	for i, e := range b.Events[1:] {
		if e.ByPlayer || e.Foe != i {
			t.Fatalf("retaliation %d: %+v", i, e)
		}
	}
	b.Act(Item, 0) // no potion: no turn played, no event
	if len(b.Events) != 0 {
		t.Fatalf("residual events: %+v", b.Events)
	}
}

func TestMissesAreEventsWithoutDamage(t *testing.T) {
	b := NewGroup(Fighter{HP: 100, MaxHP: 100, Def: 10, Hit: -100, Dmg: dice.D(1, 4, 0)}, 0, group(Fighter{HP: 10, MaxHP: 10, Def: 100, Hit: -100}), rngSeed(1))
	b.Act(Attack, 0)
	for _, e := range b.Events {
		if e.Hit && !e.Crit { // only a natural 20 always hits
			t.Fatalf("impossible hit: %+v", e)
		}
		if !e.Hit && e.Dmg != 0 {
			t.Fatalf("a miss does not wound: %+v", e)
		}
	}
}

package game

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func sheetOf(class hero.Class, mods map[hero.Ability]int) combat.Stats {
	sc := hero.Default()
	for a, v := range mods {
		sc[a] = v
	}
	return combat.NewStatsFor(class, sc)
}

func TestLevelUpRollsHitDieAndRestoresHitPointsAndMana(t *testing.T) {
	seen := map[int]bool{}
	for seed := range uint64(40) {
		sp := weak(testRat)
		sp.XP = 10 // un niveau exactement
		r := newRig(t, sp)
		r.g.rng = rand.New(rand.NewPCG(seed, 5))
		r.p.stats = sheetOf(hero.Rogue, nil)
		r.p.hp, r.p.mana = r.p.stats.MaxHP-1, 0
		before := r.p.stats.MaxHP
		r.engage()
		for r.p.fight != nil {
			r.g.act(r.p, combat.Attack, 0)
		}
		if r.p.stats.Level != 2 {
			t.Skipf("niveau %d (le joueur a pu perdre)", r.p.stats.Level)
		}
		gain := r.p.stats.MaxHP - before
		if gain < 1 || gain > 8 {
			t.Fatalf("gain de PV %d hors d8", gain)
		}
		seen[gain] = true
		if r.p.hp != r.p.stats.MaxHP || r.p.mana != r.p.stats.MaxMana {
			t.Fatalf("PV %d/%d, mana %d/%d après la montée de niveau", r.p.hp, r.p.stats.MaxHP, r.p.mana, r.p.stats.MaxMana)
		}
	}
	if len(seen) < 3 {
		t.Fatalf("le dé de vie ne varie pas: %v", seen)
	}
}

func TestWeaponKindDrivesTheAttackStyleAndModifier(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	p.stats = sheetOf(hero.Warrior, map[hero.Ability]int{hero.STR: 14, hero.AGI: 18, hero.INT: 6})
	if f := p.fighter(); f.Kind != hero.Melee || f.Hit != 2 || f.DmgMod != 2 || f.Dmg != combat.BareHands {
		t.Fatalf("mains nues: %+v", f)
	}
	for kind, c := range map[hero.Kind]struct {
		family string
		mod    int
	}{hero.Melee: {"mace", 2}, hero.Ranged: {"crossbow", 4}, hero.Magic: {"staff", -2}} {
		bow := gear.Item{ID: 1, Name: "x", Slot: gear.Weapon, Tier: 2, Family: c.family, Quality: 1, Price: 20, Kind: kind}
		p.inv = gear.Inventory{Items: []gear.Item{bow}, Worn: map[gear.Slot]int64{gear.Weapon: 1}}
		f, mod := p.fighter(), c.mod
		if f.Kind != kind || f.Hit != mod || f.DmgMod != mod || f.Dmg != bow.Damage() {
			t.Fatalf("%s: %+v (mod attendu %d)", kind, f, mod)
		}
	}
}

func TestManaRegeneratesOutsideFightsOnly(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	p.stats = sheetOf(hero.Mage, nil)
	p.mana = 0
	g.regenerate([]int{p.id})
	if p.mana != 1 {
		t.Fatalf("mana = %d", p.mana)
	}
	p.mana = p.stats.MaxMana
	g.regenerate([]int{p.id})
	if p.mana != p.stats.MaxMana {
		t.Fatalf("mana au-delà du maximum: %d", p.mana)
	}
}

func TestSpellsSpendManaInFightAndItSticks(t *testing.T) {
	sturdy := testRat
	sturdy.HP = 1000
	r := newRig(t, sturdy)
	r.p.stats = sheetOf(hero.Mage, nil)
	r.p.hp, r.p.mana = r.p.stats.MaxHP, 5
	staff := gear.Item{ID: 1, Name: "Bâton", Slot: gear.Weapon, Tier: 1, Family: "staff", Price: 20, Kind: hero.Magic}
	r.p.inv = gear.Inventory{Items: []gear.Item{staff}, Worn: map[gear.Slot]int64{gear.Weapon: 1}}
	r.engage()
	r.g.act(r.p, combat.Attack, 0)
	if r.p.mana != 5-hero.SpellCost {
		t.Fatalf("mana après un sort: %d", r.p.mana)
	}
	if c := r.last(); c.PlayerMana != r.p.mana || c.PlayerMaxMP != r.p.stats.MaxMana || c.Style != "magic" {
		t.Fatalf("état de combat envoyé: %+v", c)
	}
}

func TestCharismaMovesPricesBothWays(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats = sheetOf(hero.Warrior, map[hero.Ability]int{hero.CHA: 18}) // +4
	charming := r.g.buyPrice(r.p, r.npc)
	r.p.stats = sheetOf(hero.Warrior, nil)
	plain := r.g.buyPrice(r.p, r.npc)
	r.p.stats = sheetOf(hero.Warrior, map[hero.Ability]int{hero.CHA: 4}) // −3
	rude := r.g.buyPrice(r.p, r.npc)
	if !(charming < plain && plain < rude) || plain != potionBuyPrice {
		t.Fatalf("prix: charmant %d, banal %d, rebutant %d", charming, plain, rude)
	}
}

func TestSavedManaAndHitPointsAreClamped(t *testing.T) {
	g := newGame()
	st := combat.StatsFor(3, 0, 1, hero.Mage, hero.Default(), 9999)
	p := g.joinWith("a", func(protocol.Snapshot) {}, &Saved{Stats: st, HP: 9999, Mana: 9999, Pos: g.World.FindSpawn()})
	if p.stats.MaxHP != 6+2*6 || p.hp != p.stats.MaxHP || p.mana != p.stats.MaxMana {
		t.Fatalf("PV %d/%d mana %d/%d", p.hp, p.stats.MaxHP, p.mana, p.stats.MaxMana)
	}
	q := g.joinWith("b", func(protocol.Snapshot) {}, &Saved{Stats: st, HP: 5, Mana: -4, Pos: g.World.FindSpawn()})
	if q.mana != 0 || q.hp != 5 {
		t.Fatalf("mana %d PV %d", q.mana, q.hp)
	}
}

func TestFighterDefenseIsTenPlusAgilityPlusGear(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	p.stats = sheetOf(hero.Rogue, map[hero.Ability]int{hero.AGI: 16}) // +3
	if f := p.fighter(); f.Def != 13 || !f.Reduction.IsZero() {
		t.Fatalf("sans équipement: %+v", f)
	}
	p.inv = gear.Inventory{
		Items: []gear.Item{
			{ID: 1, Name: "Cotte", Slot: gear.Armor, Tier: 2, Family: "heavy", Def: 1, Price: 50},
			{ID: 2, Name: "Rondache", Slot: gear.Shield, Tier: 1, Def: 1, Price: 50},
			{ID: 3, Name: "Casque", Slot: gear.Helm, Tier: 1, Def: 1, Price: 50},
		},
		Worn: map[gear.Slot]int64{gear.Armor: 1, gear.Shield: 2, gear.Helm: 3},
	}
	f := p.fighter()
	if f.Def != 16 || f.Reduction.String() != "2d8" {
		t.Fatalf("DEF %d, réduction %s", f.Def, f.Reduction)
	}
	if st := statsOf(p); st.Def != 16 {
		t.Fatalf("DEF envoyée %d", st.Def)
	}
	var bag protocol.Bag
	p.sendBagMsg = func(b protocol.Bag) { bag = b }
	g.sendBag(p)
	if bag.DefBonus != 3 || bag.Reduction != "2d8" || bag.Items[0].Reduction != "2d8" || bag.Items[0].Family != "heavy" {
		t.Fatalf("sac: %+v", bag)
	}
}

func TestSmallMediumAndLargeMonstersHaveTheirDefense(t *testing.T) {
	for kind, def := range map[string]int{"rat": 8, "crab": 8, "wolf": 11, "goblin": 10, "yeti": 12, "skeleton": 10, "knight": 12, "warlord": 13} {
		if got := speciesNamed(t, kind).fighter().Def; got != def {
			t.Errorf("%s: DEF %d, attendu %d", kind, got, def)
		}
	}
}

func TestTwoHandedWeaponAndShieldAreRefusedTogether(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	p.stats.Level = 5
	var notices []string
	p.sendBagMsg = func(b protocol.Bag) { notices = append(notices, b.Notice...) }
	p.inv.Add(gear.Item{Name: "Espadon", Slot: gear.Weapon, Tier: 2, Family: "greatsword", Price: 100})
	p.inv.Add(gear.Item{Name: "Rondache", Slot: gear.Shield, Tier: 1, Def: 1, Price: 30})
	g.GearActNow(p, protocol.GearEquip, 2, "")
	g.GearActNow(p, protocol.GearEquip, 1, "")
	if p.inv.IsWorn(1) || !p.inv.IsWorn(2) || len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "deux mains") {
		t.Fatalf("espadon avec bouclier: %v", notices)
	}
}

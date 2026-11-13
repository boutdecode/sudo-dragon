package game

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Dice combat balance. Level gives neither hit nor damage: power comes from the weapon
// (rank = tier, accessible by level), the armour (DEF and reduction dice) and the characteristics.

// rankAt is the highest equipment rank accessible at a level.
func rankAt(level int) int {
	r := 1
	for t := 1; t <= gear.MaxTier; t++ {
		if gear.MinLevelOf[t] <= level {
			r = t
		}
	}
	return r
}

// bestKit is the typical equipment of a serious player: weapon of the given family at its rank, maximum quality, intermediate armour,
// shield (except two-handed weapons) and helmet at the rank's caps.
func bestKit(rank int, weapon string) []gear.Item {
	w := gear.Item{Name: "weapon", Slot: gear.Weapon, Tier: rank, Family: weapon, Quality: rank}
	_, ahi := gear.Bounds(gear.Armor, rank)
	_, shi := gear.Bounds(gear.Shield, rank)
	_, hhi := gear.Bounds(gear.Helm, rank)
	items := []gear.Item{w,
		{Name: "armor", Slot: gear.Armor, Tier: rank, Family: "medium", Def: ahi},
		{Name: "helmet", Slot: gear.Helm, Tier: rank, Def: hhi}}
	if f, _ := gear.FamilyByID(gear.Weapon, weapon); !f.TwoHanded {
		items = append(items, gear.Item{Name: "shield", Slot: gear.Shield, Tier: rank, Def: shi})
	}
	return items
}

// playerWith builds a test player of the given class, level and characteristics, equipped with items.
func playerWith(class hero.Class, level int, scores hero.Scores, items []gear.Item) *player {
	st := combat.StatsFor(level, 0, 2, class, scores, 0)
	p := &player{stats: st, hp: st.MaxHP, mana: st.MaxMana}
	for _, it := range items {
		added, _ := p.inv.Add(it)
		if p.inv.Worn == nil {
			p.inv.Worn = map[gear.Slot]int64{}
		}
		p.inv.Worn[it.Slot] = added.ID
	}
	return p
}

// winRateGroup is like winRate, against a group of species (the player always hits the first living one).
func winRateGroup(p *player, sps []species, trials int) float64 {
	wins := 0
	for i := 0; i < trials; i++ {
		foes := make([]combat.Foe, len(sps))
		for j, sp := range sps {
			foes[j] = combat.Foe{Fighter: sp.fighter(), Name: sp.Kind, Kind: sp.Kind, XP: sp.XP}
		}
		b := combat.NewGroup(p.fighter(), p.stats.Potions, foes, rand.New(rand.NewPCG(uint64(i), 11)))
		b.Start()
		for turn := 0; b.Result == combat.Ongoing && turn < 500; turn++ {
			if b.Player.HP <= p.stats.MaxHP/3 && b.Potions > 0 {
				b.Act(combat.Item, 0)
			} else {
				b.Act(combat.Attack, -1)
			}
		}
		if b.Result == combat.Won {
			wins++
		}
	}
	return float64(wins) / float64(trials)
}

// winRate simulates complete fights (potion below a third of HP) and returns the win frequency.
func winRate(p *player, sp species, trials int) float64 {
	wins := 0
	for i := 0; i < trials; i++ {
		b := combat.New(p.fighter(), p.stats.Potions, sp.fighter(), sp.name(i18n.FR), rand.New(rand.NewPCG(uint64(i), 11)))
		b.Start()
		for turn := 0; b.Result == combat.Ongoing && turn < 500; turn++ {
			if b.Player.HP <= p.stats.MaxHP/3 && b.Potions > 0 {
				b.Act(combat.Item, 0)
			} else {
				b.Act(combat.Attack, 0)
			}
		}
		if b.Result == combat.Won {
			wins++
		}
	}
	return float64(wins) / float64(trials)
}

var (
	warriorScores = hero.Scores{14, 12, 10, 12, 10, 10} // a decent warrior: STR +2, AGI +1, CON +1
	rogueScores   = hero.Scores{10, 15, 10, 12, 10, 10}
	mageScores    = hero.Scores{8, 12, 15, 12, 13, 10}
)

// Balance encodes the design intent, from the beginner (starting weapon, DEF 11) to the serious player
// equipped with the best of the rank accessible at their level.
func TestBalance(t *testing.T) {
	type kitKind int
	const (
		start kitKind = iota // starting weapon only
		full                 // best equipment of the rank (longsword, staff or bow)
	)
	cases := []struct {
		class    hero.Class
		scores   hero.Scores
		kit      kitKind
		level    int
		kind     string
		min, max float64
		why      string
	}{
		{hero.Warrior, warriorScores, start, 1, "rat", 0.95, 1, "a beginner warrior beats a rat"},
		{hero.Warrior, warriorScores, start, 1, "wolf", 0.60, 0.90, "...and a wolf, barely"},
		{hero.Warrior, warriorScores, start, 1, "yeti", 0, 0.05, "...but not a yeti"},
		{hero.Warrior, warriorScores, full, 3, "skeleton", 0.70, 1, "the skeleton falls around level 3-4"},
		{hero.Warrior, warriorScores, full, 3, "yeti", 0.50, 0.95, "the yeti becomes playable at level 3"},
		{hero.Warrior, warriorScores, full, 5, "yeti", 0.95, 1, "a seasoned player beats a yeti"},
		{hero.Warrior, warriorScores, full, 5, "knight", 0.50, 0.90, "the knight is a challenge at level 5"},
		{hero.Warrior, warriorScores, full, 8, "knight", 0.85, 1, "...beaten around level 8"},
		{hero.Warrior, warriorScores, full, 10, "warlord", 0, 0.05, "the warlord is out of reach at level 10"},
		{hero.Warrior, warriorScores, full, 13, "warlord", 0, 0.20, "...and still at level 13"},
		{hero.Warrior, warriorScores, full, 18, "warlord", 0.70, 1, "...he falls around level 16-18"},
		{hero.Warrior, warriorScores, full, 20, "warlord", 0.95, 1, "...and surely at level 20, equipped"},
		{hero.Warrior, warriorScores, start, 5, "yeti", 0, 0.30, "without equipment, level 5 is not enough for a yeti"},
		{hero.Warrior, warriorScores, start, 20, "warlord", 0, 0.05, "equipment is essential against the warlord"},
		{hero.Rogue, rogueScores, start, 1, "rat", 0.90, 1, "a beginner rogue beats a rat"},
		{hero.Rogue, rogueScores, start, 1, "wolf", 0.30, 0.80, "...but the wolf is risky"},
		{hero.Mage, mageScores, start, 1, "rat", 0.80, 1, "a beginner mage, fragile, beats a rat"},
		{hero.Mage, mageScores, start, 1, "wolf", 0, 0.40, "...and avoids the wolf"},
		{hero.Mage, mageScores, start, 3, "wolf", 0.75, 1, "...which he beats from level 3"},
		{hero.Mage, mageScores, full, 8, "yeti", 0.90, 1, "an equipped mage beats a yeti at level 8"},
	}
	fam := map[hero.Class]string{hero.Warrior: "longsword", hero.Rogue: "shortbow", hero.Mage: "staff"}
	for _, c := range cases {
		items := []gear.Item{gear.Starter(i18n.FR, c.class)}
		if c.kit == full {
			items = bestKit(rankAt(c.level), fam[c.class])
		}
		p := playerWith(c.class, c.level, c.scores, items)
		got := winRate(p, speciesNamed(t, c.kind), 400)
		if got < c.min || got > c.max {
			t.Errorf("%s level %d (%v) against %s: %.0f %% wins, expected [%.0f %%, %.0f %%] (%s)",
				c.class, c.level, c.kit, c.kind, got*100, c.min*100, c.max*100, c.why)
		}
	}
}

// Groups are the exception that demands equipment: a beginner does not meet them at spawn (see packLevels),
// and the equipment level determines whether they overcome them.
func TestGroupBalance(t *testing.T) {
	cases := []struct {
		level    int
		kinds    []string
		min, max float64
		why      string
	}{
		{3, []string{"wolf", "wolf"}, 0.85, 1, "two wolves are doable at level 3, equipped"},
		{3, []string{"yeti", "wolf"}, 0.20, 0.80, "a yeti and its wolf remain a challenge at level 3"},
		{5, []string{"wolf", "wolf", "wolf"}, 0.90, 1, "a pack of three wolves falls at level 5"},
		{5, []string{"skeleton", "knight"}, 0.15, 0.70, "skeleton and knight: a challenge at level 5"},
		{10, []string{"skeleton", "knight"}, 0.90, 1, "...who falls at level 10"},
		{10, []string{"warlord", "knight"}, 0, 0.10, "the accompanied warlord stays out of reach at level 10"},
	}
	for _, c := range cases {
		sps := make([]species, len(c.kinds))
		for i, k := range c.kinds {
			sps[i] = speciesNamed(t, k)
		}
		p := playerWith(hero.Warrior, c.level, warriorScores, bestKit(rankAt(c.level), "longsword"))
		if got := winRateGroup(p, sps, 300); got < c.min || got > c.max {
			t.Errorf("level %d against %v: %.0f %% wins, expected [%.0f %%, %.0f %%] (%s)", c.level, c.kinds, got*100, c.min*100, c.max*100, c.why)
		}
	}
}

// The further away, the stronger the creatures: a player of the region's level, equipped for their rank,
// beats ordinary monsters; a beginner who ventures too far can no longer.
func TestRegionLevelBalance(t *testing.T) {
	for _, lv := range []int{5, 10, 15, 20} {
		p := playerWith(hero.Warrior, lv, warriorScores, bestKit(rankAt(lv), "longsword"))
		for _, k := range []string{"rat", "wolf", "goblin"} {
			if got := winRate(p, scaledForLevel(speciesNamed(t, k), lv), 300); got < 0.9 {
				t.Errorf("level %d against %s of the region: %.0f %% wins, expected at least 90 %%", lv, k, got*100)
			}
		}
	}
	rookie := playerWith(hero.Warrior, 3, warriorScores, bestKit(rankAt(3), "longsword"))
	for _, k := range []string{"wolf", "goblin"} {
		if got := winRate(rookie, scaledForLevel(speciesNamed(t, k), 16), 300); got > 0.25 {
			t.Errorf("level 3 against %s of level 16: %.0f %% wins, expected at most 10 %%", k, got*100)
		}
	}
}

func TestScaledForLevel(t *testing.T) {
	base := speciesNamed(t, "wolf")
	if got := scaledForLevel(base, 1); got.HP != base.HP || got.Hit != base.Hit || got.Dmg != base.Dmg || got.XP != base.XP {
		t.Errorf("level 1 changes nothing: %+v", got)
	}
	lo, hi := scaledForLevel(base, 6), scaledForLevel(base, 20)
	if !(base.HP < lo.HP && lo.HP < hi.HP && base.XP < lo.XP && lo.XP < hi.XP && lo.Hit < hi.Hit && lo.Dmg.Bonus < hi.Dmg.Bonus) || hi.Level != 20 {
		t.Errorf("strength grows with level: %+v %+v", lo, hi)
	}
}

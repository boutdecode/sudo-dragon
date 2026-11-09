package game

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Équilibrage du combat par dés. Le niveau ne donne ni toucher ni dégâts : la puissance vient de l'arme
// (rang = palier, accessible par niveau), de l'armure (DEF et dés de réduction) et des caractéristiques.

// rankAt est le plus haut rang d'équipement accessible à un niveau.
func rankAt(level int) int {
	r := 1
	for t := 1; t <= gear.MaxTier; t++ {
		if gear.MinLevelOf[t] <= level {
			r = t
		}
	}
	return r
}

// bestKit est l'équipement typique d'un joueur sérieux : arme de la famille donnée de son rang, qualité maximale, armure intermédiaire,
// bouclier (sauf arme à deux mains) et casque aux plafonds du rang.
func bestKit(rank int, weapon string) []gear.Item {
	w := gear.Item{Name: "arme", Slot: gear.Weapon, Tier: rank, Family: weapon, Quality: rank}
	_, ahi := gear.Bounds(gear.Armor, rank)
	_, shi := gear.Bounds(gear.Shield, rank)
	_, hhi := gear.Bounds(gear.Helm, rank)
	items := []gear.Item{w,
		{Name: "armure", Slot: gear.Armor, Tier: rank, Family: "medium", Def: ahi},
		{Name: "casque", Slot: gear.Helm, Tier: rank, Def: hhi}}
	if f, _ := gear.FamilyByID(gear.Weapon, weapon); !f.TwoHanded {
		items = append(items, gear.Item{Name: "bouclier", Slot: gear.Shield, Tier: rank, Def: shi})
	}
	return items
}

// playerWith construit un joueur de test de la classe, du niveau et des caractéristiques donnés, équipé de items.
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

// winRateGroup comme winRate, contre un groupe d'espèces (le joueur frappe toujours le premier vivant).
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

// winRate simule des combats complets (potion sous le tiers des PV) et renvoie la fréquence de victoire.
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
	warriorScores = hero.Scores{14, 12, 10, 12, 10, 10} // un guerrier correct : FOR +2, AGI +1, CON +1
	rogueScores   = hero.Scores{10, 15, 10, 12, 10, 10}
	mageScores    = hero.Scores{8, 12, 15, 12, 13, 10}
)

// L'équilibrage encode l'intention de conception, du débutant (arme de départ, DEF 11) au joueur sérieux
// équipé du meilleur du rang accessible à son niveau.
func TestBalance(t *testing.T) {
	type kitKind int
	const (
		start kitKind = iota // arme de départ seulement
		full                 // meilleur équipement du rang (épée longue, bâton ou arc)
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
		{hero.Warrior, warriorScores, start, 1, "rat", 0.95, 1, "un guerrier débutant bat un rat"},
		{hero.Warrior, warriorScores, start, 1, "wolf", 0.60, 0.90, "…et un loup, de justesse"},
		{hero.Warrior, warriorScores, start, 1, "yeti", 0, 0.05, "…mais pas un yéti"},
		{hero.Warrior, warriorScores, full, 3, "skeleton", 0.70, 1, "le squelette tombe vers le niveau 3-4"},
		{hero.Warrior, warriorScores, full, 3, "yeti", 0.50, 0.95, "le yéti devient jouable au niveau 3"},
		{hero.Warrior, warriorScores, full, 5, "yeti", 0.95, 1, "un joueur aguerri bat un yéti"},
		{hero.Warrior, warriorScores, full, 5, "knight", 0.50, 0.90, "le chevalier est un défi au niveau 5"},
		{hero.Warrior, warriorScores, full, 8, "knight", 0.85, 1, "…que l'on bat vers le niveau 8"},
		{hero.Warrior, warriorScores, full, 10, "warlord", 0, 0.05, "le seigneur de guerre est hors de portée au niveau 10"},
		{hero.Warrior, warriorScores, full, 13, "warlord", 0, 0.20, "…et encore au niveau 13"},
		{hero.Warrior, warriorScores, full, 18, "warlord", 0.70, 1, "…il tombe vers le niveau 16-18"},
		{hero.Warrior, warriorScores, full, 20, "warlord", 0.95, 1, "…et sûrement au niveau 20, équipé"},
		{hero.Warrior, warriorScores, start, 5, "yeti", 0, 0.30, "sans équipement, le niveau 5 ne suffit pas pour un yéti"},
		{hero.Warrior, warriorScores, start, 20, "warlord", 0, 0.05, "l'équipement est indispensable contre le seigneur de guerre"},
		{hero.Rogue, rogueScores, start, 1, "rat", 0.90, 1, "un gredin débutant bat un rat"},
		{hero.Rogue, rogueScores, start, 1, "wolf", 0.30, 0.80, "…mais le loup est risqué"},
		{hero.Mage, mageScores, start, 1, "rat", 0.80, 1, "un magicien débutant, fragile, bat un rat"},
		{hero.Mage, mageScores, start, 1, "wolf", 0, 0.40, "…et évite le loup"},
		{hero.Mage, mageScores, start, 3, "wolf", 0.75, 1, "…qu'il bat dès le niveau 3"},
		{hero.Mage, mageScores, full, 8, "yeti", 0.90, 1, "un magicien équipé bat un yéti au niveau 8"},
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
			t.Errorf("%s niveau %d (%v) contre %s : %.0f %% de victoires, attendu [%.0f %%, %.0f %%] (%s)",
				c.class, c.level, c.kit, c.kind, got*100, c.min*100, c.max*100, c.why)
		}
	}
}

// Les groupes sont l'exception qui exige de l'équipement : un débutant n'en croise pas à l'apparition (voir packLevels),
// et le niveau d'équipement détermine s'il en vient à bout.
func TestGroupBalance(t *testing.T) {
	cases := []struct {
		level    int
		kinds    []string
		min, max float64
		why      string
	}{
		{3, []string{"wolf", "wolf"}, 0.85, 1, "deux loups sont faisables au niveau 3, équipé"},
		{3, []string{"yeti", "wolf"}, 0.20, 0.80, "un yéti et son loup restent un défi au niveau 3"},
		{5, []string{"wolf", "wolf", "wolf"}, 0.90, 1, "une meute de trois loups tombe au niveau 5"},
		{5, []string{"skeleton", "knight"}, 0.15, 0.70, "squelette et chevalier : un défi au niveau 5"},
		{10, []string{"skeleton", "knight"}, 0.90, 1, "…qui tombe au niveau 10"},
		{10, []string{"warlord", "knight"}, 0, 0.10, "le seigneur de guerre accompagné reste hors de portée au niveau 10"},
	}
	for _, c := range cases {
		sps := make([]species, len(c.kinds))
		for i, k := range c.kinds {
			sps[i] = speciesNamed(t, k)
		}
		p := playerWith(hero.Warrior, c.level, warriorScores, bestKit(rankAt(c.level), "longsword"))
		if got := winRateGroup(p, sps, 300); got < c.min || got > c.max {
			t.Errorf("niveau %d contre %v : %.0f %% de victoires, attendu [%.0f %%, %.0f %%] (%s)", c.level, c.kinds, got*100, c.min*100, c.max*100, c.why)
		}
	}
}

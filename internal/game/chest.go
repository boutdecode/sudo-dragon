package game

import (
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Coffres de donjon : on les ouvre en marchant dessus, une seule fois par instance (le premier arrivé les vide).
// Ils donnent de l'or, parfois des potions et parfois une pièce d'équipement. Le coffre du boss est verrouillé
// tant que le boss vit, et contient toujours un objet, un cran au-dessus du palier du donjon.

const (
	chestGoldPerTier = 15   // or minimal par palier ; le maximum est le double
	chestGearChance  = 0.35 // part des coffres ordinaires qui contiennent un objet
	chestPotionLuck  = 0.40
)

// openChest ouvre le coffre en pos pour p. Un coffre verrouillé, ou qui contient un objet que le sac plein ne peut
// recevoir, reste fermé : on peut revenir.
func (g *Game) openChest(p *player, inst *instance, pos world.Pos) {
	if inst.opened[pos] {
		return
	}
	boss := pos == inst.dmap.BossChest
	if boss && !inst.bossDown {
		g.chestNotice(p, i18n.T(p.lang, "dungeon.chest.locked"))
		return
	}
	tier := inst.d.Tier
	var loot *gear.Item
	if boss || g.rng.Float64() < chestGearChance {
		if len(p.inv.Items) >= gear.BagCapacity {
			g.chestNotice(p, i18n.T(p.lang, "dungeon.chest.bag_full"))
			return
		}
		lootTier := tier
		if boss {
			lootTier++
		}
		it := gear.DungeonLoot(p.lang, inst.d.Region, lootTier, g.rng.Uint64())
		loot = &it
	}
	inst.opened[pos] = true

	gold := p.stats.AddGold(chestGoldPerTier*tier + g.rng.IntN(chestGoldPerTier*tier+1))
	potions := 0
	if g.rng.Float64() < chestPotionLuck {
		potions = p.stats.AddPotions(1 + g.rng.IntN(2))
	}
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.open", gold, potions))
	if loot != nil {
		added, _ := p.inv.Add(*loot) // la place a été vérifiée plus haut
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.item", added.Name))
		p.gearDirty = true
	}
	g.sendBag(p)
	g.sendZoneToAll(inst) // tous voient le coffre vidé
	g.save(p)
}

func (g *Game) chestNotice(p *player, s string) {
	p.gearNotices = append(p.gearNotices, s)
	g.sendBag(p)
}

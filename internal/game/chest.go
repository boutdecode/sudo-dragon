package game

import (
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Dungeon chests: opened by walking onto them, once per instance (the first to arrive empties them).
// They give gold, sometimes potions and sometimes a piece of gear. The boss chest is locked
// while the boss lives, and always holds an item, one rank above the dungeon tier.

const (
	chestGoldPerTier = 15   // gold minimum per tier; the maximum is double
	chestGearChance  = 0.35 // share of ordinary chests that contain an item
	chestPotionLuck  = 0.40
)

// openChest opens the chest at pos for p. A locked chest, or one holding an item that a full bag cannot
// receive, stays closed: the player can come back.
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
		added, _ := p.inv.Add(*loot) // room was checked above
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.item", added.Name))
		p.gearDirty = true
	}
	g.sendBag(p)
	g.sendZoneToAll(inst) // everyone sees the emptied chest
	g.save(p)
}

func (g *Game) chestNotice(p *player, s string) {
	p.gearNotices = append(p.gearNotices, s)
	g.sendBag(p)
}

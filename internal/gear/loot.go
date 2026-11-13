package gear

import (
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// DungeonLoot builds the piece of equipment from a dungeon chest, in language l, without a language model.
// roll decides everything (slot, style, name): the same roll gives the same item. The tier is clamped to
// [1, MaxTier]. The item gets a quality bonus (weapon) or DEF bonus at the maximum allowed, like a
// vendor's stock, and its origin vendor is the dungeon (Index -1; see OriginDungeon).
func DungeonLoot(l i18n.Lang, dungeon world.RegionPos, tier int, roll uint64) Item {
	return loot(l, "gear.desc.dungeon", dungeon, tier, roll)
}

// MonsterLoot builds the piece of equipment that a monster killed in region rp leaves behind. Same
// crafting and same bounds as DungeonLoot; only the description changes.
func MonsterLoot(l i18n.Lang, rp world.RegionPos, tier int, roll uint64) Item {
	return loot(l, "gear.desc.monster", rp, tier, roll)
}

// TierForLevel gives the highest equipment tier accessible at a level (see MinLevelOf).
func TierForLevel(level int) int {
	t := 1
	for k := 2; k <= MaxTier; k++ {
		if level >= MinLevelOf[k] {
			t = k
		}
	}
	return t
}

func loot(l i18n.Lang, descKey string, dungeon world.RegionPos, tier int, roll uint64) Item {
	tier = max(1, min(tier, MaxTier))
	slot := Slots[roll%uint64(len(Slots))]
	roll /= uint64(len(Slots))

	var kind hero.Kind
	idx := make([]int, namesPerSlot[slot])
	for j := range idx {
		idx[j] = j
	}
	if slot == Weapon {
		kind = weaponKinds[roll%uint64(len(weaponKinds))]
		roll /= uint64(len(weaponKinds))
		idx = weaponNames[kind]
	}
	pick := idx[roll%uint64(len(idx))]

	it := Item{
		Name: itemName(l, slot, pick, tier), Desc: i18n.T(l, descKey),
		Slot: slot, Tier: tier, Origin: OriginDungeon(dungeon),
	}
	_, hi := Bounds(slot, tier)
	switch slot {
	case Weapon:
		it.Family, it.Quality = weaponNameFamilies[pick], hi
	case Armor:
		it.Family, it.Def = armorNameFamilies[pick], hi
	default:
		it.Def = hi
	}
	it = shape(it, kind)
	lo, up := PriceBounds(tier, it.Power())
	it.Price = (lo + up) / 2
	return it
}

// OriginDungeon is the origin of an item found in the dungeon of region rp: the key of a "vendor" with index
// -1, which no NPC carries. It fits in the save columns, without migration.
func OriginDungeon(rp world.RegionPos) world.NPCKey { return world.NPCKey{Region: rp, Index: -1} }

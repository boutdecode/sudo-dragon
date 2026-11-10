package gear

import (
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// DungeonLoot fabrique la pièce d'équipement d'un coffre de donjon, dans la langue l, sans modèle de langage.
// roll décide de tout (emplacement, style, nom) : le même tirage donne le même objet. Le palier est borné à
// [1, MaxTier]. L'objet reçoit un bonus de qualité (arme) ou de DEF au maximum permis, comme l'assortiment d'un
// vendeur, et son vendeur d'origine est le donjon (Index -1 ; voir OriginDungeon).
func DungeonLoot(l i18n.Lang, dungeon world.RegionPos, tier int, roll uint64) Item {
	return loot(l, "gear.desc.dungeon", dungeon, tier, roll)
}

// MonsterLoot fabrique la pièce d'équipement qu'un monstre tué dans la région rp laisse derrière lui. Même
// fabrication et mêmes bornes que DungeonLoot; seule la description change.
func MonsterLoot(l i18n.Lang, rp world.RegionPos, tier int, roll uint64) Item {
	return loot(l, "gear.desc.monster", rp, tier, roll)
}

// TierForLevel donne le plus haut palier d'équipement accessible à un niveau (voir MinLevelOf).
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

// OriginDungeon est l'origine d'un objet trouvé dans le donjon de la région rp : la clé d'un « vendeur » d'indice
// -1, qu'aucun PNJ ne porte. Elle tient dans les colonnes de la sauvegarde, sans migration.
func OriginDungeon(rp world.RegionPos) world.NPCKey { return world.NPCKey{Region: rp, Index: -1} }

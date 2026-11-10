package gear

import (
	"fmt"
	"time"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Assortiment de repli : déterministe, sans modèle de langage. Il rend le jeu complet sans clé d'API
// et sert tant que l'étalage généré n'est pas disponible. Noms et prix suivent les mêmes formules
// que les objets proposés par le modèle.

func hash(k world.NPCKey, salt uint64) uint64 {
	x := uint64(int64(k.Region.X))*0x9E3779B97F4A7C15 ^ uint64(int64(k.Region.Y))*0xC2B2AE3D27D4EB4F ^ uint64(k.Index)*0x165667B19E3779F9 ^ salt
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// namesPerSlot est le nombre de noms de base du catalogue pour chaque emplacement.
var namesPerSlot = map[Slot]int{Weapon: 12, Armor: 5, Shield: 4, Helm: 4}

// weaponNames range les noms d'armes par style (indices de gear.name.weapon.N).
var weaponNames = map[hero.Kind][]int{hero.Melee: {0, 1, 2, 3, 4, 5, 10, 11}, hero.Ranged: {6, 7}, hero.Magic: {8, 9}}

// weaponKinds : les armes du repli tournent entre les trois styles.
var weaponKinds = [...]hero.Kind{hero.Melee, hero.Ranged, hero.Magic}

// itemName compose « Épée longue en acier » / « Steel long sword » à partir du nom de base et du matériau.
func itemName(l i18n.Lang, slot Slot, base, tier int) string {
	b := i18n.T(l, fmt.Sprintf("gear.name.%s.%d", slot, base))
	m := i18n.T(l, fmt.Sprintf("gear.material.%d", tier))
	if l.Normalize() == i18n.EN {
		return i18n.T(l, "gear.name.fmt", m, b)
	}
	return i18n.T(l, "gear.name.fmt", b, m)
}

// Fallback fabrique l'assortiment de repli du vendeur npc dans la langue l.
func Fallback(npc world.NPCKey, ctx Context, l i18n.Lang) Wares {
	kind := "smith"
	if len(ctx.Slots) > 1 {
		kind = "armorer"
	}
	w := Wares{NPC: npc, Version: 0, CreatedAt: time.Time{}}
	w.Specialty = i18n.Pick(l, "gear.specialty."+kind, int(hash(npc, 1)%1000))

	n := 6
	if len(ctx.Slots) > 1 {
		n = 8
	}
	maxTier := max(1, min(ctx.MaxTier, MaxTier))
	minTier := max(1, min(ctx.MinTier, maxTier))
	used := map[string]bool{}
	for i := 0; i < n; i++ {
		slot := ctx.Slots[i%len(ctx.Slots)]
		tier := minTier + i*(maxTier-minTier+1)/n
		var name string
		idx := make([]int, namesPerSlot[slot]) // indices de noms utilisables pour cet emplacement
		for j := range idx {
			idx[j] = j
		}
		var kind hero.Kind
		if slot == Weapon {
			kind = weaponKinds[i%len(weaponKinds)]
			idx = weaponNames[kind]
		}
		span := len(idx)
		base := int(hash(npc, uint64(10+i)) % uint64(span))
		pick := idx[base]
		for try := 0; try < span; try++ {
			pick = idx[(base+try)%span]
			name = itemName(l, slot, pick, tier)
			if !used[name] {
				break
			}
		}
		used[name] = true
		it := Item{ID: int64(i + 1), Name: name, Desc: i18n.T(l, "gear.desc.fallback"), Slot: slot, Tier: tier, Origin: npc}
		_, hi := Bounds(slot, tier)
		switch slot {
		case Weapon:
			it.Family, it.Quality = weaponNameFamilies[pick], tier/2
		case Armor:
			it.Family, it.Def = armorNameFamilies[pick], hi
		default:
			it.Def = hi
		}
		it = shape(it, kind)
		lo, up := PriceBounds(tier, it.Power())
		it.Price = (lo + up) / 2
		w.Items = append(w.Items, it)
	}
	return w
}

// Starter est l'arme de départ d'un personnage de la classe donnée: épée courte, arc court ou bâton de
// rang 1, qualité 0. Elle n'a ni identifiant ni vendeur d'origine.
func Starter(l i18n.Lang, class hero.Class) Item {
	pick := 0 // épée courte
	switch class {
	case hero.Rogue:
		pick = 6 // arc court
	case hero.Mage:
		pick = 8 // bâton
	}
	it := Item{Name: itemName(l, Weapon, pick, 1), Desc: i18n.T(l, "gear.desc.starter"), Slot: Weapon, Tier: 1, Family: weaponNameFamilies[pick]}
	it = shape(it, hero.Melee)
	lo, up := PriceBounds(1, it.Power())
	it.Price = (lo + up) / 2
	return it
}

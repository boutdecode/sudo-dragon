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
var namesPerSlot = map[Slot]int{Weapon: 10, Armor: 5, Shield: 4, Helm: 4}

// weaponNames range les noms d'armes par style : 0-5 mêlée, 6-7 distance, 8-9 magie.
var weaponNames = map[hero.Kind][2]int{hero.Melee: {0, 6}, hero.Ranged: {6, 8}, hero.Magic: {8, 10}}

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
	used := map[string]bool{}
	for i := 0; i < n; i++ {
		slot := ctx.Slots[i%len(ctx.Slots)]
		tier := 1 + i*maxTier/n
		var name string
		lo, span := 0, namesPerSlot[slot]
		var kind hero.Kind
		if slot == Weapon {
			kind = weaponKinds[i%len(weaponKinds)]
			r := weaponNames[kind]
			lo, span = r[0], r[1]-r[0]
		}
		base := int(hash(npc, uint64(10+i)) % uint64(span))
		for try := 0; try < span; try++ {
			name = itemName(l, slot, lo+(base+try)%span, tier)
			if !used[name] {
				break
			}
		}
		used[name] = true
		_, hi := Bounds(slot, tier)
		it := Item{ID: int64(i + 1), Name: name, Desc: i18n.T(l, "gear.desc.fallback"), Slot: slot, Tier: tier, Kind: kind, Origin: npc}
		if slot == Weapon {
			it.Atk = hi
		} else {
			it.Def = hi
		}
		lo, up := PriceBounds(tier, hi)
		it.Price = (lo + up) / 2
		w.Items = append(w.Items, it)
	}
	return w
}

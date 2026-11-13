package gear

import (
	"github.com/kevin/ascii-adventure/internal/hero"
)

// Weapon or armour family: it sets the damage type (a weapon's die: slashing 4, cutting 6,
// blunt 8, two-handed 12; an armour's reduction die: light 4, medium 6, heavy 8).
// The language model may CHOOSE a family but never the dice themselves.
type Family struct {
	ID        string
	Slot      Slot
	Kind      hero.Kind // style of a weapon
	Die       int
	TwoHanded bool
}

// Weapon families.
var weaponFamilies = []Family{
	{ID: "dagger", Slot: Weapon, Kind: hero.Melee, Die: 4},
	{ID: "spear", Slot: Weapon, Kind: hero.Melee, Die: 4},
	{ID: "shortsword", Slot: Weapon, Kind: hero.Melee, Die: 6},
	{ID: "longsword", Slot: Weapon, Kind: hero.Melee, Die: 6},
	{ID: "axe", Slot: Weapon, Kind: hero.Melee, Die: 6},
	{ID: "mace", Slot: Weapon, Kind: hero.Melee, Die: 8},
	{ID: "greatsword", Slot: Weapon, Kind: hero.Melee, Die: 12, TwoHanded: true},
	{ID: "greataxe", Slot: Weapon, Kind: hero.Melee, Die: 12, TwoHanded: true},
	{ID: "shortbow", Slot: Weapon, Kind: hero.Ranged, Die: 6, TwoHanded: true},
	{ID: "crossbow", Slot: Weapon, Kind: hero.Ranged, Die: 8, TwoHanded: true},
	{ID: "wand", Slot: Weapon, Kind: hero.Magic, Die: 4},
	{ID: "staff", Slot: Weapon, Kind: hero.Magic, Die: 6, TwoHanded: true},
}

// Armour families (reduction dice).
var armorFamilies = []Family{
	{ID: "light", Slot: Armor, Die: 4},
	{ID: "medium", Slot: Armor, Die: 6},
	{ID: "heavy", Slot: Armor, Die: 8},
}

// FamilyByID returns the family for an identifier and a slot (shields and helmets have none).
func FamilyByID(slot Slot, id string) (Family, bool) {
	var list []Family
	switch slot {
	case Weapon:
		list = weaponFamilies
	case Armor:
		list = armorFamilies
	}
	for _, f := range list {
		if f.ID == id {
			return f, true
		}
	}
	return Family{}, false
}

// Families lists the family identifiers allowed for a slot (for the stock prompt).
func Families(slot Slot) []string {
	var out []string
	switch slot {
	case Weapon:
		for _, f := range weaponFamilies {
			out = append(out, f.ID)
		}
	case Armor:
		for _, f := range armorFamilies {
			out = append(out, f.ID)
		}
	}
	return out
}

// DefaultFamily is the family used when the model gives none (or an unknown one): based on the style
// for a weapon, medium for an armour, none for the rest.
func DefaultFamily(slot Slot, kind hero.Kind) string {
	switch slot {
	case Weapon:
		switch kind {
		case hero.Ranged:
			return "shortbow"
		case hero.Magic:
			return "staff"
		}
		return "shortsword"
	case Armor:
		return "medium"
	}
	return ""
}

// weaponNameFamilies groups the fallback catalogue names (gear.name.weapon.N) by family.
var weaponNameFamilies = []string{
	"shortsword", "longsword", "axe", "mace", "dagger", "spear", // 0-5
	"shortbow", "crossbow", "staff", "wand", // 6-9
	"greatsword", "greataxe", // 10-11
}

// armorNameFamilies groups armour names (gear.name.armor.N) by family.
var armorNameFamilies = []string{"medium", "heavy", "light", "heavy", "medium"}

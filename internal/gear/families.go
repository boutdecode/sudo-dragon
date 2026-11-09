package gear

import (
	"github.com/kevin/ascii-adventure/internal/hero"
)

// Famille d'arme ou d'armure : elle fixe le type de dégâts (le dé d'une arme : coupant 4, tranchant 6,
// contondant 8, deux mains 12 ; le dé de réduction d'une armure : légère 4, intermédiaire 6, lourde 8).
// Le modèle de langage peut CHOISIR une famille mais jamais les dés eux-mêmes.
type Family struct {
	ID        string
	Slot      Slot
	Kind      hero.Kind // style d'une arme
	Die       int
	TwoHanded bool
}

// Familles d'armes.
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

// Familles d'armures (dés de réduction).
var armorFamilies = []Family{
	{ID: "light", Slot: Armor, Die: 4},
	{ID: "medium", Slot: Armor, Die: 6},
	{ID: "heavy", Slot: Armor, Die: 8},
}

// FamilyByID renvoie la famille d'un identifiant pour un emplacement (les boucliers et casques n'en ont pas).
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

// Families liste les identifiants de famille permis pour un emplacement (pour le prompt d'étalage).
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

// DefaultFamily est la famille retenue quand le modèle n'en donne pas (ou une inconnue) : d'après le style
// pour une arme, intermédiaire pour une armure, aucune pour le reste.
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

// weaponNameFamilies range les noms du catalogue de repli (gear.name.weapon.N) par famille.
var weaponNameFamilies = []string{
	"shortsword", "longsword", "axe", "mace", "dagger", "spear", // 0-5
	"shortbow", "crossbow", "staff", "wand", // 6-9
	"greatsword", "greataxe", // 10-11
}

// armorNameFamilies range les noms d'armures (gear.name.armor.N) par famille.
var armorNameFamilies = []string{"medium", "heavy", "light", "heavy", "medium"}

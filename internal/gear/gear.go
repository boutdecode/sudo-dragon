// Package gear defines equipment: weapon, armour, shield and helmet.
//
// A language model may PROPOSE items (name, description, tier, bonus, price) for the stock
// of a blacksmith or an armourer, but Validate is the only entry point: it clamps each value
// into bounds computed from the slot and the tier, and keeps only text from the model.
package gear

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Slot is an equipment slot.
type Slot string

const (
	Weapon Slot = "weapon"
	Armor  Slot = "armor"
	Shield Slot = "shield"
	Helm   Slot = "helm"
)

// Slots lists the slots, in display order.
var Slots = []Slot{Weapon, Armor, Shield, Helm}

// Valid reports whether s is a known slot.
func (s Slot) Valid() bool {
	switch s {
	case Weapon, Armor, Shield, Helm:
		return true
	}
	return false
}

// Limits.
const (
	MaxName     = 40
	MaxDesc     = 120
	MaxTier     = 5
	BagCapacity = 24
	SellPercent = 40 // resale price, as a percentage of the price
)

// MinLevelOf is the minimum level to equip an item of a tier (the tier is also the rank:
// number of weapon dice or armour reduction dice).
var MinLevelOf = [MaxTier + 1]int{0, 1, 5, 10, 15, 20}

// Caps on the DEF bonus per slot and per tier (index 1 to 5). One DEF point is worth 5 points of chance to
// hit out of 20: they are tuned by TestBalance.
var defCap = map[Slot][MaxTier + 1]int{
	Armor:  {0, 1, 1, 2, 2, 3},
	Shield: {0, 1, 2, 2, 3, 4},
	Helm:   {0, 1, 1, 1, 2, 2},
}

// Item is an equipment item.
//
// Weapon: damage "Tier d(family Die) + Quality" (Damage), style (Kind) of the family.
// Armour: DEF bonus (Def) and reduction dice "Tier d(family Die)" (Reduction).
// Shield and helmet: DEF bonus only.
type Item struct {
	ID      int64
	Name    string
	Desc    string
	Slot    Slot
	Tier    int
	Family  string // weapon or armour family; empty for a shield or a helmet
	Quality int    // fixed damage bonus of a weapon
	Def     int    // DEF bonus
	Price   int
	Kind    hero.Kind    // style of a weapon (melee, ranged, magic); empty for the rest
	Origin  world.NPCKey // origin vendor
}

// MinLevel is the level required to equip the item.
func (it Item) MinLevel() int { return MinLevelOf[max(1, min(it.Tier, MaxTier))] }

// SellPrice is what reselling brings in.
func (it Item) SellPrice() int { return max(1, it.Price*SellPercent/100) }

// Rank is the rank of the item (its tier, 1 to 5).
func (it Item) Rank() int { return max(1, min(it.Tier, MaxTier)) }

// FamilyInfo returns the family of the item, if it exists.
func (it Item) FamilyInfo() (Family, bool) { return FamilyByID(it.Slot, it.Family) }

// TwoHanded: the item occupies both hands (weapon).
func (it Item) TwoHanded() bool {
	f, ok := it.FamilyInfo()
	return ok && f.TwoHanded
}

// Damage returns the damage dice of a weapon (zero for the rest).
func (it Item) Damage() dice.Dice {
	if f, ok := it.FamilyInfo(); ok && it.Slot == Weapon {
		return dice.D(it.Rank(), f.Die, it.Quality)
	}
	return dice.Dice{}
}

// Reduction returns the damage reduction dice of an armour (zero for the rest).
func (it Item) Reduction() dice.Dice {
	if f, ok := it.FamilyInfo(); ok && it.Slot == Armor {
		return dice.D(it.Rank(), f.Die, 0)
	}
	return dice.Dice{}
}

// Power summarises the power of the item to set its price.
func (it Item) Power() int {
	return it.Damage().Avg() + it.Reduction().Avg() + 4*it.Def
}

// Equipped is the worn equipment (one item per slot).
type Equipped struct{ Weapon, Armor, Shield, Helm *Item }

// Get returns the item worn in a slot.
func (e Equipped) Get(s Slot) *Item {
	switch s {
	case Weapon:
		return e.Weapon
	case Armor:
		return e.Armor
	case Shield:
		return e.Shield
	case Helm:
		return e.Helm
	}
	return nil
}

// Set places (or removes, if it is nil) the item of a slot.
func (e *Equipped) Set(s Slot, it *Item) {
	switch s {
	case Weapon:
		e.Weapon = it
	case Armor:
		e.Armor = it
	case Shield:
		e.Shield = it
	case Helm:
		e.Helm = it
	}
}

// Defense returns the total DEF bonus of the worn equipment.
func (e Equipped) Defense() (def int) {
	for _, s := range Slots {
		if it := e.Get(s); it != nil {
			def += it.Def
		}
	}
	return
}

// Reduction returns the reduction dice of the worn armour.
func (e Equipped) Reduction() dice.Dice {
	if e.Armor == nil {
		return dice.Dice{}
	}
	return e.Armor.Reduction()
}

// Bounds returns the [min, max] bounds of the main bonus of a slot and a tier: quality for
// a weapon, DEF for the rest.
func Bounds(s Slot, tier int) (lo, hi int) {
	tier = max(1, min(tier, MaxTier))
	if s == Weapon {
		return 0, tier
	}
	hi = defCap[s][tier]
	return max(1, hi*2/3), hi
}

// PriceBounds returns the price bounds of an item of power power at tier tier.
func PriceBounds(tier, power int) (lo, hi int) {
	ref := 10 * power * (1 + tier)
	return max(5, ref*7/10), max(8, ref*3/2)
}

// Proposal is what the model says for an item, already extracted from the JSON but not yet verified.
type Proposal struct {
	Slot, Name, Desc string
	Tier             int
	Family           string // weapon or armour family (unknown or absent: default family)
	Quality          int    // damage bonus of a weapon
	Def              int    // DEF bonus
	Price            int
	Kind             string // old format: weapon style, used to choose a default family
}

// Context is what the server allows for a vendor's stock.
type Context struct {
	Slots   []Slot // slots this vendor may offer
	MaxTier int    // maximum tier of the place
	MinTier int    // minimum tier of the place (0: no floor)
	Origin  world.NPCKey
}

// ErrInvalid is the error for any refused proposal.
var ErrInvalid = errors.New("invalid item")

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// shape clamps family, style, quality and DEF of an item into the values allowed for its slot and its
// tier. Dice are never stored: they derive from the tier and the family.
func shape(it Item, rawKind hero.Kind) Item {
	it.Tier = max(1, min(it.Tier, MaxTier))
	fam, ok := FamilyByID(it.Slot, strings.ToLower(strings.TrimSpace(it.Family)))
	if !ok {
		if it.Slot == Weapon && !rawKind.Valid() {
			rawKind = hero.Melee
		}
		fam, _ = FamilyByID(it.Slot, DefaultFamily(it.Slot, rawKind))
	}
	it.Family, it.Kind = fam.ID, ""
	lo, hi := Bounds(it.Slot, it.Tier)
	if it.Slot == Weapon {
		it.Kind = fam.Kind
		it.Quality, it.Def = max(lo, min(it.Quality, hi)), 0
	} else {
		it.Quality = 0
		it.Def = max(lo, min(it.Def, hi))
	}
	return it
}

// Validate turns a proposal into an item, or refuses it. The item does not have an identifier yet.
// Text moderation remains to be done.
func Validate(p Proposal, c Context) (Item, error) {
	slot := Slot(strings.ToLower(strings.TrimSpace(p.Slot)))
	if !slot.Valid() {
		return Item{}, reject("unknown slot %q", p.Slot)
	}
	allowed := false
	for _, s := range c.Slots {
		allowed = allowed || s == slot
	}
	if !allowed {
		return Item{}, reject("slot %q not sold here", slot)
	}
	name, desc := textsafe.Clean(p.Name, MaxName), textsafe.Clean(p.Desc, MaxDesc)
	if name == "" {
		return Item{}, reject("empty name")
	}
	if lore.LooksLikeLink(name) || lore.LooksLikeLink(desc) {
		return Item{}, reject("link or address in the text")
	}
	maxTier := max(1, min(c.MaxTier, MaxTier))
	minTier := max(1, min(c.MinTier, maxTier))
	it := shape(Item{
		Name: name, Desc: desc, Slot: slot, Tier: max(minTier, min(p.Tier, maxTier)), Family: p.Family,
		Quality: p.Quality, Def: p.Def, Origin: c.Origin,
	}, hero.Kind(strings.ToLower(strings.TrimSpace(p.Kind))))
	plo, phi := PriceBounds(it.Tier, it.Power())
	it.Price = max(plo, min(p.Price, phi))
	return it, nil
}

// Sanitize revalidates an item read back from a save: same bounds as Validate, without trusting
// the stored values (an item from before families gets the default family of its style).
// It returns false if the item is unusable.
func Sanitize(it Item) (Item, bool) {
	if !it.Slot.Valid() || it.ID <= 0 {
		return Item{}, false
	}
	name, desc := textsafe.Clean(it.Name, MaxName), textsafe.Clean(it.Desc, MaxDesc)
	if name == "" {
		return Item{}, false
	}
	it.Name, it.Desc = name, desc
	it = shape(it, it.Kind)
	_, phi := PriceBounds(it.Tier, it.Power())
	it.Price = max(1, min(it.Price, phi))
	return it, true
}

// SlotsOfSmith and SlotsOfArmorer are the slots sold by each type of vendor.
var (
	SlotsOfSmith   = []Slot{Weapon}
	SlotsOfArmorer = []Slot{Armor, Shield, Helm}
)

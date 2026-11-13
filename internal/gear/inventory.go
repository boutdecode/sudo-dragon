package gear

import (
	"errors"
	"slices"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Wares is a vendor's generated stock: its specialty and its items (identified from 1 to N in the stock).
type Wares struct {
	NPC       world.NPCKey
	Specialty string
	Items     []Item
	Version   int
	CreatedAt time.Time
}

// Bag management errors.
var (
	ErrBagFull     = errors.New("bag full")
	ErrNoSuchItem  = errors.New("unknown item")
	ErrLevelTooLow = errors.New("level too low")
	ErrWorn        = errors.New("item is equipped")
	ErrTwoHanded   = errors.New("two-handed weapon and shield are incompatible")
)

// Inventory is a player's bag and what he carries. Worn maps a slot to the identifier of
// the worn item (the item stays in Items).
type Inventory struct {
	Items []Item
	Worn  map[Slot]int64
}

// Find returns the item with identifier id, or nil.
func (inv *Inventory) Find(id int64) *Item {
	for i := range inv.Items {
		if inv.Items[i].ID == id {
			return &inv.Items[i]
		}
	}
	return nil
}

// NextID returns a free identifier.
func (inv *Inventory) NextID() int64 {
	var m int64
	for _, it := range inv.Items {
		m = max(m, it.ID)
	}
	return m + 1
}

// Add puts an item in the bag, gives it a new identifier and returns it.
func (inv *Inventory) Add(it Item) (Item, error) {
	if len(inv.Items) >= BagCapacity {
		return Item{}, ErrBagFull
	}
	it.ID = inv.NextID()
	inv.Items = append(inv.Items, it)
	return it, nil
}

// IsWorn reports whether the item is equipped.
func (inv *Inventory) IsWorn(id int64) bool {
	for _, w := range inv.Worn {
		if w == id {
			return true
		}
	}
	return false
}

// Equipped returns the worn equipment.
func (inv *Inventory) Equipped() Equipped {
	var e Equipped
	for slot, id := range inv.Worn {
		if it := inv.Find(id); it != nil && it.Slot == slot {
			e.Set(slot, it)
		}
	}
	return e
}

// Equip equips item id (replacing the one in its slot, which goes back to the bag). level is the player's.
func (inv *Inventory) Equip(id int64, level int) error {
	it := inv.Find(id)
	if it == nil {
		return ErrNoSuchItem
	}
	if level < it.MinLevel() {
		return ErrLevelTooLow
	}
	// A two-handed weapon cannot be worn with a shield.
	eq := inv.Equipped()
	if (it.Slot == Shield && eq.Weapon != nil && eq.Weapon.TwoHanded()) || (it.Slot == Weapon && it.TwoHanded() && eq.Shield != nil) {
		return ErrTwoHanded
	}
	if inv.Worn == nil {
		inv.Worn = map[Slot]int64{}
	}
	inv.Worn[it.Slot] = id
	return nil
}

// Unequip removes what is worn in a slot.
func (inv *Inventory) Unequip(slot Slot) { delete(inv.Worn, slot) }

// Remove takes an unequipped item out of the bag and returns it.
func (inv *Inventory) Remove(id int64) (Item, error) {
	if inv.IsWorn(id) {
		return Item{}, ErrWorn
	}
	for i := range inv.Items {
		if inv.Items[i].ID == id {
			it := inv.Items[i]
			inv.Items = slices.Delete(inv.Items, i, i+1)
			return it, nil
		}
	}
	return Item{}, ErrNoSuchItem
}

// Clone returns an independent copy.
func (inv Inventory) Clone() Inventory {
	out := Inventory{Items: slices.Clone(inv.Items)}
	if len(inv.Worn) > 0 {
		out.Worn = make(map[Slot]int64, len(inv.Worn))
		for k, v := range inv.Worn {
			out.Worn[k] = v
		}
	}
	return out
}

// SanitizeInventory revalidates a bag read back from a save: bounded items (Sanitize), unique identifiers,
// at most BagCapacity items, worn items present and in the right slot.
func SanitizeInventory(in Inventory) Inventory {
	var out Inventory
	seen := map[int64]bool{}
	for _, it := range in.Items {
		if len(out.Items) >= BagCapacity || seen[it.ID] {
			continue
		}
		if clean, ok := Sanitize(it); ok {
			seen[clean.ID] = true
			out.Items = append(out.Items, clean)
		}
	}
	for slot, id := range in.Worn {
		if it := out.Find(id); it != nil && it.Slot == slot {
			if out.Worn == nil {
				out.Worn = map[Slot]int64{}
			}
			out.Worn[slot] = id
		}
	}
	return out
}

// SanitizeItems revalidates items read back from a database (saved stock), keeping their identifiers.
func SanitizeItems(in []Item) []Item {
	out := make([]Item, 0, len(in))
	for _, it := range in {
		if clean, ok := Sanitize(it); ok {
			out = append(out, clean)
		}
	}
	return out
}

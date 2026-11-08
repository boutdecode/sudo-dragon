package gear

import (
	"errors"
	"slices"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Wares est l'étalage généré d'un vendeur : sa spécialité et ses objets (identifiés de 1 à N dans l'étalage).
type Wares struct {
	NPC       world.NPCKey
	Specialty string
	Items     []Item
	Version   int
	CreatedAt time.Time
}

// Erreurs de gestion du sac.
var (
	ErrBagFull     = errors.New("sac plein")
	ErrNoSuchItem  = errors.New("objet inconnu")
	ErrLevelTooLow = errors.New("niveau insuffisant")
	ErrWorn        = errors.New("objet équipé")
)

// Inventory est le sac d'un joueur et ce qu'il porte. Worn associe un emplacement à l'identifiant de
// l'objet porté (l'objet reste dans Items).
type Inventory struct {
	Items []Item
	Worn  map[Slot]int64
}

// Find renvoie l'objet d'identifiant id, ou nil.
func (inv *Inventory) Find(id int64) *Item {
	for i := range inv.Items {
		if inv.Items[i].ID == id {
			return &inv.Items[i]
		}
	}
	return nil
}

// NextID renvoie un identifiant libre.
func (inv *Inventory) NextID() int64 {
	var m int64
	for _, it := range inv.Items {
		m = max(m, it.ID)
	}
	return m + 1
}

// Add met un objet dans le sac, lui donne un identifiant neuf et le renvoie.
func (inv *Inventory) Add(it Item) (Item, error) {
	if len(inv.Items) >= BagCapacity {
		return Item{}, ErrBagFull
	}
	it.ID = inv.NextID()
	inv.Items = append(inv.Items, it)
	return it, nil
}

// IsWorn dit si l'objet est équipé.
func (inv *Inventory) IsWorn(id int64) bool {
	for _, w := range inv.Worn {
		if w == id {
			return true
		}
	}
	return false
}

// Equipped renvoie l'équipement porté.
func (inv *Inventory) Equipped() Equipped {
	var e Equipped
	for slot, id := range inv.Worn {
		if it := inv.Find(id); it != nil && it.Slot == slot {
			e.Set(slot, it)
		}
	}
	return e
}

// Equip équipe l'objet id (en remplaçant celui de son emplacement, qui retourne au sac). level est celui du joueur.
func (inv *Inventory) Equip(id int64, level int) error {
	it := inv.Find(id)
	if it == nil {
		return ErrNoSuchItem
	}
	if level < it.MinLevel() {
		return ErrLevelTooLow
	}
	if inv.Worn == nil {
		inv.Worn = map[Slot]int64{}
	}
	inv.Worn[it.Slot] = id
	return nil
}

// Unequip retire ce qui est porté dans un emplacement.
func (inv *Inventory) Unequip(slot Slot) { delete(inv.Worn, slot) }

// Remove retire un objet non équipé du sac et le renvoie.
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

// Clone renvoie une copie indépendante.
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

// SanitizeInventory revalide un sac relu d'une sauvegarde : objets bornés (Sanitize), identifiants uniques,
// au plus BagCapacity objets, objets portés présents et dans le bon emplacement.
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

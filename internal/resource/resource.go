// Package resource describes the gatherable raw materials (wood, ore, herbs) used by the
// future crafting. The catalogue is static: only identifiers travel between server, client and database.
package resource

// Category is the family of a resource.
type Category uint8

const (
	Wood Category = iota
	Ore
	Herb
	Fish
	NumCategories
)

// Key is the identifier of the category (translation key "resource.cat.<key>").
func (c Category) Key() string {
	switch c {
	case Ore:
		return "ore"
	case Herb:
		return "herb"
	case Fish:
		return "fish"
	}
	return "wood"
}

// Kind is the stable identifier of a resource (stored in the database): never rename one.
type Kind string

// Info describes a resource.
type Info struct {
	Kind Kind
	Cat  Category
	Tier int // 1 (common) to 4 (rare): rarity, which rises with the region level
	// Climates restricts spawning to certain climates (world.Climate indices); empty = everywhere.
	Climates []int
}

// Climate indices (mirror of world.Climate, without the dependency).
const (
	Temperate = iota
	Tropical
	Arid
	Boreal
)

// all is the catalogue, in a stable order.
var all = []Info{
	{"oak", Wood, 1, []int{Temperate, Tropical}},
	{"pine", Wood, 1, []int{Boreal, Arid}},
	{"birch", Wood, 2, []int{Temperate, Boreal}},
	{"ebony", Wood, 4, []int{Tropical}},
	{"yew", Wood, 3, nil},

	{"stone", Ore, 1, nil},
	{"coal", Ore, 1, nil},
	{"copper", Ore, 2, nil},
	{"tin", Ore, 2, nil},
	{"iron", Ore, 3, nil},
	{"silver", Ore, 3, nil},
	{"gold", Ore, 4, nil},

	{"gudgeon", Fish, 1, nil},
	{"trout", Fish, 1, []int{Temperate, Boreal}},
	{"carp", Fish, 2, []int{Temperate, Tropical, Arid}},
	{"pike", Fish, 2, nil},
	{"salmon", Fish, 3, []int{Temperate, Boreal}},
	{"eel", Fish, 3, nil},
	{"sturgeon", Fish, 4, nil},

	{"sage", Herb, 1, []int{Temperate, Arid}},
	{"nettle", Herb, 1, nil},
	{"mint", Herb, 2, []int{Temperate, Boreal}},
	{"lavender", Herb, 2, []int{Temperate, Arid}},
	{"mushroom", Herb, 3, nil},
	{"mandrake", Herb, 4, nil},
}

var byKind = func() map[Kind]*Info {
	m := make(map[Kind]*Info, len(all))
	for i := range all {
		m[all[i].Kind] = &all[i]
	}
	return m
}()

// All returns the catalogue (copy).
func All() []Info { return append([]Info(nil), all...) }

// Lookup returns the entry of a known resource.
func Lookup(k Kind) (Info, bool) {
	i, ok := byKind[k]
	if !ok {
		return Info{}, false
	}
	return *i, true
}

// MaxTier is the rarest tier.
const MaxTier = 4

// Pick deterministically chooses the resource of category cat for a node: h is a hash of the
// position, climate an index (see Temperate...), level the region level (1 to 20). The higher the level,
// the more frequent the rare tiers are.
func Pick(cat Category, climate, level int, h uint64) Kind {
	// Highest accessible tier: 1 at level 1-5, 4 at level 16-20.
	maxTier := min(1+(max(level, 1)-1)/5, MaxTier)
	var cands []Info
	total := 0
	for _, r := range all {
		if r.Cat != cat || r.Tier > maxTier || !climateOK(r, climate) {
			continue
		}
		cands = append(cands, r)
		total += weight(r.Tier, maxTier)
	}
	if len(cands) == 0 { // should not happen: fall back to the first common one of the category
		for _, r := range all {
			if r.Cat == cat && r.Tier == 1 {
				return r.Kind
			}
		}
	}
	n := int(h % uint64(total))
	for _, r := range cands {
		w := weight(r.Tier, maxTier)
		if n < w {
			return r.Kind
		}
		n -= w
	}
	return cands[0].Kind
}

// weight: common tiers dominate, rare ones stay rare even at high level.
func weight(tier, maxTier int) int {
	return 1 << uint(max(maxTier-tier, 0)) // 1, 2, 4, 8 from the highest tier to the lowest
}

func climateOK(r Info, c int) bool {
	if len(r.Climates) == 0 {
		return true
	}
	for _, x := range r.Climates {
		if x == c {
			return true
		}
	}
	return false
}

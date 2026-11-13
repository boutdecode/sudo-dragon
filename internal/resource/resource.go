// Package resource décrit les matières premières récoltables (bois, minerais, plantes) qui servent au
// futur artisanat. Le catalogue est statique: seuls les identifiants circulent entre serveur, client et base.
package resource

// Category est la famille d'une ressource.
type Category uint8

const (
	Wood Category = iota
	Ore
	Herb
	NumCategories
)

// Key est l'identifiant de la catégorie (clé de traduction "resource.cat.<key>").
func (c Category) Key() string {
	switch c {
	case Ore:
		return "ore"
	case Herb:
		return "herb"
	}
	return "wood"
}

// Kind est l'identifiant stable d'une ressource (stocké en base): n'en renommez jamais un.
type Kind string

// Info décrit une ressource.
type Info struct {
	Kind Kind
	Cat  Category
	Tier int // 1 (commun) à 4 (rare): la rareté, qui monte avec le niveau de la région
	// Climates restreint l'apparition à certains climats (indices de world.Climate); vide = partout.
	Climates []int
}

// Indices de climat (miroir de world.Climate, sans dépendance).
const (
	Temperate = iota
	Tropical
	Arid
	Boreal
)

// all est le catalogue, dans un ordre stable.
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

// All renvoie le catalogue (copie).
func All() []Info { return append([]Info(nil), all...) }

// Lookup renvoie la fiche d'une ressource connue.
func Lookup(k Kind) (Info, bool) {
	i, ok := byKind[k]
	if !ok {
		return Info{}, false
	}
	return *i, true
}

// MaxTier est le palier le plus rare.
const MaxTier = 4

// Pick choisit, de façon déterministe, la ressource de la catégorie cat pour un nœud: h est un hachage de la
// position, climate un indice (voir Temperate…), level le niveau de région (1 à 20). Plus le niveau est
// élevé, plus les paliers rares sont fréquents.
func Pick(cat Category, climate, level int, h uint64) Kind {
	// Palier maximal accessible: 1 au niveau 1-5, 4 au niveau 16-20.
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
	if len(cands) == 0 { // ne devrait pas arriver: repli sur le premier commun de la catégorie
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

// weight: les paliers communs dominent, les rares restent rares même en haut niveau.
func weight(tier, maxTier int) int {
	return 1 << uint(max(maxTier-tier, 0)) // 1, 2, 4, 8 du palier le plus haut au plus bas
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

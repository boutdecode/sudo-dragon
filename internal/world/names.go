package world

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Identities of places and NPCs.
//
// They are deterministic (seed + region + NPC rank) so that "the same"
// healer or "the same" village is identical for all players and after
// a restart, without storing anything: this is what gives the dialogues their
// consistency. They are drawn from a random stream SEPARATE from the one used to
// generate the plans: drawing from it would shift all the following values and
// alter the worlds already played (see TestExistingLayoutsAreFrozen).

// Sizes of the catalogue lists (keys "persona.temperament.N"...). They serve as
// bounds for the draws: changing them would shift all the identities already played.
const (
	numTemperaments = 10
	numQuirks       = 10
	numSpeeches     = 6
	numEpithets     = 6
	numFortPrefixes = 4
)

// Persona is the personality of an NPC, injected into the prompt of its dialogues.
type Persona struct {
	Temperament string // general mood
	Quirk       string // habit or distinctive trait
	Speech      string // way of speaking
}

var (
	placeStart = []string{"Val", "Ker", "Pont", "Mont", "Fer", "Roc", "Bel", "Aube", "Brume", "Cor", "Sol", "Tour",
		"Clair", "Gué", "Ombre", "Pierre", "Lande", "Brande", "Salin", "Vent"}
	placeEnd = []string{"mor", "dune", "ford", "haven", "bourg", "mont", "rive", "lande", "marais", "val", "rec",
		"naud", "vent", "ois", "ac", "elle", "urne", "brune"}
	townPrefix = []string{"Port-", "Haute-", "Grand-", "Belle-", "Pont-", "Saint-"}

	firstNames = []struct {
		name   string
		female bool
	}{
		{"Hubert", false}, {"Aélis", true}, {"Gaspard", false}, {"Mirelle", true}, {"Thibault", false}, {"Ysolde", true},
		{"Bastien", false}, {"Odile", true}, {"Garin", false}, {"Perrine", true}, {"Arnaud", false}, {"Bérénice", true},
		{"Doran", false}, {"Clotilde", true}, {"Eudes", false}, {"Fanchon", true}, {"Josse", false}, {"Gwenn", true},
		{"Léandre", false}, {"Katell", true}, {"Osric", false}, {"Maëlle", true}, {"Raoul", false}, {"Nolwenn", true},
	}
)

// mix64 is the splitmix64 bit mixer: each input bit influences
// all those of the output. Combining coordinates with multiplications and
// an XOR is not enough: regions (x, y) and (-x, -y) with odd coordinates
// gave exactly the same value, hence twin places.
func mix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// identityRNG returns a random stream specific to (seed, region, salt), independent
// of the one used to generate the plans.
func identityRNG(seed int64, rp RegionPos, salt uint64) *rand.Rand {
	h := mix64(uint64(seed) ^ 0x1D3A71FE)
	h = mix64(h + uint64(int64(rp.X)))
	h = mix64(h + uint64(int64(rp.Y)))
	h = mix64(h + salt)
	return rand.New(rand.NewPCG(h, mix64(h^0x6A09E667F3BCC908)))
}

func pick[T any](rng *rand.Rand, list []T) T { return list[rng.IntN(len(list))] }

// placeName invents the name of a place. For a fortress, fort is the index of
// its prefix ("Fort", "Citadelle"...), to be translated with fortName.
//
// override, if not empty, replaces the drawn stem (see baseFor); the draw still takes place,
// so that the following choices (prefix) do not change.
func placeName(kind Kind, rng *rand.Rand, override string) (name string, fort int, base string) {
	base = pick(rng, placeStart) + pick(rng, placeEnd)
	if override != "" {
		base = override
	}
	switch kind {
	case KindTown:
		// "Port-Valdurne": the part after the prefix keeps its capital letter.
		return pick(rng, townPrefix) + base, 0, base
	case KindFortress:
		fort = rng.IntN(numFortPrefixes)
		return fortName(i18n.FR, fort, base), fort, base
	}
	return base, 0, base
}

// fortName assembles the name of a fortress in language l: "Fort valdurne".
func fortName(l i18n.Lang, prefix int, base string) string {
	return i18n.T(l, fmt.Sprintf("place.fort.%d", prefix)) + strings.ToLower(base[:1]) + base[1:]
}

// ident keeps the choices of an NPC's identity draw, as indices into
// the catalogue lists, so that it can be named and described in all languages.
type ident struct {
	ok        bool // set by npcIdentity; false for an NPC built by hand (tests)
	first     int  // first name (firstNames; common to all languages)
	epithet   int  // trait of a villager, -1 otherwise
	female    bool // agreement of titles and epithets
	temper    int
	quirk     int
	speech    int
	fortIndex int // fortress prefix of the place (unused for an NPC)
}

// npcIdentity invents the identity of an NPC from its trade.
func npcIdentity(kind NPCKind, rng *rand.Rand) ident {
	fi := rng.IntN(len(firstNames))
	id := ident{ok: true, first: fi, epithet: -1, female: firstNames[fi].female}
	if kind == NPCVillager {
		// No trade or title: a first name and a physical or mood trait.
		id.epithet = rng.IntN(numEpithets)
	}
	id.temper = rng.IntN(numTemperaments)
	id.quirk = rng.IntN(numQuirks)
	id.speech = rng.IntN(numSpeeches)
	return id
}

func (id ident) name(l i18n.Lang, kind NPCKind) string {
	fn := firstNames[id.first].name
	g := "m"
	if id.female {
		g = "f"
	}
	switch kind {
	case NPCVillager:
		return fn + " " + i18n.T(l, fmt.Sprintf("name.epithet.%s.%d", g, id.epithet))
	case NPCHealer:
		return i18n.T(l, "name.title.healer."+g) + " " + fn
	case NPCChief:
		return i18n.T(l, "name.title.chief."+g) + " " + fn
	case NPCMayor:
		return i18n.T(l, "name.title.mayor."+g) + " " + fn
	case NPCSmith:
		return i18n.T(l, "name.title.smith."+g) + " " + fn
	case NPCArmorer:
		return i18n.T(l, "name.title.armorer."+g) + " " + fn
	case NPCGuard:
		return i18n.T(l, "name.title.guard."+g) + " " + fn
	}
	return i18n.T(l, "name.title.merchant."+g) + " " + fn
}

func (id ident) persona(l i18n.Lang) Persona {
	return Persona{
		Temperament: i18n.T(l, fmt.Sprintf("persona.temperament.%d", id.temper)),
		Quirk:       i18n.T(l, fmt.Sprintf("persona.quirk.%d", id.quirk)),
		Speech:      i18n.T(l, fmt.Sprintf("persona.speech.%d", id.speech)),
	}
}

// NameIn returns the name of the NPC in language l (first names do not change,
// titles and epithets do). Name is the French version.
func (n NPC) NameIn(l i18n.Lang) string {
	if !n.id.ok {
		return n.Name
	}
	return n.id.name(l, n.Kind)
}

// PersonaIn returns the personality of the NPC in language l.
func (n NPC) PersonaIn(l i18n.Lang) Persona {
	if !n.id.ok {
		return n.Persona
	}
	return n.id.persona(l)
}

// NPCKey identifies an NPC in a stable way: its region and its rank in the place.
type NPCKey struct {
	Region RegionPos
	Index  int
}

// Key returns the stable key of the NPC.
func (n NPC) Key() NPCKey { return NPCKey{n.Region, n.Index} }

// nameClashRadius: two places at this distance (in regions, Chebyshev) or less never bear the
// same name stem. This is the radius of rumours and quests: beyond it, a homonym remains possible
// (the name table is finite and the world is not), but no longer gets in the way.
const nameClashRadius = 3

type stem struct{ a, b int }

func (s stem) String() string { return placeStart[s.a] + placeEnd[s.b] }

// rawStem is the stem drawn for a region, independently of its neighbours: the first two
// draws of identityRNG(seed, rp, 0), exactly as before baseFor was introduced.
//
// The stem comes from the family of the district at the centre of the region: the places of the same
// district have a family resemblance.
func rawStem(seed int64, rp RegionPos) stem {
	starts, ends := familyOf(seed, territoryIDAt(seed, rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2))
	rng := identityRNG(seed, rp, 0)
	return stem{starts[rng.IntN(familySize)], ends[rng.IntN(familySize)]}
}

// baseFor returns the stem of the name of the place of region rp. A place keeps its drawn stem, unless an earlier
// neighbouring region (radius nameClashRadius) in the order (Y then X) has the same one: it then draws another, never
// equal to that of a neighbour. The test only looks at raw draws, not at structures: no plan generation,
// and the result does not depend on what the neighbours contain. When the four candidates of the band are all taken
// by neighbours, the whole family is searched instead of falling back on an already used stem.
func baseFor(seed int64, rp RegionPos) string {
	raw := rawStem(seed, rp)
	starts, ends := familyOf(seed, territoryIDAt(seed, rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2))
	taken := make(map[stem]bool, 48)
	displaced := false
	for dy := -nameClashRadius; dy <= nameClashRadius; dy++ {
		for dx := -nameClashRadius; dx <= nameClashRadius; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			q := rawStem(seed, RegionPos{rp.X + dx, rp.Y + dy})
			taken[q] = true
			if q == raw && (dy < 0 || (dy == 0 && dx < 0)) {
				displaced = true
			}
		}
	}
	if !displaced {
		return raw.String()
	}
	// Replacement: the family has 64 stems, split into 16 bands of 4 according to the region's class
	// (x mod 4, y mod 4). Two neighbouring regions (radius 3) never have the same class: two neighbouring
	// replacements can therefore never be equal. In the band we take the first stem that is
	// that of no neighbour (at worst the first one: very rare case).
	class := ((rp.X%4+4)%4)*4 + (rp.Y%4+4)%4
	var slice [familySize * familySize / 16]stem
	for i := range slice {
		k := class + 16*i
		slice[i] = stem{starts[k/familySize], ends[k%familySize]}
	}
	rot := identityRNG(seed, rp, 9000).IntN(len(slice))
	cand := slice[rot]
	free := false
	for i := range slice {
		if c := slice[(rot+i)%len(slice)]; !taken[c] {
			cand, free = c, true
			break
		}
	}
	if !free {
		n := familySize * familySize
		for i := range n {
			k := (class + 16*rot + i) % n
			if c := (stem{starts[k/familySize], ends[k%familySize]}); !taken[c] {
				cand = c
				break
			}
		}
	}
	return cand.String()
}

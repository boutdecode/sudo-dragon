package world

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// Identités des lieux et des PNJ.
//
// Elles sont déterministes (seed + région + rang du PNJ) pour que « le même »
// soigneur ou « le même » village soit identique pour tous les joueurs et après
// un redémarrage, sans rien stocker: c'est ce qui donne aux dialogues leur
// consistance. Elles sont tirées d'un flux aléatoire SÉPARÉ de celui de la
// génération des plans: y puiser décalerait toutes les valeurs suivantes et
// modifierait les mondes déjà joués (voir TestExistingLayoutsAreFrozen).

// Tailles des listes du catalogue (clés "persona.temperament.N"…). Elles servent de
// bornes aux tirages: les changer décalerait toutes les identités déjà jouées.
const (
	numTemperaments = 10
	numQuirks       = 10
	numSpeeches     = 6
	numEpithets     = 6
	numFortPrefixes = 4
)

// Persona est la personnalité d'un PNJ, injectée dans le prompt de ses dialogues.
type Persona struct {
	Temperament string // humeur générale
	Quirk       string // manie ou trait distinctif
	Speech      string // façon de parler
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

// mix64 est le mélangeur de bits splitmix64: chaque bit de l'entrée influence
// tous ceux de la sortie. Combiner des coordonnées avec des multiplications et
// un XOR ne suffit pas: les régions (x, y) et (-x, -y) à coordonnées impaires
// donnaient exactement la même valeur, donc des lieux jumeaux.
func mix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// identityRNG renvoie un flux aléatoire propre à (seed, région, sel), indépendant
// de celui de la génération des plans.
func identityRNG(seed int64, rp RegionPos, salt uint64) *rand.Rand {
	h := mix64(uint64(seed) ^ 0x1D3A71FE)
	h = mix64(h + uint64(int64(rp.X)))
	h = mix64(h + uint64(int64(rp.Y)))
	h = mix64(h + salt)
	return rand.New(rand.NewPCG(h, mix64(h^0x6A09E667F3BCC908)))
}

func pick[T any](rng *rand.Rand, list []T) T { return list[rng.IntN(len(list))] }

// placeName invente le nom d'un lieu. Pour une forteresse, fort est l'indice de
// son préfixe (« Fort », « Citadelle »…), à traduire avec fortName.
//
// override, s'il n'est pas vide, remplace le radical tiré (voir baseFor) ; le tirage a lieu
// quand même, pour que les choix suivants (préfixe) ne changent pas.
func placeName(kind Kind, rng *rand.Rand, override string) (name string, fort int, base string) {
	base = pick(rng, placeStart) + pick(rng, placeEnd)
	if override != "" {
		base = override
	}
	switch kind {
	case KindTown:
		// « Port-Valdurne »: la partie après le préfixe garde sa majuscule.
		return pick(rng, townPrefix) + base, 0, base
	case KindFortress:
		fort = rng.IntN(numFortPrefixes)
		return fortName(i18n.FR, fort, base), fort, base
	}
	return base, 0, base
}

// fortName assemble le nom d'une forteresse dans la langue l: « Fort valdurne ».
func fortName(l i18n.Lang, prefix int, base string) string {
	return i18n.T(l, fmt.Sprintf("place.fort.%d", prefix)) + strings.ToLower(base[:1]) + base[1:]
}

// ident retient les choix du tirage d'identité d'un PNJ, sous forme d'indices dans
// les listes du catalogue, pour pouvoir le nommer et le décrire dans toutes les langues.
type ident struct {
	ok        bool // renseignée par npcIdentity; faux pour un PNJ construit à la main (tests)
	first     int  // prénom (firstNames; propre à toutes les langues)
	epithet   int  // trait d'un villageois, -1 sinon
	female    bool // accord des titres et des épithètes
	temper    int
	quirk     int
	speech    int
	fortIndex int // préfixe de forteresse du lieu (non utilisé pour un PNJ)
}

// npcIdentity invente l'identité d'un PNJ d'après son métier.
func npcIdentity(kind NPCKind, rng *rand.Rand) ident {
	fi := rng.IntN(len(firstNames))
	id := ident{ok: true, first: fi, epithet: -1, female: firstNames[fi].female}
	if kind == NPCVillager {
		// Pas de métier ni de titre : un prénom et un trait physique ou d'humeur.
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

// NameIn renvoie le nom du PNJ dans la langue l (les prénoms ne changent pas, les
// titres et les épithètes oui). Name est la version française.
func (n NPC) NameIn(l i18n.Lang) string {
	if !n.id.ok {
		return n.Name
	}
	return n.id.name(l, n.Kind)
}

// PersonaIn renvoie la personnalité du PNJ dans la langue l.
func (n NPC) PersonaIn(l i18n.Lang) Persona {
	if !n.id.ok {
		return n.Persona
	}
	return n.id.persona(l)
}

// NPCKey identifie un PNJ de façon stable: sa région et son rang dans le lieu.
type NPCKey struct {
	Region RegionPos
	Index  int
}

// Key renvoie la clé stable du PNJ.
func (n NPC) Key() NPCKey { return NPCKey{n.Region, n.Index} }

// nameClashRadius : deux lieux à cette distance (en régions, Chebyshev) ou moins ne portent jamais le
// même radical de nom. C'est le rayon des rumeurs et des quêtes : au-delà, une homonymie reste possible
// (la table de noms est finie et le monde ne l'est pas), mais ne gêne plus.
const nameClashRadius = 3

type stem struct{ a, b int }

func (s stem) String() string { return placeStart[s.a] + placeEnd[s.b] }

// rawStem est le radical tiré pour une région, indépendamment de ses voisines : les deux premiers
// tirages de identityRNG(seed, rp, 0), exactement comme avant l'introduction de baseFor.
//
// Le radical vient de la famille de la contrée du centre de la région : les lieux d'une même
// contrée ont un air de parenté.
func rawStem(seed int64, rp RegionPos) stem {
	starts, ends := familyOf(seed, territoryIDAt(seed, rp.X*RegionSize+RegionSize/2, rp.Y*RegionSize+RegionSize/2))
	rng := identityRNG(seed, rp, 0)
	return stem{starts[rng.IntN(familySize)], ends[rng.IntN(familySize)]}
}

// baseFor renvoie le radical du nom du lieu de la région rp. Un lieu garde son radical tiré, sauf si une région
// voisine (rayon nameClashRadius) antérieure dans l'ordre (Y puis X) a le même : il en tire alors un autre, jamais
// égal à celui d'une voisine. Le test ne regarde que les tirages bruts, pas les structures : pas de génération
// de plan, et le résultat ne dépend pas de ce que contiennent les voisines.
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
	// Remplaçant : la famille compte 64 radicaux, répartis en 16 tranches de 4 selon la classe de la région
	// (x mod 4, y mod 4). Deux régions voisines (rayon 3) n'ont jamais la même classe : deux remplaçants
	// voisins ne peuvent donc jamais être égaux. On prend, dans la tranche, le premier radical qui n'est
	// celui d'aucune voisine (au pire le premier : cas très rare).
	class := ((rp.X%4+4)%4)*4 + (rp.Y%4+4)%4
	var slice [familySize * familySize / 16]stem
	for i := range slice {
		k := class + 16*i
		slice[i] = stem{starts[k/familySize], ends[k%familySize]}
	}
	rot := identityRNG(seed, rp, 9000).IntN(len(slice))
	cand := slice[rot]
	for i := range slice {
		if c := slice[(rot+i)%len(slice)]; !taken[c] {
			cand = c
			break
		}
	}
	return cand.String()
}

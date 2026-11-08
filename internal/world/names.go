package world

import (
	"math/rand/v2"
	"strings"
)

// Identités des lieux et des PNJ.
//
// Elles sont déterministes (seed + région + rang du PNJ) pour que « le même »
// soigneur ou « le même » village soit identique pour tous les joueurs et après
// un redémarrage, sans rien stocker: c'est ce qui donne aux dialogues leur
// consistance. Elles sont tirées d'un flux aléatoire SÉPARÉ de celui de la
// génération des plans: y puiser décalerait toutes les valeurs suivantes et
// modifierait les mondes déjà joués (voir TestExistingLayoutsAreFrozen).

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
	fortPrefix = []string{"Fort ", "Citadelle ", "Donjon ", "Bastion "}

	temperaments = []string{
		"bavard et chaleureux", "méfiant et laconique", "pompeux et sûr de lui", "rêveur, un peu distrait",
		"bougon mais au grand cœur", "curieux de tout", "nerveux et pressé", "grave et solennel",
		"malicieux et taquin", "fatigué de la route et des histoires",
	}
	quirks = []string{
		"collectionne les cailloux étranges", "compte tout à voix basse", "prétend avoir croisé un dragon",
		"parle à son chat comme à un conseiller", "ne se sépare jamais d'un vieux chapeau", "redoute les corbeaux",
		"tient un carnet de rumeurs", "chantonne en travaillant", "est persuadé que le temps va tourner",
		"donne un nom à chacun de ses outils",
	}
	speeches = []string{
		"phrases courtes et directes", "tournures un peu vieillottes", "ponctue de proverbes inventés",
		"répond souvent par une question", "parle à mi-voix, comme en confidence", "emploie des images de la vie quotidienne",
	}

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

// placeName invente le nom d'un lieu.
func placeName(kind Kind, rng *rand.Rand) string {
	base := pick(rng, placeStart) + pick(rng, placeEnd)
	switch kind {
	case KindTown:
		// « Port-Valdurne »: la partie après le préfixe garde sa majuscule.
		return pick(rng, townPrefix) + base
	case KindFortress:
		return pick(rng, fortPrefix) + strings.ToLower(base[:1]) + base[1:]
	}
	return base
}

// npcIdentity invente le nom et la personnalité d'un PNJ d'après son métier.
func npcIdentity(kind NPCKind, rng *rand.Rand) (string, Persona) {
	fn := pick(rng, firstNames)
	var title string
	switch kind {
	case NPCHealer:
		title = "Frère"
		if fn.female {
			title = "Sœur"
		}
	default: // marchand
		title = "Maître"
		if fn.female {
			title = "Dame"
		}
	}
	return title + " " + fn.name, Persona{
		Temperament: pick(rng, temperaments),
		Quirk:       pick(rng, quirks),
		Speech:      pick(rng, speeches),
	}
}

// NPCKey identifie un PNJ de façon stable: sa région et son rang dans le lieu.
type NPCKey struct {
	Region RegionPos
	Index  int
}

// Key renvoie la clé stable du PNJ.
func (n NPC) Key() NPCKey { return NPCKey{n.Region, n.Index} }

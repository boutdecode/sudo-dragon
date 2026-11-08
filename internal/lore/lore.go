// Package lore est la mémoire collective des PNJ : les présentations que les
// joueurs font d'eux-mêmes, la façon dont elles se répandent de lieu en lieu
// sous forme de rumeurs, et les souvenirs privés de chaque PNJ sur chaque joueur.
//
// Les règles (visibilité, fidélité, validation) sont des fonctions pures avec
// une horloge injectable : elles se testent sans base ni réseau.
package lore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Réglages de la mémoire. Ce sont des constantes de jeu, faciles à ajuster.
const (
	// RumorRadius est la distance maximale (en régions, Chebyshev) à laquelle une
	// présentation se répand. Une région fait 256 cases: 3 régions ≈ 770 cases.
	RumorRadius = 3
	// HopDelay est le temps que met une rumeur pour franchir une région.
	HopDelay = 5 * time.Minute
	// Forgetting est la durée après laquelle une présentation est oubliée si le
	// joueur ne la renouvelle pas.
	Forgetting = 14 * 24 * time.Hour
	// MaxHeard borne le nombre de rumeurs données à un PNJ pour une conversation.
	MaxHeard = 6
	// MaxCardsPerAccount borne les lieux où un même joueur peut avoir une fiche.
	MaxCardsPerAccount = 8

	// Tailles maximales des champs (en caractères).
	MaxAlias   = 24
	MaxField   = 80
	MaxSummary = 300
)

// Intro est ce qu'un joueur dit de lui-même (non vérifié).
type Intro struct {
	Alias  string // nom ou surnom qu'il se donne
	Role   string // métier, rang
	Origin string // d'où il vient
	Goal   string // ce qu'il cherche
	Trait  string // un trait ou une anecdote
}

// Facts sont des faits établis par le serveur, que le joueur ne peut pas falsifier.
type Facts struct {
	Level     int
	BossKills int
	Deaths    int
}

// Card est la fiche de présentation d'un joueur, faite dans un lieu donné.
type Card struct {
	Account string          // pseudo du compte (vérité serveur)
	Place   world.RegionPos // région du lieu où la présentation a été faite
	Intro
	Facts
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Memory est le souvenir privé d'un PNJ sur un joueur. Il n'est jamais montré à un autre joueur.
type Memory struct {
	NPC       world.NPCKey
	Account   string
	Summary   string
	UpdatedAt time.Time
}

// Heard est une fiche telle que la connaît un PNJ donné : complète si elle est
// née ici, de plus en plus lacunaire à mesure qu'elle vient de loin.
type Heard struct {
	Account  string
	Distance int // en régions, 0 = fait dans ce lieu
	Self     bool
	Intro    Intro // champs masqués vidés selon la distance
	Facts    Facts // idem
	Place    world.RegionPos
	Age      time.Duration // depuis la dernière mise à jour
}

// Local indique si la présentation a été faite dans le lieu même du PNJ.
func (h Heard) Local() bool { return h.Distance == 0 }

// Distance renvoie la distance de Chebyshev, en régions, entre deux lieux.
func Distance(a, b world.RegionPos) int {
	return max(abs(a.X-b.X), abs(a.Y-b.Y))
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// Reaches indique si une fiche, née en c.Place et mise à jour en c.UpdatedAt,
// est parvenue au lieu at à l'instant now. Une rumeur met HopDelay par région
// à voyager, et s'éteint après Forgetting.
func Reaches(c Card, at world.RegionPos, now time.Time) bool {
	d := Distance(c.Place, at)
	if d > RumorRadius {
		return false
	}
	age := now.Sub(c.UpdatedAt)
	return age >= time.Duration(d)*HopDelay && age <= Forgetting
}

// Fidelity applique la perte de détails selon la distance : ce qui est loin se
// raconte approximativement.
//
//	distance 0-1 : tout
//	distance 2   : alias, rôle, origine
//	distance 3   : alias, rôle
//
// Les exploits notoires (seigneurs de guerre vaincus) voyagent plus loin que le
// reste : ils restent connus à toute distance. Le niveau n'est connu que de près.
func Fidelity(c Card, distance int) (Intro, Facts) {
	in, f := c.Intro, Facts{BossKills: c.BossKills}
	if distance > 2 {
		in.Origin = ""
	}
	if distance > 1 {
		in.Goal, in.Trait = "", ""
	}
	if distance <= 1 {
		f.Level = c.Level
	}
	if distance == 0 {
		f.Deaths = c.Deaths
	}
	return in, f
}

// Hear calcule ce qu'un PNJ du lieu at sait, à l'instant now, parmi les fiches
// candidates. speaker est le compte qui lui parle : sa propre fiche, si elle est
// parvenue jusqu'ici, est renvoyée avec Self vrai et ne compte pas dans le quota
// de rumeurs. Résultat trié par proximité puis par récence, au plus MaxHeard
// rumeurs (plus la fiche de l'interlocuteur).
func Hear(cards []Card, at world.RegionPos, speaker string, now time.Time) []Heard {
	var out []Heard
	for _, c := range cards {
		if !Reaches(c, at, now) {
			continue
		}
		d := Distance(c.Place, at)
		in, f := Fidelity(c, d)
		out = append(out, Heard{
			Account: c.Account, Distance: d, Self: strings.EqualFold(c.Account, speaker),
			Intro: in, Facts: f, Place: c.Place, Age: now.Sub(c.UpdatedAt),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].Age < out[j].Age
	})

	// Un compte n'apparaît qu'une fois, avec sa fiche la plus proche.
	var result []Heard
	rumors := 0
	taken := map[string]bool{}
	for _, h := range out {
		k := strings.ToLower(h.Account)
		if taken[k] {
			continue
		}
		taken[k] = true
		if !h.Self {
			if rumors >= MaxHeard {
				continue
			}
			rumors++
		}
		result = append(result, h)
	}
	return result
}

// Refus d'une fiche.
var (
	ErrEmptyIntro    = fmt.Errorf("lore: présentation vide")
	ErrUnsafeText    = fmt.Errorf("lore: texte refusé")
	ErrImpersonation = fmt.Errorf("lore: nom déjà porté par un autre joueur")
)

// Clean nettoie et borne une présentation. Elle est refusée si, une fois nettoyée,
// elle est vide ou contient un lien ou une adresse (spam, données personnelles).
func (in Intro) Clean() (Intro, error) {
	out := Intro{
		Alias:  textsafe.Clean(in.Alias, MaxAlias),
		Role:   textsafe.Clean(in.Role, MaxField),
		Origin: textsafe.Clean(in.Origin, MaxField),
		Goal:   textsafe.Clean(in.Goal, MaxField),
		Trait:  textsafe.Clean(in.Trait, MaxField),
	}
	if out.Alias == "" && out.Role == "" && out.Origin == "" && out.Goal == "" && out.Trait == "" {
		return Intro{}, ErrEmptyIntro
	}
	for _, f := range out.Fields() {
		if looksLikeLinkOrAddress(f) {
			return Intro{}, ErrUnsafeText
		}
	}
	return out, nil
}

// Fields renvoie les champs non vides, pour la modération et les contrôles.
func (in Intro) Fields() []string {
	var out []string
	for _, f := range []string{in.Alias, in.Role, in.Origin, in.Goal, in.Trait} {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// looksLikeLinkOrAddress repère les liens et adresses de messagerie. Ce n'est pas
// un filtre de sécurité complet (la modération s'en charge) mais il évite que le
// lore serve à faire de la publicité ou à diffuser des coordonnées.
func looksLikeLinkOrAddress(s string) bool {
	l := strings.ToLower(s)
	for _, marker := range []string{"http:", "https:", "://", "www.", "@", ".com", ".fr", ".net", ".org", ".io", "discord.gg"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// LooksLikeLink repère les liens et adresses de messagerie (voir looksLikeLinkOrAddress).
func LooksLikeLink(s string) bool { return looksLikeLinkOrAddress(s) }

// CleanSummary nettoie le souvenir d'un PNJ sur un joueur.
func CleanSummary(s string) string {
	s = textsafe.Clean(s, MaxSummary)
	if looksLikeLinkOrAddress(s) {
		return ""
	}
	return s
}

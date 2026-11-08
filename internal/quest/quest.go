// Package quest définit les quêtes confiées par les PNJ.
//
// Un modèle de langage peut PROPOSER une quête (titre, histoire, objectif, récompense),
// mais jamais en décider : Validate est la seule porte d'entrée. Elle n'accepte que des
// objectifs que le serveur sait vérifier lui-même, et ramène toute récompense dans des
// plafonds calculés d'après la difficulté. Le texte libre n'a aucun effet de jeu.
package quest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Limites.
const (
	MaxTitle   = 60
	MaxStory   = 280
	MaxNote    = 80
	MinCount   = 3
	MaxCount   = 12
	MaxActive  = 5                  // quêtes actives par joueur
	Lifetime   = 7 * 24 * time.Hour // une quête non rendue expire
	GiverPause = 30 * time.Minute   // délai avant une nouvelle quête du même donneur
	BossLevel  = 10                 // niveau minimal pour une quête contre le seigneur de guerre
	PlaceRange = 3                  // distance maximale (en régions) d'un lieu cible
	bossDiff   = 250                // difficulté d'une quête de boss (XP du seigneur de guerre)
	reachBase  = 20
	reachStep  = 15
)

// Kind est le type d'objectif.
type Kind string

const (
	Kill  Kind = "kill"  // vaincre N monstres d'une espèce
	Reach Kind = "reach" // entrer dans un lieu
	Boss  Kind = "boss"  // vaincre le seigneur de guerre
)

// State est l'état d'une quête.
type State string

const (
	Active    State = "active"
	Ready     State = "ready" // objectif atteint, à rendre au donneur
	Done      State = "done"
	Abandoned State = "abandoned"
	Expired   State = "expired"
)

// Objective est ce que le joueur doit accomplir, vérifié par le serveur.
type Objective struct {
	Kind    Kind
	Species string          // Kill, Boss
	Count   int             // Kill (3 à 12), 1 sinon
	Target  world.RegionPos // Reach : région du lieu à atteindre
}

// Reward est ce que le serveur donne à la remise. Note est un texte sans effet.
type Reward struct {
	Gold, XP, Potions int
	Note              string
}

// Quest est une quête confiée à un joueur.
type Quest struct {
	ID        int64
	Account   string
	Giver     world.NPCKey
	Title     string
	Story     string
	Objective Objective
	Progress  int
	Reward    Reward
	Tier      int // difficulté, 1 à 3 : points de réputation = 1 + Tier
	State     State
	CreatedAt time.Time
	DoneAt    time.Time
}

// Place est le lieu du donneur (la réputation s'acquiert là).
func (q Quest) Place() world.RegionPos { return q.Giver.Region }

// Need est le nombre d'actions à accomplir.
func (q Quest) Need() int { return max(1, q.Objective.Count) }

// Open indique que la quête est en cours (active ou prête).
func (q Quest) Open() bool { return q.State == Active || q.State == Ready }

// Proposal est ce que dit le modèle, déjà extrait du JSON mais pas encore vérifié.
type Proposal struct {
	Title, Story string
	Type         string // "kill" | "reach" | "boss"
	Species      string
	Place        string // nom d'un lieu proposé par le contexte
	Count        int
	Gold, XP     int
	Potions      int
	Note         string
}

// Species est une espèce que le joueur peut se voir demander de chasser.
type Species struct {
	Kind string
	XP   int
}

// Context est ce que le serveur autorise pour cette proposition.
type Context struct {
	Level      int
	Giver      world.NPCKey
	Species    []Species                  // espèces autorisées (hors garnisons, adaptées au niveau)
	Places     map[string]world.RegionPos // nom du lieu (en minuscules) → région ; lieux à portée seulement
	BossAround bool                       // une forteresse est à portée
	Now        time.Time
}

// Errors.
var (
	ErrInvalid = errors.New("quête invalide")
)

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Validate transforme une proposition en quête, ou la refuse. La quête renvoyée n'a pas
// d'ID ni de compte (le jeu les fixe). La modération des textes reste à faire.
func Validate(p Proposal, c Context) (Quest, error) {
	title := textsafe.Clean(p.Title, MaxTitle)
	story := textsafe.Clean(p.Story, MaxStory)
	note := textsafe.Clean(p.Note, MaxNote)
	if title == "" || story == "" {
		return Quest{}, reject("titre ou histoire vide")
	}
	for _, s := range []string{title, story, note} {
		if lore.LooksLikeLink(s) {
			return Quest{}, reject("lien ou adresse dans le texte")
		}
	}

	q := Quest{Giver: c.Giver, Title: title, Story: story, State: Active, CreatedAt: c.Now}
	var difficulty int
	switch Kind(strings.ToLower(strings.TrimSpace(p.Type))) {
	case Kill:
		sp, ok := findSpecies(c.Species, p.Species)
		if !ok {
			return Quest{}, reject("espèce non autorisée %q", p.Species)
		}
		count := p.Count
		if count < MinCount || count > MaxCount {
			return Quest{}, reject("nombre %d hors de [%d, %d]", count, MinCount, MaxCount)
		}
		q.Objective = Objective{Kind: Kill, Species: sp.Kind, Count: count}
		difficulty = sp.XP * count
	case Reach:
		target, ok := c.Places[strings.ToLower(strings.TrimSpace(p.Place))]
		if !ok {
			return Quest{}, reject("lieu inconnu ou hors de portée %q", p.Place)
		}
		if target == c.Giver.Region {
			return Quest{}, reject("le lieu cible est celui du donneur")
		}
		q.Objective = Objective{Kind: Reach, Count: 1, Target: target}
		difficulty = reachBase + reachStep*lore.Distance(target, c.Giver.Region)
	case Boss:
		if !c.BossAround || c.Level < BossLevel {
			return Quest{}, reject("quête de seigneur de guerre indisponible")
		}
		q.Objective = Objective{Kind: Boss, Species: "warlord", Count: 1}
		difficulty = bossDiff
	default:
		return Quest{}, reject("type d'objectif inconnu %q", p.Type)
	}

	q.Tier = TierOf(difficulty)
	q.Reward = ClampReward(Reward{Gold: p.Gold, XP: p.XP, Potions: p.Potions, Note: note}, difficulty, q.Objective.Kind == Boss)
	return q, nil
}

func findSpecies(list []Species, kind string) (Species, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	for _, s := range list {
		if s.Kind == kind {
			return s, true
		}
	}
	return Species{}, false
}

// TierOf classe une difficulté en palier 1 à 3.
func TierOf(difficulty int) int {
	switch {
	case difficulty < 40:
		return 1
	case difficulty < 120:
		return 2
	}
	return 3
}

// RewardBounds renvoie les bornes de la récompense pour une difficulté donnée.
func RewardBounds(difficulty int, boss bool) (minR, maxR Reward) {
	maxPotions := 2
	if boss {
		maxPotions = 3
	}
	minR = Reward{Gold: difficulty / 2, XP: 0, Potions: 0}
	maxR = Reward{Gold: 2 * difficulty, XP: difficulty / 4, Potions: maxPotions}
	return
}

// ClampReward ramène une récompense proposée dans les bornes. Une valeur absente ou
// aberrante devient le minimum (ou le maximum si elle dépasse) : jamais plus que le plafond.
func ClampReward(r Reward, difficulty int, boss bool) Reward {
	lo, hi := RewardBounds(difficulty, boss)
	r.Gold = max(lo.Gold, min(r.Gold, hi.Gold))
	r.XP = max(lo.XP, min(r.XP, hi.XP))
	r.Potions = max(lo.Potions, min(r.Potions, hi.Potions))
	return r
}

// ReputationGain est le gain de réputation d'une quête rendue.
func (q Quest) ReputationGain() int { return 1 + max(1, min(q.Tier, 3)) }

// Advance fait progresser une quête et la passe à Ready quand l'objectif est atteint.
// Elle renvoie vrai si quelque chose a changé. Sans effet sur une quête non active.
func (q *Quest) Advance(by int) bool {
	if q.State != Active || by <= 0 {
		return false
	}
	q.Progress = min(q.Need(), q.Progress+by)
	if q.Progress >= q.Need() {
		q.State = Ready
	}
	return true
}

// Expired indique si la quête, encore ouverte, a dépassé sa durée de vie.
func (q Quest) Expired(now time.Time) bool {
	return q.Open() && now.Sub(q.CreatedAt) > Lifetime
}

// SameObjective indique que deux quêtes ont le même donneur et le même objectif.
func SameObjective(a, b Quest) bool {
	return a.Giver == b.Giver && a.Objective == b.Objective
}

// ---------------------------------------------------------------- réputation

// Paliers de réputation par lieu.
const (
	Known    = 3
	Esteemed = 8
	Honored  = 15
)

// TierName donne le palier de réputation.
func TierName(points int) string {
	switch {
	case points >= Honored:
		return "Honoré"
	case points >= Esteemed:
		return "Estimé"
	case points >= Known:
		return "Connu"
	}
	return "Inconnu"
}

// TierLabel donne le palier de réputation dans la langue du joueur.
func TierLabel(l i18n.Lang, points int) string {
	switch {
	case points >= Honored:
		return i18n.T(l, "quest.tier.3")
	case points >= Esteemed:
		return i18n.T(l, "quest.tier.2")
	case points >= Known:
		return i18n.T(l, "quest.tier.1")
	}
	return i18n.T(l, "quest.tier.0")
}

// NextTier renvoie les points manquants pour le palier suivant (0 au maximum).
func NextTier(points int) int {
	for _, t := range []int{Known, Esteemed, Honored} {
		if points < t {
			return t - points
		}
	}
	return 0
}

// DiscountPercent est la réduction des prix dans le lieu (soins, potions).
func DiscountPercent(points int) int {
	switch {
	case points >= Honored:
		return 40
	case points >= Esteemed:
		return 20
	}
	return 0
}

// Discount applique la réduction de réputation à un prix (jamais en dessous de 1 si le prix est positif).
func Discount(price, points int) int {
	if price <= 0 {
		return price
	}
	return max(1, price*(100-DiscountPercent(points))/100)
}

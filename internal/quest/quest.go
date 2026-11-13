// Package quest defines the quests handed out by NPCs.
//
// A language model can PROPOSE a quest (title, story, objective, reward),
// but never decide it: Validate is the only entry point. It accepts only
// objectives the server can verify itself, and brings every reward within
// caps computed from the difficulty. Free text has no game effect.
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

// Limits.
const (
	MaxTitle    = 60
	MaxStory    = 280
	MaxNote     = 80
	MinCount    = 3
	MaxCount    = 12
	MaxActive   = 5                  // active quests per player
	Lifetime    = 7 * 24 * time.Hour // an unreturned quest expires
	GiverPause  = 30 * time.Minute   // delay before a new quest from the same giver
	BossLevel   = 10                 // minimum level for a quest against the warlord
	PlaceRange  = 3                  // maximum distance (in regions) of a target place
	bossDiff    = 250                // difficulty of a boss quest (warlord's XP)
	dungeonBase = 150                // difficulty of a dungeon quest: dungeonBase + dungeonStep × dungeon tier
	dungeonStep = 50
	reachBase   = 20
	reachStep   = 15
)

// Kind is the objective type.
type Kind string

const (
	Kill  Kind = "kill"  // defeat N monsters of a species
	Reach Kind = "reach" // enter a place
	Boss  Kind = "boss"  // defeat the warlord
	// Dungeon: defeat the master of a secret dungeon (Objective.Target is the region hiding it).
	Dungeon Kind = "dungeon"
)

// DungeonLevel is the minimum level at which a dungeon of a given tier (1 to 5) is entrusted to you: 3, 5, 7, 9, 11.
func DungeonLevel(tier int) int { return 2*max(1, min(tier, 5)) + 1 }

// State is the state of a quest.
type State string

const (
	Active    State = "active"
	Ready     State = "ready" // objective reached, to be returned to the giver
	Done      State = "done"
	Abandoned State = "abandoned"
	Expired   State = "expired"
)

// Objective is what the player must accomplish, verified by the server.
type Objective struct {
	Kind    Kind
	Species string          // Kill, Boss
	Count   int             // Kill (3 to 12), 1 otherwise
	Target  world.RegionPos // Reach: region of the place to reach; Dungeon: region hiding the dungeon
}

// Reward is what the server gives on turn-in. Note is text with no effect.
type Reward struct {
	Gold, XP, Potions int
	Note              string
}

// Quest is a quest entrusted to a player.
type Quest struct {
	ID        int64
	Account   string
	Giver     world.NPCKey
	Title     string
	Story     string
	Objective Objective
	Progress  int
	Reward    Reward
	Tier      int // difficulty, 1 to 3: reputation points = 1 + Tier
	State     State
	CreatedAt time.Time
	DoneAt    time.Time
}

// Place is the giver's place (reputation is earned there).
func (q Quest) Place() world.RegionPos { return q.Giver.Region }

// Need is the number of actions to accomplish.
func (q Quest) Need() int { return max(1, q.Objective.Count) }

// PauseLeft gives the time remaining before the giver hands out a new quest, when this one has just
// been turned in (0 otherwise).
func (q Quest) PauseLeft(now time.Time) time.Duration {
	if q.State != Done {
		return 0
	}
	return max(0, GiverPause-now.Sub(q.DoneAt))
}

// Open reports that the quest is in progress (active or ready).
func (q Quest) Open() bool { return q.State == Active || q.State == Ready }

// Proposal is what the model says, already extracted from the JSON but not yet verified.
type Proposal struct {
	Title, Story string
	Type         string // "kill" | "reach" | "boss" | "dungeon"
	Species      string
	Place        string // name of a place (reach) or a dungeon (dungeon) offered by the context
	Count        int
	Gold, XP     int
	Potions      int
	Note         string
}

// Species is a species the player may be asked to hunt.
type Species struct {
	Kind string
	XP   int
}

// DungeonRef designates a dungeon the server allows to be entrusted: the region hiding it and its tier.
type DungeonRef struct {
	Region world.RegionPos
	Tier   int
}

// Context is what the server allows for this proposal.
type Context struct {
	Level      int
	Giver      world.NPCKey
	Species    []Species                  // allowed species (excluding garrisons, adapted to the level)
	Places     map[string]world.RegionPos // place name (lowercase) → region; places in range only
	BossAround bool                       // a fortress is in range
	Dungeons   map[string]DungeonRef      // dungeon name (lowercase) → dungeon; in range and accessible at the level only
	Now        time.Time
}

// Errors.
var (
	ErrInvalid = errors.New("invalid quest")
)

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Validate turns a proposal into a quest, or refuses it. The returned quest has no
// ID or account (the game sets them). Moderation of the texts remains to be done.
func Validate(p Proposal, c Context) (Quest, error) {
	title := textsafe.Clean(p.Title, MaxTitle)
	story := textsafe.Clean(p.Story, MaxStory)
	note := textsafe.Clean(p.Note, MaxNote)
	if title == "" || story == "" {
		return Quest{}, reject("empty title or story")
	}
	for _, s := range []string{title, story, note} {
		if lore.LooksLikeLink(s) {
			return Quest{}, reject("link or address in the text")
		}
	}

	q := Quest{Giver: c.Giver, Title: title, Story: story, State: Active, CreatedAt: c.Now}
	var difficulty int
	switch Kind(strings.ToLower(strings.TrimSpace(p.Type))) {
	case Kill:
		sp, ok := findSpecies(c.Species, p.Species)
		if !ok {
			return Quest{}, reject("species not allowed %q", p.Species)
		}
		count := p.Count
		if count < MinCount || count > MaxCount {
			return Quest{}, reject("count %d outside [%d, %d]", count, MinCount, MaxCount)
		}
		q.Objective = Objective{Kind: Kill, Species: sp.Kind, Count: count}
		difficulty = sp.XP * count
	case Reach:
		target, ok := c.Places[strings.ToLower(strings.TrimSpace(p.Place))]
		if !ok {
			return Quest{}, reject("place unknown or out of range %q", p.Place)
		}
		if target == c.Giver.Region {
			return Quest{}, reject("the target place is the giver's")
		}
		q.Objective = Objective{Kind: Reach, Count: 1, Target: target}
		difficulty = reachBase + reachStep*lore.Distance(target, c.Giver.Region)
	case Boss:
		if !c.BossAround || c.Level < BossLevel {
			return Quest{}, reject("warlord quest unavailable")
		}
		q.Objective = Objective{Kind: Boss, Species: "warlord", Count: 1}
		difficulty = bossDiff
	case Dungeon:
		ref, ok := c.Dungeons[strings.ToLower(strings.TrimSpace(p.Place))]
		if !ok {
			return Quest{}, reject("dungeon unknown or out of range %q", p.Place)
		}
		if c.Level < DungeonLevel(ref.Tier) {
			return Quest{}, reject("level %d insufficient for a tier %d dungeon", c.Level, ref.Tier)
		}
		q.Objective = Objective{Kind: Dungeon, Species: "lich", Count: 1, Target: ref.Region}
		difficulty = dungeonBase + dungeonStep*ref.Tier
	default:
		return Quest{}, reject("unknown objective type %q", p.Type)
	}

	q.Tier = TierOf(difficulty)
	q.Reward = ClampReward(Reward{Gold: p.Gold, XP: p.XP, Potions: p.Potions, Note: note}, difficulty, q.Objective.Kind == Boss || q.Objective.Kind == Dungeon)
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

// TierOf classifies a difficulty into tier 1 to 3.
func TierOf(difficulty int) int {
	switch {
	case difficulty < 40:
		return 1
	case difficulty < 120:
		return 2
	}
	return 3
}

// RewardBounds returns the reward bounds for a given difficulty.
func RewardBounds(difficulty int, boss bool) (minR, maxR Reward) {
	maxPotions := 2
	if boss {
		maxPotions = 3
	}
	minR = Reward{Gold: difficulty / 2, XP: 0, Potions: 0}
	maxR = Reward{Gold: 2 * difficulty, XP: difficulty / 4, Potions: maxPotions}
	return
}

// ClampReward brings a proposed reward within bounds. A missing or
// absurd value becomes the minimum (or the maximum if it exceeds): never more than the cap.
func ClampReward(r Reward, difficulty int, boss bool) Reward {
	lo, hi := RewardBounds(difficulty, boss)
	r.Gold = max(lo.Gold, min(r.Gold, hi.Gold))
	r.XP = max(lo.XP, min(r.XP, hi.XP))
	r.Potions = max(lo.Potions, min(r.Potions, hi.Potions))
	return r
}

// ReputationGain is the reputation gain of a completed quest.
func (q Quest) ReputationGain() int { return 1 + max(1, min(q.Tier, 3)) }

// Advance progresses a quest and moves it to Ready when the objective is reached.
// It returns true if something changed. No effect on a non-active quest.
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

// Expired reports whether the quest, still open, has exceeded its lifetime.
func (q Quest) Expired(now time.Time) bool {
	return q.Open() && now.Sub(q.CreatedAt) > Lifetime
}

// SameObjective reports that two quests have the same giver and the same objective.
func SameObjective(a, b Quest) bool {
	return a.Giver == b.Giver && a.Objective == b.Objective
}

// ---------------------------------------------------------------- reputation

// Reputation tiers per place.
const (
	Known    = 3
	Esteemed = 8
	Honored  = 15

	// Suspect and Wanted are the negative tiers: at Wanted, the place's guards attack.
	Suspect = -5
	Wanted  = -10
	// MinReputation bounds the fall: beyond it, a place cannot be more hostile.
	MinReputation = -20
)

// TierName gives the reputation tier (French names).
func TierName(points int) string {
	switch {
	case points >= Honored:
		return "Honoré"
	case points >= Esteemed:
		return "Estimé"
	case points >= Known:
		return "Connu"
	case points <= Wanted:
		return "Recherché"
	case points <= Suspect:
		return "Suspect"
	}
	return "Inconnu"
}

// TierLabel gives the reputation tier in the player's language.
func TierLabel(l i18n.Lang, points int) string {
	switch {
	case points >= Honored:
		return i18n.T(l, "quest.tier.3")
	case points >= Esteemed:
		return i18n.T(l, "quest.tier.2")
	case points >= Known:
		return i18n.T(l, "quest.tier.1")
	case points <= Wanted:
		return i18n.T(l, "quest.tier.wanted")
	case points <= Suspect:
		return i18n.T(l, "quest.tier.suspect")
	}
	return i18n.T(l, "quest.tier.0")
}

// NextTier returns the points missing for the next tier (0 at the maximum, and 0 for a player in bad standing:
// they have no "next tier" to aim for).
func NextTier(points int) int {
	if points < 0 {
		return 0
	}
	for _, t := range []int{Known, Esteemed, Honored} {
		if points < t {
			return t - points
		}
	}
	return 0
}

// IsWanted reports that the place considers the player an outlaw.
func IsWanted(points int) bool { return points <= Wanted }

// DiscountPercent is the price reduction in the place (healing, potions).
func DiscountPercent(points int) int {
	switch {
	case points >= Honored:
		return 40
	case points >= Esteemed:
		return 20
	}
	return 0
}

// Discount applies the reputation discount to a price (never below 1 if the price is positive).
func Discount(price, points int) int {
	if price <= 0 {
		return price
	}
	return max(1, price*(100-DiscountPercent(points))/100)
}

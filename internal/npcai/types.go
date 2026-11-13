// Package npcai makes NPCs talk using a language model.
//
// For each player message it orchestrates: the limits (rate, budget),
// moderation, the collective memory (internal/lore), the prompt, the call to the
// model (internal/llm) and the parsing of its response. Nothing the model says
// acts on the game: the result is text, plus, possibly, a player
// introduction validated before being memorized.
package npcai

import (
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Speaker is the player who speaks, with the facts established by the server about them.
type Speaker struct {
	Account   string // account nickname
	Level     int
	Kills     int
	BossKills int
	Deaths    int
}

// Turn is an exchange already held during the current session.
type Turn struct{ Player, NPC string }

// Request is what the game asks for: a player's line to an NPC.
type Request struct {
	NPC       world.NPC
	Lang      i18n.Lang // player's language: that of the prompt and of the expected reply
	Region    string    // name of the place's region (empty: unspecified)
	Place     string    // name of the place
	PlaceKind world.Kind
	Speaker   Speaker
	Message   string // already cleaned (textsafe) and bounded by the caller
	History   []Turn // previous exchanges of the session, oldest to newest

	// Opening asks the NPC to open the conversation: it greets the traveler and, if it
	// does not know them, invites them to introduce themselves. Message is then empty, and nothing is
	// memorized (the player has not said anything yet).
	Opening bool

	// Reputation: the player's points in this NPC's place (fact established by the server).
	Reputation int
	// Impression: the first impression given by the player's charisma (tier computed
	// by the server, never text from the player).
	Impression string
	// Offer, set only for a quest giver who can hand one to the
	// player, allows the model to propose a quest (see quest.Validate).
	Offer *OfferContext

	// Landmarks: the surrounding places this NPC knows (village chief, mayor), established by the
	// server and already located. Empty for other NPCs.
	Landmarks []Landmark

	// Residents: the place's inhabitants a guard knows (job, direction and distance in steps from them),
	// established by the server. Empty for other NPCs.
	Residents []Resident

	// Deeds: completed quests this NPC knows about (set by the service, not by the game).
	Deeds Deeds
}

// Landmark is a neighboring place as the server describes it to an NPC who knows it.
type Landmark struct {
	Name      string // « Kerhaven »
	Kind      string // "village", "town", "fortress"
	Region    string // region, if it differs from the NPC's
	Direction string // "to the east"
	Distance  string // "very close", "a good walk away", "far"
	Note      string // notable fact (e.g. garrison)
}

// Resident is an inhabitant of the place as the server describes them to a guard.
type Resident struct {
	Name      string // "Blacksmith Aldric"
	Job       string // "blacksmith"
	Direction string // "to the north-east"
	Steps     int    // distance in steps
}

// Deeds are the completed quests known to an NPC: those of its interlocutor and, as rumor, those of others.
type Deeds struct{ Mine, Others []lore.HeardDeed }

// OfferContext describes the work this NPC can hand to this player.
type OfferContext struct {
	Quest    quest.Context // what the server accepts (species, places, boss...)
	Existing []string      // titles of quests already in progress with this NPC (not to be repeated)
}

// Failure explains why no real answer could be produced.
type Failure int

const (
	OK          Failure = iota
	Busy                // queue full, request already in progress or provider limit
	Cooldown            // the player talks too fast or too often
	Budget              // daily token budget exhausted
	Blocked             // message rejected by moderation
	Unavailable         // service unavailable or unusable response
)

func (f Failure) String() string {
	return [...]string{"ok", "busy", "cooldown", "budget", "blocked", "unavailable"}[f]
}

// Reply is the result of a request. Text is always displayable: on
// failure, it is an in-character line (the NPC is tired, distracted...).
type Reply struct {
	Text       string
	Failure    Failure
	Remembered bool // an introduction card was saved
	// Quest is a quest proposed by the model, already validated (quest.Validate) and moderated.
	// It does not exist yet: the player must accept it.
	Quest *quest.Quest
	// QuestDropped: the model proposed a quest, but it was discarded (invalid or not moderated).
	// Its reply may have announced it: the game warns the player that there is in the end nothing.
	QuestDropped bool
	Tokens       int // tokens consumed by the call
}

// Config tunes the service. Zero values take the DefaultConfig values.
type Config struct {
	Workers       int           // simultaneous calls to the provider
	QueueSize     int           // pending requests before answering "busy"
	Timeout       time.Duration // maximum duration of a request
	MinInterval   time.Duration // minimum delay between two messages from the same player
	HourlyLimit   int           // messages per player per hour
	CardCooldown  time.Duration // minimum delay between two card writes by a player
	DailyTokens   int           // global token budget per day (UTC)
	MaxTokens     int           // maximum tokens of a reply
	Temperature   float64
	MaxHistory    int // session exchanges provided to the model
	BreakerFails  int // consecutive failures before suspending the service
	BreakerPause  time.Duration
	BlockedThreat float64 // threshold for blocked categories (0 to 1; lower = stricter)
}

// DefaultConfig returns cautious values for a small server.
func DefaultConfig() Config {
	return Config{
		Workers: 4, QueueSize: 32, Timeout: 25 * time.Second,
		MinInterval: 1500 * time.Millisecond, HourlyLimit: 60, CardCooldown: 30 * time.Second,
		DailyTokens: 1_000_000, MaxTokens: 260, Temperature: 0.8, MaxHistory: 12,
		BreakerFails: 5, BreakerPause: 30 * time.Second, BlockedThreat: 0.5,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.Workers <= 0 {
		c.Workers = d.Workers
	}
	if c.QueueSize <= 0 {
		c.QueueSize = d.QueueSize
	}
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.MinInterval < 0 {
		c.MinInterval = 0
	} else if c.MinInterval == 0 {
		c.MinInterval = d.MinInterval
	}
	if c.HourlyLimit <= 0 {
		c.HourlyLimit = d.HourlyLimit
	}
	if c.CardCooldown <= 0 {
		c.CardCooldown = d.CardCooldown
	}
	if c.DailyTokens <= 0 {
		c.DailyTokens = d.DailyTokens
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = d.MaxTokens
	}
	if c.Temperature <= 0 {
		c.Temperature = d.Temperature
	}
	if c.MaxHistory <= 0 {
		c.MaxHistory = d.MaxHistory
	}
	if c.BreakerFails <= 0 {
		c.BreakerFails = d.BreakerFails
	}
	if c.BreakerPause <= 0 {
		c.BreakerPause = d.BreakerPause
	}
	if c.BlockedThreat <= 0 {
		c.BlockedThreat = d.BlockedThreat
	}
	return c
}

// Stats is a snapshot of the service counters (for /stats).
type Stats struct {
	Available      bool   `json:"available"`
	Guardrails     bool   `json:"guardrails"`
	Moderation     bool   `json:"moderation"`
	Requests       uint64 `json:"requests"`
	Failures       uint64 `json:"failures"`
	Blocked        uint64 `json:"blocked"`
	Remembered     uint64 `json:"remembered"`
	QuestsOffered  uint64 `json:"quests_proposed"`
	QuestsRejected uint64 `json:"quests_rejected"`
	StockGenerated uint64 `json:"stock_generated"`
	StockRejected  uint64 `json:"stock_rejected"`
	TokensToday    int    `json:"tokens_today"`
	DailyBudget    int    `json:"daily_budget"`
	InFlight       int    `json:"in_flight"`
	Queued         int    `json:"queued"`
	AvgLatencyMS   int64  `json:"avg_latency_ms"`
}

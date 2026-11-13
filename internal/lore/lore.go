// Package lore is the NPCs' collective memory: the introductions players
// make of themselves, how they spread from place to place as rumors, and
// each NPC's private memories of each player.
//
// The rules (visibility, fidelity, validation) are pure functions with
// an injectable clock: they can be tested without a database or network.
package lore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Memory settings. These are game constants, easy to tweak.
const (
	// RumorRadius is the maximum distance (in regions, Chebyshev) to which an
	// introduction spreads: the place and the adjacent regions (a region is
	// 256 tiles wide, so ≈ 380 tiles at most from one place to another).
	RumorRadius = 1
	// HopDelay is the time a rumor takes to cross one region.
	HopDelay = time.Hour
	// Forgetting is the duration after which an introduction is forgotten if the
	// player does not renew it.
	Forgetting = 14 * 24 * time.Hour
	// MaxHeard bounds the number of rumors given to an NPC for one conversation.
	MaxHeard = 6
	// MaxCardsPerAccount bounds the places where a single player can have a card.
	MaxCardsPerAccount = 8

	// Maximum field sizes (in characters).
	MaxAlias   = 24
	MaxField   = 80
	MaxSummary = 300
)

// Intro is what a player says about themselves (unverified).
type Intro struct {
	Alias  string // name or nickname they give themselves
	Role   string // job, rank
	Origin string // where they come from
	Goal   string // what they are looking for
	Trait  string // a trait or an anecdote
}

// Facts are facts established by the server, which the player cannot forge.
type Facts struct {
	Level     int
	BossKills int
	Deaths    int
}

// Card is a player's introduction card, made in a given place.
type Card struct {
	Account string          // account nickname (server truth)
	Place   world.RegionPos // region of the place where the introduction was made
	Intro
	Facts
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Memory is an NPC's private memory of a player. It is never shown to another player.
type Memory struct {
	NPC       world.NPCKey
	Account   string
	Summary   string
	UpdatedAt time.Time
}

// Heard is a card as a given NPC knows it: complete if it was
// born here, increasingly patchy the farther it comes from.
type Heard struct {
	Account  string
	Distance int // in regions, 0 = made in this place
	Self     bool
	Intro    Intro // hidden fields emptied according to distance
	Facts    Facts // same
	Place    world.RegionPos
	Age      time.Duration // since the last update
}

// Local reports whether the introduction was made in the NPC's own place.
func (h Heard) Local() bool { return h.Distance == 0 }

// Distance returns the Chebyshev distance, in regions, between two places.
func Distance(a, b world.RegionPos) int {
	return max(abs(a.X-b.X), abs(a.Y-b.Y))
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// Reaches reports whether a card, born at c.Place and updated at c.UpdatedAt,
// has reached place at at instant now. A rumor takes HopDelay per region
// to travel, and dies out after Forgetting.
func Reaches(c Card, at world.RegionPos, now time.Time) bool {
	d := Distance(c.Place, at)
	if d > RumorRadius {
		return false
	}
	age := now.Sub(c.UpdatedAt)
	return age >= time.Duration(d)*HopDelay && age <= Forgetting
}

// Fidelity applies the loss of detail according to distance: NPC memory
// is local (RumorRadius = 1 region).
//
//	distance 0: everything, deaths included
//	distance 1: everything except deaths
func Fidelity(c Card, distance int) (Intro, Facts) {
	f := Facts{BossKills: c.BossKills, Level: c.Level}
	if distance == 0 {
		f.Deaths = c.Deaths
	}
	return c.Intro, f
}

// Hear computes what an NPC of place at knows, at instant now, among the
// candidate cards. speaker is the account talking to it: their own card, if it
// has reached here, is returned with Self true and does not count toward the
// rumor quota. Result sorted by proximity then by recency, at most MaxHeard
// rumors (plus the interlocutor's card).
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

	// An account appears only once, with its closest card.
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

// Reasons for refusing a card.
var (
	ErrEmptyIntro    = fmt.Errorf("lore: empty introduction")
	ErrUnsafeText    = fmt.Errorf("lore: text rejected")
	ErrImpersonation = fmt.Errorf("lore: name already used by another player")
)

// Clean cleans and bounds an introduction. It is refused if, once cleaned,
// it is empty or contains a link or an address (spam, personal data).
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

// Fields returns the non-empty fields, for moderation and checks.
func (in Intro) Fields() []string {
	var out []string
	for _, f := range []string{in.Alias, in.Role, in.Origin, in.Goal, in.Trait} {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// looksLikeLinkOrAddress spots links and email addresses. It is not
// a complete security filter (moderation handles that) but it prevents the
// lore from being used for advertising or to spread contact details.
func looksLikeLinkOrAddress(s string) bool {
	l := strings.ToLower(s)
	for _, marker := range []string{"http:", "https:", "://", "www.", "@", ".com", ".fr", ".net", ".org", ".io", "discord.gg"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// LooksLikeLink spots links and email addresses (see looksLikeLinkOrAddress).
func LooksLikeLink(s string) bool { return looksLikeLinkOrAddress(s) }

// CleanSummary cleans an NPC's memory of a player.
func CleanSummary(s string) string {
	s = textsafe.Clean(s, MaxSummary)
	if looksLikeLinkOrAddress(s) {
		return ""
	}
	return s
}

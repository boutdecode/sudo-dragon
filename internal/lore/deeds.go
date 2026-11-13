package lore

import (
	"context"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Deed is a completed quest: a verified fact, recorded by the server (not by the player).
// The place's NPCs remember it and the rumor travels like that of introductions.
type Deed struct {
	Account string
	Place   world.RegionPos // giver's place
	Title   string
	DoneAt  time.Time
}

// HeardDeed is a completed quest as an NPC knows of it.
type HeardDeed struct {
	Deed
	Distance int
	Age      time.Duration
}

// Local reports that the quest was completed in the NPC's own place.
func (h HeardDeed) Local() bool { return h.Distance == 0 }

// DeedRepo is the storage of completed quests (implemented by internal/store).
type DeedRepo interface {
	// DeedsNear returns the quests completed in the square of side 2*radius+1 centered on at,
	// from notBefore on, most recent first.
	DeedsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]Deed, error)
}

// MaxHeardDeeds bounds the facts reported in a prompt.
const MaxHeardDeeds = 6

// ReachesDeed: same rule as Reaches for a completed quest.
func ReachesDeed(d Deed, at world.RegionPos, now time.Time) bool {
	dist := Distance(d.Place, at)
	if dist > RumorRadius {
		return false
	}
	age := now.Sub(d.DoneAt)
	return age >= time.Duration(dist)*HopDelay && age <= Forgetting
}

// HeardDeeds returns what an NPC of place at knows of completed quests: those of its
// interlocutor (mine) and those of others (others, as rumor). Nothing if Deeds is nil.
func (b *Book) HeardDeeds(ctx context.Context, at world.RegionPos, speaker string) (mine, others []HeardDeed, err error) {
	if b.Deeds == nil {
		return nil, nil, nil
	}
	now := b.now()
	deeds, err := b.Deeds.DeedsNear(ctx, at, RumorRadius, now.Add(-Forgetting), candidateLimit)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range deeds {
		if !ReachesDeed(d, at, now) {
			continue
		}
		h := HeardDeed{Deed: d, Distance: Distance(d.Place, at), Age: now.Sub(d.DoneAt)}
		switch {
		case strings.EqualFold(d.Account, speaker):
			if len(mine) < MaxHeardDeeds {
				mine = append(mine, h)
			}
		case len(others) < MaxHeardDeeds:
			others = append(others, h)
		}
	}
	return mine, others, nil
}

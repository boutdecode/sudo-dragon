package lore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Repo is the storage of the collective memory (implemented by internal/store).
type Repo interface {
	// UpsertCard writes the card of an (account, place), keeping CreatedAt if one
	// already exists, and discards the account's oldest cards beyond
	// MaxCardsPerAccount places.
	UpsertCard(ctx context.Context, c Card) error
	// GetCard returns an account's card in a place.
	GetCard(ctx context.Context, account string, place world.RegionPos) (Card, bool, error)
	// CardsNear returns the cards whose place is in the square of side 2*radius+1
	// centered on at and updated from notBefore on, most recent first.
	CardsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]Card, error)
	// UpdateFacts updates the verified facts of all of an account's cards,
	// without touching their date (the rumor must not restart from zero).
	UpdateFacts(ctx context.Context, account string, f Facts) error
	// AccountExists tells whether a player nickname exists (case-insensitive).
	AccountExists(ctx context.Context, name string) (bool, error)
	GetMemory(ctx context.Context, npc world.NPCKey, account string) (Memory, bool, error)
	PutMemory(ctx context.Context, m Memory) error
}

// Moderator checks texts. It returns nil if they are acceptable, an error
// otherwise (text rejected, or moderation unavailable: it then fails closed).
type Moderator func(ctx context.Context, texts []string) error

// Book is the NPCs' collective memory.
type Book struct {
	Repo Repo
	// Deeds, optional, gives access to completed quests (see HeardDeeds).
	Deeds DeedRepo
	Now   func() time.Time // injectable in tests; time.Now by default
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// candidateLimit bounds the number of cards read before filtering by distance and delay.
const candidateLimit = 200

// Heard returns what an NPC of place at knows: the rumors that have reached it and,
// separately, its interlocutor's card if it has reached here.
func (b *Book) Heard(ctx context.Context, at world.RegionPos, speaker string) ([]Heard, error) {
	now := b.now()
	cards, err := b.Repo.CardsNear(ctx, at, RumorRadius, now.Add(-Forgetting), candidateLimit)
	if err != nil {
		return nil, err
	}
	return Hear(cards, at, speaker, now), nil
}

// Remember records what a player says about themselves in a place.
//
// The fields provided complete or replace those of their existing card (an empty
// field does not erase it). Nothing is written if the text is refused: empty, link or
// address, alias impersonating another player's nickname, or moderation refusal.
func (b *Book) Remember(ctx context.Context, account string, at world.RegionPos, in Intro, facts Facts, moderate Moderator) (Card, error) {
	clean, err := in.Clean()
	if err != nil {
		return Card{}, err
	}
	if clean.Alias != "" && !strings.EqualFold(clean.Alias, account) {
		exists, err := b.Repo.AccountExists(ctx, clean.Alias)
		if err != nil {
			return Card{}, err
		}
		if exists {
			return Card{}, ErrImpersonation
		}
	}

	prev, found, err := b.Repo.GetCard(ctx, account, at)
	if err != nil {
		return Card{}, err
	}
	merged := clean
	if found {
		merged = mergeIntro(prev.Intro, clean)
	}
	if moderate == nil {
		return Card{}, errors.New("lore: moderation missing, write refused")
	}
	if err := moderate(ctx, merged.Fields()); err != nil {
		return Card{}, err
	}

	now := b.now()
	card := Card{Account: account, Place: at, Intro: merged, Facts: facts, CreatedAt: now, UpdatedAt: now}
	if found {
		card.CreatedAt = prev.CreatedAt
	}
	if err := b.Repo.UpsertCard(ctx, card); err != nil {
		return Card{}, err
	}
	return card, nil
}

// mergeIntro completes old with the non-empty fields of upd.
func mergeIntro(old, upd Intro) Intro {
	pick := func(o, n string) string {
		if n != "" {
			return n
		}
		return o
	}
	return Intro{
		Alias: pick(old.Alias, upd.Alias), Role: pick(old.Role, upd.Role), Origin: pick(old.Origin, upd.Origin),
		Goal: pick(old.Goal, upd.Goal), Trait: pick(old.Trait, upd.Trait),
	}
}

// RefreshFacts updates the verified facts of a player's cards.
func (b *Book) RefreshFacts(ctx context.Context, account string, f Facts) error {
	return b.Repo.UpdateFacts(ctx, account, f)
}

// Recall returns an NPC's private memory of a player ("" if it has none).
func (b *Book) Recall(ctx context.Context, npc world.NPCKey, account string) (string, error) {
	m, found, err := b.Repo.GetMemory(ctx, npc, account)
	if err != nil || !found {
		return "", err
	}
	return m.Summary, nil
}

// Memorize records an NPC's memory of a player. A memory that is empty after cleaning is ignored.
func (b *Book) Memorize(ctx context.Context, npc world.NPCKey, account, summary string) error {
	summary = CleanSummary(summary)
	if summary == "" {
		return nil
	}
	return b.Repo.PutMemory(ctx, Memory{NPC: npc, Account: account, Summary: summary, UpdatedAt: b.now()})
}

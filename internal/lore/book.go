package lore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Repo est le stockage de la mémoire collective (implémenté par internal/store).
type Repo interface {
	// UpsertCard écrit la fiche d'un (compte, lieu) en conservant CreatedAt s'il en
	// existe déjà une, et écarte les fiches les plus anciennes du compte au-delà de
	// MaxCardsPerAccount lieux.
	UpsertCard(ctx context.Context, c Card) error
	// GetCard renvoie la fiche d'un compte dans un lieu.
	GetCard(ctx context.Context, account string, place world.RegionPos) (Card, bool, error)
	// CardsNear renvoie les fiches dont le lieu est dans le carré de côté 2*radius+1
	// centré sur at et mises à jour à partir de notBefore, les plus récentes d'abord.
	CardsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]Card, error)
	// UpdateFacts met à jour les faits vérifiés de toutes les fiches d'un compte,
	// sans toucher à leur date (la rumeur ne doit pas repartir de zéro).
	UpdateFacts(ctx context.Context, account string, f Facts) error
	// AccountExists dit si un pseudo de joueur existe (insensible à la casse).
	AccountExists(ctx context.Context, name string) (bool, error)
	GetMemory(ctx context.Context, npc world.NPCKey, account string) (Memory, bool, error)
	PutMemory(ctx context.Context, m Memory) error
}

// Moderator vérifie des textes. Il renvoie nil s'ils sont acceptables, une erreur
// sinon (texte refusé, ou modération indisponible: on échoue alors « fermé »).
type Moderator func(ctx context.Context, texts []string) error

// Book est la mémoire collective des PNJ.
type Book struct {
	Repo Repo
	Now  func() time.Time // injectable en test; time.Now par défaut
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// candidateLimit borne le nombre de fiches lues avant filtrage par distance et délai.
const candidateLimit = 200

// Heard renvoie ce que sait un PNJ du lieu at: les rumeurs qui lui sont parvenues et,
// à part, la fiche de son interlocuteur si elle est parvenue jusqu'ici.
func (b *Book) Heard(ctx context.Context, at world.RegionPos, speaker string) ([]Heard, error) {
	now := b.now()
	cards, err := b.Repo.CardsNear(ctx, at, RumorRadius, now.Add(-Forgetting), candidateLimit)
	if err != nil {
		return nil, err
	}
	return Hear(cards, at, speaker, now), nil
}

// Remember enregistre ce qu'un joueur dit de lui dans un lieu.
//
// Les champs fournis complètent ou remplacent ceux de sa fiche existante (un champ
// vide ne l'efface pas). Rien n'est écrit si le texte est refusé: vide, lien ou
// adresse, alias qui usurpe le pseudo d'un autre joueur, ou refus de la modération.
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
		return Card{}, errors.New("lore: modération absente, écriture refusée")
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

// mergeIntro complète old avec les champs non vides de upd.
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

// RefreshFacts met à jour les faits vérifiés des fiches d'un joueur.
func (b *Book) RefreshFacts(ctx context.Context, account string, f Facts) error {
	return b.Repo.UpdateFacts(ctx, account, f)
}

// Recall renvoie le souvenir privé d'un PNJ sur un joueur ("" s'il n'en a pas).
func (b *Book) Recall(ctx context.Context, npc world.NPCKey, account string) (string, error) {
	m, found, err := b.Repo.GetMemory(ctx, npc, account)
	if err != nil || !found {
		return "", err
	}
	return m.Summary, nil
}

// Memorize enregistre le souvenir d'un PNJ sur un joueur. Un souvenir vide après nettoyage est ignoré.
func (b *Book) Memorize(ctx context.Context, npc world.NPCKey, account, summary string) error {
	summary = CleanSummary(summary)
	if summary == "" {
		return nil
	}
	return b.Repo.PutMemory(ctx, Memory{NPC: npc, Account: account, Summary: summary, UpdatedAt: b.now()})
}

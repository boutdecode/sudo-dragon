package lore

import (
	"context"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

// Deed est une quête rendue : un fait vérifié, inscrit par le serveur (pas par le joueur).
// Les PNJ du lieu s'en souviennent et la rumeur voyage comme celle des présentations.
type Deed struct {
	Account string
	Place   world.RegionPos // lieu du donneur
	Title   string
	DoneAt  time.Time
}

// HeardDeed est une quête rendue telle qu'un PNJ en a connaissance.
type HeardDeed struct {
	Deed
	Distance int
	Age      time.Duration
}

// Local indique que la quête a été rendue dans le lieu même du PNJ.
func (h HeardDeed) Local() bool { return h.Distance == 0 }

// DeedRepo est le stockage des quêtes rendues (implémenté par internal/store).
type DeedRepo interface {
	// DeedsNear renvoie les quêtes rendues dans le carré de côté 2*radius+1 centré sur at,
	// à partir de notBefore, les plus récentes d'abord.
	DeedsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]Deed, error)
}

// MaxHeardDeeds borne les faits rapportés dans un prompt.
const MaxHeardDeeds = 6

// ReachesDeed : même règle que Reaches pour une quête rendue.
func ReachesDeed(d Deed, at world.RegionPos, now time.Time) bool {
	dist := Distance(d.Place, at)
	if dist > RumorRadius {
		return false
	}
	age := now.Sub(d.DoneAt)
	return age >= time.Duration(dist)*HopDelay && age <= Forgetting
}

// HeardDeeds renvoie ce qu'un PNJ du lieu at sait des quêtes rendues : celles de son
// interlocuteur (mine) et celles des autres (others, en rumeur). Rien si Deeds est nil.
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

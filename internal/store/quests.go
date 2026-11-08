package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// maxStoredQuests borne ce qu'on garde par compte : les quêtes en cours puis les plus
// récentes rendues. Les quêtes abandonnées ou expirées ne sont pas conservées.
const maxStoredQuests = 60

// saveQuests remplace les quêtes et la réputation d'un compte (dans la transaction de
// sauvegarde du personnage).
func saveQuests(tx *sql.Tx, account string, c Character) error {
	if _, err := tx.Exec(`DELETE FROM quests WHERE account = ?`, account); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM reputation WHERE account = ?`, account); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO quests(account, id, giver_rx, giver_ry, giver_idx, title, story, kind, species,
			count, target_rx, target_ry, progress, gold, xp, potions, note, tier, state, created_at, done_at)
		SELECT name, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM accounts WHERE name = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	stored := 0
	for _, q := range c.Quests {
		if q.State != quest.Active && q.State != quest.Ready && q.State != quest.Done {
			continue
		}
		if stored >= maxStoredQuests {
			break
		}
		stored++
		var doneAt int64
		if !q.DoneAt.IsZero() {
			doneAt = q.DoneAt.Unix()
		}
		o := q.Objective
		if _, err := stmt.Exec(q.ID, q.Giver.Region.X, q.Giver.Region.Y, q.Giver.Index, q.Title, q.Story,
			string(o.Kind), o.Species, o.Count, o.Target.X, o.Target.Y, q.Progress,
			q.Reward.Gold, q.Reward.XP, q.Reward.Potions, q.Reward.Note, q.Tier, string(q.State),
			q.CreatedAt.Unix(), doneAt, account); err != nil {
			return err
		}
	}
	for rp, pts := range c.Rep {
		if pts <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO reputation(account, region_x, region_y, points)
			SELECT name, ?, ?, ? FROM accounts WHERE name = ?`, rp.X, rp.Y, pts, account); err != nil {
			return err
		}
	}
	return nil
}

// loadQuests lit les quêtes (anciennes d'abord) et la réputation d'un compte.
func (s *Store) loadQuests(account string) ([]quest.Quest, map[world.RegionPos]int, error) {
	rows, err := s.db.Query(`SELECT id, giver_rx, giver_ry, giver_idx, title, story, kind, species, count,
			target_rx, target_ry, progress, gold, xp, potions, note, tier, state, created_at, done_at
		FROM quests WHERE account = ? ORDER BY id`, account)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []quest.Quest
	for rows.Next() {
		var q quest.Quest
		var kind, state string
		var created, done int64
		o := &q.Objective
		if err := rows.Scan(&q.ID, &q.Giver.Region.X, &q.Giver.Region.Y, &q.Giver.Index, &q.Title, &q.Story,
			&kind, &o.Species, &o.Count, &o.Target.X, &o.Target.Y, &q.Progress,
			&q.Reward.Gold, &q.Reward.XP, &q.Reward.Potions, &q.Reward.Note, &q.Tier, &state, &created, &done); err != nil {
			return nil, nil, err
		}
		q.Account, o.Kind, q.State = account, quest.Kind(kind), quest.State(state)
		q.CreatedAt = time.Unix(created, 0)
		if done != 0 {
			q.DoneAt = time.Unix(done, 0)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	rep := map[world.RegionPos]int{}
	rrows, err := s.db.Query(`SELECT region_x, region_y, points FROM reputation WHERE account = ?`, account)
	if err != nil {
		return nil, nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var rp world.RegionPos
		var pts int
		if err := rrows.Scan(&rp.X, &rp.Y, &pts); err != nil {
			return nil, nil, err
		}
		rep[rp] = pts
	}
	if len(rep) == 0 {
		rep = nil // même valeur que pour un personnage neuf : comparable
	}
	return out, rep, rrows.Err()
}

// DeedsNear implémente lore.DeedRepo.
func (s *Store) DeedsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]lore.Deed, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account, giver_rx, giver_ry, title, done_at FROM quests
		WHERE state = 'done' AND giver_rx BETWEEN ? AND ? AND giver_ry BETWEEN ? AND ? AND done_at >= ?
		ORDER BY done_at DESC LIMIT ?`,
		at.X-radius, at.X+radius, at.Y-radius, at.Y+radius, notBefore.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lore.Deed
	for rows.Next() {
		var d lore.Deed
		var done int64
		if err := rows.Scan(&d.Account, &d.Place.X, &d.Place.Y, &d.Title, &done); err != nil {
			return nil, err
		}
		d.DoneAt = time.Unix(done, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDeeds renvoie les quêtes rendues (d'un compte, ou de tous si account est vide),
// les plus récentes d'abord. Sert à l'opérateur.
func (s *Store) ListDeeds(ctx context.Context, account string, limit int) ([]lore.Deed, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account, giver_rx, giver_ry, title, done_at FROM quests
		WHERE state = 'done' AND (? = '' OR account = ?) ORDER BY done_at DESC LIMIT ?`, account, account, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lore.Deed
	for rows.Next() {
		var d lore.Deed
		var done int64
		if err := rows.Scan(&d.Account, &d.Place.X, &d.Place.Y, &d.Title, &done); err != nil {
			return nil, err
		}
		d.DoneAt = time.Unix(done, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

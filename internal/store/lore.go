package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Store implements lore.Repo.
var _ lore.Repo = (*Store)(nil)

// ErrNoAccount is returned when writing for an account that does not exist.
var ErrNoAccount = errors.New("store: unknown account")

const cardColumns = `account, region_x, region_y, alias, role, origin, goal, trait, level, boss_kills, deaths, created_at, updated_at`

func scanCard(sc interface{ Scan(...any) error }) (lore.Card, error) {
	var c lore.Card
	var created, updated int64
	err := sc.Scan(&c.Account, &c.Place.X, &c.Place.Y, &c.Alias, &c.Role, &c.Origin, &c.Goal, &c.Trait,
		&c.Level, &c.BossKills, &c.Deaths, &created, &updated)
	c.CreatedAt, c.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	return c, err
}

// UpsertCard writes a card, then drops the account's oldest ones beyond the cap.
func (s *Store) UpsertCard(ctx context.Context, c lore.Card) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO npc_cards(account, region_x, region_y, alias, role, origin, goal, trait,
			level, boss_kills, deaths, created_at, updated_at)
		SELECT name, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM accounts WHERE name = ?
		ON CONFLICT(account, region_x, region_y) DO UPDATE SET
			alias = excluded.alias, role = excluded.role, origin = excluded.origin, goal = excluded.goal,
			trait = excluded.trait, level = excluded.level, boss_kills = excluded.boss_kills,
			deaths = excluded.deaths, updated_at = excluded.updated_at`,
		c.Place.X, c.Place.Y, c.Alias, c.Role, c.Origin, c.Goal, c.Trait,
		c.Level, c.BossKills, c.Deaths, c.CreatedAt.Unix(), c.UpdatedAt.Unix(), c.Account)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoAccount
	}

	// At most MaxCardsPerAccount places per account: keep the most recent ones.
	if _, err := tx.ExecContext(ctx, `DELETE FROM npc_cards WHERE account = ?
		AND (region_x, region_y) NOT IN (
			SELECT region_x, region_y FROM npc_cards WHERE account = ? ORDER BY updated_at DESC, rowid DESC LIMIT ?)`,
		c.Account, c.Account, lore.MaxCardsPerAccount); err != nil {
		return err
	}
	return tx.Commit()
}

// GetCard returns an account's card for a place.
func (s *Store) GetCard(ctx context.Context, account string, place world.RegionPos) (lore.Card, bool, error) {
	c, err := scanCard(s.db.QueryRowContext(ctx, `SELECT `+cardColumns+` FROM npc_cards
		WHERE account = ? AND region_x = ? AND region_y = ?`, account, place.X, place.Y))
	if errors.Is(err, sql.ErrNoRows) {
		return lore.Card{}, false, nil
	}
	return c, err == nil, err
}

// CardsNear returns the cards of the square around at, updated since notBefore.
func (s *Store) CardsNear(ctx context.Context, at world.RegionPos, radius int, notBefore time.Time, limit int) ([]lore.Card, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cardColumns+` FROM npc_cards
		WHERE region_x BETWEEN ? AND ? AND region_y BETWEEN ? AND ? AND updated_at >= ?
		ORDER BY updated_at DESC LIMIT ?`,
		at.X-radius, at.X+radius, at.Y-radius, at.Y+radius, notBefore.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lore.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateFacts updates the verified facts of an account's cards, without touching their date.
func (s *Store) UpdateFacts(ctx context.Context, account string, f lore.Facts) error {
	_, err := s.db.ExecContext(ctx, `UPDATE npc_cards SET level = ?, boss_kills = ?, deaths = ? WHERE account = ?`,
		f.Level, f.BossKills, f.Deaths, account)
	return err
}

// AccountExists tells whether a name exists (case-insensitive).
func (s *Store) AccountExists(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE name = ?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// GetMemory returns an NPC's memory of a player.
func (s *Store) GetMemory(ctx context.Context, npc world.NPCKey, account string) (lore.Memory, bool, error) {
	m := lore.Memory{NPC: npc}
	var updated int64
	err := s.db.QueryRowContext(ctx, `SELECT account, summary, updated_at FROM npc_memories
		WHERE region_x = ? AND region_y = ? AND npc_index = ? AND account = ?`,
		npc.Region.X, npc.Region.Y, npc.Index, account).Scan(&m.Account, &m.Summary, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return lore.Memory{}, false, nil
	}
	m.UpdatedAt = time.Unix(updated, 0).UTC()
	return m, err == nil, err
}

// PutMemory writes (or replaces) an NPC's memory of a player.
func (s *Store) PutMemory(ctx context.Context, m lore.Memory) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO npc_memories(region_x, region_y, npc_index, account, summary, updated_at)
		SELECT ?, ?, ?, name, ?, ? FROM accounts WHERE name = ?
		ON CONFLICT(region_x, region_y, npc_index, account) DO UPDATE SET
			summary = excluded.summary, updated_at = excluded.updated_at`,
		m.NPC.Region.X, m.NPC.Region.Y, m.NPC.Index, m.Summary, m.UpdatedAt.Unix(), m.Account)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoAccount
	}
	return nil
}

// --- Operations (operator commands) ---

// ListCards returns the cards, from most recent to oldest, optionally
// limited to one account (account != "").
func (s *Store) ListCards(ctx context.Context, account string, limit int) ([]lore.Card, error) {
	q := `SELECT ` + cardColumns + ` FROM npc_cards`
	var args []any
	if account != "" {
		q += ` WHERE account = ?`
		args = append(args, account)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lore.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PurgeAccount deletes a player's cards, memories, quests and reputation.
func (s *Store) PurgeAccount(ctx context.Context, account string) (cards, memories, quests int64, err error) {
	cards, memories, quests, err = s.purge(ctx, `account = ?`, `account = ?`, `account = ?`, account)
	if err == nil { // the equipment bag goes too
		_, err = s.db.ExecContext(ctx, `DELETE FROM items WHERE account = ?`, account)
	}
	return
}

// PurgePlace deletes a place's cards, memories, handed-in quests and reputation.
func (s *Store) PurgePlace(ctx context.Context, place world.RegionPos) (cards, memories, quests int64, err error) {
	return s.purge(ctx, `region_x = ? AND region_y = ?`, `region_x = ? AND region_y = ?`,
		`giver_rx = ? AND giver_ry = ?`, place.X, place.Y)
}

func (s *Store) purge(ctx context.Context, cardWhere, memWhere, questWhere string, args ...any) (cards, memories, quests int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM npc_cards WHERE `+cardWhere, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cards: %w", err)
	}
	cards, _ = res.RowsAffected()
	res, err = tx.ExecContext(ctx, `DELETE FROM npc_memories WHERE `+memWhere, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("memories: %w", err)
	}
	memories, _ = res.RowsAffected()
	res, err = tx.ExecContext(ctx, `DELETE FROM quests WHERE `+questWhere, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("quests: %w", err)
	}
	quests, _ = res.RowsAffected()
	// Reputation follows the place or the account (same columns as the cards).
	if _, err = tx.ExecContext(ctx, `DELETE FROM reputation WHERE `+cardWhere, args...); err != nil {
		return 0, 0, 0, fmt.Errorf("reputation: %w", err)
	}
	return cards, memories, quests, tx.Commit()
}

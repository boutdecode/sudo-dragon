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

// Store implémente lore.Repo.
var _ lore.Repo = (*Store)(nil)

// ErrNoAccount est renvoyée quand on écrit pour un compte qui n'existe pas.
var ErrNoAccount = errors.New("store: compte inconnu")

const cardColumns = `account, region_x, region_y, alias, role, origin, goal, trait, level, boss_kills, deaths, created_at, updated_at`

func scanCard(sc interface{ Scan(...any) error }) (lore.Card, error) {
	var c lore.Card
	var created, updated int64
	err := sc.Scan(&c.Account, &c.Place.X, &c.Place.Y, &c.Alias, &c.Role, &c.Origin, &c.Goal, &c.Trait,
		&c.Level, &c.BossKills, &c.Deaths, &created, &updated)
	c.CreatedAt, c.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	return c, err
}

// UpsertCard écrit une fiche puis écarte les plus anciennes du compte au-delà du plafond.
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

	// Au plus MaxCardsPerAccount lieux par compte: on garde les plus récents.
	if _, err := tx.ExecContext(ctx, `DELETE FROM npc_cards WHERE account = ?
		AND (region_x, region_y) NOT IN (
			SELECT region_x, region_y FROM npc_cards WHERE account = ? ORDER BY updated_at DESC, rowid DESC LIMIT ?)`,
		c.Account, c.Account, lore.MaxCardsPerAccount); err != nil {
		return err
	}
	return tx.Commit()
}

// GetCard renvoie la fiche d'un compte dans un lieu.
func (s *Store) GetCard(ctx context.Context, account string, place world.RegionPos) (lore.Card, bool, error) {
	c, err := scanCard(s.db.QueryRowContext(ctx, `SELECT `+cardColumns+` FROM npc_cards
		WHERE account = ? AND region_x = ? AND region_y = ?`, account, place.X, place.Y))
	if errors.Is(err, sql.ErrNoRows) {
		return lore.Card{}, false, nil
	}
	return c, err == nil, err
}

// CardsNear renvoie les fiches du carré autour de at, mises à jour depuis notBefore.
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

// UpdateFacts met à jour les faits vérifiés des fiches d'un compte, sans toucher à leur date.
func (s *Store) UpdateFacts(ctx context.Context, account string, f lore.Facts) error {
	_, err := s.db.ExecContext(ctx, `UPDATE npc_cards SET level = ?, boss_kills = ?, deaths = ? WHERE account = ?`,
		f.Level, f.BossKills, f.Deaths, account)
	return err
}

// AccountExists dit si un pseudo existe (insensible à la casse).
func (s *Store) AccountExists(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE name = ?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// GetMemory renvoie le souvenir d'un PNJ sur un joueur.
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

// PutMemory écrit (ou remplace) le souvenir d'un PNJ sur un joueur.
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

// --- Exploitation (commandes de l'opérateur) ---

// ListCards renvoie les fiches, de la plus récente à la plus ancienne, éventuellement
// limitées à un compte (account != "").
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

// PurgeAccount supprime les fiches, les souvenirs, les quêtes et la réputation d'un joueur.
func (s *Store) PurgeAccount(ctx context.Context, account string) (cards, memories, quests int64, err error) {
	cards, memories, quests, err = s.purge(ctx, `account = ?`, `account = ?`, `account = ?`, account)
	if err == nil { // le sac d'équipement part aussi
		_, err = s.db.ExecContext(ctx, `DELETE FROM items WHERE account = ?`, account)
	}
	return
}

// PurgePlace supprime les fiches, les souvenirs, les quêtes rendues et la réputation d'un lieu.
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
		return 0, 0, 0, fmt.Errorf("fiches: %w", err)
	}
	cards, _ = res.RowsAffected()
	res, err = tx.ExecContext(ctx, `DELETE FROM npc_memories WHERE `+memWhere, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("souvenirs: %w", err)
	}
	memories, _ = res.RowsAffected()
	res, err = tx.ExecContext(ctx, `DELETE FROM quests WHERE `+questWhere, args...)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("quêtes: %w", err)
	}
	quests, _ = res.RowsAffected()
	// La réputation suit le lieu ou le compte (mêmes colonnes que les fiches).
	if _, err = tx.ExecContext(ctx, `DELETE FROM reputation WHERE `+cardWhere, args...); err != nil {
		return 0, 0, 0, fmt.Errorf("réputation: %w", err)
	}
	return cards, memories, quests, tx.Commit()
}

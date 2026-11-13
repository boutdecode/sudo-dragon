package store

import (
	"database/sql"

	"github.com/kevin/ascii-adventure/internal/world"
)

// maxStoredDiscoveries bounds the dungeons remembered per account (there is about one per five regions).
const maxStoredDiscoveries = 2000

// saveDiscoveries replaces the dungeon entries discovered by an account (inside the character
// save transaction). A dungeon is identified by the region that hides it.
func saveDiscoveries(tx *sql.Tx, account string, c Character) error {
	if _, err := tx.Exec(`DELETE FROM discoveries WHERE account = ?`, account); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO discoveries(account, region_x, region_y)
		SELECT name, ?, ? FROM accounts WHERE name = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, rp := range c.Discoveries {
		if i >= maxStoredDiscoveries {
			break
		}
		if _, err := stmt.Exec(rp.X, rp.Y, account); err != nil {
			return err
		}
	}
	return nil
}

// loadDiscoveries reads back the dungeons discovered by an account (in a stable order).
func (s *Store) loadDiscoveries(account string) ([]world.RegionPos, error) {
	rows, err := s.db.Query(`SELECT region_x, region_y FROM discoveries WHERE account = ? ORDER BY region_y, region_x`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []world.RegionPos
	for rows.Next() {
		var rp world.RegionPos
		if err := rows.Scan(&rp.X, &rp.Y); err != nil {
			return nil, err
		}
		out = append(out, rp)
	}
	return out, rows.Err()
}

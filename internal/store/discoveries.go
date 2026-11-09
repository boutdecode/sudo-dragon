package store

import (
	"database/sql"

	"github.com/kevin/ascii-adventure/internal/world"
)

// maxStoredDiscoveries borne les donjons mémorisés par compte (il y en a environ un par cinq régions).
const maxStoredDiscoveries = 2000

// saveDiscoveries remplace les entrées de donjon découvertes par un compte (dans la transaction de
// sauvegarde du personnage). Un donjon est désigné par la région qui le cache.
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

// loadDiscoveries relit les donjons découverts par un compte (dans un ordre stable).
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

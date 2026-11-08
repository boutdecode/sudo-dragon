package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/world"
)

// saveGear remplace le sac d'un compte (dans la transaction de sauvegarde du personnage).
func saveGear(tx *sql.Tx, account string, inv gear.Inventory) error {
	if _, err := tx.Exec(`DELETE FROM items WHERE account = ?`, account); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO items(account, id, slot, name, descr, tier, atk, def, price,
			origin_rx, origin_ry, origin_idx, worn)
		SELECT name, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM accounts WHERE name = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, it := range inv.Items {
		if i >= gear.BagCapacity {
			break
		}
		worn := 0
		if inv.IsWorn(it.ID) {
			worn = 1
		}
		if _, err := stmt.Exec(it.ID, string(it.Slot), it.Name, it.Desc, it.Tier, it.Atk, it.Def, it.Price,
			it.Origin.Region.X, it.Origin.Region.Y, it.Origin.Index, worn, account); err != nil {
			return err
		}
	}
	return nil
}

// loadGear relit le sac d'un compte ; les valeurs lues sont revalidées par gear.SanitizeInventory.
func (s *Store) loadGear(account string) (gear.Inventory, error) {
	rows, err := s.db.Query(`SELECT id, slot, name, descr, tier, atk, def, price, origin_rx, origin_ry, origin_idx, worn
		FROM items WHERE account = ? ORDER BY id`, account)
	if err != nil {
		return gear.Inventory{}, err
	}
	defer rows.Close()
	var inv gear.Inventory
	for rows.Next() {
		var it gear.Item
		var slot string
		var worn int
		if err := rows.Scan(&it.ID, &slot, &it.Name, &it.Desc, &it.Tier, &it.Atk, &it.Def, &it.Price,
			&it.Origin.Region.X, &it.Origin.Region.Y, &it.Origin.Index, &worn); err != nil {
			return gear.Inventory{}, err
		}
		it.Slot = gear.Slot(slot)
		inv.Items = append(inv.Items, it)
		if worn != 0 {
			if inv.Worn == nil {
				inv.Worn = map[gear.Slot]int64{}
			}
			inv.Worn[it.Slot] = it.ID
		}
	}
	if err := rows.Err(); err != nil {
		return gear.Inventory{}, err
	}
	return gear.SanitizeInventory(inv), nil
}

// ---------------------------------------------------------------- assortiments des vendeurs

// GetWares renvoie l'assortiment enregistré d'un vendeur.
func (s *Store) GetWares(ctx context.Context, npc world.NPCKey) (gear.Wares, bool, error) {
	var w gear.Wares
	var raw string
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT specialty, items, version, created_at FROM npc_wares
		WHERE region_x = ? AND region_y = ? AND npc_index = ?`, npc.Region.X, npc.Region.Y, npc.Index).
		Scan(&w.Specialty, &raw, &w.Version, &created)
	if err == sql.ErrNoRows {
		return gear.Wares{}, false, nil
	}
	if err != nil {
		return gear.Wares{}, false, err
	}
	if err := json.Unmarshal([]byte(raw), &w.Items); err != nil {
		return gear.Wares{}, false, fmt.Errorf("assortiment illisible: %w", err)
	}
	w.NPC, w.CreatedAt = npc, time.Unix(created, 0)
	return w, true, nil
}

// PutWares enregistre (ou remplace) l'assortiment d'un vendeur.
func (s *Store) PutWares(ctx context.Context, w gear.Wares) error {
	raw, err := json.Marshal(w.Items)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO npc_wares(region_x, region_y, npc_index, specialty, items, version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(region_x, region_y, npc_index) DO UPDATE SET specialty = excluded.specialty,
			items = excluded.items, version = excluded.version, created_at = excluded.created_at`,
		w.NPC.Region.X, w.NPC.Region.Y, w.NPC.Index, w.Specialty, string(raw), w.Version, w.CreatedAt.Unix())
	return err
}

// DeleteWares efface l'assortiment d'un vendeur (il sera régénéré à la prochaine visite).
func (s *Store) DeleteWares(ctx context.Context, npc world.NPCKey) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM npc_wares WHERE region_x = ? AND region_y = ? AND npc_index = ?`,
		npc.Region.X, npc.Region.Y, npc.Index)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListWares renvoie les assortiments enregistrés (pour l'opérateur).
func (s *Store) ListWares(ctx context.Context) ([]gear.Wares, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT region_x, region_y, npc_index FROM npc_wares ORDER BY region_x, region_y, npc_index`)
	if err != nil {
		return nil, err
	}
	var keys []world.NPCKey
	for rows.Next() {
		var k world.NPCKey
		if err := rows.Scan(&k.Region.X, &k.Region.Y, &k.Index); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	var out []gear.Wares
	for _, k := range keys {
		if w, ok, err := s.GetWares(ctx, k); err == nil && ok {
			out = append(out, w)
		}
	}
	return out, nil
}

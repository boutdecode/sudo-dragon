package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Treasure maps: a map remembers the coordinates of a tile to dig (see world.DigSpot). They are found in
// the chests of ruins and lairs, or on monsters. Arriving exactly on the tile unearths it: gold,
// potions and a piece of gear of the region's rank + 1. Maps belong to the character (saved with them),
// they are not bag items, the bag only holding equipment.

const (
	maxMaps        = 5    // maximum number of maps held
	mapRegionReach = 2    // the tile is in a region at most 2 regions from the source
	mapMinDistance = 40   // ... and at least 40 tiles from the source
	mapChestRuin   = 0.6  // share of ruin chests that contain a map
	mapChestLair   = 0.25 // same for a lair chest
	mapDropMonster = 0.02 // share of surface monsters that drop one
	mapDropElite   = 0.06 // same for a knight or a boss
	digGold        = 25   // minimum gold per region level; the maximum is double
)

// giveMap draws a new tile to dig near from and adds it to p's maps. Returns the journal line
// that announces it, or "" if there was none (too many maps, no valid tile).
func (g *Game) giveMap(p *player, from world.Pos) string {
	if len(p.maps) >= maxMaps {
		return ""
	}
	src := world.RegionOf(from.X, from.Y)
	for range 8 {
		rp := world.RegionPos{X: src.X + g.rng.IntN(2*mapRegionReach+1) - mapRegionReach, Y: src.Y + g.rng.IntN(2*mapRegionReach+1) - mapRegionReach}
		spot, ok := g.World.DigSpot(rp, g.rng.Uint64())
		if !ok || max(abs(spot.X-from.X), abs(spot.Y-from.Y)) < mapMinDistance || slices.Contains(p.maps, spot) {
			continue
		}
		p.maps = append(p.maps, spot)
		g.save(p)
		return i18n.T(p.lang, "poi.map.found", spot.X, spot.Y, spot.X, spot.Y)
	}
	return ""
}

// monsterMap draws the treasure map that m may leave to p. Only surface monsters drop them.
func (g *Game) monsterMap(p *player, m *monster) string {
	if m.zone != 0 || m.sp.Dungeon {
		return ""
	}
	chance := mapDropMonster
	if m.sp.Boss || m.sp.Kind == "knight" {
		chance = mapDropElite
	}
	if g.rng.Float64() >= chance {
		return ""
	}
	return g.giveMap(p, m.pos)
}

// dig unearths the treasure if p has just arrived on the tile of one of their maps. A full bag leaves the map: one
// can come back.
func (g *Game) dig(p *player, pos world.Pos) {
	i := slices.Index(p.maps, pos)
	if i < 0 {
		return
	}
	level := world.LevelAt(pos.X, pos.Y)
	if len(p.inv.Items) >= gear.BagCapacity {
		g.poiNotice(p, "poi.dig.bag_full")
		return
	}
	p.maps = slices.Delete(p.maps, i, i+1)
	it := gear.MonsterLoot(p.lang, world.RegionOf(pos.X, pos.Y), gear.TierForLevel(level)+1, g.rng.Uint64())
	added, _ := p.inv.Add(it)
	p.gearDirty = true
	gold := p.stats.AddGold(digGold*level + g.rng.IntN(digGold*level+1))
	potions := p.stats.AddPotions(1 + g.rng.IntN(2))
	p.gearNotices = append(p.gearNotices,
		i18n.T(p.lang, "poi.dig.found"),
		i18n.T(p.lang, "dungeon.chest.open", gold, potions),
		i18n.T(p.lang, "dungeon.chest.item", added.Name))
	g.sendBag(p)
	g.save(p)
}

package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Cartes au trésor : une carte retient les coordonnées d'une case à creuser (voir world.DigSpot). On les trouve dans
// les coffres des ruines et des repaires, ou sur des monstres. Arriver exactement sur la case la déterre : or,
// potions et un équipement du rang de la région + 1. Les cartes appartiennent au personnage (sauvegardées avec lui),
// elles ne sont pas des objets du sac, qui ne contient que de l'équipement.

const (
	maxMaps        = 5    // cartes détenues au plus
	mapRegionReach = 2    // la case est dans une région à 2 régions au plus de la source
	mapMinDistance = 40   // ... et à 40 cases au moins de la source
	mapChestRuin   = 0.6  // part des coffres de ruine qui contiennent une carte
	mapChestLair   = 0.25 // idem pour un coffre de repaire
	mapDropMonster = 0.02 // part des monstres de surface qui en lâchent une
	mapDropElite   = 0.06 // idem pour un chevalier ou un boss
	digGold        = 25   // or minimal par niveau de région ; le maximum est le double
)

// giveMap tire une nouvelle case à creuser près de from et l'ajoute aux cartes de p. Renvoie la ligne de journal
// qui l'annonce, ou "" s'il n'y en a pas eu (trop de cartes, pas de case valable).
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

// monsterMap tire la carte au trésor que m laisse peut-être à p. Seuls les monstres de la surface en lâchent.
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

// dig déterre le trésor si p vient d'arriver sur la case d'une de ses cartes. Un sac plein laisse la carte : on
// peut revenir.
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

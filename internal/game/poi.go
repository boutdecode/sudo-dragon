package game

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Points of interest on the main map (see world/poi.go): you interact by walking onto their tile.
//
//   - campfire: heals half and becomes the respawn point after a defeat;
//   - shrine: temporary buff, depending on the region's climate;
//   - lair: three monsters guard a chest;
//   - watchtower: reveals nearby dungeon entrances (the client also reveals the map);
//   - standing stone: activates on first visit; two activated stones link up (/menhir).
//
// The state that changes (cooldowns, buffs, lair chests) stays in memory; only campfires and activated
// stones are saved with the character.

const (
	campfireCooldown = 3000 // ticks (5 min) between two rests at the same fire
	shrineCooldown   = 6000 // ticks (10 min) between two blessings from the same shrine
	shrineDuration   = 3000 // ticks (5 min) of buff
	springCooldown   = 1200 // ticks (2 min) between two sips from the same spring
	towerRadius      = 12   // radius (in tiles) within which a tower reveals dungeon entrances

	shrineDefBonus = 2 // DEF
	shrineHitBonus = 2 // to-hit

	lairRespawn    = 18000 // ticks (30 min) before the guardians return and the chest refills
	lairLeash      = 6     // maximum distance a guardian strays from its post
	lairLevelBonus = 2     // extra region levels for the guardians (a lair is a challenge)
	ruinLevelBonus = 3     // same for a ruin's guardian
	lairGearChance = 0.5   // share of lair chests that contain an item
	ruinGearChance = 0.8   // same for a ruin
	lairSlotStride = 10    // gap between slot numbers of two lairs in the same region
	lairSlotBase   = 100   // a lair's slots are numbered from here (a fortress's, from 0)
	lairGold       = 12    // minimum gold per chest level; the maximum is double

	stoneMinCost   = 20 // gold for the shortest crossing
	stoneRegionFee = 15 // gold per region of distance
	maxStones      = 500
)

// buffKind is the kind of a shrine buff.
type buffKind uint8

const (
	buffDef   buffKind = iota + 1 // + DEF
	buffHit                       // + to-hit
	buffXP                        // XP ×1,5
	buffRegen                     // 1 extra HP per second out of combat
)

// buff is a buff active until tick until.
type buff struct {
	kind  buffKind
	until uint64
}

// shrineBuff gives a shrine's buff depending on its region's climate.
func shrineBuff(c world.Climate) buffKind {
	switch c {
	case world.Tropical:
		return buffRegen
	case world.Arid:
		return buffHit
	case world.Boreal:
		return buffXP
	}
	return buffDef
}

// info describes the buff for the character sheet, with the time remaining at instant now.
func (b buff) info(now uint64) protocol.Blessing {
	bl := protocol.Blessing{Secs: int((b.until - min(now, b.until) + 9) / 10)} // 10 ticks per second
	switch b.kind {
	case buffDef:
		bl.Kind, bl.Bonus = protocol.BlessDef, shrineDefBonus
	case buffHit:
		bl.Kind, bl.Bonus = protocol.BlessHit, shrineHitBonus
	case buffXP:
		bl.Kind, bl.Bonus = protocol.BlessXP, 50
	default:
		bl.Kind, bl.Bonus = protocol.BlessRegen, 1
	}
	return bl
}

func (p *player) hasBuff(k buffKind) bool {
	for _, b := range p.buffs {
		if b.kind == k {
			return true
		}
	}
	return false
}

// expireBuffs removes expired buffs (fighter() never looks at the clock) and says whether there were any.
func (p *player) expireBuffs(now uint64) bool {
	if len(p.buffs) == 0 {
		return false
	}
	n := len(p.buffs)
	p.buffs = slices.DeleteFunc(p.buffs, func(b buff) bool { return now >= b.until })
	return len(p.buffs) != n
}

// stoneList returns the regions of the activated stones, in a stable order (Y then X).
func (p *player) stoneList() []world.RegionPos {
	out := make([]world.RegionPos, 0, len(p.stones))
	for rp := range p.stones {
		out = append(out, rp)
	}
	slices.SortFunc(out, func(a, b world.RegionPos) int { return cmp.Or(a.Y-b.Y, a.X-b.X) })
	return out
}

// poiNotice shows a message to the player (same channel as chests).
func (g *Game) poiNotice(p *player, key string, args ...any) {
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, key, args...))
	g.sendBag(p)
}

// ticksToMinutes rounds up to the minute (one minute = 600 ticks).
func ticksToMinutes(t uint64) int { return int((t + 599) / 600) }

// cooling says whether point k still has no effect for p. Otherwise, it arms the delay d.
func (g *Game) cooling(p *player, k world.POIKey, d uint64) (left uint64, cooling bool) {
	if until := p.poiCooling[k]; g.tick < until {
		return until - g.tick, true
	}
	if len(p.poiCooling) > 256 {
		clear(p.poiCooling)
	}
	p.poiCooling[k] = g.tick + d
	return 0, false
}

// poiStep applies the effect of the point of interest p has just arrived on, if any.
func (g *Game) poiStep(p *player, pos world.Pos) {
	if len(p.maps) > 0 {
		g.dig(p, pos) // a treasure map tile is an ordinary tile
	}
	poi := g.World.POIAt(pos.X, pos.Y)
	if poi == nil {
		return
	}
	switch poi.Kind {
	case world.POICampfire:
		g.campfire(p, poi)
	case world.POIShrine:
		g.shrine(p, poi)
	case world.POILair, world.POIRuin:
		g.openLairChest(p, poi)
	case world.POITower:
		g.tower(p, poi)
	case world.POIStone:
		g.stone(p, poi)
	case world.POISpring:
		g.spring(p, poi)
	}
}

func (g *Game) campfire(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if p.respawn == nil || *p.respawn != poi.Pos {
		at := poi.Pos
		p.respawn = &at
		g.poiNotice(p, "poi.camp.set", name)
		g.save(p)
	}
	if _, wait := g.cooling(p, poi.Key(), campfireCooldown); wait {
		return
	}
	hp := min(p.stats.MaxHP-p.hp, (p.stats.MaxHP+1)/2)
	mana := min(p.stats.MaxMana-p.mana, (p.stats.MaxMana+1)/2)
	if hp <= 0 && mana <= 0 {
		delete(p.poiCooling, poi.Key()) // nothing to heal: the fire stays available
		return
	}
	p.hp += hp
	p.mana += mana
	g.poiNotice(p, "poi.camp.rest", hp, mana)
}

// respawnSpot returns the tile where p respawns after a defeat and whether it is a campfire.
func (g *Game) respawnSpot(p *player) (world.Pos, bool) {
	if p.respawn != nil {
		return g.freeNear(*p.respawn), true
	}
	return g.freeSpawn(), false
}

// spring heals 30% of HP and 50% of mana (an oasis, in arid climate: 60% and all the mana). The delay only runs
// if one drank: nothing is lost by passing at full health.
func (g *Game) spring(p *player, poi *world.POI) {
	hpFrac, manaFrac, key := 0.3, 0.5, "poi.spring.drink"
	if g.World.ClimateAt(poi.Pos.X, poi.Pos.Y) == world.Arid {
		hpFrac, manaFrac, key = 0.6, 1.0, "poi.spring.oasis"
	}
	hp := min(p.stats.MaxHP-p.hp, int(float64(p.stats.MaxHP)*hpFrac+0.5))
	mana := min(p.stats.MaxMana-p.mana, int(float64(p.stats.MaxMana)*manaFrac+0.5))
	if hp <= 0 && mana <= 0 {
		return
	}
	if _, wait := g.cooling(p, poi.Key(), springCooldown); wait {
		return
	}
	p.hp += hp
	p.mana += mana
	g.poiNotice(p, key, poi.NameIn(p.lang), hp, mana)
}

func (g *Game) shrine(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if left, wait := g.cooling(p, poi.Key(), shrineCooldown); wait {
		g.poiNotice(p, "poi.shrine.cooling", name, ticksToMinutes(left))
		return
	}
	kind := shrineBuff(g.World.ClimateAt(poi.Pos.X, poi.Pos.Y))
	p.buffs = slices.DeleteFunc(p.buffs, func(b buff) bool { return b.kind == kind })
	p.buffs = append(p.buffs, buff{kind: kind, until: g.tick + shrineDuration})
	g.poiNotice(p, fmt.Sprintf("poi.shrine.buff.%d", kind), name, shrineDuration/600)
}

// applyBuffs adds shrine buffs to a combat sheet.
func (p *player) applyBuffs(def, hit *int) {
	if p.hasBuff(buffDef) {
		*def += shrineDefBonus
	}
	if p.hasBuff(buffHit) {
		*hit += shrineHitBonus
	}
}

// xpWithBuff applies a shrine's XP bonus.
func (p *player) xpWithBuff(xp int) int {
	if p.hasBuff(buffXP) {
		return xp * 3 / 2
	}
	return xp
}

// regenBuffs gives 1 extra HP per second to players with the regeneration blessing.
func (g *Game) regenBuffs(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.fight == nil && p.hp < p.stats.MaxHP && p.hasBuff(buffRegen) {
			p.hp++
		}
	}
}

func (g *Game) tower(p *player, poi *world.POI) {
	g.poiNotice(p, "poi.tower.climb", poi.NameIn(p.lang))
	if !g.Dungeons {
		return
	}
	for _, d := range g.World.DungeonsWithin(poi.Pos.X, poi.Pos.Y, towerRadius) {
		g.discover(p, d)
	}
}

// --- Repaires ---

// lairKey is the key of slot i of lair poi.
func lairKey(poi *world.POI, i int) world.SlotKey {
	return world.SlotKey{Region: poi.Region, Index: lairSlotBase + poi.Index*lairSlotStride + i}
}

// activateLairs populates the lairs near a player, like activateGarrisons for fortresses.
func (g *Game) activateLairs() {
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		for _, poi := range g.World.POIsWithin(p.pos.X, p.pos.Y, garrisonActivation) {
			if poi.Kind != world.POILair && poi.Kind != world.POIRuin {
				continue
			}
			for i, pos := range poi.Slots {
				g.fillLairSlot(poi, i, pos)
			}
		}
	}
}

func (g *Game) fillLairSlot(poi *world.POI, i int, pos world.Pos) {
	key := lairKey(poi, i)
	if _, alive := g.slotMonster[key]; alive {
		return
	}
	if at, beaten := g.slotDefeated[key]; beaten {
		if g.tick-at < lairRespawn {
			return
		}
		delete(g.slotDefeated, key)
	}
	if g.blocked(pos) {
		return
	}
	// The guardians are biome beasts, a bit stronger than the region.
	list := speciesByTile[g.World.TileAt(pos.X, pos.Y)]
	if len(list) == 0 {
		list = speciesByTile[world.Plain]
	}
	sp := list[int((uint64(poi.Seed)>>8+uint64(i))%uint64(len(list)))]
	bonus := lairLevelBonus
	if poi.Kind == world.POIRuin {
		bonus = ruinLevelBonus // a ruin's guardian is an elite
	}
	level := min(world.LevelAt(pos.X, pos.Y)+bonus, 20)
	m := g.addMonster(scaledForLevel(sp, level), pos)
	m.slot = &key
	m.leash = lairLeash
	g.slotMonster[key] = m.id
	g.garrisonCount++
}

// lairGuarded says whether a lair guardian is still alive.
func (g *Game) lairGuarded(poi *world.POI) bool {
	for i := range poi.Slots {
		if _, alive := g.slotMonster[lairKey(poi, i)]; alive {
			return true
		}
	}
	return false
}

// openLairChest opens the chest of a lair or ruin whose guardians have fallen. The chest refills
// lairRespawn ticks after being opened; a full bag leaves it closed. A ruin also gives a lore fragment
// and, with dungeons, the entrance of the nearest one.
func (g *Game) openLairChest(p *player, poi *world.POI) {
	ruin := poi.Kind == world.POIRuin
	prefix := "poi.lair"
	if ruin {
		prefix = "poi.ruin"
	}
	if g.lairGuarded(poi) {
		g.poiNotice(p, prefix+".guarded")
		return
	}
	if at, opened := g.lairOpened[poi.Key()]; opened && g.tick-at < lairRespawn {
		g.poiNotice(p, prefix+".empty")
		return
	}
	level := world.LevelAt(poi.Pos.X, poi.Pos.Y)
	gearChance, goldBase := lairGearChance, lairGold*level
	if ruin {
		gearChance, goldBase = ruinGearChance, lairGold*level*3/2
	}
	var loot *gear.Item
	if g.rng.Float64() < gearChance {
		if len(p.inv.Items) >= gear.BagCapacity {
			g.poiNotice(p, "dungeon.chest.bag_full")
			return
		}
		it := gear.MonsterLoot(p.lang, poi.Region, gear.TierForLevel(level)+1, g.rng.Uint64())
		loot = &it
	}
	g.lairOpened[poi.Key()] = g.tick

	gold := p.stats.AddGold(goldBase + g.rng.IntN(goldBase+1))
	potions := p.stats.AddPotions(1 + g.rng.IntN(2))
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.open", gold, potions))
	if loot != nil {
		added, _ := p.inv.Add(*loot)
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.item", added.Name))
		p.gearDirty = true
	}
	if ruin {
		g.ruinLore(p, poi)
	}
	chance := mapChestLair
	if ruin {
		chance = mapChestRuin
	}
	if g.rng.Float64() < chance {
		if line := g.giveMap(p, poi.Pos); line != "" {
			p.gearNotices = append(p.gearNotices, line)
		}
	}
	g.sendBag(p)
	g.save(p)
}

// ruinLore adds the ruin's lore fragment to the journal (static text drawn from its seed) and, if the player does not
// yet know it, reveals the nearest dungeon entrance (3 regions at most).
func (g *Game) ruinLore(p *player, poi *world.POI) {
	p.gearNotices = append(p.gearNotices, i18n.Pick(p.lang, "poi.lore", int(uint64(poi.Seed)>>8)))
	if !g.Dungeons {
		return
	}
	var best *world.Dungeon
	bestDist := 0
	for _, d := range g.World.DungeonsNear(poi.Pos.X, poi.Pos.Y, 3) {
		if dist := max(abs(d.Entrance.X-poi.Pos.X), abs(d.Entrance.Y-poi.Pos.Y)); best == nil || dist < bestDist {
			best, bestDist = d, dist
		}
	}
	if best == nil || p.discovered[best.Region] {
		return
	}
	g.discover(p, best)
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "poi.ruin.hint", best.NameIn(p.lang)))
}

// --- Standing stones ---

func (g *Game) stone(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if p.stones[poi.Region] {
		g.poiNotice(p, "poi.stone.known", name)
		return
	}
	if len(p.stones) >= maxStones {
		return
	}
	p.stones[poi.Region] = true
	g.poiNotice(p, "poi.stone.activated", name)
	g.save(p)
}

// stoneCost is the price of the trip between two stones: proportional to the distance in regions.
func stoneCost(a, b world.RegionPos) int {
	return max(stoneMinCost, stoneRegionFee*max(abs(a.X-b.X), abs(a.Y-b.Y)))
}

// Menhir handles the /menhir command: with no argument, lists the activated stones with the trip price; with a
// number or the start of a name, takes the player there, who must stand on an activated stone. Returns the text to
// show them, in their language.
func (g *Game) Menhir(id int, arg string) (string, error) {
	reply := make(chan string, 1)
	if !g.do(func() { reply <- g.menhir(id, arg) }) {
		return "", ErrStopped
	}
	select {
	case s := <-reply:
		return s, nil
	case <-g.done:
		return "", ErrStopped
	}
}

func (g *Game) menhir(id int, arg string) string {
	p, ok := g.players[id]
	if !ok {
		return ""
	}
	t := func(key string, args ...any) string { return i18n.T(p.lang, key, args...) }
	here := g.World.POIAt(p.pos.X, p.pos.Y)
	if p.zone != 0 || here == nil || here.Kind != world.POIStone || !p.stones[here.Region] {
		return t("poi.menhir.not_on_stone")
	}
	type dest struct {
		poi  *world.POI
		cost int
	}
	var dests []dest
	for _, rp := range p.stoneList() {
		if rp == here.Region {
			continue
		}
		if o := g.World.POIOfRegion(rp); o != nil && o.Kind == world.POIStone {
			dests = append(dests, dest{o, stoneCost(here.Region, rp)})
		}
	}
	slices.SortStableFunc(dests, func(a, b dest) int { return a.cost - b.cost })
	if len(dests) == 0 {
		return t("poi.menhir.none")
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		var b strings.Builder
		b.WriteString(t("poi.menhir.list"))
		for i, d := range dests {
			fmt.Fprintf(&b, "\n %d. %s (%d, %d) · %d %s", i+1, d.poi.NameIn(p.lang), d.poi.Pos.X, d.poi.Pos.Y, d.cost, t("poi.menhir.gold"))
		}
		return b.String()
	}
	var pick *dest
	if n, err := strconv.Atoi(arg); err == nil {
		if n >= 1 && n <= len(dests) {
			pick = &dests[n-1]
		}
	} else {
		low := strings.ToLower(arg)
		for i := range dests {
			if strings.Contains(strings.ToLower(dests[i].poi.NameIn(p.lang)), low) {
				if pick != nil {
					return t("poi.menhir.ambiguous", arg)
				}
				pick = &dests[i]
			}
		}
	}
	if pick == nil {
		return t("poi.menhir.unknown", arg)
	}
	if p.fight != nil || p.talk != nil {
		return t("poi.menhir.busy")
	}
	if !p.stats.SpendGold(pick.cost) {
		return t("poi.menhir.poor", pick.cost)
	}
	g.setPos(p, g.freeNear(pick.poi.Pos))
	g.save(p)
	return t("poi.menhir.done", pick.poi.NameIn(p.lang), pick.cost)
}

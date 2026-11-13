package game

import (
	"cmp"
	"slices"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Dungeons: separate zones, instantiated for a group of players (see world/dungeon.go for
// generation). The main map is zone 0; each instance has its own identifier, its layout,
// and its own player and monster indexes: a dungeon's coordinates are local to its
// layout, so two instances or the main map can overlap without ever colliding.

// ZoneID identifies a zone: 0 for the main map, otherwise a dungeon instance.
type ZoneID int

// instance is the state of a dungeon: its living monsters, its opened chests. A dungeon has only one instance, shared
// by all players, which keeps its state from one visit to the next (in memory: a server restart makes it
// new again). It starts afresh when its boss dies: see bossSlain.
type instance struct {
	id   ZoneID
	d    *world.Dungeon
	dmap *world.DungeonMap

	byPos     map[world.Pos]int // players
	monsterAt map[world.Pos]int

	opened   map[world.Pos]bool // chests already opened
	bossDown bool               // the boss is defeated: its chest is no longer locked, and the instance is "exhausted"
	resetAt  uint64             // tick from which an exhausted instance can be replaced by a fresh dungeon (bossDown only)
}

// dungeonResetTicks is the delay, after the boss dies, before the dungeon can start afresh: one game day,
// so that the same dungeons cannot be chained.
const dungeonResetTicks = 24 * protocol.TicksPerGameHour

// resettable says whether the exhausted instance can be replaced by a fresh dungeon.
func (g *Game) resettable(inst *instance) bool { return inst.bossDown && g.tick >= inst.resetAt }

// coolingDown says whether the boss of dungeon d was defeated less than one game day ago.
func (g *Game) coolingDown(d *world.Dungeon) bool {
	inst := g.instances[g.dungeonZone[d.ID]]
	return inst != nil && inst.bossDown && !g.resettable(inst)
}

// playerIndex returns the player index of zone z (the main map, or an instance).
func (g *Game) playerIndex(z ZoneID) map[world.Pos]int {
	if z == 0 {
		return g.byPos
	}
	if inst := g.instances[z]; inst != nil {
		return inst.byPos
	}
	return map[world.Pos]int{} // zone is gone: a throwaway index avoids polluting the others
}

// monsterIndex is playerIndex for monsters.
func (g *Game) monsterIndex(z ZoneID) map[world.Pos]int {
	if z == 0 {
		return g.monsterAt
	}
	if inst := g.instances[z]; inst != nil {
		return inst.monsterAt
	}
	return map[world.Pos]int{}
}

// surfacePos is the player's position on the main map: their own, or the entrance of the dungeon
// they are in. This is the one that gets saved.
func (p *player) surfacePos() world.Pos {
	if p.zone != 0 {
		return p.ret
	}
	return p.pos
}

// discoverRadius is the distance (in tiles) at which a dungeon's secret entrance is noticed in passing.
const discoverRadius = 3

// discoveredList returns the regions of the discovered dungeons, in a stable order (Y then X).
func (p *player) discoveredList() []world.RegionPos {
	out := make([]world.RegionPos, 0, len(p.discovered))
	for rp := range p.discovered {
		out = append(out, rp)
	}
	slices.SortFunc(out, func(a, b world.RegionPos) int { return cmp.Or(a.Y-b.Y, a.X-b.X) })
	return out
}

// noticeDungeons discovers the dungeons whose entrance is within range of p, without telling anyone else.
func (g *Game) noticeDungeons(p *player) {
	if !g.Dungeons {
		return
	}
	for _, d := range g.World.DungeonsWithin(p.pos.X, p.pos.Y, discoverRadius) {
		g.discover(p, d)
	}
}

// discover records that p knows the entrance of d, informs them and saves. No effect if they already knew it.
func (g *Game) discover(p *player, d *world.Dungeon) {
	if p.discovered[d.Region] {
		return
	}
	p.discovered[d.Region] = true
	p.discoveredDirty = true
	g.sendDungeons(p, d.ID)
	g.save(p)
}

// sendDungeons sends p the list of dungeons they have discovered; newID, if non-empty, is the one just found.
func (g *Game) sendDungeons(p *player, newID string) {
	if p.sendDungeonsMsg == nil {
		return
	}
	msg := protocol.Dungeons{List: []protocol.DungeonInfo{}, New: newID}
	for _, rp := range p.discoveredList() {
		if d := g.World.DungeonOfRegion(rp); d != nil {
			msg.List = append(msg.List, protocol.DungeonInfo{ID: d.ID, Name: d.NameIn(p.lang), X: d.Entrance.X, Y: d.Entrance.Y, Tier: d.Tier})
		}
	}
	p.sendDungeonsMsg(msg)
}

// enterDungeon makes p enter dungeon d, with the free players around them (same radius as combat
// attraction). All find the dungeon instance in its current state, or open a new one if there is
// none (first visitor, or boss defeated since).
func (g *Game) enterDungeon(p *player, d *world.Dungeon) {
	party := []*player{p}
	for _, id := range g.sortedIDs() {
		o := g.players[id]
		if o != p && o.zone == 0 && o.fight == nil && o.talk == nil && cheb(o.pos, p.pos) <= groupRadius {
			party = append(party, o)
		}
	}
	inst := g.instanceOf(d)
	for _, m := range party {
		g.discover(m, d) // the way is known once taken
		m.ret = d.Entrance
		g.teleport(m, inst.id, inst.dmap.Spawn, 0)
	}
}

// leaveDungeon brings p back in front of the dungeon entrance, on the main map.
func (g *Game) leaveDungeon(p *player) {
	inst := g.instances[p.zone]
	g.teleport(p, 0, p.ret, 1) // not on the entrance itself, which would make them re-enter on leaving
	if inst != nil {
		g.closeIfEmpty(inst)
	}
}

// teleport places p in zone z, on the free tile nearest to c at least minR away.
func (g *Game) teleport(p *player, z ZoneID, c world.Pos, minR int) {
	at := g.playerIndex(p.zone)
	if at[p.pos] == p.id {
		delete(at, p.pos)
	}
	p.zone = z
	p.hasPending = false
	p.lastSent = nil // the client must receive a fresh view
	g.setPos(p, g.freeNearIn(z, c, minR))
	g.sendZone(p)
}

// sendZone tells the client which map it is in. Called before the snapshot that follows a zone change.
func (g *Game) sendZone(p *player) {
	if p.sendZoneMsg == nil {
		return
	}
	z := protocol.Zone{}
	if inst := g.instances[p.zone]; inst != nil {
		z = protocol.Zone{Dungeon: inst.d.ID, Name: inst.d.NameIn(p.lang), Seed: inst.d.Seed, Tier: inst.d.Tier}
		if inst.bossDown && g.tick < inst.resetAt {
			z.ResetIn = int((inst.resetAt - g.tick + protocol.TicksPerGameHour - 1) / protocol.TicksPerGameHour)
		}
		for pos := range inst.opened {
			z.Opened = append(z.Opened, [2]int{pos.X, pos.Y})
		}
		slices.SortFunc(z.Opened, func(a, b [2]int) int { return cmp.Or(a[1]-b[1], a[0]-b[0]) })
	}
	p.sendZoneMsg(z)
}

// sendZoneToAll resends the zone to all players of the instance (a chest has just been opened).
func (g *Game) sendZoneToAll(inst *instance) {
	for _, id := range g.sortedIDs() {
		if p := g.players[id]; p.zone == inst.id {
			g.sendZone(p)
		}
	}
}

// instanceOf returns the current instance of dungeon d, or opens a new one (first visitor, or boss defeated
// more than a game day ago). During that delay, the dungeon stays as the victory left it: boss dead, chests emptied.
func (g *Game) instanceOf(d *world.Dungeon) *instance {
	old := g.instances[g.dungeonZone[d.ID]]
	if old != nil && !g.resettable(old) {
		return old
	}
	inst := g.newInstance(d)
	if old != nil {
		g.closeIfEmpty(old) // the old one now only serves those still in it
	}
	return inst
}

// bossSlain records the death of the instance's boss: its chest unlocks, and the dungeon can start afresh one game
// day later, for the next first visitor. Until then, it stays as is for everyone. Later, those still
// in the old instance finish in peace, and it closes with its last player.
func (g *Game) bossSlain(inst *instance) {
	inst.bossDown = true
	inst.resetAt = g.tick + dungeonResetTicks
}

// newInstance opens an instance of dungeon d and populates it.
func (g *Game) newInstance(d *world.Dungeon) *instance {
	inst := &instance{
		id:        g.nextZone,
		d:         d,
		dmap:      world.NewDungeonMap(d),
		byPos:     make(map[world.Pos]int),
		monsterAt: make(map[world.Pos]int),
		opened:    make(map[world.Pos]bool),
	}
	g.nextZone++
	g.instances[inst.id] = inst
	g.dungeonZone[d.ID] = inst.id
	for i, s := range inst.dmap.Slots {
		sp, ok := dungeonSpecies(s.Role, i)
		if !ok {
			continue
		}
		m := g.addMonsterIn(inst.id, scaledForTier(sp, d.Tier), s.Pos)
		m.leash = garrisonLeash
	}
	return inst
}

// dungeonSpecies chooses the species that occupies slot no. i of the layout: guards and elites alternate between
// garrison monsters and dungeon monsters, the boss is always a lich.
func dungeonSpecies(r world.SlotRole, i int) (species, bool) {
	kind := ""
	switch r {
	case world.RoleGuard:
		kind = [...]string{"skeleton", "wraith"}[i%2]
	case world.RoleElite:
		kind = [...]string{"knight", "golem"}[i%2]
	case world.RoleBoss:
		kind = "lich"
	}
	for _, s := range speciesList {
		if s.Kind == kind {
			return s, true
		}
	}
	return species{}, false
}

// scaledForTier adapts a monster to the dungeon tier (1 to 5): 80% of its HP and XP at tier 1, up to
// 160% at tier 5, and one extra point of hit every two tiers.
func scaledForTier(sp species, tier int) species {
	pct := 60 + 20*tier
	sp.HP = max(1, sp.HP*pct/100)
	sp.XP = max(1, sp.XP*pct/100)
	sp.Hit += (tier - 1) / 2
	return sp
}

// scaledForLevel adapts a surface monster to the level of its region (see world.LevelAt), from 1 to 20: HP and XP
// increase by 25% per level beyond the first, and hit, damage and DEF follow at a slower
// rate. At level 1, the species stays as is.
func scaledForLevel(sp species, level int) species {
	level = max(1, level)
	pct := 100 + 25*(level-1)
	sp.HP = max(1, sp.HP*pct/100)
	sp.XP = max(1, sp.XP*pct/100)
	sp.Hit += (level - 1) / 3
	sp.Dmg = sp.Dmg.WithBonus((level - 1) / 4)
	sp.DexMod += (level - 1) / 5
	sp.Level = level
	return sp
}

// closeIfEmpty destroys the instance, with its monsters, if it can be replaced (boss dead for more than a game
// day) and no player is in it any more. While the boss lives, or the delay runs, an empty dungeon keeps
// its state: otherwise the dungeon would be rebuilt as soon as one leaves.
func (g *Game) closeIfEmpty(inst *instance) {
	if !g.resettable(inst) {
		return
	}
	for _, p := range g.players {
		if p.zone == inst.id {
			return
		}
	}
	g.closeInstance(inst)
}

// closeInstance destroys the instance and its monsters, whatever the players.
func (g *Game) closeInstance(inst *instance) {
	if g.dungeonZone[inst.d.ID] == inst.id {
		delete(g.dungeonZone, inst.d.ID)
	}
	for _, id := range g.sortedMonsterIDs() {
		if m := g.monsters[id]; m.zone == inst.id {
			g.removeMonster(m)
		}
	}
	delete(g.instances, inst.id)
}

package game

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// dungeonGame returns a game with dungeons enabled and a world dungeon, the nearest to the origin.
func dungeonGame(t *testing.T) (*Game, *world.Dungeon) {
	t.Helper()
	g := newGame()
	g.Dungeons = true
	var best *world.Dungeon
	for _, d := range g.World.DungeonsNear(0, 0, 4) {
		if best == nil || d.ID < best.ID {
			best = d
		}
	}
	if best == nil {
		t.Fatal("no dungeon near the origin")
	}
	return g, best
}

// walkIn places p next to d's entrance, on the west side, and makes them walk onto it.
func walkIn(g *Game, p *player, d *world.Dungeon) {
	g.setPos(p, world.Pos{X: d.Entrance.X - 1, Y: d.Entrance.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
}

func zoneMonsters(g *Game, z ZoneID) int {
	n := 0
	for _, m := range g.monsters {
		if m.zone == z {
			n++
		}
	}
	return n
}

func TestDungeonEntranceDoesNothingWhenDisabled(t *testing.T) {
	g, d := dungeonGame(t)
	g.Dungeons = false
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)
	if p.zone != 0 || p.pos != d.Entrance {
		t.Fatalf("zone %d, pos %v: should walk onto the entrance without entering", p.zone, p.pos)
	}
}

func TestWalkingOnEntranceOpensAnInstance(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)

	if p.zone == 0 {
		t.Fatal("the player stayed on the main map")
	}
	inst := g.instances[p.zone]
	if inst == nil || inst.d != d {
		t.Fatalf("instance: %+v", inst)
	}
	if p.ret != d.Entrance {
		t.Errorf("return point %v, expected %v", p.ret, d.Entrance)
	}
	if cheb(p.pos, inst.dmap.Spawn) > 1 || !inst.dmap.TileAt(p.pos.X, p.pos.Y).Walkable() {
		t.Errorf("arrived at %v, arrival point %v", p.pos, inst.dmap.Spawn)
	}
	if got, want := zoneMonsters(g, p.zone), len(inst.dmap.Slots); got != want {
		t.Errorf("%d monsters in the dungeon, expected %d", got, want)
	}
	if len(g.monsterAt) != 0 || g.biomeCount() != 0 {
		t.Errorf("the dungeon polluted the main map: %d, %d", len(g.monsterAt), g.biomeCount())
	}
	if _, ok := g.byPos[d.Entrance]; ok {
		t.Error("the player is still indexed on the main map")
	}
	if inst.byPos[p.pos] != p.id {
		t.Error("the player is not indexed in the instance")
	}
}

func TestPartyEntersTogetherAndFarPlayersDoNot(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	far, _ := joinRec(g, "far")
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X - 4, Y: d.Entrance.Y - 2}))
	g.setPos(far, g.freeNear(world.Pos{X: d.Entrance.X + 20, Y: d.Entrance.Y}))
	farPos := far.pos
	walkIn(g, a, d)

	if a.zone == 0 || b.zone != a.zone {
		t.Fatalf("zones: a=%d b=%d", a.zone, b.zone)
	}
	if far.zone != 0 || far.pos != farPos {
		t.Errorf("a distant player was carried along: zone %d pos %v", far.zone, far.pos)
	}
	if a.pos == b.pos {
		t.Error("two players on the same tile")
	}
	ra := g.snapshotFor(a, nil)
	if len(ra.Others) != 1 || ra.Others[0].ID != b.id {
		t.Errorf("a should see only b: %+v", ra.Others)
	}
}

func TestDifferentDungeonsAreIsolated(t *testing.T) {
	g, d := dungeonGame(t)
	var other *world.Dungeon
	for _, o := range g.World.DungeonsNear(0, 0, 6) {
		if o != d {
			other = o
			break
		}
	}
	if other == nil {
		t.Skip("only one dungeon near the origin")
	}
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	walkIn(g, a, d)
	walkIn(g, b, other)
	if a.zone == b.zone || a.zone == 0 || b.zone == 0 {
		t.Fatalf("zones a=%d b=%d: each dungeon has its own instance", a.zone, b.zone)
	}
	for _, o := range g.snapshotFor(a, g.sortedMonsterIDs()).Others {
		t.Errorf("a sees %+v from another dungeon", o)
	}
	for _, m := range g.snapshotFor(b, g.sortedMonsterIDs()).Monsters {
		if g.monsters[m.ID].zone != b.zone {
			t.Errorf("b sees monster %d from another dungeon", m.ID)
		}
	}
}

func TestPlayersShareTheInstanceOfADungeon(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	walkIn(g, a, d)
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X + 30, Y: d.Entrance.Y})) // far from a: not in the same entry group
	walkIn(g, b, d)
	if a.zone == 0 || a.zone != b.zone || len(g.instances) != 1 {
		t.Fatalf("zones a=%d b=%d, %d instances: a dungeon has only one instance, shared", a.zone, b.zone, len(g.instances))
	}
	if want := len(g.instances[a.zone].dmap.Slots); zoneMonsters(g, a.zone) != want {
		t.Errorf("%d monsters, expected %d (the second visitor must not repopulate)", zoneMonsters(g, a.zone), want)
	}
	if a.pos == b.pos {
		t.Error("two players on the same tile")
	}
	if got := g.snapshotFor(a, nil).Others; len(got) != 1 || got[0].ID != b.id {
		t.Errorf("a should see b: %+v", got)
	}
}
func TestNoBiomeMonstersOrGarrisonsInsideDungeons(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)
	z := p.zone
	before := len(g.monsters)
	for range 3 * spawnEvery {
		g.step()
	}
	if len(g.monsters) != before || zoneMonsters(g, 0) != 0 {
		t.Errorf("%d main-map monsters appeared while the player was underground", zoneMonsters(g, 0))
	}
	if p.zone != z {
		t.Error("the player changed zone")
	}
}

func TestExitStairsBringYouBack(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)
	inst := g.instances[p.zone]
	monsters := zoneMonsters(g, inst.id)
	exit := inst.dmap.Exit
	g.setPos(p, world.Pos{X: exit.X + 1, Y: exit.Y})
	p.cooldown = 0
	g.move(p.id, -1, 0)
	g.step()

	if p.zone != 0 {
		t.Fatal("the player stayed in the dungeon")
	}
	if p.pos == d.Entrance || cheb(p.pos, d.Entrance) != 1 {
		t.Errorf("returned to %v, expected next to %v", p.pos, d.Entrance)
	}
	// The boss lives: the dungeon keeps its state, with its monsters.
	if g.instances[inst.id] != inst || zoneMonsters(g, inst.id) != monsters {
		t.Errorf("the dungeon lost its state: %d instances, %d monsters out of %d", len(g.instances), zoneMonsters(g, inst.id), monsters)
	}
	if g.byPos[p.pos] != p.id {
		t.Error("the player is not reindexed on the main map")
	}
	// Once out, they do not fall back in on their own.
	for range 5 {
		g.step()
	}
	if p.zone != 0 {
		t.Error("the player re-entered on their own")
	}
}
func TestAnEmptyDungeonKeepsItsStateUntilItsBossFalls(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X - 3, Y: d.Entrance.Y}))
	walkIn(g, a, d)
	inst := g.instances[a.zone]
	g.leaveDungeon(a)
	g.leave(b.id)
	if g.instances[inst.id] != inst || zoneMonsters(g, inst.id) == 0 {
		t.Fatal("the empty dungeon lost its state")
	}
	// The boss dies: the instance is exhausted but, empty, keeps its state during the delay, otherwise leaving would rebuild the dungeon.
	g.bossSlain(inst)
	g.closeIfEmpty(inst)
	if g.instances[inst.id] != inst {
		t.Fatal("a dungeon whose boss has just fallen must not be rebuilt when emptying")
	}
	// Once the delay has elapsed, it closes as soon as it is empty.
	g.tick += dungeonResetTicks
	c, _ := joinRec(g, "c")
	g.teleport(c, inst.id, inst.dmap.Spawn, 0)
	g.closeIfEmpty(inst)
	if g.instances[inst.id] == nil {
		t.Fatal("closed while a player is inside")
	}
	g.leaveDungeon(c)
	if len(g.instances) != 0 || zoneMonsters(g, inst.id) != 0 || g.dungeonMonsters != 0 {
		t.Errorf("exhausted instance not cleaned up: %d instances, %d monsters", len(g.instances), g.dungeonMonsters)
	}
}

func TestDisconnectingInADungeonSavesTheSurfacePosition(t *testing.T) {
	g, d := dungeonGame(t)
	var saved []Saved
	g.Persist = func(_ string, s Saved) { saved = append(saved, s) }
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)
	g.leave(p.id)

	if len(saved) == 0 || saved[len(saved)-1].Pos != d.Entrance {
		t.Fatalf("saves %+v, expected a position %v", saved, d.Entrance)
	}
	if len(g.byPos) != 0 || len(g.instances) != 1 {
		t.Errorf("after leaving: %d players indexed, %d instances (the dungeon keeps its state)", len(g.byPos), len(g.instances))
	}
}
func TestDefeatInADungeonSendsYouToTheEntrance(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	p.sendCombat = func(protocol.Combat) {}
	walkIn(g, p, d)
	inst := g.instances[p.zone]
	m := g.addMonsterIn(p.zone, deadly(testRat), g.freeNearIn(p.zone, p.pos, 1))
	m.cooldown = 0
	g.startFight(p, m, false)
	for i := 0; i < 200 && p.fight != nil; i++ {
		g.act(p, 1, 0)
	}
	if p.fight != nil {
		t.Fatal("the fight did not conclude")
	}
	if p.zone != 0 || cheb(p.pos, d.Entrance) != 1 {
		t.Errorf("zone %d, pos %v: expected in front of the entrance %v", p.zone, p.pos, d.Entrance)
	}
	if g.instances[inst.id] != inst {
		t.Error("the dungeon must keep its state after a player's defeat")
	}
}

func TestFightGroupsStayInTheirZone(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)
	surface := g.addMonster(testRat, g.freeNear(d.Entrance))
	inDungeon := g.addMonsterIn(p.zone, testRat, g.freeNearIn(p.zone, p.pos, 1))
	for _, o := range g.gatherGroup(inDungeon) {
		if o == surface {
			t.Fatal("a surface monster joined a dungeon fight")
		}
	}
}

// enter opens an instance for a new player and gives them a bag that captures the notices.
func enter(t *testing.T) (*Game, *world.Dungeon, *player, *instance, *[]protocol.Bag) {
	t.Helper()
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	var bags []protocol.Bag
	p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	walkIn(g, p, d)
	return g, d, p, g.instances[p.zone], &bags
}

// stepOn makes p walk onto tile pos of their instance, from a walkable neighbouring tile.
func stepOn(g *Game, p *player, pos world.Pos) {
	inst := g.instances[p.zone]
	for _, dd := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		from := world.Pos{X: pos.X + dd[0], Y: pos.Y + dd[1]}
		if inst.dmap.TileAt(from.X, from.Y).Walkable() && !g.blockedIn(p.zone, from) {
			g.setPos(p, from)
			p.cooldown = 0
			g.move(p.id, -dd[0], -dd[1])
			g.step()
			return
		}
	}
	panic("no free neighbouring tile")
}

func TestDungeonPopulationUsesDungeonSpeciesScaledByTier(t *testing.T) {
	g, d := dungeonGame(t)
	// Same slots, all tiers: the boss is a lich, HP grow with the tier.
	var prevBossHP int
	for tier := 1; tier <= 5; tier++ {
		dd := *d
		dd.Tier = tier
		in := g.newInstance(&dd)
		bosses, elites, guards := 0, 0, 0
		var bossHP int
		for _, id := range g.sortedMonsterIDs() {
			m := g.monsters[id]
			if m.zone != in.id {
				continue
			}
			switch {
			case m.sp.Boss:
				bosses++
				bossHP = m.sp.HP
				if m.sp.Kind != "lich" {
					t.Errorf("boss %q, expected lich", m.sp.Kind)
				}
			case m.sp.Kind == "knight" || m.sp.Kind == "golem":
				elites++
			default:
				guards++
			}
		}
		if bosses != 1 || elites < 1 || guards < 1 {
			t.Errorf("tier %d: %d bosses, %d elites, %d guards", tier, bosses, elites, guards)
		}
		if tier > 1 && bossHP <= prevBossHP {
			t.Errorf("tier %d: boss HP %d, no more than %d", tier, bossHP, prevBossHP)
		}
		prevBossHP = bossHP
		g.closeInstance(in)
	}
}

func TestDungeonSpeciesNeverSpawnOnTheSurface(t *testing.T) {
	for tile, sps := range speciesByTile {
		for _, sp := range sps {
			if sp.Dungeon || sp.Garrison {
				t.Errorf("%s appears on %v", sp.Kind, tile)
			}
		}
	}
}

func TestOrdinaryChestGivesLootOnce(t *testing.T) {
	g, _, p, inst, bags := enter(t)
	var chest world.Pos
	for _, c := range inst.dmap.Chests {
		if c != inst.dmap.BossChest {
			chest = c
			break
		}
	}
	if chest == (world.Pos{}) {
		t.Skip("this dungeon has only the boss chest")
	}
	gold := p.stats.Gold
	stepOn(g, p, chest)

	if !inst.opened[chest] || p.stats.Gold <= gold {
		t.Fatalf("chest not opened: opened=%v gold %d -> %d", inst.opened[chest], gold, p.stats.Gold)
	}
	if len(*bags) == 0 || (*bags)[len(*bags)-1].Notice == nil {
		t.Fatalf("no notice sent: %+v", *bags)
	}
	// A second visit gives nothing again.
	gold = p.stats.Gold
	items := len(p.inv.Items)
	stepOn(g, p, chest)
	if p.stats.Gold != gold || len(p.inv.Items) != items {
		t.Error("the chest gave twice")
	}
}

func TestBossChestIsLockedUntilTheBossFalls(t *testing.T) {
	g, _, p, inst, _ := enter(t)
	chest := inst.dmap.BossChest
	if chest == (world.Pos{}) {
		t.Fatal("no boss chest")
	}
	stepOn(g, p, chest)
	if inst.opened[chest] {
		t.Fatal("the boss chest opened while the boss lives")
	}
	inst.bossDown = true
	items := len(p.inv.Items)
	stepOn(g, p, chest)
	if !inst.opened[chest] || len(p.inv.Items) != items+1 {
		t.Fatalf("boss chest: opened=%v, items %d -> %d", inst.opened[chest], items, len(p.inv.Items))
	}
	it := p.inv.Items[len(p.inv.Items)-1]
	if it.Origin.Index != -1 || it.Origin.Region != inst.d.Region || it.Tier < inst.d.Tier {
		t.Errorf("boss item: %+v", it)
	}
	if info := g.itemInfo(p.lang, p, it); info.Origin != "" {
		t.Errorf("origin %q announced for a dungeon item without a vendor", info.Origin)
	}
}

func TestFullBagKeepsTheBossChestClosed(t *testing.T) {
	g, _, p, inst, bags := enter(t)
	inst.bossDown = true
	for len(p.inv.Items) < 24 {
		p.inv.Add(gear.Item{Name: "x", Slot: gear.Helm, Tier: 1, Def: 1})
	}
	stepOn(g, p, inst.dmap.BossChest)
	if inst.opened[inst.dmap.BossChest] {
		t.Fatal("the chest opened despite the full bag")
	}
	if len(*bags) == 0 {
		t.Error("the player is not warned")
	}
}

func TestKillingTheBossUnlocksItAndPaysMore(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	lich, _ := dungeonSpecies(world.RoleBoss, 0)
	lich.HP, lich.Hit, lich.Dmg = 1, -50, dice.D(1, 1, 0) // it cannot hit: only the loot interests us
	boss := g.addMonsterIn(p.zone, lich, g.freeNearIn(p.zone, p.pos, 1))
	p.sendCombat = func(protocol.Combat) {}
	gold := p.stats.Gold
	g.startFight(p, boss, true)
	for i := 0; i < 100 && p.fight != nil; i++ {
		g.act(p, 1, 0)
	}
	if p.fight != nil {
		t.Fatal("fight not finished")
	}
	if !inst.bossDown {
		t.Error("the boss's death did not unlock its chest")
	}
	if got := p.stats.Gold - gold; got < bossGold+bossGoldPerTier*d.Tier {
		t.Errorf("boss gold %d, expected at least %d", got, bossGold+bossGoldPerTier*d.Tier)
	}
}

func TestDungeonBossDoesNotCompleteAFortressBossQuest(t *testing.T) {
	lich, _ := dungeonSpecies(world.RoleBoss, 0)
	warlord, _ := speciesForRole(world.RoleBoss)
	g := newGame()
	p, _ := joinRec(g, "a")
	p.quests = append(p.quests, quest.Quest{ID: 1, State: quest.Active, Objective: quest.Objective{Kind: quest.Boss, Species: "warlord", Count: 2}})
	g.questKill(p, lich)
	if p.quests[0].Progress != 0 {
		t.Error("the lich completed a warlord quest")
	}
	g.questKill(p, warlord)
	if p.quests[0].Progress != 1 {
		t.Error("the warlord should complete their quest")
	}
}

func TestZoneMessagesAnnounceEveryChange(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	var zones []protocol.Zone
	p.sendZoneMsg = func(z protocol.Zone) { zones = append(zones, z) }
	p.sendBagMsg = func(protocol.Bag) {}

	walkIn(g, p, d)
	if len(zones) != 1 {
		t.Fatalf("%d messages on entry, expected 1", len(zones))
	}
	in := zones[0]
	if in.Dungeon != d.ID || in.Name != d.NameIn(p.lang) || in.Seed != d.Seed || in.Tier != d.Tier || len(in.Opened) != 0 {
		t.Errorf("entry message: %+v", in)
	}
	// The client must be able to rebuild the same layout as the server.
	mine := world.NewDungeonMap(&world.Dungeon{Seed: in.Seed, Tier: in.Tier})
	inst := g.instances[p.zone]
	if mine.Exit != inst.dmap.Exit || mine.BossChest != inst.dmap.BossChest || len(mine.Slots) != len(inst.dmap.Slots) {
		t.Error("the layout recomputed by the client differs from the server's")
	}

	// An opened chest is resent to everyone, with the list of emptied chests.
	var chest world.Pos
	for _, c := range inst.dmap.Chests {
		if c != inst.dmap.BossChest {
			chest = c
		}
	}
	if chest != (world.Pos{}) {
		inst.opened[chest] = false
		for range 20 { // an ordinary chest only holds an item 35% of the time: try until it opens
			stepOn(g, p, chest)
			if inst.opened[chest] {
				break
			}
		}
		last := zones[len(zones)-1]
		if len(last.Opened) != 1 || last.Opened[0] != [2]int{chest.X, chest.Y} || last.Dungeon != d.ID {
			t.Errorf("after chest: %+v", last)
		}
	}

	g.leaveDungeon(p)
	if last := zones[len(zones)-1]; last.Dungeon != "" {
		t.Errorf("on exit: %+v, expected the main map", last)
	}
}

func TestZoneMessageComesBeforeTheFirstSnapshotOfTheNewMap(t *testing.T) {
	g, d := dungeonGame(t)
	var order []string
	p, _ := joinRec(g, "a")
	p.send = func(protocol.Snapshot) { order = append(order, "snap") }
	p.sendZoneMsg = func(protocol.Zone) { order = append(order, "zone") }
	walkIn(g, p, d)
	g.step()
	if len(order) < 2 || order[0] != "zone" || order[1] != "snap" {
		t.Errorf("order %v: the zone must precede the snapshot", order)
	}
}

// listen hooks a receiver onto p's "dungeons" messages.
func listen(p *player) *[]protocol.Dungeons {
	var got []protocol.Dungeons
	p.sendDungeonsMsg = func(d protocol.Dungeons) { got = append(got, d) }
	return &got
}

func TestPassingNearAnEntranceDiscoversItOnce(t *testing.T) {
	g, d := dungeonGame(t)
	var saved []Saved
	g.Persist = func(_ string, s Saved) { saved = append(saved, s) }
	p, _ := joinRec(g, "a")
	got := listen(p)

	// At six tiles, nothing is noticed.
	g.setPos(p, world.Pos{X: d.Entrance.X - 6, Y: d.Entrance.Y})
	if len(*got) != 0 || p.discovered[d.Region] {
		t.Fatal("discovered from too far")
	}
	// At three tiles, it is.
	g.setPos(p, world.Pos{X: d.Entrance.X - discoverRadius, Y: d.Entrance.Y})
	if len(*got) != 1 || !p.discovered[d.Region] {
		t.Fatalf("%d messages, discovered=%v", len(*got), p.discovered[d.Region])
	}
	msg := (*got)[0]
	if msg.New != d.ID || len(msg.List) != 1 || msg.List[0] != (protocol.DungeonInfo{ID: d.ID, Name: d.NameIn(p.lang), X: d.Entrance.X, Y: d.Entrance.Y, Tier: d.Tier}) {
		t.Errorf("message: %+v", msg)
	}
	if len(saved) == 0 || !saved[len(saved)-1].DiscoveredDirty || len(saved[len(saved)-1].Discovered) != 1 {
		t.Errorf("the discovery is not saved: %+v", saved)
	}
	// It is not announced twice.
	g.setPos(p, world.Pos{X: d.Entrance.X - 2, Y: d.Entrance.Y})
	if len(*got) != 1 {
		t.Errorf("rediscovery announced (%d messages)", len(*got))
	}
	// A following save does not rewrite the list, but always attaches it.
	g.save(p)
	if last := saved[len(saved)-1]; last.DiscoveredDirty || len(last.Discovered) != 1 {
		t.Errorf("following save: %+v", last)
	}
}

func TestDiscoveriesSurviveAReconnection(t *testing.T) {
	g, d := dungeonGame(t)
	var last Saved
	g.Persist = func(_ string, s Saved) { last = s }
	p, _ := joinRec(g, "a")
	g.setPos(p, world.Pos{X: d.Entrance.X, Y: d.Entrance.Y + 2})
	g.leave(p.id)
	if len(last.Discovered) != 1 || last.Discovered[0] != d.Region {
		t.Fatalf("saved: %+v", last)
	}

	again := g.joinWith("a", func(protocol.Snapshot) {}, &last)
	if !again.discovered[d.Region] {
		t.Error("the discovery was not restored")
	}
	got := listen(again)
	g.sendDungeons(again, "")
	if len(*got) != 1 || len((*got)[0].List) != 1 || (*got)[0].New != "" {
		t.Errorf("list sent on connection: %+v", *got)
	}
}

func TestNoDiscoveryWhenDungeonsAreOff(t *testing.T) {
	g, d := dungeonGame(t)
	g.Dungeons = false
	p, _ := joinRec(g, "a")
	g.setPos(p, world.Pos{X: d.Entrance.X - 1, Y: d.Entrance.Y})
	if len(p.discovered) != 0 {
		t.Error("discovery while dungeons are disabled")
	}
}

func TestEnteringRevealsTheEntranceToTheWholeParty(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X - 5, Y: d.Entrance.Y - 4})) // in the group, but more than 3 tiles away
	if b.discovered[d.Region] {
		t.Skip("b was already close enough")
	}
	walkIn(g, a, d)
	if b.zone != a.zone || !a.discovered[d.Region] || !b.discovered[d.Region] {
		t.Errorf("zones a=%d b=%d, discovered a=%v b=%v", a.zone, b.zone, a.discovered[d.Region], b.discovered[d.Region])
	}
}

// kill cleanly removes a monster from the instance, as a victory does.
func kill(g *Game, m *monster) { g.removeMonster(m) }

func TestProgressSurvivesLeavingAndComingBack(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	zone := p.zone

	// A defeated guard and an opened chest.
	var guard *monster
	for _, id := range g.sortedMonsterIDs() {
		if m := g.monsters[id]; m.zone == zone && !m.sp.Boss {
			guard = m
			break
		}
	}
	kill(g, guard)
	left := zoneMonsters(g, zone)
	var chest world.Pos
	for _, c := range inst.dmap.Chests {
		if c != inst.dmap.BossChest {
			chest = c
			break
		}
	}
	if chest != (world.Pos{}) {
		inst.opened[chest] = true
	}

	g.leaveDungeon(p)
	again, _ := joinRec(g, "b")
	walkIn(g, again, d)
	if again.zone != zone || g.instances[zone] != inst {
		t.Fatalf("zone %d instead of %d: the dungeon should have been found again", again.zone, zone)
	}
	if got := zoneMonsters(g, zone); got != left {
		t.Errorf("%d monsters on return, %d on leaving: a defeated monster did not come back", got, left)
	}
	if chest != (world.Pos{}) && !inst.opened[chest] {
		t.Error("the opened chest closed again")
	}
}

// evacuate makes all players leave their dungeon.
func evacuate(g *Game) {
	for _, id := range g.sortedIDs() {
		if p := g.players[id]; p.zone != 0 {
			g.leaveDungeon(p)
		}
	}
}

// killTheLich kills the boss of p's instance.
func killTheLich(t *testing.T, g *Game, p *player) {
	t.Helper()
	p.sendCombat = func(protocol.Combat) {}
	lich, _ := dungeonSpecies(world.RoleBoss, 0)
	lich.HP, lich.Hit, lich.Dmg = 1, -50, dice.D(1, 1, 0)
	boss := g.addMonsterIn(p.zone, lich, g.freeNearIn(p.zone, p.pos, 1))
	g.startFight(p, boss, true)
	for i := 0; i < 100 && p.fight != nil; i++ {
		g.act(p, 1, 0)
	}
	if !g.instances[p.zone].bossDown {
		t.Fatal("the boss did not fall")
	}
}

func TestKillingTheBossResetsTheDungeonOneGameDayLater(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	opened := inst.dmap.Chests[0]
	inst.opened[opened] = true
	var guard *monster
	for _, id := range g.sortedMonsterIDs() {
		if m := g.monsters[id]; m.zone == p.zone && !m.sp.Boss {
			guard = m
			break
		}
	}
	kill(g, guard)
	left := zoneMonsters(g, inst.id)
	killTheLich(t, g, p)
	if inst.resetAt != g.tick+dungeonResetTicks || dungeonResetTicks != 24*600 {
		t.Fatalf("delay of %d ticks, expected one game day (%d)", inst.resetAt-g.tick, 24*600)
	}

	// During the delay, the dungeon stays as the victory left it, for everyone.
	other, _ := joinRec(g, "b")
	walkIn(g, other, d)
	if other.zone != p.zone || g.instances[other.zone] != inst {
		t.Fatalf("a same-day visitor must find the defeated dungeon (zones %d / %d)", other.zone, p.zone)
	}
	if got := zoneMonsters(g, inst.id); got != left {
		t.Errorf("%d monsters, %d after the victory: nothing must return during the delay", got, left)
	}
	if !inst.opened[opened] {
		t.Error("an opened chest closed again during the delay")
	}
	g.leaveDungeon(p)
	g.leaveDungeon(other)
	if g.instances[inst.id] != inst {
		t.Fatal("empty during the delay, the defeated dungeon must remain")
	}
	g.tick = inst.resetAt - 2 // walkIn advances the game by one tick before the step: we enter one tick before the deadline
	again, _ := joinRec(g, "c")
	walkIn(g, again, d)
	if g.instances[again.zone] != inst {
		t.Fatal("one minute before the deadline, the dungeon must not be rebuilt")
	}
	evacuate(g) // entering takes along the free players lingering in front of the entrance: everyone comes back out

	// Once the deadline has passed, the first visitor finds a fresh dungeon; the old one, empty, disappears.
	g.tick = inst.resetAt
	late, _ := joinRec(g, "d")
	walkIn(g, late, d)
	fresh := g.instances[late.zone]
	if fresh == nil || fresh == inst {
		t.Fatalf("a fresh dungeon was expected (zone %d)", late.zone)
	}
	if fresh.bossDown || len(fresh.opened) != 0 {
		t.Errorf("the fresh instance is not fresh: boss dead=%v, %d chests opened", fresh.bossDown, len(fresh.opened))
	}
	if got, want := zoneMonsters(g, fresh.id), len(fresh.dmap.Slots); got != want {
		t.Errorf("%d monsters in the fresh instance, expected %d (guards back, boss included)", got, want)
	}
	if g.instances[inst.id] != nil || zoneMonsters(g, inst.id) != 0 {
		t.Error("the old instance, empty, should have been closed")
	}
}

func TestSomeoneStillInsideKeepsTheOldDungeonAfterTheReset(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	killTheLich(t, g, p)
	g.tick += dungeonResetTicks
	other, _ := joinRec(g, "b")
	walkIn(g, other, d)
	fresh := g.instances[other.zone]
	if fresh == inst || other.zone == p.zone {
		t.Fatal("the new visitor should open a fresh dungeon")
	}
	if g.instances[inst.id] != inst || p.zone != inst.id {
		t.Fatal("the one who stayed inside must not be expelled")
	}
	g.leaveDungeon(p)
	if g.instances[inst.id] != nil || g.instances[fresh.id] != fresh {
		t.Errorf("after leaving: old=%v new=%v", g.instances[inst.id] != nil, g.instances[fresh.id] != nil)
	}
}

func TestADungeonOnCooldownIsNotOfferedAsWorkAndAnnouncesItsReturn(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	var zones []protocol.Zone
	p.sendZoneMsg = func(z protocol.Zone) { zones = append(zones, z) }
	killTheLich(t, g, p)
	if !g.coolingDown(d) {
		t.Fatal("the defeated dungeon should be resting")
	}
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 24 {
		t.Errorf("return announced in %d h, expected 24", got)
	}
	g.tick += 10 * protocol.TicksPerGameHour
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 14 {
		t.Errorf("after 10 h: %d h, expected 14", got)
	}
	g.tick += 14*protocol.TicksPerGameHour - 1
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 1 {
		t.Errorf("one tick before the deadline: %d h, expected 1 (rounded up)", got)
	}
	g.tick++
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 0 || g.coolingDown(d) {
		t.Errorf("deadline: %d h, resting=%v", got, g.coolingDown(d))
	}
	_ = inst
}
func TestAnEmptyDungeonIsFrozen(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	g.leaveDungeon(p)
	before := map[int]world.Pos{}
	for id, m := range g.monsters {
		if m.zone == inst.id {
			before[id] = m.pos
		}
	}
	if len(before) == 0 {
		t.Fatal("no monsters")
	}
	for range 200 {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	for id, pos := range before {
		if g.monsters[id].pos != pos {
			t.Fatalf("monster %d moved in an empty dungeon", id)
		}
	}
	_ = d
}

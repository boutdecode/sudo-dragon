package game

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// dungeonGame renvoie un jeu avec les donjons activés et un donjon du monde, le plus proche de l'origine.
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
		t.Fatal("aucun donjon près de l'origine")
	}
	return g, best
}

// walkIn place p à côté de l'entrée de d, du côté ouest, et le fait marcher dessus.
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
		t.Fatalf("zone %d, pos %v: on devrait marcher sur l'entrée sans entrer", p.zone, p.pos)
	}
}

func TestWalkingOnEntranceOpensAnInstance(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	walkIn(g, p, d)

	if p.zone == 0 {
		t.Fatal("le joueur est resté sur la carte principale")
	}
	inst := g.instances[p.zone]
	if inst == nil || inst.d != d {
		t.Fatalf("instance: %+v", inst)
	}
	if p.ret != d.Entrance {
		t.Errorf("point de retour %v, attendu %v", p.ret, d.Entrance)
	}
	if cheb(p.pos, inst.dmap.Spawn) > 1 || !inst.dmap.TileAt(p.pos.X, p.pos.Y).Walkable() {
		t.Errorf("arrivée en %v, point d'arrivée %v", p.pos, inst.dmap.Spawn)
	}
	if got, want := zoneMonsters(g, p.zone), len(inst.dmap.Slots); got != want {
		t.Errorf("%d monstres dans le donjon, attendu %d", got, want)
	}
	if len(g.monsterAt) != 0 || g.biomeCount() != 0 {
		t.Errorf("le donjon a pollué la carte principale: %d, %d", len(g.monsterAt), g.biomeCount())
	}
	if _, ok := g.byPos[d.Entrance]; ok {
		t.Error("le joueur est encore indexé sur la carte principale")
	}
	if inst.byPos[p.pos] != p.id {
		t.Error("le joueur n'est pas indexé dans l'instance")
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
		t.Errorf("un joueur éloigné a été emporté: zone %d pos %v", far.zone, far.pos)
	}
	if a.pos == b.pos {
		t.Error("deux joueurs sur la même case")
	}
	ra := g.snapshotFor(a, nil)
	if len(ra.Others) != 1 || ra.Others[0].ID != b.id {
		t.Errorf("a devrait voir b seulement: %+v", ra.Others)
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
		t.Skip("un seul donjon près de l'origine")
	}
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	walkIn(g, a, d)
	walkIn(g, b, other)
	if a.zone == b.zone || a.zone == 0 || b.zone == 0 {
		t.Fatalf("zones a=%d b=%d: chaque donjon a sa propre instance", a.zone, b.zone)
	}
	for _, o := range g.snapshotFor(a, g.sortedMonsterIDs()).Others {
		t.Errorf("a voit %+v d'un autre donjon", o)
	}
	for _, m := range g.snapshotFor(b, g.sortedMonsterIDs()).Monsters {
		if g.monsters[m.ID].zone != b.zone {
			t.Errorf("b voit le monstre %d d'un autre donjon", m.ID)
		}
	}
}

func TestPlayersShareTheInstanceOfADungeon(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	walkIn(g, a, d)
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X + 30, Y: d.Entrance.Y})) // loin d'a: pas du même groupe d'entrée
	walkIn(g, b, d)
	if a.zone == 0 || a.zone != b.zone || len(g.instances) != 1 {
		t.Fatalf("zones a=%d b=%d, %d instances: un donjon n'a qu'une instance, partagée", a.zone, b.zone, len(g.instances))
	}
	if want := len(g.instances[a.zone].dmap.Slots); zoneMonsters(g, a.zone) != want {
		t.Errorf("%d monstres, attendu %d (le second visiteur ne doit pas repeupler)", zoneMonsters(g, a.zone), want)
	}
	if a.pos == b.pos {
		t.Error("deux joueurs sur la même case")
	}
	if got := g.snapshotFor(a, nil).Others; len(got) != 1 || got[0].ID != b.id {
		t.Errorf("a devrait voir b: %+v", got)
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
		t.Errorf("%d monstres de la carte principale apparus pendant que le joueur était sous terre", zoneMonsters(g, 0))
	}
	if p.zone != z {
		t.Error("le joueur a changé de zone")
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
		t.Fatal("le joueur est resté dans le donjon")
	}
	if p.pos == d.Entrance || cheb(p.pos, d.Entrance) != 1 {
		t.Errorf("retour en %v, attendu à côté de %v", p.pos, d.Entrance)
	}
	// Le boss vit: le donjon garde son état, avec ses monstres.
	if g.instances[inst.id] != inst || zoneMonsters(g, inst.id) != monsters {
		t.Errorf("le donjon a perdu son état: %d instances, %d monstres sur %d", len(g.instances), zoneMonsters(g, inst.id), monsters)
	}
	if g.byPos[p.pos] != p.id {
		t.Error("le joueur n'est pas réindexé sur la carte principale")
	}
	// Sorti, il ne retombe pas dedans tout seul.
	for range 5 {
		g.step()
	}
	if p.zone != 0 {
		t.Error("le joueur est rentré tout seul")
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
		t.Fatal("le donjon vide a perdu son état")
	}
	// Le boss meurt: l'instance est épuisée mais, vide, elle garde son état pendant le délai, sinon sortir referait le donjon.
	g.bossSlain(inst)
	g.closeIfEmpty(inst)
	if g.instances[inst.id] != inst {
		t.Fatal("un donjon dont le boss vient de tomber ne doit pas se refaire en se vidant")
	}
	// Le délai écoulé, elle se ferme dès qu'elle est vide.
	g.tick += dungeonResetTicks
	c, _ := joinRec(g, "c")
	g.teleport(c, inst.id, inst.dmap.Spawn, 0)
	g.closeIfEmpty(inst)
	if g.instances[inst.id] == nil {
		t.Fatal("fermée alors qu'un joueur est dedans")
	}
	g.leaveDungeon(c)
	if len(g.instances) != 0 || zoneMonsters(g, inst.id) != 0 || g.dungeonMonsters != 0 {
		t.Errorf("instance épuisée non nettoyée: %d instances, %d monstres", len(g.instances), g.dungeonMonsters)
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
		t.Fatalf("sauvegardes %+v, attendu une position %v", saved, d.Entrance)
	}
	if len(g.byPos) != 0 || len(g.instances) != 1 {
		t.Errorf("après départ: %d joueurs indexés, %d instances (le donjon garde son état)", len(g.byPos), len(g.instances))
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
		t.Fatal("le combat n'a pas abouti")
	}
	if p.zone != 0 || cheb(p.pos, d.Entrance) != 1 {
		t.Errorf("zone %d, pos %v: attendu devant l'entrée %v", p.zone, p.pos, d.Entrance)
	}
	if g.instances[inst.id] != inst {
		t.Error("le donjon doit garder son état après la défaite d'un joueur")
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
			t.Fatal("un monstre de la surface a rejoint un combat de donjon")
		}
	}
}

// enter ouvre une instance pour un nouveau joueur et lui donne un sac qui capture les notices.
func enter(t *testing.T) (*Game, *world.Dungeon, *player, *instance, *[]protocol.Bag) {
	t.Helper()
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	var bags []protocol.Bag
	p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	walkIn(g, p, d)
	return g, d, p, g.instances[p.zone], &bags
}

// stepOn fait marcher p sur la case pos de son instance, depuis une case voisine praticable.
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
	panic("aucune case voisine libre")
}

func TestDungeonPopulationUsesDungeonSpeciesScaledByTier(t *testing.T) {
	g, d := dungeonGame(t)
	// Mêmes emplacements, tous les paliers : le boss est une liche, les PV croissent avec le palier.
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
					t.Errorf("boss %q, attendu lich", m.sp.Kind)
				}
			case m.sp.Kind == "knight" || m.sp.Kind == "golem":
				elites++
			default:
				guards++
			}
		}
		if bosses != 1 || elites < 1 || guards < 1 {
			t.Errorf("palier %d: %d boss, %d élites, %d gardes", tier, bosses, elites, guards)
		}
		if tier > 1 && bossHP <= prevBossHP {
			t.Errorf("palier %d: PV du boss %d, pas plus que %d", tier, bossHP, prevBossHP)
		}
		prevBossHP = bossHP
		g.closeInstance(in)
	}
}

func TestDungeonSpeciesNeverSpawnOnTheSurface(t *testing.T) {
	for tile, sps := range speciesByTile {
		for _, sp := range sps {
			if sp.Dungeon || sp.Garrison {
				t.Errorf("%s apparaît sur %v", sp.Kind, tile)
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
		t.Skip("ce donjon n'a que le coffre du boss")
	}
	gold := p.stats.Gold
	stepOn(g, p, chest)

	if !inst.opened[chest] || p.stats.Gold <= gold {
		t.Fatalf("coffre non ouvert: opened=%v or %d -> %d", inst.opened[chest], gold, p.stats.Gold)
	}
	if len(*bags) == 0 || (*bags)[len(*bags)-1].Notice == nil {
		t.Fatalf("aucune notice envoyée: %+v", *bags)
	}
	// Une seconde visite ne redonne rien.
	gold = p.stats.Gold
	items := len(p.inv.Items)
	stepOn(g, p, chest)
	if p.stats.Gold != gold || len(p.inv.Items) != items {
		t.Error("le coffre a donné deux fois")
	}
}

func TestBossChestIsLockedUntilTheBossFalls(t *testing.T) {
	g, _, p, inst, _ := enter(t)
	chest := inst.dmap.BossChest
	if chest == (world.Pos{}) {
		t.Fatal("pas de coffre du boss")
	}
	stepOn(g, p, chest)
	if inst.opened[chest] {
		t.Fatal("le coffre du boss s'est ouvert alors que le boss vit")
	}
	inst.bossDown = true
	items := len(p.inv.Items)
	stepOn(g, p, chest)
	if !inst.opened[chest] || len(p.inv.Items) != items+1 {
		t.Fatalf("coffre du boss: ouvert=%v, objets %d -> %d", inst.opened[chest], items, len(p.inv.Items))
	}
	it := p.inv.Items[len(p.inv.Items)-1]
	if it.Origin.Index != -1 || it.Origin.Region != inst.d.Region || it.Tier < inst.d.Tier {
		t.Errorf("objet du boss: %+v", it)
	}
	if info := g.itemInfo(p.lang, p, it); info.Origin != "" {
		t.Errorf("origine %q annoncée pour un objet de donjon sans vendeur", info.Origin)
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
		t.Fatal("le coffre s'est ouvert malgré le sac plein")
	}
	if len(*bags) == 0 {
		t.Error("le joueur n'est pas prévenu")
	}
}

func TestKillingTheBossUnlocksItAndPaysMore(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	lich, _ := dungeonSpecies(world.RoleBoss, 0)
	lich.HP, lich.Hit, lich.Dmg = 1, -50, dice.D(1, 1, 0) // il ne peut pas toucher: seul le butin nous intéresse
	boss := g.addMonsterIn(p.zone, lich, g.freeNearIn(p.zone, p.pos, 1))
	p.sendCombat = func(protocol.Combat) {}
	gold := p.stats.Gold
	g.startFight(p, boss, true)
	for i := 0; i < 100 && p.fight != nil; i++ {
		g.act(p, 1, 0)
	}
	if p.fight != nil {
		t.Fatal("combat non terminé")
	}
	if !inst.bossDown {
		t.Error("la mort du boss n'a pas déverrouillé son coffre")
	}
	if got := p.stats.Gold - gold; got < bossGold+bossGoldPerTier*d.Tier {
		t.Errorf("or du boss %d, attendu au moins %d", got, bossGold+bossGoldPerTier*d.Tier)
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
		t.Error("la liche a validé une quête de seigneur de guerre")
	}
	g.questKill(p, warlord)
	if p.quests[0].Progress != 1 {
		t.Error("le seigneur de guerre devrait valider sa quête")
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
		t.Fatalf("%d messages à l'entrée, attendu 1", len(zones))
	}
	in := zones[0]
	if in.Dungeon != d.ID || in.Name != d.NameIn(p.lang) || in.Seed != d.Seed || in.Tier != d.Tier || len(in.Opened) != 0 {
		t.Errorf("message d'entrée: %+v", in)
	}
	// Le client doit pouvoir reconstruire le même plan que le serveur.
	mine := world.NewDungeonMap(&world.Dungeon{Seed: in.Seed, Tier: in.Tier})
	inst := g.instances[p.zone]
	if mine.Exit != inst.dmap.Exit || mine.BossChest != inst.dmap.BossChest || len(mine.Slots) != len(inst.dmap.Slots) {
		t.Error("le plan recalculé par le client diffère de celui du serveur")
	}

	// Un coffre ouvert est renvoyé à tous, avec la liste des coffres vidés.
	var chest world.Pos
	for _, c := range inst.dmap.Chests {
		if c != inst.dmap.BossChest {
			chest = c
		}
	}
	if chest != (world.Pos{}) {
		inst.opened[chest] = false
		for range 20 { // un coffre ordinaire ne contient un objet que 35 % du temps: on essaie jusqu'à ce qu'il s'ouvre
			stepOn(g, p, chest)
			if inst.opened[chest] {
				break
			}
		}
		last := zones[len(zones)-1]
		if len(last.Opened) != 1 || last.Opened[0] != [2]int{chest.X, chest.Y} || last.Dungeon != d.ID {
			t.Errorf("après coffre: %+v", last)
		}
	}

	g.leaveDungeon(p)
	if last := zones[len(zones)-1]; last.Dungeon != "" {
		t.Errorf("à la sortie: %+v, attendu la carte principale", last)
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
		t.Errorf("ordre %v: la zone doit précéder l'instantané", order)
	}
}

// listen branche un récepteur sur les messages « donjons » de p.
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

	// À six cases, on ne remarque rien.
	g.setPos(p, world.Pos{X: d.Entrance.X - 6, Y: d.Entrance.Y})
	if len(*got) != 0 || p.discovered[d.Region] {
		t.Fatal("découvert de trop loin")
	}
	// À trois cases, si.
	g.setPos(p, world.Pos{X: d.Entrance.X - discoverRadius, Y: d.Entrance.Y})
	if len(*got) != 1 || !p.discovered[d.Region] {
		t.Fatalf("%d messages, découvert=%v", len(*got), p.discovered[d.Region])
	}
	msg := (*got)[0]
	if msg.New != d.ID || len(msg.List) != 1 || msg.List[0] != (protocol.DungeonInfo{ID: d.ID, Name: d.NameIn(p.lang), X: d.Entrance.X, Y: d.Entrance.Y, Tier: d.Tier}) {
		t.Errorf("message: %+v", msg)
	}
	if len(saved) == 0 || !saved[len(saved)-1].DiscoveredDirty || len(saved[len(saved)-1].Discovered) != 1 {
		t.Errorf("la découverte n'est pas sauvegardée: %+v", saved)
	}
	// On ne l'annonce pas deux fois.
	g.setPos(p, world.Pos{X: d.Entrance.X - 2, Y: d.Entrance.Y})
	if len(*got) != 1 {
		t.Errorf("redécouverte annoncée (%d messages)", len(*got))
	}
	// Une sauvegarde suivante ne réécrit pas la liste, mais la joint toujours.
	g.save(p)
	if last := saved[len(saved)-1]; last.DiscoveredDirty || len(last.Discovered) != 1 {
		t.Errorf("sauvegarde suivante: %+v", last)
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
		t.Fatalf("sauvegardé: %+v", last)
	}

	again := g.joinWith("a", func(protocol.Snapshot) {}, &last)
	if !again.discovered[d.Region] {
		t.Error("la découverte n'a pas été restaurée")
	}
	got := listen(again)
	g.sendDungeons(again, "")
	if len(*got) != 1 || len((*got)[0].List) != 1 || (*got)[0].New != "" {
		t.Errorf("liste envoyée à la connexion: %+v", *got)
	}
}

func TestNoDiscoveryWhenDungeonsAreOff(t *testing.T) {
	g, d := dungeonGame(t)
	g.Dungeons = false
	p, _ := joinRec(g, "a")
	g.setPos(p, world.Pos{X: d.Entrance.X - 1, Y: d.Entrance.Y})
	if len(p.discovered) != 0 {
		t.Error("découverte alors que les donjons sont désactivés")
	}
}

func TestEnteringRevealsTheEntranceToTheWholeParty(t *testing.T) {
	g, d := dungeonGame(t)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	g.setPos(b, g.freeNear(world.Pos{X: d.Entrance.X - 5, Y: d.Entrance.Y - 4})) // dans le groupe, mais à plus de 3 cases
	if b.discovered[d.Region] {
		t.Skip("b était déjà assez près")
	}
	walkIn(g, a, d)
	if b.zone != a.zone || !a.discovered[d.Region] || !b.discovered[d.Region] {
		t.Errorf("zones a=%d b=%d, découvert a=%v b=%v", a.zone, b.zone, a.discovered[d.Region], b.discovered[d.Region])
	}
}

// kill supprime proprement un monstre de l'instance, comme le fait une victoire.
func kill(g *Game, m *monster) { g.removeMonster(m) }

func TestProgressSurvivesLeavingAndComingBack(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	zone := p.zone

	// Un garde vaincu et un coffre ouvert.
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
		t.Fatalf("zone %d au lieu de %d: le donjon aurait dû être retrouvé", again.zone, zone)
	}
	if got := zoneMonsters(g, zone); got != left {
		t.Errorf("%d monstres au retour, %d à la sortie: un monstre vaincu n'est pas revenu", got, left)
	}
	if chest != (world.Pos{}) && !inst.opened[chest] {
		t.Error("le coffre ouvert s'est refermé")
	}
}

// evacuate fait sortir de leur donjon tous les joueurs.
func evacuate(g *Game) {
	for _, id := range g.sortedIDs() {
		if p := g.players[id]; p.zone != 0 {
			g.leaveDungeon(p)
		}
	}
}

// killTheLich tue le boss de l'instance de p.
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
		t.Fatal("le boss n'est pas tombé")
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
		t.Fatalf("délai de %d ticks, attendu un jour de jeu (%d)", inst.resetAt-g.tick, 24*600)
	}

	// Pendant le délai, le donjon reste tel que la victoire l'a laissé, pour tous.
	other, _ := joinRec(g, "b")
	walkIn(g, other, d)
	if other.zone != p.zone || g.instances[other.zone] != inst {
		t.Fatalf("un visiteur du jour même doit retrouver le donjon vaincu (zones %d / %d)", other.zone, p.zone)
	}
	if got := zoneMonsters(g, inst.id); got != left {
		t.Errorf("%d monstres, %d après la victoire: rien ne doit revenir pendant le délai", got, left)
	}
	if !inst.opened[opened] {
		t.Error("un coffre ouvert s'est refermé pendant le délai")
	}
	g.leaveDungeon(p)
	g.leaveDungeon(other)
	if g.instances[inst.id] != inst {
		t.Fatal("vide pendant le délai, le donjon vaincu doit rester")
	}
	g.tick = inst.resetAt - 2 // walkIn fait avancer le jeu d'un tick avant le pas: on entre un tick avant l'échéance
	again, _ := joinRec(g, "c")
	walkIn(g, again, d)
	if g.instances[again.zone] != inst {
		t.Fatal("à une minute de l'échéance, le donjon ne doit pas être refait")
	}
	evacuate(g) // entrer emmène les joueurs libres qui traînent devant l'entrée: tout le monde ressort

	// L'échéance passée, le premier visiteur trouve un donjon neuf; l'ancien, vide, disparaît.
	g.tick = inst.resetAt
	late, _ := joinRec(g, "d")
	walkIn(g, late, d)
	fresh := g.instances[late.zone]
	if fresh == nil || fresh == inst {
		t.Fatalf("un donjon neuf était attendu (zone %d)", late.zone)
	}
	if fresh.bossDown || len(fresh.opened) != 0 {
		t.Errorf("l'instance neuve n'est pas neuve: boss mort=%v, %d coffres ouverts", fresh.bossDown, len(fresh.opened))
	}
	if got, want := zoneMonsters(g, fresh.id), len(fresh.dmap.Slots); got != want {
		t.Errorf("%d monstres dans l'instance neuve, attendu %d (gardes revenus, boss compris)", got, want)
	}
	if g.instances[inst.id] != nil || zoneMonsters(g, inst.id) != 0 {
		t.Error("l'ancienne instance, vide, aurait dû être fermée")
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
		t.Fatal("le nouveau visiteur devrait ouvrir un donjon neuf")
	}
	if g.instances[inst.id] != inst || p.zone != inst.id {
		t.Fatal("celui qui est resté dedans ne doit pas être expulsé")
	}
	g.leaveDungeon(p)
	if g.instances[inst.id] != nil || g.instances[fresh.id] != fresh {
		t.Errorf("après sa sortie: ancienne=%v neuve=%v", g.instances[inst.id] != nil, g.instances[fresh.id] != nil)
	}
}

func TestADungeonOnCooldownIsNotOfferedAsWorkAndAnnouncesItsReturn(t *testing.T) {
	g, d, p, inst, _ := enter(t)
	var zones []protocol.Zone
	p.sendZoneMsg = func(z protocol.Zone) { zones = append(zones, z) }
	killTheLich(t, g, p)
	if !g.coolingDown(d) {
		t.Fatal("le donjon vaincu devrait être en repos")
	}
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 24 {
		t.Errorf("retour annoncé dans %d h, attendu 24", got)
	}
	g.tick += 10 * protocol.TicksPerGameHour
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 14 {
		t.Errorf("après 10 h: %d h, attendu 14", got)
	}
	g.tick += 14*protocol.TicksPerGameHour - 1
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 1 {
		t.Errorf("à un tick de l'échéance: %d h, attendu 1 (arrondi au-dessus)", got)
	}
	g.tick++
	g.sendZone(p)
	if got := zones[len(zones)-1].ResetIn; got != 0 || g.coolingDown(d) {
		t.Errorf("échéance: %d h, repos=%v", got, g.coolingDown(d))
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
		t.Fatal("pas de monstres")
	}
	for range 200 {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	for id, pos := range before {
		if g.monsters[id].pos != pos {
			t.Fatalf("le monstre %d a bougé dans un donjon vide", id)
		}
	}
	_ = d
}

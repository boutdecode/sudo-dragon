package game

import (
	"context"
	"github.com/kevin/ascii-adventure/internal/dice"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// rig prepares a player and a neighbouring monster, capturing the combat messages.
type rig struct {
	g      *Game
	p      *player
	snaps  *recorder
	m      *monster
	dx, dy int
	msgs   []protocol.Combat
}

func (r *rig) last() protocol.Combat { return r.msgs[len(r.msgs)-1] }

func newRig(t *testing.T, sp species) *rig {
	t.Helper()
	g := newGame()
	p, rec := joinRec(g, "hero")
	p.lang = i18n.FR // the log assertions below are on French catalogue text
	r := &rig{g: g, p: p, snaps: rec}
	p.sendCombat = func(c protocol.Combat) { r.msgs = append(r.msgs, c) }
	r.dx, r.dy = freeDir(t, g, p)
	r.m = g.addMonster(sp, world.Pos{X: p.pos.X + r.dx, Y: p.pos.Y + r.dy})
	return r
}

// engage makes the player walk onto the monster.
func (r *rig) engage() {
	r.g.move(r.p.id, r.dx, r.dy)
	r.g.step()
}

func weak(sp species) species { sp.HP = 1; return sp }
func deadly(sp species) species {
	sp.HP, sp.Hit, sp.Dmg = 1000, 50, dice.D(1, 1, 99)
	return sp
}

func TestWalkingIntoMonsterStartsFight(t *testing.T) {
	r := newRig(t, testRat)
	start := r.p.pos
	r.engage()

	if r.p.fight == nil || r.m.fight != r.p.fight {
		t.Fatal("the fight did not start")
	}
	if r.p.pos != start {
		t.Fatalf("the player moved: %v", r.p.pos)
	}
	if len(r.msgs) != 1 {
		t.Fatalf("%d combat messages, expected 1", len(r.msgs))
	}
	c := r.last()
	if c.Over || len(c.Enemies) != 1 || c.Enemies[0].Kind != "rat" || c.Enemies[0].HP != testRat.HP || c.PlayerHP != r.p.stats.MaxHP || c.Round != 0 {
		t.Fatalf("initial state: %+v", c)
	}
	if len(c.Log) < 2 || !strings.Contains(c.Log[0], "Vous attaquez le Rat") || !strings.Contains(c.Log[1], "Initiative") {
		t.Fatalf("opening log: %v", c.Log)
	}
}

func TestMonsterContactStartsFight(t *testing.T) {
	r := newRig(t, testRat)
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("a monster in contact should attack")
	}
	if c := r.last(); len(c.Log) < 2 || !strings.Contains(c.Log[0], "Un Rat vous attaque") || !strings.Contains(c.Log[1], "Initiative") {
		t.Fatalf("opening log: %v", c.Log)
	}
}

func TestEveryoneIsFrozenDuringFight(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	pPos, mPos := r.p.pos, r.m.pos

	// The player tries to move away; the monster does not move either.
	for i := 0; i < 30; i++ {
		r.g.move(r.p.id, -r.dx, -r.dy)
		r.g.step()
	}
	if r.p.pos != pPos || r.m.pos != mPos {
		t.Fatalf("movement during a fight: player %v->%v, monster %v->%v", pPos, r.p.pos, mPos, r.m.pos)
	}
	if r.p.hasPending {
		t.Fatal("a movement intent stayed pending")
	}
}

func TestOtherMonstersLeaveFightingPlayerAlone(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	other := r.g.addMonster(testRat, freeWalkable(t, r.g, r.p.pos, 2))
	for i := 0; i < 20; i++ {
		r.g.stepMonsters(r.g.sortedMonsterIDs())
	}
	if other.fight != nil {
		t.Fatal("a second monster joined the fight")
	}
	if cheb(other.pos, r.p.pos) < 2 {
		t.Fatalf("the second monster stuck to the fighting player: %v", other.pos)
	}
}

func TestFightingFlagVisibleToOthers(t *testing.T) {
	r := newRig(t, testRat)
	obs, orec := joinRec(r.g, "watcher")
	_ = obs
	r.engage()
	r.g.step()
	snap := orec.last()
	foundPlayer := false
	for _, o := range snap.Others {
		if o.ID == r.p.id && o.Fighting {
			foundPlayer = true
		}
	}
	foundMonster := false
	for _, m := range snap.Monsters {
		if m.ID == r.m.id && m.Fighting {
			foundMonster = true
		}
	}
	if !foundPlayer || !foundMonster {
		t.Fatalf("fighters not flagged to the observer (player=%v, monster=%v): %+v", foundPlayer, foundMonster, snap)
	}
	if !r.snaps.last().Self.Fighting {
		t.Fatal("the player does not see themselves in combat")
	}
}

func TestWinGivesXPRemovesMonsterAndFreesPlayer(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

	c := r.last()
	if !c.Over || c.Result != protocol.ResultWon {
		t.Fatalf("victory expected: %+v", c)
	}
	if r.p.fight != nil {
		t.Fatal("player still in combat")
	}
	if _, ok := r.g.monsters[r.m.id]; ok || len(r.g.monsterAt) != 0 {
		t.Fatal("defeated monster still in the world")
	}
	if r.p.stats.Level != 1 || r.p.stats.XP != testRat.XP {
		t.Fatalf("level %d, XP %d, expected level 1 and %d XP", r.p.stats.Level, r.p.stats.XP, testRat.XP)
	}
	if !strings.Contains(strings.Join(c.Log, "|"), "XP") {
		t.Fatalf("log without reward: %v", c.Log)
	}
}

func TestLevelUpRestoresHP(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.XP = r.p.stats.XPToNext() - 1 // +4 XP crosses the threshold
	r.p.hp = 3
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

	if r.p.stats.Level != 2 {
		t.Fatalf("level %d, expected 2", r.p.stats.Level)
	}
	if r.p.hp != r.p.stats.MaxHP {
		t.Fatalf("HP %d/%d after level up", r.p.hp, r.p.stats.MaxHP)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Niveau 2") {
		t.Fatalf("log without level up: %v", r.last().Log)
	}
}

func TestLossRespawnsWithoutXPLoss(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.stats.XP = 8
	r.p.hp = 5
	old := r.p.pos
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

	c := r.last()
	if !c.Over || c.Result != protocol.ResultLost {
		t.Fatalf("defeat expected: %+v", c)
	}
	if r.p.fight != nil || r.m.fight != nil {
		t.Fatal("fighters not freed")
	}
	if r.p.hp != r.p.stats.MaxHP || r.p.stats.XP != 8 || r.p.stats.Level != 1 {
		t.Fatalf("after defeat: HP %d/%d, XP %d, level %d", r.p.hp, r.p.stats.MaxHP, r.p.stats.XP, r.p.stats.Level)
	}
	if r.p.pos == old {
		t.Fatal("the player was not moved back to the starting point")
	}
	if _, stale := r.g.byPos[old]; stale {
		t.Fatal("old tile never freed")
	}
	if r.g.byPos[r.p.pos] != r.p.id {
		t.Fatal("new tile not indexed")
	}
	if _, alive := r.g.monsters[r.m.id]; !alive || r.m.calm != disengageCooldown {
		t.Fatal("the winning monster must stay, with a delay before re-engaging")
	}
}

func TestFleeFreesBothAndMonsterWaits(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000 // survives failed flights
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil {
		t.Fatal("the flight never succeeded in 200 attempts")
	}
	if c := r.last(); !c.Over || c.Result != protocol.ResultFled {
		t.Fatalf("flight outcome expected: %+v", c)
	}
	if r.m.fight != nil || r.m.calm != fleeGrace {
		t.Fatalf("monster: fight=%v cooldown=%d", r.m.fight, r.m.calm)
	}

	// The player stays stuck to the monster, which only re-engages after the respite.
	for i := 0; i < fleeGrace-1; i++ {
		r.g.tick++
		r.g.stepMonsters(r.g.sortedMonsterIDs())
		if r.p.fight != nil {
			t.Fatalf("re-engaged too early (iteration %d)", i)
		}
	}
	r.g.tick++
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("the monster should have re-engaged after the respite")
	}
}

// After a flight, another monster within range does not engage the player either, until the respite ends.
func TestFleeGraceProtectsFromOtherMonsters(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil {
		t.Fatal("the flight never succeeded in 200 attempts")
	}
	// A second monster, free, in contact with the player.
	var other *monster
	for _, d := range dirs8 {
		pos := world.Pos{X: r.p.pos.X + d[0], Y: r.p.pos.Y + d[1]}
		if !r.g.blocked(pos) && !r.g.World.Safe(pos.X, pos.Y) {
			if _, taken := r.g.monsterIndex(r.p.zone)[pos]; !taken {
				other = r.g.addMonster(testRat, pos)
				break
			}
		}
	}
	if other == nil {
		t.Skip("terrain hardly walkable around the player")
	}
	for i := 0; i < fleeGrace-1; i++ {
		r.g.tick++
		r.g.stepMonsters(r.g.sortedMonsterIDs())
		if r.p.fight != nil {
			t.Fatalf("engaged during the respite (iteration %d)", i)
		}
	}
	r.g.tick++
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("a monster should have engaged after the respite")
	}
}

// Attacking a monster during the respite ends the respite.
func TestAttackEndsFleeGrace(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil || r.p.calmUntil <= r.g.tick {
		t.Fatalf("respite expected after the flight (calmUntil=%d, tick=%d)", r.p.calmUntil, r.g.tick)
	}
	r.m.calm = 0
	r.g.move(r.p.id, r.dx, r.dy)
	r.g.step()
	if r.p.fight == nil {
		t.Fatal("walking onto the monster should have opened a fight")
	}
	if r.p.calmUntil != 0 {
		t.Fatalf("the respite should have ended: calmUntil=%d", r.p.calmUntil)
	}
}

func TestLeaveDuringFightFreesMonster(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	r.g.leave(r.p.id)
	if r.m.fight != nil {
		t.Fatal("monster left trapped in a phantom fight")
	}
	if _, ok := r.g.monsters[r.m.id]; !ok {
		t.Fatal("the monster should have stayed in the world")
	}
	if r.m.calm != disengageCooldown {
		t.Fatalf("cooldown=%d", r.m.calm)
	}
}

func TestIdleFightExpires(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	r.g.tick += fightIdleTicks // right at the limit: not yet expired
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight == nil {
		t.Fatal("expired too early")
	}
	r.g.tick++
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight != nil || r.m.fight != nil {
		t.Fatal("the idle fight should have expired")
	}
	if c := r.last(); !c.Over || c.Result != protocol.ResultFled || !strings.Contains(strings.Join(c.Log, "|"), "inactivité") {
		t.Fatalf("expiry message: %+v", c)
	}
}

func TestActionResetsIdleTimer(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	r.g.tick += fightIdleTicks
	r.g.act(r.p, combat.Attack, 0) // the player acts: the timer restarts
	r.g.tick += fightIdleTicks
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight == nil {
		t.Fatal("an active player must not expire")
	}
}

func TestActWithoutFightIsIgnored(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	called := false
	p.sendCombat = func(protocol.Combat) { called = true }
	hp := p.hp
	g.act(p, combat.Attack, 0)
	if called || p.hp != hp {
		t.Fatal("an action outside combat had an effect")
	}
}

func TestRegenerationOutsideFightOnly(t *testing.T) {
	r := newRig(t, testRat)
	r.p.hp = r.p.stats.MaxHP - 3
	r.g.regenerate(r.g.sortedIDs())
	if r.p.hp != r.p.stats.MaxHP-2 {
		t.Fatalf("HP=%d, expected %d", r.p.hp, r.p.stats.MaxHP-2)
	}
	for i := 0; i < 10; i++ { // never exceeds the maximum
		r.g.regenerate(r.g.sortedIDs())
	}
	if r.p.hp != r.p.stats.MaxHP {
		t.Fatalf("HP=%d, cap %d", r.p.hp, r.p.stats.MaxHP)
	}

	r.p.hp = 1
	r.engage()
	r.g.regenerate(r.g.sortedIDs())
	if r.p.hp != 1 {
		t.Fatal("regeneration during a fight")
	}
}

func TestSnapshotCarriesStats(t *testing.T) {
	g := newGame()
	_, r := joinRec(g, "a")
	g.step()
	want := combat.NewStats()
	s := r.last().Stats
	if s.Level != 1 || s.HP != want.MaxHP || s.MaxHP != want.MaxHP || s.Def != want.Defense() || s.Dmg != "1d4" ||
		s.Potions != want.Potions || s.XPNext != want.XPToNext() {
		t.Fatalf("unexpected sheet: %+v", s)
	}
}

func TestStatsChangeTriggersSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	g.step()
	n := len(r.snaps)
	p.hp -= 5
	g.regenerate(g.sortedIDs()) // +1 HP
	g.step()
	if len(r.snaps) != n+1 {
		t.Fatal("an HP change must produce a snapshot")
	}
}

func TestPotionLootIsCappedAndHappens(t *testing.T) {
	withLoot := 0
	const n = 300
	for i := 0; i < n; i++ {
		r := newRig(t, weak(testRat))
		r.g.rng = rand.New(rand.NewPCG(uint64(i), 99))
		r.engage()
		before := r.p.stats.Potions
		r.g.act(r.p, combat.Attack, 0)
		if r.p.stats.Potions > combat.MaxPotions {
			t.Fatalf("more than %d potions: %d", combat.MaxPotions, r.p.stats.Potions)
		}
		if r.p.stats.Potions > before {
			withLoot++
		}
	}
	if withLoot < 50 || withLoot > 140 { // 30% of 300 ≈ 90
		t.Fatalf("%d loots out of %d victories, expected about 30 %%", withLoot, n)
	}
}

// A player at max potions gains nothing, without a misleading message.
func TestNoLootMessageAtPotionCap(t *testing.T) {
	for i := 0; i < 50; i++ {
		r := newRig(t, weak(testRat))
		r.g.rng = rand.New(rand.NewPCG(uint64(i), 5))
		r.p.stats.Potions = combat.MaxPotions
		r.engage()
		r.g.act(r.p, combat.Attack, 0)
		if strings.Contains(strings.Join(r.last().Log, "|"), "potion") {
			t.Fatalf("loot message while the inventory is full: %v", r.last().Log)
		}
	}
}

// Full run through the real loop (goroutine, command queue, callbacks).
func TestFightThroughRunningGame(t *testing.T) {
	g := newGame()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	msgs := make(chan protocol.Combat, 64)
	id, err := g.Join("hero", Handlers{
		Snapshot: func(protocol.Snapshot) {},
		Combat:   func(c protocol.Combat) { msgs <- c },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Inject a weak monster in contact, from the game goroutine.
	ready := make(chan struct{})
	g.do(func() {
		p := g.players[id]
		dx, dy := 1, 0
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if !g.blocked(world.Pos{X: p.pos.X + d[0], Y: p.pos.Y + d[1]}) {
				dx, dy = d[0], d[1]
				break
			}
		}
		g.addMonster(weak(testRat), world.Pos{X: p.pos.X + dx, Y: p.pos.Y + dy})
		close(ready)
	})
	<-ready

	recv := func() protocol.Combat {
		select {
		case c := <-msgs:
			return c
		case <-time.After(3 * time.Second):
			t.Fatal("no combat message received")
			return protocol.Combat{}
		}
	}

	// The monster in contact engages on its next tick.
	if c := recv(); c.Over || len(c.Enemies) != 1 || c.Enemies[0].Kind != "rat" {
		t.Fatalf("opening: %+v", c)
	}
	g.Act(id, "attack", 0)
	if c := recv(); !c.Over || c.Result != protocol.ResultWon {
		t.Fatalf("victory expected: %+v", c)
	}
	g.Act(id, "attack", 0) // outside combat: ignored
	select {
	case c := <-msgs:
		t.Fatalf("unexpected message outside combat: %+v", c)
	case <-time.After(300 * time.Millisecond):
	}
}

// sumXPForLevel returns the total XP needed to reach exactly the given level.
func sumXPForLevel(level int) int {
	total := 0
	for l := 1; l < level; l++ {
		total += 10 * l
	}
	return total
}

func TestCanGroupIsSymmetricAndLogical(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"wolf", "wolf", true}, {"wolf", "rat", true}, {"rat", "wolf", true}, {"goblin", "wolf", true},
		{"skeleton", "knight", true}, {"warlord", "knight", true},
		{"rat", "crab", false}, {"crab", "yeti", false}, {"rat", "knight", false}, {"goblin", "rat", false},
	} {
		if got := canGroup(c.a, c.b); got != c.want {
			t.Errorf("canGroup(%s,%s)=%v, expected %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFightGathersCompatibleNeighbours(t *testing.T) {
	r := newRig(t, testRat)
	at := func(dx, dy int) world.Pos { return world.Pos{X: r.m.pos.X + dx, Y: r.m.pos.Y + dy} }
	var near []*monster
	for _, d := range [][2]int{{1, 0}, {0, 1}, {1, 1}, {2, 0}} { // 3 more neighbours than maxGroup admits
		pos := at(d[0], d[1])
		if r.g.blocked(pos) || r.g.World.Safe(pos.X, pos.Y) {
			continue
		}
		near = append(near, r.g.addMonster(testRat, pos))
	}
	if len(near) < 3 {
		t.Skip("terrain hardly walkable around the test tile")
	}
	far := r.g.addMonster(testRat, at(groupRadius+2, 0))
	crab := r.g.addMonster(species{Kind: "crab", HP: 5, Dmg: dice.D(1, 4, 0), MoveEvery: 5}, at(0, -1))

	r.engage()
	f := r.p.fight
	if f == nil || len(f.ms) != maxGroup {
		t.Fatalf("group of %d, expected %d", len(f.ms), maxGroup)
	}
	if far.fight != nil || crab.fight != nil {
		t.Fatal("a distant or incompatible monster was engaged")
	}
	if got := r.last(); len(got.Enemies) != maxGroup || got.Enemies[0].Name != "Rat 1" {
		t.Fatalf("enemies sent: %+v", got.Enemies)
	}
}

func TestKillsAreRewardedPerMonsterAndFleeReleasesTheRest(t *testing.T) {
	r := newRig(t, weak(testRat))
	extra := r.g.addMonster(testRat, world.Pos{X: r.m.pos.X, Y: r.m.pos.Y + 1})
	if r.g.blocked(extra.pos) {
		t.Skip("neighbouring tile blocked")
	}
	r.engage()
	if r.p.fight == nil || len(r.p.fight.ms) < 2 {
		t.Fatal("the group should have 2 monsters")
	}
	xp := r.p.stats.XP
	for i := 0; i < 200 && r.m.fight != nil; i++ { // hit the weak monster (target 0) until it dies
		r.g.act(r.p, combat.Attack, 0)
		if r.p.fight == nil {
			t.Skip("the player died before slaying the first monster")
		}
	}
	if _, alive := r.g.monsters[r.m.id]; alive {
		t.Fatal("the slain monster must leave the world")
	}
	if r.p.stats.Kills != 1 || (r.p.stats.XP == xp && r.p.stats.Level == 1) {
		t.Fatalf("the first monster's death should be rewarded right away: kills=%d", r.p.stats.Kills)
	}
	if r.p.fight == nil || extra.fight == nil {
		t.Fatal("the fight must continue against the survivor")
	}
	r.p.fight.b.Abandon()
	r.g.endFight(r.p.fight)
	if extra.fight != nil || extra.calm != disengageCooldown {
		t.Fatalf("the survivor must be freed: fight=%v cooldown=%d", extra.fight, extra.calm)
	}
}

func TestBeginnersMeetNoPacks(t *testing.T) {
	r := newRig(t, testRat)
	before := len(r.g.monsters)
	for range 50 {
		r.g.spawnPack(r.m, r.p.pos, 1)
	}
	if len(r.g.monsters) != before {
		t.Fatalf("%d more monsters for a level 1 player", len(r.g.monsters)-before)
	}
}

// Companions that are walking (step cooldown running) join the fight; those that have just fled
// or lost (calm) stay aside.
func TestPackJoinsEvenWhileWalking(t *testing.T) {
	r := newRig(t, testRat)
	var mates []*monster
	for _, d := range [][2]int{{1, 0}, {0, 1}} {
		pos := world.Pos{X: r.m.pos.X + d[0], Y: r.m.pos.Y + d[1]}
		if r.g.blocked(pos) || r.g.World.Safe(pos.X, pos.Y) {
			t.Skip("terrain hardly walkable around the test tile")
		}
		m := r.g.addMonster(testRat, pos)
		m.cooldown = testRat.MoveEvery
		mates = append(mates, m)
	}
	mates[1].calm = disengageCooldown
	r.engage()
	if r.p.fight == nil || len(r.p.fight.ms) != 2 || mates[0].fight == nil || mates[1].fight != nil {
		t.Fatalf("group expected: the walking companion yes, the calmed one no (fight=%v)", r.p.fight)
	}
}

// A fortress lord always leaves an item, one rank above that of its region.
func TestBossDropsGearOfItsRegionLevel(t *testing.T) {
	boss := scaledForLevel(speciesNamed(t, "warlord"), 12)
	boss.HP = 1
	r := newRig(t, boss)
	r.p.stats.MaxHP, r.p.hp = 10_000, 10_000
	r.engage()
	for r.p.fight != nil {
		r.g.act(r.p, combat.Attack, 0)
	}
	if len(r.p.inv.Items) != 1 {
		t.Fatalf("%d items dropped, expected 1", len(r.p.inv.Items))
	}
	if got, want := r.p.inv.Items[0].Tier, gear.TierForLevel(12)+1; got != want {
		t.Errorf("rank %d, expected %d", got, want)
	}
}

// A level 1 monster and a full bag leave nothing; an ordinary monster only rarely leaves one.
func TestMonsterGearIsRareAndNeedsBagRoom(t *testing.T) {
	r := newRig(t, scaledForLevel(testRat, 1))
	drops := 0
	for range 2000 {
		if r.g.monsterGear(r.p, r.m) != "" {
			drops++
			r.p.inv.Items = r.p.inv.Items[:0]
		}
	}
	if drops < 40 || drops > 200 { // lootGearChance = 5 %
		t.Errorf("%d items out of 2000 monsters", drops)
	}
	r.p.inv.Items = make([]gear.Item, gear.BagCapacity)
	for range 200 {
		if r.g.monsterGear(r.p, r.m) != "" {
			t.Fatal("a full bag cannot receive anything")
		}
	}
}

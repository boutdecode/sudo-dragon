package game

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// testRat is the game's real rat: the tests thus follow its balancing.
var testRat = func() species {
	for _, s := range speciesList {
		if s.Kind == "rat" {
			return s
		}
	}
	panic("rat species not found")
}()

// freeWalkable returns the closest walkable, free tile to from among
// those at least d tiles away (search in rings of growing radius).
func freeWalkable(t *testing.T, g *Game, from world.Pos, d int) world.Pos {
	t.Helper()
	for r := d; r <= d+80; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r {
					continue
				}
				pos := world.Pos{X: from.X + dx, Y: from.Y + dy}
				if !g.blocked(pos) {
					return pos
				}
			}
		}
	}
	t.Fatal("no free tile")
	return world.Pos{}
}

func TestSpeciesTable(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range speciesList {
		if seen[s.Kind] {
			t.Errorf("duplicate species: %s", s.Kind)
		}
		seen[s.Kind] = true
		if !s.Garrison && !s.Dungeon && !s.Habitat.Walkable() { // a garrison or dungeon monster has no habitat: it has a post
			t.Errorf("%s: habitat not walkable", s.Kind)
		}
		if (s.Garrison || s.Dungeon) && s.Habitat != 0 {
			t.Errorf("%s: a garrison or dungeon species must not have a habitat", s.Kind)
		}
		if s.Garrison && s.Dungeon {
			t.Errorf("%s: garrison or dungeon, not both", s.Kind)
		}
		if s.Boss && !s.Garrison && !s.Dungeon {
			t.Errorf("%s: a boss is necessarily a garrison or dungeon species", s.Kind)
		}
		if s.MoveEvery <= MoveCooldown {
			t.Errorf("%s: must be slower than a player (MoveEvery=%d)", s.Kind, s.MoveEvery)
		}
		if s.Aggro < 0 {
			t.Errorf("%s: negative aggro", s.Kind)
		}
		if s.name(i18n.FR) == "" || s.HP <= 0 || s.Dmg.Max() <= 0 || s.fighter().Def < 8 || s.Hit < 0 || s.XP <= 0 {
			t.Errorf("%s: invalid combat stats: %+v", s.Kind, s)
		}
		if SpeciesName(i18n.FR, s.Kind) != s.name(i18n.FR) {
			t.Errorf("%s: inconsistent SpeciesName", s.Kind)
		}
	}
	if spawnHalfW > keepHalfW || spawnHalfH > keepHalfH {
		t.Error("the spawn area must be within the retention area")
	}
}

func TestSpawnOutsideViewOnHabitat(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	for i := 0; i < 300; i++ {
		g.spawnMonsters()
	}
	if len(g.monsters) == 0 {
		t.Fatal("no monster spawned")
	}
	for _, m := range g.monsters {
		if inBox(m.pos, p.pos, protocol.ViewHalfW+spawnViewMargin, protocol.ViewHalfH+spawnViewMargin) {
			t.Errorf("%s spawned in view: %v (player %v)", m.sp.Kind, m.pos, p.pos)
		}
		if tile := g.World.At(m.pos.X, m.pos.Y); tile != m.sp.Habitat {
			t.Errorf("%s spawned on %v, habitat %v", m.sp.Kind, tile, m.sp.Habitat)
		}
		if g.monsterAt[m.pos] != m.id {
			t.Errorf("inconsistent monsterAt index for %d", m.id)
		}
	}
	if len(g.monsterAt) != len(g.monsters) {
		t.Errorf("monsterAt=%d, monsters=%d (two monsters on one tile?)", len(g.monsterAt), len(g.monsters))
	}
}

func TestSpawnCapPerPlayer(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	for i := 0; i < 500; i++ {
		g.spawnMonsters()
	}
	if n := g.monstersAround(p.pos); n > maxPerPlayer {
		t.Fatalf("%d monsters around a player, maximum %d", n, maxPerPlayer)
	}
}

func TestSpawnDeterministicWithSeededRng(t *testing.T) {
	run := func() []world.Pos {
		g := newGame()
		joinRec(g, "a")
		g.rng = rand.New(rand.NewPCG(1, 2))
		for i := 0; i < 50; i++ {
			g.spawnMonsters()
		}
		var out []world.Pos
		for _, id := range g.sortedMonsterIDs() {
			out = append(out, g.monsters[id].pos)
		}
		return out
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("%d vs %d monsters", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("divergence at monster %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestMonsterChasesPlayerAndStopsAdjacent(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	start := world.Pos{X: p.pos.X, Y: p.pos.Y}
	// Place the monster 4 tiles away on a free line if possible, otherwise search.
	var mpos world.Pos
	found := false
	for dx := 4; dx >= 2 && !found; dx-- {
		cand := world.Pos{X: start.X + dx, Y: start.Y}
		clear := true
		for x := start.X + 1; x <= cand.X; x++ {
			if g.blocked(world.Pos{X: x, Y: start.Y}) {
				clear = false
			}
		}
		if clear {
			mpos, found = cand, true
		}
	}
	if !found {
		t.Skip("no free line east of the spawn")
	}
	m := g.addMonster(testRat, mpos)

	last := cheb(m.pos, p.pos)
	for i := 0; i < 60; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		d := cheb(m.pos, p.pos)
		if d > last {
			t.Fatalf("the monster moves away (%d -> %d)", last, d)
		}
		last = d
		if m.pos == p.pos {
			t.Fatal("the monster entered the player's tile")
		}
	}
	if last != 1 {
		t.Fatalf("final distance %d, expected 1 (in contact)", last)
	}
}

func TestMonsterIgnoresPlayerOutOfAggro(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	pos := freeWalkable(t, g, p.pos, testRat.Aggro+10)
	m := g.addMonster(testRat, pos)
	for i := 0; i < 300; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		if cheb(m.pos, m.home) > wanderLeash {
			t.Fatalf("the monster left its leash: %v (origin %v)", m.pos, m.home)
		}
	}
}

func TestWanderMovesAndRespectsLeash(t *testing.T) {
	g := newGame()
	pos := g.World.FindSpawn()
	m := g.addMonster(testRat, pos)
	moved := false
	for i := 0; i < 1000; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		if m.pos != pos {
			moved = true
		}
		if cheb(m.pos, m.home) > wanderLeash {
			t.Fatalf("off leash: %v", m.pos)
		}
		if !g.World.At(m.pos.X, m.pos.Y).Walkable() {
			t.Fatalf("monster on impassable terrain: %v", m.pos)
		}
	}
	if !moved {
		t.Fatal("the monster never moved")
	}
	if g.monsterAt[m.pos] != m.id || len(g.monsterAt) != 1 {
		t.Fatalf("inconsistent monsterAt index: %v", g.monsterAt)
	}
}

func TestMonsterSpeed(t *testing.T) {
	g := newGame()
	pos := g.World.FindSpawn()
	m := g.addMonster(testRat, pos)
	// Over N ticks, it cannot take more than N/MoveEvery+1 steps.
	steps, prev := 0, m.pos
	const ticks = 600
	for i := 0; i < ticks; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		if m.pos != prev {
			steps++
			prev = m.pos
		}
	}
	if max := ticks/testRat.MoveEvery + 1; steps > max {
		t.Fatalf("%d steps in %d ticks, maximum %d", steps, ticks, max)
	}
}

func TestDespawnFar(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	near := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	far := g.addMonster(testRat, freeWalkable(t, g, world.Pos{X: p.pos.X + keepHalfW + 10, Y: p.pos.Y}, 0))
	if inBox(far.pos, p.pos, keepHalfW, keepHalfH) {
		t.Skip("distant tile not found")
	}
	farID, nearID := far.id, near.id
	g.despawnFar()
	if _, ok := g.monsters[farID]; ok {
		t.Error("the distant monster should have disappeared")
	}
	if _, ok := g.monsters[nearID]; !ok {
		t.Error("the nearby monster should not have disappeared")
	}
	if len(g.monsterAt) != 1 {
		t.Errorf("monsterAt=%d, expected 1", len(g.monsterAt))
	}
}

func TestDespawnAllWhenNoPlayers(t *testing.T) {
	g := newGame()
	g.addMonster(testRat, g.World.FindSpawn())
	g.despawnFar()
	if len(g.monsters) != 0 || len(g.monsterAt) != 0 {
		t.Fatal("without a player, all monsters must disappear")
	}
}

func TestMonsterBlocksPlayer(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	dx, dy := freeDir(t, g, p)
	target := world.Pos{X: p.pos.X + dx, Y: p.pos.Y + dy}
	g.addMonster(testRat, target)

	before := p.pos
	g.move(p.id, dx, dy)
	g.step()
	if p.pos != before {
		t.Fatalf("the player walked through a monster: %v", p.pos)
	}
}

func TestSnapshotContainsOnlyVisibleMonsters(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	near := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	if !inBox(near.pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
		t.Skip("nearby tile out of view")
	}
	g.addMonster(testRat, freeWalkable(t, g, world.Pos{X: p.pos.X + protocol.ViewHalfW + 5, Y: p.pos.Y}, 0))

	g.step()
	snap := r.last()
	if len(snap.Monsters) != 1 || snap.Monsters[0].ID != near.id || snap.Monsters[0].Kind != "rat" {
		t.Fatalf("snapshot monsters: %+v", snap.Monsters)
	}
}

func TestMonsterMoveTriggersSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	m := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	m.home = m.pos
	g.step()
	n := len(r.snaps)

	// Force a monster move.
	var moved bool
	for _, d := range dirs8 {
		if g.moveMonster(m, d[0], d[1]) {
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("monster stuck")
	}
	g.sendSnapshot(p, g.sortedMonsterIDs())
	if len(r.snaps) != n+1 {
		t.Fatal("the move of a visible monster should produce a snapshot")
	}
}

func TestStepRunsMonsterLoopPeriodically(t *testing.T) {
	g := newGame()
	joinRec(g, "a")
	for i := 0; i < spawnEvery*40; i++ {
		g.step()
	}
	if len(g.monsters) == 0 {
		t.Fatal("the game loop spawned no monster")
	}
}

package game

import (
	"math/rand/v2"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// testRat est le vrai rat du jeu: les tests suivent ainsi son équilibrage.
var testRat = func() species {
	for _, s := range speciesList {
		if s.Kind == "rat" {
			return s
		}
	}
	panic("espèce rat introuvable")
}()

// freeWalkable renvoie la case praticable et libre la plus proche de from parmi
// celles à au moins d tuiles (recherche en anneaux de rayon croissant).
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
	t.Fatal("aucune case libre")
	return world.Pos{}
}

func TestSpeciesTable(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range speciesList {
		if seen[s.Kind] {
			t.Errorf("espèce en double: %s", s.Kind)
		}
		seen[s.Kind] = true
		if !s.Garrison && !s.Habitat.Walkable() { // une garnison n'a pas d'habitat: elle a un poste
			t.Errorf("%s: habitat non praticable", s.Kind)
		}
		if s.Garrison && s.Habitat != 0 {
			t.Errorf("%s: une espèce de garnison ne doit pas avoir d'habitat", s.Kind)
		}
		if s.Boss && !s.Garrison {
			t.Errorf("%s: un boss est forcément une espèce de garnison", s.Kind)
		}
		if s.MoveEvery <= MoveCooldown {
			t.Errorf("%s: doit être plus lent qu'un joueur (MoveEvery=%d)", s.Kind, s.MoveEvery)
		}
		if s.Aggro < 0 {
			t.Errorf("%s: aggro négatif", s.Kind)
		}
		if s.name(i18n.FR) == "" || s.HP <= 0 || s.Dmg.Max() <= 0 || s.fighter().Def < 8 || s.Hit < 0 || s.XP <= 0 {
			t.Errorf("%s: caractéristiques de combat invalides: %+v", s.Kind, s)
		}
		if SpeciesName(i18n.FR, s.Kind) != s.name(i18n.FR) {
			t.Errorf("%s: SpeciesName incohérent", s.Kind)
		}
	}
	if spawnHalfW > keepHalfW || spawnHalfH > keepHalfH {
		t.Error("la zone d'apparition doit être dans la zone de conservation")
	}
}

func TestSpawnOutsideViewOnHabitat(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	for i := 0; i < 300; i++ {
		g.spawnMonsters()
	}
	if len(g.monsters) == 0 {
		t.Fatal("aucun monstre n'est apparu")
	}
	for _, m := range g.monsters {
		if inBox(m.pos, p.pos, protocol.ViewHalfW+spawnViewMargin, protocol.ViewHalfH+spawnViewMargin) {
			t.Errorf("%s apparu dans la vue: %v (joueur %v)", m.sp.Kind, m.pos, p.pos)
		}
		if tile := g.World.At(m.pos.X, m.pos.Y); tile != m.sp.Habitat {
			t.Errorf("%s apparu sur %v, habitat %v", m.sp.Kind, tile, m.sp.Habitat)
		}
		if g.monsterAt[m.pos] != m.id {
			t.Errorf("index monsterAt incohérent pour %d", m.id)
		}
	}
	if len(g.monsterAt) != len(g.monsters) {
		t.Errorf("monsterAt=%d, monsters=%d (deux monstres sur une case ?)", len(g.monsterAt), len(g.monsters))
	}
}

func TestSpawnCapPerPlayer(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	for i := 0; i < 500; i++ {
		g.spawnMonsters()
	}
	if n := g.monstersAround(p.pos); n > maxPerPlayer {
		t.Fatalf("%d monstres autour d'un joueur, maximum %d", n, maxPerPlayer)
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
		t.Fatalf("%d vs %d monstres", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("divergence au monstre %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestMonsterChasesPlayerAndStopsAdjacent(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	start := world.Pos{X: p.pos.X, Y: p.pos.Y}
	// Place le monstre à 4 cases sur une ligne libre si possible, sinon cherche.
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
		t.Skip("pas de ligne libre à l'est du spawn")
	}
	m := g.addMonster(testRat, mpos)

	last := cheb(m.pos, p.pos)
	for i := 0; i < 60; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		d := cheb(m.pos, p.pos)
		if d > last {
			t.Fatalf("le monstre s'éloigne (%d -> %d)", last, d)
		}
		last = d
		if m.pos == p.pos {
			t.Fatal("le monstre est entré sur la case du joueur")
		}
	}
	if last != 1 {
		t.Fatalf("distance finale %d, attendu 1 (au contact)", last)
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
			t.Fatalf("le monstre a quitté sa laisse: %v (origine %v)", m.pos, m.home)
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
			t.Fatalf("hors laisse: %v", m.pos)
		}
		if !g.World.At(m.pos.X, m.pos.Y).Walkable() {
			t.Fatalf("monstre sur terrain infranchissable: %v", m.pos)
		}
	}
	if !moved {
		t.Fatal("le monstre n'a jamais bougé")
	}
	if g.monsterAt[m.pos] != m.id || len(g.monsterAt) != 1 {
		t.Fatalf("index monsterAt incohérent: %v", g.monsterAt)
	}
}

func TestMonsterSpeed(t *testing.T) {
	g := newGame()
	pos := g.World.FindSpawn()
	m := g.addMonster(testRat, pos)
	// Sur N ticks, il ne peut pas faire plus de N/MoveEvery+1 pas.
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
		t.Fatalf("%d pas en %d ticks, maximum %d", steps, ticks, max)
	}
}

func TestDespawnFar(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	near := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	far := g.addMonster(testRat, freeWalkable(t, g, world.Pos{X: p.pos.X + keepHalfW + 10, Y: p.pos.Y}, 0))
	if inBox(far.pos, p.pos, keepHalfW, keepHalfH) {
		t.Skip("case lointaine introuvable")
	}
	farID, nearID := far.id, near.id
	g.despawnFar()
	if _, ok := g.monsters[farID]; ok {
		t.Error("le monstre lointain devrait avoir disparu")
	}
	if _, ok := g.monsters[nearID]; !ok {
		t.Error("le monstre proche ne devrait pas avoir disparu")
	}
	if len(g.monsterAt) != 1 {
		t.Errorf("monsterAt=%d, attendu 1", len(g.monsterAt))
	}
}

func TestDespawnAllWhenNoPlayers(t *testing.T) {
	g := newGame()
	g.addMonster(testRat, g.World.FindSpawn())
	g.despawnFar()
	if len(g.monsters) != 0 || len(g.monsterAt) != 0 {
		t.Fatal("sans joueur, tous les monstres doivent disparaître")
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
		t.Fatalf("le joueur a traversé un monstre: %v", p.pos)
	}
}

func TestSnapshotContainsOnlyVisibleMonsters(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	near := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	if !inBox(near.pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
		t.Skip("case proche hors vue")
	}
	g.addMonster(testRat, freeWalkable(t, g, world.Pos{X: p.pos.X + protocol.ViewHalfW + 5, Y: p.pos.Y}, 0))

	g.step()
	snap := r.last()
	if len(snap.Monsters) != 1 || snap.Monsters[0].ID != near.id || snap.Monsters[0].Kind != "rat" {
		t.Fatalf("monstres du snapshot: %+v", snap.Monsters)
	}
}

func TestMonsterMoveTriggersSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	m := g.addMonster(testRat, freeWalkable(t, g, p.pos, 3))
	m.home = m.pos
	g.step()
	n := len(r.snaps)

	// Force un déplacement du monstre.
	var moved bool
	for _, d := range dirs8 {
		if g.moveMonster(m, d[0], d[1]) {
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("monstre coincé")
	}
	g.sendSnapshot(p, g.sortedMonsterIDs())
	if len(r.snaps) != n+1 {
		t.Fatal("le déplacement d'un monstre visible devrait produire un instantané")
	}
}

func TestStepRunsMonsterLoopPeriodically(t *testing.T) {
	g := newGame()
	joinRec(g, "a")
	for i := 0; i < spawnEvery*40; i++ {
		g.step()
	}
	if len(g.monsters) == 0 {
		t.Fatal("la boucle de jeu n'a fait apparaître aucun monstre")
	}
}

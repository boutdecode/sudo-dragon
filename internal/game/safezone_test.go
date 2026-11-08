package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// village renvoie le village de départ du monde de test.
func village(t *testing.T, g *Game) *world.Structure {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 250) {
		if s.Kind == world.KindVillage {
			return s
		}
	}
	t.Skip("pas de village près du départ")
	return nil
}

// insideVillage renvoie une case libre, dans la zone sûre, loin des bords.
func insideVillage(t *testing.T, g *Game, s *world.Structure) world.Pos {
	t.Helper()
	return freeWalkable(t, g, s.Center(), 0)
}

// outsideEdge renvoie une case libre juste hors de la zone sûre (à l'ouest du village).
func outsideEdge(t *testing.T, g *Game, s *world.Structure) world.Pos {
	t.Helper()
	y := s.Center().Y
	for dx := 0; dx < 12; dx++ {
		pos := world.Pos{X: s.Origin.X - 3 - 1 - dx, Y: y} // 3 = marge de la zone sûre
		if !g.World.Safe(pos.X, pos.Y) && !g.blocked(pos) {
			return pos
		}
	}
	t.Skip("pas de case libre hors de la zone sûre à l'ouest du village")
	return world.Pos{}
}

func TestPlayerInSafeZoneIsFlagged(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	if p.safe {
		t.Fatal("le point de départ ne doit pas être en zone sûre")
	}
	place(g, p, insideVillage(t, g, s))
	if !p.safe {
		t.Fatal("le drapeau « zone sûre » n'est pas posé dans le village")
	}
	place(g, p, outsideEdge(t, g, s))
	if p.safe {
		t.Fatal("le drapeau « zone sûre » n'est pas retombé hors du village")
	}
}

func TestMonsterCannotEnterSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	edge := outsideEdge(t, g, s)
	m := g.addMonster(testRat, edge)

	// Vers l'intérieur: refusé. Vers l'extérieur: permis.
	if g.moveMonster(m, 1, 0) && g.World.Safe(m.pos.X, m.pos.Y) {
		t.Fatal("un monstre est entré dans la zone sûre")
	}
	if !g.World.Safe(edge.X+1, edge.Y) {
		t.Skip("la case voisine n'est pas en zone sûre: la lisière est ailleurs")
	}
	m.pos = edge
	g.monsterAt = map[world.Pos]int{edge: m.id}
	if g.moveMonster(m, 1, 0) {
		t.Fatal("un monstre a franchi la lisière de la zone sûre")
	}
	if !g.moveMonster(m, -1, 0) {
		t.Fatal("un monstre devrait pouvoir s'éloigner de la zone sûre")
	}
}

func TestMonstersNeverWanderIntoSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	m := g.addMonster(testRat, outsideEdge(t, g, s))
	for i := 0; i < 3000; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		if g.World.Safe(m.pos.X, m.pos.Y) {
			t.Fatalf("le monstre erre en zone sûre (tick %d): %v", i, m.pos)
		}
	}
}

// Le même monstre, à la même distance : il attaque un joueur dehors, pas un joueur dedans.
func TestMonsterIgnoresPlayerInSafeZoneButNotOutside(t *testing.T) {
	g := newGame()
	s := village(t, g)
	edge := outsideEdge(t, g, s)
	inside := world.Pos{X: edge.X + 5, Y: edge.Y} // à 5 cases: dans la zone, à portée de détection du rat
	if !g.World.Safe(inside.X, inside.Y) || g.blocked(inside) {
		t.Skip("géométrie de test indisponible")
	}

	p, _ := joinRec(g, "a")
	place(g, p, inside)
	m := g.addMonster(testRat, edge)
	for i := 0; i < 200; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	if p.fight != nil {
		t.Fatal("un monstre a attaqué un joueur qui se trouve en zone sûre")
	}

	// Contrôle: le joueur sort de la zone, à la même distance. Le monstre le poursuit.
	out := world.Pos{X: edge.X - 3, Y: edge.Y}
	if g.blocked(out) || g.World.Safe(out.X, out.Y) {
		t.Skip("case de contrôle indisponible")
	}
	place(g, p, out)
	m.pos = edge
	g.monsterAt = map[world.Pos]int{edge: m.id}
	m.cooldown = 0
	for i := 0; i < 200 && p.fight == nil; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
	}
	if p.fight == nil {
		t.Fatal("le monstre n'attaque pas un joueur hors de la zone sûre (le test de contrôle échoue)")
	}
}

func TestNoBiomeSpawnInSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, insideVillage(t, g, s))
	for i := 0; i < 800; i++ {
		g.spawnMonsters()
	}
	if len(g.monsters) == 0 {
		t.Fatal("aucun monstre n'est apparu: le test ne prouve rien")
	}
	for _, m := range g.monsters {
		if g.World.Safe(m.pos.X, m.pos.Y) {
			t.Fatalf("%s apparu en zone sûre: %v", m.sp.Kind, m.pos)
		}
	}
}

func TestSafeZoneHealsFaster(t *testing.T) {
	g := newGame()
	s := village(t, g)
	safe, _ := joinRec(g, "safe")
	wild, _ := joinRec(g, "wild")
	place(g, safe, insideVillage(t, g, s))
	safe.hp, wild.hp = 1, 1

	for i := 0; i < regenEvery; i++ { // 3 secondes
		g.step()
	}
	if got := safe.hp - 1; got != regenEvery/safeRegenEvery {
		t.Fatalf("en zone sûre: +%d PV en %d ticks, attendu +%d", got, regenEvery, regenEvery/safeRegenEvery)
	}
	if got := wild.hp - 1; got != 1 {
		t.Fatalf("hors zone sûre: +%d PV en %d ticks, attendu +1", got, regenEvery)
	}
	// Jamais au-delà du maximum.
	for i := 0; i < 2000; i++ {
		g.step()
	}
	if safe.hp != safe.stats.MaxHP || wild.hp != wild.stats.MaxHP {
		t.Fatalf("PV après longue attente: %d et %d, maximum %d", safe.hp, wild.hp, safe.stats.MaxHP)
	}
}

func TestNoHealingDuringFightEvenInSafeZone(t *testing.T) {
	g := newGame()
	s := village(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, insideVillage(t, g, s))
	// Le joueur attaque un monstre posé à côté de lui, puis ne joue plus.
	dx, dy := freeDir(t, g, p)
	m := g.addMonster(deadly(testRat), world.Pos{X: p.pos.X + dx, Y: p.pos.Y + dy})
	g.startFight(p, m, true)
	p.hp = 3
	for i := 0; i < 100; i++ {
		g.healDue(g.sortedIDs())
		g.regenerate(g.healDue(g.sortedIDs()))
	}
	if p.hp != 3 {
		t.Fatalf("PV = %d: soigné pendant un combat", p.hp)
	}
}

// --- Or ---

func TestGoldRewardRange(t *testing.T) {
	g := newGame()
	for _, sp := range speciesList {
		lo, hi := sp.XP/2, sp.XP
		seen := map[int]bool{}
		for i := 0; i < 500; i++ {
			got := g.goldReward(sp)
			if got < lo || got > hi {
				t.Fatalf("%s: or %d hors de [%d, %d]", sp.Kind, got, lo, hi)
			}
			seen[got] = true
		}
		if len(seen) < 2 && hi > lo {
			t.Fatalf("%s: l'or ne varie jamais", sp.Kind)
		}
	}
}

func TestWinGivesGoldAndSaysSo(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack)

	if r.p.stats.Gold < testRat.XP/2 || r.p.stats.Gold > testRat.XP {
		t.Fatalf("or = %d, attendu entre %d et %d", r.p.stats.Gold, testRat.XP/2, testRat.XP)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "or.") {
		t.Fatalf("le journal ne mentionne pas l'or: %v", r.last().Log)
	}
}

func TestLosingOrFleeingGivesNoGold(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.hp = 1
	r.engage()
	r.g.act(r.p, combat.Attack) // perdu
	if r.p.stats.Gold != 0 {
		t.Fatalf("or = %d après une défaite", r.p.stats.Gold)
	}
}

func TestGoldIsCappedOnWin(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.Gold = combat.MaxGold
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Gold != combat.MaxGold {
		t.Fatalf("or = %d, plafond %d", r.p.stats.Gold, combat.MaxGold)
	}
	if strings.Contains(strings.Join(r.last().Log, "|"), "or.") {
		t.Fatalf("message d'or alors que rien n'a été ajouté: %v", r.last().Log)
	}
}

func TestGoldInSnapshotAndPersistence(t *testing.T) {
	g, sv := gameWithSaves()
	p, rec := joinRec(g, "a")
	p.stats.Gold = 321
	g.step()
	if got := rec.last().Stats.Gold; got != 321 {
		t.Fatalf("or du snapshot = %d", got)
	}

	g.leave(p.id)
	saved, ok := sv.last("a")
	if !ok || saved.Stats.Gold != 321 {
		t.Fatalf("or sauvegardé: %+v (ok=%v)", saved.Stats, ok)
	}

	g2 := newGame()
	q := g2.joinWith("a", func(protocol.Snapshot) {}, &saved)
	if q.stats.Gold != 321 {
		t.Fatalf("or restauré = %d", q.stats.Gold)
	}
}

func TestRestoredGoldIsClamped(t *testing.T) {
	g := newGame()
	for in, want := range map[int]int{-9: 0, 5: 5, combat.MaxGold + 10: combat.MaxGold} {
		saved := &Saved{Stats: combat.NewStats().WithGold(0), HP: 10, Pos: g.World.FindSpawn()}
		saved.Stats.Gold = in // valeur brute, comme lue dans une base corrompue
		p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
		if p.stats.Gold != want {
			t.Errorf("or restauré depuis %d = %d, attendu %d", in, p.stats.Gold, want)
		}
		g.leave(p.id)
	}
}

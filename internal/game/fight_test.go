package game

import (
	"context"
	"github.com/kevin/ascii-adventure/internal/dice"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// rig prépare un joueur et un monstre voisin, avec capture des messages de combat.
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
	r := &rig{g: g, p: p, snaps: rec}
	p.sendCombat = func(c protocol.Combat) { r.msgs = append(r.msgs, c) }
	r.dx, r.dy = freeDir(t, g, p)
	r.m = g.addMonster(sp, world.Pos{X: p.pos.X + r.dx, Y: p.pos.Y + r.dy})
	return r
}

// engage fait marcher le joueur sur le monstre.
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
		t.Fatal("le combat n'a pas démarré")
	}
	if r.p.pos != start {
		t.Fatalf("le joueur a bougé: %v", r.p.pos)
	}
	if len(r.msgs) != 1 {
		t.Fatalf("%d messages de combat, attendu 1", len(r.msgs))
	}
	c := r.last()
	if c.Over || c.Monster != "rat" || c.MonsterHP != testRat.HP || c.PlayerHP != r.p.stats.MaxHP || c.Round != 0 {
		t.Fatalf("état initial: %+v", c)
	}
	if len(c.Log) != 1 || !strings.Contains(c.Log[0], "Vous attaquez le Rat") {
		t.Fatalf("journal d'ouverture: %v", c.Log)
	}
}

func TestMonsterContactStartsFight(t *testing.T) {
	r := newRig(t, testRat)
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("un monstre au contact devrait attaquer")
	}
	if c := r.last(); len(c.Log) != 1 || !strings.Contains(c.Log[0], "Un Rat vous attaque") {
		t.Fatalf("journal d'ouverture: %v", c.Log)
	}
}

func TestEveryoneIsFrozenDuringFight(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	pPos, mPos := r.p.pos, r.m.pos

	// Le joueur essaie de s'éloigner; le monstre ne bouge pas non plus.
	for i := 0; i < 30; i++ {
		r.g.move(r.p.id, -r.dx, -r.dy)
		r.g.step()
	}
	if r.p.pos != pPos || r.m.pos != mPos {
		t.Fatalf("déplacement pendant un combat: joueur %v->%v, monstre %v->%v", pPos, r.p.pos, mPos, r.m.pos)
	}
	if r.p.hasPending {
		t.Fatal("une intention de déplacement est restée en attente")
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
		t.Fatal("un second monstre s'est joint au combat")
	}
	if cheb(other.pos, r.p.pos) < 2 {
		t.Fatalf("le second monstre s'est collé au joueur en combat: %v", other.pos)
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
		t.Fatalf("combattants non signalés à l'observateur (joueur=%v, monstre=%v): %+v", foundPlayer, foundMonster, snap)
	}
	if !r.snaps.last().Self.Fighting {
		t.Fatal("le joueur ne se voit pas en combat")
	}
}

func TestWinGivesXPRemovesMonsterAndFreesPlayer(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.engage()
	r.g.act(r.p, combat.Attack)

	c := r.last()
	if !c.Over || c.Result != protocol.ResultWon {
		t.Fatalf("victoire attendue: %+v", c)
	}
	if r.p.fight != nil {
		t.Fatal("joueur toujours en combat")
	}
	if _, ok := r.g.monsters[r.m.id]; ok || len(r.g.monsterAt) != 0 {
		t.Fatal("monstre vaincu encore dans le monde")
	}
	if r.p.stats.Level != 1 || r.p.stats.XP != testRat.XP {
		t.Fatalf("niveau %d, XP %d, attendu niveau 1 et %d XP", r.p.stats.Level, r.p.stats.XP, testRat.XP)
	}
	if !strings.Contains(strings.Join(c.Log, "|"), "XP") {
		t.Fatalf("journal sans récompense: %v", c.Log)
	}
}

func TestLevelUpRestoresHP(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.XP = 9 // +4 XP franchit le palier de 10
	r.p.hp = 3
	r.engage()
	r.g.act(r.p, combat.Attack)

	if r.p.stats.Level != 2 {
		t.Fatalf("niveau %d, attendu 2", r.p.stats.Level)
	}
	if r.p.hp != r.p.stats.MaxHP {
		t.Fatalf("PV %d/%d après montée de niveau", r.p.hp, r.p.stats.MaxHP)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Niveau 2") {
		t.Fatalf("journal sans montée de niveau: %v", r.last().Log)
	}
}

func TestLossRespawnsWithPenalty(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.stats.XP = 8
	r.p.hp = 5
	old := r.p.pos
	r.engage()
	r.g.act(r.p, combat.Attack)

	c := r.last()
	if !c.Over || c.Result != protocol.ResultLost {
		t.Fatalf("défaite attendue: %+v", c)
	}
	if r.p.fight != nil || r.m.fight != nil {
		t.Fatal("combattants non libérés")
	}
	if r.p.hp != r.p.stats.MaxHP || r.p.stats.XP != 6 || r.p.stats.Level != 1 {
		t.Fatalf("après défaite: PV %d/%d, XP %d, niveau %d", r.p.hp, r.p.stats.MaxHP, r.p.stats.XP, r.p.stats.Level)
	}
	if r.p.pos == old {
		t.Fatal("le joueur n'a pas été replacé au point de départ")
	}
	if _, stale := r.g.byPos[old]; stale {
		t.Fatal("ancienne case jamais libérée")
	}
	if r.g.byPos[r.p.pos] != r.p.id {
		t.Fatal("nouvelle case non indexée")
	}
	if _, alive := r.g.monsters[r.m.id]; !alive || r.m.cooldown != disengageCooldown {
		t.Fatal("le monstre vainqueur doit rester, avec un délai avant de ré-engager")
	}
}

func TestFleeFreesBothAndMonsterWaits(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000 // survit aux fuites ratées
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee)
	}
	if r.p.fight != nil {
		t.Fatal("la fuite n'a jamais réussi en 200 essais")
	}
	if c := r.last(); !c.Over || c.Result != protocol.ResultFled {
		t.Fatalf("issue de fuite attendue: %+v", c)
	}
	if r.m.fight != nil || r.m.cooldown != disengageCooldown {
		t.Fatalf("monstre: fight=%v cooldown=%d", r.m.fight, r.m.cooldown)
	}

	// Le joueur reste collé au monstre, qui ne ré-engage qu'après le délai.
	for i := 0; i < disengageCooldown-1; i++ {
		r.g.stepMonsters(r.g.sortedMonsterIDs())
		if r.p.fight != nil {
			t.Fatalf("ré-engagé trop tôt (itération %d)", i)
		}
	}
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("le monstre aurait dû ré-engager après le délai")
	}
}

func TestLeaveDuringFightFreesMonster(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	r.g.leave(r.p.id)
	if r.m.fight != nil {
		t.Fatal("monstre resté prisonnier d'un combat fantôme")
	}
	if _, ok := r.g.monsters[r.m.id]; !ok {
		t.Fatal("le monstre aurait dû rester dans le monde")
	}
	if r.m.cooldown != disengageCooldown {
		t.Fatalf("cooldown=%d", r.m.cooldown)
	}
}

func TestIdleFightExpires(t *testing.T) {
	r := newRig(t, testRat)
	r.engage()
	r.g.tick += fightIdleTicks // pile à la limite: pas encore expiré
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight == nil {
		t.Fatal("expiré trop tôt")
	}
	r.g.tick++
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight != nil || r.m.fight != nil {
		t.Fatal("le combat inactif aurait dû expirer")
	}
	if c := r.last(); !c.Over || c.Result != protocol.ResultFled || !strings.Contains(strings.Join(c.Log, "|"), "inactivité") {
		t.Fatalf("message d'expiration: %+v", c)
	}
}

func TestActionResetsIdleTimer(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	r.g.tick += fightIdleTicks
	r.g.act(r.p, combat.Defend) // le joueur agit: le chronomètre repart
	r.g.tick += fightIdleTicks
	r.g.expireIdleFights(r.g.sortedIDs())
	if r.p.fight == nil {
		t.Fatal("un joueur actif ne doit pas expirer")
	}
}

func TestActWithoutFightIsIgnored(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	called := false
	p.sendCombat = func(protocol.Combat) { called = true }
	hp := p.hp
	g.act(p, combat.Attack)
	if called || p.hp != hp {
		t.Fatal("une action hors combat a eu un effet")
	}
}

func TestRegenerationOutsideFightOnly(t *testing.T) {
	r := newRig(t, testRat)
	r.p.hp = r.p.stats.MaxHP - 3
	r.g.regenerate(r.g.sortedIDs())
	if r.p.hp != r.p.stats.MaxHP-2 {
		t.Fatalf("PV=%d, attendu %d", r.p.hp, r.p.stats.MaxHP-2)
	}
	for i := 0; i < 10; i++ { // ne dépasse jamais le maximum
		r.g.regenerate(r.g.sortedIDs())
	}
	if r.p.hp != r.p.stats.MaxHP {
		t.Fatalf("PV=%d, plafond %d", r.p.hp, r.p.stats.MaxHP)
	}

	r.p.hp = 1
	r.engage()
	r.g.regenerate(r.g.sortedIDs())
	if r.p.hp != 1 {
		t.Fatal("régénération pendant un combat")
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
		t.Fatalf("fiche inattendue: %+v", s)
	}
}

func TestStatsChangeTriggersSnapshot(t *testing.T) {
	g := newGame()
	p, r := joinRec(g, "a")
	g.step()
	n := len(r.snaps)
	p.hp -= 5
	g.regenerate(g.sortedIDs()) // +1 PV
	g.step()
	if len(r.snaps) != n+1 {
		t.Fatal("un changement de PV doit produire un instantané")
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
		r.g.act(r.p, combat.Attack)
		if r.p.stats.Potions > combat.MaxPotions {
			t.Fatalf("plus de %d potions: %d", combat.MaxPotions, r.p.stats.Potions)
		}
		if r.p.stats.Potions > before {
			withLoot++
		}
	}
	if withLoot < 50 || withLoot > 140 { // 30 % de 300 ≈ 90
		t.Fatalf("%d butins sur %d victoires, attendu environ 30 %%", withLoot, n)
	}
}

// Un joueur au maximum de potions ne gagne rien, sans message trompeur.
func TestNoLootMessageAtPotionCap(t *testing.T) {
	for i := 0; i < 50; i++ {
		r := newRig(t, weak(testRat))
		r.g.rng = rand.New(rand.NewPCG(uint64(i), 5))
		r.p.stats.Potions = combat.MaxPotions
		r.engage()
		r.g.act(r.p, combat.Attack)
		if strings.Contains(strings.Join(r.last().Log, "|"), "potion") {
			t.Fatalf("message de butin alors que l'inventaire est plein: %v", r.last().Log)
		}
	}
}

// Parcours complet à travers la vraie boucle (goroutine, file de commandes, callbacks).
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

	// Injecte un monstre faible au contact, depuis la goroutine de jeu.
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
			t.Fatal("aucun message de combat reçu")
			return protocol.Combat{}
		}
	}

	// Le monstre au contact engage à son prochain tick.
	if c := recv(); c.Over || c.Monster != "rat" {
		t.Fatalf("ouverture: %+v", c)
	}
	g.Act(id, "attack")
	if c := recv(); !c.Over || c.Result != protocol.ResultWon {
		t.Fatalf("victoire attendue: %+v", c)
	}
	g.Act(id, "attack") // hors combat: ignoré
	select {
	case c := <-msgs:
		t.Fatalf("message inattendu hors combat: %+v", c)
	case <-time.After(300 * time.Millisecond):
	}
}

// sumXPForLevel renvoie l'XP totale pour atteindre exactement le niveau donné.
func sumXPForLevel(level int) int {
	total := 0
	for l := 1; l < level; l++ {
		total += 10 * l
	}
	return total
}

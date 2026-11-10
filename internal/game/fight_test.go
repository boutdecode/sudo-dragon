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
	if c.Over || len(c.Enemies) != 1 || c.Enemies[0].Kind != "rat" || c.Enemies[0].HP != testRat.HP || c.PlayerHP != r.p.stats.MaxHP || c.Round != 0 {
		t.Fatalf("état initial: %+v", c)
	}
	if len(c.Log) < 2 || !strings.Contains(c.Log[0], "Vous attaquez le Rat") || !strings.Contains(c.Log[1], "Initiative") {
		t.Fatalf("journal d'ouverture: %v", c.Log)
	}
}

func TestMonsterContactStartsFight(t *testing.T) {
	r := newRig(t, testRat)
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("un monstre au contact devrait attaquer")
	}
	if c := r.last(); len(c.Log) < 2 || !strings.Contains(c.Log[0], "Un Rat vous attaque") || !strings.Contains(c.Log[1], "Initiative") {
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
	r.g.act(r.p, combat.Attack, 0)

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
	r.p.stats.XP = r.p.stats.XPToNext() - 1 // +4 XP franchit le palier
	r.p.hp = 3
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

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

func TestLossRespawnsWithoutXPLoss(t *testing.T) {
	r := newRig(t, deadly(testRat))
	r.p.stats.XP = 8
	r.p.hp = 5
	old := r.p.pos
	r.engage()
	r.g.act(r.p, combat.Attack, 0)

	c := r.last()
	if !c.Over || c.Result != protocol.ResultLost {
		t.Fatalf("défaite attendue: %+v", c)
	}
	if r.p.fight != nil || r.m.fight != nil {
		t.Fatal("combattants non libérés")
	}
	if r.p.hp != r.p.stats.MaxHP || r.p.stats.XP != 8 || r.p.stats.Level != 1 {
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
	if _, alive := r.g.monsters[r.m.id]; !alive || r.m.calm != disengageCooldown {
		t.Fatal("le monstre vainqueur doit rester, avec un délai avant de ré-engager")
	}
}

func TestFleeFreesBothAndMonsterWaits(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000 // survit aux fuites ratées
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil {
		t.Fatal("la fuite n'a jamais réussi en 200 essais")
	}
	if c := r.last(); !c.Over || c.Result != protocol.ResultFled {
		t.Fatalf("issue de fuite attendue: %+v", c)
	}
	if r.m.fight != nil || r.m.calm != fleeGrace {
		t.Fatalf("monstre: fight=%v cooldown=%d", r.m.fight, r.m.calm)
	}

	// Le joueur reste collé au monstre, qui ne ré-engage qu'après le répit.
	for i := 0; i < fleeGrace-1; i++ {
		r.g.tick++
		r.g.stepMonsters(r.g.sortedMonsterIDs())
		if r.p.fight != nil {
			t.Fatalf("ré-engagé trop tôt (itération %d)", i)
		}
	}
	r.g.tick++
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("le monstre aurait dû ré-engager après le répit")
	}
}

// Après une fuite, un autre monstre à portée n'engage pas non plus le joueur, jusqu'à la fin du répit.
func TestFleeGraceProtectsFromOtherMonsters(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil {
		t.Fatal("la fuite n'a jamais réussi en 200 essais")
	}
	// Un second monstre, libre, au contact du joueur.
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
		t.Skip("terrain peu praticable autour du joueur")
	}
	for i := 0; i < fleeGrace-1; i++ {
		r.g.tick++
		r.g.stepMonsters(r.g.sortedMonsterIDs())
		if r.p.fight != nil {
			t.Fatalf("engagé pendant le répit (itération %d)", i)
		}
	}
	r.g.tick++
	r.g.stepMonsters(r.g.sortedMonsterIDs())
	if r.p.fight == nil {
		t.Fatal("un monstre aurait dû engager après le répit")
	}
}

// Attaquer un monstre pendant le répit met fin au répit.
func TestAttackEndsFleeGrace(t *testing.T) {
	r := newRig(t, testRat)
	r.p.stats.MaxHP, r.p.hp = 1000, 1000
	r.engage()
	for i := 0; i < 200 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Flee, 0)
	}
	if r.p.fight != nil || r.p.calmUntil <= r.g.tick {
		t.Fatalf("répit attendu après la fuite (calmUntil=%d, tick=%d)", r.p.calmUntil, r.g.tick)
	}
	r.m.calm = 0
	r.g.move(r.p.id, r.dx, r.dy)
	r.g.step()
	if r.p.fight == nil {
		t.Fatal("marcher sur le monstre aurait dû ouvrir un combat")
	}
	if r.p.calmUntil != 0 {
		t.Fatalf("le répit aurait dû prendre fin: calmUntil=%d", r.p.calmUntil)
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
	if r.m.calm != disengageCooldown {
		t.Fatalf("cooldown=%d", r.m.calm)
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
	r.g.act(r.p, combat.Attack, 0) // le joueur agit: le chronomètre repart
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
	g.act(p, combat.Attack, 0)
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
		r.g.act(r.p, combat.Attack, 0)
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
		r.g.act(r.p, combat.Attack, 0)
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
	if c := recv(); c.Over || len(c.Enemies) != 1 || c.Enemies[0].Kind != "rat" {
		t.Fatalf("ouverture: %+v", c)
	}
	g.Act(id, "attack", 0)
	if c := recv(); !c.Over || c.Result != protocol.ResultWon {
		t.Fatalf("victoire attendue: %+v", c)
	}
	g.Act(id, "attack", 0) // hors combat: ignoré
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
			t.Errorf("canGroup(%s,%s)=%v, attendu %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFightGathersCompatibleNeighbours(t *testing.T) {
	r := newRig(t, testRat)
	at := func(dx, dy int) world.Pos { return world.Pos{X: r.m.pos.X + dx, Y: r.m.pos.Y + dy} }
	var near []*monster
	for _, d := range [][2]int{{1, 0}, {0, 1}, {1, 1}, {2, 0}} { // 3 voisins de plus que maxGroup n'en admet
		pos := at(d[0], d[1])
		if r.g.blocked(pos) || r.g.World.Safe(pos.X, pos.Y) {
			continue
		}
		near = append(near, r.g.addMonster(testRat, pos))
	}
	if len(near) < 3 {
		t.Skip("terrain peu praticable autour de la case de test")
	}
	far := r.g.addMonster(testRat, at(groupRadius+2, 0))
	crab := r.g.addMonster(species{Kind: "crab", HP: 5, Dmg: dice.D(1, 4, 0), MoveEvery: 5}, at(0, -1))

	r.engage()
	f := r.p.fight
	if f == nil || len(f.ms) != maxGroup {
		t.Fatalf("groupe de %d, attendu %d", len(f.ms), maxGroup)
	}
	if far.fight != nil || crab.fight != nil {
		t.Fatal("un monstre éloigné ou incompatible a été engagé")
	}
	if got := r.last(); len(got.Enemies) != maxGroup || got.Enemies[0].Name != "Rat 1" {
		t.Fatalf("ennemis envoyés: %+v", got.Enemies)
	}
}

func TestKillsAreRewardedPerMonsterAndFleeReleasesTheRest(t *testing.T) {
	r := newRig(t, weak(testRat))
	extra := r.g.addMonster(testRat, world.Pos{X: r.m.pos.X, Y: r.m.pos.Y + 1})
	if r.g.blocked(extra.pos) {
		t.Skip("case voisine bloquée")
	}
	r.engage()
	if r.p.fight == nil || len(r.p.fight.ms) < 2 {
		t.Fatal("le groupe devait compter 2 monstres")
	}
	xp := r.p.stats.XP
	for i := 0; i < 200 && r.m.fight != nil; i++ { // frappe le monstre faible (cible 0) jusqu'à sa mort
		r.g.act(r.p, combat.Attack, 0)
		if r.p.fight == nil {
			t.Skip("le joueur est mort avant d'abattre le premier monstre")
		}
	}
	if _, alive := r.g.monsters[r.m.id]; alive {
		t.Fatal("le monstre abattu doit quitter le monde")
	}
	if r.p.stats.Kills != 1 || (r.p.stats.XP == xp && r.p.stats.Level == 1) {
		t.Fatalf("la mort du premier monstre devait être récompensée tout de suite: kills=%d", r.p.stats.Kills)
	}
	if r.p.fight == nil || extra.fight == nil {
		t.Fatal("le combat doit continuer contre le survivant")
	}
	r.p.fight.b.Abandon()
	r.g.endFight(r.p.fight)
	if extra.fight != nil || extra.calm != disengageCooldown {
		t.Fatalf("le survivant doit être libéré: fight=%v cooldown=%d", extra.fight, extra.calm)
	}
}

func TestBeginnersMeetNoPacks(t *testing.T) {
	r := newRig(t, testRat)
	before := len(r.g.monsters)
	for range 50 {
		r.g.spawnPack(r.m, r.p.pos, 1)
	}
	if len(r.g.monsters) != before {
		t.Fatalf("%d monstres de plus pour un joueur de niveau 1", len(r.g.monsters)-before)
	}
}

// Les compagnons en train de marcher (cooldown de pas en cours) rejoignent le combat; ceux qui viennent de fuir
// ou de perdre (calm) restent à l'écart.
func TestPackJoinsEvenWhileWalking(t *testing.T) {
	r := newRig(t, testRat)
	var mates []*monster
	for _, d := range [][2]int{{1, 0}, {0, 1}} {
		pos := world.Pos{X: r.m.pos.X + d[0], Y: r.m.pos.Y + d[1]}
		if r.g.blocked(pos) || r.g.World.Safe(pos.X, pos.Y) {
			t.Skip("terrain peu praticable autour de la case de test")
		}
		m := r.g.addMonster(testRat, pos)
		m.cooldown = testRat.MoveEvery
		mates = append(mates, m)
	}
	mates[1].calm = disengageCooldown
	r.engage()
	if r.p.fight == nil || len(r.p.fight.ms) != 2 || mates[0].fight == nil || mates[1].fight != nil {
		t.Fatalf("groupe attendu: le compagnon en marche oui, le calmé non (fight=%v)", r.p.fight)
	}
}

// Le seigneur d'une forteresse laisse toujours un objet, d'un rang au-dessus de celui de sa région.
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
		t.Fatalf("%d objets lâchés, attendu 1", len(r.p.inv.Items))
	}
	if got, want := r.p.inv.Items[0].Tier, gear.TierForLevel(12)+1; got != want {
		t.Errorf("rang %d, attendu %d", got, want)
	}
}

// Un monstre de niveau 1 et un sac plein ne laissent rien; un monstre ordinaire n'en laisse que rarement.
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
		t.Errorf("%d objets sur 2000 monstres", drops)
	}
	r.p.inv.Items = make([]gear.Item, gear.BagCapacity)
	for range 200 {
		if r.g.monsterGear(r.p, r.m) != "" {
			t.Fatal("un sac plein ne peut rien recevoir")
		}
	}
}

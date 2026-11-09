package game

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// fortress renvoie une forteresse du monde de test.
func fortress(t *testing.T, g *Game) *world.Structure {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 400) {
		if s.Kind == world.KindFortress {
			return s
		}
	}
	t.Skip("pas de forteresse près du départ")
	return nil
}

func speciesNamed(t *testing.T, kind string) species {
	t.Helper()
	for _, s := range speciesList {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("espèce %q inconnue", kind)
	return species{}
}

// outsideFortress renvoie une case libre à d cases du centre, hors de l'emprise.
func nearFortress(t *testing.T, g *Game, s *world.Structure, d int) world.Pos {
	t.Helper()
	c := s.Center()
	return freeWalkable(t, g, world.Pos{X: c.X + s.W/2 + d, Y: c.Y}, 0)
}

func TestGarrisonSpeciesAreConsistent(t *testing.T) {
	for _, role := range []world.SlotRole{world.RoleGuard, world.RoleElite, world.RoleBoss} {
		sp, ok := speciesForRole(role)
		if !ok {
			t.Fatalf("pas d'espèce pour le rôle %d", role)
		}
		if !sp.Garrison {
			t.Errorf("%s n'est pas marquée comme garnison", sp.Kind)
		}
		if sp.Boss != (role == world.RoleBoss) {
			t.Errorf("%s: Boss=%v pour le rôle %d", sp.Kind, sp.Boss, role)
		}
	}
	// Les garnisons n'apparaissent jamais selon le biome.
	for tile, list := range speciesByTile {
		for _, sp := range list {
			if sp.Garrison {
				t.Errorf("%s apparaîtrait en biome (%d)", sp.Kind, tile)
			}
		}
	}
	// La puissance croît avec le rang.
	g, e, b := speciesNamed(t, "skeleton"), speciesNamed(t, "knight"), speciesNamed(t, "warlord")
	if !(g.HP < e.HP && e.HP < b.HP && g.Atk < e.Atk && e.Atk < b.Atk && g.Def < e.Def && e.Def < b.Def && g.XP < e.XP && e.XP < b.XP) {
		t.Fatal("les rangs de garnison ne sont pas strictement croissants")
	}
	// Le seigneur est unique par forteresse.
	bosses := 0
	for _, s := range speciesList {
		if s.Boss {
			bosses++
		}
	}
	if bosses != 1 {
		t.Fatalf("%d espèces de boss, une attendue", bosses)
	}
}

func TestFortressActivatesWhenPlayerIsNear(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()

	if len(g.monsters) != len(s.Slots) || g.garrisonCount != len(s.Slots) {
		t.Fatalf("%d monstres (garnison %d), %d emplacements", len(g.monsters), g.garrisonCount, len(s.Slots))
	}
	roles := map[string]int{}
	for i, slot := range s.Slots {
		id, ok := g.slotMonster[s.Key(i)]
		if !ok {
			t.Fatalf("emplacement %d vide", i)
		}
		m := g.monsters[id]
		want, _ := speciesForRole(slot.Role)
		if m.sp.Kind != want.Kind || m.pos != slot.Pos || m.home != slot.Pos {
			t.Fatalf("emplacement %d: %s en %v, attendu %s en %v", i, m.sp.Kind, m.pos, want.Kind, slot.Pos)
		}
		if m.bounds != s || m.leash != garrisonLeash || m.slot == nil || *m.slot != s.Key(i) {
			t.Fatalf("emplacement %d: monstre mal initialisé: %+v", i, m)
		}
		roles[m.sp.Kind]++
	}
	if roles["warlord"] != 1 || roles["knight"] != 2 || roles["skeleton"] != 6 {
		t.Fatalf("garnison %v, attendu 1 seigneur, 2 chevaliers, 6 squelettes", roles)
	}
	if len(g.monsterAt) != len(g.monsters) {
		t.Fatal("index des positions incohérent")
	}
}

func TestFortressStaysEmptyWhenNobodyIsNear(t *testing.T) {
	g := newGame()
	fortress(t, g)
	joinRec(g, "a") // au point de départ, loin de toute forteresse
	g.activateGarrisons()
	if len(g.monsters) != 0 {
		t.Fatalf("%d monstres apparus sans joueur à proximité", len(g.monsters))
	}
}

func TestActivationIsIdempotentAndSharedBetweenPlayers(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	a, _ := joinRec(g, "a")
	b, _ := joinRec(g, "b")
	place(g, a, nearFortress(t, g, s, 15))
	place(g, b, nearFortress(t, g, s, 25))
	for i := 0; i < 5; i++ {
		g.activateGarrisons()
	}
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("%d monstres pour %d emplacements: doublons", len(g.monsters), len(s.Slots))
	}
}

func TestGarrisonNeverLeavesTheFortress(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	// Joueur juste devant la porte, à portée de détection : il attire la garnison.
	place(g, p, nearFortress(t, g, s, 3))
	g.activateGarrisons()
	for i := 0; i < 2000; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		for _, m := range g.monsters {
			if !s.Contains(m.pos.X, m.pos.Y) {
				t.Fatalf("%s a quitté la forteresse: %v", m.sp.Kind, m.pos)
			}
			if p.fight != nil { // un combat s'est engagé: on libère le joueur pour continuer l'observation
				g.endFightQuietly(p)
			}
		}
	}
}

// endFightQuietly abandonne le combat du joueur, pour les tests d'observation.
func (g *Game) endFightQuietly(p *player) {
	if f := p.fight; f != nil {
		f.b.Abandon()
		g.endFight(f)
	}
}

func TestGarrisonWanderStaysNearPost(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 30)) // assez près pour l'activation, trop loin pour la détection
	g.activateGarrisons()
	for i := 0; i < 1500; i++ {
		g.stepMonsters(g.sortedMonsterIDs())
		for _, m := range g.monsters {
			if cheb(m.pos, m.home) > garrisonLeash {
				t.Fatalf("%s s'est éloigné de son poste de %d cases (laisse %d)", m.sp.Kind, cheb(m.pos, m.home), garrisonLeash)
			}
		}
	}
}

func TestVictoryStartsRespawnDelay(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()

	// On bat le soldat du premier emplacement de rôle « soldat ».
	idx := -1
	for i, sl := range s.Slots {
		if sl.Role == world.RoleGuard {
			idx = i
			break
		}
	}
	key := s.Key(idx)
	m := g.monsters[g.slotMonster[key]]
	m.sp.HP = 1 // affaibli pour la victoire en un coup
	dx, dy := freeDir(t, g, p)
	place(g, p, world.Pos{X: m.pos.X - dx, Y: m.pos.Y - dy}) // à côté du monstre
	if g.blocked(p.pos) && g.byPos[p.pos] != p.id {
		t.Skip("case voisine du poste indisponible")
	}
	g.startFight(p, m, true)
	g.act(p, combat.Attack)
	if p.fight != nil {
		t.Fatal("le combat aurait dû être gagné")
	}
	if _, alive := g.slotMonster[key]; alive {
		t.Fatal("l'emplacement est encore marqué occupé")
	}
	if g.garrisonCount != len(s.Slots)-1 {
		t.Fatalf("garnison vivante = %d, attendu %d", g.garrisonCount, len(s.Slots)-1)
	}
	if _, beaten := g.slotDefeated[key]; !beaten {
		t.Fatal("la victoire n'a pas lancé la repousse")
	}

	// Pendant la repousse: rien.
	g.tick += guardRespawn - 1
	g.activateGarrisons()
	if _, alive := g.slotMonster[key]; alive {
		t.Fatal("le poste a été réoccupé avant la fin du délai")
	}
	// Délai écoulé: le poste est réoccupé.
	g.tick++
	g.activateGarrisons()
	if _, alive := g.slotMonster[key]; !alive {
		t.Fatal("le poste n'a pas été réoccupé après le délai")
	}
	if _, beaten := g.slotDefeated[key]; beaten {
		t.Fatal("l'entrée de repousse n'a pas été nettoyée")
	}
	// Les autres postes n'ont pas été touchés.
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("%d monstres, %d attendus", len(g.monsters), len(s.Slots))
	}
}

func TestBossRespawnIsLongerThanGuards(t *testing.T) {
	boss, guard := speciesNamed(t, "warlord"), speciesNamed(t, "skeleton")
	if respawnDelay(boss) <= respawnDelay(guard) {
		t.Fatalf("délai du seigneur (%d) <= soldat (%d)", respawnDelay(boss), respawnDelay(guard))
	}
	if respawnDelay(boss) != bossRespawn || respawnDelay(guard) != guardRespawn {
		t.Fatal("délais de repousse incohérents avec les constantes")
	}
}

// S'éloigner ne punit pas la forteresse : le poste est libéré sans délai.
func TestDespawnFreesSlotWithoutDelay(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()
	if len(g.slotMonster) != len(s.Slots) {
		t.Fatal("garnison non créée")
	}

	place(g, p, g.World.FindSpawn()) // le joueur s'en va très loin
	g.despawnFar()
	if len(g.monsters) != 0 || len(g.slotMonster) != 0 || g.garrisonCount != 0 {
		t.Fatalf("après éloignement: %d monstres, %d postes occupés, garnison %d", len(g.monsters), len(g.slotMonster), g.garrisonCount)
	}
	if len(g.slotDefeated) != 0 {
		t.Fatal("un simple éloignement ne doit pas lancer de repousse")
	}

	place(g, p, nearFortress(t, g, s, 20)) // il revient
	g.activateGarrisons()
	if len(g.monsters) != len(s.Slots) {
		t.Fatalf("à son retour: %d monstres, %d attendus", len(g.monsters), len(s.Slots))
	}
}

func TestOccupiedPostWaits(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, s.Slots[0].Pos) // le joueur se tient sur un poste
	g.activateGarrisons()
	if _, alive := g.slotMonster[s.Key(0)]; alive {
		t.Fatal("un monstre est apparu sur la case d'un joueur")
	}
	if len(g.monsters) != len(s.Slots)-1 {
		t.Fatalf("%d monstres, %d attendus (tous sauf le poste occupé)", len(g.monsters), len(s.Slots)-1)
	}
	place(g, p, nearFortress(t, g, s, 10))
	g.activateGarrisons()
	if _, alive := g.slotMonster[s.Key(0)]; !alive {
		t.Fatal("le poste libéré n'a pas été réoccupé")
	}
}

func TestGarrisonDoesNotUseBiomeQuota(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 25))
	g.activateGarrisons()
	garrison := g.garrisonCount

	if n := g.monstersAround(p.pos); n != 0 {
		t.Fatalf("la garnison compte dans le quota de biome (%d)", n)
	}
	for i := 0; i < 600; i++ {
		g.spawnMonsters()
	}
	biome := len(g.monsters) - garrison
	if biome == 0 {
		t.Fatal("aucun monstre de biome n'est apparu près de la forteresse")
	}
	if biome > maxPerPlayer {
		t.Fatalf("%d monstres de biome, quota %d", biome, maxPerPlayer)
	}
}

func TestGarrisonIsVisibleInSnapshots(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, rec := joinRec(g, "a")
	c := s.Center()
	place(g, p, freeWalkable(t, g, world.Pos{X: c.X, Y: c.Y + s.H/2 + 4}, 0))
	g.activateGarrisons()
	g.step()
	kinds := map[string]bool{}
	for _, m := range rec.last().Monsters {
		kinds[m.Kind] = true
	}
	if !kinds["skeleton"] && !kinds["knight"] && !kinds["warlord"] {
		t.Fatalf("aucun monstre de garnison dans l'instantané: %+v", rec.last().Monsters)
	}
}

// --- Butin du seigneur ---

func TestBossLootIsGuaranteed(t *testing.T) {
	for i := 0; i < 30; i++ {
		r := newRig(t, func() species { s := speciesNamed(t, "warlord"); s.HP = 1; return s }())
		r.g.rng = rand.New(rand.NewPCG(uint64(i), 3))
		r.p.stats.Potions = 0
		r.p.stats.MaxHP, r.p.hp = 10_000, 10_000 // le seigneur ne doit pas l'emporter avant le coup fatal
		r.engage()
		for r.p.fight != nil { // un coup peut manquer
			r.g.act(r.p, combat.Attack)
		}

		if r.p.stats.Potions != bossPotions {
			t.Fatalf("essai %d: %d potions, attendu %d", i, r.p.stats.Potions, bossPotions)
		}
		min := bossGold + speciesNamed(t, "warlord").XP/2
		if r.p.stats.Gold < min {
			t.Fatalf("essai %d: %d or, au moins %d attendus", i, r.p.stats.Gold, min)
		}
		if !strings.Contains(strings.Join(r.last().Log, "|"), "trésor") {
			t.Fatalf("essai %d: journal sans mention du trésor: %v", i, r.last().Log)
		}
	}
}

func TestBossLootRespectsCaps(t *testing.T) {
	boss := speciesNamed(t, "warlord")
	boss.HP = 1
	r := newRig(t, boss)
	r.p.stats.Potions = combat.MaxPotions - 1
	r.p.stats.Gold = combat.MaxGold - 10
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Potions != combat.MaxPotions || r.p.stats.Gold != combat.MaxGold {
		t.Fatalf("plafonds dépassés: %d potions, %d or", r.p.stats.Potions, r.p.stats.Gold)
	}
}

func TestOrdinaryMonsterDoesNotDropBossLoot(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.stats.Potions = 0
	r.engage()
	r.g.act(r.p, combat.Attack)
	if r.p.stats.Potions > 1 || strings.Contains(strings.Join(r.last().Log, "|"), "trésor") {
		t.Fatalf("butin de boss pour un rat: %d potions, %v", r.p.stats.Potions, r.last().Log)
	}
}

// --- Équilibrage ---

// L'équilibrage encode l'intention de conception : soldat vers le niveau 4,
// chevalier vers le 7-8, seigneur de guerre vers le 13-14.
func TestGarrisonBalance(t *testing.T) {
	winRate := func(kind string, level int) float64 {
		sp := speciesNamed(t, kind)
		st := combat.NewStats()
		st.AddXP(sumXPForLevel(level), nil)
		if st.Level != level {
			t.Fatalf("niveau %d atteint au lieu de %d", st.Level, level)
		}
		wins := 0
		const trials = 600
		for i := 0; i < trials; i++ {
			b := combat.New(st.Fighter(st.MaxHP), st.Potions,
				combat.Fighter{HP: sp.HP, MaxHP: sp.HP, Atk: sp.Atk, Def: sp.Def}, sp.name(i18n.FR),
				rand.New(rand.NewPCG(uint64(i), 7)))
			for turn := 0; b.Result == combat.Ongoing && turn < 500; turn++ {
				if b.Player.HP <= st.MaxHP/3 && b.Potions > 0 {
					b.Act(combat.Item)
				} else {
					b.Act(combat.Attack)
				}
			}
			if b.Result == combat.Won {
				wins++
			}
		}
		return float64(wins) / trials
	}
	cases := []struct {
		kind     string
		level    int
		min, max float64
		why      string
	}{
		{"skeleton", 1, 0, 0.05, "un débutant ne bat pas un squelette"},
		{"skeleton", 4, 0.85, 1, "un joueur de niveau 4 bat un squelette"},
		{"knight", 4, 0, 0.10, "un niveau 4 ne bat pas un chevalier"},
		{"knight", 8, 0.85, 1, "un niveau 8 bat un chevalier"},
		{"warlord", 8, 0, 0.02, "un niveau 8 ne bat pas le seigneur de guerre"},
		{"warlord", 11, 0, 0.25, "un niveau 11 a peu de chances contre le seigneur"},
		{"warlord", 16, 0.90, 1, "un niveau 16 bat le seigneur de guerre"},
	}
	for _, c := range cases {
		got := winRate(c.kind, c.level)
		if got < c.min || got > c.max {
			t.Errorf("%s niveau %d: %.0f %% de victoires, attendu [%.0f %%, %.0f %%] (%s)",
				c.kind, c.level, got*100, c.min*100, c.max*100, c.why)
		}
	}
}

func TestGarrisonSurvivesSaveAndRestoreRoundTrip(t *testing.T) {
	// La garnison est de l'état éphémère: elle ne doit rien ajouter à la sauvegarde.
	g, sv := gameWithSaves()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))
	g.activateGarrisons()
	g.leave(p.id)
	saved, ok := sv.last("a")
	if !ok {
		t.Fatal("pas de sauvegarde")
	}
	g2 := newGame()
	q := g2.joinWith("a", func(protocol.Snapshot) {}, &saved)
	if q.pos != saved.Pos {
		t.Fatalf("position restaurée %v, sauvegardée %v", q.pos, saved.Pos)
	}
	if len(g2.monsters) != 0 {
		t.Fatal("des monstres sont apparus à la restauration")
	}
}

// La vraie boucle (step) doit déclencher l'activation, pas seulement l'appel direct.
func TestStepActivatesGarrisonPeriodically(t *testing.T) {
	g := newGame()
	s := fortress(t, g)
	p, _ := joinRec(g, "a")
	place(g, p, nearFortress(t, g, s, 20))

	for i := 0; i < spawnEvery-1; i++ {
		g.step()
	}
	if g.garrisonCount != 0 {
		t.Fatalf("garnison créée avant l'échéance (%d)", g.garrisonCount)
	}
	g.step() // tick spawnEvery
	if g.garrisonCount != len(s.Slots) {
		t.Fatalf("garnison = %d après %d ticks, attendu %d", g.garrisonCount, spawnEvery, len(s.Slots))
	}
}

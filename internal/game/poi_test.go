package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"

	"github.com/kevin/ascii-adventure/internal/world"
)

// poisOf renvoie les points d'intérêt de type kind autour de l'origine, du plus proche au plus lointain.
func poisOf(g *Game, kind world.POIKind) []*world.POI {
	var out []*world.POI
	for _, p := range g.World.POIsWithin(0, 0, 8*world.RegionSize) {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
}

func firstPOI(t *testing.T, g *Game, kind world.POIKind) *world.POI {
	t.Helper()
	list := poisOf(g, kind)
	if len(list) == 0 {
		t.Fatalf("aucun point de type %d près de l'origine", kind)
	}
	return list[0]
}

// stepOn fait marcher p sur la case du point, par l'ouest.
func walkOnto(g *Game, p *player, poi *world.POI) {
	g.setPos(p, g.freeNear(world.Pos{X: poi.Pos.X - 1, Y: poi.Pos.Y}))
	// freeNear peut avoir choisi une autre case voisine : on se remet à l'ouest si possible.
	g.setPos(p, world.Pos{X: poi.Pos.X - 1, Y: poi.Pos.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
}

func notices(p *player) string { return strings.Join(p.gearNotices, " | ") }

func TestCampfireHealsAndSetsRespawn(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	fire := firstPOI(t, g, world.POICampfire)
	p.hp = 1
	var saved Saved
	g.Persist = func(_ string, s Saved) { saved = s }

	walkOnto(g, p, fire)
	if p.pos != fire.Pos {
		t.Fatalf("pas sur le feu: %v, attendu %v", p.pos, fire.Pos)
	}
	if p.respawn == nil || *p.respawn != fire.Pos {
		t.Fatalf("point de réapparition %v, attendu %v", p.respawn, fire.Pos)
	}
	if p.hp <= 1 {
		t.Fatal("le feu n'a pas soigné")
	}
	if saved.Respawn == nil || *saved.Respawn != fire.Pos {
		t.Fatalf("point de réapparition non sauvegardé: %v", saved.Respawn)
	}
	if spot, camp := g.respawnSpot(p); !camp || g.blocked(spot) {
		t.Fatalf("réapparition %v (feu: %v)", spot, camp)
	}

	// Le repos est limité : un second passage immédiat ne soigne pas de nouveau.
	hp := p.hp
	p.hp = 1
	walkOnto(g, p, fire)
	if p.hp != 1 {
		t.Fatalf("le feu a soigné deux fois de suite (%d -> %d)", hp, p.hp)
	}
}

func TestNoCampfireMeansWorldSpawn(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	if _, camp := g.respawnSpot(p); camp {
		t.Fatal("réapparition au feu sans feu allumé")
	}
}

func TestShrineBuffsAndCooldown(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	sh := firstPOI(t, g, world.POIShrine)
	kind := shrineBuff(g.World.ClimateAt(sh.Pos.X, sh.Pos.Y))

	base := p.fighter()
	walkOnto(g, p, sh)
	if !p.hasBuff(kind) {
		t.Fatalf("pas de bonus %d après le sanctuaire (%s)", kind, notices(p))
	}
	buffed := p.fighter()
	switch kind {
	case buffDef:
		if buffed.Def != base.Def+shrineDefBonus {
			t.Errorf("DEF %d, attendu %d", buffed.Def, base.Def+shrineDefBonus)
		}
	case buffHit:
		if buffed.Hit != base.Hit+shrineHitBonus {
			t.Errorf("toucher %d, attendu %d", buffed.Hit, base.Hit+shrineHitBonus)
		}
	case buffXP:
		if got := p.xpWithBuff(10); got != 15 {
			t.Errorf("XP 10 -> %d, attendu 15", got)
		}
	}

	// Revenir tout de suite ne prolonge rien.
	until := p.buffs[0].until
	walkOnto(g, p, sh)
	if len(p.buffs) != 1 || p.buffs[0].until != until {
		t.Fatalf("bonus prolongé ou dupliqué: %v", p.buffs)
	}
	if !strings.Contains(notices(p), "sommeille") {
		t.Errorf("pas d'avis de sommeil: %s", notices(p))
	}

	g.tick += shrineDuration
	p.expireBuffs(g.tick)
	if len(p.buffs) != 0 || p.fighter().Def != base.Def {
		t.Fatalf("bonus non expiré: %v", p.buffs)
	}
	// Le cooldown du sanctuaire est plus long que le bonus.
	walkOnto(g, p, sh)
	if p.hasBuff(kind) {
		t.Fatal("sanctuaire réutilisé avant la fin de son cooldown")
	}
	g.tick += shrineCooldown
	walkOnto(g, p, sh)
	if !p.hasBuff(kind) {
		t.Fatal("sanctuaire inutilisable après son cooldown")
	}
}

func TestLairGuardsChest(t *testing.T) {
	g := newGame()
	g.Dungeons = true
	p, _ := joinRec(g, "a")
	lair := firstPOI(t, g, world.POILair)
	g.setPos(p, g.freeNear(world.Pos{X: lair.Pos.X, Y: lair.Pos.Y + 6}))

	g.activateLairs()
	alive := 0
	for i := range lair.Slots {
		if id, ok := g.slotMonster[lairKey(lair, i)]; ok {
			alive++
			if g.monsters[id].leash != lairLeash {
				t.Errorf("gardien %d sans laisse", i)
			}
		}
	}
	if alive != len(lair.Slots) {
		t.Fatalf("%d gardiens sur %d", alive, len(lair.Slots))
	}

	// Les gardiens vivants verrouillent le coffre.
	gold := p.stats.Gold
	g.openLairChest(p, lair)
	if p.stats.Gold != gold || !strings.Contains(notices(p), "gardiens") {
		t.Fatalf("coffre ouvert malgré les gardiens (or %d -> %d, %s)", gold, p.stats.Gold, notices(p))
	}

	// Gardiens vaincus : le coffre s'ouvre une fois, puis se vide jusqu'à la repousse.
	for i := range lair.Slots {
		key := lairKey(lair, i)
		g.removeMonster(g.monsters[g.slotMonster[key]])
		g.slotDefeated[key] = g.tick
	}
	g.openLairChest(p, lair)
	if p.stats.Gold <= gold {
		t.Fatal("le coffre n'a rien donné")
	}
	gold = p.stats.Gold
	g.openLairChest(p, lair)
	if p.stats.Gold != gold {
		t.Fatal("le coffre s'est rouvert aussitôt")
	}

	// Après le délai, les gardiens et le coffre reviennent.
	g.tick += lairRespawn
	g.activateLairs()
	if len(g.slotMonster) == 0 {
		t.Fatal("les gardiens ne reviennent pas")
	}
	for i := range lair.Slots {
		key := lairKey(lair, i)
		if id, ok := g.slotMonster[key]; ok {
			g.removeMonster(g.monsters[id])
		}
	}
	g.openLairChest(p, lair)
	if p.stats.Gold <= gold {
		t.Fatal("le coffre ne s'est pas rempli de nouveau")
	}
}

func TestStonesTravel(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	stones := poisOf(g, world.POIStone)
	if len(stones) < 2 {
		t.Fatalf("%d pierres près de l'origine, il en faut deux", len(stones))
	}
	a, b := stones[0], stones[1]

	if out := g.menhir(p.id, ""); !strings.Contains(out, "pierre dressée") {
		t.Fatalf("hors d'une pierre: %q", out)
	}
	walkOnto(g, p, a)
	if !p.stones[a.Region] {
		t.Fatalf("pierre non activée (%s)", notices(p))
	}
	if out := g.menhir(p.id, ""); !strings.Contains(out, "Aucune autre") {
		t.Fatalf("une seule pierre: %q", out)
	}
	walkOnto(g, p, b)
	walkOnto(g, p, a)
	if p.pos != a.Pos {
		t.Fatalf("pas sur la pierre: %v", p.pos)
	}
	cost := stoneCost(a.Region, b.Region)
	if out := g.menhir(p.id, ""); !strings.Contains(out, b.NameIn(p.lang)) {
		t.Fatalf("liste sans la pierre cible: %q", out)
	}

	p.stats.SpendGold(p.stats.Gold)
	if out := g.menhir(p.id, "1"); !strings.Contains(out, "or pour ce voyage") || p.pos != a.Pos {
		t.Fatalf("voyage sans or: %q (pos %v)", out, p.pos)
	}
	p.stats.AddGold(cost + 5)
	if out := g.menhir(p.id, "1"); p.pos == a.Pos || !strings.Contains(out, b.NameIn(p.lang)) {
		t.Fatalf("pas de voyage: %q (pos %v)", out, p.pos)
	}
	if p.stats.Gold != 5 {
		t.Fatalf("or %d, attendu 5 (coût %d)", p.stats.Gold, cost)
	}
	if max(abs(p.pos.X-b.Pos.X), abs(p.pos.Y-b.Pos.Y)) > 2 {
		t.Fatalf("arrivée trop loin de la pierre cible: %v / %v", p.pos, b.Pos)
	}
}

func TestSavedStonesAndRespawnRoundTrip(t *testing.T) {
	g := newGame()
	fire := firstPOI(t, g, world.POICampfire)
	stone := firstPOI(t, g, world.POIStone)
	sv := Saved{Respawn: &fire.Pos, Stones: []world.RegionPos{stone.Region}, HP: 5, Pos: g.World.FindSpawn()}
	p := g.joinWith("b", func(protocol.Snapshot) {}, &sv)
	if p.respawn == nil || *p.respawn != fire.Pos || !p.stones[stone.Region] {
		t.Fatalf("état non restauré: %v %v", p.respawn, p.stones)
	}
}

func TestBlessingsReachTheSheetAndExpire(t *testing.T) {
	g := newGame()
	var bags []protocol.Bag
	p, _ := joinRec(g, "a")
	p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	sh := firstPOI(t, g, world.POIShrine)
	walkOnto(g, p, sh)
	if len(bags) == 0 || len(bags[len(bags)-1].Blessings) != 1 {
		t.Fatalf("pas de bénédiction dans le sac envoyé: %+v", bags)
	}
	bl := bags[len(bags)-1].Blessings[0]
	if bl.Bonus <= 0 || bl.Secs <= 0 || bl.Secs > shrineDuration/10 {
		t.Fatalf("bénédiction incohérente: %+v", bl)
	}

	// À l'échéance, le serveur renvoie un sac sans bénédiction.
	n := len(bags)
	g.tick = p.buffs[0].until - 1
	g.step()
	if len(bags) <= n || len(bags[len(bags)-1].Blessings) != 0 {
		t.Fatalf("sac non renvoyé à l'expiration: %d -> %d, %+v", n, len(bags), bags[len(bags)-1].Blessings)
	}
}

func TestSpringHealsWithCooldownAndOasisBoost(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	springs := poisOf(g, world.POISpring)
	if len(springs) == 0 {
		t.Fatal("aucune source près de l'origine")
	}
	var plain, oasis *world.POI
	for _, sp := range springs {
		if g.World.ClimateAt(sp.Pos.X, sp.Pos.Y) == world.Arid {
			if oasis == nil {
				oasis = sp
			}
		} else if plain == nil {
			plain = sp
		}
	}
	if plain == nil {
		t.Fatal("aucune source hors climat aride")
	}
	p.stats.MaxHP, p.stats.MaxMana = 100, 10

	// Plein de vie : rien n'est bu, le délai ne court pas.
	p.hp, p.mana = 100, 10
	walkOnto(g, p, plain)
	if _, wait := g.cooling(p, plain.Region, 1); wait {
		t.Fatal("le délai court alors qu'il n'y avait rien à soigner")
	}
	delete(p.poiCooling, plain.Region)

	p.hp, p.mana = 10, 0
	walkOnto(g, p, plain)
	if p.hp != 40 || p.mana != 5 {
		t.Fatalf("source: PV %d (attendu 40), PM %d (attendu 5)", p.hp, p.mana)
	}
	p.hp = 10
	walkOnto(g, p, plain)
	if p.hp != 10 {
		t.Fatalf("la source a soigné deux fois de suite (PV %d)", p.hp)
	}
	g.tick += springCooldown
	walkOnto(g, p, plain)
	if p.hp != 40 {
		t.Fatalf("la source ne soigne pas après son délai (PV %d)", p.hp)
	}

	if oasis == nil {
		t.Log("pas d'oasis près de l'origine pour cette graine : bonus aride non vérifié")
		return
	}
	p.hp, p.mana = 10, 0
	walkOnto(g, p, oasis)
	if p.hp != 70 || p.mana != 10 {
		t.Fatalf("oasis: PV %d (attendu 70), PM %d (attendu 10)", p.hp, p.mana)
	}
}

func TestRuinGuardianChestLoreAndHint(t *testing.T) {
	g := newGame()
	g.Dungeons = true
	p, _ := joinRec(g, "a")
	ruins := poisOf(g, world.POIRuin)
	if len(ruins) == 0 {
		t.Fatal("aucune ruine près de l'origine")
	}
	// Une ruine qui a un donjon à moins de 3 régions : l'indice doit le révéler.
	var ruin *world.POI
	var dungeon *world.Dungeon
	for _, r := range ruins {
		if ds := g.World.DungeonsNear(r.Pos.X, r.Pos.Y, 3); len(ds) > 0 {
			ruin, dungeon = r, ds[0]
			break
		}
	}
	if ruin == nil {
		t.Fatal("aucune ruine à moins de 3 régions d'un donjon")
	}
	g.setPos(p, g.freeNear(world.Pos{X: ruin.Pos.X, Y: ruin.Pos.Y + 7}))

	g.activateLairs()
	key := lairKey(ruin, 0)
	id, ok := g.slotMonster[key]
	if !ok || len(ruin.Slots) != 1 {
		t.Fatalf("gardien de la ruine absent (%d emplacement(s))", len(ruin.Slots))
	}
	if g.monsters[id].sp.Level < world.LevelAt(ruin.Pos.X, ruin.Pos.Y)+1 {
		t.Errorf("gardien de niveau %d : pas plus fort que la région", g.monsters[id].sp.Level)
	}

	gold := p.stats.Gold
	g.openLairChest(p, ruin)
	if p.stats.Gold != gold || !strings.Contains(notices(p), "gardien") {
		t.Fatalf("coffre ouvert malgré le gardien (%s)", notices(p))
	}

	g.removeMonster(g.monsters[id])
	g.slotDefeated[key] = g.tick
	p.gearNotices = nil
	g.openLairChest(p, ruin)
	if p.stats.Gold <= gold {
		t.Fatal("la ruine n'a rien donné")
	}
	if !p.discovered[dungeon.Region] && len(g.World.DungeonsNear(ruin.Pos.X, ruin.Pos.Y, 3)) > 0 {
		// Le donjon révélé est le plus proche, pas forcément le premier de la liste.
		found := false
		for _, d := range g.World.DungeonsNear(ruin.Pos.X, ruin.Pos.Y, 3) {
			found = found || p.discovered[d.Region]
		}
		if !found {
			t.Fatal("l'indice n'a révélé aucun donjon")
		}
	}
	if !strings.Contains(notices(p), "plan gravé") {
		t.Errorf("pas d'indice dans le journal: %s", notices(p))
	}
	lore := i18n.Pick(p.lang, "poi.lore", int(uint64(ruin.Seed)>>8))
	if !strings.Contains(notices(p), lore) {
		t.Errorf("fragment de lore %q absent du journal: %s", lore, notices(p))
	}
}

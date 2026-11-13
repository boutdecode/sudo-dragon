package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"

	"github.com/kevin/ascii-adventure/internal/world"
)

// poisOf returns the points of interest of type kind around the origin, from nearest to farthest.
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
		t.Fatalf("no point of type %d near the origin", kind)
	}
	return list[0]
}

// stepOn walks p onto the point's tile, from the west.
func walkOnto(g *Game, p *player, poi *world.POI) {
	g.setPos(p, g.freeNear(world.Pos{X: poi.Pos.X - 1, Y: poi.Pos.Y}))
	// freeNear may have chosen another neighbouring tile: we go back to the west if possible.
	g.setPos(p, world.Pos{X: poi.Pos.X - 1, Y: poi.Pos.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
}

func notices(p *player) string { return strings.Join(p.gearNotices, " | ") }

func TestCampfireHealsAndSetsRespawn(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	fire := firstPOI(t, g, world.POICampfire)
	p.hp = 1
	var saved Saved
	g.Persist = func(_ string, s Saved) { saved = s }

	walkOnto(g, p, fire)
	if p.pos != fire.Pos {
		t.Fatalf("not on the fire: %v, expected %v", p.pos, fire.Pos)
	}
	if p.respawn == nil || *p.respawn != fire.Pos {
		t.Fatalf("respawn point %v, expected %v", p.respawn, fire.Pos)
	}
	if p.hp <= 1 {
		t.Fatal("the fire did not heal")
	}
	if saved.Respawn == nil || *saved.Respawn != fire.Pos {
		t.Fatalf("respawn point not saved: %v", saved.Respawn)
	}
	if spot, camp := g.respawnSpot(p); !camp || g.blocked(spot) {
		t.Fatalf("respawn %v (fire: %v)", spot, camp)
	}

	// Rest is limited: an immediate second pass does not heal again.
	hp := p.hp
	p.hp = 1
	walkOnto(g, p, fire)
	if p.hp != 1 {
		t.Fatalf("the fire healed twice in a row (%d -> %d)", hp, p.hp)
	}
}

func TestNoCampfireMeansWorldSpawn(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	if _, camp := g.respawnSpot(p); camp {
		t.Fatal("respawn at a fire with no fire lit")
	}
}

func TestShrineBuffsAndCooldown(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	sh := firstPOI(t, g, world.POIShrine)
	kind := shrineBuff(g.World.ClimateAt(sh.Pos.X, sh.Pos.Y))

	base := p.fighter()
	walkOnto(g, p, sh)
	if !p.hasBuff(kind) {
		t.Fatalf("no buff %d after the shrine (%s)", kind, notices(p))
	}
	buffed := p.fighter()
	switch kind {
	case buffDef:
		if buffed.Def != base.Def+shrineDefBonus {
			t.Errorf("DEF %d, expected %d", buffed.Def, base.Def+shrineDefBonus)
		}
	case buffHit:
		if buffed.Hit != base.Hit+shrineHitBonus {
			t.Errorf("to-hit %d, expected %d", buffed.Hit, base.Hit+shrineHitBonus)
		}
	case buffXP:
		if got := p.xpWithBuff(10); got != 15 {
			t.Errorf("XP 10 -> %d, expected 15", got)
		}
	}

	// Coming back right away extends nothing.
	until := p.buffs[0].until
	walkOnto(g, p, sh)
	if len(p.buffs) != 1 || p.buffs[0].until != until {
		t.Fatalf("buff extended or duplicated: %v", p.buffs)
	}
	if !strings.Contains(notices(p), "sommeille") {
		t.Errorf("no sleep notice: %s", notices(p))
	}

	g.tick += shrineDuration
	p.expireBuffs(g.tick)
	if len(p.buffs) != 0 || p.fighter().Def != base.Def {
		t.Fatalf("buff not expired: %v", p.buffs)
	}
	// The shrine's cooldown is longer than the buff.
	walkOnto(g, p, sh)
	if p.hasBuff(kind) {
		t.Fatal("shrine reused before the end of its cooldown")
	}
	g.tick += shrineCooldown
	walkOnto(g, p, sh)
	if !p.hasBuff(kind) {
		t.Fatal("shrine unusable after its cooldown")
	}
}

func TestLairGuardsChest(t *testing.T) {
	g := newGame()
	g.Dungeons = true
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	lair := firstPOI(t, g, world.POILair)
	g.setPos(p, g.freeNear(world.Pos{X: lair.Pos.X, Y: lair.Pos.Y + 6}))

	g.activateLairs()
	alive := 0
	for i := range lair.Slots {
		if id, ok := g.slotMonster[lairKey(lair, i)]; ok {
			alive++
			if g.monsters[id].leash != lairLeash {
				t.Errorf("guardian %d without leash", i)
			}
		}
	}
	if alive != len(lair.Slots) {
		t.Fatalf("%d guardians out of %d", alive, len(lair.Slots))
	}

	// Living guardians lock the chest.
	gold := p.stats.Gold
	g.openLairChest(p, lair)
	if p.stats.Gold != gold || !strings.Contains(notices(p), "gardiens") {
		t.Fatalf("chest opened despite the guardians (or %d -> %d, %s)", gold, p.stats.Gold, notices(p))
	}

	// Guardians defeated: the chest opens once, then stays empty until it regrows.
	for i := range lair.Slots {
		key := lairKey(lair, i)
		g.removeMonster(g.monsters[g.slotMonster[key]])
		g.slotDefeated[key] = g.tick
	}
	g.openLairChest(p, lair)
	if p.stats.Gold <= gold {
		t.Fatal("the chest gave nothing")
	}
	gold = p.stats.Gold
	g.openLairChest(p, lair)
	if p.stats.Gold != gold {
		t.Fatal("the chest reopened right away")
	}

	// After the delay, the guardians and the chest come back.
	g.tick += lairRespawn
	g.activateLairs()
	if len(g.slotMonster) == 0 {
		t.Fatal("the guardians do not come back")
	}
	for i := range lair.Slots {
		key := lairKey(lair, i)
		if id, ok := g.slotMonster[key]; ok {
			g.removeMonster(g.monsters[id])
		}
	}
	g.openLairChest(p, lair)
	if p.stats.Gold <= gold {
		t.Fatal("the chest did not refill")
	}
}

func TestStonesTravel(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	stones := poisOf(g, world.POIStone)
	if len(stones) < 2 {
		t.Fatalf("%d stones near the origin, two are needed", len(stones))
	}
	a, b := stones[0], stones[1]

	if out := g.menhir(p.id, ""); !strings.Contains(out, "pierre dressée") {
		t.Fatalf("off a stone: %q", out)
	}
	walkOnto(g, p, a)
	if !p.stones[a.Region] {
		t.Fatalf("stone not activated (%s)", notices(p))
	}
	if out := g.menhir(p.id, ""); !strings.Contains(out, "Aucune autre") {
		t.Fatalf("only one stone: %q", out)
	}
	walkOnto(g, p, b)
	walkOnto(g, p, a)
	if p.pos != a.Pos {
		t.Fatalf("not on the stone: %v", p.pos)
	}
	cost := stoneCost(a.Region, b.Region)
	if out := g.menhir(p.id, ""); !strings.Contains(out, b.NameIn(p.lang)) {
		t.Fatalf("list without the target stone: %q", out)
	}

	p.stats.SpendGold(p.stats.Gold)
	if out := g.menhir(p.id, "1"); !strings.Contains(out, "or pour ce voyage") || p.pos != a.Pos {
		t.Fatalf("trip without gold: %q (pos %v)", out, p.pos)
	}
	p.stats.AddGold(cost + 5)
	if out := g.menhir(p.id, "1"); p.pos == a.Pos || !strings.Contains(out, b.NameIn(p.lang)) {
		t.Fatalf("no trip: %q (pos %v)", out, p.pos)
	}
	if p.stats.Gold != 5 {
		t.Fatalf("gold %d, expected 5 (cost %d)", p.stats.Gold, cost)
	}
	if max(abs(p.pos.X-b.Pos.X), abs(p.pos.Y-b.Pos.Y)) > 2 {
		t.Fatalf("arrival too far from the target stone: %v / %v", p.pos, b.Pos)
	}
}

func TestSavedStonesAndRespawnRoundTrip(t *testing.T) {
	g := newGame()
	fire := firstPOI(t, g, world.POICampfire)
	stone := firstPOI(t, g, world.POIStone)
	sv := Saved{Respawn: &fire.Pos, Stones: []world.RegionPos{stone.Region}, HP: 5, Pos: g.World.FindSpawn()}
	p := g.joinWith("b", func(protocol.Snapshot) {}, &sv)
	if p.respawn == nil || *p.respawn != fire.Pos || !p.stones[stone.Region] {
		t.Fatalf("state not restored: %v %v", p.respawn, p.stones)
	}
}

func TestBlessingsReachTheSheetAndExpire(t *testing.T) {
	g := newGame()
	var bags []protocol.Bag
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	p.sendBagMsg = func(b protocol.Bag) { bags = append(bags, b) }
	sh := firstPOI(t, g, world.POIShrine)
	walkOnto(g, p, sh)
	if len(bags) == 0 || len(bags[len(bags)-1].Blessings) != 1 {
		t.Fatalf("no blessing in the bag sent: %+v", bags)
	}
	bl := bags[len(bags)-1].Blessings[0]
	if bl.Bonus <= 0 || bl.Secs <= 0 || bl.Secs > shrineDuration/10 {
		t.Fatalf("inconsistent blessing: %+v", bl)
	}

	// At expiry, the server sends back a bag without the blessing.
	n := len(bags)
	g.tick = p.buffs[0].until - 1
	g.step()
	if len(bags) <= n || len(bags[len(bags)-1].Blessings) != 0 {
		t.Fatalf("bag not resent at expiry: %d -> %d, %+v", n, len(bags), bags[len(bags)-1].Blessings)
	}
}

func TestSpringHealsWithCooldownAndOasisBoost(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	springs := poisOf(g, world.POISpring)
	if len(springs) == 0 {
		t.Fatal("no spring near the origin")
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
		t.Fatal("no spring outside an arid climate")
	}
	p.stats.MaxHP, p.stats.MaxMana = 100, 10

	// Full of health: nothing is drunk, the delay does not run.
	p.hp, p.mana = 100, 10
	walkOnto(g, p, plain)
	if _, wait := g.cooling(p, plain.Key(), 1); wait {
		t.Fatal("the delay runs although there was nothing to heal")
	}
	delete(p.poiCooling, plain.Key())

	p.hp, p.mana = 10, 0
	walkOnto(g, p, plain)
	if p.hp != 40 || p.mana != 5 {
		t.Fatalf("source: HP %d (expected 40), MP %d (expected 5)", p.hp, p.mana)
	}
	p.hp = 10
	walkOnto(g, p, plain)
	if p.hp != 10 {
		t.Fatalf("the spring healed twice in a row (HP %d)", p.hp)
	}
	g.tick += springCooldown
	walkOnto(g, p, plain)
	if p.hp != 40 {
		t.Fatalf("the spring does not heal after its delay (HP %d)", p.hp)
	}

	if oasis == nil {
		t.Log("no oasis near the origin for this seed: arid bonus not verified")
		return
	}
	p.hp, p.mana = 10, 0
	walkOnto(g, p, oasis)
	if p.hp != 70 || p.mana != 10 {
		t.Fatalf("oasis: HP %d (expected 70), MP %d (expected 10)", p.hp, p.mana)
	}
}

func TestRuinGuardianChestLoreAndHint(t *testing.T) {
	g := newGame()
	g.Dungeons = true
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	ruins := poisOf(g, world.POIRuin)
	if len(ruins) == 0 {
		t.Fatal("no ruin near the origin")
	}
	// A ruin with a dungeon within 3 regions: the hint must reveal it.
	var ruin *world.POI
	var dungeon *world.Dungeon
	for _, r := range ruins {
		if ds := g.World.DungeonsNear(r.Pos.X, r.Pos.Y, 3); len(ds) > 0 {
			ruin, dungeon = r, ds[0]
			break
		}
	}
	if ruin == nil {
		t.Fatal("no ruin within 3 regions of a dungeon")
	}
	g.setPos(p, g.freeNear(world.Pos{X: ruin.Pos.X, Y: ruin.Pos.Y + 7}))

	g.activateLairs()
	key := lairKey(ruin, 0)
	id, ok := g.slotMonster[key]
	if !ok || len(ruin.Slots) != 1 {
		t.Fatalf("ruin guardian missing (%d slot(s))", len(ruin.Slots))
	}
	if g.monsters[id].sp.Level < world.LevelAt(ruin.Pos.X, ruin.Pos.Y)+1 {
		t.Errorf("guardian of level %d: not stronger than the region", g.monsters[id].sp.Level)
	}

	gold := p.stats.Gold
	g.openLairChest(p, ruin)
	if p.stats.Gold != gold || !strings.Contains(notices(p), "gardien") {
		t.Fatalf("chest opened despite the guardian (%s)", notices(p))
	}

	g.removeMonster(g.monsters[id])
	g.slotDefeated[key] = g.tick
	p.gearNotices = nil
	g.openLairChest(p, ruin)
	if p.stats.Gold <= gold {
		t.Fatal("the ruin gave nothing")
	}
	if !p.discovered[dungeon.Region] && len(g.World.DungeonsNear(ruin.Pos.X, ruin.Pos.Y, 3)) > 0 {
		// The revealed dungeon is the nearest, not necessarily the first in the list.
		found := false
		for _, d := range g.World.DungeonsNear(ruin.Pos.X, ruin.Pos.Y, 3) {
			found = found || p.discovered[d.Region]
		}
		if !found {
			t.Fatal("the hint revealed no dungeon")
		}
	}
	if !strings.Contains(notices(p), "plan gravé") {
		t.Errorf("no hint in the journal: %s", notices(p))
	}
	lore := i18n.Pick(p.lang, "poi.lore", int(uint64(ruin.Seed)>>8))
	if !strings.Contains(notices(p), lore) {
		t.Errorf("lore fragment %q missing from the journal: %s", lore, notices(p))
	}
}

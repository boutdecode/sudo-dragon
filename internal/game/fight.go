package game

import (
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	disengageCooldown = 20  // ticks (2 s) during which a monster does not engage after a flight or a defeat
	fleeGrace         = 50  // ticks (5 s) during which no monster engages a player who has just fled
	fightIdleTicks    = 600 // a player inactive for 60 s is considered to have fled
	lootPotionChance  = 0.30
	lootGearChance    = 0.05 // equipment item dropped by an ordinary surface monster
	lootGearGarrison  = 0.15 // same for a garrison monster (bosses always drop one)
	regenEvery        = 30   // ticks between two HP restored out of combat (3 s)
	safeRegenEvery    = 10   // ticks between two HP restored in a safe zone (1 s)
)

// fight links a player and one or more engaged monsters. During the fight, all stay on their tile,
// immobile and out of reach of the others.
type fight struct {
	p       *player
	ms      []*monster // aligned with b.Foes
	slain   []bool     // monsters already rewarded and removed from the world
	b       *combat.Battle
	lastAct uint64 // tick of the player's last action
}

// Act applies a player's combat action ("attack", "item", "flee") to the target monster (index in the
// fight; invalid: the first living one). No effect if the player is not in combat or the action is unknown.
func (g *Game) Act(id int, action string, target int) {
	a, ok := combat.ParseAction(action)
	if !ok {
		return
	}
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.act(p, a, target)
		}
	})
}

// startFight engages p and m (and the compatible monsters around them) if they are all free.
func (g *Game) startFight(p *player, m *monster, playerStarted bool) {
	if p.fight != nil || m.fight != nil {
		return
	}
	if playerStarted {
		p.calmUntil = 0 // attacking ends the respite
	}
	g.engage(p, g.gatherGroup(m), playerStarted, "")
}

// engage opens p's fight against group (already gathered and free). intro replaces, if non-empty, the
// default opening sentence.
func (g *Game) engage(p *player, group []*monster, playerStarted bool, intro string) {
	f := &fight{p: p, ms: group, slain: make([]bool, len(group)), lastAct: g.tick}
	foes := make([]combat.Foe, len(group))
	for i, gm := range group {
		foes[i] = combat.Foe{Fighter: gm.sp.fighter(), Name: gm.sp.name(p.lang), Kind: gm.sp.Kind, XP: gm.sp.XP}
	}
	numberDuplicates(foes)
	f.b = combat.NewGroup(p.fighter(), p.stats.Potions, foes, g.rng)
	f.b.Lang = p.lang
	p.fight = f
	for _, gm := range group {
		gm.fight = f
	}
	p.hasPending = false

	switch {
	case intro != "":
	case len(group) > 1 && playerStarted:
		intro = i18n.T(p.lang, "fight.you_attack_group", groupLabel(foes))
	case len(group) > 1:
		intro = i18n.T(p.lang, "fight.group_attacks", groupLabel(foes))
	case playerStarted:
		intro = i18n.T(p.lang, "fight.you_attack", foes[0].Name)
	default:
		intro = i18n.T(p.lang, "fight.monster_attacks", foes[0].Name)
	}
	log := append([]string{intro}, f.b.Start()...)
	if f.b.Result != combat.Ongoing { // struck down by the opening blows
		log = append(log, g.endFight(f)...)
	}
	g.sendFight(f, log)
}

// numberDuplicates numbers monsters with the same name ("Wolf 1", "Wolf 2").
func numberDuplicates(foes []combat.Foe) {
	count := map[string]int{}
	for _, f := range foes {
		count[f.Name]++
	}
	seen := map[string]int{}
	for i := range foes {
		if n := foes[i].Name; count[n] > 1 {
			seen[n]++
			foes[i].Name = fmt.Sprintf("%s %d", n, seen[n])
		}
	}
}

// groupLabel summarises a group: "Wolf ×2, Rat" (unnumbered names).
func groupLabel(foes []combat.Foe) string {
	var order []string
	count := map[string]int{}
	for _, f := range foes {
		name := strings.TrimRight(f.Name, " 0123456789")
		if count[name] == 0 {
			order = append(order, name)
		}
		count[name]++
	}
	for i, n := range order {
		if count[n] > 1 {
			order[i] = fmt.Sprintf("%s ×%d", n, count[n])
		}
	}
	return strings.Join(order, ", ")
}

// act resolves a combat turn, rewards each slain monster and frees the fighters if it is over.
func (g *Game) act(p *player, a combat.Action, target int) {
	f := p.fight
	if f == nil {
		return
	}
	f.lastAct = g.tick
	log := f.b.Act(a, target)
	p.hp, p.mana = f.b.Player.HP, f.b.Player.Mana
	p.stats.Potions = f.b.Potions

	leveled := false
	for i, foe := range f.b.Foes {
		if !foe.Alive() && !f.slain[i] {
			var l []string
			l, lv := g.monsterSlain(f, i)
			log = append(log, l...)
			leveled = leveled || lv
		}
	}
	if f.b.Result != combat.Ongoing {
		log = append(log, g.endFight(f)...)
	} else if leveled { // a level gained mid-fight: max HP, MP and sheet up to date
		f.b.Player = p.fighter()
	}
	g.sendFight(f, log)
}

// monsterSlain rewards the death of monster i and removes it from the world. Returns the log lines and whether the
// player gained a level.
func (g *Game) monsterSlain(f *fight, i int) ([]string, bool) {
	p, m := f.p, f.ms[i]
	f.slain[i] = true
	m.fight = nil
	if m.guard != nil { // a guard leaves neither XP nor loot: only the place's grudge
		return g.guardSlain(p, m.guard), false
	}
	if m.slot != nil { // the slot only respawns after a delay
		g.slotDefeated[*m.slot] = g.tick
	}
	g.removeMonster(m)
	p.stats.Kills++
	if m.sp.Boss {
		p.stats.BossKills++
	}
	log := g.questKill(p, m.sp)
	if inst := g.instances[m.zone]; m.zone != 0 && inst != nil && m.sp.Boss {
		log = append(log, g.questDungeon(p, inst.d.Region)...)
	}
	xp := p.xpWithBuff(m.sp.XP) // blessing of a shrine
	log = append(log, i18n.T(p.lang, "fight.xp", xp))
	if gold := p.stats.AddGold(g.goldReward(m.sp)); gold > 0 {
		log = append(log, i18n.T(p.lang, "fight.gold", gold))
	}
	leveled := false
	if p.stats.AddXP(xp, g.rng) > 0 {
		leveled = true
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
		log = append(log, i18n.T(p.lang, "fight.level_up", p.stats.Level))
	}
	if m.sp.Boss {
		// The lord of a fortress or the master of a dungeon leaves a guaranteed treasure, richer in deeper
		// dungeons; in a dungeon, its death also unlocks the boss chest.
		bonus := 0
		if inst := g.instances[m.zone]; m.zone != 0 && inst != nil {
			g.bossSlain(inst)
			bonus = bossGoldPerTier * inst.d.Tier
		}
		gold, potions := p.stats.AddGold(bossGold+bonus), p.stats.AddPotions(bossPotions)
		log = append(log, i18n.T(p.lang, "fight.treasure", m.sp.name(p.lang), gold, potions))
	} else if g.rng.Float64() < lootPotionChance && p.stats.AddPotions(1) > 0 {
		log = append(log, i18n.T(p.lang, "fight.loot_potion"))
	}
	if line := g.monsterGear(p, m); line != "" {
		log = append(log, line)
	}
	if line := g.monsterMap(p, m); line != "" {
		log = append(log, line)
	}
	return log, leveled
}

// monsterGear draws the equipment item that m leaves to p, of its region's rank (one rank higher for a boss
// or an elite), and puts it in the bag. Only surface monsters drop them: those in dungeons have their
// chests. Returns the log line, empty if there is nothing (or if the bag is full).
func (g *Game) monsterGear(p *player, m *monster) string {
	if m.zone != 0 || m.sp.Dungeon || len(p.inv.Items) >= gear.BagCapacity {
		return ""
	}
	chance := lootGearChance
	if m.sp.Garrison {
		chance = lootGearGarrison
	}
	if !m.sp.Boss && g.rng.Float64() >= chance {
		return ""
	}
	tier := gear.TierForLevel(m.sp.Level)
	if m.sp.Boss || m.sp.Kind == "knight" {
		tier++
	}
	it := gear.MonsterLoot(p.lang, world.RegionOf(m.pos.X, m.pos.Y), tier, g.rng.Uint64())
	added, err := p.inv.Add(it)
	if err != nil {
		return ""
	}
	p.gearDirty = true
	return i18n.T(p.lang, "fight.loot_gear", added.Name)
}

// endFight frees the fighters, applies the consequences of the outcome and returns the matching
// log lines. The rewards for slain monsters have already been given (monsterSlain).
func (g *Game) endFight(f *fight) []string {
	p := f.p
	p.fight = nil

	var log []string
	switch f.b.Result {
	case combat.Lost:
		p.stats.Deaths++
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
		respawn := "fight.respawn"
		if p.zone != 0 {
			g.leaveDungeon(p) // defeated in a dungeon: wake up in front of its entrance
		} else {
			spot, camp := g.respawnSpot(p)
			g.setPos(p, spot)
			if camp {
				respawn = "fight.respawn_camp"
			}
		}
		log = append(log, i18n.T(p.lang, respawn))
	}
	if f.b.Result != combat.Won {
		calm := disengageCooldown
		if f.b.Result == combat.Fled {
			calm = fleeGrace
			p.calmUntil = g.tick + fleeGrace // and no other monster chases you
		}
		for i, m := range f.ms {
			if !f.slain[i] {
				m.calm = calm
			}
		}
	}
	for i, m := range f.ms {
		if !f.slain[i] {
			m.fight = nil
		}
	}
	g.sendBag(p) // the character sheet shows the record
	g.save(p)    // a fight changes progression: do not wait for the periodic save
	return log
}

// abortFight interrupts the fight of a player who is leaving, with no outcome.
func (g *Game) abortFight(p *player) {
	if f := p.fight; f != nil {
		for i, m := range f.ms {
			if !f.slain[i] {
				m.fight = nil
				m.calm = disengageCooldown
			}
		}
		p.fight = nil
	}
}

// expireIdleFights makes players who have not played for too long flee.
func (g *Game) expireIdleFights(ids []int) {
	for _, id := range ids {
		f := g.players[id].fight
		if f == nil || g.tick-f.lastAct <= fightIdleTicks {
			continue
		}
		f.b.Abandon()
		log := append([]string{i18n.T(f.p.lang, "fight.abandoned")}, g.endFight(f)...)
		g.sendFight(f, log)
	}
}

// goldReward draws the gold dropped by a monster: from half to all of its XP.
func (g *Game) goldReward(sp species) int {
	return sp.XP/2 + g.rng.IntN(sp.XP-sp.XP/2+1)
}

// healDue returns the players in ids who recover one HP at this tick: every
// second in a safe zone, every three seconds elsewhere.
// Called at every multiple of safeRegenEvery.
func (g *Game) healDue(ids []int) []int {
	var due []int
	for _, id := range ids {
		p := g.players[id]
		if g.tick%regenEvery == 0 || p.safe {
			due = append(due, id)
		}
	}
	return due
}

// regenerate restores 1 HP to the players in ids who are wounded and not fighting.
// It is step that decides which players are "due" (see healDue).
func (g *Game) regenerate(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.fight != nil {
			continue
		}
		if p.hp < p.stats.MaxHP {
			p.hp++
		}
		if p.mana < p.stats.MaxMana {
			p.mana++
		}
	}
}

// sendFight sends the fight state to the player.
func (g *Game) sendFight(f *fight, log []string) {
	if f.p.sendCombat == nil {
		return
	}
	b := f.b
	enemies := make([]protocol.Enemy, len(b.Foes))
	for i, foe := range b.Foes {
		enemies[i] = protocol.Enemy{
			Kind: foe.Kind, Name: foe.Name, HP: foe.HP, MaxHP: foe.MaxHP, Def: foe.Def, Hit: foe.Hit,
			Dmg: foe.Dmg.String(), Red: foe.Reduction.String(),
		}
	}
	strikes := make([]protocol.Strike, len(b.Events))
	for i, e := range b.Events {
		strikes[i] = protocol.Strike{ByPlayer: e.ByPlayer, Foe: e.Foe, Dmg: e.Dmg, Hit: e.Hit, Crit: e.Crit}
	}
	f.p.sendCombat(protocol.Combat{
		Round:       b.Round,
		Enemies:     enemies,
		Boss:        max(0, b.Strongest()),
		PlayerHP:    b.Player.HP,
		PlayerMaxHP: b.Player.MaxHP,
		PlayerDef:   b.Player.Def,
		PlayerMana:  b.Player.Mana,
		PlayerMaxMP: b.Player.MaxMana,
		Style:       string(b.Player.Kind.OrMelee()),
		Potions:     b.Potions,
		Log:         log,
		Strikes:     strikes,
		Over:        b.Result != combat.Ongoing,
		Result:      b.Result.String(),
	})
}

// statsOf builds a player's network sheet.
func statsOf(p *player) protocol.Stats {
	f := p.fighter()
	return protocol.Stats{
		Level: p.stats.Level, XP: p.stats.XP, XPNext: p.stats.XPToNext(),
		HP: p.hp, MaxHP: p.stats.MaxHP, Def: f.Def, Hit: f.Hit, Dmg: f.Dmg.WithBonus(f.DmgMod).String(),
		Potions: p.stats.Potions, Gold: p.stats.Gold,
		Mana: p.mana, MaxMana: p.stats.MaxMana,
	}
}

// fighter converts the player into a combatant, worn equipment included.
func (p *player) fighter() combat.Fighter {
	f := p.stats.Fighter(p.hp)
	eq := p.inv.Equipped()
	f.Def += eq.Defense()
	f.Reduction = eq.Reduction()
	if w := eq.Weapon; w != nil {
		f.Dmg, f.Kind = w.Damage(), w.Kind.OrMelee()
	}
	f.Hit = p.stats.Mod(f.Kind)
	f.DmgMod = f.Hit
	f.MeleeHit = p.stats.Mod(hero.Melee)
	f.MeleeMod = f.MeleeHit
	f.Mana, f.MaxMana = p.mana, p.stats.MaxMana
	f.Init = p.stats.Scores.Mod(hero.AGI)
	p.applyBuffs(&f.Def, &f.Hit)
	return f
}

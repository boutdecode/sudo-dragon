package game

import (
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
)

const (
	disengageCooldown = 20  // ticks (2 s) pendant lesquels un monstre n'engage pas après une fuite ou une défaite
	fightIdleTicks    = 600 // un joueur inactif depuis 60 s est considéré comme ayant fui
	lootPotionChance  = 0.30
	regenEvery        = 30 // ticks entre deux PV rendus hors combat (3 s)
	safeRegenEvery    = 10 // ticks entre deux PV rendus dans une zone sûre (1 s)
)

// fight relie un joueur et un ou plusieurs monstres engagés. Pendant le combat, tous restent sur leur case,
// immobiles et hors d'atteinte des autres.
type fight struct {
	p       *player
	ms      []*monster // aligné sur b.Foes
	slain   []bool     // monstres déjà récompensés et retirés du monde
	b       *combat.Battle
	lastAct uint64 // tick de la dernière action du joueur
}

// Act applique l'action de combat d'un joueur ("attack", "item", "flee") sur le monstre target (indice dans le
// combat; invalide: le premier vivant). Sans effet si le joueur n'est pas en combat ou si l'action est inconnue.
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

// startFight engage p et m (et les monstres compatibles qui l'entourent) s'ils sont tous libres.
func (g *Game) startFight(p *player, m *monster, playerStarted bool) {
	if p.fight != nil || m.fight != nil {
		return
	}
	group := g.gatherGroup(m)
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

	var intro string
	switch {
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
	if f.b.Result != combat.Ongoing { // terrassé par les coups d'ouverture
		log = append(log, g.endFight(f)...)
	}
	g.sendFight(f, log)
}

// numberDuplicates numérote les monstres de même nom ("Loup 1", "Loup 2").
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

// groupLabel résume un groupe: "Loup ×2, Rat" (noms non numérotés).
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

// act résout un tour de combat, récompense chaque monstre abattu et libère les combattants si c'est fini.
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
	} else if leveled { // un niveau gagné en plein combat: PV max, PM et fiche à jour
		f.b.Player = p.fighter()
	}
	g.sendFight(f, log)
}

// monsterSlain récompense la mort du monstre i et le retire du monde. Renvoie les lignes de journal et si le
// joueur a gagné un niveau.
func (g *Game) monsterSlain(f *fight, i int) ([]string, bool) {
	p, m := f.p, f.ms[i]
	f.slain[i] = true
	m.fight = nil
	if m.slot != nil { // l'emplacement ne repousse qu'après un délai
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
	log = append(log, i18n.T(p.lang, "fight.xp", m.sp.XP))
	if gold := p.stats.AddGold(g.goldReward(m.sp)); gold > 0 {
		log = append(log, i18n.T(p.lang, "fight.gold", gold))
	}
	leveled := false
	if p.stats.AddXP(m.sp.XP, g.rng) > 0 {
		leveled = true
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
		log = append(log, i18n.T(p.lang, "fight.level_up", p.stats.Level))
	}
	if m.sp.Boss {
		// Le seigneur d'une forteresse ou le maître d'un donjon laisse un trésor garanti, plus riche en donjon
		// profond ; en donjon, sa mort déverrouille aussi le coffre du boss.
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
	return log, leveled
}

// endFight libère les combattants, applique les conséquences de l'issue et renvoie les lignes de journal
// correspondantes. Les récompenses des monstres abattus ont déjà été données (monsterSlain).
func (g *Game) endFight(f *fight) []string {
	p := f.p
	p.fight = nil

	var log []string
	switch f.b.Result {
	case combat.Lost:
		p.stats.Deaths++
		p.stats.Penalize()
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
		if p.zone != 0 {
			g.leaveDungeon(p) // vaincu dans un donjon: on se réveille devant son entrée
		} else {
			g.setPos(p, g.freeSpawn())
		}
		log = append(log, i18n.T(p.lang, "fight.respawn"))
	}
	if f.b.Result != combat.Won {
		for i, m := range f.ms {
			if !f.slain[i] {
				m.calm = disengageCooldown
			}
		}
	}
	for i, m := range f.ms {
		if !f.slain[i] {
			m.fight = nil
		}
	}
	g.sendBag(p) // la fiche de personnage affiche le palmarès
	g.save(p)    // un combat change la progression: on n'attend pas la sauvegarde périodique
	return log
}

// abortFight interrompt le combat d'un joueur qui part, sans issue.
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

// expireIdleFights fait fuir les joueurs qui n'ont pas joué depuis trop longtemps.
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

// goldReward tire l'or lâché par un monstre: de la moitié à la totalité de son XP.
func (g *Game) goldReward(sp species) int {
	return sp.XP/2 + g.rng.IntN(sp.XP-sp.XP/2+1)
}

// healDue renvoie les joueurs de ids qui récupèrent un PV à ce tick: toutes
// les secondes dans une zone sûre, toutes les trois secondes ailleurs.
// Appelée à chaque multiple de safeRegenEvery.
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

// regenerate rend 1 PV aux joueurs de ids qui sont blessés et ne se battent pas.
// C'est step qui décide quels joueurs sont « dus » (voir healDue).
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

// sendFight envoie l'état du combat au joueur.
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

// statsOf construit la fiche réseau d'un joueur.
func statsOf(p *player) protocol.Stats {
	f := p.fighter()
	return protocol.Stats{
		Level: p.stats.Level, XP: p.stats.XP, XPNext: p.stats.XPToNext(),
		HP: p.hp, MaxHP: p.stats.MaxHP, Def: f.Def, Hit: f.Hit, Dmg: f.Dmg.WithBonus(f.DmgMod).String(),
		Potions: p.stats.Potions, Gold: p.stats.Gold,
		Mana: p.mana, MaxMana: p.stats.MaxMana,
	}
}

// fighter convertit le joueur en combattant, équipement porté compris.
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
	return f
}

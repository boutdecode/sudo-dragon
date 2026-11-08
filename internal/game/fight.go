package game

import (
	"github.com/kevin/ascii-adventure/internal/combat"
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

// fight relie un joueur et un monstre engagés dans un duel. Pendant le duel,
// les deux restent sur leur case, immobiles et hors d'atteinte des autres.
type fight struct {
	p       *player
	m       *monster
	b       *combat.Battle
	lastAct uint64 // tick de la dernière action du joueur
}

// Act applique l'action de combat d'un joueur ("attack", "defend", "item", "flee").
// Sans effet si le joueur n'est pas en combat ou si l'action est inconnue.
func (g *Game) Act(id int, action string) {
	a, ok := combat.ParseAction(action)
	if !ok {
		return
	}
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.act(p, a)
		}
	})
}

// startFight engage p et m s'ils sont tous deux libres.
func (g *Game) startFight(p *player, m *monster, playerStarted bool) {
	if p.fight != nil || m.fight != nil {
		return
	}
	f := &fight{p: p, m: m, lastAct: g.tick}
	f.b = combat.New(
		p.stats.Fighter(p.hp), p.stats.Potions,
		combat.Fighter{HP: m.sp.HP, MaxHP: m.sp.HP, Atk: m.sp.Atk, Def: m.sp.Def},
		m.sp.name(p.lang), g.rng,
	)
	f.b.Lang = p.lang
	p.fight, m.fight = f, f
	p.hasPending = false

	intro := i18n.T(p.lang, "fight.monster_attacks", m.sp.name(p.lang))
	if playerStarted {
		intro = i18n.T(p.lang, "fight.you_attack", m.sp.name(p.lang))
	}
	g.sendFight(f, []string{intro})
}

// act résout un tour de combat et libère les combattants si c'est fini.
func (g *Game) act(p *player, a combat.Action) {
	f := p.fight
	if f == nil {
		return
	}
	f.lastAct = g.tick
	log := f.b.Act(a)
	p.hp = f.b.Player.HP
	p.stats.Potions = f.b.Potions
	if f.b.Result != combat.Ongoing {
		log = append(log, g.endFight(f)...)
	}
	g.sendFight(f, log)
}

// endFight libère les combattants, applique les conséquences de l'issue et
// renvoie les lignes de journal correspondantes.
func (g *Game) endFight(f *fight) []string {
	p, m := f.p, f.m
	p.fight, m.fight = nil, nil

	var log []string
	switch f.b.Result {
	case combat.Won:
		if m.slot != nil { // l'emplacement ne repousse qu'après un délai
			g.slotDefeated[*m.slot] = g.tick
		}
		g.removeMonster(m)
		p.stats.Kills++
		if m.sp.Boss {
			p.stats.BossKills++
		}
		log = append(log, g.questKill(p, m.sp)...)
		log = append(log, i18n.T(p.lang, "fight.xp", m.sp.XP))
		if gold := p.stats.AddGold(g.goldReward(m.sp)); gold > 0 {
			log = append(log, i18n.T(p.lang, "fight.gold", gold))
		}
		if p.stats.AddXP(m.sp.XP) > 0 {
			p.hp = p.stats.MaxHP
			log = append(log, i18n.T(p.lang, "fight.level_up", p.stats.Level))
		}
		if m.sp.Boss {
			// Le seigneur d'une forteresse laisse un trésor garanti.
			gold, potions := p.stats.AddGold(bossGold), p.stats.AddPotions(bossPotions)
			log = append(log, i18n.T(p.lang, "fight.treasure", m.sp.name(p.lang), gold, potions))
		} else if g.rng.Float64() < lootPotionChance && p.stats.AddPotions(1) > 0 {
			log = append(log, i18n.T(p.lang, "fight.loot_potion"))
		}
	case combat.Lost:
		p.stats.Deaths++
		p.stats.Penalize()
		p.hp = p.stats.MaxHP
		g.setPos(p, g.freeSpawn())
		m.cooldown = disengageCooldown
		log = append(log, i18n.T(p.lang, "fight.respawn"))
	case combat.Fled:
		m.cooldown = disengageCooldown
	}
	g.save(p) // un combat change la progression: on n'attend pas la sauvegarde périodique
	return log
}

// abortFight interrompt le combat d'un joueur qui part, sans issue.
func (g *Game) abortFight(p *player) {
	if f := p.fight; f != nil {
		f.m.fight = nil
		f.m.cooldown = disengageCooldown
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
		if p.fight == nil && p.hp < p.stats.MaxHP {
			p.hp++
		}
	}
}

// sendFight envoie l'état du combat au joueur.
func (g *Game) sendFight(f *fight, log []string) {
	if f.p.sendCombat == nil {
		return
	}
	b := f.b
	f.p.sendCombat(protocol.Combat{
		Round:        b.Round,
		Monster:      f.m.sp.Kind,
		MonsterHP:    b.Monster.HP,
		MonsterMaxHP: b.Monster.MaxHP,
		PlayerHP:     b.Player.HP,
		PlayerMaxHP:  b.Player.MaxHP,
		Potions:      b.Potions,
		Log:          log,
		Over:         b.Result != combat.Ongoing,
		Result:       b.Result.String(),
	})
}

// statsOf construit la fiche réseau d'un joueur.
func statsOf(p *player) protocol.Stats {
	return protocol.Stats{
		Level: p.stats.Level, XP: p.stats.XP, XPNext: p.stats.XPToNext(),
		HP: p.hp, MaxHP: p.stats.MaxHP, Atk: p.stats.Atk, Def: p.stats.Def,
		Potions: p.stats.Potions, Gold: p.stats.Gold,
	}
}

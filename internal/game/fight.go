package game

import (
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
		p.fighter(), p.stats.Potions,
		m.sp.fighter(),
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
	p.hp, p.mana = f.b.Player.HP, f.b.Player.Mana
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
		if p.stats.AddXP(m.sp.XP, g.rng) > 0 {
			p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
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
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
		g.setPos(p, g.freeSpawn())
		m.cooldown = disengageCooldown
		log = append(log, i18n.T(p.lang, "fight.respawn"))
	case combat.Fled:
		m.cooldown = disengageCooldown
	}
	g.sendBag(p) // la fiche de personnage affiche le palmarès
	g.save(p)    // un combat change la progression: on n'attend pas la sauvegarde périodique
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
	f.p.sendCombat(protocol.Combat{
		Round:        b.Round,
		Monster:      f.m.sp.Kind,
		MonsterHP:    b.Monster.HP,
		MonsterMaxHP: b.Monster.MaxHP,
		PlayerHP:     b.Player.HP,
		PlayerMaxHP:  b.Player.MaxHP,
		PlayerDef:    b.Player.Def,
		MonsterDef:   b.Monster.Def,
		PlayerMana:   b.Player.Mana,
		PlayerMaxMP:  b.Player.MaxMana,
		Style:        string(b.Player.Kind.OrMelee()),
		Potions:      b.Potions,
		Log:          log,
		Over:         b.Result != combat.Ongoing,
		Result:       b.Result.String(),
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
	return f
}

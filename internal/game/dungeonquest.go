package game

import (
	"fmt"
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Quêtes de donjon: « vaincre le maître d'un donjon secret ». Deux sources, qui passent par la même validation
// (quest.Validate): le modèle de langage, qui propose librement un donjon de la liste autorisée, et un villageois
// qui confie du travail, sans modèle de langage, en désignant le donjon accessible le plus proche. Accepter la
// quête révèle l'entrée du donjon ; la mort de la liche dans son donjon l'accomplit.

// dungeonRange est la distance maximale (en régions) du donneur à un donjon qu'il confie.
const dungeonRange = quest.PlaceRange

// workReward donne la récompense proposée pour un donjon: confortable, mais bornée par quest.ClampReward.
func workReward(tier int) quest.Reward {
	diff := 150 + 50*tier
	return quest.Reward{Gold: diff * 5 / 4, XP: diff / 5, Potions: 2}
}

// dungeonsFor liste les donjons que npc peut confier à p: à portée, au niveau du joueur, pas déjà en cours ni
// rendus. Le plus proche d'abord, puis par identifiant (ordre stable).
func (g *Game) dungeonsFor(p *player, npc world.NPC) []*world.Dungeon {
	taken := map[world.RegionPos]bool{}
	for _, q := range p.quests {
		if q.Objective.Kind == quest.Dungeon && (q.Open() || q.State == quest.Done) {
			taken[q.Objective.Target] = true
		}
	}
	var out []*world.Dungeon
	for ry := npc.Region.Y - dungeonRange; ry <= npc.Region.Y+dungeonRange; ry++ {
		for rx := npc.Region.X - dungeonRange; rx <= npc.Region.X+dungeonRange; rx++ {
			d := g.World.DungeonOfRegion(world.RegionPos{X: rx, Y: ry})
			// Un donjon dont le boss vient de tomber ne peut pas être vaincu avant son retour: on ne le confie pas.
			if d != nil && !taken[d.Region] && p.stats.Level >= quest.DungeonLevel(d.Tier) && !g.coolingDown(d) {
				out = append(out, d)
			}
		}
	}
	slices.SortFunc(out, func(a, b *world.Dungeon) int {
		if da, db := lore.Distance(a.Region, npc.Region), lore.Distance(b.Region, npc.Region); da != db {
			return da - db
		}
		return compareStrings(a.ID, b.ID)
	})
	return out
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// dungeonRefs donne au contexte de validation les donjons que npc peut confier, par nom dans la langue de p.
// Deux donjons de même nom (impossible à moins de 3 régions l'un de l'autre, mais le donneur voit plus loin)
// ne sont proposés ni l'un ni l'autre.
func (g *Game) dungeonRefs(p *player, npc world.NPC) map[string]quest.DungeonRef {
	refs := map[string]quest.DungeonRef{}
	dup := map[string]bool{}
	for _, d := range g.dungeonsFor(p, npc) {
		name := lowerName(d.NameIn(p.lang))
		if _, seen := refs[name]; seen {
			dup[name] = true
		}
		refs[name] = quest.DungeonRef{Region: d.Region, Tier: d.Tier}
	}
	for name := range dup {
		delete(refs, name)
	}
	return refs
}

// hasWork dit si npc a un donjon à confier à p (et peut lui confier une quête).
func (g *Game) hasWork(p *player, npc world.NPC) bool {
	return npc.GivesQuests && g.canOffer(p, npc) && len(g.dungeonsFor(p, npc)) > 0
}

// workQuest fabrique la quête de donjon que npc confie à p, ou nil. Elle passe par quest.Validate comme toute
// proposition: le niveau, la portée et les plafonds de récompense sont les mêmes que pour le modèle de langage.
func (g *Game) workQuest(p *player, npc world.NPC) *quest.Quest {
	if !g.hasWork(p, npc) {
		return nil
	}
	d := g.dungeonsFor(p, npc)[0]
	name := d.NameIn(p.lang)
	pick := int(npc.Key().Region.X*31+npc.Key().Region.Y*17+npc.Key().Index) + d.Region.X*7 + d.Region.Y
	r := workReward(d.Tier)
	q, err := quest.Validate(quest.Proposal{
		Title:   fmt.Sprintf(i18n.Pick(p.lang, "quest.dungeon.title", pick), name),
		Story:   fmt.Sprintf(i18n.Pick(p.lang, "quest.dungeon.story", pick), name),
		Type:    string(quest.Dungeon),
		Place:   name,
		Gold:    r.Gold,
		XP:      r.XP,
		Potions: r.Potions,
		Note:    i18n.T(p.lang, "quest.dungeon.note"),
	}, quest.Context{
		Level: p.stats.Level, Giver: npc.Key(), Now: g.now(),
		Dungeons: map[string]quest.DungeonRef{lowerName(name): {Region: d.Region, Tier: d.Tier}},
	})
	if err != nil {
		return nil
	}
	return &q
}

// askWork répond à « avez-vous du travail ? » : la quête de donjon est proposée, ou le PNJ n'a rien à confier.
func (g *Game) askWork(p *player) []string {
	t := p.talk
	q := g.workQuest(p, t.npc)
	if q == nil {
		return []string{i18n.T(p.lang, "npc.no_work")}
	}
	g.proposeQuest(p, q)
	if t.proposal == nil { // déjà en cours auprès de ce PNJ, ou plus recevable
		return []string{i18n.T(p.lang, "npc.no_work")}
	}
	return nil
}

// revealDungeonOf révèle l'entrée du donjon visé par la quête q à son porteur.
func (g *Game) revealDungeonOf(p *player, q quest.Quest) {
	if q.Objective.Kind != quest.Dungeon {
		return
	}
	if d := g.World.DungeonOfRegion(q.Objective.Target); d != nil {
		g.discover(p, d)
	}
}

// questDungeon compte la victoire sur le maître du donjon de la région rp. Renvoie des lignes de journal.
func (g *Game) questDungeon(p *player, rp world.RegionPos) []string {
	var log []string
	changed := false
	for i := range p.quests {
		q := &p.quests[i]
		if q.State != quest.Active || q.Objective.Kind != quest.Dungeon || q.Objective.Target != rp {
			continue
		}
		if q.Advance(1) {
			changed = true
			log = append(log, g.progressLine(p, *q))
		}
	}
	if changed {
		g.questsChanged(p)
	}
	return log
}

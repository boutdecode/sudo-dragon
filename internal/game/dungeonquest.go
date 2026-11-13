package game

import (
	"fmt"
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Dungeon quests: "defeat the master of a secret dungeon". Two sources, which go through the same validation
// (quest.Validate): the language model, which freely proposes a dungeon from the allowed list, and a villager
// who entrusts work, without a language model, by designating the nearest accessible dungeon. Accepting the
// quest reveals the dungeon entrance; the lich's death in its dungeon completes it.

// dungeonRange is the maximum distance (in regions) from the giver to a dungeon it entrusts.
const dungeonRange = quest.PlaceRange

// workReward gives the reward offered for a dungeon: comfortable, but bounded by quest.ClampReward.
func workReward(tier int) quest.Reward {
	diff := 150 + 50*tier
	return quest.Reward{Gold: diff * 5 / 4, XP: diff / 5, Potions: 2}
}

// dungeonsFor lists the dungeons that npc can entrust to p: within range, at the player's level, not already in progress nor
// completed. Nearest first, then by identifier (stable order).
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
			// A dungeon whose boss has just fallen cannot be defeated before it returns: do not entrust it.
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

// dungeonRefs gives the validation context the dungeons that npc can entrust, by name in p's language.
// Two dungeons with the same name (impossible within 3 regions of each other, but the giver sees further)
// are offered neither.
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

// hasWork says whether npc has a dungeon to entrust to p (and can entrust a quest).
func (g *Game) hasWork(p *player, npc world.NPC) bool {
	return npc.GivesQuests && g.canOffer(p, npc) && len(g.dungeonsFor(p, npc)) > 0
}

// workQuest builds the dungeon quest that npc entrusts to p, or nil. It goes through quest.Validate like any
// proposal: the level, range and reward caps are the same as for the language model.
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

// askWork answers "do you have any work?": the dungeon quest is offered, or the NPC has nothing to entrust.
func (g *Game) askWork(p *player) []string {
	t := p.talk
	q := g.workQuest(p, t.npc)
	if q == nil {
		return []string{i18n.T(p.lang, "npc.no_work")}
	}
	g.proposeQuest(p, q)
	if t.proposal == nil { // already in progress with this NPC, or no longer acceptable
		return []string{i18n.T(p.lang, "npc.no_work")}
	}
	return nil
}

// revealDungeonOf reveals the entrance of the dungeon targeted by quest q to its bearer.
func (g *Game) revealDungeonOf(p *player, q quest.Quest) {
	if q.Objective.Kind != quest.Dungeon {
		return
	}
	if d := g.World.DungeonOfRegion(q.Objective.Target); d != nil {
		g.discover(p, d)
	}
}

// questDungeon counts the victory over the dungeon master of region rp. Returns journal lines.
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

package game

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Quests. The language model only PROPOSES (see quest.Validate); everything that
// matters (acceptance, progress, hand-in, reward, reputation) is decided here,
// in the game goroutine, from values that are already bounded.

const (
	maxBookHistory  = 10 // completed quests shown in the book
	maxKeptDone     = 40 // completed quests kept per player
	proposalMaxAge  = 20 * time.Minute
	speciesXPBase   = 6 // a species is proposed if its XP <= speciesXPBase + speciesXPPerLvl × level
	speciesXPPerLvl = 3
)

// now is the quests' clock (replaceable in tests).
func (g *Game) now() time.Time {
	if g.Clock != nil {
		return g.Clock()
	}
	return time.Now()
}

// ---------------------------------------------------------------- player state

func (p *player) openQuests() (n int) {
	for _, q := range p.quests {
		if q.Open() {
			n++
		}
	}
	return n
}

func (p *player) questByID(id int64) *quest.Quest {
	for i := range p.quests {
		if p.quests[i].ID == id {
			return &p.quests[i]
		}
	}
	return nil
}

// refreshQuestFlags recomputes what avoids scanning the quests on every step.
func (p *player) refreshQuestFlags() {
	p.hasOpenReach = false
	for _, q := range p.quests {
		if q.State == quest.Active && q.Objective.Kind == quest.Reach {
			p.hasOpenReach = true
		}
	}
}

// loadQuests restores the saved quests (values re-read from the database, hence re-checked).
func (g *Game) loadQuests(p *player, saved *Saved) {
	for rp, pts := range saved.Rep {
		if pts != 0 {
			p.rep[rp] = max(quest.MinReputation, pts)
		}
	}
	for _, q := range saved.Quests {
		if q.ID <= 0 || !(q.Open() || q.State == quest.Done) {
			continue
		}
		q.Account = p.name
		q.Progress = max(0, min(q.Progress, q.Need()))
		if q.State == quest.Active && q.Progress >= q.Need() {
			q.State = quest.Ready
		}
		p.quests = append(p.quests, q)
		p.nextQuestID = max(p.nextQuestID, q.ID)
	}
	p.nextQuestID++
	g.expireQuests(p)
	p.refreshQuestFlags()
}

// expireQuests expires quests that are too old and prunes the history.
func (g *Game) expireQuests(p *player) {
	now, changed := g.now(), false
	kept := p.quests[:0]
	done := 0
	for _, q := range p.quests {
		if q.Expired(now) {
			changed = true
			continue
		}
		kept = append(kept, q)
		if q.State == quest.Done {
			done++
		}
	}
	p.quests = kept
	for done > maxKeptDone { // removes the oldest completed ones
		for i, q := range p.quests {
			if q.State == quest.Done {
				p.quests = slices.Delete(p.quests, i, i+1)
				done--
				changed = true
				break
			}
		}
	}
	if changed {
		p.questsDirty = true
	}
}

// ---------------------------------------------------------------- sending to the client

func (g *Game) questInfo(l i18n.Lang, q quest.Quest) protocol.QuestInfo {
	giver, place := "", g.placeName(l, q.Giver.Region)
	if s := g.World.StructureOfRegion(q.Giver.Region); s != nil && q.Giver.Index >= 0 && q.Giver.Index < len(s.NPCs) {
		giver = s.NPCs[q.Giver.Index].NameIn(l)
	}
	return protocol.QuestInfo{
		ID: q.ID, Title: q.Title, Story: q.Story, Giver: giver, Place: place,
		Goal: g.goalText(l, q), Progress: q.Progress, Need: q.Need(),
		Gold: q.Reward.Gold, XP: q.Reward.XP, Potions: q.Reward.Potions, Note: q.Reward.Note,
		State: string(q.State), Gain: q.ReputationGain(),
		GiverRX: q.Giver.Region.X, GiverRY: q.Giver.Region.Y, GiverIdx: q.Giver.Index,
	}
}

// goalText describes a quest's goal in the player's language.
func (g *Game) goalText(l i18n.Lang, q quest.Quest) string {
	o := q.Objective
	switch o.Kind {
	case quest.Kill:
		return i18n.T(l, "quest.goal.kill", i18n.T(l, "monster."+o.Species), o.Count)
	case quest.Boss:
		return i18n.T(l, "quest.goal.boss", i18n.T(l, "monster.warlord"))
	case quest.Reach:
		return i18n.T(l, "quest.goal.reach", g.placeName(l, o.Target))
	case quest.Dungeon:
		name := "?"
		if d := g.World.DungeonOfRegion(o.Target); d != nil {
			name = d.NameIn(l)
		}
		return i18n.T(l, "quest.goal.dungeon", name)
	}
	return ""
}

// sendQuests sends the complete quest book.
func (g *Game) sendQuests(p *player) {
	if p.sendQuestBook == nil {
		return
	}
	qs := protocol.Quests{Open: []protocol.QuestInfo{}, History: []protocol.QuestInfo{}, Standing: []protocol.Standing{}}
	var done []quest.Quest
	for _, q := range p.quests {
		switch {
		case q.Open():
			qs.Open = append(qs.Open, g.questInfo(p.lang, q))
		case q.State == quest.Done:
			done = append(done, q)
		}
	}
	slices.SortFunc(done, func(a, b quest.Quest) int { return b.DoneAt.Compare(a.DoneAt) })
	for i := 0; i < len(done) && i < maxBookHistory; i++ {
		qs.History = append(qs.History, g.questInfo(p.lang, done[i]))
	}
	places := make([]world.RegionPos, 0, len(p.rep))
	for rp := range p.rep {
		places = append(places, rp)
	}
	slices.SortFunc(places, func(a, b world.RegionPos) int {
		if d := p.rep[b] - p.rep[a]; d != 0 {
			return d
		}
		return a.X*10000 + a.Y - (b.X*10000 + b.Y)
	})
	for _, rp := range places {
		pts := p.rep[rp]
		qs.Standing = append(qs.Standing, protocol.Standing{
			Place: g.placeName(p.lang, rp), Points: pts, Tier: tierLabel(p.lang, pts), Next: quest.NextTier(pts),
		})
	}
	// Enough to grey out the "!" of givers that cannot hand out anything (same rule as canOffer).
	qs.Full = p.openQuests() >= quest.MaxActive
	now := g.now()
	for _, q := range p.quests {
		if left := q.PauseLeft(now); left > 0 {
			qs.Pauses = append(qs.Pauses, protocol.GiverPause{RX: q.Giver.Region.X, RY: q.Giver.Region.Y, Idx: q.Giver.Index, Secs: int(left.Seconds()) + 1})
		}
	}
	qs.Notice, p.questNotices = p.questNotices, nil
	p.sendQuestBook(qs)
}

func tierLabel(l i18n.Lang, points int) string { return quest.TierLabel(l, points) }

// questsChanged records a durable change (save) and updates the book.
func (g *Game) questsChanged(p *player) {
	p.questsDirty = true
	p.refreshQuestFlags()
	g.sendQuests(p)
}

// ---------------------------------------------------------------- progression

// questKill counts a victory over a monster. Only victories obtained
// after acceptance count (a quest is only created on acceptance). Returns journal lines.
func (g *Game) questKill(p *player, sp species) []string {
	var log []string
	changed := false
	for i := range p.quests {
		q := &p.quests[i]
		if q.State != quest.Active {
			continue
		}
		o := q.Objective
		if !(o.Kind == quest.Kill && o.Species == sp.Kind) && !(o.Kind == quest.Boss && sp.Boss && !sp.Dungeon) { // a dungeon's boss is not a fortress lord
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

// questMoved checks the "reach a place" goals after a move.
func (g *Game) questMoved(p *player) {
	s := g.World.StructureAt(p.pos.X, p.pos.Y)
	if s == nil {
		return
	}
	changed := false
	for i := range p.quests {
		q := &p.quests[i]
		if q.State == quest.Active && q.Objective.Kind == quest.Reach && q.Objective.Target == s.Region {
			if q.Advance(1) {
				changed = true
				p.questNotices = append(p.questNotices, g.progressLine(p, *q))
			}
		}
	}
	if changed {
		g.questsChanged(p)
		g.save(p)
	}
}

func (g *Game) progressLine(p *player, q quest.Quest) string {
	if q.State == quest.Ready {
		giver := g.questInfo(p.lang, q).Giver
		return i18n.T(p.lang, "quest.ready", q.Title, giver)
	}
	return i18n.T(p.lang, "quest.progress", q.Title, q.Progress, q.Need())
}

// ---------------------------------------------------------------- giver and proposal

// canOffer says whether this NPC can hand a new quest to this player now (never to a race it hates
// beyond world.HateNoQuest).
func (g *Game) canOffer(p *player, npc world.NPC) bool {
	if !npc.GivesQuests || p.openQuests() >= quest.MaxActive {
		return false
	}
	if npc.HatredOf(p.stats.Race) >= world.HateNoQuest { // hands no work to a race it hates
		return false
	}
	now := g.now()
	for _, q := range p.quests {
		if q.Giver != npc.Key() {
			continue
		}
		if q.Open() || q.PauseLeft(now) > 0 {
			return false
		}
	}
	return true
}

// offerContext prepares what the model may propose, or nil if this NPC has nothing to hand out.
func (g *Game) offerContext(p *player, npc world.NPC) *npcai.OfferContext {
	if !g.canOffer(p, npc) {
		return nil
	}
	c := quest.Context{Level: p.stats.Level, Giver: npc.Key(), Places: map[string]world.RegionPos{}, Now: g.now()}
	for _, sp := range speciesList {
		if !sp.Garrison && sp.XP <= speciesXPBase+speciesXPPerLvl*p.stats.Level {
			c.Species = append(c.Species, quest.Species{Kind: sp.Kind, XP: sp.XP})
		}
	}
	if len(c.Species) == 0 {
		return nil
	}
	rr := 3
	ambiguous := map[string]bool{}
	for ry := npc.Region.Y - rr; ry <= npc.Region.Y+rr; ry++ {
		for rx := npc.Region.X - rr; rx <= npc.Region.X+rr; rx++ {
			rp := world.RegionPos{X: rx, Y: ry}
			s := g.World.StructureOfRegion(rp)
			switch {
			case s == nil || rp == npc.Region:
			case s.Kind == world.KindFortress:
				c.BossAround = true
			default:
				name := lowerName(s.NameIn(p.lang))
				if other, dup := c.Places[name]; dup && other != rp {
					ambiguous[name] = true // two places with the same name: none is proposed
				}
				c.Places[name] = rp
			}
		}
	}
	for name := range ambiguous {
		delete(c.Places, name)
	}
	c.Dungeons = g.dungeonRefs(p, npc)
	o := &npcai.OfferContext{Quest: c}
	for _, q := range p.quests {
		if q.Giver == npc.Key() {
			o.Existing = append(o.Existing, q.Title)
		}
	}
	return o
}

// proposeQuest presents the quest proposed by the model, if it is still acceptable.
func (g *Game) proposeQuest(p *player, q *quest.Quest) {
	t := p.talk
	if t == nil || q == nil || t.proposal != nil || !g.canOffer(p, t.npc) {
		return
	}
	for _, o := range p.quests {
		if o.Open() && quest.SameObjective(o, *q) {
			return
		}
	}
	t.proposal = q
	t.prevMode, t.mode = t.mode, protocol.NPCModeQuest
}

func (g *Game) questAct(p *player, action, arg string) []string {
	t := p.talk
	switch action {
	case protocol.NPCQuestAccept:
		if t.mode != protocol.NPCModeQuest || t.proposal == nil {
			return nil
		}
		q := *t.proposal
		t.proposal = nil
		t.mode = protocol.NPCModeChat
		if !g.canOffer(p, t.npc) {
			return []string{i18n.T(p.lang, "quest.limit")}
		}
		q.ID, q.Account, q.CreatedAt = p.nextQuestID, p.name, g.now()
		p.nextQuestID++
		p.quests = append(p.quests, q)
		g.revealDungeonOf(p, q) // accepting a dungeon reveals its entrance
		g.questsChanged(p)
		g.save(p)
		return []string{i18n.T(p.lang, "quest.accepted", q.Title)}
	case protocol.NPCQuestRefuse:
		if t.mode != protocol.NPCModeQuest {
			return nil
		}
		t.proposal = nil
		t.mode = protocol.NPCModeChat
		return []string{i18n.T(p.lang, "quest.refused")}
	case protocol.NPCTurnIn:
		return g.turnIn(p, arg)
	}
	return nil
}

// turnIn hands in a ready quest to its giver: reward, reputation, place memory.
func (g *Game) turnIn(p *player, arg string) []string {
	id, err := strconv.ParseInt(arg, 10, 64)
	q := p.questByID(id)
	if err != nil || q == nil || q.State != quest.Ready || q.Giver != p.talk.npc.Key() {
		return []string{i18n.T(p.lang, "quest.nothing_to_turn_in")}
	}
	q.State, q.DoneAt = quest.Done, g.now()
	gold := p.stats.AddGold(q.Reward.Gold)
	potions := p.stats.AddPotions(q.Reward.Potions)
	levels := p.stats.AddXP(q.Reward.XP, g.rng)
	if levels > 0 {
		p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
	}
	repLog := g.addRep(p, q.Place(), q.ReputationGain())

	giver := p.talk.npc.NameIn(p.lang)
	log := []string{
		i18n.T(p.lang, "quest.thanks", giver, q.Title),
		i18n.T(p.lang, "quest.reward", gold, q.Reward.XP, potions),
	}
	if levels > 0 {
		log = append(log, i18n.T(p.lang, "fight.level_up", p.stats.Level))
	}
	log = append(log, repLog...)
	g.questsChanged(p)
	g.save(p) // the hand-in changes gold, XP, reputation and place memory: immediate write
	return log
}

// addRep changes the player's reputation in a place (bounded by quest.MinReputation) and
// returns the journal line if the tier changes. It does not save: that is up to the caller.
func (g *Game) addRep(p *player, place world.RegionPos, delta int) []string {
	before := p.rep[place]
	after := max(quest.MinReputation, before+delta)
	if after == 0 {
		delete(p.rep, place)
	} else {
		p.rep[place] = after
	}
	if tierLabel(p.lang, after) == tierLabel(p.lang, before) {
		return nil
	}
	return []string{i18n.T(p.lang, "quest.rep_up", g.placeName(p.lang, place), tierLabel(p.lang, after))}
}

// abandonRepLoss is the reputation lost by abandoning an accepted quest.
const abandonRepLoss = 1

// abandonQuest abandons an ongoing quest (from the quest book).
func (g *Game) abandonQuest(p *player, id int64) {
	q := p.questByID(id)
	if q == nil || !q.Open() {
		return
	}
	title, place := q.Title, q.Place()
	p.quests = slices.DeleteFunc(p.quests, func(x quest.Quest) bool { return x.ID == id })
	p.questNotices = append(p.questNotices, i18n.T(p.lang, "quest.abandoned", title))
	p.questNotices = append(p.questNotices, g.addRep(p, place, -abandonRepLoss)...)
	g.questsChanged(p)
	g.save(p)
}

// QuestAct applies a quest book action (called from the network session).
func (g *Game) QuestAct(id int, action string, questID int64) {
	g.do(func() {
		p, ok := g.players[id]
		if !ok {
			return
		}
		switch action {
		case protocol.QuestAbandon:
			g.abandonQuest(p, questID)
		}
	})
}

// RefreshQuests returns the book (e.g. on opening), after expiring what must expire.
func (g *Game) RefreshQuests(id int) {
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.expireQuests(p)
			g.sendQuests(p)
		}
	})
}

func lowerName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

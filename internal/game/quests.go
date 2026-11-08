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

// Quêtes. Le modèle de langage ne fait que PROPOSER (voir quest.Validate) ; tout ce qui
// compte (acceptation, progression, remise, récompense, réputation) est décidé ici,
// dans la goroutine de jeu, à partir de valeurs déjà bornées.

const (
	maxBookHistory  = 10 // quêtes rendues montrées dans le livre
	maxKeptDone     = 40 // quêtes rendues gardées par joueur
	proposalMaxAge  = 20 * time.Minute
	speciesXPBase   = 6 // une espèce est proposée si son XP ≤ speciesXPBase + speciesXPPerLvl × niveau
	speciesXPPerLvl = 3
)

// now est l'horloge des quêtes (remplaçable en test).
func (g *Game) now() time.Time {
	if g.Clock != nil {
		return g.Clock()
	}
	return time.Now()
}

// ---------------------------------------------------------------- état du joueur

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

// refreshQuestFlags recalcule ce qui évite de parcourir les quêtes à chaque pas.
func (p *player) refreshQuestFlags() {
	p.hasOpenReach = false
	for _, q := range p.quests {
		if q.State == quest.Active && q.Objective.Kind == quest.Reach {
			p.hasOpenReach = true
		}
	}
}

// loadQuests restaure les quêtes sauvegardées (valeurs relues de la base, donc revérifiées).
func (g *Game) loadQuests(p *player, saved *Saved) {
	for rp, pts := range saved.Rep {
		if pts > 0 {
			p.rep[rp] = pts
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

// expireQuests fait expirer les quêtes trop anciennes et élague l'historique.
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
	for done > maxKeptDone { // supprime les plus anciennes rendues
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

// ---------------------------------------------------------------- envoi au client

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

// goalText décrit l'objectif d'une quête dans la langue du joueur.
func (g *Game) goalText(l i18n.Lang, q quest.Quest) string {
	o := q.Objective
	switch o.Kind {
	case quest.Kill:
		return i18n.T(l, "quest.goal.kill", i18n.T(l, "monster."+o.Species), o.Count)
	case quest.Boss:
		return i18n.T(l, "quest.goal.boss", i18n.T(l, "monster.warlord"))
	case quest.Reach:
		return i18n.T(l, "quest.goal.reach", g.placeName(l, o.Target))
	}
	return ""
}

// sendQuests envoie le livre de quêtes complet.
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
	qs.Notice, p.questNotices = p.questNotices, nil
	p.sendQuestBook(qs)
}

func tierLabel(l i18n.Lang, points int) string { return quest.TierLabel(l, points) }

// questsChanged note un changement durable (sauvegarde) et met le livre à jour.
func (g *Game) questsChanged(p *player) {
	p.questsDirty = true
	p.refreshQuestFlags()
	g.sendQuests(p)
}

// ---------------------------------------------------------------- progression

// questKill compte une victoire sur un monstre. Seules comptent les victoires obtenues
// après l'acceptation (une quête ne se crée qu'à l'acceptation). Renvoie des lignes de journal.
func (g *Game) questKill(p *player, sp species) []string {
	var log []string
	changed := false
	for i := range p.quests {
		q := &p.quests[i]
		if q.State != quest.Active {
			continue
		}
		o := q.Objective
		if !(o.Kind == quest.Kill && o.Species == sp.Kind) && !(o.Kind == quest.Boss && sp.Boss) {
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

// questMoved vérifie les objectifs « atteindre un lieu » après un déplacement.
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

// ---------------------------------------------------------------- donneur et proposition

// canOffer dit si ce PNJ peut confier une nouvelle quête à ce joueur maintenant.
func (g *Game) canOffer(p *player, npc world.NPC) bool {
	if !npc.GivesQuests || p.openQuests() >= quest.MaxActive {
		return false
	}
	now := g.now()
	for _, q := range p.quests {
		if q.Giver != npc.Key() {
			continue
		}
		if q.Open() || (q.State == quest.Done && now.Sub(q.DoneAt) < quest.GiverPause) {
			return false
		}
	}
	return true
}

// offerContext prépare ce que le modèle peut proposer, ou nil si ce PNJ n'a rien à confier.
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
					ambiguous[name] = true // deux lieux de même nom : aucun n'est proposé
				}
				c.Places[name] = rp
			}
		}
	}
	for name := range ambiguous {
		delete(c.Places, name)
	}
	o := &npcai.OfferContext{Quest: c}
	for _, q := range p.quests {
		if q.Giver == npc.Key() {
			o.Existing = append(o.Existing, q.Title)
		}
	}
	return o
}

// proposeQuest présente la quête proposée par le modèle, si elle est encore recevable.
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

// turnIn rend une quête prête à son donneur : récompense, réputation, mémoire du lieu.
func (g *Game) turnIn(p *player, arg string) []string {
	id, err := strconv.ParseInt(arg, 10, 64)
	q := p.questByID(id)
	if err != nil || q == nil || q.State != quest.Ready || q.Giver != p.talk.npc.Key() {
		return []string{i18n.T(p.lang, "quest.nothing_to_turn_in")}
	}
	q.State, q.DoneAt = quest.Done, g.now()
	gold := p.stats.AddGold(q.Reward.Gold)
	potions := p.stats.AddPotions(q.Reward.Potions)
	levels := p.stats.AddXP(q.Reward.XP)
	if levels > 0 {
		p.hp = p.stats.MaxHP
	}
	before := p.rep[q.Place()]
	p.rep[q.Place()] = before + q.ReputationGain()

	giver := p.talk.npc.NameIn(p.lang)
	log := []string{
		i18n.T(p.lang, "quest.thanks", giver, q.Title),
		i18n.T(p.lang, "quest.reward", gold, q.Reward.XP, potions),
	}
	if levels > 0 {
		log = append(log, i18n.T(p.lang, "fight.level_up", p.stats.Level))
	}
	if after := p.rep[q.Place()]; tierLabel(p.lang, after) != tierLabel(p.lang, before) {
		log = append(log, i18n.T(p.lang, "quest.rep_up", g.placeName(p.lang, q.Place()), tierLabel(p.lang, after)))
	}
	g.questsChanged(p)
	g.save(p) // la remise change l'or, l'XP, la réputation et la mémoire du lieu : écriture immédiate
	return log
}

// abandonQuest abandonne une quête en cours (depuis le livre de quêtes).
func (g *Game) abandonQuest(p *player, id int64) {
	q := p.questByID(id)
	if q == nil || !q.Open() {
		return
	}
	title := q.Title
	p.quests = slices.DeleteFunc(p.quests, func(x quest.Quest) bool { return x.ID == id })
	p.questNotices = append(p.questNotices, i18n.T(p.lang, "quest.abandoned", title))
	g.questsChanged(p)
	g.save(p)
}

// QuestAct applique une action du livre de quêtes (appelée depuis la session réseau).
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

// RefreshQuests renvoie le livre (ex. à l'ouverture), après avoir fait expirer ce qui doit l'être.
func (g *Game) RefreshQuests(id int) {
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.expireQuests(p)
			g.sendQuests(p)
		}
	})
}

func lowerName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

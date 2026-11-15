package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Talker makes NPCs speak. It is implemented by npcai.Service. Ask never blocks
// and only calls done from a goroutine other than the caller's.
type Talker interface {
	Available() bool
	Provider() string
	Ask(req npcai.Request, done func(npcai.Reply))
}

const (
	// chatIdleTicks: a free discussion tolerates long pauses (reading, typing).
	chatIdleTicks = 1800 // 3 minutes

	maxTranscript = 30 // lines kept in the transcript
	maxHistory    = 12 // exchanges handed to the model
)

// idleLimit is the inactivity delay before the conversation is closed.
func (t *talk) idleLimit() uint64 {
	if t.mode == protocol.NPCModeChat {
		return chatIdleTicks
	}
	return fightIdleTicks
}

func (g *Game) talkerReady() bool { return g.Talker != nil && g.Talker.Available() }

// placeName returns the name of a region's place.
func (g *Game) placeName(l i18n.Lang, rp world.RegionPos) string {
	if s := g.World.StructureOfRegion(rp); s != nil {
		return s.NameIn(l)
	}
	return ""
}

// noticeText is the notice shown before the first discussion: what goes to a
// third-party service, what is kept and shared.
func (g *Game) noticeText(l i18n.Lang) string {
	provider := i18n.T(l, "talk.notice.default_provider")
	if g.Talker != nil {
		if name := g.Talker.Provider(); name != "" {
			provider = name
		}
	}
	return i18n.T(l, "talk.notice", provider)
}

// chatAct handles the actions specific to free discussion.
func (g *Game) chatAct(p *player, action, text string) {
	t := p.talk
	switch action {
	case protocol.NPCTalk:
		if t.mode != protocol.NPCModeMenu {
			return
		}
		if !g.talkerReady() {
			g.sendTalk(p, t.npc.Kind, []string{i18n.T(p.lang, "talk.not_in_mood", t.npc.NameIn(p.lang))}, false)
			return
		}
		if !p.consent {
			t.mode = protocol.NPCModeNotice
			g.sendTalk(p, t.npc.Kind, nil, false)
			return
		}
		g.enterChat(p)

	case protocol.NPCConsent:
		if t.mode != protocol.NPCModeNotice {
			return
		}
		p.consent = true
		g.save(p) // the consent is durable: we do not wait for the periodic save
		g.enterChat(p)

	case protocol.NPCDecline:
		if t.mode != protocol.NPCModeNotice {
			return
		}
		t.mode = protocol.NPCModeMenu
		g.sendTalk(p, t.npc.Kind, []string{i18n.T(p.lang, "talk.keep_silent")}, false)

	case protocol.NPCBack:
		if t.mode == protocol.NPCModeMenu {
			return
		}
		t.mode = protocol.NPCModeMenu
		g.sendTalk(p, t.npc.Kind, nil, false)

	case protocol.NPCSay:
		g.say(p, text)
	}
}

// enterChat switches to free discussion; on first entry, the NPC takes the
// floor (and asks the traveller to introduce themselves if it does not know them).
func (g *Game) enterChat(p *player) {
	t := p.talk
	t.mode = protocol.NPCModeChat
	if !t.opened {
		t.opened = true
		t.pending = true
		g.ask(p, "", true)
	}
	g.sendTalk(p, t.npc.Kind, nil, false)
}

// say sends a player's line to the NPC.
func (g *Game) say(p *player, text string) {
	t := p.talk
	if t.mode != protocol.NPCModeChat || t.pending {
		return // only one line in flight at a time
	}
	text = textsafe.Clean(text, protocol.MaxSayLen)
	if text == "" {
		return
	}
	t.lines = appendLine(t.lines, protocol.NPCLine{Who: "you", Text: text})
	if !g.talkerReady() {
		t.lines = appendLine(t.lines, protocol.NPCLine{Who: "npc", Text: npcai.InCharacter(p.lang, t.npc.NameIn(p.lang), npcai.Unavailable)})
		g.sendTalk(p, t.npc.Kind, nil, false)
		return
	}
	t.pending = true
	g.ask(p, text, false)
	g.sendTalk(p, t.npc.Kind, nil, false)
}

// ask sends the request to the model. The reply comes back in the game goroutine via g.do.
func (g *Game) ask(p *player, text string, opening bool) {
	t := p.talk
	place, kind := i18n.T(p.lang, "talk.this_place"), world.KindVillage
	if s := g.World.StructureOfRegion(t.npc.Region); s != nil {
		place, kind = s.NameIn(p.lang), s.Kind
	}
	req := npcai.Request{
		NPC: t.npc, Lang: p.lang, Place: place, PlaceKind: kind,
		Region: g.World.TerritoryAt(t.npc.Pos.X, t.npc.Pos.Y).NameIn(p.lang),
		Speaker: npcai.Speaker{
			Account: p.name, Race: p.stats.Race, Level: p.stats.Level, Kills: p.stats.Kills,
			BossKills: p.stats.BossKills, Deaths: p.stats.Deaths,
		},
		Message: text, History: slices.Clone(t.history), Opening: opening,
		Reputation: p.rep[t.npc.Region], Impression: i18n.T(p.lang, "hero.impression."+hero.Impression(p.stats.Scores.Mod(hero.CHA))), Offer: g.offerContext(p, t.npc),
		Landmarks: g.landmarksIfKnown(t.npc, p.lang), Residents: g.residentsFor(t.npc, p.lang),
	}
	id, gen := p.id, t.gen
	g.Talker.Ask(req, func(rep npcai.Reply) {
		g.do(func() { g.deliver(id, gen, text, opening, rep) })
	})
}

// deliver adds an NPC's reply to the transcript. A reply that arrives
// after its conversation ended (the player left, or opened another) is ignored.
func (g *Game) deliver(id int, gen uint64, text string, opening bool, rep npcai.Reply) {
	p := g.players[id]
	if p == nil || p.talk == nil || p.talk.gen != gen {
		return
	}
	t := p.talk
	t.pending = false
	t.lines = appendLine(t.lines, protocol.NPCLine{Who: "npc", Text: rep.Text})
	if rep.Remembered {
		t.lines = appendLine(t.lines, protocol.NPCLine{Who: "sys", Text: i18n.T(p.lang, "talk.remembers", t.npc.NameIn(p.lang))})
	}
	if rep.Failure == npcai.OK && !opening {
		t.history = append(t.history, npcai.Turn{Player: text, NPC: rep.Text})
		if len(t.history) > maxHistory {
			t.history = t.history[len(t.history)-maxHistory:]
		}
	}
	if rep.QuestDropped && rep.Failure == npcai.OK {
		t.lines = appendLine(t.lines, protocol.NPCLine{Who: "sys", Text: i18n.T(p.lang, "quest.none", t.npc.NameIn(p.lang))})
	}
	if rep.Quest != nil && rep.Failure == npcai.OK {
		g.proposeQuest(p, rep.Quest) // switches to quest mode if the proposal is still acceptable
	}
	if t.mode == protocol.NPCModeChat || t.mode == protocol.NPCModeQuest { // otherwise the line is kept for the return to discussion
		g.sendTalk(p, t.npc.Kind, nil, false)
	}
}

func appendLine(lines []protocol.NPCLine, l protocol.NPCLine) []protocol.NPCLine {
	lines = append(lines, l)
	if len(lines) > maxTranscript {
		lines = lines[len(lines)-maxTranscript:]
	}
	return lines
}

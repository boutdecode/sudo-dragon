package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Talker fait parler les PNJ. Il est implémenté par npcai.Service. Ask ne bloque
// jamais et n'appelle done que depuis une autre goroutine que l'appelant.
type Talker interface {
	Available() bool
	Provider() string
	Ask(req npcai.Request, done func(npcai.Reply))
}

const (
	// chatIdleTicks: une discussion libre tolère de longues pauses (lecture, saisie).
	chatIdleTicks = 1800 // 3 minutes

	maxTranscript = 30 // lignes conservées dans la transcription
	maxHistory    = 12 // échanges confiés au modèle
)

// idleLimit est le délai d'inactivité avant que la conversation soit fermée.
func (t *talk) idleLimit() uint64 {
	if t.mode == protocol.NPCModeChat {
		return chatIdleTicks
	}
	return fightIdleTicks
}

func (g *Game) talkerReady() bool { return g.Talker != nil && g.Talker.Available() }

// placeName renvoie le nom du lieu d'une région.
func (g *Game) placeName(l i18n.Lang, rp world.RegionPos) string {
	if s := g.World.StructureOfRegion(rp); s != nil {
		return s.NameIn(l)
	}
	return ""
}

// noticeText est l'avis montré avant la première discussion: ce qui part vers un
// service tiers, ce qui est retenu et partagé.
func (g *Game) noticeText(l i18n.Lang) string {
	provider := i18n.T(l, "talk.notice.default_provider")
	if g.Talker != nil {
		if name := g.Talker.Provider(); name != "" {
			provider = name
		}
	}
	return i18n.T(l, "talk.notice", provider)
}

// chatAct traite les actions propres à la discussion libre.
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
		g.save(p) // l'accord est durable: on n'attend pas la sauvegarde périodique
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

// enterChat passe en discussion libre; à la première entrée, le PNJ prend la
// parole (et demande au voyageur de se présenter s'il ne le connaît pas).
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

// say envoie une réplique du joueur au PNJ.
func (g *Game) say(p *player, text string) {
	t := p.talk
	if t.mode != protocol.NPCModeChat || t.pending {
		return // une seule réplique en cours à la fois
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

// ask lance la demande au modèle. La réponse revient dans la goroutine de jeu via g.do.
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
			Account: p.name, Level: p.stats.Level, Kills: p.stats.Kills,
			BossKills: p.stats.BossKills, Deaths: p.stats.Deaths,
		},
		Message: text, History: slices.Clone(t.history), Opening: opening,
		Reputation: p.rep[t.npc.Region], Offer: g.offerContext(p, t.npc),
		Landmarks: g.landmarksIfKnown(t.npc, p.lang),
	}
	id, gen := p.id, t.gen
	g.Talker.Ask(req, func(rep npcai.Reply) {
		g.do(func() { g.deliver(id, gen, text, opening, rep) })
	})
}

// deliver ajoute la réponse d'un PNJ à la transcription. Une réponse qui arrive
// après la fin de sa conversation (le joueur est parti, ou en a ouvert une autre) est ignorée.
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
	if rep.Quest != nil && rep.Failure == npcai.OK {
		g.proposeQuest(p, rep.Quest) // passe en mode quête si la proposition est encore recevable
	}
	if t.mode == protocol.NPCModeChat || t.mode == protocol.NPCModeQuest { // sinon la ligne est gardée pour le retour en discussion
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

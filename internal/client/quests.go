package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Quest book (Q key) and an NPC's offer card. Everything displayed there comes
// from the server: the amounts are those it will actually pay out, never those of an NPC.

const (
	maxQuestText = 300
	maxQuestRows = 60
)

// cleanQuest cleans the texts of a received quest (the client does not trust the server).
func cleanQuest(q protocol.QuestInfo) protocol.QuestInfo {
	q.Title = textsafe.Clean(q.Title, 80)
	q.Story = textsafe.Clean(q.Story, maxQuestText)
	q.Giver = textsafe.Clean(q.Giver, 60)
	q.Place = textsafe.Clean(q.Place, 60)
	q.Goal = textsafe.Clean(q.Goal, 120)
	q.Note = textsafe.Clean(q.Note, 100)
	if q.State != "active" && q.State != "ready" && q.State != "done" {
		q.State = "active"
	}
	return q
}

func cleanQuests(b protocol.Quests) protocol.Quests {
	clean := func(in []protocol.QuestInfo) []protocol.QuestInfo {
		out := make([]protocol.QuestInfo, 0, min(len(in), maxQuestRows))
		for i, q := range in {
			if i >= maxQuestRows {
				break
			}
			out = append(out, cleanQuest(q))
		}
		return out
	}
	b.Open, b.History = clean(b.Open), clean(b.History)
	st := make([]protocol.Standing, 0, min(len(b.Standing), maxQuestRows))
	for i, s := range b.Standing {
		if i >= maxQuestRows {
			break
		}
		s.Place, s.Tier = textsafe.Clean(s.Place, 60), textsafe.Clean(s.Tier, 30)
		st = append(st, s)
	}
	b.Standing = st
	notes := make([]string, 0, len(b.Notice))
	for _, n := range b.Notice {
		if n = textsafe.Clean(n, maxNPCText); n != "" {
			notes = append(notes, n)
		}
	}
	b.Notice = notes
	return b
}

// applyQuests records the book received and shows its notifications in the log.
func (m *Model) applyQuests(b protocol.Quests) {
	b = cleanQuests(b)
	m.quests = b
	m.questsAt = time.Now()
	m.questSel = max(0, min(m.questSel, len(b.Open)-1))
	m.questConfirm = false
	for _, n := range b.Notice {
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: n})
	}
}

// readyQuests counts the quests ready to be handed in.
func (m Model) readyQuests() (open, ready int) {
	for _, q := range m.quests.Open {
		if q.State == "ready" {
			ready++
		}
	}
	return len(m.quests.Open), ready
}

// readyGivers returns the NPCs to whom a ready quest can be handed in (shown as "?" on the map).
func (m Model) readyGivers() map[world.NPCKey]bool {
	var out map[world.NPCKey]bool
	for _, q := range m.quests.Open {
		if q.State != "ready" {
			continue
		}
		if out == nil {
			out = map[world.NPCKey]bool{}
		}
		out[world.NPCKey{Region: world.RegionPos{X: q.GiverRX, Y: q.GiverRY}, Index: q.GiverIdx}] = true
	}
	return out
}

// giverIdle tells whether a giver has nothing to entrust right now (quest already open with them, pause after
// a hand-in, or quota reached): their "!" disappears. Pauses are counted down locally, so the "!"
// comes back without a new server message. A giver who hates the player's race (same deterministic draw as
// the server) never has anything for them either.
func (m Model) giverIdle(k world.NPCKey) bool {
	if m.quests.Full {
		return true
	}
	if m.world != nil {
		if s := m.world.StructureOfRegion(k.Region); s != nil && k.Index >= 0 && k.Index < len(s.NPCs) &&
			s.NPCs[k.Index].HatredOf(hero.Race(m.bag.Race)) >= world.HateNoQuest {
			return true
		}
	}
	for _, q := range m.quests.Open {
		if (world.NPCKey{Region: world.RegionPos{X: q.GiverRX, Y: q.GiverRY}, Index: q.GiverIdx}) == k {
			return true
		}
	}
	for _, p := range m.quests.Pauses {
		if (world.NPCKey{Region: world.RegionPos{X: p.RX, Y: p.RY}, Index: p.Idx}) == k &&
			time.Since(m.questsAt) < time.Duration(p.Secs)*time.Second {
			return true
		}
	}
	return false
}

// questSummary is the side panel line ("" if there is no quest).
func (m Model) questSummary() string {
	open, ready := m.readyQuests()
	if open == 0 {
		return ""
	}
	return " " + goldStyle.Render(m.t("quest.hud", open, ready))
}

// updateQuestKey handles a key in the quest book.
func (m Model) updateQuestKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	confirm := m.questConfirm
	m.questConfirm = false
	switch key {
	case "esc", "q":
		m.questBook = false
	case "tab":
		m.questTab = (m.questTab + 1) % 2
	case "up", "k":
		m.questSel = max(0, m.questSel-1)
	case "down", "j":
		m.questSel = min(max(0, len(m.quests.Open)-1), m.questSel+1)
	case "x":
		if m.questTab != 0 || m.questSel >= len(m.quests.Open) || m.conn == nil {
			break
		}
		if !confirm { // an abandon is final: it takes two presses
			m.questConfirm = true
			break
		}
		m.conn.Send(protocol.TQuestAction, protocol.QuestAction{Action: protocol.QuestAbandon, ID: m.quests.Open[m.questSel].ID})
	}
	return m, nil
}

func (m Model) questRewardLine(q protocol.QuestInfo) string {
	return m.t("quest.book.reward", q.Gold, q.XP, q.Potions)
}

// questView draws the quest book in exactly w x h cells.
func (m Model) questView(w, h int) string {
	if m.questTab == 1 {
		return m.questStandingView(w, h)
	}
	listW := min(36, w/2)
	detailW := w - listW

	var list []string
	if len(m.quests.Open) == 0 {
		list = append(list, "", " "+dimStyle.Render(m.t("quest.book.none")))
	}
	for i, q := range m.quests.Open {
		mark := "  "
		if q.State == "ready" {
			mark = goldStyle.Render("✓ ")
		}
		line := mark + ui.Fit(q.Title, listW-8)
		if i == m.questSel {
			line = ui.Select.Render(" " + ansiStripPad(mark+ui.Fit(q.Title, listW-8), listW-4))
		} else {
			line = " " + line
		}
		list = append(list, line)
	}

	var detail []string
	if m.questSel < len(m.quests.Open) {
		q := m.quests.Open[m.questSel]
		iw := detailW - 4
		detail = append(detail, "", " "+titleStyle.Render(q.Title), "")
		for _, l := range wrap(q.Story, iw) {
			detail = append(detail, " "+l)
		}
		detail = append(detail, "",
			" "+m.t("quest.book.goal", q.Goal),
			" "+m.t("quest.book.progress", q.Progress, q.Need),
			" "+m.t("quest.book.giver", q.Giver, q.Place),
			" "+goldStyle.Render(m.questRewardLine(q)),
			" "+dimStyle.Render(m.t("quest.book.gain", q.Gain)),
		)
		if q.Note != "" {
			detail = append(detail, " "+dimStyle.Render("« "+q.Note+" »"))
		}
		if q.State == "ready" {
			detail = append(detail, "", " "+ui.OK.Render(m.t("quest.book.turn_in", q.Giver)))
		}
		if m.questConfirm {
			detail = append(detail, "", " "+errStyle.Render(m.t("quest.book.confirm")))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.t("quest.book.title"), strings.Join(list, "\n"), listW, h),
		ui.Panel(m.t("quest.book.detail"), strings.Join(detail, "\n"), detailW, h),
	)
}

func ansiStripPad(s string, w int) string {
	return ui.Fit(s, w)
}

// questStandingView shows reputation per place and the latest quests handed in.
func (m Model) questStandingView(w, h int) string {
	leftW := w / 2
	var rep []string
	if len(m.quests.Standing) == 0 {
		rep = append(rep, "", " "+dimStyle.Render(m.t("quest.book.rep_none")))
	}
	for _, s := range m.quests.Standing {
		rep = append(rep, "", " "+titleStyle.Render(s.Place))
		line := fmt.Sprintf(" %s · %d", s.Tier, s.Points)
		if s.Next > 0 {
			line += " " + dimStyle.Render(m.t("quest.book.next", s.Next))
		}
		rep = append(rep, line)
	}
	var done []string
	if len(m.quests.History) == 0 {
		done = append(done, "", " "+dimStyle.Render(m.t("quest.book.history_none")))
	}
	for _, q := range m.quests.History {
		done = append(done, "", " "+q.Title, " "+dimStyle.Render(q.Giver+" · "+q.Place))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.t("quest.book.reputation"), strings.Join(rep, "\n"), leftW, h),
		ui.Panel(m.t("quest.book.history"), strings.Join(done, "\n"), w-leftW, h),
	)
}

// npcQuestView draws a quest offer card in exactly w x h cells.
func (m Model) npcQuestView(w, h int) string {
	d := m.npc
	var body []string
	if q := d.Proposal; q != nil {
		cq := cleanQuest(*q)
		body = append(body, "", " "+titleStyle.Render(d.Name)+" "+dimStyle.Render(m.t("quest.card.proposes")), "", " "+titleStyle.Render(cq.Title), "")
		for _, l := range wrap(cq.Story, w-4) {
			body = append(body, " "+l)
		}
		body = append(body, "",
			" "+m.t("quest.book.goal", cq.Goal),
			" "+goldStyle.Render(m.questRewardLine(cq)),
			" "+dimStyle.Render(m.t("quest.book.gain", cq.Gain)),
		)
		if cq.Note != "" {
			body = append(body, " "+dimStyle.Render("« "+cq.Note+" »"))
		}
	}
	body = append(body, "")
	for _, o := range d.Offers {
		body = append(body, " "+fmt.Sprintf("[%s] %s", m.npcOfferKey(o, 0), o.Label))
	}
	return ui.Panel(m.t("quest.card.title"), strings.Join(body, "\n"), w, h)
}

// updateNPCQuestKey handles a key during a quest offer. Enter does nothing:
// accepting must be a deliberate gesture.
func (m Model) updateNPCQuestKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	switch msg.String() {
	case strings.ToLower(m.t("quest.key.accept")):
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCQuestAccept})
	case strings.ToLower(m.t("quest.key.refuse")), "esc":
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCQuestRefuse})
	}
	return m, nil
}

// npcOfferKey gives the key of an offer. Quest hand-ins, which share the same action,
// are chosen by a digit (1, 2…) according to their rank among the hand-ins.
func (m Model) npcOfferKey(o protocol.NPCOffer, turnInRank int) string {
	switch o.Action {
	case protocol.NPCQuestAccept:
		return m.t("quest.key.accept")
	case protocol.NPCQuestRefuse:
		return m.t("quest.key.refuse")
	case protocol.NPCTurnIn, protocol.NPCBuyItem, protocol.NPCSellItem, protocol.NPCWhere:
		return strconv.Itoa(turnInRank + 1)
	}
	return m.npcKeyLabel(o.Action)
}

// isNumbered: offers that carry an argument (quest to hand in, item to buy or sell) are chosen by a digit.
func isNumbered(action string) bool {
	return action == protocol.NPCTurnIn || action == protocol.NPCBuyItem || action == protocol.NPCSellItem || action == protocol.NPCWhere
}

// itemOffers returns the equipment offers (those carrying an item sheet), in order.
func (m Model) itemOffers() []protocol.NPCOffer {
	var out []protocol.NPCOffer
	for _, o := range m.npc.Offers {
		if o.Info != nil {
			out = append(out, o)
		}
	}
	return out
}

// numberedOffers returns, in order, the offers chosen by a digit.
func (m Model) numberedOffers() []protocol.NPCOffer {
	var out []protocol.NPCOffer
	for _, o := range m.npc.Offers {
		if isNumbered(o.Action) {
			out = append(out, o)
		}
	}
	return out
}

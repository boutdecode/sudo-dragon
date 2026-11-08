package client

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Livre de quêtes (touche Q) et carte de proposition d'un PNJ. Tout ce qui s'y affiche vient
// du serveur : les quantités sont celles qu'il versera réellement, jamais celles d'un PNJ.

const (
	maxQuestText = 300
	maxQuestRows = 60
)

// cleanQuest nettoie les textes d'une quête reçue (le client ne se fie pas au serveur).
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

// applyQuests enregistre le livre reçu et affiche ses notifications dans le journal.
func (m *Model) applyQuests(b protocol.Quests) {
	b = cleanQuests(b)
	m.quests = b
	m.questSel = max(0, min(m.questSel, len(b.Open)-1))
	m.questConfirm = false
	for _, n := range b.Notice {
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: n})
	}
}

// readyQuests compte les quêtes à rendre.
func (m Model) readyQuests() (open, ready int) {
	for _, q := range m.quests.Open {
		if q.State == "ready" {
			ready++
		}
	}
	return len(m.quests.Open), ready
}

// readyGivers renvoie les PNJ à qui une quête prête peut être rendue (affichés « ? » sur la carte).
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

// questSummary est la ligne du panneau latéral ("" s'il n'y a aucune quête).
func (m Model) questSummary() string {
	open, ready := m.readyQuests()
	if open == 0 {
		return ""
	}
	return " " + goldStyle.Render(m.t("quest.hud", open, ready))
}

// updateQuestKey traite une touche dans le livre de quêtes.
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
		if !confirm { // un abandon est définitif : il faut appuyer deux fois
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

// questView dessine le livre de quêtes dans exactement w x h cellules.
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

// questStandingView montre la réputation par lieu et les dernières quêtes rendues.
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

// npcQuestView dessine la carte de proposition d'une quête dans exactement w x h cellules.
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

// updateNPCQuestKey traite une touche pendant une proposition de quête. Entrée ne fait rien :
// accepter doit être un geste délibéré.
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

// npcOfferKey donne la touche d'une offre. Les remises de quête, qui partagent la même action,
// se choisissent par un chiffre (1, 2…) selon leur rang parmi les remises.
func (m Model) npcOfferKey(o protocol.NPCOffer, turnInRank int) string {
	switch o.Action {
	case protocol.NPCQuestAccept:
		return m.t("quest.key.accept")
	case protocol.NPCQuestRefuse:
		return m.t("quest.key.refuse")
	case protocol.NPCTurnIn, protocol.NPCBuyItem, protocol.NPCSellItem:
		return strconv.Itoa(turnInRank + 1)
	}
	return m.npcKeyLabel(o.Action)
}

// isNumbered : les offres qui portent un argument (quête à rendre, objet à acheter ou à revendre) se choisissent par un chiffre.
func isNumbered(action string) bool {
	return action == protocol.NPCTurnIn || action == protocol.NPCBuyItem || action == protocol.NPCSellItem
}

// numberedOffers renvoie, dans l'ordre, les offres choisies par un chiffre.
func (m Model) numberedOffers() []protocol.NPCOffer {
	var out []protocol.NPCOffer
	for _, o := range m.npc.Offers {
		if isNumbered(o.Action) {
			out = append(out, o)
		}
	}
	return out
}

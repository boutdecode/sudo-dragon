package client

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Discussion libre avec un PNJ : écran d'avis de confidentialité et écran de
// conversation. Le dialogue de métier (acheter, vendre, soigner) reste dans model.go.

const (
	maxNPCText   = 400 // longueur maximale d'une ligne de transcription reçue
	maxNPCNotice = 1200
	inputPanelH  = 3 // cadre + une ligne de saisie
)

func newNPCInput(lang i18n.Lang) textinput.Model {
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = protocol.MaxSayLen
	in.Placeholder = i18n.T(lang, "npc.input_placeholder")
	return in
}

// cleanNPCDialog nettoie les textes d'un dialogue reçu. Le serveur le fait déjà ;
// le client ne s'y fie pas : un serveur tiers ou compromis pourrait glisser des
// séquences d'échappement (écran effacé, titre de fenêtre, presse-papiers) ou des
// marques bidirectionnelles dans ce qu'un PNJ « dit ».
func cleanNPCDialog(d protocol.NPCDialog) protocol.NPCDialog {
	d.Name = textsafe.Clean(d.Name, 60)
	d.Place = textsafe.Clean(d.Place, 60)
	d.Notice = textsafe.Clean(d.Notice, maxNPCNotice)
	d.Specialty = textsafe.Clean(d.Specialty, 100)
	if d.Proposal != nil {
		q := cleanQuest(*d.Proposal)
		d.Proposal = &q
	}
	if len(d.Log) > 0 {
		log := make([]string, 0, len(d.Log))
		for _, l := range d.Log {
			if l = textsafe.Clean(l, maxNPCText); l != "" {
				log = append(log, l)
			}
		}
		d.Log = log
	}
	if len(d.Lines) > 0 {
		lines := make([]protocol.NPCLine, 0, len(d.Lines))
		for _, l := range d.Lines {
			who := l.Who
			if who != "npc" && who != "you" && who != "sys" {
				who = "sys"
			}
			if text := textsafe.Clean(l.Text, maxNPCText); text != "" {
				lines = append(lines, protocol.NPCLine{Who: who, Text: text})
			}
		}
		d.Lines = lines
	}
	if len(d.Offers) > 0 {
		offers := make([]protocol.NPCOffer, len(d.Offers))
		for i, o := range d.Offers {
			o.Label = textsafe.Clean(o.Label, 80)
			o.Arg = textsafe.Clean(o.Arg, 20)
			o.Detail = textsafe.Clean(o.Detail, 60)
			if o.Info != nil {
				it := cleanItem(*o.Info)
				o.Info = &it
			}
			offers[i] = o
		}
		d.Offers = offers
	}
	return d
}

// syncNPCInput prépare le champ de saisie quand le mode du dialogue change.
func (m *Model) syncNPCInput(prevMode string) {
	if m.npc == nil || m.npc.Mode != protocol.NPCModeChat {
		m.npcInput.Blur()
		return
	}
	if prevMode != protocol.NPCModeChat {
		m.npcInput = newNPCInput(m.lang)
		m.npcInput.Focus()
	}
}

// updateNPCChatKey traite une touche pendant la discussion libre : tout ce qui est
// imprimable va dans le champ de saisie (aucune touche de menu n'agit ici).
func (m Model) updateNPCChatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.conn != nil {
			m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCBack})
		}
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.npcInput.Value())
		if text == "" || m.npc.Thinking || m.conn == nil {
			return m, nil
		}
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSay, Text: text})
		m.npcInput.Reset()
		return m, nil
	}
	var cmd tea.Cmd
	m.npcInput, cmd = m.npcInput.Update(msg)
	return m, cmd
}

// updateNPCNoticeKey traite une touche sur l'écran d'avis. Entrée ne fait rien :
// un accord doit être un geste délibéré, pas une touche frappée par réflexe.
func (m Model) updateNPCNoticeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.conn == nil {
		return m, nil
	}
	switch msg.String() {
	case strings.ToLower(m.t("npc.key.consent")):
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCConsent})
	case strings.ToLower(m.t("npc.key.decline")), "esc":
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCDecline})
	}
	return m, nil
}

// npcHelp est la barre de raccourcis du dialogue, selon son mode.
func (m Model) npcHelp() string {
	switch m.npc.Mode {
	case protocol.NPCModeChat:
		return ui.KeyBar([2]string{"↵", m.t("key.send")}, [2]string{"esc", m.t("key.back")})
	case protocol.NPCModeQuest:
		return ui.KeyBar([2]string{strings.ToLower(m.t("quest.key.accept")), m.t("key.accept")}, [2]string{strings.ToLower(m.t("quest.key.refuse")), m.t("key.refuse")}, [2]string{"esc", m.t("key.refuse")})
	case protocol.NPCModeNotice:
		return ui.KeyBar([2]string{strings.ToLower(m.t("npc.key.consent")), m.t("key.accept")}, [2]string{strings.ToLower(m.t("npc.key.decline")), m.t("key.refuse")}, [2]string{"esc", m.t("key.refuse")})
	}
	keys := strings.ToLower(strings.Join([]string{m.t("npc.key.buy"), m.t("npc.key.sell"), m.t("npc.key.heal"), m.t("npc.key.talk")}, " "))
	if len(m.itemOffers()) > 0 {
		return ui.KeyBar([2]string{"tab", m.t("key.inspect")}, [2]string{"↵", m.t("key.choose")}, [2]string{keys, m.t("key.choose")}, [2]string{"esc", m.t("key.quit")})
	}
	return ui.KeyBar([2]string{keys, m.t("key.choose")}, [2]string{"esc", m.t("key.quit")})
}

// npcTitle est le titre du cadre du PNJ : son nom et son lieu (connus des deux côtés
// puisqu'ils sont déterministes), à défaut son métier.
func (m Model) npcTitle() string {
	d := m.npc
	switch {
	case d.Name != "" && d.Place != "":
		return d.Name + " · " + d.Place
	case d.Name != "":
		return d.Name
	}
	return strings.ToUpper(render.NPCName(m.lang, world.NPCKind(d.Kind)))
}

// wrap découpe s en lignes de w colonnes au plus, sans jamais dépasser, même pour un
// mot plus long que la ligne.
func wrap(s string, w int) []string {
	if w < 1 {
		return nil
	}
	return strings.Split(ansi.Wrap(s, w, ""), "\n")
}

// npcNoticeView dessine l'avis de confidentialité dans exactement w x h cellules.
func (m Model) npcNoticeView(w, h int) string {
	d := m.npc
	iw := w - 4
	var body []string
	body = append(body, "")
	body = append(body, " "+titleStyle.Render(d.Name)+" "+dimStyle.Render(m.t("npc.proposes")))
	body = append(body, "")
	for _, l := range wrap(d.Notice, iw) {
		body = append(body, " "+l)
	}
	body = append(body, "")
	for _, o := range d.Offers {
		body = append(body, " "+fmt.Sprintf("[%s] %s", m.npcKeyLabel(o.Action), o.Label))
	}
	body = append(body, "", " "+dimStyle.Render(m.t("npc.decline_hint")))
	return ui.Panel(m.t("npc.before_chat"), strings.Join(body, "\n"), w, h)
}

// transcript met en forme la transcription : lignes colorées par locuteur, retour à la ligne,
// et une indication quand le PNJ réfléchit. Les dernières lignes qui tiennent dans maxLines.
func (m Model) transcript(width, maxLines int) []string {
	d := m.npc
	kind := world.NPCKind(d.Kind)
	npcStyle := render.NPCArtStyle(kind)
	name := d.Name
	if name == "" {
		name = render.NPCName(m.lang, kind)
	}

	var out []string
	for _, l := range d.Lines {
		var prefix, text string
		switch l.Who {
		case "npc":
			prefix = npcStyle.Render(name+":") + " "
			text = l.Text
		case "you":
			prefix = dimStyle.Render(m.t("npc.you")) + " "
			text = l.Text
		default:
			prefix = ""
			text = dimStyle.Render(l.Text)
		}
		// Le préfixe est mesuré sans ses styles ; le retour à la ligne se fait sur le texte brut
		// pour ne jamais couper une séquence de couleur.
		plainPrefix := ansi.Strip(prefix)
		wrapped := wrap(plainPrefix+ansi.Strip(text), width)
		for i, wl := range wrapped {
			switch {
			case i == 0 && prefix != "":
				out = append(out, prefix+strings.TrimPrefix(wl, plainPrefix))
			case l.Who == "sys":
				out = append(out, dimStyle.Render(wl))
			default:
				out = append(out, wl)
			}
		}
	}
	if d.Thinking {
		out = append(out, "", dimStyle.Render(m.t("npc.thinking", name)))
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}

// npcChatView dessine la discussion libre dans exactement w x h cellules : le portrait
// et l'état à gauche, la transcription à droite, la saisie dessous.
func (m Model) npcChatView(w, h int) string {
	d := m.npc
	kind := world.NPCKind(d.Kind)

	leftW := min(28, max(16, w/3))
	rightW := w - leftW
	midH := h - inputPanelH
	iw, ih := leftW-2, midH-2

	status := []string{
		" " + goldStyle.Render(m.t("npc.gold", d.Gold)),
		m.t("npc.potions", d.Potions),
		m.t("npc.hp", d.HP, d.MaxHP),
	}
	var left []string
	if art := render.NPCArt(kind); ih >= len(art)+1+len(status) {
		style := render.NPCArtStyle(kind)
		for _, l := range art {
			left = append(left, ui.Center(style.Render(l), iw))
		}
		left = append(left, "")
	}
	left = append(left, status...)

	tw := rightW - 4
	lines := m.transcript(tw, midH-2)
	body := make([]string, len(lines))
	for i, l := range lines {
		body[i] = " " + l
	}

	in := m.npcInput
	in.SetWidth(max(4, w-8))
	input := " " + in.View()

	mid := lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.npcTitle(), strings.Join(left, "\n"), leftW, midH),
		ui.Panel(m.t("npc.conversation"), strings.Join(body, "\n"), rightW, midH),
	)
	return mid + "\n" + ui.Panel(m.t("npc.you_say"), input, w, inputPanelH)
}

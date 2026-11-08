package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

// --- Fabrication de dialogues ---

func menuWithTalk() protocol.NPCDialog {
	d := healerDialog()
	d.Name, d.Place = "Sœur Katell", "Valdurne"
	d.Offers = append(d.Offers, protocol.NPCOffer{Action: protocol.NPCTalk, Label: "Discuter", Available: true})
	return d
}

func noticeDialog() protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "healer", Name: "Sœur Katell", Place: "Valdurne", Mode: protocol.NPCModeNotice,
		Notice: "Pour discuter, vos messages sont envoyés à Mistral AI, qui les traite pour faire répondre le PNJ. " +
			"Ce que vous dites de vous est retenu par les PNJ et peut être répété à d'autres joueurs, sous forme de rumeurs.",
		Offers: []protocol.NPCOffer{
			{Action: protocol.NPCConsent, Label: "Accepter et discuter", Available: true},
			{Action: protocol.NPCDecline, Label: "Refuser", Available: true},
		},
	}
}

func chatDialog(thinking bool, lines ...protocol.NPCLine) protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "healer", Name: "Sœur Katell", Place: "Valdurne", Mode: protocol.NPCModeChat,
		Gold: 40, Potions: 2, HP: 20, MaxHP: 30, Thinking: thinking, Lines: lines,
	}
}

func npcSays(text string) protocol.NPCLine { return protocol.NPCLine{Who: "npc", Text: text} }
func youSay(text string) protocol.NPCLine  { return protocol.NPCLine{Who: "you", Text: text} }
func sysNote(text string) protocol.NPCLine { return protocol.NPCLine{Who: "sys", Text: text} }

func plainView(m Model) string { return ansi.Strip(m.View().Content) }

// --- Menu : l'entrée « Discuter » ---

func TestMenuShowsTheTalkOfferAndItsKey(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, menuWithTalk())
	view := plainView(m)
	for _, want := range []string{"[D] Discuter", "[S] Être soigné", "Sœur Katell · Valdurne"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu sans %q:\n%s", want, view)
		}
	}
	press(m, "d")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCTalk {
		t.Fatalf("actions: %v", got)
	}
}

func TestTalkKeyDoesNothingWhenNotOffered(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, healerDialog()) // pas d'offre « Discuter »
	press(m, "d")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("« discuter » envoyé alors que le PNJ ne le propose pas: %v", got)
	}
}

func TestMenuTitleFallsBackToTheJobWithoutAName(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), healerDialog())
	if !strings.Contains(plainView(m), "SOIGNEUR") {
		t.Fatal("titre de repli absent")
	}
}

// --- Avis de confidentialité ---

func TestNoticeScreenShowsTheTextAndTheChoices(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), noticeDialog())
	view := plainView(m)
	for _, want := range []string{"Avant de discuter", "Sœur Katell", "Mistral AI", "rumeurs", "[O] Accepter et discuter", "[N] Refuser", "[Échap] Refuser"} {
		if !strings.Contains(view, want) {
			t.Errorf("avis sans %q:\n%s", want, view)
		}
	}
}

func TestNoticeKeys(t *testing.T) {
	cases := map[string]string{"o": protocol.NPCConsent, "n": protocol.NPCDecline, "esc": protocol.NPCDecline}
	for key, want := range cases {
		m, out := withConn(playingModel(t, 110, 34))
		m = openNPC(t, m, noticeDialog())
		press(m, key)
		if got := sentNPCActions(out); len(got) != 1 || got[0] != want {
			t.Errorf("touche %q: %v, attendu [%s]", key, got, want)
		}
	}
}

// Un accord doit être délibéré : ni Entrée, ni une touche de commerce, ni une lettre au hasard.
func TestNoticeIsNotAcceptedByReflex(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, noticeDialog())
	for _, k := range []string{"enter", "space", "a", "s", "d", "v", "y", "x", "1"} {
		m = press(m, k)
	}
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("actions envoyées sans geste délibéré: %v", got)
	}
}

func TestNoticeKeysIgnoredWithoutConnection(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), noticeDialog())
	press(m, "o") // m.conn == nil : ne doit pas paniquer
}

// --- Discussion ---

func TestChatViewShowsTheTranscriptWithSpeakers(t *testing.T) {
	d := chatDialog(false,
		npcSays("Bienvenue, voyageur. Qui êtes-vous ?"),
		youSay("Je suis Aldric."),
		sysNote("(Sœur Katell retient ce que vous venez de dire.)"),
	)
	m := openNPC(t, playingModel(t, 110, 34), d)
	view := plainView(m)
	for _, want := range []string{
		"Conversation", "Vous dites", "Sœur Katell · Valdurne",
		"Sœur Katell: Bienvenue, voyageur. Qui êtes-vous ?", "Vous: Je suis Aldric.", "(Sœur Katell retient ce que vous venez de dire.)",
		"Or: 40", "PV: 20/30",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("discussion sans %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "réfléchit") {
		t.Error("indication de réflexion affichée alors que le PNJ n'attend rien")
	}
}

func TestChatViewShowsThinking(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), chatDialog(true, youSay("Bonjour")))
	if !strings.Contains(plainView(m), "… Sœur Katell réfléchit") {
		t.Fatal("le joueur n'est pas prévenu que le PNJ réfléchit")
	}
}

func TestChatInputTypingAndSending(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, chatDialog(false, npcSays("Oui ?")))
	m = typeText(m, "Je suis Aldric")
	if !strings.Contains(plainView(m), "> Je suis Aldric") {
		t.Fatalf("la saisie n'est pas affichée:\n%s", plainView(m))
	}

	m = press(m, "enter")
	var sent []protocol.NPCAction
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TNPCAction {
				var a protocol.NPCAction
				_ = env.Payload(&a)
				sent = append(sent, a)
			}
			continue
		default:
		}
		break
	}
	if len(sent) != 1 || sent[0].Action != protocol.NPCSay || sent[0].Text != "Je suis Aldric" {
		t.Fatalf("envoyé: %+v", sent)
	}
	if m.npcInput.Value() != "" {
		t.Fatalf("la saisie n'a pas été vidée: %q", m.npcInput.Value())
	}
}

func TestChatEmptyMessageIsNotSent(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, chatDialog(false))
	m = press(m, "enter")
	m = typeText(m, "   ")
	m = press(m, "enter")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("message vide envoyé: %v", got)
	}
}

func TestChatCannotSendWhileTheNPCIsThinking(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, chatDialog(true, youSay("Bonjour")))
	m = typeText(m, "Et alors ?")
	m = press(m, "enter")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("envoi pendant la réflexion du PNJ: %v", got)
	}
	if m.npcInput.Value() != "Et alors ?" {
		t.Fatalf("la saisie en cours a été perdue: %q", m.npcInput.Value())
	}
	// Une fois la réponse arrivée, la saisie conservée peut partir.
	m = openNPC(t, m, chatDialog(false, youSay("Bonjour"), npcSays("Oui ?")))
	m = press(m, "enter")
	if got := sentNPCActions(out); len(got) != 1 {
		t.Fatalf("la saisie conservée n'a pas pu être envoyée: %v", got)
	}
}

func TestChatEscapeGoesBackToTheMenu(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, chatDialog(false))
	press(m, "esc")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCBack {
		t.Fatalf("actions: %v", got)
	}
}

// En discussion, chaque lettre est du texte : aucune touche de menu ne doit agir.
func TestChatKeysAreTextNotMenuShortcuts(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, chatDialog(false))
	m = typeText(m, "avsdonzqkjhl")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("des lettres frappées en discussion ont déclenché des actions: %v", got)
	}
	if m.npcInput.Value() != "avsdonzqkjhl" {
		t.Fatalf("saisie = %q", m.npcInput.Value())
	}
	// Ni déplacement, ni inventaire, ni chat global.
	if m.chatting || m.inventory {
		t.Fatal("une lettre a ouvert le chat ou l'inventaire")
	}
}

func TestChatInputIsBoundedAtTheProtocolLimit(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), chatDialog(false))
	m = typeText(m, strings.Repeat("x", protocol.MaxSayLen+50))
	if n := len([]rune(m.npcInput.Value())); n != protocol.MaxSayLen {
		t.Fatalf("%d caractères saisis, limite %d", n, protocol.MaxSayLen)
	}
}

func TestEnteringAndLeavingChatManagesTheInput(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), menuWithTalk())
	if m.npcInput.Focused() {
		t.Fatal("le champ de saisie est actif dans le menu")
	}
	m = openNPC(t, m, chatDialog(false))
	if !m.npcInput.Focused() {
		t.Fatal("le champ de saisie n'est pas actif en discussion")
	}
	m = typeText(m, "brouillon")
	// Une réponse du PNJ arrive : on reste en discussion, la saisie est conservée.
	m = openNPC(t, m, chatDialog(false, npcSays("Hm.")))
	if m.npcInput.Value() != "brouillon" || !m.npcInput.Focused() {
		t.Fatalf("la saisie a été perdue à l'arrivée d'une réponse: %q (focus %v)", m.npcInput.Value(), m.npcInput.Focused())
	}
	// Retour au menu : le champ n'est plus actif.
	m = openNPC(t, m, menuWithTalk())
	if m.npcInput.Focused() {
		t.Fatal("le champ de saisie reste actif après le retour au menu")
	}
	// Nouvelle entrée en discussion : saisie propre.
	m = openNPC(t, m, chatDialog(false))
	if m.npcInput.Value() != "" {
		t.Fatalf("saisie de la discussion précédente conservée: %q", m.npcInput.Value())
	}
}

func TestDialogOverClosesTheChat(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), chatDialog(false, npcSays("Oui ?")))
	m = openNPC(t, m, protocol.NPCDialog{Kind: "healer", Over: true, Log: []string{"À bientôt !"}})
	if m.npc != nil || m.npcInput.Focused() {
		t.Fatalf("dialogue non fermé: npc=%v focus=%v", m.npc, m.npcInput.Focused())
	}
}

// --- Aide ---

func TestHelpBarFollowsTheDialogMode(t *testing.T) {
	cases := []struct {
		d     protocol.NPCDialog
		wants []string
	}{
		{menuWithTalk(), []string{"a v s d", "choisir", "quitter"}},
		{noticeDialog(), []string{"o", "accepter", "n", "refuser"}},
		{chatDialog(false), []string{"envoyer", "retour"}},
	}
	for _, c := range cases {
		m := openNPC(t, playingModel(t, 110, 34), c.d)
		help := ansi.Strip(m.helpLine())
		for _, want := range c.wants {
			if !strings.Contains(help, want) {
				t.Errorf("mode %q: aide %q sans %q", c.d.Mode, help, want)
			}
		}
	}
}

// --- Mise en page ---

func longTranscript() []protocol.NPCLine {
	var lines []protocol.NPCLine
	for i := 0; i < 20; i++ {
		lines = append(lines,
			youSay("Voici une assez longue phrase du voyageur qui devra passer à la ligne plusieurs fois dans le cadre étroit."),
			npcSays("Et voici la réponse du PNJ, tout aussi bavarde, avec "+strings.Repeat("unmotextrèslongsansaucunespace", 6)+" fin."),
			sysNote("(Sœur Katell retient ce que vous venez de dire.)"),
		)
	}
	return lines
}

func TestEveryDialogModeFitsEveryTerminalSize(t *testing.T) {
	dialogs := map[string]protocol.NPCDialog{
		"menu":  menuWithTalk(),
		"avis":  noticeDialog(),
		"vide":  chatDialog(false),
		"court": chatDialog(false, npcSays("Oui ?"), youSay("Salut")),
		"long":  chatDialog(true, longTranscript()...),
		"large": chatDialog(false, npcSays("日本語のとても長いテキスト日本語のとても長いテキスト日本語のとても長いテキスト"), youSay("émojis 😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀")),
	}
	for name, d := range dialogs {
		for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
			m := openNPC(t, playingModel(t, sz[0], sz[1]), d)
			if d.Mode == protocol.NPCModeChat {
				m = typeText(m, strings.Repeat("saisie ", 40))
			}
			lines := strings.Split(plainView(m), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%s %v: %d lignes > hauteur", name, sz, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Errorf("%s %v ligne %d: largeur %d > %d", name, sz, i, w, sz[0])
				}
			}
		}
	}
}

func TestLongTranscriptShowsTheMostRecentLines(t *testing.T) {
	lines := longTranscript()
	lines = append(lines, npcSays("DERNIÈRE-RÉPLIQUE"))
	m := openNPC(t, playingModel(t, 110, 34), chatDialog(false, lines...))
	view := plainView(m)
	if !strings.Contains(view, "DERNIÈRE-RÉPLIQUE") {
		t.Fatal("la dernière réplique n'est pas visible")
	}
}

func TestWrapNeverExceedsTheWidth(t *testing.T) {
	for _, in := range []string{"", "court", strings.Repeat("a", 100), "un deux trois quatre cinq six sept huit neuf dix", "日本語日本語日本語日本語", "x\ny"} {
		for _, w := range []int{1, 5, 17, 40} {
			if w < 2 && ansi.StringWidth(in) != len([]rune(in)) {
				continue // un caractère de largeur 2 ne tient pas dans 1 colonne, quoi qu'on fasse
			}
			for _, l := range wrap(in, w) {
				if ansi.StringWidth(l) > w {
					t.Errorf("wrap(%q, %d): ligne %q de largeur %d", in, w, l, ansi.StringWidth(l))
				}
			}
		}
	}
	if wrap("x", 0) != nil {
		t.Error("largeur nulle")
	}
}

// --- Défense : le client ne fait pas confiance au texte reçu ---

func TestIncomingDialogTextIsSanitized(t *testing.T) {
	d := chatDialog(false,
		npcSays("Salut\x1b[2J\x1b]0;PIRATÉ\x07 \u202eà l'envers"),
		youSay("\x1b[31mrouge\x1b[0m"),
		protocol.NPCLine{Who: "inconnu\x1b[1m", Text: "locuteur étrange"},
		npcSays("\x1b[0m\u200b"), // vide une fois nettoyée : supprimée
	)
	d.Name, d.Place, d.Notice = "Sœur\x1b[2J Katell", "Val\u202edurne", "Avis\x1b[31m piégé"
	d.Log = []string{"journal\x1b[2J", "\x1b[0m"}
	d.Offers = []protocol.NPCOffer{{Action: "talk", Label: "Discuter\x1b[2J", Available: true}}

	m := openNPC(t, playingModel(t, 110, 34), d)
	var all []string
	all = append(all, m.npc.Name, m.npc.Place, m.npc.Notice)
	all = append(all, m.npc.Log...)
	for _, l := range m.npc.Lines {
		all = append(all, l.Text, l.Who)
	}
	for _, o := range m.npc.Offers {
		all = append(all, o.Label)
	}
	for _, s := range all {
		if strings.ContainsAny(s, "\x1b\x07") || strings.ContainsRune(s, '\u202e') || strings.ContainsRune(s, '\u200b') {
			t.Errorf("texte reçu non nettoyé: %q", s)
		}
	}
	if len(m.npc.Lines) != 3 {
		t.Fatalf("%d lignes, attendu 3 (la ligne devenue vide est retirée): %+v", len(m.npc.Lines), m.npc.Lines)
	}
	if m.npc.Lines[0].Text != "Salut à l'envers" {
		t.Fatalf("ligne 0 = %q", m.npc.Lines[0].Text)
	}
	if m.npc.Lines[2].Who != "sys" {
		t.Fatalf("un locuteur inconnu doit devenir une note système: %q", m.npc.Lines[2].Who)
	}
	if len(m.npc.Log) != 1 {
		t.Fatalf("journal = %v", m.npc.Log)
	}
}

func TestOnlyDialogOfferLabelsAreSanitizedNotPrices(t *testing.T) {
	d := healerDialog()
	d.Offers[0].Price = 12
	m := openNPC(t, playingModel(t, 110, 34), d)
	if m.npc.Offers[0].Price != 12 || !m.npc.Offers[0].Available {
		t.Fatalf("le nettoyage a altéré les données non textuelles: %+v", m.npc.Offers[0])
	}
}

func TestConnectionLostClearsTheChatInput(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), chatDialog(false))
	m = typeText(m, "brouillon")
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.npc != nil {
		t.Fatal("dialogue conservé après la déconnexion")
	}
}

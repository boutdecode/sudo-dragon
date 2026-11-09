package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/server"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

func startServer(t *testing.T) string {
	t.Helper()
	be := storetest.NewBackend(t, 10*time.Millisecond)
	srv := server.New(world.New(1), slog.New(slog.NewTextHandler(io.Discard, nil)), be)
	srv.SetRegistrationLimit(0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); cancel(); <-done })
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

// next lit les événements jusqu'à trouver un message du type voulu.
func next(t *testing.T, c *Conn, typ string) protocol.Envelope {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.In:
			if ev.Err != nil {
				t.Fatalf("connexion fermée: %v", ev.Err)
			}
			if ev.Env.T == typ {
				return ev.Env
			}
		case <-timeout:
			t.Fatalf("message %q non reçu", typ)
		}
	}
}

func TestDialMoveAndChat(t *testing.T) {
	url := startServer(t)

	c, w, err := Dial(context.Background(), url, "alice", storetest.Password, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if w.Name != "alice" || w.Seed != 1 {
		t.Fatalf("welcome: %+v", w)
	}

	var first protocol.Snapshot
	if err := next(t, c, protocol.TSnapshot).Payload(&first); err != nil {
		t.Fatal(err)
	}
	if first.Self.ID != w.ID {
		t.Fatalf("snapshot.self=%d, welcome.id=%d", first.Self.ID, w.ID)
	}

	// Déplacement: une des 4 directions finit par changer la position.
	moved := false
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		c.Send(protocol.TMove, protocol.Move{DX: d[0], DY: d[1]})
		time.Sleep(300 * time.Millisecond)
	drain:
		for {
			select {
			case ev := <-c.In:
				if ev.Env.T == protocol.TSnapshot {
					var s protocol.Snapshot
					_ = ev.Env.Payload(&s)
					if s.Self.X != first.Self.X || s.Self.Y != first.Self.Y {
						moved = true
					}
				}
			default:
				break drain
			}
		}
		if moved {
			break
		}
	}
	if !moved {
		t.Fatal("la position n'a jamais changé")
	}

	c.Send(protocol.TChatSend, protocol.ChatSend{Text: "bonjour"})
	for {
		var ch protocol.Chat
		if err := next(t, c, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if ch.From == "alice" && ch.Text == "bonjour" {
			break
		}
	}
}

func TestDialRejected(t *testing.T) {
	url := startServer(t)
	if _, _, err := Dial(context.Background(), url, "x", storetest.Password, true); err == nil || !strings.Contains(err.Error(), "pseudo") {
		t.Fatalf("erreur attendue sur le pseudo, reçu: %v", err)
	}
	if _, _, err := Dial(context.Background(), "ws://127.0.0.1:1/ws", "alice", storetest.Password, true); err == nil {
		t.Fatal("connexion impossible attendue")
	}
}

// --- Modèle (sans réseau) ---

func playingModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := New("ws://x/ws", "bob")
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
	tm, _ = tm.Update(connectedMsg{welcome: protocol.Welcome{ID: 1, Name: "bob", Seed: 1}})

	snap := protocol.Snapshot{
		Tick: 1,
		Self: protocol.Entity{ID: 1, Name: "bob", X: 5, Y: 5},
		Others: []protocol.Entity{
			{ID: 2, Name: "zoe", X: 8, Y: 6},
		},
	}
	env, _ := protocol.Encode(protocol.TSnapshot, snap)
	e, _ := protocol.Decode(env)
	// conn == nil: waitEvent ne doit pas être exécutée ici, on ignore la Cmd.
	tm, _ = tm.Update(eventMsg{Event{Env: e}})
	return tm.(Model)
}

func TestViewFitsTerminal(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 80}, {60, 20}} {
		m := playingModel(t, sz[0], sz[1])
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lignes > hauteur", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v ligne %d: largeur %d > %d", sz, i, w, sz[0])
			}
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "zoe") {
			t.Errorf("%v: joueur proche absent du panneau", sz)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	m := playingModel(t, 40, 10)
	if !strings.Contains(m.View().Content, "trop petit") {
		t.Fatal("message de terminal trop petit attendu")
	}
}

func TestDirKeys(t *testing.T) {
	cases := map[string][2]int{
		"up": {0, -1}, "z": {0, -1}, "w": {0, -1}, "k": {0, -1},
		"down": {0, 1}, "s": {0, 1}, "j": {0, 1},
		"left": {-1, 0}, "q": {-1, 0}, "a": {-1, 0}, "h": {-1, 0},
		"right": {1, 0}, "d": {1, 0}, "l": {1, 0},
		"y": {-1, -1}, "u": {1, -1}, "b": {-1, 1}, "n": {1, 1},
	}
	for k, want := range cases {
		dx, dy, ok := dirOf(k)
		if !ok || dx != want[0] || dy != want[1] {
			t.Errorf("dirOf(%q) = %d,%d,%v", k, dx, dy, ok)
		}
	}
	if _, _, ok := dirOf("x"); ok {
		t.Error("'x' ne devrait pas être une direction")
	}
}

func TestChatModeCapturesMovementKeys(t *testing.T) {
	m := playingModel(t, 100, 30)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tm.(Model).chatting {
		t.Fatal("Entrée devrait ouvrir le chat")
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if got := tm.(Model).chatInput.Value(); got != "z" {
		t.Fatalf("la touche z devrait être saisie dans le chat, valeur=%q", got)
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tm.(Model).chatting {
		t.Fatal("Échap devrait fermer le chat")
	}
}

func TestChatLinesAndTrim(t *testing.T) {
	m := playingModel(t, 100, 30)
	for i := 0; i < maxChat+50; i++ {
		m.addChat(chatLine{From: "x", Text: "m"})
	}
	if len(m.chat) != maxChat {
		t.Fatalf("historique = %d, attendu %d", len(m.chat), maxChat)
	}
}

func TestFormatChatByChannel(t *testing.T) {
	m := playingModel(t, 100, 30) // le joueur local s'appelle bob
	cases := []struct {
		line chatLine
		want string
	}{
		{chatLine{Channel: protocol.ChanLocal, From: "zoe", Text: "hello"}, "zoe: hello"},
		{chatLine{Channel: protocol.ChanGlobal, From: "zoe", Text: "hello"}, "[G] zoe: hello"},
		{chatLine{Channel: protocol.ChanEmote, From: "zoe", Text: "danse"}, "* zoe danse"},
		{chatLine{Channel: protocol.ChanWhisper, From: "zoe", To: "bob", Text: "psst"}, "zoe vous chuchote: psst"},
		{chatLine{Channel: protocol.ChanWhisper, From: "bob", To: "zoe", Text: "psst"}, "à zoe: psst"},
		{chatLine{Channel: protocol.ChanSystem, Text: "zoe a rejoint le monde"}, "* zoe a rejoint le monde"},
		{chatLine{Channel: chanError, Text: "commande inconnue"}, "! commande inconnue"},
	}
	for _, c := range cases {
		if got := ansi.Strip(m.formatChat(c.line)); got != c.want {
			t.Errorf("%+v -> %q, attendu %q", c.line, got, c.want)
		}
	}
}

func TestChatScroll(t *testing.T) {
	m := playingModel(t, 100, 30)
	for i := 0; i < 20; i++ {
		m.addChat(chatLine{Channel: protocol.ChanLocal, From: "zoe", Text: "m" + string(rune('a'+i))})
	}
	view := func() string { return ansi.Strip(m.View().Content) }

	if !strings.Contains(view(), "zoe: mt") || strings.Contains(view(), "zoe: ma") {
		t.Fatal("en bas, seuls les derniers messages doivent apparaître")
	}

	// PgHaut recule d'une page.
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = tm.(Model)
	if m.chatScroll != chatLines-1 {
		t.Fatalf("chatScroll=%d", m.chatScroll)
	}
	if strings.Contains(view(), "zoe: mt") || !strings.Contains(view(), "historique") {
		t.Fatal("la page précédente doit être affichée avec l'indicateur d'historique")
	}

	// Un nouveau message ne déplace pas la page lue.
	// Les 5 lignes de chat sont dans le cadre juste au-dessus de la ligne d'aide.
	chatRegion := func(s string) []string {
		lines := strings.Split(s, "\n")
		return lines[len(lines)-2-chatLines : len(lines)-2]
	}
	before := chatRegion(view())
	m.addChat(chatLine{Channel: protocol.ChanLocal, From: "zoe", Text: "nouveau"})
	after := chatRegion(view())
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("la page lue a bougé à l'arrivée d'un message:\navant:\n%s\naprès:\n%s",
			strings.Join(before, "\n"), strings.Join(after, "\n"))
	}

	// Retour en bas et bornes.
	for i := 0; i < 10; i++ {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if tm.(Model).chatScroll != 0 {
		t.Fatalf("retour en bas attendu, chatScroll=%d", tm.(Model).chatScroll)
	}
	m.scrollChat(1000)
	if m.chatScroll != m.maxScroll() {
		t.Fatalf("borne haute non respectée: %d", m.chatScroll)
	}
}

func TestConnectionLostReturnsToLogin(t *testing.T) {
	m := playingModel(t, 100, 30)
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.state != stateLogin || !strings.Contains(got.errMsg, "connexion perdue") {
		t.Fatalf("état=%v err=%q", got.state, got.errMsg)
	}
}

func TestLoginViewRenders(t *testing.T) {
	m := New("ws://localhost:8080/ws", "")
	out := ansi.Strip(m.View().Content)
	for _, want := range []string{"sudo", "Bonne chance", "Serveur", "Pseudo", "Mot de passe", "Connexion", "ws://localhost:8080/ws"} {
		if !strings.Contains(out, want) {
			t.Errorf("écran de connexion sans %q", want)
		}
	}
}

// --- Combat ---

// withConn donne au modèle une connexion factice dont on peut lire les envois.
func withConn(m Model) (Model, chan []byte) {
	out := make(chan []byte, 16)
	m.conn = &Conn{out: out}
	return m, out
}

func combatMsg(t *testing.T, c protocol.Combat) eventMsg {
	t.Helper()
	data, err := protocol.Encode(protocol.TCombat, c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := protocol.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return eventMsg{Event{Env: env}}
}

func startCombat(t *testing.T, m Model, log ...string) Model {
	t.Helper()
	tm, _ := m.Update(combatMsg(t, protocol.Combat{
		Monster: "wolf", MonsterHP: 16, MonsterMaxHP: 16, PlayerHP: 30, PlayerMaxHP: 30, Potions: 2, Log: log,
	}))
	return tm.(Model)
}

func press(m Model, s string) Model {
	key := tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	switch s {
	case "enter":
		key = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		key = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		key = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	tm, _ := m.Update(key)
	return tm.(Model)
}

func sentActions(out chan []byte) (actions, others []string) {
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TCombatAction {
				var a protocol.CombatAction
				_ = env.Payload(&a)
				actions = append(actions, a.Action)
			} else {
				others = append(others, env.T)
			}
		default:
			return
		}
	}
}

func TestCombatKeysSendActions(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m, "Un Loup vous attaque !")

	for key, want := range map[string]string{
		"a": "attack", "1": "attack", "d": "defend", "2": "defend",
		"i": "item", "3": "item", "f": "flee", "4": "flee",
	} {
		m = press(m, key)
		got, _ := sentActions(out)
		if len(got) != 1 || got[0] != want {
			t.Errorf("touche %q: actions envoyées %v, attendu [%s]", key, got, want)
		}
	}
}

func TestCombatIgnoresMovementAndChatKeys(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m)

	for _, k := range []string{"z", "q", "s", "h", "j", "k", "l", "w", "y", "x"} {
		m = press(m, k)
	}
	actions, others := sentActions(out)
	if len(actions) != 0 || len(others) != 0 {
		t.Fatalf("des touches hors combat ont émis: actions=%v autres=%v", actions, others)
	}
	if m.chatting {
		t.Fatal("le chat ne doit pas s'ouvrir pendant un combat")
	}
}

func TestCombatMenuNavigation(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m)
	m = press(m, "down")
	m = press(m, "down")
	if m.combatSel != 2 {
		t.Fatalf("combatSel=%d", m.combatSel)
	}
	m = press(m, "enter")
	if got, _ := sentActions(out); len(got) != 1 || got[0] != "item" {
		t.Fatalf("Entrée devait envoyer l'entrée choisie: %v", got)
	}
	m = press(m, "up")
	m = press(m, "up")
	m = press(m, "up") // 2 -> 1 -> 0 -> 3 (boucle)
	if m.combatSel != 3 {
		t.Fatalf("combatSel=%d, attendu 3", m.combatSel)
	}
}

func TestCombatClosesChatInput(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = press(m, "enter") // ouvre le chat
	if !m.chatting {
		t.Fatal("chat non ouvert")
	}
	m = startCombat(t, m)
	if m.chatting {
		t.Fatal("le combat aurait dû fermer la saisie du chat")
	}
}

func TestCombatOverNeedsDismissal(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m)
	tm, _ := m.Update(combatMsg(t, protocol.Combat{
		Monster: "wolf", Round: 2, MonsterHP: 0, MonsterMaxHP: 16, PlayerHP: 20, PlayerMaxHP: 30,
		Over: true, Result: protocol.ResultWon, Log: []string{"Le Loup est vaincu !", "+10 XP."},
	}))
	m = tm.(Model)

	if m.combat == nil || !m.combat.Over {
		t.Fatal("le combat terminé doit rester affiché")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "VICTOIRE") {
		t.Fatal("bannière de victoire absente")
	}

	// Les actions ne partent plus; les autres touches ne ferment pas.
	for _, k := range []string{"a", "d", "x"} {
		m = press(m, k)
	}
	if actions, _ := sentActions(out); len(actions) != 0 {
		t.Fatalf("actions envoyées après la fin: %v", actions)
	}
	if m.combat == nil {
		t.Fatal("une touche quelconque a fermé le combat")
	}

	for _, k := range []string{"enter", "space", "esc"} {
		mm := press(m, k)
		if mm.combat != nil || mm.combatLog != nil {
			t.Errorf("la touche %q aurait dû fermer le combat", k)
		}
	}
}

func TestCombatLogAccumulatesAndResets(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = startCombat(t, m, "Vous attaquez le Loup !")
	tm, _ := m.Update(combatMsg(t, protocol.Combat{Monster: "wolf", MonsterHP: 11, MonsterMaxHP: 16, PlayerHP: 26, PlayerMaxHP: 30, Round: 1,
		Log: []string{"Vous frappez: 5 dégâts.", "Le Loup vous attaque: 4 dégâts."}}))
	m = tm.(Model)
	if len(m.combatLog) != 3 {
		t.Fatalf("journal de %d lignes, attendu 3: %v", len(m.combatLog), m.combatLog)
	}

	// Un nouveau combat (le précédent étant terminé) repart d'un journal vide.
	tm, _ = m.Update(combatMsg(t, protocol.Combat{Monster: "wolf", Over: true, Result: protocol.ResultFled, Log: []string{"Vous prenez la fuite !"}}))
	m = tm.(Model)
	m = startCombat(t, m, "Un Loup vous attaque !")
	if len(m.combatLog) != 1 || m.combatLog[0] != "Un Loup vous attaque !" {
		t.Fatalf("journal non réinitialisé: %v", m.combatLog)
	}
}

func TestCombatLogIsBounded(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = startCombat(t, m)
	for i := 0; i < 100; i++ {
		tm, _ := m.Update(combatMsg(t, protocol.Combat{Monster: "wolf", Log: []string{"x"}}))
		m = tm.(Model)
	}
	if len(m.combatLog) != maxCombatLog {
		t.Fatalf("journal de %d lignes, borne %d", len(m.combatLog), maxCombatLog)
	}
}

func TestCombatViewFitsAndShowsEssentials(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		m := playingModel(t, sz[0], sz[1])
		m = startCombat(t, m, "Un Loup vous attaque !")
		out := ansi.Strip(m.View().Content)
		lines := strings.Split(out, "\n")

		if len(lines) > sz[1] {
			t.Errorf("%v: %d lignes > hauteur", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v ligne %d: largeur %d > %d", sz, i, w, sz[0])
			}
		}
		for _, want := range []string{"COMBAT", "Loup", "16/16", "30/30", "[A] Frapper", "[F] Fuir"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: %q absent de l'écran de combat", sz, want)
			}
		}
		// Le sprite n'apparaît que s'il y a la place; il a un oeil ('o.o') pour le loup.
		hasSprite := strings.Contains(out, "o.o")
		if sz[1] >= 34 && !hasSprite {
			t.Errorf("%v: sprite attendu sur un grand terminal", sz)
		}
	}
}

func TestStatsShownInSidePanel(t *testing.T) {
	m := playingModel(t, 110, 34)
	data, _ := protocol.Encode(protocol.TSnapshot, protocol.Snapshot{
		Tick: 2, Self: protocol.Entity{ID: 1, Name: "bob", X: 5, Y: 5},
		Stats: protocol.Stats{Level: 3, XP: 7, XPNext: 30, HP: 20, MaxHP: 42, Atk: 10, Def: 4, Potions: 2, Gold: 120},
	})
	env, _ := protocol.Decode(data)
	tm, _ := m.Update(eventMsg{Event{Env: env}})
	out := ansi.Strip(tm.(Model).View().Content)
	for _, want := range []string{"Niv 3", "XP 7/30", "20/42", "Atq 10", "Déf 4", "Pot 2", "Or  120"} {
		if !strings.Contains(out, want) {
			t.Errorf("fiche sans %q", want)
		}
	}
}

func TestConnectionLostClearsCombat(t *testing.T) {
	m := startCombat(t, playingModel(t, 110, 34))
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.combat != nil || got.combatLog != nil {
		t.Fatal("le combat aurait dû être effacé à la déconnexion")
	}
}

// --- Écran de connexion ---

func typeText(m Model, text string) Model {
	for _, r := range text {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	return m
}

func key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	tm, cmd := m.Update(k)
	return tm.(Model), cmd
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyCtrlR = tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
)

func TestLoginInitialFocus(t *testing.T) {
	for _, c := range []struct {
		addr, name string
		want       int
	}{
		{"ws://x/ws", "bob", 2}, // tout est prérempli: reste le mot de passe
		{"ws://x/ws", "", 1},
		{"", "", 0},
	} {
		if got := New(c.addr, c.name).focus; got != c.want {
			t.Errorf("New(%q,%q).focus = %d, attendu %d", c.addr, c.name, got, c.want)
		}
	}
}

func TestLoginFocusCycles(t *testing.T) {
	m := New("ws://x/ws", "")
	seen := []int{m.focus}
	for range loginFields {
		m, _ = key(m, keyTab)
		seen = append(seen, m.focus)
	}
	if seen[0] != seen[len(seen)-1] {
		t.Fatalf("Tab ne boucle pas sur %d champs: %v", loginFields, seen)
	}
	uniq := map[int]bool{}
	for _, f := range seen {
		uniq[f] = true
	}
	if len(uniq) != loginFields {
		t.Fatalf("champs visités: %v", seen)
	}
	back, _ := key(New("ws://x/ws", ""), tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if back.focus != 0 {
		t.Fatalf("Maj+Tab depuis le pseudo devrait aller à l'adresse, focus=%d", back.focus)
	}
}

func TestLoginEnterAdvancesThenValidates(t *testing.T) {
	m := New("ws://x/ws", "")
	m = typeText(m, "bob")
	m, cmd := key(m, keyEnter) // pseudo -> mot de passe
	if m.focus != 2 || cmd != nil || m.state != stateLogin {
		t.Fatalf("Entrée sur le pseudo: focus=%d cmd=%v état=%v", m.focus, cmd != nil, m.state)
	}

	m, cmd = key(m, keyEnter) // mot de passe vide
	if cmd != nil || m.state != stateLogin || !strings.Contains(m.errMsg, "mot de passe requis") {
		t.Fatalf("mot de passe vide accepté: état=%v err=%q", m.state, m.errMsg)
	}

	m = typeText(m, "secret-solide")
	m, cmd = key(m, keyEnter)
	if cmd == nil || m.state != stateConnecting || m.errMsg != "" {
		t.Fatalf("connexion non lancée: état=%v err=%q cmd=%v", m.state, m.errMsg, cmd != nil)
	}
}

func TestLoginRequiresNameAndAddress(t *testing.T) {
	m := New("ws://x/ws", "")
	m = typeText(m, "")
	m, _ = key(m, keyEnter) // -> mot de passe, pseudo vide
	m = typeText(m, "secret-solide")
	m, cmd := key(m, keyEnter)
	if cmd != nil || !strings.Contains(m.errMsg, "pseudo") {
		t.Fatalf("pseudo vide accepté: err=%q", m.errMsg)
	}
}

func TestRegisterModeToggleAndPasswordLength(t *testing.T) {
	m := New("ws://x/ws", "bob")
	if m.register || !strings.Contains(ansi.Strip(m.View().Content), "Connexion") {
		t.Fatal("le mode par défaut doit être la connexion")
	}
	m, _ = key(m, keyCtrlR)
	if !m.register || !strings.Contains(ansi.Strip(m.View().Content), "Création de compte") {
		t.Fatal("Ctrl+R doit passer en création de compte")
	}

	// En création, un mot de passe trop court est refusé côté client, sans réseau.
	m = typeText(m, "court")
	m, cmd := key(m, keyEnter)
	if cmd != nil || !strings.Contains(m.errMsg, "caractères minimum") {
		t.Fatalf("mot de passe court accepté: err=%q", m.errMsg)
	}

	// En connexion, la même saisie part au serveur (qui tranchera).
	m, _ = key(m, keyCtrlR)
	if m.register {
		t.Fatal("deuxième Ctrl+R: retour en mode connexion attendu")
	}
	m, cmd = key(m, keyEnter)
	if cmd == nil {
		t.Fatal("en mode connexion, la validation locale ne devrait pas bloquer")
	}
}

func TestPasswordIsMaskedInView(t *testing.T) {
	m := New("ws://x/ws", "bob")
	m = typeText(m, "tres-secret-123")
	out := ansi.Strip(m.View().Content)
	if strings.Contains(out, "tres-secret") || strings.Contains(out, "secret") {
		t.Fatal("le mot de passe est affiché en clair")
	}
	if !strings.Contains(out, strings.Repeat("*", len("tres-secret-123"))) {
		t.Fatalf("masque attendu dans la vue:\n%s", out)
	}
}

func TestPasswordClearedAfterConnect(t *testing.T) {
	m := New("ws://x/ws", "bob")
	m = typeText(m, "tres-secret-123")
	tm, _ := m.Update(connectedMsg{welcome: protocol.Welcome{ID: 1, Name: "bob", Seed: 1}})
	if got := tm.(Model).pass.Value(); got != "" {
		t.Fatalf("mot de passe conservé en mémoire après connexion: %q", got)
	}
}

func TestFailedConnectKeepsFormForRetry(t *testing.T) {
	m := New("ws://x/ws", "bob")
	m = typeText(m, "tres-secret-123")
	tm, _ := m.Update(connectFailedMsg{err: errors.New("identifiants invalides")})
	got := tm.(Model)
	if got.state != stateLogin || got.errMsg != "identifiants invalides" {
		t.Fatalf("état=%v err=%q", got.state, got.errMsg)
	}
	if got.pass.Value() != "tres-secret-123" || got.name.Value() != "bob" {
		t.Fatal("le formulaire devrait être conservé pour réessayer")
	}
}

// Bout en bout: création de compte puis connexion depuis le même formulaire.
func TestLoginFormEndToEnd(t *testing.T) {
	url := startServer(t)
	run := func(register bool) Model {
		m := New(url, "alice")
		if register {
			m, _ = key(m, keyCtrlR)
		}
		m = typeText(m, storetest.Password)
		m, cmd := key(m, keyEnter)
		if cmd == nil {
			t.Fatal("pas de commande de connexion")
		}
		tm, _ := m.Update(cmd())
		return tm.(Model)
	}

	m := run(true)
	if m.state != stateCreating || m.conn == nil || m.create == nil {
		t.Fatalf("création de compte: état=%v err=%q", m.state, m.errMsg)
	}
	m = finishCreation(t, m)
	m.conn.Close()

	// Le départ est asynchrone côté serveur: on réessaie brièvement.
	var again Model
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		again = run(false)
		if again.state == statePlaying {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if again.state != statePlaying {
		t.Fatalf("reconnexion: état=%v err=%q", again.state, again.errMsg)
	}
	again.conn.Close()
}

// --- Dialogue avec un PNJ ---

func npcMsg(t *testing.T, d protocol.NPCDialog) eventMsg {
	t.Helper()
	data, err := protocol.Encode(protocol.TNPC, d)
	if err != nil {
		t.Fatal(err)
	}
	env, err := protocol.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return eventMsg{Event{Env: env}}
}

func merchantDialog(log ...string) protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "merchant", Gold: 55, Potions: 2, HP: 20, MaxHP: 30, Log: log,
		Offers: []protocol.NPCOffer{
			{Action: protocol.NPCBuy, Label: "Acheter une potion", Price: 20, Available: true},
			{Action: protocol.NPCSell, Label: "Vendre une potion", Price: 8, Gain: true, Available: true},
		},
	}
}

func healerDialog() protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "healer", Gold: 0, Potions: 2, HP: 5, MaxHP: 30, Log: []string{"Bonjour."},
		Offers: []protocol.NPCOffer{{Action: protocol.NPCHeal, Label: "Être soigné", Price: 0, Available: true}},
	}
}

func openNPC(t *testing.T, m Model, d protocol.NPCDialog) Model {
	t.Helper()
	tm, _ := m.Update(npcMsg(t, d))
	return tm.(Model)
}

func sentNPCActions(out chan []byte) (actions []string) {
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TNPCAction {
				var a protocol.NPCAction
				_ = env.Payload(&a)
				actions = append(actions, a.Action)
			}
		default:
			return
		}
	}
}

func TestNPCDialogShowsOffersAndPrices(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog("Bienvenue !"))
	out := ansi.Strip(m.View().Content)
	for _, want := range []string{"MARCHAND", "[A] Acheter une potion", "20 or", "[V] Vendre une potion", "+8 or",
		"Or: 55", "Potions: 2", "PV: 20/30", "Bienvenue !", "[Échap] Quitter"} {
		if !strings.Contains(out, want) {
			t.Errorf("écran de dialogue sans %q:\n%s", want, out)
		}
	}
}

func TestHealerFreeOfferIsLabelled(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), healerDialog())
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "SOIGNEUR") || !strings.Contains(out, "[S] Être soigné") || !strings.Contains(out, "gratuit") {
		t.Fatalf("offre gratuite mal affichée:\n%s", out)
	}
}

func TestNPCKeysSendOnlyOfferedActions(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, merchantDialog())

	for key, want := range map[string]string{"a": protocol.NPCBuy, "v": protocol.NPCSell} {
		m = press(m, key)
		got := sentNPCActions(out)
		if len(got) != 1 || got[0] != want {
			t.Errorf("touche %q: actions %v, attendu [%s]", key, got, want)
		}
	}
	// Le marchand ne soigne pas: la touche S ne part pas.
	m = press(m, "s")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("une action non proposée a été envoyée: %v", got)
	}
	// Échap et Entrée quittent.
	for _, k := range []string{"esc", "enter"} {
		m = press(m, k)
		got := sentNPCActions(out)
		if len(got) != 1 || got[0] != protocol.NPCLeave {
			t.Errorf("touche %q: actions %v, attendu [leave]", k, got)
		}
	}
	_ = m
}

func TestHealerKeySendsHeal(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, healerDialog())
	m = press(m, "s")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCHeal {
		t.Fatalf("actions: %v", got)
	}
	m = press(m, "a") // le soigneur ne vend pas
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("achat envoyé au soigneur: %v", got)
	}
}

func TestNPCDialogIgnoresMovementAndChatKeys(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, merchantDialog())
	for _, k := range []string{"z", "q", "d", "h", "j", "k", "l", "w", "y", "x"} {
		m = press(m, k)
	}
	m = press(m, "f") // F est « fuir » en combat, sans effet ici
	var moves int
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TMove {
				moves++
			}
			continue
		default:
		}
		break
	}
	if moves != 0 {
		t.Fatalf("%d déplacements envoyés pendant un dialogue", moves)
	}
}

func TestNPCDialogClosesChatInput(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = press(m, "enter")
	if !m.chatting {
		t.Fatal("chat non ouvert")
	}
	m = openNPC(t, m, merchantDialog())
	if m.chatting {
		t.Fatal("le dialogue aurait dû fermer la saisie du chat")
	}
}

func TestNPCDialogOverClosesAndLogAccumulates(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog("Bienvenue !"))
	m = openNPC(t, m, merchantDialog("Vous achetez une potion pour 20 or."))
	if len(m.npcLog) != 2 {
		t.Fatalf("journal de %d lignes, attendu 2: %v", len(m.npcLog), m.npcLog)
	}

	m = openNPC(t, m, protocol.NPCDialog{Kind: "merchant", Over: true, Log: []string{"À bientôt !"}})
	if m.npc != nil || m.npcLog != nil {
		t.Fatal("un dialogue terminé doit se fermer aussitôt")
	}
	// Un nouveau dialogue repart d'un journal vide.
	m = openNPC(t, m, healerDialog())
	if len(m.npcLog) != 1 || m.npcLog[0] != "Bonjour." {
		t.Fatalf("journal non réinitialisé: %v", m.npcLog)
	}
}

func TestNPCLogIsBounded(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog())
	for i := 0; i < 100; i++ {
		m = openNPC(t, m, merchantDialog("ligne"))
	}
	if len(m.npcLog) != maxNPCLog {
		t.Fatalf("journal de %d lignes, borne %d", len(m.npcLog), maxNPCLog)
	}
}

func TestNPCViewFitsAllTerminalSizes(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		for _, d := range []protocol.NPCDialog{merchantDialog("Bonjour !", "Une potion ?"), healerDialog()} {
			m := openNPC(t, playingModel(t, sz[0], sz[1]), d)
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%v %s: %d lignes > hauteur", sz, d.Kind, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Errorf("%v %s ligne %d: largeur %d > %d", sz, d.Kind, i, w, sz[0])
				}
			}
			out := ansi.Strip(m.View().Content)
			if !strings.Contains(out, "[Échap] Quitter") {
				t.Errorf("%v %s: indication de sortie absente", sz, d.Kind)
			}
			for _, o := range d.Offers {
				if !strings.Contains(out, o.Label) {
					t.Errorf("%v %s: offre %q absente", sz, d.Kind, o.Label)
				}
			}
		}
	}
}

func TestConnectionLostClearsNPCDialog(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog())
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.npc != nil || got.npcLog != nil {
		t.Fatal("le dialogue aurait dû être effacé à la déconnexion")
	}
}

func TestGameClock(t *testing.T) {
	for _, c := range []struct {
		tick      uint64
		day, hour int
	}{
		{0, 1, 8},
		{ticksPerGameHour, 1, 9},
		{16 * ticksPerGameHour, 2, 0},
		{(24*3 + 13) * ticksPerGameHour, 4, 21},
	} {
		if d, h := gameClock(c.tick); d != c.day || h != c.hour {
			t.Errorf("tick %d: jour %d, %dh (attendu %d, %dh)", c.tick, d, h, c.day, c.hour)
		}
	}
}

func TestHeaderShowsOnlinePingAndTime(t *testing.T) {
	m := playingModel(t, 110, 34)
	m.snap.Online, m.snap.Tick = 12, 16*ticksPerGameHour
	m.pingMS, m.hasPing = 28, true
	out := ansi.Strip(m.View().Content)
	for _, want := range []string{"sudo Dragon", "12 en ligne", "28 ms", "Jour 2, 00h"} {
		if !strings.Contains(out, want) {
			t.Errorf("en-tête sans %q", want)
		}
	}
}

func TestPongSetsPing(t *testing.T) {
	m, _ := withConn(playingModel(t, 110, 34))
	m.pingSent = time.Now().Add(-40 * time.Millisecond)
	data, _ := protocol.Encode(protocol.TPong, nil)
	env, _ := protocol.Decode(data)
	tm, _ := m.Update(eventMsg{Event{Env: env}})
	got := tm.(Model)
	if !got.hasPing || got.pingMS < 40 {
		t.Fatalf("ping=%d ok=%v", got.pingMS, got.hasPing)
	}
}

func TestInventoryScreen(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = press(m, "i")
	if !m.inventory {
		t.Fatal("inventaire non ouvert")
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Sac à dos", "Potion de soin", "Détail"} {
		if !strings.Contains(view, want) {
			t.Errorf("inventaire sans %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 110 {
			t.Fatalf("ligne trop large: %q", line)
		}
	}
	m = press(m, "d") // les déplacements sont bloqués tant que le sac est ouvert
	if _, others := sentActions(out); len(others) != 0 {
		t.Fatalf("message émis depuis l'inventaire: %v", others)
	}
	m = press(m, "esc")
	if m.inventory {
		t.Fatal("inventaire non fermé")
	}
}

func TestBackgroundPaintedUnlessTransparent(t *testing.T) {
	m := New("ws://x/ws", "bob")
	if v := m.View(); v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Error("le fond du thème devrait être peint par défaut")
	}
	if v := m.WithTransparent(true).View(); v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Error("--transparent ne doit rien peindre")
	}
}

func TestViewFillsWholeTerminal(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 60}, {300, 90}} {
		m := playingModel(t, sz[0], sz[1])
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%v: %d lignes, attendu %d", sz, len(lines), sz[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%v ligne %d: largeur %d, attendu %d", sz, i, w, sz[0])
				break
			}
		}
	}
}

func TestLoginSudoAlignedWithLogo(t *testing.T) {
	m := New("ws://x/ws", "bob")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	lines := strings.Split(ansi.Strip(tm.(Model).View().Content), "\n")
	col := func(sub string) int {
		for _, l := range lines {
			if i := strings.Index(l, sub); i >= 0 {
				return len([]rune(l[:i]))
			}
		}
		t.Fatalf("%q introuvable", sub)
		return -1
	}
	if a, b := col("$ sudo"), col("████   ████"); a != b {
		t.Errorf("« $ sudo » en colonne %d, logo en colonne %d", a, b)
	}
}

func TestViewHasMargins(t *testing.T) {
	m := playingModel(t, 120, 40)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Error("lignes de marge haute et basse attendues")
	}
	for i, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, strings.Repeat(" ", marginX)) {
			t.Fatalf("ligne %d sans marge gauche: %q", i+1, l)
		}
	}
	// Trop petit pour des marges: elles disparaissent plutôt que de réduire l'intérieur.
	if mx, my := playingModel(t, minWidth, minHeight).margins(); mx != 0 || my != 0 {
		t.Errorf("marges %d,%d sur un terminal minimal", mx, my)
	}
}

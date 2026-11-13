package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
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

// next reads events until a message of the wanted type is found.
func next(t *testing.T, c *Conn, typ string) protocol.Envelope {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.In:
			if ev.Err != nil {
				t.Fatalf("connection closed: %v", ev.Err)
			}
			if ev.Env.T == typ {
				return ev.Env
			}
		case <-timeout:
			t.Fatalf("message %q not received", typ)
		}
	}
}

func TestDialMoveAndChat(t *testing.T) {
	url := startServer(t)

	c, w, err := DialLang(context.Background(), url, "alice", storetest.Password, true, i18n.FR)
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

	// Movement: one of the 4 directions eventually changes the position.
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
		t.Fatal("the position never changed")
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
	if _, _, err := DialLang(context.Background(), url, "x", storetest.Password, true, i18n.FR); err == nil || !strings.Contains(err.Error(), "pseudo") {
		t.Fatalf("error about the username expected, got: %v", err)
	}
	if _, _, err := DialLang(context.Background(), "ws://127.0.0.1:1/ws", "alice", storetest.Password, true, i18n.FR); err == nil {
		t.Fatal("connection failure expected")
	}
}

// --- Model (no network) ---

func playingModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := NewLang("ws://x/ws", "bob", i18n.FR)
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
	// conn == nil: waitEvent must not be run here, the Cmd is ignored.
	tm, _ = tm.Update(eventMsg{Event{Env: e}})
	return tm.(Model)
}

func TestViewFitsTerminal(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 80}, {60, 20}} {
		m := playingModel(t, sz[0], sz[1])
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines > height", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v line %d: width %d > %d", sz, i, w, sz[0])
			}
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "zoe") {
			t.Errorf("%v: nearby player missing from the panel", sz)
		}
	}
}

func TestViewTooSmall(t *testing.T) {
	m := playingModel(t, 40, 10)
	if !strings.Contains(m.View().Content, "trop petit") {
		t.Fatal("terminal-too-small message expected")
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
		t.Error("'x' should not be a direction")
	}
}

func TestChatModeCapturesMovementKeys(t *testing.T) {
	m := playingModel(t, 100, 30)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !tm.(Model).chatting {
		t.Fatal("Enter should open the chat")
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if got := tm.(Model).chatInput.Value(); got != "z" {
		t.Fatalf("the z key should be typed into the chat, value=%q", got)
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tm.(Model).chatting {
		t.Fatal("Escape should close the chat")
	}
}

func TestChatLinesAndTrim(t *testing.T) {
	m := playingModel(t, 100, 30)
	for i := 0; i < maxChat+50; i++ {
		m.addChat(chatLine{From: "x", Text: "m"})
	}
	if len(m.chat) != maxChat {
		t.Fatalf("history = %d, expected %d", len(m.chat), maxChat)
	}
}

func TestFormatChatByChannel(t *testing.T) {
	m := playingModel(t, 100, 30) // the local player is called bob
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
			t.Errorf("%+v -> %q, expected %q", c.line, got, c.want)
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
		t.Fatal("at the bottom, only the latest messages must appear")
	}

	// PgUp goes back one page.
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = tm.(Model)
	if m.chatScroll != chatLines-1 {
		t.Fatalf("chatScroll=%d", m.chatScroll)
	}
	if strings.Contains(view(), "zoe: mt") || !strings.Contains(view(), "historique") {
		t.Fatal("the previous page must be displayed with the history indicator")
	}

	// A new message does not move the page being read.
	// The 5 chat lines are in the frame just above the help line.
	chatRegion := func(s string) []string {
		lines := strings.Split(s, "\n")
		return lines[len(lines)-2-chatLines : len(lines)-2]
	}
	before := chatRegion(view())
	m.addChat(chatLine{Channel: protocol.ChanLocal, From: "zoe", Text: "nouveau"})
	after := chatRegion(view())
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("the page being read moved when a message arrived:\nbefore:\n%s\nafter:\n%s",
			strings.Join(before, "\n"), strings.Join(after, "\n"))
	}

	// Back to the bottom and bounds.
	for i := 0; i < 10; i++ {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if tm.(Model).chatScroll != 0 {
		t.Fatalf("return to the bottom expected, chatScroll=%d", tm.(Model).chatScroll)
	}
	m.scrollChat(1000)
	if m.chatScroll != m.maxScroll() {
		t.Fatalf("upper bound not respected: %d", m.chatScroll)
	}
}

func TestConnectionLostReturnsToLogin(t *testing.T) {
	m := playingModel(t, 100, 30)
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.state != stateLogin || !strings.Contains(got.errMsg, "connexion perdue") {
		t.Fatalf("state=%v err=%q", got.state, got.errMsg)
	}
}

func TestLoginViewRenders(t *testing.T) {
	m := NewLang("ws://localhost:8080/ws", "", i18n.FR)
	out := ansi.Strip(m.View().Content)
	for _, want := range []string{"sudo", "Bonne chance", "Serveur", "Pseudo", "Mot de passe", "Connexion", "ws://localhost:8080/ws"} {
		if !strings.Contains(out, want) {
			t.Errorf("login screen without %q", want)
		}
	}
}

// --- Combat ---

// withConn gives the model a fake connection whose sends can be read.
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
		Enemies: []protocol.Enemy{{Kind: "wolf", Name: "Loup", HP: 16, MaxHP: 16, Def: 11, Hit: 3, Dmg: "1d6+1"}}, PlayerHP: 30, PlayerMaxHP: 30, Potions: 2, Log: log,
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
		"a": "attack", "1": "attack",
		"i": "item", "2": "item", "f": "flee", "3": "flee",
	} {
		m = press(m, key)
		got, _ := sentActions(out)
		if len(got) != 1 || got[0] != want {
			t.Errorf("key %q: actions sent %v, expected [%s]", key, got, want)
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
		t.Fatalf("keys outside combat emitted: actions=%v others=%v", actions, others)
	}
	if m.chatting {
		t.Fatal("the chat must not open during a combat")
	}
}

func TestCombatMenuNavigation(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m)
	m = press(m, "down")
	if m.combatSel != 1 {
		t.Fatalf("combatSel=%d", m.combatSel)
	}
	m = press(m, "enter")
	if got, _ := sentActions(out); len(got) != 1 || got[0] != "item" {
		t.Fatalf("Enter should have sent the chosen entry: %v", got)
	}
	m = press(m, "up")
	m = press(m, "up") // 1 -> 0 -> 2 (wraps)
	if m.combatSel != 2 {
		t.Fatalf("combatSel=%d, expected 2", m.combatSel)
	}
}

func TestCombatClosesChatInput(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = press(m, "enter") // opens the chat
	if !m.chatting {
		t.Fatal("chat not open")
	}
	m = startCombat(t, m)
	if m.chatting {
		t.Fatal("the combat should have closed the chat input")
	}
}

func TestCombatOverNeedsDismissal(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = startCombat(t, m)
	tm, _ := m.Update(combatMsg(t, protocol.Combat{
		Enemies: []protocol.Enemy{{Kind: "wolf", Name: "Loup", HP: 0, MaxHP: 16}}, Round: 2, PlayerHP: 20, PlayerMaxHP: 30,
		Over: true, Result: protocol.ResultWon, Log: []string{"Le Loup est vaincu !", "+10 XP."},
	}))
	m = tm.(Model)

	if m.combat == nil || !m.combat.Over {
		t.Fatal("the finished combat must stay displayed")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "VICTOIRE") {
		t.Fatal("victory banner missing")
	}

	// Actions are no longer sent; other keys do not close.
	for _, k := range []string{"a", "d", "x"} {
		m = press(m, k)
	}
	if actions, _ := sentActions(out); len(actions) != 0 {
		t.Fatalf("actions sent after the end: %v", actions)
	}
	if m.combat == nil {
		t.Fatal("an arbitrary key closed the combat")
	}

	for _, k := range []string{"enter", "space", "esc"} {
		mm := press(m, k)
		if mm.combat != nil || mm.combatLog != nil {
			t.Errorf("key %q should have closed the combat", k)
		}
	}
}

func TestCombatLogAccumulatesAndResets(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = startCombat(t, m, "Vous attaquez le Loup !")
	tm, _ := m.Update(combatMsg(t, protocol.Combat{Enemies: []protocol.Enemy{{Kind: "wolf", Name: "Loup", HP: 11, MaxHP: 16}}, PlayerHP: 26, PlayerMaxHP: 30, Round: 1,
		Log: []string{"Vous frappez: 5 dégâts.", "Le Loup vous attaque: 4 dégâts."}}))
	m = tm.(Model)
	if len(m.combatLog) != 3 {
		t.Fatalf("log of %d lines, expected 3: %v", len(m.combatLog), m.combatLog)
	}

	// A new combat (the previous one being over) starts from an empty log.
	tm, _ = m.Update(combatMsg(t, protocol.Combat{Enemies: []protocol.Enemy{{Kind: "wolf", Name: "Loup", HP: 5, MaxHP: 16}}, Over: true, Result: protocol.ResultFled, Log: []string{"Vous prenez la fuite !"}}))
	m = tm.(Model)
	m = startCombat(t, m, "Un Loup vous attaque !")
	if len(m.combatLog) != 1 || m.combatLog[0] != "Un Loup vous attaque !" {
		t.Fatalf("log not reset: %v", m.combatLog)
	}
}

func TestCombatLogIsBounded(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = startCombat(t, m)
	for i := 0; i < 100; i++ {
		tm, _ := m.Update(combatMsg(t, protocol.Combat{Enemies: []protocol.Enemy{{Kind: "wolf", Name: "Loup", HP: 5, MaxHP: 16}}, Log: []string{"x"}}))
		m = tm.(Model)
	}
	if len(m.combatLog) != maxCombatLog {
		t.Fatalf("log of %d lines, bound %d", len(m.combatLog), maxCombatLog)
	}
}

func TestCombatViewFitsAndShowsEssentials(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		m := playingModel(t, sz[0], sz[1])
		m = startCombat(t, m, "Un Loup vous attaque !")
		out := ansi.Strip(m.View().Content)
		lines := strings.Split(out, "\n")

		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines > height", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v line %d: width %d > %d", sz, i, w, sz[0])
			}
		}
		for _, want := range []string{"COMBAT", "Loup", "16/16", "30/30", "[A] Frapper", "[F] Fuir"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: %q missing from the combat screen", sz, want)
			}
		}
		if sz[1] >= 34 {
			if !strings.Contains(out, "Détail") {
				t.Errorf("%v: Detail panel expected", sz)
			}
			if strings.Contains(out, "Défendre") {
				t.Errorf("%v: the Defend action no longer exists", sz)
			}
		}
		// The sprite only appears if there is room; for the wolf it is the illustration (half-blocks).
		hasSprite := strings.Contains(out, "▀")
		if sz[1] >= 34 && !hasSprite {
			t.Errorf("%v: sprite expected on a large terminal", sz)
		}
	}
}

func TestStatsShownInSidePanel(t *testing.T) {
	m := playingModel(t, 110, 34)
	data, _ := protocol.Encode(protocol.TSnapshot, protocol.Snapshot{
		Tick: 2, Self: protocol.Entity{ID: 1, Name: "bob", X: 5, Y: 5},
		Stats: protocol.Stats{Level: 3, XP: 7, XPNext: 30, HP: 20, MaxHP: 42, Def: 12, Dmg: "1d6+2", Potions: 2, Gold: 120},
	})
	env, _ := protocol.Decode(data)
	tm, _ := m.Update(eventMsg{Event{Env: env}})
	out := ansi.Strip(tm.(Model).View().Content)
	for _, want := range []string{"Niv 3", "XP", "7/30", "PV", "20/42"} {
		if !strings.Contains(out, want) {
			t.Errorf("sheet without %q", want)
		}
	}
	for _, gone := range []string{"Déf 12", "Dég 1d6+2", "Pot 2", "Or  120"} {
		if strings.Contains(out, gone) {
			t.Errorf("sheet with %q", gone)
		}
	}
}

func TestConnectionLostClearsCombat(t *testing.T) {
	m := startCombat(t, playingModel(t, 110, 34))
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	got := tm.(Model)
	if got.combat != nil || got.combatLog != nil {
		t.Fatal("the combat should have been cleared on disconnection")
	}
}

// --- Login screen ---

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
		{"ws://x/ws", "bob", 2}, // everything is prefilled: only the password remains
		{"ws://x/ws", "", 1},
		{"", "", 0},
	} {
		if got := NewLang(c.addr, c.name, i18n.FR).focus; got != c.want {
			t.Errorf("New(%q,%q).focus = %d, expected %d", c.addr, c.name, got, c.want)
		}
	}
}

func TestLoginFocusCycles(t *testing.T) {
	m := NewLang("ws://x/ws", "", i18n.FR)
	seen := []int{m.focus}
	for range loginFields {
		m, _ = key(m, keyTab)
		seen = append(seen, m.focus)
	}
	if seen[0] != seen[len(seen)-1] {
		t.Fatalf("Tab does not wrap over %d fields: %v", loginFields, seen)
	}
	uniq := map[int]bool{}
	for _, f := range seen {
		uniq[f] = true
	}
	if len(uniq) != loginFields {
		t.Fatalf("fields visited: %v", seen)
	}
	back, _ := key(NewLang("ws://x/ws", "", i18n.FR), tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if back.focus != 0 {
		t.Fatalf("Shift+Tab from the username should go to the address, focus=%d", back.focus)
	}
}

func TestLoginEnterAdvancesThenValidates(t *testing.T) {
	m := NewLang("ws://x/ws", "", i18n.FR)
	m = typeText(m, "bob")
	m, cmd := key(m, keyEnter) // username -> password
	if m.focus != 2 || cmd != nil || m.state != stateLogin {
		t.Fatalf("Enter on the username: focus=%d cmd=%v state=%v", m.focus, cmd != nil, m.state)
	}

	m, cmd = key(m, keyEnter) // empty password
	if cmd != nil || m.state != stateLogin || !strings.Contains(m.errMsg, "mot de passe requis") {
		t.Fatalf("empty password accepted: state=%v err=%q", m.state, m.errMsg)
	}

	m = typeText(m, "secret-solide")
	m, cmd = key(m, keyEnter)
	if cmd == nil || m.state != stateConnecting || m.errMsg != "" {
		t.Fatalf("login not started: state=%v err=%q cmd=%v", m.state, m.errMsg, cmd != nil)
	}
}

func TestLoginRequiresNameAndAddress(t *testing.T) {
	m := NewLang("ws://x/ws", "", i18n.FR)
	m = typeText(m, "")
	m, _ = key(m, keyEnter) // -> password, empty username
	m = typeText(m, "secret-solide")
	m, cmd := key(m, keyEnter)
	if cmd != nil || !strings.Contains(m.errMsg, "pseudo") {
		t.Fatalf("empty username accepted: err=%q", m.errMsg)
	}
}

func TestRegisterModeToggleAndPasswordLength(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	if m.register || !strings.Contains(ansi.Strip(m.View().Content), "Connexion") {
		t.Fatal("the default mode must be login")
	}
	m, _ = key(m, keyCtrlR)
	if !m.register || !strings.Contains(ansi.Strip(m.View().Content), "Création de compte") {
		t.Fatal("Ctrl+R must switch to account creation")
	}

	// In creation mode, a too-short password is refused client-side, without network.
	m = typeText(m, "court")
	m, cmd := key(m, keyEnter)
	if cmd != nil || !strings.Contains(m.errMsg, "caractères minimum") {
		t.Fatalf("short password accepted: err=%q", m.errMsg)
	}

	// In login mode, the same input goes to the server (which will decide).
	m, _ = key(m, keyCtrlR)
	if m.register {
		t.Fatal("second Ctrl+R: return to login mode expected")
	}
	m, cmd = key(m, keyEnter)
	if cmd == nil {
		t.Fatal("in login mode, local validation should not block")
	}
}

func TestPasswordIsMaskedInView(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	m = typeText(m, "tres-secret-123")
	out := ansi.Strip(m.View().Content)
	if strings.Contains(out, "tres-secret") || strings.Contains(out, "secret") {
		t.Fatal("the password is displayed in clear")
	}
	if !strings.Contains(out, strings.Repeat("*", len("tres-secret-123"))) {
		t.Fatalf("mask expected in the view:\n%s", out)
	}
}

func TestPasswordClearedAfterConnect(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	m = typeText(m, "tres-secret-123")
	tm, _ := m.Update(connectedMsg{welcome: protocol.Welcome{ID: 1, Name: "bob", Seed: 1}})
	if got := tm.(Model).pass.Value(); got != "" {
		t.Fatalf("password kept in memory after login: %q", got)
	}
}

func TestFailedConnectKeepsFormForRetry(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	m = typeText(m, "tres-secret-123")
	tm, _ := m.Update(connectFailedMsg{err: errors.New("invalid credentials")})
	got := tm.(Model)
	if got.state != stateLogin || got.errMsg != "invalid credentials" {
		t.Fatalf("state=%v err=%q", got.state, got.errMsg)
	}
	if got.pass.Value() != "tres-secret-123" || got.name.Value() != "bob" {
		t.Fatal("the form should be kept to retry")
	}
}

// End to end: account creation then login from the same form.
func TestLoginFormEndToEnd(t *testing.T) {
	url := startServer(t)
	run := func(register bool) Model {
		m := NewLang(url, "alice", i18n.FR)
		if register {
			m, _ = key(m, keyCtrlR)
		}
		m = typeText(m, storetest.Password)
		m, cmd := key(m, keyEnter)
		if cmd == nil {
			t.Fatal("no login command")
		}
		tm, _ := m.Update(cmd())
		return tm.(Model)
	}

	m := run(true)
	if m.state != stateCreating || m.conn == nil || m.create == nil {
		t.Fatalf("account creation: state=%v err=%q", m.state, m.errMsg)
	}
	m = finishCreation(t, m)
	m.conn.Close()

	// Leaving is asynchronous on the server side: we retry briefly.
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
		t.Fatalf("reconnection: state=%v err=%q", again.state, again.errMsg)
	}
	again.conn.Close()
}

// --- Dialogue with an NPC ---

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
			t.Errorf("dialogue screen without %q:\n%s", want, out)
		}
	}
}

func TestHealerFreeOfferIsLabelled(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), healerDialog())
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "SOIGNEUR") || !strings.Contains(out, "[S] Être soigné") || !strings.Contains(out, "gratuit") {
		t.Fatalf("free offer badly displayed:\n%s", out)
	}
}

func TestNPCKeysSendOnlyOfferedActions(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, merchantDialog())

	for key, want := range map[string]string{"a": protocol.NPCBuy, "v": protocol.NPCSell} {
		m = press(m, key)
		got := sentNPCActions(out)
		if len(got) != 1 || got[0] != want {
			t.Errorf("key %q: actions %v, expected [%s]", key, got, want)
		}
	}
	// The merchant does not heal: the S key sends nothing.
	m = press(m, "s")
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("an action not offered was sent: %v", got)
	}
	// Escape and Enter leave.
	for _, k := range []string{"esc", "enter"} {
		m = press(m, k)
		got := sentNPCActions(out)
		if len(got) != 1 || got[0] != protocol.NPCLeave {
			t.Errorf("key %q: actions %v, expected [leave]", k, got)
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
	m = press(m, "a") // the healer does not sell
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("purchase sent to the healer: %v", got)
	}
}

func TestNPCDialogIgnoresMovementAndChatKeys(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, merchantDialog())
	for _, k := range []string{"z", "q", "d", "h", "j", "k", "l", "w", "y", "x"} {
		m = press(m, k)
	}
	m = press(m, "f") // F is "flee" in combat, no effect here
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
		t.Fatalf("%d moves sent during a dialogue", moves)
	}
}

func TestNPCDialogClosesChatInput(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = press(m, "enter")
	if !m.chatting {
		t.Fatal("chat not open")
	}
	m = openNPC(t, m, merchantDialog())
	if m.chatting {
		t.Fatal("the dialogue should have closed the chat input")
	}
}

func TestNPCDialogOverClosesAndLogAccumulates(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog("Bienvenue !"))
	m = openNPC(t, m, merchantDialog("Vous achetez une potion pour 20 or."))
	if len(m.npcLog) != 2 {
		t.Fatalf("log of %d lines, expected 2: %v", len(m.npcLog), m.npcLog)
	}

	m = openNPC(t, m, protocol.NPCDialog{Kind: "merchant", Over: true, Log: []string{"À bientôt !"}})
	if m.npc != nil || m.npcLog != nil {
		t.Fatal("a finished dialogue must close immediately")
	}
	// A new dialogue starts from an empty log.
	m = openNPC(t, m, healerDialog())
	if len(m.npcLog) != 1 || m.npcLog[0] != "Bonjour." {
		t.Fatalf("log not reset: %v", m.npcLog)
	}
}

func TestNPCLogIsBounded(t *testing.T) {
	m := openNPC(t, playingModel(t, 110, 34), merchantDialog())
	for i := 0; i < 100; i++ {
		m = openNPC(t, m, merchantDialog("ligne"))
	}
	if len(m.npcLog) != maxNPCLog {
		t.Fatalf("log of %d lines, bound %d", len(m.npcLog), maxNPCLog)
	}
}

func TestNPCViewFitsAllTerminalSizes(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		for _, d := range []protocol.NPCDialog{merchantDialog("Bonjour !", "Une potion ?"), healerDialog()} {
			m := openNPC(t, playingModel(t, sz[0], sz[1]), d)
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%v %s: %d lines > height", sz, d.Kind, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Errorf("%v %s line %d: width %d > %d", sz, d.Kind, i, w, sz[0])
				}
			}
			out := ansi.Strip(m.View().Content)
			if !strings.Contains(out, "[Échap] Quitter") {
				t.Errorf("%v %s: exit hint missing", sz, d.Kind)
			}
			for _, o := range d.Offers {
				if !strings.Contains(out, o.Label) {
					t.Errorf("%v %s: offer %q missing", sz, d.Kind, o.Label)
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
		t.Fatal("the dialogue should have been cleared on disconnection")
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
			t.Errorf("tick %d: day %d, %dh (expected %d, %dh)", c.tick, d, h, c.day, c.hour)
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
			t.Errorf("header without %q", want)
		}
	}
}

func TestHeaderSkyIconAndClockExtrapolation(t *testing.T) {
	m := playingModel(t, 110, 34)
	m.snap.Tick = 0 // 8 am: day
	if !strings.Contains(ansi.Strip(m.View().Content), "Jour 1, 08h *") {
		t.Error("day: indicator * expected")
	}
	m.snap.Tick = 16 * ticksPerGameHour // midnight: night
	if !strings.Contains(ansi.Strip(m.View().Content), "Jour 2, 00h (") {
		t.Error("night: indicator ( expected")
	}
	m.snap.Tick, m.snapAt = 0, time.Now().Add(-time.Minute) // 1 real minute = 1 game hour
	if got := m.clockTick(); got < ticksPerGameHour || got > ticksPerGameHour+20 {
		t.Errorf("clockTick = %d, want ~%d", got, ticksPerGameHour)
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
		t.Fatal("inventory not open")
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Sac à dos", "Potion de soin", "Détail"} {
		if !strings.Contains(view, want) {
			t.Errorf("inventory without %q", want)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 110 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	m = press(m, "d") // movement is blocked while the bag is open
	if _, others := sentActions(out); len(others) != 0 {
		t.Fatalf("message emitted from the inventory: %v", others)
	}
	m = press(m, "esc")
	if m.inventory {
		t.Fatal("inventory not closed")
	}
}

func TestBackgroundPaintedUnlessTransparent(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	if v := m.View(); v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Error("the theme background should be painted by default")
	}
	if v := m.WithTransparent(true).View(); v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Error("--transparent ne doit rien peindre")
	}
}

func TestBackgroundPaintedInCells(t *testing.T) {
	m := playingModel(t, 80, 24)
	base := themeSGR()
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != 24 {
		t.Fatalf("%d lines, expected 24", len(lines))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, base) {
			t.Fatalf("line %d without painted background", i)
		}
		// Every internal reset must be followed by the background.
		for _, part := range strings.Split(l, "\x1b[m")[1:] {
			if part != "" && !strings.HasPrefix(part, base) {
				t.Fatalf("line %d: background lost after a reset", i)
			}
		}
	}
	if c := m.WithTransparent(true).View().Content; strings.Contains(c, base) {
		t.Error("--transparent ne doit rien peindre")
	}
}

func TestViewFillsWholeTerminal(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {200, 60}, {300, 90}} {
		m := playingModel(t, sz[0], sz[1])
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%v: %d lines, expected %d", sz, len(lines), sz[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%v line %d: width %d, expected %d", sz, i, w, sz[0])
				break
			}
		}
	}
}

func TestLoginSudoAlignedWithLogo(t *testing.T) {
	m := NewLang("ws://x/ws", "bob", i18n.FR)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	lines := strings.Split(ansi.Strip(tm.(Model).View().Content), "\n")
	col := func(sub string) int {
		for _, l := range lines {
			if i := strings.Index(l, sub); i >= 0 {
				return len([]rune(l[:i]))
			}
		}
		t.Fatalf("%q not found", sub)
		return -1
	}
	if a, b := col("$ sudo"), col("████   ████"); a != b {
		t.Errorf("\"$ sudo\" at column %d, logo at column %d", a, b)
	}
}

func TestViewHasMargins(t *testing.T) {
	m := playingModel(t, 120, 40)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if strings.TrimSpace(lines[0]) != "" || strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Error("top and bottom margin lines expected")
	}
	for i, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, strings.Repeat(" ", marginX)) {
			t.Fatalf("line %d without left margin: %q", i+1, l)
		}
	}
	// Too small for margins: they disappear rather than shrinking the inside.
	if mx, my := playingModel(t, minWidth, minHeight).margins(); mx != 0 || my != 0 {
		t.Errorf("margins %d,%d on a minimal terminal", mx, my)
	}
}

func TestSidePanelGauges(t *testing.T) {
	m := playingModel(t, 120, 36)
	m.snap.Stats = protocol.Stats{Level: 3, XP: 120, XPNext: 400, HP: 30, MaxHP: 40, Mana: 5, MaxMana: 12, Gold: 77, Def: 4, Dmg: "1d6"}
	m.bag.Scores = []int{10, 11, 12, 13, 14, 15}
	lines := strings.Split(ansi.Strip(m.sidePanel()), "\n")
	for _, want := range []string{"XP", "PV", "PM"} {
		n := 0
		for _, l := range lines {
			if strings.Contains(l, want) {
				n++
				if !strings.Contains(l, "█") && !strings.Contains(l, "░") {
					t.Errorf("%s without a gauge on its line: %q", want, l)
				}
			}
		}
		if n != 1 {
			t.Errorf("%s on %d lines", want, n)
		}
	}
	for _, l := range []string{"120/400", "30/40", "5/12"} {
		if !strings.Contains(ansi.Strip(m.sidePanel()), l) {
			t.Errorf("value %s missing", l)
		}
	}
	for _, gone := range []string{"77", "Déf", "FOR"} {
		if strings.Contains(ansi.Strip(m.sidePanel()), gone) {
			t.Errorf("%q should no longer appear", gone)
		}
	}
	m.snap.Stats.MaxMana = 0
	if strings.Contains(ansi.Strip(m.sidePanel()), "PM") {
		t.Error("MP without mana")
	}
}

func TestCtrlArrowsResizeChat(t *testing.T) {
	m := playingModel(t, 120, 36)
	for i := 0; i < 20; i++ {
		m.addChat(chatLine{Channel: protocol.ChanLocal, From: "zoe", Text: fmt.Sprintf("msg%02d", i)})
	}
	count := func(m Model) int {
		n := 0
		for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.Contains(l, "zoe: msg") {
				n++
			}
		}
		return n
	}
	send := func(m Model, code rune) Model {
		tm, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
		return tm.(Model)
	}
	if got := count(m); got != chatLines {
		t.Fatalf("start: %d lines", got)
	}
	m = send(m, tea.KeyUp)
	if got := count(m); got != chatLines+chatStep {
		t.Fatalf("ctrl+up: %d lines", got)
	}
	m = send(m, tea.KeyDown)
	m = send(m, tea.KeyDown) // does not go below the minimum
	if got := count(m); got != chatLines {
		t.Fatalf("ctrl+down: %d lines", got)
	}
	// Ceiling: the map keeps minMapH lines, and nothing overflows, even on a small terminal.
	for _, sz := range [][2]int{{80, 24}, {120, 36}, {60, 20}} {
		s := playingModel(t, sz[0], sz[1])
		for i := 0; i < 30; i++ {
			s = send(s, tea.KeyUp)
		}
		if got := s.chatHeight(); got != max(chatLines, sz[1]-6-minMapH) {
			t.Errorf("%v: ceiling %d", sz, got)
		}
		if lines := strings.Split(ansi.Strip(s.View().Content), "\n"); len(lines) > sz[1] {
			t.Errorf("%v: %d lines > height", sz, len(lines))
		}
	}
}

// packCombat is a combat against a rat, a wolf and a yeti (the strongest).
func packCombat(over bool) protocol.Combat {
	return protocol.Combat{
		Enemies: []protocol.Enemy{
			{Kind: "rat", Name: "Rat", HP: 8, MaxHP: 8, Def: 8, Hit: 1, Dmg: "1d4"},
			{Kind: "wolf", Name: "Loup", HP: 16, MaxHP: 16, Def: 11, Hit: 3, Dmg: "1d6+1"},
			{Kind: "yeti", Name: "Yéti", HP: 34, MaxHP: 34, Def: 12, Hit: 5, Dmg: "2d6"},
		},
		Boss: 2, PlayerHP: 30, PlayerMaxHP: 30, Potions: 2, Over: over,
	}
}

func TestMultiCombatTargetsAndShowsEveryEnemy(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 40))
	tm, _ := m.Update(combatMsg(t, packCombat(false)))
	m = tm.(Model)
	if m.combatTarget != 2 {
		t.Fatalf("the default target is the strongest: %d", m.combatTarget)
	}
	view := plainView(m)
	for _, want := range []string{"Rat", "Loup", "Yéti", "› Yéti", "34/34"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q missing:\n%s", want, view)
		}
	}
	m = press(m, "right") // 2 -> 0 (wraps)
	if m.combatTarget != 0 {
		t.Fatalf("combatTarget=%d, expected 0", m.combatTarget)
	}
	m = press(m, "left") // 0 -> 2
	m = press(m, "left") // 2 -> 1
	if m.combatTarget != 1 {
		t.Fatalf("combatTarget=%d, expected 1", m.combatTarget)
	}
	m = press(m, "a")
	var sent protocol.CombatAction
	for len(out) > 0 {
		env, _ := protocol.Decode(<-out)
		if env.T == protocol.TCombatAction {
			_ = env.Payload(&sent)
		}
	}
	if sent.Action != "attack" || sent.Target != 1 {
		t.Fatalf("action sent %+v, expected attack on 1", sent)
	}
}

func TestMultiCombatSkipsDeadTargets(t *testing.T) {
	m := playingModel(t, 120, 40)
	c := packCombat(false)
	tm, _ := m.Update(combatMsg(t, c))
	m = tm.(Model) // target 2 (yeti)
	c.Enemies[2].HP = 0
	c.Boss = 1 // the strongest living one is now the wolf
	tm, _ = m.Update(combatMsg(t, c))
	m = tm.(Model)
	if m.combatTarget != 1 {
		t.Fatalf("the downed target must switch to the strongest living one: %d", m.combatTarget)
	}
	if m = press(m, "right"); m.combatTarget != 0 {
		t.Fatalf("the dead are skipped: %d", m.combatTarget)
	}
	if m = press(m, "right"); m.combatTarget != 1 {
		t.Fatalf("the dead are skipped: %d", m.combatTarget)
	}
}

func TestMultiCombatFitsEverySize(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		m := playingModel(t, sz[0], sz[1])
		tm, _ := m.Update(combatMsg(t, packCombat(false)))
		lines := strings.Split(plainView(tm.(Model)), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines > height", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v line %d: width %d > %d", sz, i, w, sz[0])
			}
		}
	}
}

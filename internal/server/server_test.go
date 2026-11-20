package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
)

// testPass is the password of all test accounts.
const testPass = storetest.Password

// reg builds a login that creates the account.
func reg(name string) protocol.Login {
	return protocol.Login{Name: name, Password: testPass, Register: true, Lang: "fr"}
}

// signIn builds a login on an existing account.
func signIn(name, pass string) protocol.Login {
	return protocol.Login{Name: name, Password: pass, Lang: "fr"}
}

// startServer starts a server on a temporary database and returns its address,
// its backend and the server (to adjust the quotas).
func startServer(t *testing.T, flushEvery time.Duration) (string, store.Backend, *Server) {
	t.Helper()
	be := storetest.NewBackend(t, flushEvery)
	srv := New(testWorld(1), slog.New(slog.NewTextHandler(io.Discard, nil)), be)
	srv.SetRegistrationLimit(0) // the tests create many accounts
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	ts := httptest.NewServer(srv.Handler())
	// The game loop saves players as it stops: wait for it before
	// the database cleanup (registered earlier, hence run later) closes the writer.
	t.Cleanup(func() { ts.Close(); cancel(); <-done })
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws", be, srv
}

func newTestServer(t *testing.T) string {
	t.Helper()
	url, _, _ := startServer(t, 10*time.Millisecond)
	return url
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func send(t *testing.T, c *websocket.Conn, typ string, payload any) {
	t.Helper()
	data, _ := protocol.Encode(typ, payload)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// recv reads a message. Tests that do not care about character creation leave it
// to this helper: it confirms the first roll as a warrior. See recvRaw to see it go by.
func recv(t *testing.T, c *websocket.Conn) protocol.Envelope {
	t.Helper()
	env := recvRaw(t, c)
	if env.T == protocol.TCreate {
		send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "warrior"})
		return recvRaw(t, c)
	}
	return env
}

// recvRaw reads a message without interpreting anything.
func recvRaw(t *testing.T, c *websocket.Conn) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, err := protocol.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// recvType reads until a message of the wanted type is received (at most 30: the connection already sends half a dozen,
// welcome, quests, dungeons, bag, then snapshots and the chat echo).
func recvType(t *testing.T, c *websocket.Conn, typ string) protocol.Envelope {
	t.Helper()
	for range 30 {
		if env := recv(t, c); env.T == typ {
			return env
		}
	}
	t.Fatalf("message %q not received", typ)
	return protocol.Envelope{}
}

func TestLoginAndChat(t *testing.T) {
	url := newTestServer(t)

	alice := dial(t, url)
	send(t, alice, protocol.TLogin, reg("alice"))
	var w protocol.Welcome
	if err := recvType(t, alice, protocol.TWelcome).Payload(&w); err != nil {
		t.Fatal(err)
	}
	if w.Name != "alice" || w.ID == 0 || w.Seed != 1 {
		t.Fatalf("unexpected welcome: %+v", w)
	}

	bob := dial(t, url)
	send(t, bob, protocol.TLogin, reg("bob"))
	recvType(t, bob, protocol.TWelcome)

	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "salut"})
	for _, c := range []*websocket.Conn{alice, bob} {
		for {
			var ch protocol.Chat
			if err := recvType(t, c, protocol.TChat).Payload(&ch); err != nil {
				t.Fatal(err)
			}
			if ch.From == "alice" {
				if ch.Text != "salut" {
					t.Fatalf("text: %q", ch.Text)
				}
				break
			}
		}
	}
}

func TestMoveAndSnapshots(t *testing.T) {
	url := newTestServer(t)

	alice := dial(t, url)
	send(t, alice, protocol.TLogin, reg("alice"))
	// The welcome must precede any snapshot.
	if env := recv(t, alice); env.T != protocol.TWelcome {
		t.Fatalf("first message: %q, expected welcome", env.T)
	}
	var first protocol.Snapshot
	if err := recvType(t, alice, protocol.TSnapshot).Payload(&first); err != nil {
		t.Fatal(err)
	}

	bob := dial(t, url)
	send(t, bob, protocol.TLogin, reg("bob"))
	recvType(t, bob, protocol.TWelcome)

	// alice sees bob arrive.
	var snap protocol.Snapshot
	for {
		if err := recvType(t, alice, protocol.TSnapshot).Payload(&snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.Others) == 1 && snap.Others[0].Name == "bob" {
			break
		}
	}
	bobStart := snap.Others[0]

	// bob tries the 4 directions; at least one is walkable.
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		send(t, bob, protocol.TMove, protocol.Move{DX: d[0], DY: d[1]})
		time.Sleep(3 * 100 * time.Millisecond) // > cooldown
	}

	// alice eventually sees bob elsewhere.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := recvType(t, alice, protocol.TSnapshot).Payload(&snap); err != nil {
			t.Fatal(err)
		}
		if len(snap.Others) == 1 && (snap.Others[0].X != bobStart.X || snap.Others[0].Y != bobStart.Y) {
			return
		}
	}
	t.Fatal("alice did not see bob move")
}

// login connects a client and consumes its welcome.
func login(t *testing.T, url, name string) *websocket.Conn {
	t.Helper()
	c := dial(t, url)
	send(t, c, protocol.TLogin, reg(name))
	recvType(t, c, protocol.TWelcome)
	return c
}

// chatFrom reads until the first non-system chat.
func chatFrom(t *testing.T, c *websocket.Conn) protocol.Chat {
	t.Helper()
	for range 20 {
		var ch protocol.Chat
		if err := recvType(t, c, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if ch.Channel != protocol.ChanSystem {
			return ch
		}
	}
	t.Fatal("no non-system chat received")
	return protocol.Chat{}
}

// expectNoChat checks that no non-system chat arrives during d.
func expectNoChat(t *testing.T, c *websocket.Conn, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return // time elapsed: nothing received
		}
		env, _ := protocol.Decode(data)
		if env.T != protocol.TChat {
			continue
		}
		var ch protocol.Chat
		_ = env.Payload(&ch)
		if ch.Channel != protocol.ChanSystem {
			t.Fatalf("unexpected chat: %+v", ch)
		}
	}
}

func TestChatChannels(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")
	bob := login(t, url, "bob")
	carol := login(t, url, "carol")

	// Local by default (the three appear side by side).
	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "coucou"})
	for _, c := range []*websocket.Conn{alice, bob, carol} {
		if ch := chatFrom(t, c); ch.Channel != protocol.ChanLocal || ch.From != "alice" || ch.Text != "coucou" {
			t.Fatalf("local: %+v", ch)
		}
	}

	// Global.
	send(t, bob, protocol.TChatSend, protocol.ChatSend{Text: "/g tout le monde"})
	for _, c := range []*websocket.Conn{alice, bob, carol} {
		if ch := chatFrom(t, c); ch.Channel != protocol.ChanGlobal || ch.Text != "tout le monde" {
			t.Fatalf("global: %+v", ch)
		}
	}

	// Emote.
	send(t, carol, protocol.TChatSend, protocol.ChatSend{Text: "/me danse"})
	if ch := chatFrom(t, alice); ch.Channel != protocol.ChanEmote || ch.From != "carol" || ch.Text != "danse" {
		t.Fatalf("emote: %+v", ch)
	}
	chatFrom(t, bob)
	chatFrom(t, carol)

	// Private: recipient and sender receive, the third party does not.
	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "/w BOB psst"})
	for _, c := range []*websocket.Conn{alice, bob} {
		ch := chatFrom(t, c)
		if ch.Channel != protocol.ChanWhisper || ch.From != "alice" || ch.To != "bob" || ch.Text != "psst" {
			t.Fatalf("whisper: %+v", ch)
		}
	}
	expectNoChat(t, carol, 300*time.Millisecond)
}

func TestChatCommandErrors(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")

	for text, want := range map[string]string{
		"/w nobody salut": "introuvable",
		"/w alice moi":    "vous-même",
		"/w alice":        "usage",
		"/g":              "usage",
		"/me":             "usage",
		"/dance":          "inconnue",
	} {
		send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: text})
		var e protocol.Error
		if err := recvType(t, alice, protocol.TError).Payload(&e); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(e.Msg, want) {
			t.Errorf("%q: error %q, expected %q", text, e.Msg, want)
		}
		time.Sleep(250 * time.Millisecond) // do not trigger the rate limit
	}
}

func TestChatHelp(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")
	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "/help"})
	for range 5 {
		var ch protocol.Chat
		if err := recvType(t, alice, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(ch.Text, "/w pseudo") {
			return
		}
	}
	t.Fatal("help not received")
}

func TestChatRateLimit(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")
	for i := 0; i < 12; i++ {
		send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "spam"})
	}
	var e protocol.Error
	if err := recvType(t, alice, protocol.TError).Payload(&e); err != nil {
		t.Fatalf("no rate limit: %v", err)
	}
	if !strings.Contains(e.Msg, "trop de messages") {
		t.Fatalf("error: %q", e.Msg)
	}
}

func TestCombatActionValidation(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")

	// Unknown action: explicit error.
	send(t, alice, protocol.TCombatAction, protocol.CombatAction{Action: "dance"})
	var e protocol.Error
	if err := recvType(t, alice, protocol.TError).Payload(&e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Msg, "dance") {
		t.Fatalf("error: %q", e.Msg)
	}

	// Valid action outside a fight: ignored. Since the messages of a session are
	// ordered, the chat sent afterwards must arrive with no fight or error before it.
	send(t, alice, protocol.TCombatAction, protocol.CombatAction{Action: "attack"})
	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "toujours là"})
	for range 30 {
		env := recv(t, alice)
		switch env.T {
		case protocol.TCombat, protocol.TError:
			t.Fatalf("unexpected response to an action outside a fight: %s", env.T)
		case protocol.TChat:
			var ch protocol.Chat
			if err := env.Payload(&ch); err != nil {
				t.Fatal(err)
			}
			if ch.Channel != protocol.ChanSystem {
				if ch.Text != "toujours là" {
					t.Fatalf("chat: %+v", ch)
				}
				return
			}
		}
	}
	t.Fatal("chat not received")
}

func TestRejectsBadAndDuplicateNames(t *testing.T) {
	url := newTestServer(t)

	bad := dial(t, url)
	send(t, bad, protocol.TLogin, reg("x"))
	if env := recv(t, bad); env.T != protocol.TError {
		t.Fatalf("expected error, got %q", env.T)
	}

	a := dial(t, url)
	send(t, a, protocol.TLogin, reg("Carol"))
	recvType(t, a, protocol.TWelcome)

	dup := dial(t, url)
	send(t, dup, protocol.TLogin, reg("carol"))
	if env := recv(t, dup); env.T != protocol.TError {
		t.Fatalf("duplicate accepted: %q", env.T)
	}
}

func TestFirstMessageMustBeLogin(t *testing.T) {
	url := newTestServer(t)
	c := dial(t, url)
	send(t, c, protocol.TChatSend, protocol.ChatSend{Text: "hi"})
	if env := recv(t, c); env.T != protocol.TError {
		t.Fatalf("expected error, got %q", env.T)
	}
}

func TestHealthAndStatsEndpoints(t *testing.T) {
	be := storetest.NewBackend(t, 10*time.Millisecond)
	srv := New(testWorld(1), slog.New(slog.NewTextHandler(io.Discard, nil)), be)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); cancel(); <-done })

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != "ok" {
		t.Fatalf("/healthz: %d %q", resp.StatusCode, body)
	}

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	login(t, url, "alice")
	login(t, url, "bob")

	var st Stats
	waitUntil(t, "players counted in /stats", func() bool {
		r, err := http.Get(ts.URL + "/stats")
		if err != nil {
			return false
		}
		defer r.Body.Close()
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type %q", ct)
		}
		return json.NewDecoder(r.Body).Decode(&st) == nil && st.Players == 2 && st.Ticks > 0
	})
	if st.TickMaxMS < 0 || st.TickTotalMS < st.TickMaxMS {
		t.Fatalf("inconsistent durations: %+v", st)
	}
}

func TestNPCActionValidation(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")

	send(t, alice, protocol.TNPCAction, protocol.NPCAction{Action: "voler"})
	var e protocol.Error
	if err := recvType(t, alice, protocol.TError).Payload(&e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Msg, "voler") {
		t.Fatalf("error: %q", e.Msg)
	}

	// Valid action outside a dialogue: ignored, no response. Since the message order
	// of a session is guaranteed, the chat sent afterwards must arrive with no
	// dialogue or error before it.
	for _, a := range []string{"buy", "sell", "heal", "leave"} {
		send(t, alice, protocol.TNPCAction, protocol.NPCAction{Action: a})
	}
	send(t, alice, protocol.TChatSend, protocol.ChatSend{Text: "toujours là"})
	for range 30 {
		env := recv(t, alice)
		switch env.T {
		case protocol.TNPC, protocol.TError:
			t.Fatalf("unexpected response to an action outside a dialogue: %s", env.T)
		case protocol.TChat:
			var ch protocol.Chat
			if err := env.Payload(&ch); err != nil {
				t.Fatal(err)
			}
			if ch.Channel != protocol.ChanSystem {
				if ch.Text != "toujours là" {
					t.Fatalf("chat: %+v", ch)
				}
				return
			}
		}
	}
	t.Fatal("chat not received")
}

func TestPingGetsPongAndSnapshotCountsOnline(t *testing.T) {
	url := newTestServer(t)
	alice := login(t, url, "alice")
	_ = login(t, url, "bob")

	send(t, alice, protocol.TPing, nil)
	recvType(t, alice, protocol.TPong)

	for range 50 {
		var snap protocol.Snapshot
		if err := recvType(t, alice, protocol.TSnapshot).Payload(&snap); err != nil {
			t.Fatal(err)
		}
		if snap.Online == 2 {
			return
		}
	}
	t.Fatal("the online players counter never reached 2")
}

// loginLang opens a session with the given language.
func loginLang(t *testing.T, url, name, lang string) *websocket.Conn {
	t.Helper()
	c := dial(t, url)
	lg := reg(name)
	lg.Lang = lang
	send(t, c, protocol.TLogin, lg)
	recvType(t, c, protocol.TWelcome)
	return c
}

func TestServerSpeaksTheClientsLanguage(t *testing.T) {
	url := newTestServer(t)

	// Login errors: French on request, English by default and for an unknown language.
	for lang, want := range map[string]string{"fr": "pseudo invalide", "": "invalid username", "tlh": "invalid username"} {
		c := dial(t, url)
		lg := reg("x")
		lg.Lang = lang
		send(t, c, protocol.TLogin, lg)
		var e protocol.Error
		if err := recvType(t, c, protocol.TError).Payload(&e); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(e.Msg, want) {
			t.Errorf("language %q: %q, expected %q", lang, e.Msg, want)
		}
	}

	// Chat errors and help.
	en := loginLang(t, url, "alice", "en")
	send(t, en, protocol.TChatSend, protocol.ChatSend{Text: "/w nobody hi"})
	var e protocol.Error
	if err := recvType(t, en, protocol.TError).Payload(&e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Msg, "player not found") {
		t.Errorf("whisper error: %q", e.Msg)
	}
	time.Sleep(250 * time.Millisecond)
	send(t, en, protocol.TChatSend, protocol.ChatSend{Text: "/help"})
	for range 5 {
		var ch protocol.Chat
		if err := recvType(t, en, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(ch.Text, "/w username") {
			goto helpOK
		}
	}
	t.Fatal("English help not received")
helpOK:

	// Announcements follow each recipient's language.
	fr := loginLang(t, url, "bernard", "fr")
	for range 10 {
		var ch protocol.Chat
		if err := recvType(t, en, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if ch.Channel == protocol.ChanSystem && strings.Contains(ch.Text, "bernard") {
			if ch.Text != "bernard has joined the world" {
				t.Fatalf("English announcement: %q", ch.Text)
			}
			break
		}
	}
	loginLang(t, url, "carole", "en")
	for range 10 {
		var ch protocol.Chat
		if err := recvType(t, fr, protocol.TChat).Payload(&ch); err != nil {
			t.Fatal(err)
		}
		if ch.Channel == protocol.ChanSystem && strings.Contains(ch.Text, "carole") {
			if ch.Text != "carole a rejoint le monde" {
				t.Fatalf("French announcement: %q", ch.Text)
			}
			return
		}
	}
	t.Fatal("announcement not received")
}

// sheet completes a test character with a neutral sheet (warrior, abilities at 10) so
// that it does not go through creation again; its max HP stay computed from its level.
func sheet(c store.Character) store.Character {
	if c.Scores == [6]int{} {
		c.Class, c.Scores = "warrior", [6]int{10, 10, 10, 10, 10, 10}
	}
	return c
}

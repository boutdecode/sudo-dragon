package server

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

// expectError reads until an error and returns its message.
func expectError(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	var e protocol.Error
	if err := recvType(t, c, protocol.TError).Payload(&e); err != nil {
		t.Fatal(err)
	}
	return e.Msg
}

// register creates the account then disconnects.
func register(t *testing.T, url, name string) {
	t.Helper()
	c := login(t, url, name)
	c.Close(websocket.StatusNormalClosure, "")
}

// waitUntil waits for a condition to become true (a player's departure is asynchronous).
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func TestRegisterThenSignIn(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "Alice")

	// The account exists in the database with the name as typed.
	if name, err := be.Authenticate("alice", testPass); err != nil || name != "Alice" {
		t.Fatalf("account not created: %q, %v", name, err)
	}

	// Normal reconnection; the name's case is ignored, the registered name is used.
	// The name stays taken until the previous departure is processed.
	var w protocol.Welcome
	waitUntil(t, "reconnection possible", func() bool {
		c := dial(t, url)
		send(t, c, protocol.TLogin, signIn("ALICE", testPass))
		env := recv(t, c)
		if env.T != protocol.TWelcome {
			return false
		}
		_ = env.Payload(&w)
		return true
	})
	if w.Name != "Alice" {
		t.Fatalf("welcome name = %q, expected the registered name \"Alice\"", w.Name)
	}
}

func TestSignInFailuresAreIndistinguishable(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")

	wrong := dial(t, url)
	send(t, wrong, protocol.TLogin, signIn("alice", "mauvais-mot-de-passe"))
	unknown := dial(t, url)
	send(t, unknown, protocol.TLogin, signIn("personne", testPass))

	a, b := expectError(t, wrong), expectError(t, unknown)
	if a != b {
		t.Fatalf("different messages: %q vs %q (reveals which accounts exist)", a, b)
	}
	if !strings.Contains(a, "identifiants invalides") {
		t.Fatalf("message: %q", a)
	}
}

func TestSignInWithoutRegisterDoesNotCreateAccount(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	c := dial(t, url)
	send(t, c, protocol.TLogin, signIn("nouveau", testPass))
	expectError(t, c)
	if _, err := be.Authenticate("nouveau", testPass); err == nil {
		t.Fatal("a plain login created an account")
	}
}

func TestRegisterExistingNameRejected(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")

	c := dial(t, url)
	send(t, c, protocol.TLogin, reg("ALICE")) // same name, other case
	if msg := expectError(t, c); !strings.Contains(msg, "existe déjà") {
		t.Fatalf("message: %q", msg)
	}
}

func TestRegisterEnforcesPasswordPolicy(t *testing.T) {
	url := newTestServer(t)
	for _, pw := range []string{"", "court", strings.Repeat("x", 7), strings.Repeat("x", 73)} {
		c := dial(t, url)
		send(t, c, protocol.TLogin, protocol.Login{Name: "alice", Password: pw, Register: true, Lang: "fr"})
		if msg := expectError(t, c); !strings.Contains(msg, "mot de passe") {
			t.Errorf("%d bytes: message %q", len(pw), msg)
		}
	}
}

func TestSecondSessionOnSameAccountRejected(t *testing.T) {
	url := newTestServer(t)
	first := login(t, url, "alice")
	_ = first

	c := dial(t, url)
	send(t, c, protocol.TLogin, signIn("ALICE", testPass))
	if msg := expectError(t, c); !strings.Contains(msg, "déjà connecté") {
		t.Fatalf("message: %q", msg)
	}
}

func TestFailedLoginRateLimit(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")

	for i := 0; i < maxFailedLogins; i++ {
		c := dial(t, url)
		send(t, c, protocol.TLogin, signIn("alice", "mauvais-mot-de-passe"))
		if msg := expectError(t, c); !strings.Contains(msg, "identifiants invalides") {
			t.Fatalf("attempt %d: %q", i, msg)
		}
	}

	// The quota is reached: even the right password is refused for now.
	c := dial(t, url)
	send(t, c, protocol.TLogin, signIn("alice", testPass))
	if msg := expectError(t, c); !strings.Contains(msg, "trop de tentatives") {
		t.Fatalf("rate limit expected, message: %q", msg)
	}
}

func TestRegistrationRateLimit(t *testing.T) {
	url, _, srv := startServer(t, 10*time.Millisecond)
	srv.SetRegistrationLimit(3)

	for i := 0; i < 3; i++ {
		register(t, url, "user"+string(rune('a'+i)))
	}
	c := dial(t, url)
	send(t, c, protocol.TLogin, reg("userz"))
	if msg := expectError(t, c); !strings.Contains(msg, "trop de comptes") {
		t.Fatalf("rate limit expected, message: %q", msg)
	}
}

// --- End-to-end persistence ---

// spawn returns the starting point of the test world (seed 1).
func spawn() world.Pos { return world.New(1).FindSpawn() }

func readSnapshot(t *testing.T, c *websocket.Conn) protocol.Snapshot {
	t.Helper()
	var s protocol.Snapshot
	if err := recvType(t, c, protocol.TSnapshot).Payload(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// waitSavedOnLeave waits for the player's departure to have saved their character:
// before that, a save injected by the test could be overwritten.
func waitSavedOnLeave(t *testing.T, be store.Backend, name string) {
	t.Helper()
	waitUntil(t, "save on departure of "+name, func() bool {
		c, _ := be.LoadCharacter(name)
		return c != nil
	})
}

// signInEventually signs in, retrying as long as the previous departure of the
// same account has not been processed by the server.
func signInEventually(t *testing.T, url, name string) (*websocket.Conn, protocol.Welcome) {
	t.Helper()
	var c *websocket.Conn
	var w protocol.Welcome
	waitUntil(t, "reconnection of "+name, func() bool {
		c = dial(t, url)
		send(t, c, protocol.TLogin, signIn(name, testPass))
		env := recv(t, c)
		if env.T != protocol.TWelcome {
			return false
		}
		_ = env.Payload(&w)
		return true
	})
	return c, w
}

func TestSavedCharacterIsRestoredOnLogin(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")

	sp := spawn()
	be.SaveCharacter("alice", sheet(store.Character{Level: 5, XP: 3, Potions: 4, HP: 20, X: sp.X, Y: sp.Y, Gold: 250}))

	c, w := signInEventually(t, url, "alice")
	got := readSnapshot(t, c).Stats

	want := combat.StatsAt(5, 3, 4)
	if got.Gold != 250 {
		t.Fatalf("restored gold = %d, expected 250", got.Gold)
	}
	if got.Level != 5 || got.XP != 3 || got.Potions != 4 || got.MaxHP != want.MaxHP || got.Def != want.Defense() {
		t.Fatalf("restored sheet: %+v, expected level 5 / %+v", got, want)
	}
	if got.HP < 20 || got.HP > 21 { // +1 HP possible from regeneration
		t.Fatalf("HP=%d, expected 20", got.HP)
	}
	if w.X != sp.X || w.Y != sp.Y {
		t.Fatalf("position (%d,%d), expected %v", w.X, w.Y, sp)
	}
}

func TestLeaveSavesCharacter(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")

	sp := spawn()
	be.SaveCharacter("alice", sheet(store.Character{Level: 3, XP: 7, Potions: 1, HP: 15, X: sp.X, Y: sp.Y, Gold: 88}))

	c, _ := signInEventually(t, url, "alice")
	snap := readSnapshot(t, c)
	c.Close(websocket.StatusNormalClosure, "")

	// The departure rewrites the character as it was (position, level, XP).
	waitUntil(t, "save on departure", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.X == snap.Self.X && ch.Y == snap.Self.Y && ch.Level == 3 && ch.XP == 7 && ch.Potions == 1 && ch.Gold == 88
	})
}

// walkableDir returns a cardinal direction that can be walked from p.
func walkableDir(t *testing.T, p world.Pos) (int, int) {
	t.Helper()
	w := world.New(1)
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		if w.At(p.X+d[0], p.Y+d[1]).Walkable() {
			return d[0], d[1]
		}
	}
	t.Fatalf("no walkable neighbouring tile around %v", p)
	return 0, 0
}

// Reconnecting right after a departure must give the most recent state,
// even if the database write has not happened yet.
func TestQuickReconnectSeesLatestPosition(t *testing.T) {
	url, be, _ := startServer(t, time.Hour) // the writer never drains its queue on its own
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice") // queued, not in the database

	c, w := signInEventually(t, url, "alice")
	start := world.Pos{X: w.X, Y: w.Y}
	dx, dy := walkableDir(t, start)

	// One step, then wait for the snapshot that confirms it.
	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	want := world.Pos{X: start.X + dx, Y: start.Y + dy}
	for range 20 {
		if s := readSnapshot(t, c); s.Self.X == want.X && s.Self.Y == want.Y {
			break
		}
	}
	c.Close(websocket.StatusNormalClosure, "")

	// Precondition: nothing has been written to the database (flush disabled).
	if ch, _ := be.Store.LoadCharacter("alice"); ch != nil {
		t.Fatalf("the database already contains a character: %+v", ch)
	}

	_, w2 := signInEventually(t, url, "alice")
	if w2.X != want.X || w2.Y != want.Y {
		t.Fatalf("reconnected at (%d,%d), expected the last position %v (stale state?)", w2.X, w2.Y, want)
	}
}

// Several simultaneous connections on the same account: exactly one must succeed.
func TestConcurrentLoginsOnSameAccountAdmitExactlyOne(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")
	// Wait for the registration departure to be processed (name released).
	waitUntil(t, "name released", func() bool {
		c := dial(t, url)
		send(t, c, protocol.TLogin, signIn("alice", testPass))
		ok := recv(t, c).T == protocol.TWelcome
		c.Close(websocket.StatusNormalClosure, "")
		return ok
	})
	time.Sleep(200 * time.Millisecond) // lets that last departure finish

	const n = 8
	results := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			c, _, err := websocket.Dial(t.Context(), url, nil)
			if err != nil {
				results <- "dial: " + err.Error()
				return
			}
			defer c.CloseNow()
			data, _ := protocol.Encode(protocol.TLogin, signIn("alice", testPass))
			if err := c.Write(t.Context(), websocket.MessageText, data); err != nil {
				results <- "write: " + err.Error()
				return
			}
			_, reply, err := c.Read(t.Context())
			if err != nil {
				results <- "read: " + err.Error()
				return
			}
			env, _ := protocol.Decode(reply)
			results <- env.T
			time.Sleep(300 * time.Millisecond) // keeps the session open long enough for the others to try
		}()
	}

	welcomes, errs := 0, 0
	for i := 0; i < n; i++ {
		switch r := <-results; r {
		case protocol.TWelcome:
			welcomes++
		case protocol.TError:
			errs++
		default:
			t.Errorf("unexpected result: %s", r)
		}
	}
	if welcomes != 1 || errs != n-1 {
		t.Fatalf("%d admitted and %d refused out of %d simultaneous attempts, expected 1 and %d", welcomes, errs, n, n-1)
	}
}

// Connections of different accounts must not wait for each other.
func TestConcurrentLoginsOfDifferentAccountsAllSucceed(t *testing.T) {
	url := newTestServer(t)
	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			c, _, err := websocket.Dial(t.Context(), url, nil)
			if err != nil {
				errs <- err
				return
			}
			defer c.CloseNow()
			name := "joueur" + string(rune('a'+i))
			data, _ := protocol.Encode(protocol.TLogin, reg(name))
			if err := c.Write(t.Context(), websocket.MessageText, data); err != nil {
				errs <- err
				return
			}
			_, reply, err := c.Read(t.Context())
			if err != nil {
				errs <- err
				return
			}
			env, _ := protocol.Decode(reply)
			if env.T == protocol.TCreate { // new account: confirm the roll
				data, _ := protocol.Encode(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "mage"})
				if err := c.Write(t.Context(), websocket.MessageText, data); err != nil {
					errs <- err
					return
				}
				if _, reply, err = c.Read(t.Context()); err != nil {
					errs <- err
					return
				}
				env, _ = protocol.Decode(reply)
			}
			if env.T != protocol.TWelcome {
				errs <- errors.New("no welcome: " + env.T)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// A name reservation is released even if the connection fails.
func TestReservationIsReleased(t *testing.T) {
	url, _, srv := startServer(t, 10*time.Millisecond)
	c := login(t, url, "alice")
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "reservation and session released", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.reserved) == 0 && len(srv.sessions) == 0
	})
}

// healerSpot returns the healer of the starting village and a free tile next to
// it, with the direction that leads there (the test world has seed 1).
func healerSpot(t *testing.T) (npc world.NPC, stand world.Pos, dx, dy int) {
	t.Helper()
	w := world.New(1)
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCHealer {
				continue
			}
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if w.At(p.X, p.Y).Walkable() {
					if _, isNPC := w.NPCAt(p.X, p.Y); !isNPC {
						return n, p, d[0], d[1]
					}
				}
			}
		}
	}
	t.Skip("no healer near the start")
	return
}

func readNPC(t *testing.T, c *websocket.Conn) protocol.NPCDialog {
	t.Helper()
	var d protocol.NPCDialog
	if err := recvType(t, c, protocol.TNPC).Payload(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

// Full flow: talk to the healer, pay, get healed, and find the
// spent gold again after disconnecting.
func TestHealerEndToEndWithPersistence(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")

	npc, stand, dx, dy := healerSpot(t)
	price := 3 * 4 // level 3: 4 gold per level
	be.SaveCharacter("alice", sheet(store.Character{Level: 3, XP: 0, Potions: 1, HP: 5, X: stand.X, Y: stand.Y, Gold: 100}))

	c, w := signInEventually(t, url, "alice")
	if w.X != stand.X || w.Y != stand.Y {
		t.Fatalf("reconnected at (%d,%d), expected next to the healer (%d,%d)", w.X, w.Y, stand.X, stand.Y)
	}

	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	d := readNPC(t, c)
	if d.Kind != "healer" || d.Over || len(d.Offers) != 1 || d.Offers[0].Action != protocol.NPCHeal {
		t.Fatalf("dialogue opening: %+v (NPC at %v)", d, npc.Pos)
	}
	if d.Offers[0].Price != price || !d.Offers[0].Available || d.Gold != 100 || d.HP > 6 {
		t.Fatalf("unexpected offer or state: %+v", d)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCHeal})
	d = readNPC(t, c)
	if d.HP != d.MaxHP || d.Gold != 100-price {
		t.Fatalf("after healing: HP %d/%d, gold %d (expected %d)", d.HP, d.MaxHP, d.Gold, 100-price)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCLeave})
	if d := readNPC(t, c); !d.Over {
		t.Fatalf("closing: %+v", d)
	}
	c.Close(websocket.StatusNormalClosure, "")

	waitUntil(t, "spent gold saved", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.Gold == 100-price && ch.HP == combat.StatsAt(3, 0, 1).MaxHP
	})
}

func TestMerchantEndToEnd(t *testing.T) {
	w := world.New(1)
	var npc world.NPC
	var stand world.Pos
	var dx, dy int
search:
	for _, s := range w.StructuresNear(w.FindSpawn(), 700) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCMerchant {
				continue
			}
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if _, isNPC := w.NPCAt(p.X, p.Y); w.At(p.X, p.Y).Walkable() && !isNPC {
					npc, stand, dx, dy = n, p, d[0], d[1]
					break search
				}
			}
		}
	}
	if npc.Kind == "" {
		t.Skip("no merchant near the start")
	}

	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")
	be.SaveCharacter("alice", sheet(store.Character{Level: 2, Potions: 1, HP: 20, X: stand.X, Y: stand.Y, Gold: 50}))

	c, _ := signInEventually(t, url, "alice")
	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	if d := readNPC(t, c); d.Kind != "merchant" || len(d.Offers) != 3 {
		t.Fatalf("opening: %+v", d)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCBuy})
	d := readNPC(t, c)
	if d.Potions != 2 || d.Gold != 30 {
		t.Fatalf("after the purchase: %d potions, %d gold (expected 2 and 30)", d.Potions, d.Gold)
	}
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell})
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell})
	readNPC(t, c)
	d = readNPC(t, c)
	if d.Potions != 0 || d.Gold != 30+2*8 {
		t.Fatalf("after two sales: %d potions, %d gold (expected 0 and 46)", d.Potions, d.Gold)
	}
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell}) // nothing left to sell
	if d := readNPC(t, c); d.Potions != 0 || d.Gold != 46 {
		t.Fatalf("sale without a potion: %+v", d)
	}
	c.Close(websocket.StatusNormalClosure, "")

	waitUntil(t, "transactions saved", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.Gold == 46 && ch.Potions == 0
	})
}

// The state converted between the game and the database loses nothing (consent and facts included).
func TestSavedCharacterConversionRoundTrips(t *testing.T) {
	in := store.Character{
		Level: 7, XP: 12, Potions: 3, HP: 40, X: -5, Y: 9, Gold: 88,
		LLMConsent: true, Kills: 40, BossKills: 3, Deaths: 6,
		Class: "rogue", Race: "halfling", Scores: [6]int{9, 15, 12, 10, 8, 13}, MaxHP: 40, Mana: 3,
		Quests: []quest.Quest{{ID: 4, Title: "Les loups", State: quest.Ready, Progress: 3, Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 3}}},
		Rep:    map[world.RegionPos]int{{X: 1, Y: 2}: 5},
	}
	sv := toSaved(in)
	if !sv.LLMConsent || sv.Stats.Kills != 40 || sv.Stats.BossKills != 3 || sv.Stats.Deaths != 6 {
		t.Fatalf("toSaved: %+v", sv)
	}
	if out := fromSaved(*sv); !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip: %+v, expected %+v", out, in)
	}
}

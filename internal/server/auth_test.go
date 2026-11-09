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

// expectError lit jusqu'à une erreur et renvoie son message.
func expectError(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	var e protocol.Error
	if err := recvType(t, c, protocol.TError).Payload(&e); err != nil {
		t.Fatal(err)
	}
	return e.Msg
}

// register crée le compte puis se déconnecte.
func register(t *testing.T, url, name string) {
	t.Helper()
	c := login(t, url, name)
	c.Close(websocket.StatusNormalClosure, "")
}

// waitUntil attend qu'une condition devienne vraie (le départ d'un joueur est asynchrone).
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("délai dépassé: %s", what)
}

func TestRegisterThenSignIn(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "Alice")

	// Le compte existe en base avec le pseudo tel que saisi.
	if name, err := be.Authenticate("alice", testPass); err != nil || name != "Alice" {
		t.Fatalf("compte non créé: %q, %v", name, err)
	}

	// Reconnexion normale; la casse du pseudo est ignorée, le pseudo enregistré est utilisé.
	// Le pseudo reste occupé tant que le départ précédent n'est pas traité.
	var w protocol.Welcome
	waitUntil(t, "reconnexion possible", func() bool {
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
		t.Fatalf("pseudo du welcome = %q, attendu le pseudo enregistré \"Alice\"", w.Name)
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
		t.Fatalf("messages différents: %q vs %q (révèle quels comptes existent)", a, b)
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
		t.Fatal("un simple login a créé un compte")
	}
}

func TestRegisterExistingNameRejected(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")

	c := dial(t, url)
	send(t, c, protocol.TLogin, reg("ALICE")) // même pseudo, autre casse
	if msg := expectError(t, c); !strings.Contains(msg, "existe déjà") {
		t.Fatalf("message: %q", msg)
	}
}

func TestRegisterEnforcesPasswordPolicy(t *testing.T) {
	url := newTestServer(t)
	for _, pw := range []string{"", "court", strings.Repeat("x", 7), strings.Repeat("x", 73)} {
		c := dial(t, url)
		send(t, c, protocol.TLogin, protocol.Login{Name: "alice", Password: pw, Register: true})
		if msg := expectError(t, c); !strings.Contains(msg, "mot de passe") {
			t.Errorf("%d octets: message %q", len(pw), msg)
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
			t.Fatalf("tentative %d: %q", i, msg)
		}
	}

	// Le quota est atteint: même le bon mot de passe est refusé pour l'instant.
	c := dial(t, url)
	send(t, c, protocol.TLogin, signIn("alice", testPass))
	if msg := expectError(t, c); !strings.Contains(msg, "trop de tentatives") {
		t.Fatalf("limitation attendue, message: %q", msg)
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
		t.Fatalf("limitation attendue, message: %q", msg)
	}
}

// --- Persistance de bout en bout ---

// spawn renvoie le point de départ du monde de test (seed 1).
func spawn() world.Pos { return world.New(1).FindSpawn() }

func readSnapshot(t *testing.T, c *websocket.Conn) protocol.Snapshot {
	t.Helper()
	var s protocol.Snapshot
	if err := recvType(t, c, protocol.TSnapshot).Payload(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// waitSavedOnLeave attend que le départ du joueur ait enregistré son personnage:
// avant cela, une sauvegarde injectée par le test pourrait être écrasée.
func waitSavedOnLeave(t *testing.T, be store.Backend, name string) {
	t.Helper()
	waitUntil(t, "sauvegarde au départ de "+name, func() bool {
		c, _ := be.LoadCharacter(name)
		return c != nil
	})
}

// signInEventually se connecte en réessayant tant que le départ précédent du
// même compte n'est pas traité par le serveur.
func signInEventually(t *testing.T, url, name string) (*websocket.Conn, protocol.Welcome) {
	t.Helper()
	var c *websocket.Conn
	var w protocol.Welcome
	waitUntil(t, "reconnexion de "+name, func() bool {
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
		t.Fatalf("or restauré = %d, attendu 250", got.Gold)
	}
	if got.Level != 5 || got.XP != 3 || got.Potions != 4 || got.MaxHP != want.MaxHP || got.Def != want.Defense() {
		t.Fatalf("fiche restaurée: %+v, attendu niveau 5 / %+v", got, want)
	}
	if got.HP < 20 || got.HP > 21 { // +1 PV possible par régénération
		t.Fatalf("PV=%d, attendu 20", got.HP)
	}
	if w.X != sp.X || w.Y != sp.Y {
		t.Fatalf("position (%d,%d), attendu %v", w.X, w.Y, sp)
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

	// Le départ réécrit le personnage tel qu'il était (position, niveau, XP).
	waitUntil(t, "sauvegarde au départ", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.X == snap.Self.X && ch.Y == snap.Self.Y && ch.Level == 3 && ch.XP == 7 && ch.Potions == 1 && ch.Gold == 88
	})
}

// walkableDir renvoie une direction cardinale franchissable depuis p.
func walkableDir(t *testing.T, p world.Pos) (int, int) {
	t.Helper()
	w := world.New(1)
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		if w.At(p.X+d[0], p.Y+d[1]).Walkable() {
			return d[0], d[1]
		}
	}
	t.Fatalf("aucune case voisine praticable autour de %v", p)
	return 0, 0
}

// Se reconnecter aussitôt après un départ doit donner l'état le plus récent,
// même si l'écriture en base n'a pas encore eu lieu.
func TestQuickReconnectSeesLatestPosition(t *testing.T) {
	url, be, _ := startServer(t, time.Hour) // l'écrivain ne vide jamais sa file seul
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice") // en file d'attente, pas en base

	c, w := signInEventually(t, url, "alice")
	start := world.Pos{X: w.X, Y: w.Y}
	dx, dy := walkableDir(t, start)

	// Un pas, puis on attend l'instantané qui le confirme.
	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	want := world.Pos{X: start.X + dx, Y: start.Y + dy}
	for range 20 {
		if s := readSnapshot(t, c); s.Self.X == want.X && s.Self.Y == want.Y {
			break
		}
	}
	c.Close(websocket.StatusNormalClosure, "")

	// Précondition: rien n'a été écrit en base (flush désactivé).
	if ch, _ := be.Store.LoadCharacter("alice"); ch != nil {
		t.Fatalf("la base contient déjà un personnage: %+v", ch)
	}

	_, w2 := signInEventually(t, url, "alice")
	if w2.X != want.X || w2.Y != want.Y {
		t.Fatalf("reconnecté en (%d,%d), attendu la dernière position %v (état périmé ?)", w2.X, w2.Y, want)
	}
}

// Plusieurs connexions simultanées sur le même compte: une seule doit aboutir.
func TestConcurrentLoginsOnSameAccountAdmitExactlyOne(t *testing.T) {
	url := newTestServer(t)
	register(t, url, "alice")
	// Attend que le départ de l'inscription soit traité (pseudo libéré).
	waitUntil(t, "pseudo libéré", func() bool {
		c := dial(t, url)
		send(t, c, protocol.TLogin, signIn("alice", testPass))
		ok := recv(t, c).T == protocol.TWelcome
		c.Close(websocket.StatusNormalClosure, "")
		return ok
	})
	time.Sleep(200 * time.Millisecond) // laisse ce dernier départ se terminer

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
			time.Sleep(300 * time.Millisecond) // garde la session ouverte le temps que les autres tentent
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
			t.Errorf("résultat inattendu: %s", r)
		}
	}
	if welcomes != 1 || errs != n-1 {
		t.Fatalf("%d admis et %d refusés sur %d tentatives simultanées, attendu 1 et %d", welcomes, errs, n, n-1)
	}
}

// Des connexions de comptes différents ne doivent pas s'attendre les unes les autres.
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
			if env.T == protocol.TCreate { // nouveau compte: on valide le tirage
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
				errs <- errors.New("pas de welcome: " + env.T)
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

// La réservation d'un pseudo est libérée même si la connexion échoue.
func TestReservationIsReleased(t *testing.T) {
	url, _, srv := startServer(t, 10*time.Millisecond)
	c := login(t, url, "alice")
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "réservation et session libérées", func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.reserved) == 0 && len(srv.sessions) == 0
	})
}

// healerSpot renvoie le soigneur du village de départ et une case libre à côté de
// lui, avec la direction qui y mène (le monde de test a pour seed 1).
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
	t.Skip("pas de soigneur près du départ")
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

// Parcours complet: parler au soigneur, payer, se faire soigner, et retrouver
// l'or dépensé après la déconnexion.
func TestHealerEndToEndWithPersistence(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")

	npc, stand, dx, dy := healerSpot(t)
	price := 3 * 4 // niveau 3 : 4 or par niveau
	be.SaveCharacter("alice", sheet(store.Character{Level: 3, XP: 0, Potions: 1, HP: 5, X: stand.X, Y: stand.Y, Gold: 100}))

	c, w := signInEventually(t, url, "alice")
	if w.X != stand.X || w.Y != stand.Y {
		t.Fatalf("reconnecté en (%d,%d), attendu à côté du soigneur (%d,%d)", w.X, w.Y, stand.X, stand.Y)
	}

	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	d := readNPC(t, c)
	if d.Kind != "healer" || d.Over || len(d.Offers) != 1 || d.Offers[0].Action != protocol.NPCHeal {
		t.Fatalf("ouverture du dialogue: %+v (PNJ en %v)", d, npc.Pos)
	}
	if d.Offers[0].Price != price || !d.Offers[0].Available || d.Gold != 100 || d.HP > 6 {
		t.Fatalf("offre ou état inattendu: %+v", d)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCHeal})
	d = readNPC(t, c)
	if d.HP != d.MaxHP || d.Gold != 100-price {
		t.Fatalf("après les soins: PV %d/%d, or %d (attendu %d)", d.HP, d.MaxHP, d.Gold, 100-price)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCLeave})
	if d := readNPC(t, c); !d.Over {
		t.Fatalf("fermeture: %+v", d)
	}
	c.Close(websocket.StatusNormalClosure, "")

	waitUntil(t, "or dépensé enregistré", func() bool {
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
		t.Skip("pas de marchand près du départ")
	}

	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")
	be.SaveCharacter("alice", sheet(store.Character{Level: 2, Potions: 1, HP: 20, X: stand.X, Y: stand.Y, Gold: 50}))

	c, _ := signInEventually(t, url, "alice")
	send(t, c, protocol.TMove, protocol.Move{DX: dx, DY: dy})
	if d := readNPC(t, c); d.Kind != "merchant" || len(d.Offers) != 2 {
		t.Fatalf("ouverture: %+v", d)
	}

	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCBuy})
	d := readNPC(t, c)
	if d.Potions != 2 || d.Gold != 30 {
		t.Fatalf("après l'achat: %d potions, %d or (attendu 2 et 30)", d.Potions, d.Gold)
	}
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell})
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell})
	readNPC(t, c)
	d = readNPC(t, c)
	if d.Potions != 0 || d.Gold != 30+2*8 {
		t.Fatalf("après deux ventes: %d potions, %d or (attendu 0 et 46)", d.Potions, d.Gold)
	}
	send(t, c, protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCSell}) // plus rien à vendre
	if d := readNPC(t, c); d.Potions != 0 || d.Gold != 46 {
		t.Fatalf("vente sans potion: %+v", d)
	}
	c.Close(websocket.StatusNormalClosure, "")

	waitUntil(t, "transactions enregistrées", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.Gold == 46 && ch.Potions == 0
	})
}

// L'état converti entre le jeu et la base ne perd rien (accord et faits compris).
func TestSavedCharacterConversionRoundTrips(t *testing.T) {
	in := store.Character{
		Level: 7, XP: 12, Potions: 3, HP: 40, X: -5, Y: 9, Gold: 88,
		LLMConsent: true, Kills: 40, BossKills: 3, Deaths: 6,
		Class: "rogue", Scores: [6]int{9, 15, 12, 10, 8, 13}, MaxHP: 40, Mana: 3,
		Quests: []quest.Quest{{ID: 4, Title: "Les loups", State: quest.Ready, Progress: 3, Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 3}}},
		Rep:    map[world.RegionPos]int{{X: 1, Y: 2}: 5},
	}
	sv := toSaved(in)
	if !sv.LLMConsent || sv.Stats.Kills != 40 || sv.Stats.BossKills != 3 || sv.Stats.Deaths != 6 {
		t.Fatalf("toSaved: %+v", sv)
	}
	if out := fromSaved(*sv); !reflect.DeepEqual(out, in) {
		t.Fatalf("aller-retour: %+v, attendu %+v", out, in)
	}
}

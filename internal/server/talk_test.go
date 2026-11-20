package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// talkEnv is a full server (game, database, conversation service) with a fake model.
type talkEnv struct {
	url  string
	be   store.Backend
	fake *llm.Fake
	svc  *npcai.Service
}

func startTalkServer(t *testing.T) *talkEnv {
	t.Helper()
	be := storetest.NewBackend(t, 10*time.Millisecond)
	srv := New(testWorld(1), slog.New(slog.NewTextHandler(io.Discard, nil)), be)
	srv.SetRegistrationLimit(0)

	fake := &llm.Fake{}
	fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		last := req.Messages[len(req.Messages)-1].Content
		out := map[string]any{"reply": "Je vous écoute.", "intro": nil, "memory": ""}
		switch {
		case strings.HasPrefix(last, "(Le voyageur s'approche"):
			out["reply"] = "Bienvenue, voyageur. Qui êtes-vous ?"
		case strings.Contains(last, "Aldric"):
			out["reply"] = "Enchanté, forgeron."
			out["intro"] = map[string]string{"alias": "Aldric le Rouge", "role": "forgeron", "goal": "retrouver son frère"}
			out["memory"] = "SECRET-D-ALDRIC: il cherche son frère."
		}
		b, _ := json.Marshal(out)
		return llm.Response{Content: string(b), Usage: llm.Usage{Total: 50}}, nil
	}
	book := &lore.Book{Repo: be.Store, Deeds: be.Store}
	cfg := npcai.DefaultConfig()
	cfg.MinInterval = -1
	svc := npcai.New(fake, book, cfg,
		npcai.WithProvider("Mistral AI"),
		npcai.WithWares(be.Store),
		npcai.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	srv.SetTalker(svc)

	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); cancel(); <-done; svc.Wait() })
	return &talkEnv{url: "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws", be: be, fake: fake, svc: svc}
}

// spot is a free tile next to the healer of the starting village.
type spot struct {
	stand  world.Pos
	dx, dy int
}

func healerSpots(t *testing.T) (world.NPC, []spot) {
	t.Helper()
	w := testWorld(1)
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCHealer {
				continue
			}
			var spots []spot
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if _, isNPC := w.NPCAt(p.X, p.Y); w.At(p.X, p.Y).Walkable() && !isNPC {
					spots = append(spots, spot{p, d[0], d[1]})
				}
			}
			if len(spots) >= 2 {
				return n, spots
			}
		}
	}
	t.Skip("no healer with two free access tiles near the start")
	return world.NPC{}, nil
}

// player is a connected client, positioned next to the healer.
type player struct {
	t    *testing.T
	name string
	c    *websocket.Conn
	sp   spot
}

// join creates the account (if it does not exist), puts its character next to the healer and connects the client.
func (e *talkEnv) join(t *testing.T, name string, sp spot, consent bool) *player {
	t.Helper()
	if _, err := e.be.Store.CreateAccount(name, storetest.Password); err != nil {
		// account already created: reconnect
		_ = err
	}
	e.be.SaveCharacter(name, sheet(store.Character{Level: 5, HP: 30, X: sp.stand.X, Y: sp.stand.Y, LLMConsent: consent}))
	var c *websocket.Conn
	waitUntil(t, "connection of "+name, func() bool {
		c = dial(t, e.url)
		send(t, c, protocol.TLogin, signIn(name, storetest.Password))
		return recv(t, c).T == protocol.TWelcome
	})
	return &player{t: t, name: name, c: c, sp: sp}
}

func (p *player) act(action, text string) {
	p.t.Helper()
	send(p.t, p.c, protocol.TNPCAction, protocol.NPCAction{Action: action, Text: text})
}

// until reads dialogues until one satisfies pred.
func (p *player) until(what string, pred func(protocol.NPCDialog) bool) protocol.NPCDialog {
	p.t.Helper()
	for range 60 {
		env := recv(p.t, p.c)
		if env.T != protocol.TNPC {
			continue
		}
		var d protocol.NPCDialog
		if err := env.Payload(&d); err != nil {
			p.t.Fatal(err)
		}
		if pred(d) {
			return d
		}
	}
	p.t.Fatalf("%s: expected dialogue not received", p.name+" "+what)
	return protocol.NPCDialog{}
}

func (p *player) approach() protocol.NPCDialog {
	p.t.Helper()
	send(p.t, p.c, protocol.TMove, protocol.Move{DX: p.sp.dx, DY: p.sp.dy})
	return p.until("ouverture du dialogue", func(d protocol.NPCDialog) bool { return d.Mode == protocol.NPCModeMenu && !d.Over })
}

func hasOffer(d protocol.NPCDialog, action string) bool {
	for _, o := range d.Offers {
		if o.Action == action {
			return true
		}
	}
	return false
}

func lastLine(d protocol.NPCDialog) protocol.NPCLine { return d.Lines[len(d.Lines)-1] }

// startChat takes a player from the menu to the NPC's opening line.
func (p *player) startChat(needConsent bool) protocol.NPCDialog {
	p.t.Helper()
	p.act(protocol.NPCTalk, "")
	if needConsent {
		n := p.until("avis", func(d protocol.NPCDialog) bool { return d.Mode == protocol.NPCModeNotice })
		if n.Notice == "" || !hasOffer(n, protocol.NPCConsent) {
			p.t.Fatalf("incomplete notice: %+v", n)
		}
		p.act(protocol.NPCConsent, "")
	}
	return p.until("ouverture par le PNJ", func(d protocol.NPCDialog) bool {
		return d.Mode == protocol.NPCModeChat && !d.Thinking && len(d.Lines) > 0
	})
}

func TestNPCConversationEndToEndWithRumorsAndPrivacy(t *testing.T) {
	e := startTalkServer(t)
	npc, spots := healerSpots(t)

	// --- Aldric: notice, consent, opening, introduction ---
	aldric := e.join(t, "aldric", spots[0], false)
	menu := aldric.approach()
	if menu.Name != npc.Name || !hasOffer(menu, protocol.NPCTalk) || !hasOffer(menu, protocol.NPCHeal) {
		t.Fatalf("menu: %+v", menu)
	}
	opening := aldric.startChat(true)
	if l := lastLine(opening); l.Who != "npc" || l.Text != "Bienvenue, voyageur. Qui êtes-vous ?" {
		t.Fatalf("the NPC must open the discussion by asking who the traveler is: %+v", opening.Lines)
	}

	aldric.act(protocol.NPCSay, "Je suis Aldric le Rouge, forgeron, je cherche mon frère.")
	reply := aldric.until("reply", func(d protocol.NPCDialog) bool {
		return d.Mode == protocol.NPCModeChat && !d.Thinking && len(d.Lines) >= 4
	})
	if l := reply.Lines[len(reply.Lines)-2]; l.Who != "npc" || l.Text != "Enchanté, forgeron." {
		t.Fatalf("reply: %+v", reply.Lines)
	}
	if l := lastLine(reply); l.Who != "sys" || !strings.Contains(l.Text, "retient") {
		t.Fatalf("the player must be told that their introduction is retained: %+v", reply.Lines)
	}
	card, ok, _ := e.be.Store.GetCard(context.Background(), "aldric", npc.Region)
	if !ok || card.Alias != "Aldric le Rouge" || card.Role != "forgeron" || card.Goal != "retrouver son frère" || card.Level != 5 {
		t.Fatalf("saved card: %+v (found=%v)", card, ok)
	}
	aldric.c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "consent saved", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && ch.LLMConsent
	})

	// --- Brenna, another player, same NPC: she learns the rumor, but not the private memory ---
	brenna := e.join(t, "brenna", spots[1], true)
	brenna.approach()
	brenna.startChat(false) // already accepted: no notice
	calls := e.fake.ChatCalls()
	opening2 := calls[len(calls)-1]
	sys := opening2.Messages[0].Content
	if !strings.Contains(sys, "Aldric le Rouge") || !strings.Contains(sys, "forgeron") || !strings.Contains(sys, "vu ici") {
		t.Fatalf("Brenna should have heard about Aldric (\"seen here\" rumor):\n%s", sys)
	}
	if strings.Contains(sys, "SECRET-D-ALDRIC") {
		t.Fatal("Aldric's private memory leaked to Brenna")
	}
	if !strings.Contains(sys, "ne s'est pas encore présenté") {
		t.Error("Brenna has not spoken yet: the NPC must ask her to introduce herself")
	}

	// --- Aldric comes back: consent kept, his private memory is found again ---
	again := e.join(t, "aldric", spots[0], true)
	again.approach()
	again.startChat(false)
	calls = e.fake.ChatCalls()
	last := calls[len(calls)-1].Messages[0].Content
	if !strings.Contains(last, "SECRET-D-ALDRIC") {
		t.Fatal("Aldric does not find his own memory again")
	}
	if !strings.Contains(last, `"sa_presentation":{"nom_ou_surnom":"Aldric le Rouge"`) {
		t.Fatal("the NPC does not recognize the introduction already made")
	}
	if strings.Contains(last, "ne s'est pas encore présenté") {
		t.Error("a traveler who has already introduced themselves must no longer be invited to do so")
	}
}

func TestSayTextIsSanitizedBeforeReachingTheModel(t *testing.T) {
	e := startTalkServer(t)
	_, spots := healerSpots(t)
	p := e.join(t, "mallory", spots[0], true)
	p.approach()
	p.startChat(false)

	p.act(protocol.NPCSay, "\x1b[2J\x1b]0;PIRATÉ\x07Bonjour‮ "+strings.Repeat("x", 500))
	p.until("reply", func(d protocol.NPCDialog) bool {
		return d.Mode == protocol.NPCModeChat && !d.Thinking && len(d.Lines) >= 3
	})

	calls := e.fake.ChatCalls()
	msg := calls[len(calls)-1].Messages
	got := msg[len(msg)-1].Content
	if strings.ContainsAny(got, "\x1b\x07") || strings.ContainsRune(got, '‮') {
		t.Fatalf("dangerous characters passed to the model: %q", got)
	}
	if n := len([]rune(got)); n > protocol.MaxSayLen {
		t.Fatalf("%d characters passed, cap %d", n, protocol.MaxSayLen)
	}
}

func TestSayFloodIsRateLimited(t *testing.T) {
	e := startTalkServer(t)
	_, spots := healerSpots(t)
	p := e.join(t, "spammer", spots[0], true)
	p.approach()
	p.startChat(false)

	for i := 0; i < 12; i++ {
		p.act(protocol.NPCSay, "message")
	}
	var e2 protocol.Error
	if err := recvType(t, p.c, protocol.TError).Payload(&e2); err != nil {
		t.Fatalf("no limit: %v", err)
	}
	if !strings.Contains(e2.Msg, "doucement") {
		t.Fatalf("message: %q", e2.Msg)
	}
	if n := len(e.fake.ChatCalls()); n > 6 {
		t.Fatalf("%d model calls for 12 messages sent at once", n)
	}
}

func TestConversationFailsGracefullyWhenTheModelIsDown(t *testing.T) {
	e := startTalkServer(t)
	_, spots := healerSpots(t)
	e.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnavailable }
	p := e.join(t, "aldric", spots[0], true)
	p.approach()
	p.act(protocol.NPCTalk, "")
	d := p.until("repli", func(d protocol.NPCDialog) bool {
		return d.Mode == protocol.NPCModeChat && !d.Thinking && len(d.Lines) > 0
	})
	l := lastLine(d)
	if l.Who != "npc" || !strings.Contains(l.Text, "perd le fil") {
		t.Fatalf("in-character fallback expected: %+v", l)
	}
	if strings.Contains(strings.ToLower(l.Text), "erreur") || strings.Contains(l.Text, "llm") {
		t.Fatalf("technical message shown to the player: %q", l.Text)
	}
}

func TestNoTalkOptionWithoutAService(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond) // no Talker plugged in
	_, spots := healerSpots(t)
	if _, err := be.Store.CreateAccount("aldric", storetest.Password); err != nil {
		t.Fatal(err)
	}
	be.SaveCharacter("aldric", sheet(store.Character{Level: 5, HP: 30, X: spots[0].stand.X, Y: spots[0].stand.Y}))
	var c *websocket.Conn
	waitUntil(t, "connection", func() bool {
		c = dial(t, url)
		send(t, c, protocol.TLogin, signIn("aldric", storetest.Password))
		return recv(t, c).T == protocol.TWelcome
	})
	p := &player{t: t, name: "aldric", c: c, sp: spots[0]}
	menu := p.approach()
	if hasOffer(menu, protocol.NPCTalk) {
		t.Fatal("\"Talk\" offered although no language service is plugged in")
	}
	// And a forged request is ignored cleanly.
	p.act(protocol.NPCTalk, "")
	p.act(protocol.NPCSay, "bonjour")
	p.act(protocol.NPCLeave, "")
	if d := p.until("fermeture", func(d protocol.NPCDialog) bool { return d.Over }); !d.Over {
		t.Fatal("the dialogue does not close")
	}
}

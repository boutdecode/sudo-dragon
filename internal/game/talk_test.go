package game

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// fakeTalker is a Talker whose test decides when each reply happens.
type fakeTalker struct {
	mu       sync.Mutex
	avail    bool
	provider string
	reqs     []npcai.Request
	dones    []func(npcai.Reply)
}

func newFakeTalker() *fakeTalker { return &fakeTalker{avail: true, provider: "Mistral AI"} }

func (f *fakeTalker) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.avail
}

func (f *fakeTalker) Provider() string { return f.provider }

func (f *fakeTalker) setAvailable(v bool) { f.mu.Lock(); f.avail = v; f.mu.Unlock() }

func (f *fakeTalker) Ask(req npcai.Request, done func(npcai.Reply)) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.dones = append(f.dones, done)
	f.mu.Unlock()
}

func (f *fakeTalker) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.reqs) }

func (f *fakeTalker) req(i int) npcai.Request { f.mu.Lock(); defer f.mu.Unlock(); return f.reqs[i] }

// answer delivers the reply to request i (as the service would, from another goroutine).
func (f *fakeTalker) answer(i int, rep npcai.Reply) {
	f.mu.Lock()
	done := f.dones[i]
	f.mu.Unlock()
	done(rep)
}

// pump runs the commands that the replies put in the game queue.
func (r *npcRig) pump() {
	for {
		select {
		case fn := <-r.g.inbox:
			fn()
		default:
			return
		}
	}
}

func (r *npcRig) say(text string) { r.g.talkAct(r.p, protocol.NPCSay, text) }

func (r *npcRig) lines() []protocol.NPCLine { return r.last().Lines }

// chatRig sets up a player in discussion with a healer, ready to speak.
func chatRig(t *testing.T, consented bool) (*npcRig, *fakeTalker) {
	t.Helper()
	r := newNPCRig(t, world.NPCHealer)
	ft := newFakeTalker()
	r.g.Talker = ft
	r.p.consent = consented
	r.talk()
	return r, ft
}

func ok(text string) npcai.Reply { return npcai.Reply{Text: text} }

func TestTalkOfferAppearsOnlyWhenAModelIsAvailable(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	if _, has := r.offer(protocol.NPCTalk); has {
		t.Fatal("Chat offered without a language model")
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("the healer's trade disappeared")
	}

	ft := newFakeTalker()
	ft.setAvailable(false)
	r2 := newNPCRig(t, world.NPCHealer)
	r2.g.Talker = ft
	r2.talk()
	if _, has := r2.offer(protocol.NPCTalk); has {
		t.Fatal("Chat offered although the model is unavailable")
	}

	r3, _ := chatRig(t, false)
	if o, has := r3.offer(protocol.NPCTalk); !has || !o.Available || o.Label != "Discuter" {
		t.Fatalf("offer: %+v", o)
	}
	if _, has := r3.offer(protocol.NPCHeal); !has {
		t.Fatal("healing is no longer offered next to the discussion")
	}
}

func TestTalkingRequiresConsentFirst(t *testing.T) {
	r, ft := chatRig(t, false)
	r.act(protocol.NPCTalk)

	d := r.last()
	if d.Mode != protocol.NPCModeNotice || d.Over {
		t.Fatalf("mode = %q, expected the privacy notice", d.Mode)
	}
	if !strings.Contains(d.Notice, "Mistral AI") || !strings.Contains(d.Notice, "rumeurs") || !strings.Contains(d.Notice, "n'a d'effet sur le jeu") {
		t.Fatalf("incomplete notice: %q", d.Notice)
	}
	if len(d.Offers) != 2 || d.Offers[0].Action != protocol.NPCConsent || d.Offers[1].Action != protocol.NPCDecline {
		t.Fatalf("notice offers: %+v", d.Offers)
	}
	if ft.count() != 0 {
		t.Fatal("a request went to the model before the player's consent")
	}
	if r.p.consent {
		t.Fatal("consent recorded without the player having given it")
	}
}

func TestDecliningKeepsTheMenuAndConsentOff(t *testing.T) {
	r, ft := chatRig(t, false)
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCDecline)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || r.p.consent || ft.count() != 0 {
		t.Fatalf("mode %q, consent %v, %d requests", d.Mode, r.p.consent, ft.count())
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("the healer's menu must stay usable after a refusal")
	}
	// Offer the notice again next time.
	r.act(protocol.NPCTalk)
	if r.last().Mode != protocol.NPCModeNotice {
		t.Fatal("the notice must be offered again after a refusal")
	}
}

func TestConsentIsSavedImmediatelyAndOpensTheConversation(t *testing.T) {
	r, ft := chatRig(t, false)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCConsent)

	if !r.p.consent {
		t.Fatal("consent not taken into account")
	}
	saved, found := sv.last("hero")
	if !found || !saved.LLMConsent {
		t.Fatalf("the consent was not saved right away: %+v (found=%v)", saved, found)
	}
	d := r.last()
	if d.Mode != protocol.NPCModeChat || !d.Thinking || len(d.Lines) != 0 {
		t.Fatalf("after consent: mode %q, thinking %v, lines %v", d.Mode, d.Thinking, d.Lines)
	}
	if ft.count() != 1 || !ft.req(0).Opening || ft.req(0).Message != "" {
		t.Fatalf("the first request must be the opening by the NPC: %+v", ft.reqs)
	}

	ft.answer(0, ok("Bienvenue, voyageur. Qui êtes-vous ?"))
	r.pump()
	d = r.last()
	if d.Thinking || len(d.Lines) != 1 || d.Lines[0] != (protocol.NPCLine{Who: "npc", Text: "Bienvenue, voyageur. Qui êtes-vous ?"}) {
		t.Fatalf("after the opening reply: %+v", d)
	}
}

func TestConsentedPlayerGoesStraightToTheConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	if r.last().Mode != protocol.NPCModeChat || ft.count() != 1 || !ft.req(0).Opening {
		t.Fatalf("mode %q, %d requests", r.last().Mode, ft.count())
	}
}

func TestConsentOutsideTheNoticeIsIgnored(t *testing.T) {
	r, _ := chatRig(t, false)
	r.act(protocol.NPCConsent) // from the menu: no effect
	if r.p.consent {
		t.Fatal("a consent could be given without having seen the notice")
	}
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCDecline)
	r.act(protocol.NPCConsent) // the notice is closed again
	if r.p.consent {
		t.Fatal("a consent was recorded after a refusal")
	}
}

func TestSayExchangeBuildsTheTranscriptAndHistory(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()

	r.say("Je suis Aldric.")
	d := r.last()
	if !d.Thinking || len(d.Lines) != 2 || d.Lines[1] != (protocol.NPCLine{Who: "you", Text: "Je suis Aldric."}) {
		t.Fatalf("after sending: %+v", d)
	}
	req := ft.req(1)
	if req.Opening || req.Message != "Je suis Aldric." || len(req.History) != 0 {
		t.Fatalf("request: %+v", req)
	}

	ft.answer(1, ok("Enchanté, Aldric."))
	r.pump()
	d = r.last()
	if d.Thinking || len(d.Lines) != 3 || d.Lines[2].Text != "Enchanté, Aldric." {
		t.Fatalf("after the reply: %+v", d)
	}

	r.say("Et vous ?")
	if h := ft.req(2).History; len(h) != 1 || h[0].Player != "Je suis Aldric." || h[0].NPC != "Enchanté, Aldric." {
		t.Fatalf("the previous exchange is not handed to the model: %+v", h)
	}
}

func TestSpeakerFactsAndIdentityAreSentToTheModel(t *testing.T) {
	r, ft := chatRig(t, true)
	r.p.stats = r.p.stats.WithRecord(31, 2, 5)
	r.p.stats.Level = 9
	r.act(protocol.NPCTalk)

	req := ft.req(0)
	if req.Speaker.Account != "hero" || req.Speaker.Level != 9 || req.Speaker.Kills != 31 || req.Speaker.BossKills != 2 || req.Speaker.Deaths != 5 {
		t.Fatalf("speaker: %+v", req.Speaker)
	}
	if req.NPC != r.npc {
		t.Fatalf("PNJ: %+v, expected %+v", req.NPC, r.npc)
	}
	st := r.g.World.StructureOfRegion(r.npc.Region)
	if req.Place != st.Name || req.PlaceKind != st.Kind {
		t.Fatalf("place: %q (%v), expected %q (%v)", req.Place, req.PlaceKind, st.Name, st.Kind)
	}
	d := r.last()
	if d.Name != r.npc.Name || d.Place != st.Name {
		t.Fatalf("identity in the dialogue: %q / %q", d.Name, d.Place)
	}
}

func TestSayIsIgnoredWhenNotAllowed(t *testing.T) {
	r, ft := chatRig(t, true)

	r.say("avant la discussion") // still in menu mode
	if ft.count() != 0 {
		t.Fatal("a line was sent outside a discussion")
	}
	r.act(protocol.NPCTalk) // opening in progress (pending)
	r.say("pendant l'ouverture")
	if ft.count() != 1 {
		t.Fatalf("%d requests: no speaking while the NPC is thinking", ft.count())
	}
	ft.answer(0, ok("Oui ?"))
	r.pump()

	for _, empty := range []string{"", "   ", "\x1b[0m", "​‮"} {
		r.say(empty)
	}
	if ft.count() != 1 {
		t.Fatalf("%d requests: an empty message must send nothing", ft.count())
	}
	r.say("ok")
	r.say("encore")
	if ft.count() != 2 {
		t.Fatalf("%d requests: only one line at a time", ft.count())
	}
}

func TestSayTextIsSanitizedAndBounded(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.say("\x1b[2JBonjour‮ " + strings.Repeat("x", 500))
	got := ft.req(1).Message
	if strings.ContainsAny(got, "\x1b") || strings.ContainsRune(got, '‮') {
		t.Fatalf("dangerous characters passed to the model: %q", got)
	}
	if n := len([]rune(got)); n > protocol.MaxSayLen {
		t.Fatalf("%d characters passed, ceiling %d", n, protocol.MaxSayLen)
	}
	for _, l := range r.lines() {
		if l.Who == "you" && strings.ContainsAny(l.Text, "\x1b‮") {
			t.Fatalf("transcript not cleaned: %q", l.Text)
		}
	}
}

func TestFailuresAreShownButNotRememberedInHistory(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.say("Bonjour")
	ft.answer(1, npcai.Reply{Text: "Sœur Katell perd le fil de ses pensées…", Failure: npcai.Unavailable})
	r.pump()
	d := r.last()
	if d.Thinking || d.Lines[len(d.Lines)-1].Text != "Sœur Katell perd le fil de ses pensées…" {
		t.Fatalf("the failure must be shown in character: %+v", d.Lines)
	}
	r.say("Bonjour ?")
	if h := ft.req(2).History; len(h) != 0 {
		t.Fatalf("a failure entered the model's history: %+v", h)
	}
}

func TestRememberedAddsAVisibleNotice(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.say("Je suis Aldric.")
	ft.answer(1, npcai.Reply{Text: "Enchanté.", Remembered: true})
	r.pump()

	lines := r.lines()
	last := lines[len(lines)-1]
	if last.Who != "sys" || !strings.Contains(last.Text, r.npc.Name) || !strings.Contains(last.Text, "retient") {
		t.Fatalf("the player is not warned that the NPC remembers their introduction: %+v", last)
	}
}

func TestBackKeepsTheTranscriptAndNeverReopens(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.say("Salut")
	ft.answer(1, ok("Bonjour."))
	r.pump()

	r.act(protocol.NPCBack)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || len(d.Lines) != 0 {
		t.Fatalf("back to the menu: mode %q, lines %v", d.Mode, d.Lines)
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("the menu did not come back")
	}

	r.act(protocol.NPCTalk)
	d = r.last()
	if d.Mode != protocol.NPCModeChat || len(d.Lines) != 3 {
		t.Fatalf("the transcript must be kept: mode %q, %d lines", d.Mode, len(d.Lines))
	}
	if ft.count() != 2 {
		t.Fatalf("%d requests: the NPC must not reopen the discussion a second time", ft.count())
	}
}

func TestANewConversationOpensAgain(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.act(protocol.NPCLeave)
	if r.p.talk != nil {
		t.Fatal("conversation still open")
	}
	r.talk() // back with the NPC: new conversation
	r.act(protocol.NPCTalk)
	if ft.count() != 2 || !ft.req(1).Opening || len(ft.req(1).History) != 0 {
		t.Fatalf("%d requests: a new conversation starts from scratch with an opening", ft.count())
	}
}

func TestMenuActionsAreIgnoredDuringTheConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.p.hp, r.p.stats.Gold = 1, 500
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	for _, a := range []string{protocol.NPCHeal, protocol.NPCBuy, protocol.NPCSell} {
		r.act(a)
	}
	if r.p.hp != 1 || r.p.stats.Gold != 500 {
		t.Fatalf("a trade action took place during the discussion: HP %d, gold %d", r.p.hp, r.p.stats.Gold)
	}
}

func TestLateReplyAfterLeavingIsDropped(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // opening pending
	r.act(protocol.NPCLeave)
	n := len(r.msgs)

	ft.answer(0, ok("Trop tard."))
	r.pump()
	if len(r.msgs) != n {
		t.Fatalf("a late reply produced a message: %+v", r.last())
	}
}

func TestLateReplyFromAnOlderConversationNeverReachesTheNewOne(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // conversation 1, opening pending
	oldGen := r.p.talk.gen
	r.act(protocol.NPCLeave)
	r.talk() // conversation 2
	if r.p.talk.gen == oldGen {
		t.Fatal("two conversations share the same number: late replies would get mixed up")
	}
	r.act(protocol.NPCTalk) // opening 2 (request no. 1)
	n := len(r.msgs)

	ft.answer(0, ok("Réponse de la conversation 1.")) // arrives late
	r.pump()
	for _, l := range r.p.talk.lines {
		if strings.Contains(l.Text, "conversation 1") {
			t.Fatalf("a stale reply entered the new conversation: %+v", r.p.talk.lines)
		}
	}
	if len(r.msgs) != n || !r.p.talk.pending {
		t.Fatal("the stale reply modified the state of the new conversation")
	}

	ft.answer(1, ok("Réponse de la conversation 2."))
	r.pump()
	if got := r.lines(); len(got) != 1 || got[0].Text != "Réponse de la conversation 2." {
		t.Fatalf("the right reply did not arrive: %+v", got)
	}
}

func TestLateReplyAfterThePlayerLeftTheGame(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	r.g.leave(r.p.id)
	ft.answer(0, ok("Personne ne m'écoute."))
	r.pump() // must neither panic nor resurrect the player
	if _, present := r.g.players[r.p.id]; present {
		t.Fatal("the player came back")
	}
}

func TestReplyArrivingWhileInTheMenuIsKeptForLater(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // opening pending
	r.act(protocol.NPCBack)
	n := len(r.msgs)

	ft.answer(0, ok("Je vous attendais."))
	r.pump()
	if len(r.msgs) != n {
		t.Fatal("a reply that arrived while the player is in the menu displayed the discussion screen")
	}
	r.act(protocol.NPCTalk)
	if got := r.lines(); len(got) != 1 || got[0].Text != "Je vous attendais." {
		t.Fatalf("the reply was not kept: %+v", got)
	}
}

func TestTalkerGoingDownMidConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	ft.setAvailable(false)
	r.say("Vous êtes là ?")
	d := r.last()
	if d.Thinking || ft.count() != 1 {
		t.Fatalf("thinking %v, %d requests: an unavailable model must not be called", d.Thinking, ft.count())
	}
	if last := d.Lines[len(d.Lines)-1]; last.Who != "npc" || !strings.HasPrefix(last.Text, r.npc.Name) {
		t.Fatalf("fallback line expected in character: %+v", last)
	}
	// And the menu no longer offers to chat.
	r.act(protocol.NPCBack)
	if _, has := r.offer(protocol.NPCTalk); has {
		t.Fatal("Chat offered although the model is down")
	}
}

func TestTalkWhenUnavailableExplainsAndStaysInTheMenu(t *testing.T) {
	r, ft := chatRig(t, true)
	ft.setAvailable(false)
	r.act(protocol.NPCTalk)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || ft.count() != 0 || len(d.Log) == 0 {
		t.Fatalf("mode %q, %d requests, log %v", d.Mode, ft.count(), d.Log)
	}
}

func TestChatIdleLimitIsLongerThanMenuIdleLimit(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.g.tick += fightIdleTicks + 10 // more than the menu delay
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("a free discussion was closed after the menu delay")
	}
	r.g.tick += chatIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk != nil {
		t.Fatal("an abandoned discussion must eventually close")
	}

	// The menu keeps its short delay.
	r2 := newNPCRig(t, world.NPCHealer)
	r2.talk()
	r2.g.tick += fightIdleTicks + 1
	r2.g.expireIdleTalks(r2.g.sortedIDs())
	if r2.p.talk != nil {
		t.Fatal("the menu should expire after 60 s")
	}
}

func TestSayingResetsTheIdleTimer(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	r.g.tick += chatIdleTicks - 5
	r.say("Toujours là")
	r.g.tick += chatIdleTicks - 5
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("an active player must not be kicked out")
	}
}

func TestTranscriptAndHistoryAreBounded(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	for i := 0; i < 60; i++ {
		r.say("message")
		ft.answer(ft.count()-1, ok("réponse"))
		r.pump()
	}
	if n := len(r.lines()); n > maxTranscript {
		t.Fatalf("transcript of %d lines, ceiling %d", n, maxTranscript)
	}
	if n := len(r.p.talk.history); n != maxHistory {
		t.Fatalf("history of %d exchanges, expected %d", n, maxHistory)
	}
	if n := len(ft.req(ft.count() - 1).History); n != maxHistory {
		t.Fatalf("%d exchanges handed to the model", n)
	}
}

func TestOnlyConsentAndTransactionsAreSaved(t *testing.T) {
	r, ft := chatRig(t, true)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	r.say("Bonjour")
	ft.answer(1, ok("Bonjour."))
	r.pump()
	r.act(protocol.NPCBack)
	if len(sv.calls) != 0 {
		t.Fatalf("%d saves for a simple discussion", len(sv.calls))
	}
}

// Full walkthrough through the real game loop (goroutine, command queue).
func TestConversationThroughRunningGame(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	ft := newFakeTalker()
	g.Talker = ft
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	msgs := make(chan protocol.NPCDialog, 32)
	id, err := g.Join("hero", Handlers{
		Snapshot: func(protocol.Snapshot) {},
		NPC:      func(d protocol.NPCDialog) { msgs <- d },
	})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan [2]int, 1)
	g.do(func() {
		p := g.players[id]
		p.consent = true
		for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
			pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
			if !g.blocked(pos) {
				g.setPos(p, pos)
				ready <- d
				return
			}
		}
		ready <- [2]int{}
	})
	d := <-ready
	if d == ([2]int{}) {
		t.Skip("no free tile next to the NPC")
	}
	g.Move(id, d[0], d[1])

	recv := func() protocol.NPCDialog {
		select {
		case m := <-msgs:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no dialogue message")
			return protocol.NPCDialog{}
		}
	}
	if m := recv(); m.Mode != protocol.NPCModeMenu {
		t.Fatalf("opening: %+v", m)
	}
	g.TalkAct(id, protocol.NPCTalk, "")
	if m := recv(); m.Mode != protocol.NPCModeChat || !m.Thinking {
		t.Fatalf("entering discussion: %+v", m)
	}

	// The service replies from another goroutine, as in real life.
	deadline := time.Now().Add(3 * time.Second)
	for ft.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	go ft.answer(0, ok("Que la lumière vous garde."))
	if m := recv(); m.Thinking || len(m.Lines) != 1 || m.Lines[0].Text != "Que la lumière vous garde." {
		t.Fatalf("opening reply: %+v", m)
	}

	g.TalkAct(id, protocol.NPCSay, "Bonjour")
	if m := recv(); !m.Thinking || m.Lines[len(m.Lines)-1].Who != "you" {
		t.Fatalf("after sending: %+v", m)
	}
	for ft.count() < 2 && time.Now().Before(deadline.Add(2*time.Second)) {
		time.Sleep(5 * time.Millisecond)
	}
	go ft.answer(1, ok("Bonjour à vous."))
	if m := recv(); m.Thinking || m.Lines[len(m.Lines)-1].Text != "Bonjour à vous." {
		t.Fatalf("reply: %+v", m)
	}
}

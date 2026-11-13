package npcai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

var base = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	t      *testing.T
	be     store.Backend
	book   *lore.Book
	fake   *llm.Fake
	svc    *Service
	clk    *clock
	cancel context.CancelFunc
}

// reply builds the JSON response of a well-behaved model.
func reply(text string, intro *lore.Intro, memory string) llm.Response {
	m := map[string]any{"reply": text, "memory": memory, "intro": nil}
	if intro != nil {
		m["intro"] = map[string]string{"alias": intro.Alias, "role": intro.Role, "origin": intro.Origin, "goal": intro.Goal, "trait": intro.Trait}
	}
	b, _ := json.Marshal(m)
	return llm.Response{Content: string(b), Usage: llm.Usage{Total: 100}}
}

func newRig(t *testing.T, cfg Config, accounts ...string) *rig {
	t.Helper()
	be := storetest.NewBackend(t, time.Hour)
	for _, a := range accounts {
		if _, err := be.Store.CreateAccount(a, storetest.Password); err != nil {
			t.Fatal(err)
		}
	}
	r := &rig{t: t, be: be, clk: &clock{t: base}, fake: &llm.Fake{}}
	r.book = &lore.Book{Repo: be.Store, Deeds: be.Store, Now: r.clk.Now}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = -1 // disabled by default in tests; tested separately
	}
	r.svc = New(r.fake, r.book, cfg,
		WithClock(r.clk.Now),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithPlaceNames(func(_ i18n.Lang, rp world.RegionPos) string { return "Lieu" + string(rune('A'+rp.X)) }),
	)
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("Bonjour.", nil, ""), nil }
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.svc.Start(ctx)
	t.Cleanup(func() { cancel(); r.svc.Wait() })
	return r
}

func (r *rig) req(account, msg string) Request {
	rq := testReq(msg)
	rq.Speaker = Speaker{Account: account, Level: 5, Kills: 10}
	return rq
}

// ask sends a request and waits for its response.
func (r *rig) ask(account, msg string) Reply {
	r.t.Helper()
	return r.askReq(r.req(account, msg))
}

func (r *rig) askReq(req Request) Reply {
	r.t.Helper()
	ch := make(chan Reply, 1)
	r.svc.Ask(req, func(rep Reply) { ch <- rep })
	select {
	case rep := <-ch:
		return rep
	case <-time.After(5 * time.Second):
		r.t.Fatal("no response from the service")
		return Reply{}
	}
}

func (r *rig) lastSystem() string {
	r.t.Helper()
	calls := r.fake.ChatCalls()
	if len(calls) == 0 {
		r.t.Fatal("no model call")
	}
	return calls[len(calls)-1].Messages[0].Content
}

func (r *rig) card(account string) (lore.Card, bool) {
	c, ok, err := r.be.Store.GetCard(context.Background(), account, testNPC().Region)
	if err != nil {
		r.t.Fatal(err)
	}
	return c, ok
}

// --- Nominal path ---

func TestAskHappyPathSendsAWellFormedRequest(t *testing.T) {
	r := newRig(t, Config{}, "brenna")
	rep := r.ask("brenna", "Bonjour, soigneuse.")
	if rep.Failure != OK || rep.Text != "Bonjour." || rep.Tokens != 100 {
		t.Fatalf("%+v", rep)
	}
	calls := r.fake.ChatCalls()
	if len(calls) != 1 {
		t.Fatalf("%d calls", len(calls))
	}
	c := calls[0]
	if !c.JSON || c.MaxTokens != DefaultConfig().MaxTokens || c.Temperature == nil || *c.Temperature != DefaultConfig().Temperature {
		t.Fatalf("parameters: %+v", c)
	}
	if c.Guardrail == nil || len(c.Guardrail.Thresholds) != len(DefaultBlockedCategories) {
		t.Fatalf("guardrail: %+v", c.Guardrail)
	}
	if c.Messages[0].Role != llm.System || c.Messages[len(c.Messages)-1].Content != "Bonjour, soigneuse." {
		t.Fatalf("messages: %+v", c.Messages)
	}
	if !strings.Contains(c.Messages[0].Content, "Sœur Katell") {
		t.Fatal("the NPC is not embodied in the prompt")
	}
}

func TestSessionHistoryIsForwarded(t *testing.T) {
	r := newRig(t, Config{}, "brenna")
	rq := r.req("brenna", "Et ensuite ?")
	rq.History = []Turn{{"Salut", "Bienvenue."}}
	r.askReq(rq)
	msgs := r.fake.ChatCalls()[0].Messages
	if len(msgs) != 4 || msgs[1].Content != "Salut" || !strings.Contains(msgs[2].Content, "Bienvenue.") {
		t.Fatalf("history missing: %+v", msgs)
	}
}

func TestIntroIsRememberedAndHeardByAnotherPlayer(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Enchantée, forgeron.", &lore.Intro{Alias: "Aldric le Rouge", Role: "forgeron", Goal: "retrouver son frère"}, "Aldric est forgeron."), nil
	}
	rep := r.ask("aldric", "Je suis Aldric le Rouge, forgeron, je cherche mon frère.")
	if !rep.Remembered {
		t.Fatalf("the introduction was not retained: %+v", rep)
	}
	card, ok := r.card("aldric")
	if !ok || card.Alias != "Aldric le Rouge" || card.Role != "forgeron" || card.Level != 5 {
		t.Fatalf("card: %+v (ok=%v)", card, ok)
	}

	// Another player, with the same NPC: they learn the introduction, labeled "seen here" (vu ici).
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Un forgeron est passé.", nil, ""), nil
	}
	r.ask("brenna", "Du nouveau au village ?")
	sys := r.lastSystem()
	data := dataBlock(t, sys)
	rumors := data["rumeurs"].([]any)
	if len(rumors) != 1 {
		t.Fatalf("rumors: %v", rumors)
	}
	rum := rumors[0].(map[string]any)
	said := rum["dit_de_lui"].(map[string]any)
	if rum["pseudo"] != "aldric" || said["nom_ou_surnom"] != "Aldric le Rouge" || said["but"] != "retrouver son frère" {
		t.Fatalf("rumor: %v", rum)
	}
	if !strings.HasPrefix(rum["provenance"].(string), "vu ici") {
		t.Fatalf("provenance: %v", rum["provenance"])
	}
}

func TestRumorReachesAnotherPlaceOnlyAfterTheDelay(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric", Role: "forgeron"}, ""), nil
	}
	r.ask("aldric", "Je suis Aldric, forgeron.")

	// An NPC from the neighboring place (one region to the east).
	far := r.req("brenna", "Du nouveau ?")
	far.NPC.Region = world.RegionPos{X: 2, Y: 1}
	far.Place = "Kermarec"
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("Hm.", nil, ""), nil }

	r.askReq(far)
	if rumors := dataBlock(t, r.lastSystem())["rumeurs"].([]any); len(rumors) != 0 {
		t.Fatalf("the rumor arrived instantly: %v", rumors)
	}
	r.clk.Advance(lore.HopDelay)
	r.askReq(far)
	rumors := dataBlock(t, r.lastSystem())["rumeurs"].([]any)
	if len(rumors) != 1 {
		t.Fatalf("the rumor should have arrived: %v", rumors)
	}
	prov := rumors[0].(map[string]any)["provenance"].(string)
	if !strings.HasPrefix(prov, "ouï-dire venu de Lieu") {
		t.Fatalf("provenance: %q", prov)
	}
}

func TestMemoryIsPrivateToTheSpeaker(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Je vois.", nil, "SECRET-D-ALDRIC: il cache une dette."), nil
	}
	r.ask("aldric", "Je dois de l'argent à quelqu'un.")

	// Brenna sees none of it, even after the rumor delay.
	r.clk.Advance(time.Hour)
	r.ask("brenna", "Bonjour.")
	if strings.Contains(r.lastSystem(), "SECRET-D-ALDRIC") {
		t.Fatal("Aldric's private memory leaked into Brenna's conversation")
	}
	// Aldric, for his part, recovers his memory.
	r.ask("aldric", "Rebonjour.")
	inter := dataBlock(t, r.lastSystem())["interlocuteur"].(map[string]any)
	if inter["mon_souvenir_de_lui"] != "SECRET-D-ALDRIC: il cache une dette." {
		t.Fatalf("memory: %v", inter["mon_souvenir_de_lui"])
	}
}

func TestVerifiedFactsAreRefreshedOnEveryTurn(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ah.", &lore.Intro{Alias: "Aldric"}, "m"), nil
	}
	r.ask("aldric", "Je suis Aldric.")
	if c, _ := r.card("aldric"); c.BossKills != 0 {
		t.Fatalf("initial card: %+v", c.Facts)
	}

	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("Bravo.", nil, "m"), nil }
	rq := r.req("aldric", "J'ai vaincu le seigneur de guerre.")
	rq.Speaker.BossKills, rq.Speaker.Level = 3, 22
	r.askReq(rq)
	c, _ := r.card("aldric")
	if c.BossKills != 3 || c.Level != 22 {
		t.Fatalf("the verified facts were not updated: %+v", c.Facts)
	}
}

// --- Imperfect model responses ---

func TestPlainTextReplyIsDeliveredWithoutAnyMemoryWrite(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return llm.Response{Content: "Je suis ravi de vous rencontrer, voyageur."}, nil
	}
	rep := r.ask("aldric", "Je suis Aldric le forgeron.")
	if rep.Failure != OK || rep.Text != "Je suis ravi de vous rencontrer, voyageur." || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("a card was written from an unstructured response")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("a memory was written: %q", m)
	}
}

func TestGarbageReplyIsAnInCharacterFailure(t *testing.T) {
	for _, content := range []string{"", "   ", `{"reply": ""}`, `{"reply": "coupé`, "\x1b[2J"} {
		r := newRig(t, Config{}, "aldric")
		r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{Content: content}, nil }
		rep := r.ask("aldric", "Bonjour")
		if rep.Failure != Unavailable || !strings.Contains(rep.Text, "Sœur Katell") {
			t.Errorf("%q: %+v", content, rep)
		}
	}
}

func TestReplyIsSanitizedForTheTerminal(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Salut\x1b[2J\x1b]0;PIRATÉ\x07 ‮à l'envers", nil, ""), nil
	}
	rep := r.ask("aldric", "Bonjour")
	if rep.Text != "Salut à l'envers" || strings.ContainsAny(rep.Text, "\x1b\x07‮") {
		t.Fatalf("reply not cleaned: %q", rep.Text)
	}
}

func TestPanicInTheModelPathDoesNotKillTheService(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	calls := 0
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		calls++
		if calls == 1 {
			panic("unexpected bug")
		}
		return reply("Je suis là.", nil, ""), nil
	}
	if rep := r.ask("aldric", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("after a crash: %+v", rep)
	}
	// The same player can retry (their "in progress" slot was released) and the service answers.
	if rep := r.ask("aldric", "Encore"); rep.Failure != OK || rep.Text != "Je suis là." {
		t.Fatalf("the service stayed blocked: %+v", rep)
	}
}

// --- Card writing: refusals ---

func TestImpersonationAliasIsNotStoredButTheReplyIsDelivered(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Soit, Brenna.", &lore.Intro{Alias: "Brenna", Role: "usurpateur"}, ""), nil
	}
	rep := r.ask("aldric", "Je suis Brenna.")
	if rep.Failure != OK || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("a card impersonating another player was written")
	}
}

func TestModerationRefusalOfACardBlocksTheWrite(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Hmm.", &lore.Intro{Alias: "Aldric", Role: "quelque chose de choquant"}, ""), nil
	}
	r.fake.ModerateFunc = func(_ context.Context, texts []string) ([]llm.Verdict, error) {
		out := make([]llm.Verdict, len(texts))
		out[len(texts)-1].Flagged = []string{"hate_and_discrimination"}
		return out, nil
	}
	rep := r.ask("aldric", "Je suis Aldric.")
	if rep.Remembered || rep.Failure != OK {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("a card flagged by moderation was written")
	}
}

func TestModerationOutageFailsClosedForCards(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric"}, ""), nil
	}
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) { return nil, llm.ErrUnavailable }
	rep := r.ask("aldric", "Je suis Aldric.")
	if rep.Remembered || rep.Failure != OK {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("a card was written without moderation check")
	}
}

func TestModerationOfCategoriesOutsideTheBlockListIsIgnored(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric", Role: "médecin"}, ""), nil
	}
	r.fake.ModerateFunc = func(_ context.Context, texts []string) ([]llm.Verdict, error) {
		out := make([]llm.Verdict, len(texts))
		out[0].Flagged = []string{"health", "financial"} // a healer or a merchant legitimately talk about them
		return out, nil
	}
	if rep := r.ask("aldric", "Je suis Aldric, médecin."); !rep.Remembered {
		t.Fatalf("a legitimate category blocked the card: %+v", rep)
	}
}

func TestCardWriteCooldown(t *testing.T) {
	cfg := Config{CardCooldown: time.Minute}
	r := newRig(t, cfg, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric", Role: "forgeron"}, ""), nil
	}
	if !r.ask("aldric", "Je suis Aldric.").Remembered {
		t.Fatal("first card refused")
	}
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ah ?", &lore.Intro{Role: "roi du monde"}, ""), nil
	}
	if rep := r.ask("aldric", "Non, je suis roi."); rep.Remembered || rep.Failure != OK {
		t.Fatalf("second card too close in time accepted: %+v", rep)
	}
	if c, _ := r.card("aldric"); c.Role != "forgeron" {
		t.Fatalf("card modified despite the delay: %+v", c.Intro)
	}
	r.clk.Advance(time.Minute)
	if !r.ask("aldric", "Finalement, roi.").Remembered {
		t.Fatal("card refused after the delay")
	}
	if c, _ := r.card("aldric"); c.Role != "roi du monde" || c.Alias != "Aldric" {
		t.Fatalf("field merge: %+v", c.Intro)
	}
}

// --- Provider errors ---

func TestProviderErrorsMapToInCharacterFailures(t *testing.T) {
	cases := []struct {
		err  error
		want Failure
	}{
		{llm.ErrBlocked, Blocked}, {llm.ErrRateLimited, Busy}, {llm.ErrUnavailable, Unavailable},
		{llm.ErrBadResponse, Unavailable}, {context.DeadlineExceeded, Unavailable}, {errors.New("inconnue"), Unavailable},
	}
	for _, c := range cases {
		r := newRig(t, Config{}, "aldric")
		r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, c.err }
		rep := r.ask("aldric", "Bonjour")
		if rep.Failure != c.want || !strings.Contains(rep.Text, "Sœur Katell") {
			t.Errorf("%v: %+v, want %v with the NPC's name", c.err, rep, c.want)
		}
		if strings.Contains(rep.Text, c.err.Error()) && c.err.Error() != "" {
			t.Errorf("%v: the technical error text is shown to the player: %q", c.err, rep.Text)
		}
	}
}

func TestRejectedKeyDisablesTheService(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnauthorized }
	r.ask("aldric", "Bonjour")
	if r.svc.Available() {
		t.Fatal("the service stays available after a key refusal")
	}
	calls := len(r.fake.ChatCalls())
	if rep := r.ask("brenna", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.ChatCalls()) != calls {
		t.Fatal("the service keeps calling the API with a rejected key")
	}
}

func TestCircuitBreakerOpensAndCloses(t *testing.T) {
	cfg := Config{BreakerFails: 3, BreakerPause: 30 * time.Second}
	r := newRig(t, cfg, "a", "b", "c", "d")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnavailable }
	for _, a := range []string{"a", "b", "c"} {
		r.ask(a, "Bonjour")
	}
	if r.svc.Available() {
		t.Fatal("the circuit breaker should have opened after 3 consecutive failures")
	}
	calls := len(r.fake.ChatCalls())
	if rep := r.ask("d", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.ChatCalls()) != calls {
		t.Fatal("a call went out while the circuit breaker is open")
	}

	r.clk.Advance(31 * time.Second)
	if !r.svc.Available() {
		t.Fatal("the circuit breaker should have closed after the pause")
	}
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("De retour.", nil, ""), nil }
	if rep := r.ask("d", "Bonjour"); rep.Failure != OK || rep.Text != "De retour." {
		t.Fatalf("%+v", rep)
	}
}

func TestSuccessResetsTheBreakerCounter(t *testing.T) {
	r := newRig(t, Config{BreakerFails: 3}, "a")
	fail := true
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		if fail {
			return llm.Response{}, llm.ErrUnavailable
		}
		return reply("ok", nil, ""), nil
	}
	for i := 0; i < 2; i++ {
		r.ask("a", "x")
	}
	fail = false
	r.ask("a", "x")
	fail = true
	for i := 0; i < 2; i++ {
		r.ask("a", "x")
	}
	if !r.svc.Available() {
		t.Fatal("failures separated by a success must not open the circuit breaker")
	}
}

func TestRateLimitedAndBlockedDoNotTripTheBreaker(t *testing.T) {
	r := newRig(t, Config{BreakerFails: 2}, "a")
	for _, err := range []error{llm.ErrRateLimited, llm.ErrBlocked, llm.ErrRateLimited, llm.ErrBlocked, llm.ErrRateLimited} {
		err := err
		r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, err }
		r.ask("a", "x")
	}
	if !r.svc.Available() {
		t.Fatal("a moderation refusal or a rate limit does not signal an outage")
	}
}

// --- Input moderation ---

func TestGuardrailFallbackWhenTheAPIRejectsTheField(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		if req.Guardrail != nil {
			return llm.Response{}, llm.ErrBadRequest
		}
		return reply("Bonjour.", nil, ""), nil
	}
	rep := r.ask("aldric", "Salut")
	if rep.Failure != OK || rep.Text != "Bonjour." {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.ModerationCalls()) == 0 || r.fake.ModerationCalls()[0][0] != "Salut" {
		t.Fatalf("the message was not explicitly moderated: %v", r.fake.ModerationCalls())
	}

	// Afterwards the service stays in explicit mode, without retrying the rejected field.
	before := len(r.fake.ChatCalls())
	r.ask("brenna", "Coucou")
	calls := r.fake.ChatCalls()
	if len(calls)-before != 1 || calls[len(calls)-1].Guardrail != nil {
		t.Fatalf("the rejected guardrail is sent again on every request: %d calls, %+v", len(calls)-before, calls[len(calls)-1].Guardrail)
	}
	if !r.svc.Stats().Moderation || r.svc.Stats().Guardrails {
		t.Fatalf("state: %+v", r.svc.Stats())
	}
}

func TestExplicitModerationBlocksBeforeCallingTheModel(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.svc.guardrailsOK.Store(false) // explicit mode
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) {
		return []llm.Verdict{{Flagged: []string{"violence_and_threats"}}}, nil
	}
	rep := r.ask("aldric", "message violent")
	if rep.Failure != Blocked || len(r.fake.ChatCalls()) != 0 {
		t.Fatalf("%+v, %d model calls", rep, len(r.fake.ChatCalls()))
	}
	if r.svc.Stats().Blocked != 1 {
		t.Fatalf("block counter: %+v", r.svc.Stats())
	}
}

func TestExplicitModerationOutageAllowsTalkingButNotRemembering(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.svc.guardrailsOK.Store(false)
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) { return nil, llm.ErrUnavailable }
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Enchanté.", &lore.Intro{Alias: "Aldric"}, "souvenir"), nil
	}
	rep := r.ask("aldric", "Je suis Aldric.")
	if rep.Failure != OK || rep.Text != "Enchanté." || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("card written although the message could not be moderated")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("memory written although the message could not be moderated: %q", m)
	}
}

func TestBlockedByGuardrailCountsAndDoesNotChargeTokens(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrBlocked }
	rep := r.ask("aldric", "message refusé")
	st := r.svc.Stats()
	if rep.Failure != Blocked || st.Blocked != 1 || st.TokensToday != 0 {
		t.Fatalf("%+v %+v", rep, st)
	}
}

// --- Probe ---

func TestProbeFullSuccess(t *testing.T) {
	r := newRig(t, Config{})
	res := r.svc.Probe(context.Background())
	if !res.Chat || !res.Guardrails || !res.Moderation || !r.svc.Available() {
		t.Fatalf("%+v", res)
	}
	call := r.fake.ChatCalls()[0]
	if !call.JSON || call.Guardrail == nil || call.MaxTokens > 32 {
		t.Fatalf("the probe must be tiny and exercise the same fields: %+v", call)
	}
}

func TestProbeDetectsRejectedGuardrails(t *testing.T) {
	r := newRig(t, Config{})
	r.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		if req.Guardrail != nil {
			return llm.Response{}, llm.ErrBadRequest
		}
		return reply("ok", nil, ""), nil
	}
	res := r.svc.Probe(context.Background())
	if !res.Chat || res.Guardrails || !res.Moderation || !r.svc.Available() || r.svc.Stats().Guardrails {
		t.Fatalf("%+v", res)
	}
}

func TestProbeDisablesTheServiceWhenChatFails(t *testing.T) {
	for _, err := range []error{llm.ErrUnauthorized, llm.ErrUnavailable, llm.ErrBlocked} {
		r := newRig(t, Config{})
		err := err
		r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, err }
		res := r.svc.Probe(context.Background())
		if res.Chat || !errors.Is(res.ChatErr, err) || r.svc.Available() {
			t.Errorf("%v: %+v, available=%v", err, res, r.svc.Available())
		}
	}
}

func TestProbeWithoutModerationStillAllowsTalkingButNeverRemembering(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) { return nil, llm.ErrBadRequest }
	res := r.svc.Probe(context.Background())
	if !res.Chat || res.Moderation || !r.svc.Available() {
		t.Fatalf("%+v", res)
	}
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Enchanté.", &lore.Intro{Alias: "Aldric"}, "souvenir"), nil
	}
	rep := r.ask("aldric", "Je suis Aldric.")
	if rep.Failure != OK || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("card written although moderation is unavailable")
	}
}

func TestProbeCountsItsTokens(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.Probe(context.Background())
	if r.svc.Stats().TokensToday != 100 {
		t.Fatalf("tokens: %d", r.svc.Stats().TokensToday)
	}
}

// --- Limits ---

func TestMinIntervalPerPlayer(t *testing.T) {
	r := newRig(t, Config{MinInterval: 2 * time.Second}, "aldric", "brenna")
	if rep := r.ask("aldric", "un"); rep.Failure != OK {
		t.Fatalf("%+v", rep)
	}
	calls := len(r.fake.ChatCalls())
	rep := r.ask("aldric", "deux")
	if rep.Failure != Cooldown || len(r.fake.ChatCalls()) != calls {
		t.Fatalf("%+v: a message too close in time must be refused without a model call", rep)
	}
	if rep := r.ask("brenna", "autre joueur"); rep.Failure != OK {
		t.Fatalf("one player's limit must not affect the others: %+v", rep)
	}
	r.clk.Advance(2 * time.Second)
	if rep := r.ask("aldric", "trois"); rep.Failure != OK {
		t.Fatalf("after the delay: %+v", rep)
	}
}

func TestAccountsAreCaseInsensitiveForLimits(t *testing.T) {
	r := newRig(t, Config{MinInterval: time.Minute}, "Aldric")
	r.ask("Aldric", "un")
	if rep := r.ask("ALDRIC", "deux"); rep.Failure != Cooldown {
		t.Fatalf("a different case bypasses the limit: %+v", rep)
	}
}

func TestHourlyLimit(t *testing.T) {
	r := newRig(t, Config{HourlyLimit: 3}, "aldric", "brenna")
	for i := 0; i < 3; i++ {
		if rep := r.ask("aldric", "x"); rep.Failure != OK {
			t.Fatalf("message %d: %+v", i, rep)
		}
	}
	if rep := r.ask("aldric", "x"); rep.Failure != Cooldown {
		t.Fatalf("4th message: %+v", rep)
	}
	if rep := r.ask("brenna", "x"); rep.Failure != OK {
		t.Fatalf("other player: %+v", rep)
	}
	r.clk.Advance(time.Hour + time.Second)
	if rep := r.ask("aldric", "x"); rep.Failure != OK {
		t.Fatalf("after an hour: %+v", rep)
	}
}

func TestDailyTokenBudget(t *testing.T) {
	r := newRig(t, Config{DailyTokens: 500, MaxTokens: 100}, "a", "b", "c", "d", "e", "f", "g")
	// A request is refused if the tokens already consumed plus the maximum of a response
	// would exceed the budget: so it can be used up to the exact cap.
	// Here, each response costs 100 tokens: 5 responses make 500, the 6th is refused.
	ok := 0
	for _, a := range []string{"a", "b", "c", "d", "e", "f"} {
		if r.ask(a, "x").Failure == OK {
			ok++
		}
	}
	if ok != 5 {
		t.Fatalf("%d responses before the budget ran out, want 5", ok)
	}
	rep := r.ask("g", "x")
	if rep.Failure != Budget || !strings.Contains(rep.Text, "Revenez demain") {
		t.Fatalf("%+v", rep)
	}
	st := r.svc.Stats()
	if st.TokensToday != 500 || st.DailyBudget != 500 {
		t.Fatalf("%+v", st)
	}

	// The next day (UTC), the counter restarts from zero.
	r.clk.Advance(24 * time.Hour)
	if rep := r.ask("g", "x"); rep.Failure != OK {
		t.Fatalf("the budget was not reset: %+v", rep)
	}
	if r.svc.Stats().TokensToday != 100 {
		t.Fatalf("new day's tokens: %d", r.svc.Stats().TokensToday)
	}
}

func TestTokensAreEstimatedWhenUsageIsMissing(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		b, _ := json.Marshal(map[string]any{"reply": "ok"})
		return llm.Response{Content: string(b)}, nil // no usage provided
	}
	r.ask("aldric", "x")
	if r.svc.Stats().TokensToday < 100 {
		t.Fatalf("the budget does not count calls without usage: %d", r.svc.Stats().TokensToday)
	}
}

func TestOneRequestInFlightPerPlayer(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		started <- struct{}{}
		<-release
		return reply("enfin", nil, ""), nil
	}
	first := make(chan Reply, 1)
	r.svc.Ask(r.req("aldric", "premier"), func(rep Reply) { first <- rep })
	<-started

	rep := r.ask("aldric", "second")
	if rep.Failure != Busy {
		t.Fatalf("a second request from the same player must be refused: %+v", rep)
	}
	close(release)
	if got := <-first; got.Failure != OK {
		t.Fatalf("%+v", got)
	}
	if rep := r.ask("aldric", "troisième"); rep.Failure != OK {
		t.Fatalf("the slot must be released at the end: %+v", rep)
	}
}

func TestFullQueueAnswersBusy(t *testing.T) {
	r := newRig(t, Config{Workers: 1, QueueSize: 1}, "a", "b", "c")
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		started <- struct{}{}
		<-release
		return reply("ok", nil, ""), nil
	}
	got := make(chan Reply, 3)
	r.svc.Ask(r.req("a", "x"), func(rep Reply) { got <- rep })
	<-started                                                  // a occupies the only worker
	r.svc.Ask(r.req("b", "x"), func(rep Reply) { got <- rep }) // b waits in the queue
	r.svc.Ask(r.req("c", "x"), func(rep Reply) { got <- rep }) // c no longer fits

	select {
	case rep := <-got:
		if rep.Failure != Busy {
			t.Fatalf("the excess request must receive \"busy\": %+v", rep)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no \"busy\" response")
	}
	close(release)
	for i := 0; i < 2; i++ {
		if rep := <-got; rep.Failure != OK {
			t.Fatalf("%+v", rep)
		}
	}
	// The refused player is not blocked for all that.
	if rep := r.ask("c", "x"); rep.Failure != OK {
		t.Fatalf("slot left blocked after a \"busy\" refusal: %+v", rep)
	}
}

func TestWorkersCapConcurrentProviderCalls(t *testing.T) {
	r := newRig(t, Config{Workers: 2, QueueSize: 16}, "a", "b", "c", "d", "e", "f")
	var cur, peak atomic.Int32
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		return reply("ok", nil, ""), nil
	}
	var wg sync.WaitGroup
	for _, a := range []string{"a", "b", "c", "d", "e", "f"} {
		wg.Add(1)
		go func() { defer wg.Done(); r.ask(a, "x") }()
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Fatalf("%d simultaneous calls to the provider, cap 2", peak.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("no parallelism (peak %d): the workers are useless", peak.Load())
	}
}

// done must never be called by the caller's goroutine, even for an immediate
// refusal: the caller (the game loop) holds its own state and could deadlock.
func TestDoneIsNeverCalledInline(t *testing.T) {
	cfg := Config{MinInterval: time.Hour}
	r := newRig(t, cfg, "aldric")
	r.ask("aldric", "premier") // arms the minimum delay

	var held sync.Mutex
	held.Lock() // the "caller" holds a lock during Ask
	got := make(chan Failure, 1)
	r.svc.Ask(r.req("aldric", "trop tôt"), func(rep Reply) {
		held.Lock() // if done ran inline, this would block forever
		held.Unlock()
		got <- rep.Failure
	})
	held.Unlock()
	select {
	case f := <-got:
		if f != Cooldown {
			t.Fatalf("failure = %v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadlock: done was called inline")
	}
}

func TestRejectionsAreInstantAndNeverReachTheProvider(t *testing.T) {
	r := newRig(t, Config{DailyTokens: 50, MaxTokens: 100}, "aldric") // budget lower than one response
	start := time.Now()
	rep := r.ask("aldric", "x")
	if rep.Failure != Budget || len(r.fake.ChatCalls()) != 0 {
		t.Fatalf("%+v, %d calls", rep, len(r.fake.ChatCalls()))
	}
	if time.Since(start) > time.Second {
		t.Fatal("a budget refusal must not wait")
	}
}

// --- Memory degradations ---

type failingRepo struct{ lore.Repo }

func (failingRepo) CardsNear(context.Context, world.RegionPos, int, time.Time, int) ([]lore.Card, error) {
	return nil, errors.New("database unavailable")
}

func (failingRepo) GetMemory(context.Context, world.NPCKey, string) (lore.Memory, bool, error) {
	return lore.Memory{}, false, errors.New("database unavailable")
}

func TestConversationSurvivesAMemoryOutage(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.book.Repo = failingRepo{Repo: r.be.Store}
	rep := r.ask("aldric", "Bonjour")
	if rep.Failure != OK || rep.Text != "Bonjour." {
		t.Fatalf("%+v: an outage of the memory database must not prevent talking", rep)
	}
}

// --- Instrumentation ---

func TestStatsCounters(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric"}, "m"), nil
	}
	r.ask("aldric", "Je suis Aldric.")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnavailable }
	r.ask("brenna", "x")

	st := r.svc.Stats()
	if st.Requests != 2 || st.Failures != 1 || st.Remembered != 1 || st.InFlight != 0 || st.Queued != 0 || !st.Available {
		t.Fatalf("%+v", st)
	}
	if st.TokensToday != 100 {
		t.Fatalf("tokens: %d", st.TokensToday)
	}
}

func TestStartedServiceStopsWithItsContext(t *testing.T) {
	r := newRig(t, Config{Workers: 3}, "aldric")
	r.cancel()
	done := make(chan struct{})
	go func() { r.svc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the workers do not stop when the context is canceled")
	}
}

func TestFailureStrings(t *testing.T) {
	for f, want := range map[Failure]string{OK: "ok", Busy: "busy", Cooldown: "cooldown", Budget: "budget", Blocked: "blocked", Unavailable: "unavailable"} {
		if f.String() != want {
			t.Errorf("%d = %q", f, f.String())
		}
	}
}

func TestConfigDefaultsFillZeros(t *testing.T) {
	c := (Config{}).withDefaults()
	d := DefaultConfig()
	if c != d {
		t.Fatalf("%+v != %+v", c, d)
	}
	c = (Config{Workers: 9, DailyTokens: 5}).withDefaults()
	if c.Workers != 9 || c.DailyTokens != 5 || c.QueueSize != d.QueueSize {
		t.Fatalf("%+v", c)
	}
}

// --- Conversation opening ---

func TestOpeningAsksTheNPCToSpeakFirstAndWritesNothing(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		// Even if the model returns an introduction and a memory, nothing must be written.
		return reply("Bienvenue, voyageur. Qui êtes-vous ?", &lore.Intro{Alias: "Inventé"}, "souvenir inventé"), nil
	}
	rq := r.req("aldric", "")
	rq.Opening = true
	rep := r.askReq(rq)
	if rep.Failure != OK || rep.Text != "Bienvenue, voyageur. Qui êtes-vous ?" || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("a card was written at opening, although the player said nothing")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("a memory was written at opening: %q", m)
	}

	msgs := r.fake.ChatCalls()[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != llm.User || last.Content != i18n.T(i18n.FR, "prompt.opening.stage") {
		t.Fatalf("last message: %+v", last)
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "C'est le début de la conversation") || !strings.Contains(sys, "demande-lui qui il est") {
		t.Fatal("the prompt does not invite the NPC to ask the traveler to introduce themselves")
	}
}

func TestOpeningForAKnownTravellerMentionsHeAlreadyIntroducedHimself(t *testing.T) {
	r := newRig(t, Config{CardCooldown: time.Second}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ravi.", &lore.Intro{Alias: "Aldric", Role: "forgeron"}, "Aldric est forgeron."), nil
	}
	r.ask("aldric", "Je suis Aldric le forgeron.")

	rq := r.req("aldric", "")
	rq.Opening = true
	r.askReq(rq)
	sys := r.lastSystem()
	if strings.Contains(sys, "demande-lui qui il est") || !strings.Contains(sys, "tu le connais déjà") {
		t.Fatal("an already introduced traveler must not be invited to introduce themselves again")
	}
}

func TestInCharacterWording(t *testing.T) {
	for _, f := range []Failure{Busy, Cooldown, Budget, Blocked, Unavailable} {
		got := InCharacter(i18n.FR, "Frère Arnaud", f)
		if !strings.HasPrefix(got, "Frère Arnaud ") {
			t.Errorf("%v: %q", f, got)
		}
	}
	if !strings.HasPrefix(InCharacter(i18n.FR, "", Busy), "Le PNJ ") {
		t.Error("empty name")
	}
}

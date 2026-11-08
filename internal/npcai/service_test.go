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

// reply fabrique la réponse JSON d'un modèle bien élevé.
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
	r.book = &lore.Book{Repo: be.Store, Now: r.clk.Now}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = -1 // désactivé par défaut dans les tests; testé à part
	}
	r.svc = New(r.fake, r.book, cfg,
		WithClock(r.clk.Now),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithPlaceNames(func(rp world.RegionPos) string { return "Lieu" + string(rune('A'+rp.X)) }),
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

// ask envoie une demande et attend sa réponse.
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
		r.t.Fatal("aucune réponse du service")
		return Reply{}
	}
}

func (r *rig) lastSystem() string {
	r.t.Helper()
	calls := r.fake.ChatCalls()
	if len(calls) == 0 {
		r.t.Fatal("aucun appel au modèle")
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

// --- Parcours nominal ---

func TestAskHappyPathSendsAWellFormedRequest(t *testing.T) {
	r := newRig(t, Config{}, "brenna")
	rep := r.ask("brenna", "Bonjour, soigneuse.")
	if rep.Failure != OK || rep.Text != "Bonjour." || rep.Tokens != 100 {
		t.Fatalf("%+v", rep)
	}
	calls := r.fake.ChatCalls()
	if len(calls) != 1 {
		t.Fatalf("%d appels", len(calls))
	}
	c := calls[0]
	if !c.JSON || c.MaxTokens != DefaultConfig().MaxTokens || c.Temperature == nil || *c.Temperature != DefaultConfig().Temperature {
		t.Fatalf("paramètres: %+v", c)
	}
	if c.Guardrail == nil || len(c.Guardrail.Thresholds) != len(DefaultBlockedCategories) {
		t.Fatalf("garde-fou: %+v", c.Guardrail)
	}
	if c.Messages[0].Role != llm.System || c.Messages[len(c.Messages)-1].Content != "Bonjour, soigneuse." {
		t.Fatalf("messages: %+v", c.Messages)
	}
	if !strings.Contains(c.Messages[0].Content, "Sœur Katell") {
		t.Fatal("le PNJ n'est pas incarné dans le prompt")
	}
}

func TestSessionHistoryIsForwarded(t *testing.T) {
	r := newRig(t, Config{}, "brenna")
	rq := r.req("brenna", "Et ensuite ?")
	rq.History = []Turn{{"Salut", "Bienvenue."}}
	r.askReq(rq)
	msgs := r.fake.ChatCalls()[0].Messages
	if len(msgs) != 4 || msgs[1].Content != "Salut" || !strings.Contains(msgs[2].Content, "Bienvenue.") {
		t.Fatalf("historique absent: %+v", msgs)
	}
}

func TestIntroIsRememberedAndHeardByAnotherPlayer(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Enchantée, forgeron.", &lore.Intro{Alias: "Aldric le Rouge", Role: "forgeron", Goal: "retrouver son frère"}, "Aldric est forgeron."), nil
	}
	rep := r.ask("aldric", "Je suis Aldric le Rouge, forgeron, je cherche mon frère.")
	if !rep.Remembered {
		t.Fatalf("la présentation n'a pas été retenue: %+v", rep)
	}
	card, ok := r.card("aldric")
	if !ok || card.Alias != "Aldric le Rouge" || card.Role != "forgeron" || card.Level != 5 {
		t.Fatalf("fiche: %+v (ok=%v)", card, ok)
	}

	// Un autre joueur, au même PNJ: il apprend la présentation, étiquetée « vu ici ».
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Un forgeron est passé.", nil, ""), nil
	}
	r.ask("brenna", "Du nouveau au village ?")
	sys := r.lastSystem()
	data := dataBlock(t, sys)
	rumors := data["rumeurs"].([]any)
	if len(rumors) != 1 {
		t.Fatalf("rumeurs: %v", rumors)
	}
	rum := rumors[0].(map[string]any)
	said := rum["dit_de_lui"].(map[string]any)
	if rum["pseudo"] != "aldric" || said["nom_ou_surnom"] != "Aldric le Rouge" || said["but"] != "retrouver son frère" {
		t.Fatalf("rumeur: %v", rum)
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

	// Un PNJ du lieu voisin (une région à l'est).
	far := r.req("brenna", "Du nouveau ?")
	far.NPC.Region = world.RegionPos{X: 2, Y: 1}
	far.Place = "Kermarec"
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("Hm.", nil, ""), nil }

	r.askReq(far)
	if rumors := dataBlock(t, r.lastSystem())["rumeurs"].([]any); len(rumors) != 0 {
		t.Fatalf("la rumeur est arrivée instantanément: %v", rumors)
	}
	r.clk.Advance(lore.HopDelay)
	r.askReq(far)
	rumors := dataBlock(t, r.lastSystem())["rumeurs"].([]any)
	if len(rumors) != 1 {
		t.Fatalf("la rumeur aurait dû arriver: %v", rumors)
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

	// Brenna n'en voit rien, même après le délai de rumeur.
	r.clk.Advance(time.Hour)
	r.ask("brenna", "Bonjour.")
	if strings.Contains(r.lastSystem(), "SECRET-D-ALDRIC") {
		t.Fatal("le souvenir privé d'Aldric a fuité dans la conversation de Brenna")
	}
	// Aldric, lui, retrouve son souvenir.
	r.ask("aldric", "Rebonjour.")
	inter := dataBlock(t, r.lastSystem())["interlocuteur"].(map[string]any)
	if inter["mon_souvenir_de_lui"] != "SECRET-D-ALDRIC: il cache une dette." {
		t.Fatalf("souvenir: %v", inter["mon_souvenir_de_lui"])
	}
}

func TestVerifiedFactsAreRefreshedOnEveryTurn(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ah.", &lore.Intro{Alias: "Aldric"}, "m"), nil
	}
	r.ask("aldric", "Je suis Aldric.")
	if c, _ := r.card("aldric"); c.BossKills != 0 {
		t.Fatalf("fiche initiale: %+v", c.Facts)
	}

	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return reply("Bravo.", nil, "m"), nil }
	rq := r.req("aldric", "J'ai vaincu le seigneur de guerre.")
	rq.Speaker.BossKills, rq.Speaker.Level = 3, 22
	r.askReq(rq)
	c, _ := r.card("aldric")
	if c.BossKills != 3 || c.Level != 22 {
		t.Fatalf("les faits vérifiés n'ont pas été mis à jour: %+v", c.Facts)
	}
}

// --- Réponses du modèle imparfaites ---

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
		t.Fatal("une fiche a été écrite à partir d'une réponse non structurée")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("un souvenir a été écrit: %q", m)
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
		t.Fatalf("réplique non nettoyée: %q", rep.Text)
	}
}

func TestPanicInTheModelPathDoesNotKillTheService(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	calls := 0
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		calls++
		if calls == 1 {
			panic("bug imprévu")
		}
		return reply("Je suis là.", nil, ""), nil
	}
	if rep := r.ask("aldric", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("après un plantage: %+v", rep)
	}
	// Le même joueur peut réessayer (son créneau « en cours » a été libéré) et le service répond.
	if rep := r.ask("aldric", "Encore"); rep.Failure != OK || rep.Text != "Je suis là." {
		t.Fatalf("le service est resté bloqué: %+v", rep)
	}
}

// --- Écriture de fiche : refus ---

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
		t.Fatal("une fiche usurpant un autre joueur a été écrite")
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
		t.Fatal("une fiche signalée par la modération a été écrite")
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
		t.Fatal("une fiche a été écrite sans contrôle de modération")
	}
}

func TestModerationOfCategoriesOutsideTheBlockListIsIgnored(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric", Role: "médecin"}, ""), nil
	}
	r.fake.ModerateFunc = func(_ context.Context, texts []string) ([]llm.Verdict, error) {
		out := make([]llm.Verdict, len(texts))
		out[0].Flagged = []string{"health", "financial"} // un soigneur ou un marchand en parlent légitimement
		return out, nil
	}
	if rep := r.ask("aldric", "Je suis Aldric, médecin."); !rep.Remembered {
		t.Fatalf("une catégorie légitime a bloqué la fiche: %+v", rep)
	}
}

func TestCardWriteCooldown(t *testing.T) {
	cfg := Config{CardCooldown: time.Minute}
	r := newRig(t, cfg, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Bien.", &lore.Intro{Alias: "Aldric", Role: "forgeron"}, ""), nil
	}
	if !r.ask("aldric", "Je suis Aldric.").Remembered {
		t.Fatal("première fiche refusée")
	}
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ah ?", &lore.Intro{Role: "roi du monde"}, ""), nil
	}
	if rep := r.ask("aldric", "Non, je suis roi."); rep.Remembered || rep.Failure != OK {
		t.Fatalf("seconde fiche trop rapprochée acceptée: %+v", rep)
	}
	if c, _ := r.card("aldric"); c.Role != "forgeron" {
		t.Fatalf("fiche modifiée malgré le délai: %+v", c.Intro)
	}
	r.clk.Advance(time.Minute)
	if !r.ask("aldric", "Finalement, roi.").Remembered {
		t.Fatal("fiche refusée après le délai")
	}
	if c, _ := r.card("aldric"); c.Role != "roi du monde" || c.Alias != "Aldric" {
		t.Fatalf("fusion des champs: %+v", c.Intro)
	}
}

// --- Erreurs du fournisseur ---

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
			t.Errorf("%v: %+v, attendu %v avec le nom du PNJ", c.err, rep, c.want)
		}
		if strings.Contains(rep.Text, c.err.Error()) && c.err.Error() != "" {
			t.Errorf("%v: le texte d'erreur technique est montré au joueur: %q", c.err, rep.Text)
		}
	}
}

func TestRejectedKeyDisablesTheService(t *testing.T) {
	r := newRig(t, Config{}, "aldric", "brenna")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnauthorized }
	r.ask("aldric", "Bonjour")
	if r.svc.Available() {
		t.Fatal("le service reste disponible après un refus de clé")
	}
	calls := len(r.fake.ChatCalls())
	if rep := r.ask("brenna", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.ChatCalls()) != calls {
		t.Fatal("le service continue d'appeler l'API avec une clé refusée")
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
		t.Fatal("le disjoncteur aurait dû s'ouvrir après 3 échecs consécutifs")
	}
	calls := len(r.fake.ChatCalls())
	if rep := r.ask("d", "Bonjour"); rep.Failure != Unavailable {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.ChatCalls()) != calls {
		t.Fatal("un appel est parti alors que le disjoncteur est ouvert")
	}

	r.clk.Advance(31 * time.Second)
	if !r.svc.Available() {
		t.Fatal("le disjoncteur aurait dû se refermer après la pause")
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
		t.Fatal("des échecs séparés par un succès ne doivent pas ouvrir le disjoncteur")
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
		t.Fatal("un refus de modération ou une limite de débit ne signale pas une panne")
	}
}

// --- Modération de l'entrée ---

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
		t.Fatalf("le message n'a pas été modéré explicitement: %v", r.fake.ModerationCalls())
	}

	// Ensuite le service reste en mode explicite, sans réessayer le champ refusé.
	before := len(r.fake.ChatCalls())
	r.ask("brenna", "Coucou")
	calls := r.fake.ChatCalls()
	if len(calls)-before != 1 || calls[len(calls)-1].Guardrail != nil {
		t.Fatalf("le garde-fou refusé est renvoyé à chaque requête: %d appels, %+v", len(calls)-before, calls[len(calls)-1].Guardrail)
	}
	if !r.svc.Stats().Moderation || r.svc.Stats().Guardrails {
		t.Fatalf("état: %+v", r.svc.Stats())
	}
}

func TestExplicitModerationBlocksBeforeCallingTheModel(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.svc.guardrailsOK.Store(false) // mode explicite
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) {
		return []llm.Verdict{{Flagged: []string{"violence_and_threats"}}}, nil
	}
	rep := r.ask("aldric", "message violent")
	if rep.Failure != Blocked || len(r.fake.ChatCalls()) != 0 {
		t.Fatalf("%+v, %d appels au modèle", rep, len(r.fake.ChatCalls()))
	}
	if r.svc.Stats().Blocked != 1 {
		t.Fatalf("compteur de blocages: %+v", r.svc.Stats())
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
		t.Fatal("fiche écrite alors que le message n'a pas pu être modéré")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("souvenir écrit alors que le message n'a pas pu être modéré: %q", m)
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

// --- Sonde ---

func TestProbeFullSuccess(t *testing.T) {
	r := newRig(t, Config{})
	res := r.svc.Probe(context.Background())
	if !res.Chat || !res.Guardrails || !res.Moderation || !r.svc.Available() {
		t.Fatalf("%+v", res)
	}
	call := r.fake.ChatCalls()[0]
	if !call.JSON || call.Guardrail == nil || call.MaxTokens > 32 {
		t.Fatalf("la sonde doit être minuscule et exercer les mêmes champs: %+v", call)
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
			t.Errorf("%v: %+v, disponible=%v", err, res, r.svc.Available())
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
		t.Fatal("fiche écrite alors que la modération est indisponible")
	}
}

func TestProbeCountsItsTokens(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.Probe(context.Background())
	if r.svc.Stats().TokensToday != 100 {
		t.Fatalf("jetons: %d", r.svc.Stats().TokensToday)
	}
}

// --- Limites ---

func TestMinIntervalPerPlayer(t *testing.T) {
	r := newRig(t, Config{MinInterval: 2 * time.Second}, "aldric", "brenna")
	if rep := r.ask("aldric", "un"); rep.Failure != OK {
		t.Fatalf("%+v", rep)
	}
	calls := len(r.fake.ChatCalls())
	rep := r.ask("aldric", "deux")
	if rep.Failure != Cooldown || len(r.fake.ChatCalls()) != calls {
		t.Fatalf("%+v: un message trop rapproché doit être refusé sans appel au modèle", rep)
	}
	if rep := r.ask("brenna", "autre joueur"); rep.Failure != OK {
		t.Fatalf("la limite d'un joueur ne doit pas toucher les autres: %+v", rep)
	}
	r.clk.Advance(2 * time.Second)
	if rep := r.ask("aldric", "trois"); rep.Failure != OK {
		t.Fatalf("après le délai: %+v", rep)
	}
}

func TestAccountsAreCaseInsensitiveForLimits(t *testing.T) {
	r := newRig(t, Config{MinInterval: time.Minute}, "Aldric")
	r.ask("Aldric", "un")
	if rep := r.ask("ALDRIC", "deux"); rep.Failure != Cooldown {
		t.Fatalf("une autre casse contourne la limite: %+v", rep)
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
		t.Fatalf("4e message: %+v", rep)
	}
	if rep := r.ask("brenna", "x"); rep.Failure != OK {
		t.Fatalf("autre joueur: %+v", rep)
	}
	r.clk.Advance(time.Hour + time.Second)
	if rep := r.ask("aldric", "x"); rep.Failure != OK {
		t.Fatalf("après une heure: %+v", rep)
	}
}

func TestDailyTokenBudget(t *testing.T) {
	r := newRig(t, Config{DailyTokens: 500, MaxTokens: 100}, "a", "b", "c", "d", "e", "f", "g")
	// Une demande est refusée si les jetons déjà consommés plus le maximum d'une réponse
	// dépasseraient le budget: on peut donc l'utiliser jusqu'au plafond exact.
	// Ici, chaque réponse coûte 100 jetons: 5 réponses font 500, la 6e est refusée.
	ok := 0
	for _, a := range []string{"a", "b", "c", "d", "e", "f"} {
		if r.ask(a, "x").Failure == OK {
			ok++
		}
	}
	if ok != 5 {
		t.Fatalf("%d réponses avant l'épuisement du budget, attendu 5", ok)
	}
	rep := r.ask("g", "x")
	if rep.Failure != Budget || !strings.Contains(rep.Text, "Revenez demain") {
		t.Fatalf("%+v", rep)
	}
	st := r.svc.Stats()
	if st.TokensToday != 500 || st.DailyBudget != 500 {
		t.Fatalf("%+v", st)
	}

	// Le lendemain (UTC), le compteur repart de zéro.
	r.clk.Advance(24 * time.Hour)
	if rep := r.ask("g", "x"); rep.Failure != OK {
		t.Fatalf("le budget n'a pas été remis à zéro: %+v", rep)
	}
	if r.svc.Stats().TokensToday != 100 {
		t.Fatalf("jetons du nouveau jour: %d", r.svc.Stats().TokensToday)
	}
}

func TestTokensAreEstimatedWhenUsageIsMissing(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		b, _ := json.Marshal(map[string]any{"reply": "ok"})
		return llm.Response{Content: string(b)}, nil // pas de consommation fournie
	}
	r.ask("aldric", "x")
	if r.svc.Stats().TokensToday < 100 {
		t.Fatalf("le budget ne compte pas les appels sans usage: %d", r.svc.Stats().TokensToday)
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
		t.Fatalf("une seconde demande du même joueur doit être refusée: %+v", rep)
	}
	close(release)
	if got := <-first; got.Failure != OK {
		t.Fatalf("%+v", got)
	}
	if rep := r.ask("aldric", "troisième"); rep.Failure != OK {
		t.Fatalf("le créneau doit être libéré à la fin: %+v", rep)
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
	<-started                                                  // a occupe l'unique worker
	r.svc.Ask(r.req("b", "x"), func(rep Reply) { got <- rep }) // b attend dans la file
	r.svc.Ask(r.req("c", "x"), func(rep Reply) { got <- rep }) // c ne tient plus

	select {
	case rep := <-got:
		if rep.Failure != Busy {
			t.Fatalf("la demande en trop doit recevoir « occupé »: %+v", rep)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pas de réponse « occupé »")
	}
	close(release)
	for i := 0; i < 2; i++ {
		if rep := <-got; rep.Failure != OK {
			t.Fatalf("%+v", rep)
		}
	}
	// Le joueur refusé n'est pas bloqué pour autant.
	if rep := r.ask("c", "x"); rep.Failure != OK {
		t.Fatalf("créneau resté bloqué après un refus « occupé »: %+v", rep)
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
		t.Fatalf("%d appels simultanés au fournisseur, plafond 2", peak.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("aucun parallélisme (pic %d): les workers ne servent à rien", peak.Load())
	}
}

// done ne doit jamais être appelé par la goroutine de l'appelant, même pour un refus
// immédiat: l'appelant (la boucle de jeu) tient son propre état et pourrait s'interbloquer.
func TestDoneIsNeverCalledInline(t *testing.T) {
	cfg := Config{MinInterval: time.Hour}
	r := newRig(t, cfg, "aldric")
	r.ask("aldric", "premier") // arme le délai minimal

	var held sync.Mutex
	held.Lock() // l'« appelant » tient un verrou pendant Ask
	got := make(chan Failure, 1)
	r.svc.Ask(r.req("aldric", "trop tôt"), func(rep Reply) {
		held.Lock() // si done s'exécutait en ligne, ceci bloquerait pour toujours
		held.Unlock()
		got <- rep.Failure
	})
	held.Unlock()
	select {
	case f := <-got:
		if f != Cooldown {
			t.Fatalf("échec = %v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interblocage: done a été appelé en ligne")
	}
}

func TestRejectionsAreInstantAndNeverReachTheProvider(t *testing.T) {
	r := newRig(t, Config{DailyTokens: 50, MaxTokens: 100}, "aldric") // budget inférieur à une réponse
	start := time.Now()
	rep := r.ask("aldric", "x")
	if rep.Failure != Budget || len(r.fake.ChatCalls()) != 0 {
		t.Fatalf("%+v, %d appels", rep, len(r.fake.ChatCalls()))
	}
	if time.Since(start) > time.Second {
		t.Fatal("un refus de budget ne doit pas attendre")
	}
}

// --- Dégradations de la mémoire ---

type failingRepo struct{ lore.Repo }

func (failingRepo) CardsNear(context.Context, world.RegionPos, int, time.Time, int) ([]lore.Card, error) {
	return nil, errors.New("base indisponible")
}

func (failingRepo) GetMemory(context.Context, world.NPCKey, string) (lore.Memory, bool, error) {
	return lore.Memory{}, false, errors.New("base indisponible")
}

func TestConversationSurvivesAMemoryOutage(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.book.Repo = failingRepo{Repo: r.be.Store}
	rep := r.ask("aldric", "Bonjour")
	if rep.Failure != OK || rep.Text != "Bonjour." {
		t.Fatalf("%+v: une panne de la base de mémoire ne doit pas empêcher de parler", rep)
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
		t.Fatalf("jetons: %d", st.TokensToday)
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
		t.Fatal("les workers ne s'arrêtent pas à l'annulation du contexte")
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

// --- Ouverture de conversation ---

func TestOpeningAsksTheNPCToSpeakFirstAndWritesNothing(t *testing.T) {
	r := newRig(t, Config{}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		// Même si le modèle rend une présentation et un souvenir, rien ne doit être écrit.
		return reply("Bienvenue, voyageur. Qui êtes-vous ?", &lore.Intro{Alias: "Inventé"}, "souvenir inventé"), nil
	}
	rq := r.req("aldric", "")
	rq.Opening = true
	rep := r.askReq(rq)
	if rep.Failure != OK || rep.Text != "Bienvenue, voyageur. Qui êtes-vous ?" || rep.Remembered {
		t.Fatalf("%+v", rep)
	}
	if _, ok := r.card("aldric"); ok {
		t.Fatal("une fiche a été écrite à l'ouverture, alors que le joueur n'a rien dit")
	}
	if m, _ := r.book.Recall(context.Background(), testNPC().Key(), "aldric"); m != "" {
		t.Fatalf("un souvenir a été écrit à l'ouverture: %q", m)
	}

	msgs := r.fake.ChatCalls()[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != llm.User || last.Content != openingStageDirection {
		t.Fatalf("dernier message: %+v", last)
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "C'est le début de la conversation") || !strings.Contains(sys, "demande-lui qui il est") {
		t.Fatal("le prompt n'invite pas le PNJ à demander au voyageur de se présenter")
	}
}

func TestOpeningForAKnownTravellerMentionsHeAlreadyIntroducedHimself(t *testing.T) {
	r := newRig(t, Config{CardCooldown: time.Second}, "aldric")
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return reply("Ravi.", &lore.Intro{Alias: "Aldric", Role: "forgeron"}, ""), nil
	}
	r.ask("aldric", "Je suis Aldric le forgeron.")

	rq := r.req("aldric", "")
	rq.Opening = true
	r.askReq(rq)
	sys := r.lastSystem()
	if strings.Contains(sys, "demande-lui qui il est") || !strings.Contains(sys, "tu le connais déjà") {
		t.Fatal("un voyageur déjà présenté ne doit pas être invité à se présenter à nouveau")
	}
}

func TestInCharacterWording(t *testing.T) {
	for _, f := range []Failure{Busy, Cooldown, Budget, Blocked, Unavailable} {
		got := InCharacter("Frère Arnaud", f)
		if !strings.HasPrefix(got, "Frère Arnaud ") {
			t.Errorf("%v: %q", f, got)
		}
	}
	if !strings.HasPrefix(InCharacter("", Busy), "Le PNJ ") {
		t.Error("nom vide")
	}
}

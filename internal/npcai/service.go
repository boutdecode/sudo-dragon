package npcai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// DefaultBlockedCategories sont les catégories de modération qui font refuser un message
// ou une présentation. Les catégories « santé », « finance » et « droit » en sont absentes:
// un soigneur ou un marchand en parle légitimement.
var DefaultBlockedCategories = []string{
	"hate_and_discrimination", "violence_and_threats", "sexual", "selfharm", "dangerous", "criminal", "pii", "jailbreaking",
}

var (
	errModerationOff = errors.New("npcai: modération indisponible")
	errFlagged       = errors.New("npcai: texte refusé par la modération")
)

type job struct {
	req       Request
	stock     *StockRequest // demande d'étalage (au lieu d'une réplique)
	stockDone func(StockReply)
	done      func(Reply)
	acct      string
	enqueued  time.Time
}

// Service fait parler les PNJ. Il est sûr pour un usage concurrent.
type Service struct {
	llm      llm.Client
	book     *lore.Book
	cfg      Config
	log      *slog.Logger
	now      func() time.Time
	places   func(i18n.Lang, world.RegionPos) string
	blocked  map[string]bool
	provider string

	queue chan *job
	wg    sync.WaitGroup

	// Disponibilité, fixée par Probe (optimiste tant qu'il n'a pas été appelé).
	chatOK, moderationOK, guardrailsOK atomic.Bool

	mu       sync.Mutex
	inflight map[string]bool
	lastMsg  map[string]time.Time
	hours    map[string][]time.Time
	lastCard map[string]time.Time
	day      time.Time // jour UTC du compteur de jetons
	tokens   int
	fails    int
	openTill time.Time // disjoncteur: pas d'appel avant cet instant

	nRequests, nFailures, nBlocked, nRemembered atomic.Uint64
	nQuestsProposed, nQuestsRejected            atomic.Uint64
	nStockGenerated, nStockRejected             atomic.Uint64

	wares               WaresRepo
	stockInflight       map[world.NPCKey]bool
	latencyMS, latencyN atomic.Int64
	active              atomic.Int32
}

// Option configure un Service.
type Option func(*Service)

// WithLogger fixe le journal.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// WithClock remplace l'horloge (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithProvider fixe le nom du fournisseur montré aux joueurs dans l'avis de confidentialité.
func WithProvider(name string) Option { return func(s *Service) { s.provider = name } }

// WithPlaceNames fournit le nom d'un lieu d'après sa région, pour nommer l'origine des rumeurs.
func WithPlaceNames(f func(i18n.Lang, world.RegionPos) string) Option {
	return func(s *Service) { s.places = f }
}

// WithBlockedCategories remplace les catégories de modération bloquantes.
func WithBlockedCategories(cats []string) Option {
	return func(s *Service) {
		s.blocked = map[string]bool{}
		for _, c := range cats {
			s.blocked[c] = true
		}
	}
}

// New crée un service. Appeler Start pour lancer les workers, et Probe pour vérifier l'API.
func New(c llm.Client, book *lore.Book, cfg Config, opts ...Option) *Service {
	cfg = cfg.withDefaults()
	s := &Service{
		llm: c, book: book, cfg: cfg, log: slog.Default(), now: time.Now,
		queue:    make(chan *job, cfg.QueueSize),
		inflight: map[string]bool{}, lastMsg: map[string]time.Time{}, hours: map[string][]time.Time{}, lastCard: map[string]time.Time{},
	}
	s.chatOK.Store(true)
	s.moderationOK.Store(true)
	s.guardrailsOK.Store(true)
	WithBlockedCategories(DefaultBlockedCategories)(s)
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start lance les workers; ils s'arrêtent à l'annulation de ctx.
func (s *Service) Start(ctx context.Context) {
	for i := 0; i < s.cfg.Workers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-s.queue:
					s.run(ctx, j)
				}
			}
		}()
	}
}

// Wait attend la fin des workers (après l'annulation du contexte de Start).
func (s *Service) Wait() { s.wg.Wait() }

// Provider renvoie le nom du fournisseur du modèle (pour l'avis de confidentialité),
// ou "" s'il n'est pas connu: l'appelant emploie alors une formule générique dans la
// langue du joueur.
func (s *Service) Provider() string { return s.provider }

// Available indique si les PNJ peuvent converser: l'API répond et le disjoncteur est fermé.
func (s *Service) Available() bool {
	if !s.chatOK.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.now().Before(s.openTill)
}

// Ask demande une réplique de PNJ. Il ne bloque jamais et n'appelle done QUE depuis
// une autre goroutine que l'appelant (jamais en ligne), pour qu'un appelant qui tient
// un verrou ou une file propre puisse appeler Ask sans risque d'interblocage.
func (s *Service) Ask(req Request, done func(Reply)) {
	reject := func(f Failure) {
		s.nFailures.Add(1)
		go done(s.failureReply(req, f))
	}
	if !s.Available() {
		reject(Unavailable)
		return
	}
	acct := strings.ToLower(req.Speaker.Account)
	now := s.now()

	s.mu.Lock()
	s.rollDay(now)
	if len(s.lastMsg) > 10_000 {
		s.sweep(now)
	}
	hist := trimBefore(s.hours[acct], now.Add(-time.Hour))
	var fail Failure
	switch {
	case s.inflight[acct]:
		fail = Busy
	case now.Sub(s.lastMsg[acct]) < s.cfg.MinInterval:
		fail = Cooldown
	case len(hist) >= s.cfg.HourlyLimit:
		fail = Cooldown
	case s.tokens+s.cfg.MaxTokens > s.cfg.DailyTokens:
		fail = Budget
	}
	if fail != OK {
		s.mu.Unlock()
		reject(fail)
		return
	}
	s.inflight[acct] = true
	s.lastMsg[acct] = now
	s.hours[acct] = append(hist, now)
	s.mu.Unlock()

	select {
	case s.queue <- &job{req: req, done: done, acct: acct, enqueued: now}:
	default: // file pleine
		s.release(acct)
		reject(Busy)
	}
}

func (s *Service) release(acct string) {
	s.mu.Lock()
	delete(s.inflight, acct)
	s.mu.Unlock()
}

func trimBefore(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	return ts[i:]
}

// rollDay remet le compteur de jetons à zéro quand le jour UTC change (verrou tenu).
func (s *Service) rollDay(now time.Time) {
	day := now.UTC().Truncate(24 * time.Hour)
	if !day.Equal(s.day) {
		s.day, s.tokens = day, 0
	}
}

// sweep oublie les joueurs inactifs depuis plus d'une heure (verrou tenu).
func (s *Service) sweep(now time.Time) {
	for a, t := range s.lastMsg {
		if now.Sub(t) > time.Hour && !s.inflight[a] {
			delete(s.lastMsg, a)
			delete(s.hours, a)
			delete(s.lastCard, a)
		}
	}
}

// run traite une demande sans jamais faire tomber le serveur: un plantage inattendu
// (réponse du modèle imprévue…) devient une réplique d'échec.
func (s *Service) run(ctx context.Context, j *job) {
	if j.stock != nil {
		s.runStock(ctx, j)
		return
	}
	s.active.Add(1)
	start := s.now()
	reply := func() (r Reply) {
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("plantage dans le service de dialogue", "panic", fmt.Sprint(p))
				r = s.failureReply(j.req, Unavailable)
			}
		}()
		return s.process(ctx, j.req, j.acct)
	}()
	s.active.Add(-1)
	s.release(j.acct)

	s.nRequests.Add(1)
	if reply.Failure != OK {
		s.nFailures.Add(1)
	}
	s.latencyMS.Add(s.now().Sub(start).Milliseconds())
	s.latencyN.Add(1)
	j.done(reply)
}

func (s *Service) guardrail() *llm.Guardrail {
	th := map[string]float64{}
	for c := range s.blocked {
		th[c] = s.cfg.BlockedThreat
	}
	return &llm.Guardrail{Thresholds: th}
}

// moderator est le contrôle appliqué aux présentations avant leur écriture: refus si le texte
// est signalé, et aussi si la modération est indisponible (on n'écrit jamais sans contrôle).
func (s *Service) moderator(ctx context.Context, texts []string) error {
	if !s.moderationOK.Load() {
		return errModerationOff
	}
	verdicts, err := s.llm.Moderate(ctx, texts)
	if err != nil {
		return err
	}
	if len(verdicts) != len(texts) {
		return llm.ErrBadResponse
	}
	for _, v := range verdicts {
		if v.Violates(s.blocked) {
			return errFlagged
		}
	}
	return nil
}

func (s *Service) process(parent context.Context, req Request, acct string) Reply {
	ctx, cancel := context.WithTimeout(parent, s.cfg.Timeout)
	defer cancel()
	npc := req.NPC
	account := req.Speaker.Account

	// Mémoire: en cas de panne de la base, la conversation continue sans souvenirs.
	heard, err := s.book.Heard(ctx, npc.Region, account)
	if err != nil {
		s.log.Warn("lecture des rumeurs", "err", err)
		heard = nil
	}
	memory, err := s.book.Recall(ctx, npc.Key(), account)
	if err != nil {
		s.log.Warn("lecture du souvenir", "err", err)
	}

	mine, others, err := s.book.HeardDeeds(ctx, npc.Region, account)
	if err != nil {
		s.log.Warn("lecture des quêtes rendues", "err", err)
	}
	req.Deeds = Deeds{Mine: mine, Others: others}

	maxTokens := s.cfg.MaxTokens
	if req.Offer != nil {
		maxTokens += questExtraTokens // le champ "quest" allonge la réponse
	}
	temp := s.cfg.Temperature
	chat := llm.Request{
		Messages:    BuildMessages(req, heard, memory, s.cfg.MaxHistory, s.places),
		JSON:        true,
		MaxTokens:   maxTokens,
		Temperature: &temp,
	}

	// Modération de l'entrée: par le champ « guardrails » (une seule requête) ou, à défaut, explicitement.
	memoryWrites := s.moderationOK.Load()
	if s.guardrailsOK.Load() {
		chat.Guardrail = s.guardrail()
	} else if blocked, checked := s.screen(ctx, req.Message); blocked {
		s.nBlocked.Add(1)
		return s.failureReply(req, Blocked)
	} else if !checked {
		memoryWrites = false // pas pu vérifier ce message: on parle, mais on ne retient rien
	}

	resp, err := s.llm.Chat(ctx, chat)
	if err != nil && chat.Guardrail != nil && errors.Is(err, llm.ErrBadRequest) {
		// L'API refuse désormais le champ « guardrails »: repli sur la modération explicite.
		s.log.Warn("guardrails refusés par l'API, repli sur la modération explicite", "err", err)
		s.guardrailsOK.Store(false)
		chat.Guardrail = nil
		if blocked, checked := s.screen(ctx, req.Message); blocked {
			s.nBlocked.Add(1)
			return s.failureReply(req, Blocked)
		} else if !checked {
			memoryWrites = false
		}
		resp, err = s.llm.Chat(ctx, chat)
	}
	if err != nil {
		return s.chatFailure(req, err)
	}
	tokens := resp.TotalTokens(chat)
	s.success(tokens)

	out, ok := parseOutput(resp.Content)
	if !ok {
		s.log.Warn("réponse du modèle inexploitable")
		return Reply{Text: s.failureReply(req, Unavailable).Text, Failure: Unavailable, Tokens: tokens}
	}
	reply := Reply{Text: out.Reply, Tokens: tokens}
	if req.Offer != nil && out.Quest != nil && out.Structured && !req.Opening {
		reply.Quest = s.checkQuest(ctx, *out.Quest, req.Offer.Quest)
	}

	// Ce qui est retenu n'est écrit que si la réponse respecte le format et que la modération fonctionne.
	if out.Structured && memoryWrites && !req.Opening {
		facts := lore.Facts{Level: req.Speaker.Level, BossKills: req.Speaker.BossKills, Deaths: req.Speaker.Deaths}
		if out.Intro != nil && s.cardAllowed(acct) {
			if _, err := s.book.Remember(ctx, account, npc.Region, *out.Intro, facts, s.moderator); err != nil {
				s.log.Info("présentation non retenue", "raison", err.Error())
			} else {
				reply.Remembered = true
				s.nRemembered.Add(1)
				s.markCard(acct)
			}
		}
		if err := s.book.Memorize(ctx, npc.Key(), account, out.Memory); err != nil {
			s.log.Warn("écriture du souvenir", "err", err)
		}
		if err := s.book.RefreshFacts(ctx, account, facts); err != nil {
			s.log.Warn("mise à jour des faits", "err", err)
		}
	}
	return reply
}

// questExtraTokens est la marge de réponse ajoutée quand le modèle peut proposer une quête.
const questExtraTokens = 160

// checkQuest valide (quest.Validate) puis modère une quête proposée. Toute erreur, y compris une
// modération indisponible, écarte la proposition : on n'affiche jamais un texte non contrôlé.
func (s *Service) checkQuest(ctx context.Context, p quest.Proposal, qc quest.Context) *quest.Quest {
	s.nQuestsProposed.Add(1)
	q, err := quest.Validate(p, qc)
	if err != nil {
		s.nQuestsRejected.Add(1)
		s.log.Info("quête proposée écartée", "raison", err.Error())
		return nil
	}
	texts := []string{q.Title, q.Story}
	if q.Reward.Note != "" {
		texts = append(texts, q.Reward.Note)
	}
	if err := s.moderator(ctx, texts); err != nil {
		s.nQuestsRejected.Add(1)
		s.log.Info("quête proposée non modérée", "raison", err.Error())
		return nil
	}
	return &q
}

// screen modère un message avec l'endpoint de modération. blocked: le message est refusé.
// checked: la vérification a pu avoir lieu (faux si la modération est indisponible).
func (s *Service) screen(ctx context.Context, msg string) (blocked, checked bool) {
	if !s.moderationOK.Load() {
		return false, false
	}
	verdicts, err := s.llm.Moderate(ctx, []string{msg})
	if err != nil || len(verdicts) != 1 {
		return false, false
	}
	return verdicts[0].Violates(s.blocked), true
}

func (s *Service) cardAllowed(acct string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastCard[acct]
	return !ok || s.now().Sub(last) >= s.cfg.CardCooldown
}

func (s *Service) markCard(acct string) {
	s.mu.Lock()
	s.lastCard[acct] = s.now()
	s.mu.Unlock()
}

// success enregistre un appel réussi: jetons du jour, disjoncteur remis à zéro.
func (s *Service) success(tokens int) {
	s.mu.Lock()
	s.rollDay(s.now())
	s.tokens += tokens
	s.fails = 0
	s.mu.Unlock()
}

// chatFailure traduit une erreur du fournisseur en réplique d'échec et alimente le disjoncteur.
func (s *Service) chatFailure(req Request, err error) Reply {
	switch {
	case errors.Is(err, llm.ErrBlocked):
		s.nBlocked.Add(1)
		return s.failureReply(req, Blocked)
	case errors.Is(err, llm.ErrRateLimited):
		return s.failureReply(req, Busy)
	case errors.Is(err, llm.ErrUnauthorized):
		s.log.Error("clé d'API refusée: dialogues de PNJ désactivés")
		s.chatOK.Store(false)
		return s.failureReply(req, Unavailable)
	}
	// Panne, délai dépassé, réponse inexploitable: le disjoncteur s'ouvre après plusieurs échecs
	// consécutifs pour ne pas faire attendre chaque joueur sur un service qui ne répond plus.
	s.log.Warn("appel au modèle échoué", "err", err.Error())
	s.mu.Lock()
	s.fails++
	if s.fails >= s.cfg.BreakerFails {
		s.openTill = s.now().Add(s.cfg.BreakerPause)
		s.fails = 0
		s.log.Error("service de dialogue suspendu", "pause", s.cfg.BreakerPause.String())
	}
	s.mu.Unlock()
	return s.failureReply(req, Unavailable)
}

// failureReply fabrique une réplique « en personnage » pour un échec.
func (s *Service) failureReply(req Request, f Failure) Reply {
	return Reply{Text: InCharacter(req.Lang, req.NPC.NameIn(req.Lang), f), Failure: f}
}

// InCharacter renvoie la réplique d'un PNJ qui ne peut pas répondre, sans jamais
// montrer au joueur de message technique.
func InCharacter(l i18n.Lang, name string, f Failure) string {
	if name == "" {
		name = i18n.T(l, "npc.fail.anonymous")
	}
	switch f {
	case Busy:
		return i18n.T(l, "npc.fail.busy", name)
	case Cooldown:
		return i18n.T(l, "npc.fail.cooldown", name)
	case Budget:
		return i18n.T(l, "npc.fail.budget", name)
	case Blocked:
		return i18n.T(l, "npc.fail.blocked", name)
	}
	return i18n.T(l, "npc.fail.default", name)
}

// ProbeResult est le résultat de la vérification de l'API.
type ProbeResult struct {
	Chat, Guardrails, Moderation bool
	ChatErr, ModerationErr       error
}

// Probe vérifie, avec de toutes petites requêtes, que le chat fonctionne, que le champ
// « guardrails » est accepté et que la modération répond; il règle ensuite la
// disponibilité du service en conséquence. À appeler au démarrage.
func (s *Service) Probe(ctx context.Context) ProbeResult {
	var res ProbeResult
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	temp := 0.0
	probe := llm.Request{
		Messages: []llm.Message{
			{Role: llm.System, Content: `Réponds uniquement par l'objet JSON {"ok": true}.`},
			{Role: llm.User, Content: "test"},
		},
		JSON: true, MaxTokens: 16, Temperature: &temp, Guardrail: s.guardrail(),
	}
	resp, err := s.llm.Chat(ctx, probe)
	if err != nil && errors.Is(err, llm.ErrBadRequest) { // champ « guardrails » refusé
		probe.Guardrail = nil
		resp, err = s.llm.Chat(ctx, probe)
	} else if err == nil {
		res.Guardrails = true
	}
	if err != nil {
		res.ChatErr = err
	} else {
		res.Chat = true
		s.success(resp.TotalTokens(probe))
	}

	if _, err := s.llm.Moderate(ctx, []string{"Bonjour voyageur"}); err != nil {
		res.ModerationErr = err
	} else {
		res.Moderation = true
	}

	s.chatOK.Store(res.Chat)
	s.guardrailsOK.Store(res.Guardrails)
	s.moderationOK.Store(res.Moderation)
	return res
}

// Supervise relance la sonde à intervalle régulier tant que le chat ou la modération
// sont indisponibles : une panne au démarrage (ou une clé corrigée entre-temps) ne doit
// pas désactiver les dialogues jusqu'au prochain redémarrage. Bloquant, à lancer dans une
// goroutine ; s'arrête à l'annulation de ctx.
func (s *Service) Supervise(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.chatOK.Load() && s.moderationOK.Load() {
				continue
			}
			res := s.Probe(ctx)
			s.log.Info("nouvelle sonde du service de dialogue", "chat", res.Chat, "moderation", res.Moderation)
		}
	}
}

// Stats renvoie l'état courant des compteurs.
func (s *Service) Stats() Stats {
	s.mu.Lock()
	s.rollDay(s.now())
	tokens := s.tokens
	s.mu.Unlock()
	st := Stats{
		Available: s.Available(), Guardrails: s.guardrailsOK.Load(), Moderation: s.moderationOK.Load(),
		Requests: s.nRequests.Load(), Failures: s.nFailures.Load(), Blocked: s.nBlocked.Load(), Remembered: s.nRemembered.Load(),
		QuestsOffered: s.nQuestsProposed.Load(), QuestsRejected: s.nQuestsRejected.Load(),
		StockGenerated: s.nStockGenerated.Load(), StockRejected: s.nStockRejected.Load(),
		TokensToday: tokens, DailyBudget: s.cfg.DailyTokens, InFlight: int(s.active.Load()), Queued: len(s.queue),
	}
	if n := s.latencyN.Load(); n > 0 {
		st.AvgLatencyMS = s.latencyMS.Load() / n
	}
	return st
}

// runStock traite une demande d'étalage sans jamais faire tomber le serveur.
func (s *Service) runStock(ctx context.Context, j *job) {
	s.active.Add(1)
	reply := func() (r StockReply) {
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("plantage dans la génération d'étalage", "panic", fmt.Sprint(p))
				r = StockReply{Failure: Unavailable}
			}
		}()
		return s.processStock(ctx, *j.stock)
	}()
	s.active.Add(-1)
	if reply.Failure != OK {
		s.nFailures.Add(1)
	}
	j.stockDone(reply)
}

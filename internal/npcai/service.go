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
	"github.com/kevin/ascii-adventure/internal/worldlore"
)

// DefaultBlockedCategories are the moderation categories that cause a message
// or an introduction to be refused. The "health", "financial" and "law" categories are absent:
// a healer or a merchant legitimately talks about them.
var DefaultBlockedCategories = []string{
	"hate_and_discrimination", "violence_and_threats", "sexual", "selfharm", "dangerous", "criminal", "pii", "jailbreaking",
}

var (
	errModerationOff = errors.New("npcai: moderation unavailable")
	errFlagged       = errors.New("npcai: text rejected by moderation")
)

type job struct {
	req       Request
	stock     *StockRequest // stock request (instead of a reply)
	stockDone func(StockReply)
	done      func(Reply)
	acct      string
	enqueued  time.Time
}

// Service makes NPCs talk. It is safe for concurrent use.
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

	// Availability, set by Probe (optimistic until it has been called).
	chatOK, moderationOK, guardrailsOK atomic.Bool

	mu       sync.Mutex
	inflight map[string]bool
	lastMsg  map[string]time.Time
	hours    map[string][]time.Time
	lastCard map[string]time.Time
	day      time.Time // UTC day of the token counter
	tokens   int
	fails    int
	openTill time.Time // circuit breaker: no call before this instant

	nRequests, nFailures, nBlocked, nRemembered atomic.Uint64
	nQuestsProposed, nQuestsRejected            atomic.Uint64
	nStockGenerated, nStockRejected             atomic.Uint64

	canon               *worldlore.Canon
	wares               WaresRepo
	stockInflight       map[world.NPCKey]bool
	latencyMS, latencyN atomic.Int64
	active              atomic.Int32
}

// Option configures a Service.
type Option func(*Service)

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// WithClock replaces the clock (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithProvider sets the provider name shown to players in the privacy notice.
func WithProvider(name string) Option { return func(s *Service) { s.provider = name } }

// WithPlaceNames provides a place's name from its region, to name the origin of rumors.
func WithPlaceNames(f func(i18n.Lang, world.RegionPos) string) Option {
	return func(s *Service) { s.places = f }
}

// WithWorldLore gives NPCs the world's canon (see package worldlore). It is read at
// each request, so a reload of the canon applies to the next conversation.
func WithWorldLore(c *worldlore.Canon) Option { return func(s *Service) { s.canon = c } }

// WithBlockedCategories replaces the blocking moderation categories.
func WithBlockedCategories(cats []string) Option {
	return func(s *Service) {
		s.blocked = map[string]bool{}
		for _, c := range cats {
			s.blocked[c] = true
		}
	}
}

// New creates a service. Call Start to launch the workers, and Probe to check the API.
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

// Start launches the workers; they stop when ctx is canceled.
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

// Wait waits for the workers to finish (after the Start context is canceled).
func (s *Service) Wait() { s.wg.Wait() }

// Provider returns the model provider's name (for the privacy notice),
// or "" if unknown: the caller then uses a generic phrase in the
// player's language.
func (s *Service) Provider() string { return s.provider }

// Available reports whether NPCs can converse: the API responds and the circuit breaker is closed.
func (s *Service) Available() bool {
	if !s.chatOK.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.now().Before(s.openTill)
}

// Ask requests an NPC reply. It never blocks and calls done ONLY from
// a goroutine other than the caller's (never inline), so that a caller holding
// a lock or its own queue can call Ask without risk of deadlock.
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
	default: // queue full
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

// rollDay resets the token counter to zero when the UTC day changes (lock held).
func (s *Service) rollDay(now time.Time) {
	day := now.UTC().Truncate(24 * time.Hour)
	if !day.Equal(s.day) {
		s.day, s.tokens = day, 0
	}
}

// sweep forgets players inactive for more than an hour (lock held).
func (s *Service) sweep(now time.Time) {
	for a, t := range s.lastMsg {
		if now.Sub(t) > time.Hour && !s.inflight[a] {
			delete(s.lastMsg, a)
			delete(s.hours, a)
			delete(s.lastCard, a)
		}
	}
}

// run handles a request without ever bringing the server down: an unexpected crash
// (unforeseen model response...) becomes a failure reply.
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
				s.log.Error("crash in the dialogue service", "panic", fmt.Sprint(p))
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

// moderator is the check applied to introductions before they are written: refusal if the text
// is flagged, and also if moderation is unavailable (nothing is ever written unchecked).
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

	// Memory: if the database is down, the conversation continues without memories.
	heard, err := s.book.Heard(ctx, npc.Region, account)
	if err != nil {
		s.log.Warn("reading rumors", "err", err)
		heard = nil
	}
	memory, err := s.book.Recall(ctx, npc.Key(), account)
	if err != nil {
		s.log.Warn("reading memory", "err", err)
	}

	mine, others, err := s.book.HeardDeeds(ctx, npc.Region, account)
	if err != nil {
		s.log.Warn("reading completed quests", "err", err)
	}
	req.Deeds = Deeds{Mine: mine, Others: others}

	req.WorldLore = s.canon.For(req.Lang)

	maxTokens := s.cfg.MaxTokens
	if req.Offer != nil {
		maxTokens += questExtraTokens // the "quest" field lengthens the response
	}
	temp := s.cfg.Temperature
	chat := llm.Request{
		Messages:    BuildMessages(req, heard, memory, s.cfg.MaxHistory, s.places),
		JSON:        true,
		MaxTokens:   maxTokens,
		Temperature: &temp,
	}

	// Input moderation: through the "guardrails" field (a single request) or, failing that, explicitly.
	memoryWrites := s.moderationOK.Load()
	if s.guardrailsOK.Load() {
		chat.Guardrail = s.guardrail()
	} else if blocked, checked := s.screen(ctx, req.Message); blocked {
		s.nBlocked.Add(1)
		return s.failureReply(req, Blocked)
	} else if !checked {
		memoryWrites = false // could not check this message: we talk, but retain nothing
	}

	resp, err := s.llm.Chat(ctx, chat)
	if err != nil && chat.Guardrail != nil && errors.Is(err, llm.ErrBadRequest) {
		// The API now rejects the "guardrails" field: fall back to explicit moderation.
		s.log.Warn("guardrails rejected by the API, falling back to explicit moderation", "err", err)
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
		s.log.Warn("unusable model response")
		return Reply{Text: s.failureReply(req, Unavailable).Text, Failure: Unavailable, Tokens: tokens}
	}
	reply := Reply{Text: out.Reply, Tokens: tokens}
	if req.Offer != nil && out.Quest != nil && out.Structured && !req.Opening {
		reply.Quest = s.checkQuest(ctx, *out.Quest, req.Offer.Quest)
		reply.QuestDropped = reply.Quest == nil
	}

	// What is retained is written only if the response follows the format and moderation works.
	if out.Structured && memoryWrites && !req.Opening {
		facts := lore.Facts{Level: req.Speaker.Level, BossKills: req.Speaker.BossKills, Deaths: req.Speaker.Deaths}
		if out.Intro != nil && s.cardAllowed(acct) {
			if _, err := s.book.Remember(ctx, account, npc.Region, *out.Intro, facts, s.moderator); err != nil {
				s.log.Info("introduction not retained", "reason", err.Error())
			} else {
				reply.Remembered = true
				s.nRemembered.Add(1)
				s.markCard(acct)
			}
		}
		if err := s.book.Memorize(ctx, npc.Key(), account, out.Memory); err != nil {
			s.log.Warn("writing memory", "err", err)
		}
		if err := s.book.RefreshFacts(ctx, account, facts); err != nil {
			s.log.Warn("updating facts", "err", err)
		}
	}
	return reply
}

// questExtraTokens is the extra response margin added when the model can propose a quest.
const questExtraTokens = 160

// checkQuest validates (quest.Validate) then moderates a proposed quest. Any error, including
// unavailable moderation, discards the proposal: unchecked text is never displayed.
func (s *Service) checkQuest(ctx context.Context, p quest.Proposal, qc quest.Context) *quest.Quest {
	s.nQuestsProposed.Add(1)
	q, err := quest.Validate(p, qc)
	if err != nil {
		s.nQuestsRejected.Add(1)
		s.log.Info("proposed quest discarded", "reason", err.Error())
		return nil
	}
	texts := []string{q.Title, q.Story}
	if q.Reward.Note != "" {
		texts = append(texts, q.Reward.Note)
	}
	if err := s.moderator(ctx, texts); err != nil {
		s.nQuestsRejected.Add(1)
		s.log.Info("proposed quest not moderated", "reason", err.Error())
		return nil
	}
	return &q
}

// screen moderates a message with the moderation endpoint. blocked: the message is refused.
// checked: the check could take place (false if moderation is unavailable).
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

// success records a successful call: day's tokens, circuit breaker reset.
func (s *Service) success(tokens int) {
	s.mu.Lock()
	s.rollDay(s.now())
	s.tokens += tokens
	s.fails = 0
	s.mu.Unlock()
}

// chatFailure turns a provider error into a failure reply and feeds the circuit breaker.
func (s *Service) chatFailure(req Request, err error) Reply {
	switch {
	case errors.Is(err, llm.ErrBlocked):
		s.nBlocked.Add(1)
		return s.failureReply(req, Blocked)
	case errors.Is(err, llm.ErrRateLimited):
		return s.failureReply(req, Busy)
	case errors.Is(err, llm.ErrUnauthorized):
		s.log.Error("API key rejected: NPC dialogues disabled")
		s.chatOK.Store(false)
		return s.failureReply(req, Unavailable)
	}
	// Outage, timeout, unusable response: the circuit breaker opens after several consecutive
	// failures so as not to make every player wait on a service that no longer responds.
	s.log.Warn("model call failed", "err", err.Error())
	s.mu.Lock()
	s.fails++
	if s.fails >= s.cfg.BreakerFails {
		s.openTill = s.now().Add(s.cfg.BreakerPause)
		s.fails = 0
		s.log.Error("dialogue service suspended", "pause", s.cfg.BreakerPause.String())
	}
	s.mu.Unlock()
	return s.failureReply(req, Unavailable)
}

// failureReply builds an in-character reply for a failure.
func (s *Service) failureReply(req Request, f Failure) Reply {
	return Reply{Text: InCharacter(req.Lang, req.NPC.NameIn(req.Lang), f), Failure: f}
}

// InCharacter returns the line of an NPC who cannot answer, without ever
// showing the player a technical message.
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

// ProbeResult is the result of the API check.
type ProbeResult struct {
	Chat, Guardrails, Moderation bool
	ChatErr, ModerationErr       error
}

// Probe checks, with tiny requests, that chat works, that the "guardrails"
// field is accepted and that moderation responds; it then sets the service
// availability accordingly. To be called at startup.
func (s *Service) Probe(ctx context.Context) ProbeResult {
	var res ProbeResult
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	temp := 0.0
	probe := llm.Request{
		Messages: []llm.Message{
			{Role: llm.System, Content: `Reply only with the JSON object {"ok": true}.`},
			{Role: llm.User, Content: "test"},
		},
		JSON: true, MaxTokens: 16, Temperature: &temp, Guardrail: s.guardrail(),
	}
	resp, err := s.llm.Chat(ctx, probe)
	if err != nil && errors.Is(err, llm.ErrBadRequest) { // "guardrails" field rejected
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

	if _, err := s.llm.Moderate(ctx, []string{"Hello traveler"}); err != nil {
		res.ModerationErr = err
	} else {
		res.Moderation = true
	}

	s.chatOK.Store(res.Chat)
	s.guardrailsOK.Store(res.Guardrails)
	s.moderationOK.Store(res.Moderation)
	return res
}

// Supervise reruns the probe at regular intervals as long as chat or moderation
// is unavailable: an outage at startup (or a key fixed in the meantime) must not
// disable dialogues until the next restart. Blocking, to be run in a
// goroutine; stops when ctx is canceled.
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
			s.log.Info("new probe of the dialogue service", "chat", res.Chat, "moderation", res.Moderation)
		}
	}
}

// Stats returns the current state of the counters.
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

// runStock handles a stock request without ever bringing the server down.
func (s *Service) runStock(ctx context.Context, j *job) {
	s.active.Add(1)
	reply := func() (r StockReply) {
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("crash in stock generation", "panic", fmt.Sprint(p))
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

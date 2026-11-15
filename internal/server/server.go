// Package server handles the WebSocket transport, sessions and broadcasting.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/game"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	loginTimeout = 10 * time.Second
	writeTimeout = 5 * time.Second
	sendBuffer   = 64
	readLimit    = 4096

	// Dead connection detection: without it, a client whose network
	// disappeared would keep its account "connected" and block its reconnection.
	pingEvery   = 25 * time.Second
	pingTimeout = 10 * time.Second

	// Per-IP-address quotas.
	maxFailedLogins    = 8  // authentication failures per minute
	maxRegistrations   = 10 // account creations per hour
	failedLoginWindow  = time.Minute
	registrationWindow = time.Hour
)

// Backend provides the accounts and the persistence of characters.
type Backend interface {
	// CreateAccount and Authenticate return the name as stored.
	CreateAccount(name, password string) (string, error)
	Authenticate(name, password string) (string, error)
	// LoadCharacter returns the most recent state, or nil for a new character.
	LoadCharacter(name string) (*store.Character, error)
	// SaveCharacter must not block.
	SaveCharacter(name string, c store.Character)
}

// Server accepts WebSocket connections and keeps the list of connected players.
type Server struct {
	Game *game.Game
	Log  *slog.Logger

	be            Backend
	failedLogins  *limiter
	registrations *limiter

	mu       sync.Mutex
	sessions map[int]*session
	// reserved holds the names (lowercase) being connected: they
	// are taken while the game loop processes Join, without holding s.mu.
	reserved map[string]bool
}

// New creates a server for the given world. Call Run to start the game loop.
func New(w *world.World, log *slog.Logger, be Backend) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		Game:          game.New(w),
		Log:           log,
		be:            be,
		failedLogins:  newLimiter(maxFailedLogins, failedLoginWindow),
		registrations: newLimiter(maxRegistrations, registrationWindow),
		sessions:      make(map[int]*session),
		reserved:      make(map[string]bool),
	}
	s.Game.Dungeons = true
	s.Game.Persist = func(name string, sv game.Saved) { be.SaveCharacter(name, fromSaved(sv)) }
	return s
}

// SetRegistrationLimit sets the number of accounts that can be created per hour and per
// IP address. Zero or less: no limit (load tests, development).
func (s *Server) SetRegistrationLimit(perHour int) {
	if perHour <= 0 {
		perHour = int(^uint(0) >> 1)
	}
	s.registrations = newLimiter(perHour, registrationWindow)
}

// fromSaved and toSaved convert between the game state and the stored state.
func fromSaved(sv game.Saved) store.Character {
	c := store.Character{
		Level: sv.Stats.Level, XP: sv.Stats.XP, Potions: sv.Stats.Potions, Gold: sv.Stats.Gold,
		HP: sv.HP, X: sv.Pos.X, Y: sv.Pos.Y,
		LLMConsent: sv.LLMConsent, Kills: sv.Stats.Kills, BossKills: sv.Stats.BossKills, Deaths: sv.Stats.Deaths,
		Class: string(sv.Stats.Class), Race: string(sv.Stats.Race), Scores: sv.Stats.Scores, MaxHP: sv.Stats.MaxHP, Mana: sv.Mana,
		Quests: sv.Quests, Rep: sv.Rep, QuestsDirty: sv.QuestsDirty,
		Gear: sv.Gear, GearDirty: sv.GearDirty,
		Discoveries: sv.Discovered, DiscoveriesDirty: sv.DiscoveredDirty,
		Stones: sv.Stones, Maps: sv.Maps, Resources: sv.Resources, Skills: sv.Skills,
	}
	if sv.Respawn != nil {
		c.RespawnSet, c.RespawnX, c.RespawnY = true, sv.Respawn.X, sv.Respawn.Y
	}
	return c
}

func toSaved(c store.Character) *game.Saved {
	sv := &game.Saved{
		Stats: combat.StatsFor(c.Level, c.XP, c.Potions, hero.Class(c.Class), hero.Scores(c.Scores), c.MaxHP).
			WithRace(hero.Race(c.Race)).WithGold(c.Gold).WithRecord(c.Kills, c.BossKills, c.Deaths),
		HP:         c.HP,
		Mana:       c.Mana,
		Pos:        world.Pos{X: c.X, Y: c.Y},
		LLMConsent: c.LLMConsent,
		Quests:     c.Quests, Rep: c.Rep, Gear: c.Gear, GearDirty: c.GearDirty,
		Discovered: c.Discoveries,
		Stones:     c.Stones,
		Maps:       c.Maps,
		Resources:  c.Resources,
		Skills:     c.Skills,
	}
	if c.RespawnSet {
		sv.Respawn = &world.Pos{X: c.RespawnX, Y: c.RespawnY}
	}
	return sv
}

// SetTalker plugs in the service that makes NPCs talk (nil: no free discussion).
// To be called before Run.
func (s *Server) SetTalker(t game.Talker) { s.Game.Talker = t }

// Run executes the game loop (blocking) until ctx is cancelled.
func (s *Server) Run(ctx context.Context) { s.Game.Run(ctx) }

// Handler returns the HTTP handler: /ws (game), /healthz (probe) and /stats (metrics).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/stats", s.handleStats)
	return mux
}

// Stats is the JSON shape of /stats.
type Stats struct {
	Players     int     `json:"players"`
	Monsters    int     `json:"monsters"`
	Ticks       uint64  `json:"ticks"`
	SlowTicks   uint64  `json:"slow_ticks"`
	TickTotalMS float64 `json:"tick_total_ms"`
	TickMaxMS   float64 `json:"tick_max_ms"`
	// Steady state: the last 60 seconds.
	TickP99MS       float64 `json:"tick_p99_ms"`
	TickRecentMaxMS float64 `json:"tick_recent_max_ms"`

	// LLM describes the NPC dialogue service (absent if not plugged in).
	LLM *npcai.Stats `json:"llm,omitempty"`
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	m := s.Game.Metrics()
	w.Header().Set("Content-Type", "application/json")
	out := Stats{
		Players:     m.Players,
		Monsters:    m.Monsters,
		Ticks:       m.Ticks,
		SlowTicks:   m.SlowTicks,
		TickTotalMS: float64(m.TickTotal) / float64(time.Millisecond),
		TickMaxMS:   float64(m.TickMax) / float64(time.Millisecond),

		TickP99MS:       float64(m.RecentP99) / float64(time.Millisecond),
		TickRecentMaxMS: float64(m.RecentMax) / float64(time.Millisecond),
	}
	// The dialogue service is only plugged in before Run: Talker does not change afterwards.
	if sv, ok := s.Game.Talker.(interface{ Stats() npcai.Stats }); ok {
		st := sv.Stats()
		out.LLM = &st
	}
	_ = json.NewEncoder(w).Encode(out)
}

type session struct {
	id   int
	name string
	lang i18n.Lang // client language, chosen at login
	conn *websocket.Conn
	srv  *Server

	// send carries the reliable messages (chat, combat, welcome, errors): all
	// must arrive, in order. A full queue signals a client that is too slow.
	send chan []byte

	// Snapshots are a state, not an event: only the latest counts.
	// Only one is kept, which overwrites the previous one if it has not yet been
	// sent. A momentarily slow client skips frames instead of being
	// disconnected, and the JSON encoding happens in its write goroutine, not
	// in the game loop.
	snapMu  sync.Mutex
	snap    *protocol.Snapshot
	snapSig chan struct{} // capacity 1: "a snapshot is pending"

	// Chat rate limiting (touched only by readLoop).
	tokens   float64
	lastChat time.Time

	// Same for the lines addressed to NPCs.
	sayTokens float64
	lastSay   time.Time
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.Log.Warn("accept ws", "err", err)
		return
	}
	conn.SetReadLimit(readLimit)
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sess, err := s.login(ctx, conn, clientIP(r))
	if err != nil {
		return
	}
	defer s.remove(sess)

	go sess.writeLoop(ctx, cancel)
	sess.readLoop(ctx)
}

// login waits for the login message, validates the name and registers the session.
func (s *Server) login(ctx context.Context, conn *websocket.Conn, ip string) (*session, error) {
	lctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	lang := i18n.EN // the client language is only known after reading the login
	fail := func(key string, args ...any) (*session, error) {
		msg := i18n.T(lang, key, args...)
		data, _ := protocol.Encode(protocol.TError, protocol.Error{Msg: msg})
		wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
		defer wcancel()
		_ = conn.Write(wctx, websocket.MessageText, data)
		conn.Close(websocket.StatusPolicyViolation, msg)
		return nil, context.Canceled
	}

	_, data, err := conn.Read(lctx)
	if err != nil {
		return nil, err
	}
	env, err := protocol.Decode(data)
	if err != nil || env.T != protocol.TLogin {
		return fail("srv.err.first_login")
	}
	var lg protocol.Login
	if err := env.Payload(&lg); err != nil {
		return fail("srv.err.bad_login")
	}
	lang = i18n.Lang(lg.Lang).Normalize()
	name := strings.TrimSpace(lg.Name)
	if !validName(name) {
		return fail("srv.err.bad_name")
	}
	if s.failedLogins.blocked(ip) {
		s.Log.Warn("too many login failures", "ip", ip)
		return fail("srv.err.too_many_attempts")
	}

	var canonical string
	if lg.Register {
		if l := len(lg.Password); l < protocol.MinPassLen || l > protocol.MaxPassLen {
			return fail("srv.err.pass_len", protocol.MinPassLen, protocol.MaxPassLen)
		}
		if s.registrations.blocked(ip) {
			s.Log.Warn("too many account creations", "ip", ip)
			return fail("srv.err.too_many_accounts")
		}
		canonical, err = s.be.CreateAccount(name, lg.Password)
		switch {
		case errors.Is(err, store.ErrExists):
			return fail("srv.err.name_taken")
		case errors.Is(err, store.ErrWeakPassword):
			return fail("srv.err.weak_password")
		case err != nil:
			s.Log.Error("account creation", "err", err)
			return fail("srv.err.server")
		}
		s.registrations.add(ip)
		s.Log.Info("account created", "name", canonical)
	} else {
		canonical, err = s.be.Authenticate(name, lg.Password)
		switch {
		case errors.Is(err, store.ErrInvalid):
			s.failedLogins.add(ip)
			return fail("srv.err.bad_credentials")
		case err != nil:
			s.Log.Error("authentication", "err", err)
			return fail("srv.err.server")
		}
	}
	name = canonical

	var saved *game.Saved
	ch, err := s.be.LoadCharacter(name)
	if err != nil {
		s.Log.Error("loading character", "name", name, "err", err)
		return fail("srv.err.server")
	}
	if needsCreation(ch) {
		if ch, err = s.runCreation(ctx, conn, name, lang, ch); err != nil {
			return nil, err
		}
	} else {
		// Character created before dice weapons: it gets its starting weapon if it has none.
		ensureStarter(ch, lang)
	}
	saved = toSaved(*ch)

	sess := &session{
		name:    name,
		conn:    conn,
		send:    make(chan []byte, sendBuffer),
		snapSig: make(chan struct{}, 1),
		srv:     s,
		lang:    lang,
	}

	// The name is reserved under lock, then the lock is released before Join:
	// Join waits for the game loop (up to the end of the current tick), and
	// holding it would serialize all connections behind that latency.
	key := strings.ToLower(name)
	s.mu.Lock()
	taken := s.reserved[key]
	for _, o := range s.sessions {
		if strings.EqualFold(o.name, name) {
			taken = true
		}
	}
	if taken {
		s.mu.Unlock()
		return fail("srv.err.already_online")
	}
	s.reserved[key] = true
	s.mu.Unlock()

	id, err := s.Game.Join(name, game.Handlers{
		State:    saved,
		Lang:     lang,
		Snapshot: sess.setSnapshot,
		Combat:   func(c protocol.Combat) { sess.sendMsg(protocol.TCombat, c) },
		NPC:      func(d protocol.NPCDialog) { sess.sendMsg(protocol.TNPC, d) },
		Quests:   func(q protocol.Quests) { sess.sendMsg(protocol.TQuests, q) },
		Bag:      func(b protocol.Bag) { sess.sendMsg(protocol.TBag, b) },
		Zone:     func(z protocol.Zone) { sess.sendMsg(protocol.TZone, z) },
		Dungeons: func(d protocol.Dungeons) { sess.sendMsg(protocol.TDungeons, d) },
		Nodes:    func(n protocol.Nodes) { sess.sendMsg(protocol.TNodes, n) },
		Joined: func(id, x, y int) {
			sess.sendMsg(protocol.TWelcome, protocol.Welcome{
				ID: id, Name: name, X: x, Y: y, Seed: s.Game.World.Seed, Gen: s.Game.World.GenVersion,
			})
		},
	})

	s.mu.Lock()
	delete(s.reserved, key)
	if err == nil {
		sess.id = id
		s.sessions[id] = sess
	}
	s.mu.Unlock()
	if err != nil {
		return fail("srv.err.unavailable")
	}

	s.Log.Info("player connected", "id", sess.id, "name", name)
	s.announce("srv.joined", name)
	return sess, nil
}

func (s *Server) remove(sess *session) {
	// Leave waits for the game loop to have processed the departure (and thus queued
	// the save) before releasing the name: an immediate reconnection
	// can thus never load a stale state.
	s.Game.Leave(sess.id)
	s.mu.Lock()
	delete(s.sessions, sess.id)
	s.mu.Unlock()
	s.Log.Info("player disconnected", "id", sess.id, "name", sess.name)
	s.announce("srv.left", sess.name)
}

// broadcast sends a message to all connected players.
func (s *Server) broadcast(t string, payload any) { s.broadcastWith(t, payload, false) }

// announce broadcasts a system announcement (arrival, departure). It is dropped for
// a client whose queue is loaded: these announcements grow with the
// square of the number of players during a connection wave.
//
// The text is written in each recipient's language: key and args are
// passed to i18n.T, and encoding is done only once per language.
func (s *Server) announce(key string, args ...any) {
	encoded := make(map[i18n.Lang][]byte, len(i18n.Langs))
	for _, l := range i18n.Langs {
		data, err := protocol.Encode(protocol.TChat, protocol.Chat{Channel: protocol.ChanSystem, Text: i18n.T(l, key, args...)})
		if err != nil {
			s.Log.Error("encode", "err", err)
			return
		}
		encoded[l] = data
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		sess.enqueueBestEffort(encoded[sess.lang])
	}
}

func (s *Server) broadcastWith(t string, payload any, bestEffort bool) {
	data, err := protocol.Encode(t, payload)
	if err != nil {
		s.Log.Error("encode", "err", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if bestEffort {
			sess.enqueueBestEffort(data)
		} else {
			sess.enqueue(data)
		}
	}
}

// enqueueBestEffort puts a non-essential message in the queue, unless it
// is already half full: better to lose an announcement than to disconnect a
// slow client, or to saturate everyone during a connection wave.
func (sess *session) enqueueBestEffort(data []byte) {
	if len(sess.send) > sendBuffer/2 {
		return
	}
	sess.enqueue(data)
}

// enqueue puts a message in the queue; if it is full (client too slow),
// the connection is closed rather than blocking the server.
func (sess *session) enqueue(data []byte) {
	select {
	case sess.send <- data:
	default:
		sess.srv.Log.Warn("send queue full, closing", "id", sess.id)
		go sess.conn.Close(websocket.StatusPolicyViolation, "client too slow")
	}
}

func (sess *session) sendMsg(t string, payload any) {
	data, err := protocol.Encode(t, payload)
	if err != nil {
		sess.srv.Log.Error("encode", "err", err)
		return
	}
	sess.enqueue(data)
}

// setSnapshot replaces the pending snapshot. Called from the game loop:
// never blocks. The snap slices are no longer modified by the game
// loop after this call, so they can be read from another goroutine.
func (sess *session) setSnapshot(snap protocol.Snapshot) {
	sess.snapMu.Lock()
	sess.snap = &snap
	sess.snapMu.Unlock()
	select {
	case sess.snapSig <- struct{}{}:
	default: // a signal is already pending
	}
}

func (sess *session) write(ctx context.Context, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return sess.conn.Write(wctx, websocket.MessageText, data)
}

func (sess *session) writeLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, pingTimeout)
			err := sess.conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case data := <-sess.send:
			if sess.write(ctx, data) != nil {
				return
			}
		case <-sess.snapSig:
			// Reliable messages already in the queue go first: the welcome
			// must precede the first snapshot.
			for drained := false; !drained; {
				select {
				case data := <-sess.send:
					if sess.write(ctx, data) != nil {
						return
					}
				default:
					drained = true
				}
			}
			sess.snapMu.Lock()
			snap := sess.snap
			sess.snap = nil
			sess.snapMu.Unlock()
			if snap == nil {
				continue
			}
			data, err := protocol.Encode(protocol.TSnapshot, snap)
			if err != nil {
				sess.srv.Log.Error("snapshot encoding", "err", err)
				continue
			}
			if sess.write(ctx, data) != nil {
				return
			}
		}
	}
}

func (sess *session) readLoop(ctx context.Context) {
	for {
		_, data, err := sess.conn.Read(ctx)
		if err != nil {
			return
		}
		env, err := protocol.Decode(data)
		if err != nil {
			sess.sendMsg(protocol.TError, protocol.Error{Msg: i18n.T(sess.lang, "srv.err.invalid_message", err.Error())})
			continue
		}
		switch env.T {
		case protocol.TMove:
			var m protocol.Move
			if err := env.Payload(&m); err != nil {
				continue
			}
			sess.srv.Game.Move(sess.id, m.DX, m.DY)
		case protocol.TCombatAction:
			var a protocol.CombatAction
			if err := env.Payload(&a); err != nil {
				continue
			}
			if _, ok := combat.ParseAction(a.Action); !ok {
				sess.sendErr("srv.err.unknown_combat_action", a.Action)
				continue
			}
			sess.srv.Game.Act(sess.id, a.Action, a.Target)
		case protocol.TNPCAction:
			var a protocol.NPCAction
			if err := env.Payload(&a); err != nil {
				continue
			}
			if !game.ValidNPCAction(a.Action) {
				sess.sendErr("srv.err.unknown_dialog_action", a.Action)
				continue
			}
			// The text comes from a client: cleaned and bounded here, before it reaches the game
			// or a language model (escape sequences, bidirectional marks...).
			text := textsafe.Clean(a.Text, protocol.MaxSayLen)
			if a.Action == protocol.NPCSay {
				if text == "" {
					continue
				}
				if !sess.allowSay() {
					sess.sendErr("srv.err.slow_down_npc")
					continue
				}
			}
			sess.srv.Game.TalkAct(sess.id, a.Action, text)
		case protocol.TQuestAction:
			var a protocol.QuestAction
			if err := env.Payload(&a); err != nil {
				continue
			}
			if a.Action != protocol.QuestAbandon {
				continue
			}
			if !sess.allowSay() {
				sess.sendErr("srv.err.slow_down_npc")
				continue
			}
			sess.srv.Game.QuestAct(sess.id, a.Action, a.ID)
		case protocol.TGearAction:
			var a protocol.GearAction
			if err := env.Payload(&a); err != nil {
				continue
			}
			if a.Action != protocol.GearEquip && a.Action != protocol.GearUnequip {
				continue
			}
			if !sess.allowSay() {
				sess.sendErr("srv.err.slow_down_npc")
				continue
			}
			sess.srv.Game.GearAct(sess.id, a.Action, a.ID, a.Slot)
		case protocol.TGather:
			if !sess.allowSay() {
				sess.sendErr("srv.err.slow_down_npc")
				continue
			}
			sess.srv.Game.Gather(sess.id)
		case protocol.TPing:
			sess.sendMsg(protocol.TPong, nil)
		case protocol.TChatSend:
			var c protocol.ChatSend
			if err := env.Payload(&c); err != nil {
				continue
			}
			text := strings.TrimSpace(c.Text)
			if text == "" {
				continue
			}
			if r := []rune(text); len(r) > protocol.MaxChatLen {
				text = string(r[:protocol.MaxChatLen])
			}
			sess.handleChat(text)
		default:
			sess.sendMsg(protocol.TError, protocol.Error{Msg: i18n.T(sess.lang, "srv.err.unknown_type", env.T)})
		}
	}
}

func validName(n string) bool {
	l := len([]rune(n))
	if l < protocol.MinNameLen || l > protocol.MaxNameLen {
		return false
	}
	for _, r := range n {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

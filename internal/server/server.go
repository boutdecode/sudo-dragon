// Package server gère le transport WebSocket, les sessions et la diffusion.
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

	// Détection des connexions mortes: sans cela, un client dont le réseau a
	// disparu garderait son compte « connecté » et bloquerait sa reconnexion.
	pingEvery   = 25 * time.Second
	pingTimeout = 10 * time.Second

	// Quotas par adresse IP.
	maxFailedLogins    = 8  // échecs d'authentification par minute
	maxRegistrations   = 10 // créations de compte par heure
	failedLoginWindow  = time.Minute
	registrationWindow = time.Hour
)

// Backend fournit les comptes et la persistance des personnages.
type Backend interface {
	// CreateAccount et Authenticate renvoient le pseudo tel qu'enregistré.
	CreateAccount(name, password string) (string, error)
	Authenticate(name, password string) (string, error)
	// LoadCharacter renvoie l'état le plus récent, ou nil pour un nouveau personnage.
	LoadCharacter(name string) (*store.Character, error)
	// SaveCharacter ne doit pas bloquer.
	SaveCharacter(name string, c store.Character)
}

// Server accepte les connexions WebSocket et tient la liste des joueurs connectés.
type Server struct {
	Game *game.Game
	Log  *slog.Logger

	be            Backend
	failedLogins  *limiter
	registrations *limiter

	mu       sync.Mutex
	sessions map[int]*session
	// reserved contient les pseudos (en minuscules) en cours de connexion: ils
	// sont pris le temps que la boucle de jeu traite Join, sans garder s.mu.
	reserved map[string]bool
}

// New crée un serveur pour le monde donné. Appeler Run pour démarrer la boucle de jeu.
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

// SetRegistrationLimit règle le nombre de comptes créables par heure et par
// adresse IP. Zéro ou moins: pas de limite (tests de charge, développement).
func (s *Server) SetRegistrationLimit(perHour int) {
	if perHour <= 0 {
		perHour = int(^uint(0) >> 1)
	}
	s.registrations = newLimiter(perHour, registrationWindow)
}

// fromSaved et toSaved convertissent entre l'état du jeu et l'état stocké.
func fromSaved(sv game.Saved) store.Character {
	c := store.Character{
		Level: sv.Stats.Level, XP: sv.Stats.XP, Potions: sv.Stats.Potions, Gold: sv.Stats.Gold,
		HP: sv.HP, X: sv.Pos.X, Y: sv.Pos.Y,
		LLMConsent: sv.LLMConsent, Kills: sv.Stats.Kills, BossKills: sv.Stats.BossKills, Deaths: sv.Stats.Deaths,
		Class: string(sv.Stats.Class), Scores: sv.Stats.Scores, MaxHP: sv.Stats.MaxHP, Mana: sv.Mana,
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
			WithGold(c.Gold).WithRecord(c.Kills, c.BossKills, c.Deaths),
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

// SetTalker branche le service qui fait parler les PNJ (nil: pas de discussion libre).
// À appeler avant Run.
func (s *Server) SetTalker(t game.Talker) { s.Game.Talker = t }

// Run exécute la boucle de jeu (bloquant) jusqu'à l'annulation de ctx.
func (s *Server) Run(ctx context.Context) { s.Game.Run(ctx) }

// Handler renvoie le handler HTTP: /ws (jeu), /healthz (sonde) et /stats (métriques).
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

// Stats est la forme JSON de /stats.
type Stats struct {
	Players     int     `json:"players"`
	Monsters    int     `json:"monsters"`
	Ticks       uint64  `json:"ticks"`
	SlowTicks   uint64  `json:"slow_ticks"`
	TickTotalMS float64 `json:"tick_total_ms"`
	TickMaxMS   float64 `json:"tick_max_ms"`
	// Régime établi: les 60 dernières secondes.
	TickP99MS       float64 `json:"tick_p99_ms"`
	TickRecentMaxMS float64 `json:"tick_recent_max_ms"`

	// LLM décrit le service de dialogue des PNJ (absent s'il n'est pas branché).
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
	// Le service de dialogue n'est branché qu'avant Run : Talker ne change plus ensuite.
	if sv, ok := s.Game.Talker.(interface{ Stats() npcai.Stats }); ok {
		st := sv.Stats()
		out.LLM = &st
	}
	_ = json.NewEncoder(w).Encode(out)
}

type session struct {
	id   int
	name string
	lang i18n.Lang // langue du client, choisie à la connexion
	conn *websocket.Conn
	srv  *Server

	// send porte les messages fiables (chat, combat, bienvenue, erreurs): tous
	// doivent arriver, dans l'ordre. Une file pleine signale un client trop lent.
	send chan []byte

	// Les instantanés sont un état, pas un événement: seul le dernier compte.
	// On en garde un seul, qui écrase le précédent s'il n'a pas encore été
	// envoyé. Un client momentanément lent saute des images au lieu d'être
	// déconnecté, et l'encodage JSON se fait dans sa goroutine d'écriture, pas
	// dans la boucle de jeu.
	snapMu  sync.Mutex
	snap    *protocol.Snapshot
	snapSig chan struct{} // capacité 1: « un instantané est en attente »

	// Limitation du débit de chat (touchés seulement par readLoop).
	tokens   float64
	lastChat time.Time

	// Idem pour les répliques adressées aux PNJ.
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

// login attend le message de connexion, valide le pseudo et enregistre la session.
func (s *Server) login(ctx context.Context, conn *websocket.Conn, ip string) (*session, error) {
	lctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	lang := i18n.FR // la langue du client n'est connue qu'après lecture du login
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
		s.Log.Warn("trop d'échecs de connexion", "ip", ip)
		return fail("srv.err.too_many_attempts")
	}

	var canonical string
	if lg.Register {
		if l := len(lg.Password); l < protocol.MinPassLen || l > protocol.MaxPassLen {
			return fail("srv.err.pass_len", protocol.MinPassLen, protocol.MaxPassLen)
		}
		if s.registrations.blocked(ip) {
			s.Log.Warn("trop de créations de compte", "ip", ip)
			return fail("srv.err.too_many_accounts")
		}
		canonical, err = s.be.CreateAccount(name, lg.Password)
		switch {
		case errors.Is(err, store.ErrExists):
			return fail("srv.err.name_taken")
		case errors.Is(err, store.ErrWeakPassword):
			return fail("srv.err.weak_password")
		case err != nil:
			s.Log.Error("création de compte", "err", err)
			return fail("srv.err.server")
		}
		s.registrations.add(ip)
		s.Log.Info("compte créé", "name", canonical)
	} else {
		canonical, err = s.be.Authenticate(name, lg.Password)
		switch {
		case errors.Is(err, store.ErrInvalid):
			s.failedLogins.add(ip)
			return fail("srv.err.bad_credentials")
		case err != nil:
			s.Log.Error("authentification", "err", err)
			return fail("srv.err.server")
		}
	}
	name = canonical

	var saved *game.Saved
	ch, err := s.be.LoadCharacter(name)
	if err != nil {
		s.Log.Error("chargement du personnage", "name", name, "err", err)
		return fail("srv.err.server")
	}
	if needsCreation(ch) {
		if ch, err = s.runCreation(ctx, conn, name, lang, ch); err != nil {
			return nil, err
		}
	} else {
		// Personnage créé avant les armes à dés: il reçoit son arme de départ s'il n'en a aucune.
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

	// On réserve le pseudo sous verrou, puis on relâche le verrou avant Join:
	// Join attend la boucle de jeu (jusqu'à la fin du tick en cours), et le
	// garder serait sérialiser toutes les connexions derrière cette latence.
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

	s.Log.Info("joueur connecté", "id", sess.id, "name", name)
	s.announce("srv.joined", name)
	return sess, nil
}

func (s *Server) remove(sess *session) {
	// Leave attend que la boucle de jeu ait traité le départ (et donc mis la
	// sauvegarde en file) avant de libérer le pseudo: une reconnexion immédiate
	// ne peut ainsi jamais charger un état périmé.
	s.Game.Leave(sess.id)
	s.mu.Lock()
	delete(s.sessions, sess.id)
	s.mu.Unlock()
	s.Log.Info("joueur déconnecté", "id", sess.id, "name", sess.name)
	s.announce("srv.left", sess.name)
}

// broadcast envoie un message à tous les joueurs connectés.
func (s *Server) broadcast(t string, payload any) { s.broadcastWith(t, payload, false) }

// announce diffuse une annonce système (arrivée, départ). Elle est perdue pour
// un client dont la file est chargée: ces annonces sont proportionnelles au
// nombre de joueurs au carré lors d'une vague de connexions.
//
// Le texte est rédigé dans la langue de chaque destinataire: key et args sont
// passés à i18n.T, et l'encodage n'est fait qu'une fois par langue.
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

// enqueueBestEffort place un message non essentiel dans la file, sauf si elle
// est déjà à moitié pleine: mieux vaut perdre une annonce que déconnecter un
// client lent, ou saturer tout le monde lors d'une vague de connexions.
func (sess *session) enqueueBestEffort(data []byte) {
	if len(sess.send) > sendBuffer/2 {
		return
	}
	sess.enqueue(data)
}

// enqueue place un message dans la file; si elle est pleine (client trop lent),
// la connexion est fermée plutôt que de bloquer le serveur.
func (sess *session) enqueue(data []byte) {
	select {
	case sess.send <- data:
	default:
		sess.srv.Log.Warn("file d'envoi pleine, fermeture", "id", sess.id)
		go sess.conn.Close(websocket.StatusPolicyViolation, "client trop lent")
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

// setSnapshot remplace l'instantané en attente. Appelé depuis la boucle de
// jeu: ne bloque jamais. Les tranches de snap ne sont plus modifiées par la
// boucle de jeu après cet appel, on peut donc les lire depuis une autre goroutine.
func (sess *session) setSnapshot(snap protocol.Snapshot) {
	sess.snapMu.Lock()
	sess.snap = &snap
	sess.snapMu.Unlock()
	select {
	case sess.snapSig <- struct{}{}:
	default: // un signal est déjà en attente
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
			// Les messages fiables déjà en file passent d'abord: la bienvenue
			// doit précéder le premier instantané.
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
				sess.srv.Log.Error("encodage de l'instantané", "err", err)
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
			// Le texte vient d'un client: nettoyé et borné ici, avant qu'il n'atteigne le jeu
			// ou un modèle de langage (séquences d'échappement, marques bidirectionnelles…).
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

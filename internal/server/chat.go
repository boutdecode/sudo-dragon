package server

import (
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
)

// Chat rate limiting: burst of 5 messages, then 1 message per second.
const (
	chatBurst  = 5.0
	chatRefill = 1.0 // tokens per second
)

// allowChat applies the token bucket. Called only from the session's
// read goroutine, hence lock-free.
func (sess *session) allowChat() bool {
	now := time.Now()
	if sess.lastChat.IsZero() {
		sess.tokens = chatBurst
	} else {
		sess.tokens = min(chatBurst, sess.tokens+now.Sub(sess.lastChat).Seconds()*chatRefill)
	}
	sess.lastChat = now
	if sess.tokens < 1 {
		return false
	}
	sess.tokens--
	return true
}

// sendErr sends an error written in the player's language (key: see i18n).
func (sess *session) sendErr(key string, args ...any) {
	sess.sendMsg(protocol.TError, protocol.Error{Msg: i18n.T(sess.lang, key, args...)})
}

func (sess *session) sendSystem(key string, args ...any) {
	sess.sendMsg(protocol.TChat, protocol.Chat{Channel: protocol.ChanSystem, Text: i18n.T(sess.lang, key, args...)})
}

// handleChat interprets a chat text (already cleaned and truncated).
func (sess *session) handleChat(text string) {
	if !sess.allowChat() {
		sess.sendErr("srv.err.chat_rate")
		return
	}
	if !strings.HasPrefix(text, "/") {
		sess.sayNearby(protocol.ChanLocal, text)
		return
	}

	cmd, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(cmd) {
	case "/g", "/global":
		if rest == "" {
			sess.sendErr("srv.err.usage_g")
			return
		}
		sess.srv.broadcast(protocol.TChat, protocol.Chat{Channel: protocol.ChanGlobal, From: sess.name, Text: rest})
	case "/me":
		if rest == "" {
			sess.sendErr("srv.err.usage_me")
			return
		}
		sess.sayNearby(protocol.ChanEmote, rest)
	case "/w", "/msg", "/whisper":
		target, msg, _ := strings.Cut(rest, " ")
		msg = strings.TrimSpace(msg)
		if target == "" || msg == "" {
			sess.sendErr("srv.err.usage_w")
			return
		}
		sess.whisper(target, msg)
	case "/menhir":
		text, err := sess.srv.Game.Menhir(sess.id, rest)
		if err == nil {
			for _, line := range strings.Split(text, "\n") {
				if line != "" {
					sess.sendMsg(protocol.TChat, protocol.Chat{Channel: protocol.ChanSystem, Text: line})
				}
			}
		}
	case "/help", "/?":
		sess.sendSystem("srv.help")
	default:
		sess.sendErr("srv.err.unknown_command", cmd)
	}
}

// sayNearby broadcasts to the players in the view area (sender included).
func (sess *session) sayNearby(channel, text string) {
	s := sess.srv
	ids, err := s.Game.Nearby(sess.id)
	if err != nil || len(ids) == 0 {
		ids = []int{sess.id} // game stopped or player already gone: at least the echo
	}
	data, encErr := protocol.Encode(protocol.TChat, protocol.Chat{Channel: channel, From: sess.name, Text: text})
	if encErr != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if o, ok := s.sessions[id]; ok {
			o.enqueue(data)
		}
	}
}

// whisper sends a private message to the recipient and echoes it back to the sender.
func (sess *session) whisper(targetName, text string) {
	s := sess.srv
	var target *session
	s.mu.Lock()
	for _, o := range s.sessions {
		if strings.EqualFold(o.name, targetName) {
			target = o
			break
		}
	}
	s.mu.Unlock()

	switch {
	case target == nil:
		sess.sendErr("srv.err.player_not_found", targetName)
	case target == sess:
		sess.sendErr("srv.err.whisper_self")
	default:
		msg := protocol.Chat{Channel: protocol.ChanWhisper, From: sess.name, To: target.name, Text: text}
		target.sendMsg(protocol.TChat, msg)
		sess.sendMsg(protocol.TChat, msg)
	}
}

// Rate limiting of lines to NPCs: burst of 3, then one every 2 seconds. The
// conversation service applies its own per-player limits; this one only protects
// the game loop from a client that would flood the connection.
const (
	sayBurst  = 3.0
	sayRefill = 0.5 // tokens per second
)

// allowSay applies the token bucket of lines to NPCs (read goroutine only).
func (sess *session) allowSay() bool {
	now := time.Now()
	if sess.lastSay.IsZero() {
		sess.sayTokens = sayBurst
	} else {
		sess.sayTokens = min(sayBurst, sess.sayTokens+now.Sub(sess.lastSay).Seconds()*sayRefill)
	}
	sess.lastSay = now
	if sess.sayTokens < 1 {
		return false
	}
	sess.sayTokens--
	return true
}

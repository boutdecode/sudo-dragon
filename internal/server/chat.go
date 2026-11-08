package server

import (
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

// Limitation du débit de chat: rafale de 5 messages, puis 1 message par seconde.
const (
	chatBurst  = 5.0
	chatRefill = 1.0 // jetons par seconde
)

const helpText = "Commandes: /g message (tous) · /me action · /w pseudo message (privé) · /help. " +
	"Sans commande, le message est local (joueurs proches)."

// allowChat applique le seau à jetons. Appelée uniquement depuis la goroutine
// de lecture de la session, donc sans verrou.
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

func (sess *session) sendErr(msg string) {
	sess.sendMsg(protocol.TError, protocol.Error{Msg: msg})
}

func (sess *session) sendSystem(text string) {
	sess.sendMsg(protocol.TChat, protocol.Chat{Channel: protocol.ChanSystem, Text: text})
}

// handleChat interprète un texte de chat (déjà nettoyé et tronqué).
func (sess *session) handleChat(text string) {
	if !sess.allowChat() {
		sess.sendErr("trop de messages, ralentissez")
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
			sess.sendErr("usage: /g message")
			return
		}
		sess.srv.broadcast(protocol.TChat, protocol.Chat{Channel: protocol.ChanGlobal, From: sess.name, Text: rest})
	case "/me":
		if rest == "" {
			sess.sendErr("usage: /me action")
			return
		}
		sess.sayNearby(protocol.ChanEmote, rest)
	case "/w", "/msg", "/whisper":
		target, msg, _ := strings.Cut(rest, " ")
		msg = strings.TrimSpace(msg)
		if target == "" || msg == "" {
			sess.sendErr("usage: /w pseudo message")
			return
		}
		sess.whisper(target, msg)
	case "/help", "/?":
		sess.sendSystem(helpText)
	default:
		sess.sendErr("commande inconnue: " + cmd + " (essayez /help)")
	}
}

// sayNearby diffuse aux joueurs de la zone de vision (émetteur inclus).
func (sess *session) sayNearby(channel, text string) {
	s := sess.srv
	ids, err := s.Game.Nearby(sess.id)
	if err != nil || len(ids) == 0 {
		ids = []int{sess.id} // jeu arrêté ou joueur déjà parti: au moins l'écho
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

// whisper envoie un message privé au destinataire et en renvoie l'écho à l'émetteur.
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
		sess.sendErr("joueur introuvable: " + targetName)
	case target == sess:
		sess.sendErr("vous ne pouvez pas vous chuchoter à vous-même")
	default:
		msg := protocol.Chat{Channel: protocol.ChanWhisper, From: sess.name, To: target.name, Text: text}
		target.sendMsg(protocol.TChat, msg)
		sess.sendMsg(protocol.TChat, msg)
	}
}

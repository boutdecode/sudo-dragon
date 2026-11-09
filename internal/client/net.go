// Package client contient le client TUI (Bubble Tea) et sa connexion réseau.
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
)

const (
	dialTimeout  = 15 * time.Second // inclut l'attente d'un créneau de hachage côté serveur
	writeTimeout = 5 * time.Second
	readLimit    = 1 << 20
	outBuffer    = 64
)

// Event est un message reçu du serveur, ou la fin de la connexion (Err != nil).
type Event struct {
	Env protocol.Envelope
	Err error
}

// Conn est une connexion établie et authentifiée auprès du serveur.
type Conn struct {
	// In reçoit les messages du serveur. Après un Event avec Err != nil,
	// plus rien n'est envoyé.
	In chan Event

	ws     *websocket.Conn
	out    chan []byte
	cancel context.CancelFunc
}

// Dial se connecte à url, s'authentifie (ou crée le compte si register) et
// attend le welcome. Le mot de passe voyage en clair dans la trame: préférer
// une adresse wss://.
func Dial(ctx context.Context, url, name, password string, register bool) (*Conn, protocol.Welcome, error) {
	return DialLang(ctx, url, name, password, register, i18n.FR)
}

// DialLang est Dial pour un client qui parle la langue lang: le serveur y rédige
// ses messages (erreurs, journal de combat, dialogues des PNJ).
//
// Si le compte doit créer son personnage, DialLang valide le premier tirage en guerrier (bots, outils
// et tests) ; l'interface passe par DialCreate pour laisser le joueur choisir.
func DialLang(ctx context.Context, url, name, password string, register bool, lang i18n.Lang) (*Conn, protocol.Welcome, error) {
	c, w, _, err := dial(ctx, url, name, password, register, lang, true)
	return c, w, err
}

// DialCreate est DialLang pour l'interface : quand le personnage reste à créer, la connexion est
// renvoyée avec le tirage proposé (create non nil) et sans welcome ; les actions de création partent
// par Send (protocol.TCreateAction) et TWelcome arrive ensuite dans In.
func DialCreate(ctx context.Context, url, name, password string, register bool, lang i18n.Lang) (c *Conn, w protocol.Welcome, create *protocol.Create, err error) {
	return dial(ctx, url, name, password, register, lang, false)
}

func dial(ctx context.Context, url, name, password string, register bool, lang i18n.Lang, auto bool) (*Conn, protocol.Welcome, *protocol.Create, error) {
	lang = lang.Normalize()
	var w protocol.Welcome
	var create *protocol.Create

	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	defer dcancel()

	ws, _, err := websocket.Dial(dctx, url, nil)
	if err != nil {
		return nil, w, nil, fmt.Errorf("%s", i18n.T(lang, "net.cannot_connect", err))
	}
	ws.SetReadLimit(readLimit)
	fail := func(err error) (*Conn, protocol.Welcome, *protocol.Create, error) {
		ws.CloseNow()
		return nil, w, nil, err
	}

	data, err := protocol.Encode(protocol.TLogin, protocol.Login{Name: name, Password: password, Register: register, Lang: string(lang)})
	if err != nil {
		return fail(err)
	}
	if err := ws.Write(dctx, websocket.MessageText, data); err != nil {
		return fail(err)
	}
	_, data, err = ws.Read(dctx)
	if err != nil {
		return fail(fmt.Errorf("%s", i18n.T(lang, "net.no_reply", err)))
	}
	env, err := protocol.Decode(data)
	if err != nil {
		return fail(err)
	}
	if env.T == protocol.TCreate && auto {
		reply, err := protocol.Encode(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "warrior"})
		if err != nil {
			return fail(err)
		}
		if err := ws.Write(dctx, websocket.MessageText, reply); err != nil {
			return fail(err)
		}
		if _, data, err = ws.Read(dctx); err != nil {
			return fail(fmt.Errorf("%s", i18n.T(lang, "net.no_reply", err)))
		}
		if env, err = protocol.Decode(data); err != nil {
			return fail(err)
		}
	}
	switch env.T {
	case protocol.TCreate:
		var cr protocol.Create
		if err := env.Payload(&cr); err != nil {
			return fail(err)
		}
		create = &cr
	case protocol.TWelcome:
		if err := env.Payload(&w); err != nil {
			return fail(err)
		}
	case protocol.TError:
		var e protocol.Error
		_ = env.Payload(&e)
		return fail(errors.New(e.Msg))
	default:
		return fail(fmt.Errorf("%s", i18n.T(lang, "net.unexpected", env.T)))
	}

	cctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		In:     make(chan Event, 256),
		ws:     ws,
		out:    make(chan []byte, outBuffer),
		cancel: cancel,
	}
	go c.readLoop(cctx)
	go c.writeLoop(cctx)
	return c, w, create, nil
}

func (c *Conn) emit(ctx context.Context, ev Event) bool {
	select {
	case c.In <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c *Conn) readLoop(ctx context.Context) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			c.emit(ctx, Event{Err: err})
			return
		}
		env, err := protocol.Decode(data)
		if err != nil {
			continue
		}
		if !c.emit(ctx, Event{Env: env}) {
			return
		}
	}
}

func (c *Conn) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.ws.CloseNow()
				return
			}
		}
	}
}

// Send met un message en file d'envoi sans bloquer. Renvoie false si la file
// est pleine (le message est alors abandonné).
func (c *Conn) Send(typ string, payload any) bool {
	data, err := protocol.Encode(typ, payload)
	if err != nil {
		return false
	}
	select {
	case c.out <- data:
		return true
	default:
		return false
	}
}

// Close ferme la connexion proprement (trame de fermeture), sans attendre le
// serveur plus d'une demi-seconde: on quitte le jeu, pas question de bloquer.
func (c *Conn) Close() {
	c.cancel()
	done := make(chan struct{})
	go func() {
		_ = c.ws.Close(websocket.StatusNormalClosure, "bye")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		c.ws.CloseNow()
	}
}

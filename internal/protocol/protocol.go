// Package protocol définit les messages échangés entre client et serveur.
//
// Chaque message est une enveloppe JSON {"t": <type>, "d": <charge utile>}
// envoyée dans une trame WebSocket texte.
package protocol

import (
	"encoding/json"
	"fmt"
)

// Types de messages.
const (
	// Client -> serveur
	TLogin    = "login"
	TChatSend = "chat_send"
	TMove     = "move"
	// TCombatAction: voir CombatAction.
	TCombatAction = "combat_action"
	// TNPCAction: voir NPCAction.
	TNPCAction = "npc_action"
	// TPing: mesure de latence, le serveur répond TPong (sans charge utile).
	TPing = "ping"

	// Serveur -> client
	TWelcome  = "welcome"
	TError    = "error"
	TChat     = "chat"
	TSnapshot = "snapshot"
	TCombat   = "combat"
	TNPC      = "npc"
	TPong     = "pong"
)

// Limites partagées.
const (
	MaxNameLen = 16
	MinNameLen = 3
	MaxChatLen = 200

	// Longueur du mot de passe en octets: bcrypt ignore tout au-delà de 72.
	MinPassLen = 8
	MaxPassLen = 72

	// Zone de vision (demi-largeur / demi-hauteur, en tuiles) autour d'un joueur.
	// Le serveur n'envoie que les entités dans ce rectangle; le client dessine
	// le terrain lui-même à partir de la seed.
	ViewHalfW = 40
	ViewHalfH = 20
)

// Move est une intention de déplacement d'une tuile. dx et dy valent -1, 0 ou 1.
type Move struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// Entity est un joueur visible dans un instantané.
type Entity struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Fighting bool   `json:"fight,omitempty"` // engagé dans un combat
}

// Monster est un monstre visible dans un instantané. Kind est l'identifiant de
// l'espèce ("rat", "wolf"…); le client en déduit l'apparence.
type Monster struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Fighting bool   `json:"fight,omitempty"`
}

// Stats est la fiche du joueur local, envoyée dans chaque instantané.
type Stats struct {
	Level   int `json:"lvl"`
	XP      int `json:"xp"`
	XPNext  int `json:"xpn"`
	HP      int `json:"hp"`
	MaxHP   int `json:"mhp"`
	Atk     int `json:"atk"`
	Def     int `json:"def"`
	Potions int `json:"pot"`
	Gold    int `json:"gold"`
}

// Snapshot décrit ce qu'un joueur voit à un tick donné. Il n'est envoyé que
// si son contenu a changé depuis le dernier envoi.
type Snapshot struct {
	Tick     uint64    `json:"tick"`
	Self     Entity    `json:"self"`
	Others   []Entity  `json:"others,omitempty"`
	Monsters []Monster `json:"monsters,omitempty"`
	Stats    Stats     `json:"stats"`
	Online   int       `json:"on,omitempty"` // joueurs connectés
}

// CombatAction est le choix du joueur pour son tour de combat:
// "attack", "defend", "item" ou "flee".
type CombatAction struct {
	Action string `json:"action"`
}

// Issues d'un combat (Combat.Result).
const (
	ResultWon  = "won"
	ResultLost = "lost"
	ResultFled = "fled"
)

// Combat est l'état d'un combat, envoyé à son début puis après chaque tour.
// Log contient les événements du dernier tour. Quand Over est vrai, Result
// donne l'issue et le joueur est déjà libéré dans le monde.
type Combat struct {
	Round        int      `json:"round"`
	Monster      string   `json:"monster"` // espèce
	MonsterHP    int      `json:"mhp"`
	MonsterMaxHP int      `json:"mmhp"`
	PlayerHP     int      `json:"php"`
	PlayerMaxHP  int      `json:"pmhp"`
	Potions      int      `json:"pot"`
	Log          []string `json:"log,omitempty"`
	Over         bool     `json:"over,omitempty"`
	Result       string   `json:"result,omitempty"`
}

// Actions de dialogue avec un PNJ (NPCAction.Action).
const (
	NPCBuy   = "buy"   // acheter une potion (marchand)
	NPCSell  = "sell"  // vendre une potion (marchand)
	NPCHeal  = "heal"  // se faire soigner (soigneur)
	NPCLeave = "leave" // mettre fin à la conversation
)

// NPCAction est le choix du joueur pendant un dialogue avec un PNJ.
type NPCAction struct {
	Action string `json:"action"`
}

// NPCOffer est une action proposée par un PNJ. Le serveur décide du prix et de
// la disponibilité (or insuffisant, sac plein…): le client se contente de l'afficher.
type NPCOffer struct {
	Action    string `json:"action"`
	Label     string `json:"label"`
	Price     int    `json:"price"`          // 0 = gratuit (ou, pour une vente, sans objet)
	Gain      bool   `json:"gain,omitempty"` // vrai si le joueur reçoit de l'or (vente) au lieu d'en payer
	Available bool   `json:"ok"`
}

// NPCDialog est l'état d'un dialogue, envoyé à son ouverture puis après chaque
// action. Quand Over est vrai, le dialogue est terminé et le joueur est libre.
type NPCDialog struct {
	Kind    string     `json:"kind"` // "merchant" ou "healer"
	Gold    int        `json:"gold"`
	Potions int        `json:"pot"`
	HP      int        `json:"hp"`
	MaxHP   int        `json:"mhp"`
	Offers  []NPCOffer `json:"offers,omitempty"`
	Log     []string   `json:"log,omitempty"`
	Over    bool       `json:"over,omitempty"`
}

// Envelope est l'enveloppe commune à tous les messages.
type Envelope struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

// Login est le premier message envoyé par un client. Avec Register, le
// serveur crée le compte (le pseudo ne doit pas exister); sinon il
// authentifie un compte existant.
type Login struct {
	Name     string `json:"name"`
	Password string `json:"pass"`
	Register bool   `json:"register,omitempty"`
}

// Welcome confirme la connexion et donne l'état initial du joueur.
type Welcome struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Seed int64  `json:"seed"`
}

// Error signale un refus ou une erreur au client.
type Error struct {
	Msg string `json:"msg"`
}

// ChatSend est un message de chat émis par un client.
type ChatSend struct {
	Text string `json:"text"`
}

// Canaux de chat.
const (
	ChanLocal   = "local"   // joueurs dans la zone de vision de l'émetteur
	ChanGlobal  = "global"  // tous les joueurs
	ChanEmote   = "emote"   // /me, comme local
	ChanWhisper = "whisper" // message privé
	ChanSystem  = "system"  // annonces du serveur
)

// Chat est un message de chat diffusé par le serveur.
// From est vide pour un message système. To n'est renseigné que pour un
// message privé (reçu par le destinataire, et renvoyé à l'émetteur).
type Chat struct {
	Channel string `json:"ch,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Text    string `json:"text"`
}

// Encode sérialise une charge utile dans une enveloppe. Les charges qui savent
// s'encoder seules (instantanés, voir fastjson.go) évitent la réflexion.
func Encode(t string, payload any) ([]byte, error) {
	if a, ok := payload.(appender); ok {
		size := 512
		if h, ok := payload.(sizeHinter); ok {
			size = h.SizeHint() + 64
		}
		b := make([]byte, 0, size)
		b = append(b, `{"t":`...)
		b = appendString(b, t)
		b = append(b, `,"d":`...)
		b = a.AppendJSON(b)
		return append(b, '}'), nil
	}
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(Envelope{T: t, D: raw})
}

// Decode lit une enveloppe depuis des octets.
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("message invalide: %w", err)
	}
	if e.T == "" {
		return e, fmt.Errorf("message sans type")
	}
	return e, nil
}

// Payload désérialise la charge utile de l'enveloppe dans v.
func (e Envelope) Payload(v any) error {
	if len(e.D) == 0 {
		return fmt.Errorf("charge utile absente pour %q", e.T)
	}
	return json.Unmarshal(e.D, v)
}

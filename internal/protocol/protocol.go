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
	// TQuestAction: voir QuestAction.
	TQuestAction = "quest_action"
	// TGearAction: voir GearAction.
	TGearAction = "gear_action"
	// TCreateAction: voir CreateAction (phase de création du personnage, avant TWelcome).
	TCreateAction = "create_action"
	// TPing: mesure de latence, le serveur répond TPong (sans charge utile).
	TPing = "ping"

	// Serveur -> client
	// TCreate: voir Create, envoyé à la place de TWelcome tant que le personnage n'est pas créé.
	TCreate   = "create"
	TWelcome  = "welcome"
	TError    = "error"
	TChat     = "chat"
	TSnapshot = "snapshot"
	TCombat   = "combat"
	TNPC      = "npc"
	TQuests   = "quests" // livre de quêtes: voir Quests
	TBag      = "bag"    // sac d'équipement: voir Bag
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
	Mana    int `json:"mp"`
	MaxMana int `json:"mmp"`
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
	PlayerMana   int      `json:"pmp"`
	PlayerMaxMP  int      `json:"pmmp"`
	Style        string   `json:"style,omitempty"` // style de l'arme du joueur: melee, ranged ou magic
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

	// Conversation libre avec un PNJ (modèle de langage).
	NPCTalk    = "talk"    // ouvrir la discussion
	NPCSay     = "say"     // dire quelque chose (NPCAction.Text)
	NPCConsent = "consent" // accepter l'envoi de ses messages à un service de langage tiers
	NPCDecline = "decline" // refuser
	NPCBack    = "back"    // quitter la discussion et revenir au menu du PNJ

	// Quêtes. NPCTurnIn rend une quête prête à son donneur (NPCAction.Text = identifiant).
	NPCQuestAccept = "quest_accept" // accepter la quête proposée
	NPCQuestRefuse = "quest_refuse" // la refuser
	NPCTurnIn      = "quest_turnin"

	// Équipement. NPCBuyItem et NPCSellItem portent l'objet dans NPCAction.Text (voir NPCOffer.Arg).
	NPCBuyItem  = "buy_item"
	NPCSellItem = "sell_item"
	NPCSellMenu = "sell_menu" // ouvre la liste des objets du sac à revendre
)

// MaxSayLen est la longueur maximale (en caractères) d'une réplique de joueur.
const MaxSayLen = 200

// NPCAction est le choix du joueur pendant un dialogue avec un PNJ. Text n'est
// utilisé que par NPCSay.
type NPCAction struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
}

// Modes d'un dialogue (NPCDialog.Mode).
const (
	NPCModeMenu   = "menu"   // menu du métier du PNJ (acheter, vendre, soigner…)
	NPCModeNotice = "notice" // avis de confidentialité à accepter avant de discuter
	NPCModeChat   = "chat"   // conversation libre
	NPCModeQuest  = "quest"  // proposition de quête à accepter ou refuser
	NPCModeSell   = "sell"   // objets du sac à revendre à un vendeur d'équipement
)

// NPCLine est une ligne de la transcription d'une discussion.
type NPCLine struct {
	Who  string `json:"who"` // "npc", "you" ou "sys"
	Text string `json:"text"`
}

// NPCOffer est une action proposée par un PNJ. Le serveur décide du prix et de
// la disponibilité (or insuffisant, sac plein…): le client se contente de l'afficher.
type NPCOffer struct {
	Action    string `json:"action"`
	Label     string `json:"label"`
	Price     int    `json:"price"`          // 0 = gratuit (ou, pour une vente, sans objet)
	Gain      bool   `json:"gain,omitempty"` // vrai si le joueur reçoit de l'or (vente) au lieu d'en payer
	Available bool   `json:"ok"`
	Arg       string `json:"arg,omitempty"`    // à renvoyer dans NPCAction.Text (quête à rendre, objet à acheter ou vendre)
	Detail    string `json:"detail,omitempty"` // précision affichée à côté (bonus, niveau requis…)
}

// ItemInfo est un objet d'équipement tel que le joueur le voit.
type ItemInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Desc     string `json:"desc,omitempty"`
	Slot     string `json:"slot"`           // weapon, armor, shield, helm
	Kind     string `json:"kind,omitempty"` // style d'une arme: melee, ranged, magic
	Tier     int    `json:"tier"`
	Atk      int    `json:"atk,omitempty"`
	Def      int    `json:"def,omitempty"`
	MinLevel int    `json:"lvl"`
	Sell     int    `json:"sell"` // prix de revente
	Worn     bool   `json:"worn,omitempty"`
	Origin   string `json:"origin,omitempty"` // nom du vendeur d'origine
}

// Create est le tirage proposé pendant la création du personnage: le serveur lance les dés, le client
// affiche. Il est renvoyé après chaque relance. Le client répond par des CreateAction ; TWelcome
// clôt la création.
type Create struct {
	Scores  []int       `json:"scores"` // FOR AGI INT CON SAG CHA
	Left    int         `json:"left"`   // relances restantes (3 au total)
	Level   int         `json:"lvl"`    // niveau conservé d'un personnage existant (1 pour un nouveau)
	Classes []ClassInfo `json:"classes"`
}

// ClassInfo décrit une base de personnage.
type ClassInfo struct {
	ID   string `json:"id"` // warrior, rogue, mage
	Die  int    `json:"die"`
	Mana int    `json:"mana"` // mana de départ avant le modificateur de SAG
}

// Actions de CreateAction.Action.
const (
	CreateReroll  = "reroll"  // relancer la caractéristique Index (0 à 5)
	CreateConfirm = "confirm" // valider le tirage avec la classe Class
)

// CreateAction est une décision du joueur pendant la création.
type CreateAction struct {
	Action string `json:"action"`
	Index  int    `json:"index,omitempty"`
	Class  string `json:"class,omitempty"`
}

// Bag est le sac d'équipement, envoyé à la connexion et à chaque changement.
type Bag struct {
	Notice   []string   `json:"notice,omitempty"` // lignes à ajouter au journal
	Items    []ItemInfo `json:"items"`            // tout le sac, objets portés compris (Worn)
	Capacity int        `json:"cap"`
	AtkBonus int        `json:"atk"` // bonus total de l'équipement porté
	DefBonus int        `json:"def"`

	// Fiche de création (voir internal/hero) : base, six caractéristiques (FOR AGI INT CON SAG CHA),
	// style de l'arme portée et dé de vie.
	Class  string `json:"class,omitempty"`
	Scores []int  `json:"scores,omitempty"`
	Style  string `json:"style,omitempty"`
	HitDie int    `json:"die,omitempty"`
}

// GearAction est une action sur le sac : équiper un objet (ID) ou retirer l'objet d'un emplacement (Slot).
type GearAction struct {
	Action string `json:"action"` // "equip" ou "unequip"
	ID     int64  `json:"id,omitempty"`
	Slot   string `json:"slot,omitempty"`
}

// Actions du sac.
const (
	GearEquip   = "equip"
	GearUnequip = "unequip"
)

// QuestInfo est une quête telle que le joueur la voit. Tout est calculé par le serveur.
type QuestInfo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Story    string `json:"story"`
	Giver    string `json:"giver"`    // nom du PNJ
	Place    string `json:"place"`    // nom du lieu du donneur
	Goal     string `json:"goal"`     // objectif en toutes lettres (« Vaincre 5 loups »)
	Progress int    `json:"progress"` // actions accomplies
	Need     int    `json:"need"`
	Gold     int    `json:"gold"`
	XP       int    `json:"xp"`
	Potions  int    `json:"potions"`
	Note     string `json:"note,omitempty"`
	State    string `json:"state"` // "active", "ready" ou "done"
	Gain     int    `json:"gain"`  // points de réputation à la remise

	// Clé du PNJ donneur (région et rang dans le lieu) : le client y repère, sur la carte,
	// le PNJ à qui rendre une quête prête.
	GiverRX  int `json:"grx"`
	GiverRY  int `json:"gry"`
	GiverIdx int `json:"gi"`
}

// Standing est la réputation du joueur dans un lieu.
type Standing struct {
	Place  string `json:"place"`
	Points int    `json:"points"`
	Tier   string `json:"tier"`
	Next   int    `json:"next,omitempty"` // points manquants pour le palier suivant
}

// Quests est le livre de quêtes, envoyé à la connexion et à chaque changement.
type Quests struct {
	Open     []QuestInfo `json:"open"`    // actives et prêtes
	History  []QuestInfo `json:"history"` // dernières quêtes rendues
	Standing []Standing  `json:"standing"`
	Notice   []string    `json:"notice,omitempty"` // lignes à ajouter au journal (progression, abandon…)
}

// QuestAction est une action sur le livre de quêtes. Aujourd'hui: abandonner.
type QuestAction struct {
	Action string `json:"action"` // "abandon"
	ID     int64  `json:"id"`
}

// Actions du livre de quêtes.
const QuestAbandon = "abandon"

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

	// Identité du PNJ (déterministe: le client la connaît aussi) et état de la discussion.
	Name     string    `json:"name,omitempty"`
	Place    string    `json:"place,omitempty"`
	Mode     string    `json:"mode,omitempty"`     // NPCModeMenu par défaut
	Thinking bool      `json:"thinking,omitempty"` // le PNJ prépare sa réponse
	Notice   string    `json:"notice,omitempty"`   // texte de l'avis (mode notice)
	Lines    []NPCLine `json:"lines,omitempty"`    // transcription (mode chat)

	Proposal *QuestInfo `json:"proposal,omitempty"` // quête proposée (mode quest)

	Specialty string `json:"specialty,omitempty"` // spécialité d'un vendeur d'équipement
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
	// Lang est la langue de l'interface du client ("fr" ou "en"): le serveur y
	// rédige les messages qu'il lui adresse. Absente (ancien client), c'est le français.
	Lang string `json:"lang,omitempty"`
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

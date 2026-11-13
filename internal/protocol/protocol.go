// Package protocol defines the messages exchanged between client and server.
//
// Each message is a JSON envelope {"t": <type>, "d": <payload>}
// sent in a text WebSocket frame.
package protocol

import (
	"encoding/json"
	"fmt"
)

// Message types.
const (
	// Client -> server
	TLogin    = "login"
	TChatSend = "chat_send"
	TMove     = "move"
	// TCombatAction: see CombatAction.
	TCombatAction = "combat_action"
	// TNPCAction: see NPCAction.
	TNPCAction = "npc_action"
	// TQuestAction: see QuestAction.
	TQuestAction = "quest_action"
	// TGearAction: see GearAction.
	TGearAction = "gear_action"
	// TCreateAction: see CreateAction (character creation phase, before TWelcome).
	TCreateAction = "create_action"
	// TGather (no payload): the player gathers the resource node under or next to them.
	TGather = "gather"
	// TPing: latency measurement, the server answers TPong (no payload).
	TPing = "ping"

	// Server -> client
	// TCreate: see Create, sent instead of TWelcome until the character is created.
	TCreate   = "create"
	TWelcome  = "welcome"
	TError    = "error"
	TChat     = "chat"
	TSnapshot = "snapshot"
	TCombat   = "combat"
	TNPC      = "npc"
	TQuests   = "quests"   // quest book: see Quests
	TBag      = "bag"      // equipment bag: see Bag
	TZone     = "zone"     // zone change (dungeon or main map): see Zone
	TDungeons = "dungeons" // discovered dungeons: see Dungeons
	TNodes    = "nodes"    // depleted resource nodes around the player: see Nodes
	TPong     = "pong"
)

// Shared limits.
const (
	MaxNameLen = 16
	MinNameLen = 3
	MaxChatLen = 200

	// Password length in bytes: bcrypt ignores everything beyond 72.
	MinPassLen = 8
	MaxPassLen = 72

	// View area (half-width / half-height, in tiles) around a player.
	// The server only sends the entities inside this rectangle; the client draws
	// the terrain itself from the seed.
	ViewHalfW = 40
	ViewHalfH = 20
)

// Game clock: the server counts in ticks (10 per second), the client derives the displayed day and time from them.
// One game hour lasts one real minute, one game day 24 minutes.
const (
	TicksPerGameHour = 600
	StartGameHour    = 8 // game hour at tick 0
)

// Move is an intent to move by one tile. dx and dy are -1, 0 or 1.
type Move struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// Entity is a player visible in a snapshot.
type Entity struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Fighting bool   `json:"fight,omitempty"` // engaged in a fight
}

// Monster is a monster visible in a snapshot. Kind is the species identifier
// ("rat", "wolf"...); the client derives the appearance from it.
type Monster struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Fighting bool   `json:"fight,omitempty"`
}

// Stats is the local player's sheet, sent in every snapshot.
type Stats struct {
	Level   int    `json:"lvl"`
	XP      int    `json:"xp"`
	XPNext  int    `json:"xpn"`
	HP      int    `json:"hp"`
	MaxHP   int    `json:"mhp"`
	Def     int    `json:"def"`
	Hit     int    `json:"hit"`
	Dmg     string `json:"dmg"` // dice of the wielded weapon and modifier, e.g. "2d6+3"
	Potions int    `json:"pot"`
	Gold    int    `json:"gold"`
	Mana    int    `json:"mp"`
	MaxMana int    `json:"mmp"`
}

// Snapshot describes what a player sees at a given tick. It is only sent
// if its content has changed since the last send.
type Snapshot struct {
	Tick     uint64    `json:"tick"`
	Self     Entity    `json:"self"`
	Others   []Entity  `json:"others,omitempty"`
	Monsters []Monster `json:"monsters,omitempty"`
	Stats    Stats     `json:"stats"`
	Online   int       `json:"on,omitempty"` // connected players
}

// Zone announces which map the player is on. It is sent on every dungeon entry or exit, and
// again each time a chest is opened in the instance. It always precedes the first snapshot of the new
// zone. Empty Dungeon: the main map (snapshot coordinates are world coordinates). Otherwise, snapshot
// coordinates are local to the dungeon layout, which the client recomputes from Seed and Tier
// (see world.NewDungeonMap).
type Zone struct {
	Dungeon string   `json:"dungeon,omitempty"` // dungeon identifier
	Name    string   `json:"name,omitempty"`    // its name, in the player's language
	Seed    int64    `json:"seed,omitempty"`
	Tier    int      `json:"tier,omitempty"`
	Opened  [][2]int `json:"opened,omitempty"` // chests already opened (x, y)
	// ResetIn, when non-zero, says the dungeon boss is already defeated: number of game hours (rounded up)
	// before the dungeon can reset.
	ResetIn int `json:"reset,omitempty"`
}

// DungeonInfo describes the entrance of a dungeon the player has discovered.
type DungeonInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"` // in the player's language
	X    int    `json:"x"`    // entrance tile on the main map
	Y    int    `json:"y"`
	Tier int    `json:"tier"`
}

// Dungeons is the full list of dungeons discovered by the player, sent at login and then on every
// new discovery. New, when non-empty, is the identifier of the dungeon just discovered (the client announces it).
type Dungeons struct {
	List []DungeonInfo `json:"list"`
	New  string        `json:"new,omitempty"`
}

// CombatAction is the player's choice for their combat turn:
// "attack", "item" or "flee".
type CombatAction struct {
	Action string `json:"action"`
	Target int    `json:"target,omitempty"` // index of the targeted monster in Combat.Enemies
}

// Outcomes of a fight (Combat.Result).
const (
	ResultWon  = "won"
	ResultLost = "lost"
	ResultFled = "fled"
)

// Enemy is a monster in the fight. Once slain, it stays in the list with HP == 0 (indices are stable).
type Enemy struct {
	Kind  string `json:"kind"` // species
	Name  string `json:"name"` // displayed name, numbered if several of the same species
	HP    int    `json:"hp"`
	MaxHP int    `json:"mhp"`
	Def   int    `json:"def"`
	Hit   int    `json:"hit"`           // to-hit bonus
	Dmg   string `json:"dmg,omitempty"` // damage dice
	Red   string `json:"red,omitempty"` // reduction dice
}

// Strike is an attack of the turn, for visual effects: Foe is the target (if ByPlayer) or the attacker.
type Strike struct {
	ByPlayer bool `json:"you,omitempty"`
	Foe      int  `json:"foe"`
	Dmg      int  `json:"dmg"`
	Hit      bool `json:"hit,omitempty"`
	Crit     bool `json:"crit,omitempty"`
}

// Combat is the state of a fight, sent at its start and after each turn.
// Log holds the events of the last turn. When Over is true, Result
// gives the outcome and the player is already released into the world. Boss is the index
// of the strongest living monster (the one whose portrait is displayed).
type Combat struct {
	Round       int      `json:"round"`
	Enemies     []Enemy  `json:"enemies"`
	Boss        int      `json:"boss"`
	PlayerHP    int      `json:"php"`
	PlayerMaxHP int      `json:"pmhp"`
	PlayerDef   int      `json:"pdef"`
	PlayerMana  int      `json:"pmp"`
	PlayerMaxMP int      `json:"pmmp"`
	Style       string   `json:"style,omitempty"` // style of the player's weapon: melee, ranged or magic
	Potions     int      `json:"pot"`
	Log         []string `json:"log,omitempty"`
	Strikes     []Strike `json:"strikes,omitempty"`
	Over        bool     `json:"over,omitempty"`
	Result      string   `json:"result,omitempty"`
}

// NPC dialogue actions (NPCAction.Action).
const (
	NPCBuy   = "buy"   // buy a potion (merchant)
	NPCSell  = "sell"  // sell a potion (merchant)
	NPCHeal  = "heal"  // get healed (healer)
	NPCLeave = "leave" // end the conversation

	// Free conversation with an NPC (language model).
	NPCTalk    = "talk"    // open the discussion
	NPCSay     = "say"     // say something (NPCAction.Text)
	NPCConsent = "consent" // agree to send one's messages to a third-party language service
	NPCDecline = "decline" // decline
	NPCBack    = "back"    // leave the discussion and return to the NPC menu

	// Quests. NPCTurnIn hands a ready quest back to its giver (NPCAction.Text = identifier).
	NPCQuestAccept = "quest_accept" // accept the offered quest
	NPCQuestRefuse = "quest_refuse" // refuse it
	NPCTurnIn      = "quest_turnin"
	NPCAskWork     = "quest_ask" // ask whether there is work (dungeon quest, no language model)

	// Equipment. NPCBuyItem and NPCSellItem carry the item in NPCAction.Text (see NPCOffer.Arg).
	NPCBuyItem  = "buy_item"
	NPCSellItem = "sell_item"
	NPCSellMenu = "sell_menu" // opens the list of bag items to resell

	// Guards and theft. NPCWhere asks where to find a trade (NPCAction.Text = NPC type, see NPCOffer.Arg).
	NPCWhere  = "where"  // guard: where to find a given inhabitant
	NPCPlaces = "places" // guard: which neighbouring places
	NPCSteal  = "steal"  // merchant: pick pockets
)

// MaxSayLen is the maximum length (in characters) of a player's line.
const MaxSayLen = 200

// NPCAction is the player's choice during a dialogue with an NPC. Text is
// only used by NPCSay.
type NPCAction struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
}

// Dialogue modes (NPCDialog.Mode).
const (
	NPCModeMenu   = "menu"   // the NPC's trade menu (buy, sell, heal...)
	NPCModeNotice = "notice" // privacy notice to accept before chatting
	NPCModeChat   = "chat"   // free conversation
	NPCModeQuest  = "quest"  // quest proposal to accept or refuse
	NPCModeSell   = "sell"   // bag items to resell to an equipment vendor
)

// NPCLine is a line of a discussion transcript.
type NPCLine struct {
	Who  string `json:"who"` // "npc", "you" or "sys"
	Text string `json:"text"`
}

// NPCOffer is an action offered by an NPC. The server decides the price and
// the availability (not enough gold, bag full...): the client just displays it.
type NPCOffer struct {
	Action    string    `json:"action"`
	Label     string    `json:"label"`
	Price     int       `json:"price"`          // 0 = free (or, for a sale, no item)
	Gain      bool      `json:"gain,omitempty"` // true if the player receives gold (sale) instead of paying
	Available bool      `json:"ok"`
	Arg       string    `json:"arg,omitempty"`    // to send back in NPCAction.Text (quest to hand in, item to buy or sell)
	Detail    string    `json:"detail,omitempty"` // detail displayed alongside (bonus, required level...)
	Info      *ItemInfo `json:"info,omitempty"`   // full item sheet (equipment offers), for the details panel
}

// ItemInfo is an equipment item as the player sees it.
type ItemInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Desc      string `json:"desc,omitempty"`
	Slot      string `json:"slot"`           // weapon, armor, shield, helm
	Kind      string `json:"kind,omitempty"` // weapon style: melee, ranged, magic
	Tier      int    `json:"tier"`
	Family    string `json:"family,omitempty"`
	Dice      string `json:"dice,omitempty"`      // weapon damage, e.g. "2d6+1"
	Reduction string `json:"reduction,omitempty"` // dice subtracted from damage taken (armor)
	Def       int    `json:"def,omitempty"`       // DEF bonus
	MinLevel  int    `json:"lvl"`
	Sell      int    `json:"sell"` // resale price
	Worn      bool   `json:"worn,omitempty"`
	Origin    string `json:"origin,omitempty"` // name of the original vendor
}

// Create is the roll offered during character creation: the server rolls the dice, the client
// displays. It is sent again after each reroll. The client answers with CreateActions; TWelcome
// ends the creation.
type Create struct {
	Scores  []int       `json:"scores"` // STR AGI INT CON WIS CHA
	Left    int         `json:"left"`   // remaining rerolls (3 in total)
	Level   int         `json:"lvl"`    // level kept from an existing character (1 for a new one)
	Classes []ClassInfo `json:"classes"`
}

// ClassInfo describes a character base class.
type ClassInfo struct {
	ID   string `json:"id"` // warrior, rogue, mage
	Die  int    `json:"die"`
	Mana int    `json:"mana"` // starting mana before the WIS modifier
}

// CreateAction.Action values.
const (
	CreateReroll  = "reroll"  // reroll ability Index (0 to 5)
	CreateConfirm = "confirm" // confirm the roll with class Class
)

// CreateAction is a player decision during creation.
type CreateAction struct {
	Action string `json:"action"`
	Index  int    `json:"index,omitempty"`
	Class  string `json:"class,omitempty"`
}

// Bag is the equipment bag, sent at login and on every change.
type Bag struct {
	Notice    []string   `json:"notice,omitempty"` // lines to append to the log
	Items     []ItemInfo `json:"items"`            // the whole bag, worn items included (Worn)
	Capacity  int        `json:"cap"`
	DefBonus  int        `json:"def"`       // total DEF bonus of the worn equipment
	Reduction string     `json:"reduction"` // reduction dice of the worn armor ("0" without armor)

	// Creation sheet (see internal/hero): class, six abilities (STR AGI INT CON WIS CHA),
	// style of the worn weapon and hit die.
	Class  string `json:"class,omitempty"`
	Scores []int  `json:"scores,omitempty"`
	Style  string `json:"style,omitempty"`
	HitDie int    `json:"die,omitempty"`

	// Facts established by the server about the character's history (character sheet).
	Kills     int `json:"kills,omitempty"`
	BossKills int `json:"bosses,omitempty"`
	Deaths    int `json:"deaths,omitempty"`

	// Active shrine blessings (character sheet).
	Blessings []Blessing `json:"blessings,omitempty"`

	// Treasure maps held: the tile to dig for each one (character sheet).
	Maps []TreasureMap `json:"maps,omitempty"`

	// Gathered materials (materials bag, see internal/resource), sorted by category then identifier.
	Resources []ResourceStack `json:"res,omitempty"`

	// Gathering jobs, one per resource category, in catalogue order.
	Jobs []JobInfo `json:"jobs,omitempty"`
}

// JobInfo describes a gathering job (lumberjack, miner, herbalist, fisher): Job is the category key
// (resource.Category.Key), Used and Cap the fill of the category's bag, Chance the success percentage.
// Next is 0 at the maximum level.
type JobInfo struct {
	Job    string `json:"job"`
	Level  int    `json:"lvl"`
	XP     int    `json:"xp"`
	Next   int    `json:"next,omitempty"`
	Used   int    `json:"used"`
	Cap    int    `json:"cap"`
	Chance int    `json:"chance"`
}

// ResourceStack is a quantity of material: ID is the catalogue identifier (see resource.Kind).
type ResourceStack struct {
	ID  string `json:"id"`
	Qty int    `json:"n"`
}

// Nodes lists the depleted resource nodes (harvested, not yet regrown) around the player. Since the terrain
// is recomputed by the client, this message tells it to draw those nodes as bare terrain. It
// replaces the previous list.
type Nodes struct {
	Spent [][2]int `json:"spent,omitempty"` // positions (x, y)
}

// TreasureMap is a treasure map: the coordinates of the tile to dig.
type TreasureMap struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Blessing is a temporary shrine bonus. Kind is BlessDef, BlessHit, BlessXP or BlessRegen; Bonus is its
// value (DEF or to-hit points, XP percentage, HP per second) and Secs the time remaining at send time.
type Blessing struct {
	Kind  string `json:"kind"`
	Bonus int    `json:"bonus"`
	Secs  int    `json:"secs"`
}

// Blessing kinds.
const (
	BlessDef   = "def"
	BlessHit   = "hit"
	BlessXP    = "xp"
	BlessRegen = "regen"
)

// GearAction is an action on the bag: equip an item (ID) or remove the item from a slot (Slot).
type GearAction struct {
	Action string `json:"action"` // "equip" or "unequip"
	ID     int64  `json:"id,omitempty"`
	Slot   string `json:"slot,omitempty"`
}

// Bag actions.
const (
	GearEquip   = "equip"
	GearUnequip = "unequip"
)

// QuestInfo is a quest as the player sees it. Everything is computed by the server.
type QuestInfo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Story    string `json:"story"`
	Giver    string `json:"giver"`    // NPC name
	Place    string `json:"place"`    // name of the giver's place
	Goal     string `json:"goal"`     // objective spelled out ("Defeat 5 wolves")
	Progress int    `json:"progress"` // actions completed
	Need     int    `json:"need"`
	Gold     int    `json:"gold"`
	XP       int    `json:"xp"`
	Potions  int    `json:"potions"`
	Note     string `json:"note,omitempty"`
	State    string `json:"state"` // "active", "ready" or "done"
	Gain     int    `json:"gain"`  // reputation points on hand-in

	// Key of the giver NPC (region and rank within the place): the client uses it to locate, on the map,
	// the NPC to hand a ready quest to.
	GiverRX  int `json:"grx"`
	GiverRY  int `json:"gry"`
	GiverIdx int `json:"gi"`
}

// Standing is the player's reputation in a place.
type Standing struct {
	Place  string `json:"place"`
	Points int    `json:"points"`
	Tier   string `json:"tier"`
	Next   int    `json:"next,omitempty"` // points missing for the next tier
}

// Quests is the quest book, sent at login and on every change.
type Quests struct {
	Open     []QuestInfo `json:"open"`    // active and ready
	History  []QuestInfo `json:"history"` // latest quests handed in
	Standing []Standing  `json:"standing"`
	// Full: the open-quest quota is reached, no giver can assign anything.
	Full bool `json:"full,omitempty"`
	// Pauses: givers to whom a quest was just handed in, with the delay remaining before the next one.
	// Together with the givers of open quests, this is enough to hide the "!" of those with nothing to offer.
	Pauses []GiverPause `json:"pauses,omitempty"`
	Notice []string     `json:"notice,omitempty"` // lines to append to the log (progress, abandonment...)
}

// GiverPause is a quest giver on pause (NPC key, seconds remaining).
type GiverPause struct {
	RX   int `json:"rx"`
	RY   int `json:"ry"`
	Idx  int `json:"i"`
	Secs int `json:"s"`
}

// QuestAction is an action on the quest book. Currently: abandon.
type QuestAction struct {
	Action string `json:"action"` // "abandon"
	ID     int64  `json:"id"`
}

// Quest book actions.
const QuestAbandon = "abandon"

// NPCDialog is the state of a dialogue, sent when it opens and then after each
// action. When Over is true, the dialogue is finished and the player is free.
type NPCDialog struct {
	Kind    string     `json:"kind"` // "merchant" or "healer"
	Gold    int        `json:"gold"`
	Potions int        `json:"pot"`
	HP      int        `json:"hp"`
	MaxHP   int        `json:"mhp"`
	Offers  []NPCOffer `json:"offers,omitempty"`
	Log     []string   `json:"log,omitempty"`
	Over    bool       `json:"over,omitempty"`

	// NPC identity (deterministic: the client knows it too) and discussion state.
	Name     string    `json:"name,omitempty"`
	Place    string    `json:"place,omitempty"`
	Mode     string    `json:"mode,omitempty"`     // NPCModeMenu by default
	Thinking bool      `json:"thinking,omitempty"` // the NPC is preparing its reply
	Notice   string    `json:"notice,omitempty"`   // notice text (notice mode)
	Lines    []NPCLine `json:"lines,omitempty"`    // transcript (chat mode)

	Proposal *QuestInfo `json:"proposal,omitempty"` // proposed quest (quest mode)

	Specialty string `json:"specialty,omitempty"` // specialty of an equipment vendor
}

// Envelope is the envelope common to all messages.
type Envelope struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

// Login is the first message sent by a client. With Register, the
// server creates the account (the name must not exist); otherwise it
// authenticates an existing account.
type Login struct {
	Name     string `json:"name"`
	Password string `json:"pass"`
	Register bool   `json:"register,omitempty"`
	// Lang is the client's interface language ("fr" or "en"): the server writes
	// the messages it addresses to it in that language. When absent (old client), it is French.
	Lang string `json:"lang,omitempty"`
}

// Welcome confirms the connection and gives the player's initial state.
type Welcome struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Seed int64  `json:"seed"`
	// Gen is the terrain generator version (absent = 1, old servers).
	Gen int `json:"gen,omitempty"`
}

// Error reports a refusal or an error to the client.
type Error struct {
	Msg string `json:"msg"`
}

// ChatSend is a chat message sent by a client.
type ChatSend struct {
	Text string `json:"text"`
}

// Chat channels.
const (
	ChanLocal   = "local"   // players within the sender's view area
	ChanGlobal  = "global"  // all players
	ChanEmote   = "emote"   // /me, same as local
	ChanWhisper = "whisper" // private message
	ChanSystem  = "system"  // server announcements
)

// Chat is a chat message broadcast by the server.
// From is empty for a system message. To is only set for a
// private message (received by the recipient, and sent back to the sender).
type Chat struct {
	Channel string `json:"ch,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Text    string `json:"text"`
}

// Encode serializes a payload into an envelope. Payloads that know how to
// encode themselves (snapshots, see fastjson.go) avoid reflection.
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

// Decode reads an envelope from bytes.
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("invalid message: %w", err)
	}
	if e.T == "" {
		return e, fmt.Errorf("message without type")
	}
	return e, nil
}

// Payload deserializes the envelope's payload into v.
func (e Envelope) Payload(v any) error {
	if len(e.D) == 0 {
		return fmt.Errorf("missing payload for %q", e.T)
	}
	return json.Unmarshal(e.D, v)
}

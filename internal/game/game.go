// Package game contains the authoritative game loop.
//
// All state (players, positions) belongs to a single goroutine, that of
// Run. The rest of the program sends it commands via Join, Leave and Move
// and receives snapshots through the callbacks given to Join. No lock
// is therefore needed on the game state.
package game

import (
	"context"
	"errors"
	"github.com/kevin/ascii-adventure/internal/resource"
	"maps"
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	// TickInterval is the duration of a tick (10 Hz).
	TickInterval = 100 * time.Millisecond
	// MoveCooldown is the number of ticks between two steps (5 tiles/s).
	MoveCooldown = 2

	inboxSize      = 1024
	autosaveEvery  = 300 // ticks between two periodic saves (30 s)
	unloadEvery    = 300 // ticks between two chunk unloads
	spawnSearchMax = 25  // search radius for a free tile at spawn
)

// ErrStopped is returned when the game loop is stopped.
var ErrStopped = errors.New("game loop stopped")

type player struct {
	id   int
	name string
	lang i18n.Lang // player's language: that of the messages the game addresses to them
	pos  world.Pos
	send func(protocol.Snapshot)

	// Player's zone: 0 for the main map, otherwise a dungeon instance (see dungeon.go).
	// In a dungeon, pos is local to the dungeon map and ret is the main map tile
	// where they return on leaving, dying or disconnecting.
	zone ZoneID
	ret  world.Pos

	calmUntil  uint64                   // tick until which no monster engages this player (respite after fleeing)
	safe       bool                     // in a safe zone; recomputed on every tile change (see setPos)
	sendCombat func(protocol.Combat)    // may be nil
	sendNPC    func(protocol.NPCDialog) // may be nil
	talk       *talk                    // conversation in progress with an NPC
	consent    bool                     // has agreed to send their messages to a language service
	stats      combat.Stats
	mana       int // current reserve (the maximum is in stats)
	hp         int
	fight      *fight

	hasPending bool
	pendingDX  int
	pendingDY  int
	cooldown   int

	lastSent *protocol.Snapshot // last snapshot sent (without tick)

	// Quests (see quests.go).
	quests        []quest.Quest
	rep           map[world.RegionPos]int // reputation per place
	nextQuestID   int64
	questsDirty   bool // to be rewritten at the next save
	hasOpenReach  bool // a "reach a place" quest is active
	questNotices  []string
	sendQuestBook func(protocol.Quests)

	// Equipment (see shop.go).
	inv         gear.Inventory
	gearDirty   bool
	gearNotices []string
	sendBagMsg  func(protocol.Bag)

	sendZoneMsg func(protocol.Zone) // may be nil

	// Dungeons whose entrance the player has discovered, by region (see dungeon.go).
	discovered      map[world.RegionPos]bool
	discoveredDirty bool // to be rewritten at the next save
	sendDungeonsMsg func(protocol.Dungeons)

	// Raw materials (see gather.go).
	resources    map[resource.Kind]int     // materials bag: quantity per resource
	skills       map[resource.Category]int // accumulated experience of each gathering job
	gatherUntil  uint64                    // tick before which one cannot gather again
	sendNodesMsg func(protocol.Nodes)
	nodesVer     uint64    // version of g.spent when the last Nodes message was sent
	nodesAt      world.Pos // where the player was at that moment

	// Points of interest (see poi.go).
	respawn    *world.Pos               // last campfire: respawn there after a defeat
	stones     map[world.RegionPos]bool // activated standing stones, per region
	maps       []world.Pos              // treasure maps: tiles to dig (see treasure.go)
	buffs      []buff                   // shrine bonuses in progress (not persisted)
	poiCooling map[world.POIKey]uint64  // tick before which the point has no effect for this player
}

// Saved is a player's persistent state: what is needed to find them again
// on reconnection.
type Saved struct {
	Stats combat.Stats // class, characteristics and max HP included (zero: neutral warrior)
	HP    int
	Mana  int
	Pos   world.Pos

	// LLMConsent: the player has agreed to have their messages sent to a
	// third-party language service to converse with NPCs.
	LLMConsent bool

	// Quests and reputation per place (see internal/quest). QuestsDirty indicates that the
	// save must rewrite them in the database.
	Quests      []quest.Quest
	Rep         map[world.RegionPos]int
	QuestsDirty bool

	// Equipment bag and worn items (see internal/gear). GearDirty: to be rewritten in the database.
	Gear      gear.Inventory
	GearDirty bool

	// Discovered dungeons (regions that hide them). DiscoveredDirty: to be rewritten in the database.
	Discovered      []world.RegionPos
	DiscoveredDirty bool

	// Respawn point (campfire) and activated standing stones.
	Respawn *world.Pos
	Stones  []world.RegionPos
	Maps    []world.Pos // treasure maps held: the tile to dig for each

	// Materials bag: quantity per resource identifier (see gather.go).
	Resources map[string]int
	// Accumulated experience of gathering jobs, per category key (resource.Category.Key).
	Skills map[string]int
}

// Game is the state of the living world.
type Game struct {
	World *world.World

	// Talker, if set and available, makes NPCs speak with a language model
	// (see talk.go). nil: no free discussion, only menus. To be set before Run.
	Talker Talker

	// Dungeons enables dungeons: walking onto an entrance leads into an instance. Off by
	// default, as long as the client cannot display a dungeon map.
	Dungeons bool

	geo map[geoKey][]npcai.Landmark // places known to chiefs and mayors (see geography.go)

	// Stalls of equipment vendors (see shop.go).
	shops        map[world.NPCKey]shopWares
	stockPending map[world.NPCKey]bool
	stockRetryAt map[world.NPCKey]uint64

	// Clock gives the quests' time (creation, handing in, expiry). nil: time.Now.
	// To be set before Run (tests).
	Clock func() time.Time

	// Persist, if set, is called to save a player (disconnection,
	// end of combat, periodic save, shutdown). It is called from the
	// game goroutine and must never block. To be set before Run.
	Persist func(name string, s Saved)

	inbox chan func()
	done  chan struct{} // closed when Run ends

	// Exclusive property of the Run goroutine:
	players map[int]*player
	idCache []int             // sorted identifiers, valid if non-nil; invalidated when players changes
	byPos   map[world.Pos]int // main map index (zone 0); each instance has its own
	nextID  int
	tick    uint64

	metrics metrics // readable from any goroutine

	talkSeq uint64 // last conversation number assigned

	monsters      map[int]*monster
	monsterAt     map[world.Pos]int        // same: main map only
	slotMonster   map[world.SlotKey]int    // fortress slot -> living monster
	slotDefeated  map[world.SlotKey]uint64 // slot -> tick of the last victory (respawn)
	gd            guardState               // guards of villages and towns (see guards.go)
	garrisonCount int                      // living garrison monsters
	car           caravanState             // tracked travelling merchants (see caravan.go)
	spent         map[world.Pos]uint64     // resource node -> tick of its gathering (see gather.go)
	spentVer      uint64                   // changes on every modification of spent
	lairOpened    map[world.POIKey]uint64  // lair -> tick of its chest's opening (see poi.go)
	nextMonsterID int

	instances       map[ZoneID]*instance // dungeon instances (see dungeon.go)
	dungeonZone     map[string]ZoneID    // dungeon (identifier) -> its current instance
	nextZone        ZoneID
	dungeonMonsters int // living monsters in dungeons (outside the main map's quotas)

	rng *rand.Rand // game randomness (spawns, wandering); injectable in tests
}

// New creates a game for the given world. Then launch Run.
func New(w *world.World) *Game {
	return &Game{
		World:   w,
		inbox:   make(chan func(), inboxSize),
		done:    make(chan struct{}),
		players: make(map[int]*player),
		byPos:   make(map[world.Pos]int),
		nextID:  1,

		monsters:      make(map[int]*monster),
		monsterAt:     make(map[world.Pos]int),
		slotMonster:   make(map[world.SlotKey]int),
		slotDefeated:  make(map[world.SlotKey]uint64),
		spent:         make(map[world.Pos]uint64),
		lairOpened:    make(map[world.POIKey]uint64),
		nextMonsterID: 1,
		instances:     make(map[ZoneID]*instance),
		dungeonZone:   make(map[string]ZoneID),
		nextZone:      1,
		rng:           rand.New(rand.NewPCG(uint64(w.Seed), 0x9e3779b97f4a7c15)),
	}
}

// Run executes the game loop until ctx is cancelled.
func (g *Game) Run(ctx context.Context) {
	defer close(g.done)
	t := time.NewTicker(TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, id := range g.sortedIDs() {
				g.save(g.players[id])
			}
			return
		case fn := <-g.inbox:
			fn()
		case <-t.C:
			g.timedStep()
		}
	}
}

// do executes fn in the game goroutine. Returns false if the game is stopped.
// Blocks if the queue is full (reserved for commands that must not be lost).
func (g *Game) do(fn func()) bool {
	select {
	case g.inbox <- fn:
		return true
	case <-g.done:
		return false
	}
}

// Handlers groups a player's callbacks. Snapshot and Combat are called
// from the game goroutine: they must never block.
type Handlers struct {
	// Lang is the language of the messages addressed to the player (the zero value is English).
	Lang i18n.Lang
	// State, if non-nil, restores an already known player (level, HP, position).
	State    *Saved
	Snapshot func(protocol.Snapshot)
	Combat   func(protocol.Combat)    // optional
	NPC      func(protocol.NPCDialog) // optional
	Quests   func(protocol.Quests)    // optional: quest book
	Bag      func(protocol.Bag)       // optional: equipment bag
	Zone     func(protocol.Zone)      // optional: entering or leaving a dungeon, opened chests
	Dungeons func(protocol.Dungeons)  // optional: discovered dungeons
	Nodes    func(protocol.Nodes)     // optional: depleted resource nodes around the player
	// Joined is called in the game goroutine before any snapshot, which
	// allows sending the welcome message before the first snapshot.
	Joined func(id, x, y int)
}

// Join adds a player and returns their identifier.
func (g *Game) Join(name string, h Handlers) (int, error) {
	reply := make(chan int, 1)
	if !g.do(func() {
		p := g.joinWith(name, h.Snapshot, h.State)
		p.lang = h.Lang.Normalize()
		p.sendCombat = h.Combat
		p.sendNPC = h.NPC
		p.sendQuestBook = h.Quests
		p.sendBagMsg = h.Bag
		p.sendZoneMsg = h.Zone
		p.sendDungeonsMsg = h.Dungeons
		p.sendNodesMsg = h.Nodes
		if h.Joined != nil {
			h.Joined(p.id, p.pos.X, p.pos.Y)
		}
		g.sendQuests(p) // after the welcome message
		g.sendDungeons(p, "")
		g.sendBag(p)
		reply <- p.id
	}) {
		return 0, ErrStopped
	}
	select {
	case id := <-reply:
		return id, nil
	case <-g.done:
		return 0, ErrStopped
	}
}

// Leave removes a player and only waits for the departure to be processed (including
// the Persist call). Returns immediately if the game is stopped.
func (g *Game) Leave(id int) {
	done := make(chan struct{})
	if !g.do(func() { g.leave(id); close(done) }) {
		return
	}
	select {
	case <-done:
	case <-g.done:
	}
}

// Move records a player's movement intent (the last overwrites
// the previous ones). May be lost if the server is saturated.
func (g *Game) Move(id, dx, dy int) {
	select {
	case g.inbox <- func() { g.move(id, dx, dy) }:
	default:
	}
}

// Nearby returns the identifiers of the players within the vision zone of player
// id (themselves included). Used for local chat range.
func (g *Game) Nearby(id int) ([]int, error) {
	reply := make(chan []int, 1)
	if !g.do(func() { reply <- g.nearby(id) }) {
		return nil, ErrStopped
	}
	select {
	case ids := <-reply:
		return ids, nil
	case <-g.done:
		return nil, ErrStopped
	}
}

// --- Internal functions, called only from the game goroutine ---

// sees says whether b is in a's vision zone.
func sees(a, b *player) bool {
	return a.zone == b.zone && abs(a.pos.X-b.pos.X) <= protocol.ViewHalfW && abs(a.pos.Y-b.pos.Y) <= protocol.ViewHalfH
}

func (g *Game) nearby(id int) []int {
	p, ok := g.players[id]
	if !ok {
		return nil
	}
	var ids []int
	for _, oid := range g.sortedIDs() {
		if sees(p, g.players[oid]) {
			ids = append(ids, oid)
		}
	}
	return ids
}

func (g *Game) join(name string, send func(protocol.Snapshot)) *player {
	return g.joinWith(name, send, nil)
}

// joinWith adds a player, new or restored from saved. A saved
// position that has become occupied is replaced by the nearest free tile;
// out-of-range values are brought back within valid bounds.
func (g *Game) joinWith(name string, send func(protocol.Snapshot), saved *Saved) *player {
	p := &player{id: g.nextID, name: name, send: send, stats: combat.NewStats(), rep: map[world.RegionPos]int{}, nextQuestID: 1,
		discovered: map[world.RegionPos]bool{}, stones: map[world.RegionPos]bool{}, poiCooling: map[world.POIKey]uint64{}}
	p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
	start := g.World.FindSpawn()
	if saved != nil {
		p.stats = combat.StatsFor(saved.Stats.Level, saved.Stats.XP, saved.Stats.Potions,
			saved.Stats.Class, saved.Stats.Scores, saved.Stats.MaxHP).
			WithGold(saved.Stats.Gold).
			WithRecord(saved.Stats.Kills, saved.Stats.BossKills, saved.Stats.Deaths)
		p.consent = saved.LLMConsent
		g.loadQuests(p, saved)
		p.inv = gear.SanitizeInventory(saved.Gear)
		for _, rp := range saved.Discovered {
			p.discovered[rp] = true
		}
		p.discoveredDirty = saved.DiscoveredDirty
		if saved.Respawn != nil {
			r := *saved.Respawn
			p.respawn = &r
		}
		p.maps = slices.Clone(saved.Maps)
		p.resources = loadResources(saved.Resources)
		p.skills = loadSkills(saved.Skills)
		for _, rp := range saved.Stones {
			p.stones[rp] = true
		}
		p.gearDirty = saved.GearDirty // items added before connection (starting weapon): to be written at the first save
		p.hp = max(1, min(saved.HP, p.stats.MaxHP))
		p.mana = max(0, min(saved.Mana, p.stats.MaxMana))
		start = saved.Pos
	}
	g.nextID++
	g.players[p.id] = p
	g.idCache = nil
	g.setPos(p, g.freeNear(start))
	return p
}

// save passes the player's state to the persistence callback.
func (g *Game) save(p *player) {
	if g.Persist != nil {
		// Quests are always attached (a reconnection before the write must find them);
		// QuestsDirty only says whether they must be rewritten in the database.
		g.Persist(p.name, Saved{
			Stats: p.stats, HP: p.hp, Mana: p.mana, Pos: p.surfacePos(), LLMConsent: p.consent,
			Quests: slices.Clone(p.quests), Rep: maps.Clone(p.rep), QuestsDirty: p.questsDirty,
			Gear: p.inv.Clone(), GearDirty: p.gearDirty,
			Discovered: p.discoveredList(), DiscoveredDirty: p.discoveredDirty,
			Respawn: p.respawn, Stones: p.stoneList(), Maps: slices.Clone(p.maps), Resources: saveResources(p.resources), Skills: saveSkills(p.skills),
		})
		p.questsDirty, p.gearDirty, p.discoveredDirty = false, false, false
	}
}

func (g *Game) leave(id int) {
	p, ok := g.players[id]
	if !ok {
		return
	}
	g.abortFight(p)
	g.save(p)
	if at := g.playerIndex(p.zone); at[p.pos] == p.id {
		delete(at, p.pos)
	}
	delete(g.players, id)
	g.idCache = nil
	if inst := g.instances[p.zone]; inst != nil {
		g.closeIfEmpty(inst)
	}
}

func (g *Game) move(id, dx, dy int) {
	p, ok := g.players[id]
	if !ok {
		return
	}
	dx, dy = clamp1(dx), clamp1(dy)
	if dx == 0 && dy == 0 {
		return
	}
	p.hasPending, p.pendingDX, p.pendingDY = true, dx, dy
}

func clamp1(v int) int { return max(-1, min(1, v)) }

// setPos places p at pos while maintaining the position index.
func (g *Game) setPos(p *player, pos world.Pos) {
	at := g.playerIndex(p.zone)
	if cur, ok := at[p.pos]; ok && cur == p.id {
		delete(at, p.pos)
	}
	p.pos = pos
	p.safe = p.zone == 0 && g.World.Safe(pos.X, pos.Y)
	at[pos] = p.id
	if p.zone == 0 {
		g.noticeDungeons(p)
		if p.hasOpenReach {
			g.questMoved(p)
		}
	}
}

// blocked says whether a main map tile is impassable (terrain, player,
// monster or NPC).
func (g *Game) blocked(pos world.Pos) bool { return g.blockedIn(0, pos) }

// blockedIn is blocked for zone z: the main map (0) or a dungeon instance.
func (g *Game) blockedIn(z ZoneID, pos world.Pos) bool {
	if z != 0 {
		inst := g.instances[z]
		if inst == nil || !inst.dmap.TileAt(pos.X, pos.Y).Walkable() {
			return true
		}
		if _, occupied := inst.byPos[pos]; occupied {
			return true
		}
		_, occupied := inst.monsterAt[pos]
		return occupied
	}
	if !g.World.At(pos.X, pos.Y).Walkable() {
		return true
	}
	if _, occupied := g.byPos[pos]; occupied {
		return true
	}
	if _, occupied := g.monsterAt[pos]; occupied {
		return true
	}
	if g.guardAt(pos) != nil { // a guard too
		return true
	}
	if g.caravanAt(pos) != nil { // and a caravan
		return true
	}
	_, npc := g.World.NPCAt(pos.X, pos.Y) // an NPC occupies their tile
	return npc
}

// freeSpawn returns a free, walkable tile near the world's starting point.
func (g *Game) freeSpawn() world.Pos { return g.freeNear(g.World.FindSpawn()) }

// freeNear returns the free, walkable tile nearest to c (in rings).
func (g *Game) freeNear(c world.Pos) world.Pos { return g.freeNearIn(0, c, 0) }

// freeNearIn is freeNear in zone z, ignoring tiles within minR of c.
func (g *Game) freeNearIn(z ZoneID, c world.Pos, minR int) world.Pos {
	for r := minR; r <= spawnSearchMax; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r {
					continue
				}
				if pos := (world.Pos{X: c.X + dx, Y: c.Y + dy}); !g.blockedIn(z, pos) {
					return pos
				}
			}
		}
	}
	return c
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// step advances the world by one tick.
func (g *Game) step() {
	g.tick++

	ids := g.sortedIDs()

	// Movements, by increasing identifier for deterministic arbitration.
	for _, id := range ids {
		p := g.players[id]
		if p.cooldown > 0 {
			p.cooldown--
		}
		if p.expireBuffs(g.tick) {
			g.sendBag(p) // the character sheet must no longer show the blessing
		}
		if p.fight != nil || p.talk != nil { // in combat or conversation: immobile
			p.hasPending = false
			continue
		}
		if !p.hasPending || p.cooldown > 0 {
			continue
		}
		p.hasPending = false
		dst := world.Pos{X: p.pos.X + p.pendingDX, Y: p.pos.Y + p.pendingDY}
		if mid, ok := g.monsterIndex(p.zone)[dst]; ok { // walking onto a monster attacks it
			g.startFight(p, g.monsters[mid], true)
			continue
		}
		if p.zone == 0 {
			if c := g.caravanAt(dst); c != nil { // walking onto a caravan opens its shop
				g.bumpCaravan(p, c)
				continue
			}
			if gd := g.guardAt(dst); gd != nil { // walking onto a guard talks to it (or fights it, if one is an outlaw)
				g.bumpGuard(p, gd)
				continue
			}
			if npc, ok := g.World.NPCAt(dst.X, dst.Y); ok { // walking onto an NPC addresses them
				g.startTalk(p, npc)
				continue
			}
			// A dungeon's secret entrance is an ordinary tile: one enters it by walking onto it.
			if d := g.World.DungeonAt(dst.X, dst.Y); d != nil && g.Dungeons && !g.blocked(dst) {
				g.enterDungeon(p, d)
				continue
			}
		} else if inst := g.instances[p.zone]; inst != nil && dst == inst.dmap.Exit {
			g.leaveDungeon(p)
			continue
		}
		if g.blockedIn(p.zone, dst) {
			continue
		}
		g.setPos(p, dst)
		p.cooldown = MoveCooldown
		if inst := g.instances[p.zone]; inst != nil && inst.dmap.TileAt(dst.X, dst.Y) == world.Chest {
			g.openChest(p, inst, dst) // walking onto a chest opens it
		} else if p.zone == 0 {
			g.poiStep(p, dst) // campfire, shrine, lair, tower, standing stone
		}
	}

	g.expireIdleFights(ids)
	g.expireIdleTalks(ids)
	if g.tick%safeRegenEvery == 0 {
		g.regenerate(g.healDue(ids))
		g.regenBuffs(ids)
	}
	if g.tick%autosaveEvery == 0 {
		for _, id := range ids {
			g.save(g.players[id])
		}
	}

	// Monsters: movement, then periodic spawns/despawns.
	mids := g.sortedMonsterIDs()
	g.stepMonsters(mids)
	g.stepGuards()
	g.stepCaravans()
	if g.tick%spawnEvery == 0 {
		g.despawnFar()
		g.spawnMonsters()
		g.activateGarrisons()
		g.activateGuards()
		g.activateCaravans()
		g.activateLairs()
		mids = g.sortedMonsterIDs()
	}

	// Snapshots, only if what the player sees has changed.
	g.expireNodes()
	for _, id := range ids {
		g.sendSnapshot(g.players[id], mids)
		g.syncNodes(g.players[id])
	}

	if g.tick%unloadEvery == 0 {
		g.unloadChunks()
	}
}

// sortedIDs returns the players' identifiers, sorted. The result is
// cached until the next departure or arrival: it is consulted thousands of
// times per tick (once per monster and per player) and must not be
// re-sorted on every call. The caller must not modify the returned slice.
func (g *Game) sortedIDs() []int {
	if g.idCache == nil {
		ids := make([]int, 0, len(g.players))
		for id := range g.players {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		g.idCache = ids
	}
	return g.idCache
}

func entityOf(p *player) protocol.Entity {
	return protocol.Entity{ID: p.id, Name: p.name, X: p.pos.X, Y: p.pos.Y, Fighting: p.fight != nil}
}

// snapshotFor builds what p sees (rectangular vision zone).
func (g *Game) snapshotFor(p *player, monsterIDs []int) protocol.Snapshot {
	snap := protocol.Snapshot{Tick: g.tick, Self: entityOf(p), Stats: statsOf(p), Online: len(g.players)}
	for _, id := range g.sortedIDs() {
		o := g.players[id]
		if o == p {
			continue
		}
		if sees(p, o) {
			snap.Others = append(snap.Others, entityOf(o))
		}
	}
	for _, id := range monsterIDs {
		m := g.monsters[id]
		if m.zone == p.zone && inBox(m.pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
			snap.Monsters = append(snap.Monsters, protocol.Monster{ID: m.id, Kind: m.sp.Kind, X: m.pos.X, Y: m.pos.Y, Fighting: m.fight != nil})
		}
	}
	snap.Monsters = g.appendGuards(snap.Monsters, p)
	snap.Monsters = g.appendCaravans(snap.Monsters, p)
	return snap
}

func sameView(a, b protocol.Snapshot) bool {
	return a.Self == b.Self && a.Stats == b.Stats && a.Online == b.Online &&
		slices.Equal(a.Others, b.Others) && slices.Equal(a.Monsters, b.Monsters)
}

func (g *Game) sendSnapshot(p *player, monsterIDs []int) {
	snap := g.snapshotFor(p, monsterIDs)
	if p.lastSent != nil && sameView(*p.lastSent, snap) {
		return
	}
	cp := snap
	p.lastSent = &cp
	p.send(snap)
}

// unloadChunks frees the chunks far from all players.
func (g *Game) unloadChunks() {
	keep := make(map[world.ChunkPos]bool)
	for _, p := range g.players {
		sp := p.surfacePos() // in a dungeon, keep the surroundings of the entrance
		c := world.ChunkOf(sp.X, sp.Y)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				keep[world.ChunkPos{X: c.X + dx, Y: c.Y + dy}] = true
			}
		}
	}
	g.World.Unload(keep)
}

// metrics contains the loop's health counters (written by the
// game goroutine, read by anyone without a lock).
type metrics struct {
	ticks     atomic.Uint64
	slowTicks atomic.Uint64 // ticks longer than TickInterval
	totalNs   atomic.Uint64
	maxNs     atomic.Uint64
	players   atomic.Int64
	monsters  atomic.Int64

	// Sliding window of the latest tick durations (game goroutine only),
	// summarised in the atomics below: this shows the steady state,
	// without startup masking the measurement.
	ring      [recentTicks]time.Duration
	recentP99 atomic.Uint64
	recentMax atomic.Uint64
}

const (
	recentTicks    = 600 // 60 s of ticks
	summarizeEvery = 50
)

// Metrics is a copy of the game loop counters.
type Metrics struct {
	Ticks     uint64        // ticks executed since startup
	SlowTicks uint64        // ticks that exceeded TickInterval (the cadence then slipped)
	TickTotal time.Duration // cumulative time spent in ticks
	TickMax   time.Duration // longest tick since startup
	RecentP99 time.Duration // 99th percentile of the last 60 seconds
	RecentMax time.Duration // longest tick of the last 60 seconds
	Players   int
	Monsters  int
}

// Metrics returns the state of the counters. Safe from any goroutine.
func (g *Game) Metrics() Metrics {
	return Metrics{
		Ticks:     g.metrics.ticks.Load(),
		SlowTicks: g.metrics.slowTicks.Load(),
		TickTotal: time.Duration(g.metrics.totalNs.Load()),
		TickMax:   time.Duration(g.metrics.maxNs.Load()),
		RecentP99: time.Duration(g.metrics.recentP99.Load()),
		RecentMax: time.Duration(g.metrics.recentMax.Load()),
		Players:   int(g.metrics.players.Load()),
		Monsters:  int(g.metrics.monsters.Load()),
	}
}

// timedStep executes a tick and records its duration.
func (g *Game) timedStep() {
	start := time.Now()
	g.step()
	g.record(time.Since(start))
}

func (g *Game) record(d time.Duration) {
	m := &g.metrics
	m.ticks.Add(1)
	m.totalNs.Add(uint64(d))
	if d > TickInterval {
		m.slowTicks.Add(1)
	}
	for { // maximum without a lock
		cur := m.maxNs.Load()
		if uint64(d) <= cur || m.maxNs.CompareAndSwap(cur, uint64(d)) {
			break
		}
	}
	n := m.ticks.Load()
	m.ring[(n-1)%recentTicks] = d
	if n%summarizeEvery == 0 {
		m.summarize(int(min(n, recentTicks)))
	}
	m.players.Store(int64(len(g.players)))
	m.monsters.Store(int64(len(g.monsters)))
}

// summarize computes the p99 and the maximum of the last n durations.
func (m *metrics) summarize(n int) {
	buf := make([]time.Duration, n)
	copy(buf, m.ring[:n]) // order matters little for a percentile; before 600 ticks, only the first n cells are filled
	slices.Sort(buf)
	m.recentP99.Store(uint64(buf[min(n-1, n*99/100)]))
	m.recentMax.Store(uint64(buf[n-1]))
}

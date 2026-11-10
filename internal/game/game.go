// Package game contient la boucle de jeu autoritative.
//
// Tout l'état (joueurs, positions) appartient à une unique goroutine, celle de
// Run. Le reste du programme lui envoie des commandes via Join, Leave et Move
// et reçoit des instantanés par les callbacks fournis à Join. Aucun verrou
// n'est donc nécessaire sur l'état du jeu.
package game

import (
	"context"
	"errors"
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
	// TickInterval est la durée d'un tick (10 Hz).
	TickInterval = 100 * time.Millisecond
	// MoveCooldown est le nombre de ticks entre deux pas (5 tuiles/s).
	MoveCooldown = 2

	inboxSize      = 1024
	autosaveEvery  = 300 // ticks entre deux sauvegardes périodiques (30 s)
	unloadEvery    = 300 // ticks entre deux déchargements de chunks
	spawnSearchMax = 25  // rayon de recherche d'une case libre au spawn
)

// ErrStopped est renvoyée quand la boucle de jeu est arrêtée.
var ErrStopped = errors.New("la boucle de jeu est arrêtée")

type player struct {
	id   int
	name string
	lang i18n.Lang // langue du joueur: celle des messages que le jeu lui adresse
	pos  world.Pos
	send func(protocol.Snapshot)

	// Zone du joueur : 0 pour la carte principale, sinon l'instance d'un donjon (voir dungeon.go).
	// Dans un donjon, pos est local à la carte du donjon et ret est la case de la carte principale
	// où l'on revient en sortant, en mourant ou en se déconnectant.
	zone ZoneID
	ret  world.Pos

	calmUntil  uint64                   // tick jusqu'auquel aucun monstre n'engage ce joueur (répit après une fuite)
	safe       bool                     // en zone sûre; recalculé à chaque changement de case (voir setPos)
	sendCombat func(protocol.Combat)    // peut être nil
	sendNPC    func(protocol.NPCDialog) // peut être nil
	talk       *talk                    // conversation en cours avec un PNJ
	consent    bool                     // a accepté l'envoi de ses messages à un service de langage
	stats      combat.Stats
	mana       int // réserve actuelle (le maximum est dans stats)
	hp         int
	fight      *fight

	hasPending bool
	pendingDX  int
	pendingDY  int
	cooldown   int

	lastSent *protocol.Snapshot // dernier instantané envoyé (sans tick)

	// Quêtes (voir quests.go).
	quests        []quest.Quest
	rep           map[world.RegionPos]int // réputation par lieu
	nextQuestID   int64
	questsDirty   bool // à réécrire à la prochaine sauvegarde
	hasOpenReach  bool // une quête « atteindre un lieu » est active
	questNotices  []string
	sendQuestBook func(protocol.Quests)

	// Équipement (voir shop.go).
	inv         gear.Inventory
	gearDirty   bool
	gearNotices []string
	sendBagMsg  func(protocol.Bag)

	sendZoneMsg func(protocol.Zone) // peut être nil

	// Donjons dont le joueur a découvert l'entrée, par région (voir dungeon.go).
	discovered      map[world.RegionPos]bool
	discoveredDirty bool // à réécrire à la prochaine sauvegarde
	sendDungeonsMsg func(protocol.Dungeons)

	// Points d'intérêt (voir poi.go).
	respawn    *world.Pos               // dernier feu de camp : on y réapparaît après une défaite
	stones     map[world.RegionPos]bool // pierres dressées activées, par région
	maps       []world.Pos              // cartes au trésor : cases à creuser (voir treasure.go)
	buffs      []buff                   // bonus de sanctuaire en cours (pas persistés)
	poiCooling map[world.POIKey]uint64  // tick avant lequel le point est sans effet pour ce joueur
}

// Saved est l'état persistant d'un joueur: ce qu'il faut pour le retrouver
// à sa reconnexion.
type Saved struct {
	Stats combat.Stats // classe, caractéristiques et PV maximaux compris (à zéro: guerrier neutre)
	HP    int
	Mana  int
	Pos   world.Pos

	// LLMConsent : le joueur a accepté que ses messages soient envoyés à un
	// service de langage tiers pour dialoguer avec les PNJ.
	LLMConsent bool

	// Quêtes et réputation par lieu (voir internal/quest). QuestsDirty indique que la
	// sauvegarde doit les réécrire en base.
	Quests      []quest.Quest
	Rep         map[world.RegionPos]int
	QuestsDirty bool

	// Sac d'équipement et objets portés (voir internal/gear). GearDirty : à réécrire en base.
	Gear      gear.Inventory
	GearDirty bool

	// Donjons découverts (régions qui les cachent). DiscoveredDirty: à réécrire en base.
	Discovered      []world.RegionPos
	DiscoveredDirty bool

	// Point de réapparition (feu de camp) et pierres dressées activées.
	Respawn *world.Pos
	Stones  []world.RegionPos
	Maps    []world.Pos // cartes au trésor détenues : la case à creuser de chacune
}

// Game est l'état du monde vivant.
type Game struct {
	World *world.World

	// Talker, si défini et disponible, fait parler les PNJ avec un modèle de langage
	// (voir talk.go). nil: pas de discussion libre, seulement les menus. À définir avant Run.
	Talker Talker

	// Dungeons active les donjons : marcher sur une entrée emmène dans une instance. Éteint par
	// défaut, tant que le client ne sait pas afficher une carte de donjon.
	Dungeons bool

	geo map[geoKey][]npcai.Landmark // lieux connus des chefs et maires (voir geography.go)

	// Étalages des vendeurs d'équipement (voir shop.go).
	shops        map[world.NPCKey]shopWares
	stockPending map[world.NPCKey]bool
	stockRetryAt map[world.NPCKey]uint64

	// Clock donne l'heure des quêtes (création, remise, expiration). nil: time.Now.
	// À définir avant Run (tests).
	Clock func() time.Time

	// Persist, si défini, est appelé pour enregistrer un joueur (déconnexion,
	// fin de combat, sauvegarde périodique, arrêt). Il est appelé depuis la
	// goroutine de jeu et ne doit jamais bloquer. À définir avant Run.
	Persist func(name string, s Saved)

	inbox chan func()
	done  chan struct{} // fermé quand Run se termine

	// Propriété exclusive de la goroutine de Run :
	players map[int]*player
	idCache []int             // identifiants triés, valide si non nil; invalidé quand players change
	byPos   map[world.Pos]int // index de la carte principale (zone 0); chaque instance a le sien
	nextID  int
	tick    uint64

	metrics metrics // lisible depuis n'importe quelle goroutine

	talkSeq uint64 // dernier numéro de conversation attribué

	monsters      map[int]*monster
	monsterAt     map[world.Pos]int        // idem : carte principale seulement
	slotMonster   map[world.SlotKey]int    // emplacement de forteresse -> monstre vivant
	slotDefeated  map[world.SlotKey]uint64 // emplacement -> tick de la dernière victoire (repousse)
	gd            guardState               // gardes des villages et des villes (voir guards.go)
	garrisonCount int                      // monstres de garnison vivants
	car           caravanState             // marchands ambulants suivis (voir caravan.go)
	lairOpened    map[world.POIKey]uint64  // repaire -> tick de l'ouverture de son coffre (voir poi.go)
	nextMonsterID int

	instances       map[ZoneID]*instance // instances de donjon (voir dungeon.go)
	dungeonZone     map[string]ZoneID    // donjon (identifiant) -> son instance en cours
	nextZone        ZoneID
	dungeonMonsters int // monstres vivants dans les donjons (hors quotas de la carte principale)

	rng *rand.Rand // aléa du jeu (apparitions, errance); injectable en test
}

// New crée un jeu pour le monde donné. Lancer ensuite Run.
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
		lairOpened:    make(map[world.POIKey]uint64),
		nextMonsterID: 1,
		instances:     make(map[ZoneID]*instance),
		dungeonZone:   make(map[string]ZoneID),
		nextZone:      1,
		rng:           rand.New(rand.NewPCG(uint64(w.Seed), 0x9e3779b97f4a7c15)),
	}
}

// Run exécute la boucle de jeu jusqu'à l'annulation de ctx.
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

// do exécute fn dans la goroutine de jeu. Renvoie false si le jeu est arrêté.
// Bloque si la file est pleine (réservé aux commandes qui ne doivent pas être perdues).
func (g *Game) do(fn func()) bool {
	select {
	case g.inbox <- fn:
		return true
	case <-g.done:
		return false
	}
}

// Handlers regroupe les callbacks d'un joueur. Snapshot et Combat sont appelés
// depuis la goroutine de jeu: ils ne doivent jamais bloquer.
type Handlers struct {
	// Lang est la langue des messages adressés au joueur (le zéro vaut le français).
	Lang i18n.Lang
	// State, si non nil, restaure un joueur déjà connu (niveau, PV, position).
	State    *Saved
	Snapshot func(protocol.Snapshot)
	Combat   func(protocol.Combat)    // optionnel
	NPC      func(protocol.NPCDialog) // optionnel
	Quests   func(protocol.Quests)    // optionnel: livre de quêtes
	Bag      func(protocol.Bag)       // optionnel: sac d'équipement
	Zone     func(protocol.Zone)      // optionnel: entrée ou sortie d'un donjon, coffres ouverts
	Dungeons func(protocol.Dungeons)  // optionnel: donjons découverts
	// Joined est appelé dans la goroutine de jeu avant tout instantané, ce qui
	// permet d'envoyer le message de bienvenue avant le premier snapshot.
	Joined func(id, x, y int)
}

// Join ajoute un joueur et renvoie son identifiant.
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
		if h.Joined != nil {
			h.Joined(p.id, p.pos.X, p.pos.Y)
		}
		g.sendQuests(p) // après le message de bienvenue
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

// Leave retire un joueur et n'attend que le traitement du départ (y compris
// l'appel à Persist). Renvoie immédiatement si le jeu est arrêté.
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

// Move enregistre l'intention de déplacement d'un joueur (la dernière écrase
// les précédentes). Peut être perdue si le serveur est saturé.
func (g *Game) Move(id, dx, dy int) {
	select {
	case g.inbox <- func() { g.move(id, dx, dy) }:
	default:
	}
}

// Nearby renvoie les identifiants des joueurs dans la zone de vision du joueur
// id (lui-même inclus). Sert à la portée du chat local.
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

// --- Fonctions internes, appelées uniquement depuis la goroutine de jeu ---

// sees indique si b est dans la zone de vision de a.
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

// joinWith ajoute un joueur, neuf ou restauré depuis saved. Une position
// sauvegardée devenue occupée est remplacée par la case libre la plus proche;
// les valeurs hors bornes sont ramenées dans les bornes valides.
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
		for _, rp := range saved.Stones {
			p.stones[rp] = true
		}
		p.gearDirty = saved.GearDirty // objets ajoutés avant la connexion (arme de départ): à écrire à la première sauvegarde
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

// save transmet l'état du joueur au callback de persistance.
func (g *Game) save(p *player) {
	if g.Persist != nil {
		// Les quêtes sont toujours jointes (une reconnexion avant l'écriture doit les retrouver) ;
		// QuestsDirty dit seulement s'il faut les réécrire en base.
		g.Persist(p.name, Saved{
			Stats: p.stats, HP: p.hp, Mana: p.mana, Pos: p.surfacePos(), LLMConsent: p.consent,
			Quests: slices.Clone(p.quests), Rep: maps.Clone(p.rep), QuestsDirty: p.questsDirty,
			Gear: p.inv.Clone(), GearDirty: p.gearDirty,
			Discovered: p.discoveredList(), DiscoveredDirty: p.discoveredDirty,
			Respawn: p.respawn, Stones: p.stoneList(), Maps: slices.Clone(p.maps),
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

// setPos place p en pos en maintenant l'index des positions.
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

// blocked indique si une tuile de la carte principale est infranchissable (terrain, joueur,
// monstre ou PNJ).
func (g *Game) blocked(pos world.Pos) bool { return g.blockedIn(0, pos) }

// blockedIn est blocked pour la zone z : la carte principale (0) ou l'instance d'un donjon.
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
	if g.guardAt(pos) != nil { // un garde aussi
		return true
	}
	if g.caravanAt(pos) != nil { // et une caravane
		return true
	}
	_, npc := g.World.NPCAt(pos.X, pos.Y) // un PNJ occupe sa case
	return npc
}

// freeSpawn renvoie une case libre et praticable proche du point de départ du monde.
func (g *Game) freeSpawn() world.Pos { return g.freeNear(g.World.FindSpawn()) }

// freeNear renvoie la case libre et praticable la plus proche de c (en anneaux).
func (g *Game) freeNear(c world.Pos) world.Pos { return g.freeNearIn(0, c, 0) }

// freeNearIn est freeNear dans la zone z, en ignorant les cases à moins de minR de c.
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

// step avance le monde d'un tick.
func (g *Game) step() {
	g.tick++

	ids := g.sortedIDs()

	// Déplacements, par identifiant croissant pour un arbitrage déterministe.
	for _, id := range ids {
		p := g.players[id]
		if p.cooldown > 0 {
			p.cooldown--
		}
		if p.expireBuffs(g.tick) {
			g.sendBag(p) // la fiche de personnage ne doit plus afficher la bénédiction
		}
		if p.fight != nil || p.talk != nil { // en combat ou en conversation: immobile
			p.hasPending = false
			continue
		}
		if !p.hasPending || p.cooldown > 0 {
			continue
		}
		p.hasPending = false
		dst := world.Pos{X: p.pos.X + p.pendingDX, Y: p.pos.Y + p.pendingDY}
		if mid, ok := g.monsterIndex(p.zone)[dst]; ok { // marcher sur un monstre l'attaque
			g.startFight(p, g.monsters[mid], true)
			continue
		}
		if p.zone == 0 {
			if c := g.caravanAt(dst); c != nil { // marcher sur une caravane ouvre sa boutique
				g.bumpCaravan(p, c)
				continue
			}
			if gd := g.guardAt(dst); gd != nil { // marcher sur un garde lui parle (ou l'affronte, si l'on est hors-la-loi)
				g.bumpGuard(p, gd)
				continue
			}
			if npc, ok := g.World.NPCAt(dst.X, dst.Y); ok { // marcher sur un PNJ lui adresse la parole
				g.startTalk(p, npc)
				continue
			}
			// L'entrée secrète d'un donjon est une case ordinaire : on y entre en marchant dessus.
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
			g.openChest(p, inst, dst) // marcher sur un coffre l'ouvre
		} else if p.zone == 0 {
			g.poiStep(p, dst) // feu de camp, sanctuaire, repaire, tour, pierre dressée
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

	// Monstres: déplacement, puis apparitions/disparitions périodiques.
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

	// Instantanés, uniquement si ce que voit le joueur a changé.
	for _, id := range ids {
		g.sendSnapshot(g.players[id], mids)
	}

	if g.tick%unloadEvery == 0 {
		g.unloadChunks()
	}
}

// sortedIDs renvoie les identifiants des joueurs, triés. Le résultat est mis en
// cache jusqu'au prochain départ ou arrivée: il est consulté des milliers de
// fois par tick (une fois par monstre et par joueur) et ne doit pas être
// retrié à chaque appel. L'appelant ne doit pas modifier la tranche renvoyée.
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

// snapshotFor construit ce que p voit (zone de vision rectangulaire).
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

// unloadChunks libère les chunks éloignés de tous les joueurs.
func (g *Game) unloadChunks() {
	keep := make(map[world.ChunkPos]bool)
	for _, p := range g.players {
		sp := p.surfacePos() // en donjon, on garde les abords de l'entrée
		c := world.ChunkOf(sp.X, sp.Y)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				keep[world.ChunkPos{X: c.X + dx, Y: c.Y + dy}] = true
			}
		}
	}
	g.World.Unload(keep)
}

// metrics contient les compteurs de santé de la boucle (écrits par la
// goroutine de jeu, lus par n'importe qui sans verrou).
type metrics struct {
	ticks     atomic.Uint64
	slowTicks atomic.Uint64 // ticks plus longs que TickInterval
	totalNs   atomic.Uint64
	maxNs     atomic.Uint64
	players   atomic.Int64
	monsters  atomic.Int64

	// Fenêtre glissante des dernières durées de tick (goroutine de jeu seulement),
	// résumée dans les atomiques ci-dessous: on voit ainsi le régime établi,
	// sans que le démarrage ne masque la mesure.
	ring      [recentTicks]time.Duration
	recentP99 atomic.Uint64
	recentMax atomic.Uint64
}

const (
	recentTicks    = 600 // 60 s de ticks
	summarizeEvery = 50
)

// Metrics est une copie des compteurs de la boucle de jeu.
type Metrics struct {
	Ticks     uint64        // ticks exécutés depuis le démarrage
	SlowTicks uint64        // ticks ayant dépassé TickInterval (la cadence a alors fléchi)
	TickTotal time.Duration // temps cumulé passé dans les ticks
	TickMax   time.Duration // tick le plus long depuis le démarrage
	RecentP99 time.Duration // 99e centile des 60 dernières secondes
	RecentMax time.Duration // tick le plus long des 60 dernières secondes
	Players   int
	Monsters  int
}

// Metrics renvoie l'état des compteurs. Sûr depuis n'importe quelle goroutine.
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

// timedStep exécute un tick et enregistre sa durée.
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
	for { // maximum sans verrou
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

// summarize calcule le p99 et le maximum des n dernières durées.
func (m *metrics) summarize(n int) {
	buf := make([]time.Duration, n)
	copy(buf, m.ring[:n]) // l'ordre importe peu pour un centile; avant 600 ticks, seules les n premières cases sont remplies
	slices.Sort(buf)
	m.recentP99.Store(uint64(buf[min(n-1, n*99/100)]))
	m.recentMax.Store(uint64(buf[n-1]))
}

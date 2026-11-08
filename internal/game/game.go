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
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
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
	pos  world.Pos
	send func(protocol.Snapshot)

	safe       bool                     // en zone sûre; recalculé à chaque changement de case (voir setPos)
	sendCombat func(protocol.Combat)    // peut être nil
	sendNPC    func(protocol.NPCDialog) // peut être nil
	talk       *talk                    // conversation en cours avec un PNJ
	consent    bool                     // a accepté l'envoi de ses messages à un service de langage
	stats      combat.Stats
	hp         int
	fight      *fight

	hasPending bool
	pendingDX  int
	pendingDY  int
	cooldown   int

	lastSent *protocol.Snapshot // dernier instantané envoyé (sans tick)
}

// Saved est l'état persistant d'un joueur: ce qu'il faut pour le retrouver
// à sa reconnexion.
type Saved struct {
	Stats combat.Stats
	HP    int
	Pos   world.Pos

	// LLMConsent : le joueur a accepté que ses messages soient envoyés à un
	// service de langage tiers pour dialoguer avec les PNJ.
	LLMConsent bool
}

// Game est l'état du monde vivant.
type Game struct {
	World *world.World

	// Talker, si défini et disponible, fait parler les PNJ avec un modèle de langage
	// (voir talk.go). nil: pas de discussion libre, seulement les menus. À définir avant Run.
	Talker Talker

	// Persist, si défini, est appelé pour enregistrer un joueur (déconnexion,
	// fin de combat, sauvegarde périodique, arrêt). Il est appelé depuis la
	// goroutine de jeu et ne doit jamais bloquer. À définir avant Run.
	Persist func(name string, s Saved)

	inbox chan func()
	done  chan struct{} // fermé quand Run se termine

	// Propriété exclusive de la goroutine de Run :
	players map[int]*player
	idCache []int // identifiants triés, valide si non nil; invalidé quand players change
	byPos   map[world.Pos]int
	nextID  int
	tick    uint64

	metrics metrics // lisible depuis n'importe quelle goroutine

	talkSeq uint64 // dernier numéro de conversation attribué

	monsters      map[int]*monster
	monsterAt     map[world.Pos]int
	slotMonster   map[world.SlotKey]int    // emplacement de forteresse -> monstre vivant
	slotDefeated  map[world.SlotKey]uint64 // emplacement -> tick de la dernière victoire (repousse)
	garrisonCount int                      // monstres de garnison vivants
	nextMonsterID int
	rng           *rand.Rand // aléa du jeu (apparitions, errance); injectable en test
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
		nextMonsterID: 1,
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
	// State, si non nil, restaure un joueur déjà connu (niveau, PV, position).
	State    *Saved
	Snapshot func(protocol.Snapshot)
	Combat   func(protocol.Combat)    // optionnel
	NPC      func(protocol.NPCDialog) // optionnel
	// Joined est appelé dans la goroutine de jeu avant tout instantané, ce qui
	// permet d'envoyer le message de bienvenue avant le premier snapshot.
	Joined func(id, x, y int)
}

// Join ajoute un joueur et renvoie son identifiant.
func (g *Game) Join(name string, h Handlers) (int, error) {
	reply := make(chan int, 1)
	if !g.do(func() {
		p := g.joinWith(name, h.Snapshot, h.State)
		p.sendCombat = h.Combat
		p.sendNPC = h.NPC
		if h.Joined != nil {
			h.Joined(p.id, p.pos.X, p.pos.Y)
		}
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
	return abs(a.pos.X-b.pos.X) <= protocol.ViewHalfW && abs(a.pos.Y-b.pos.Y) <= protocol.ViewHalfH
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
	p := &player{id: g.nextID, name: name, send: send, stats: combat.NewStats()}
	p.hp = p.stats.MaxHP
	start := g.World.FindSpawn()
	if saved != nil {
		p.stats = combat.StatsAt(saved.Stats.Level, saved.Stats.XP, saved.Stats.Potions).
			WithGold(saved.Stats.Gold).
			WithRecord(saved.Stats.Kills, saved.Stats.BossKills, saved.Stats.Deaths)
		p.consent = saved.LLMConsent
		p.hp = max(1, min(saved.HP, p.stats.MaxHP))
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
		g.Persist(p.name, Saved{Stats: p.stats, HP: p.hp, Pos: p.pos, LLMConsent: p.consent})
	}
}

func (g *Game) leave(id int) {
	p, ok := g.players[id]
	if !ok {
		return
	}
	g.abortFight(p)
	g.save(p)
	delete(g.byPos, p.pos)
	delete(g.players, id)
	g.idCache = nil
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
	if cur, ok := g.byPos[p.pos]; ok && cur == p.id {
		delete(g.byPos, p.pos)
	}
	p.pos = pos
	p.safe = g.World.Safe(pos.X, pos.Y)
	g.byPos[pos] = p.id
}

// blocked indique si une tuile est infranchissable (terrain, joueur, monstre ou PNJ).
func (g *Game) blocked(pos world.Pos) bool {
	if !g.World.At(pos.X, pos.Y).Walkable() {
		return true
	}
	if _, occupied := g.byPos[pos]; occupied {
		return true
	}
	if _, occupied := g.monsterAt[pos]; occupied {
		return true
	}
	_, npc := g.World.NPCAt(pos.X, pos.Y) // un PNJ occupe sa case
	return npc
}

// freeSpawn renvoie une case libre et praticable proche du point de départ du monde.
func (g *Game) freeSpawn() world.Pos { return g.freeNear(g.World.FindSpawn()) }

// freeNear renvoie la case libre et praticable la plus proche de c (en anneaux).
func (g *Game) freeNear(c world.Pos) world.Pos {
	for r := 0; r <= spawnSearchMax; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r {
					continue
				}
				if pos := (world.Pos{X: c.X + dx, Y: c.Y + dy}); !g.blocked(pos) {
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
		if p.fight != nil || p.talk != nil { // en combat ou en conversation: immobile
			p.hasPending = false
			continue
		}
		if !p.hasPending || p.cooldown > 0 {
			continue
		}
		p.hasPending = false
		dst := world.Pos{X: p.pos.X + p.pendingDX, Y: p.pos.Y + p.pendingDY}
		if mid, ok := g.monsterAt[dst]; ok { // marcher sur un monstre l'attaque
			g.startFight(p, g.monsters[mid], true)
			continue
		}
		if npc, ok := g.World.NPCAt(dst.X, dst.Y); ok { // marcher sur un PNJ lui adresse la parole
			g.startTalk(p, npc)
			continue
		}
		if g.blocked(dst) {
			continue
		}
		g.setPos(p, dst)
		p.cooldown = MoveCooldown
	}

	g.expireIdleFights(ids)
	g.expireIdleTalks(ids)
	if g.tick%safeRegenEvery == 0 {
		g.regenerate(g.healDue(ids))
	}
	if g.tick%autosaveEvery == 0 {
		for _, id := range ids {
			g.save(g.players[id])
		}
	}

	// Monstres: déplacement, puis apparitions/disparitions périodiques.
	mids := g.sortedMonsterIDs()
	g.stepMonsters(mids)
	if g.tick%spawnEvery == 0 {
		g.despawnFar()
		g.spawnMonsters()
		g.activateGarrisons()
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
		if inBox(m.pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
			snap.Monsters = append(snap.Monsters, protocol.Monster{ID: m.id, Kind: m.sp.Kind, X: m.pos.X, Y: m.pos.Y, Fighting: m.fight != nil})
		}
	}
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
		c := world.ChunkOf(p.pos.X, p.pos.Y)
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

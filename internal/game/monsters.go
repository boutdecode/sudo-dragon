package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// species describes a monster species.
type species struct {
	Kind      string     // network identifier ("rat")
	Habitat   world.Tile // terrain where it spawns
	MoveEvery int        // ticks between two steps (for a player: MoveCooldown)
	Aggro     int        // detection radius in tiles (Chebyshev)

	// Combat sheet (see internal/combat): DEF = size base + DexMod, to-hit = d20 + Hit,
	// damage Dmg minus the opponent's Reduction dice. And the reward.
	HP        int
	Size      hero.Size
	DexMod    int
	Hit       int
	Dmg       dice.Dice
	Reduction dice.Dice
	XP        int

	// Garrison: only spawns in a fortress (never by biome).
	// Boss: unique monster of a fortress, with improved loot.
	Garrison bool
	Boss     bool

	// Dungeon: only spawns in a dungeon (never by biome). Dungeon monsters are scaled to the
	// dungeon's tier (see scaledForTier).
	Dungeon bool

	// Level is the region level the species was scaled to (see scaledForLevel); 0 for
	// dungeon monsters, which follow their dungeon's tier.
	Level int
}

// All monsters are slower than a player (MoveEvery > MoveCooldown):
// you can always outrun them.
var speciesList = []species{
	{Kind: "rat", Habitat: world.Plain, MoveEvery: 6, Aggro: 5, HP: 8, Size: hero.Small, Hit: 1, Dmg: dice.D(1, 4, 0), XP: 4},
	{Kind: "crab", Habitat: world.Sand, MoveEvery: 5, Aggro: 3, HP: 12, Size: hero.Small, Hit: 2, Dmg: dice.D(1, 6, 0), Reduction: dice.D(1, 4, 0), XP: 6},
	{Kind: "wolf", Habitat: world.Forest, MoveEvery: 3, Aggro: 9, HP: 16, Size: hero.Medium, DexMod: 1, Hit: 3, Dmg: dice.D(1, 6, 1), XP: 10},
	{Kind: "goblin", Habitat: world.Hills, MoveEvery: 4, Aggro: 8, HP: 22, Size: hero.Medium, Hit: 3, Dmg: dice.D(1, 8, 0), XP: 15},
	{Kind: "yeti", Habitat: world.Snow, MoveEvery: 4, Aggro: 7, HP: 34, Size: hero.Large, Hit: 5, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 30},

	// Biomes (see world/terrain2.go).
	{Kind: "scorpion", Habitat: world.Desert, MoveEvery: 4, Aggro: 4, HP: 14, Size: hero.Small, Hit: 3, Dmg: dice.D(1, 6, 0), Reduction: dice.D(1, 4, 0), XP: 12},
	{Kind: "panther", Habitat: world.Jungle, MoveEvery: 3, Aggro: 9, HP: 24, Size: hero.Medium, DexMod: 2, Hit: 4, Dmg: dice.D(1, 8, 1), XP: 20},
	{Kind: "toad", Habitat: world.Swamp, MoveEvery: 5, Aggro: 4, HP: 14, Size: hero.Small, Hit: 2, Dmg: dice.D(1, 6, 0), XP: 8},
	{Kind: "bear", Habitat: world.Tundra, MoveEvery: 4, Aggro: 6, HP: 30, Size: hero.Large, Hit: 4, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 22},

	// Fortress garrisons: no habitat, see garrison.go.
	{Kind: "skeleton", Garrison: true, MoveEvery: 4, Aggro: 7, HP: 36, Size: hero.Medium, Hit: 4, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 25},
	{Kind: "knight", Garrison: true, MoveEvery: 4, Aggro: 7, HP: 90, Size: hero.Medium, DexMod: 2, Hit: 7, Dmg: dice.D(2, 8, 0), Reduction: dice.D(1, 8, 0), XP: 50},
	{Kind: "warlord", Garrison: true, Boss: true, MoveEvery: 5, Aggro: 6, HP: 180, Size: hero.Large, DexMod: 1, Hit: 8, Dmg: dice.D(3, 10, 3), Reduction: dice.D(2, 6, 0), XP: 250},

	// Dungeons (see dungeon.go): in addition to skeletons and knights.
	{Kind: "wraith", Dungeon: true, MoveEvery: 4, Aggro: 7, HP: 30, Size: hero.Medium, DexMod: 2, Hit: 5, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 30},
	{Kind: "golem", Dungeon: true, MoveEvery: 6, Aggro: 5, HP: 110, Size: hero.Large, DexMod: -1, Hit: 6, Dmg: dice.D(2, 10, 0), Reduction: dice.D(1, 10, 0), XP: 60},
	{Kind: "lich", Dungeon: true, Boss: true, MoveEvery: 5, Aggro: 7, HP: 200, Size: hero.Medium, DexMod: 2, Hit: 8, Dmg: dice.D(3, 10, 4), Reduction: dice.D(2, 6, 0), XP: 300},
}

var speciesByTile = func() map[world.Tile][]species {
	m := make(map[world.Tile][]species)
	for _, s := range speciesList {
		if s.Garrison || s.Dungeon { // garrisons and dungeons do not spawn by biome
			continue
		}
		m[s.Habitat] = append(m[s.Habitat], s)
	}
	return m
}()

// companions gives, for each species, the other species it plausibly fights alongside
// (the relation is symmetrized by canGroup). Each species also fights alongside its own kind.
var companions = map[string][]string{
	"wolf":     {"rat"},    // rats lurk in the wake of wolves
	"goblin":   {"wolf"},   // the goblin and its wolves
	"yeti":     {"wolf"},   // snow wolves
	"skeleton": {"knight"}, // garrison
	"warlord":  {"knight", "skeleton"},
	"wraith":   {"skeleton"}, // dungeon
	"golem":    {"knight", "wraith"},
	"lich":     {"golem", "wraith"},
}

// canGroup says whether two species can fight together.
func canGroup(a, b string) bool {
	return a == b || slices.Contains(companions[a], b) || slices.Contains(companions[b], a)
}

// Group settings.
const (
	maxGroup    = 3 // at most this many monsters in a single fight
	maxGarrison = 2 // same for a fortress garrison (already formidable alone)
	packLevels  = 3 // a player only meets packs with one more companion every packLevels levels
	groupRadius = 5 // distance (Chebyshev) at which a free monster joins a fight
)

// packExtra gives the maximum number of companions that spawn with a biome species.
var packExtra = map[string]int{"rat": 2, "crab": 1, "wolf": 2, "goblin": 1, "yeti": 0, "scorpion": 1, "panther": 1, "toad": 2, "bear": 0}

// gatherGroup returns m and the free, compatible monsters around it (at most maxGroup in total, the
// closest first).
func (g *Game) gatherGroup(m *monster) []*monster {
	var near []*monster
	for _, id := range g.sortedMonsterIDs() {
		o := g.monsters[id]
		if o == m || o.zone != m.zone || o.fight != nil || o.calm > 0 || cheb(o.pos, m.pos) > groupRadius || !canGroup(m.sp.Kind, o.sp.Kind) {
			continue
		}
		near = append(near, o)
	}
	slices.SortStableFunc(near, func(a, b *monster) int { return cheb(a.pos, m.pos) - cheb(b.pos, m.pos) })
	limit := maxGroup
	if m.sp.Garrison || m.sp.Dungeon {
		limit = maxGarrison
	}
	return append([]*monster{m}, near[:min(len(near), limit-1)]...)
}

// spawnPack spawns companions around m, according to the species and the player's level (without exceeding the quota around around), on free tiles of its habitat.
func (g *Game) spawnPack(m *monster, around world.Pos, level int) {
	n := g.rng.IntN(min(packExtra[m.sp.Kind], level/packLevels) + 1) // no pack for a beginner
	for range n {
		if g.monstersAround(around) >= maxPerPlayer || g.biomeCount() >= maxMonsters {
			return // the pack obeys the same quotas as lone monsters
		}
		for range spawnTries {
			d := dirs8[g.rng.IntN(len(dirs8))]
			pos := world.Pos{X: m.pos.X + d[0]*(1+g.rng.IntN(2)), Y: m.pos.Y + d[1]*(1+g.rng.IntN(2))}
			if _, taken := g.monsterAt[pos]; taken || g.blocked(pos) || g.World.Safe(pos.X, pos.Y) || g.World.POIFootprintAt(pos.X, pos.Y) != nil || g.seenByAnyPlayer(pos) {
				continue
			}
			// Same species, or (rarely) a companion species whose habitat is that of the tile.
			sp := m.sp
			lvl := world.LevelAt(pos.X, pos.Y)
			if cs := companions[m.sp.Kind]; len(cs) > 0 && g.rng.IntN(4) == 0 {
				for _, c := range speciesList {
					if c.Kind == cs[g.rng.IntN(len(cs))] && !c.Garrison && c.Habitat == g.World.At(pos.X, pos.Y) {
						sp = scaledForLevel(c, lvl)
					}
				}
			}
			if sp.Habitat != g.World.At(pos.X, pos.Y) {
				continue
			}
			c := g.addMonster(sp, pos)
			c.home = m.home
			break
		}
	}
}

// SpeciesKinds lists the species identifiers (to check that the client
// can draw them all).
func SpeciesKinds() []string {
	kinds := make([]string, len(speciesList))
	for i, s := range speciesList {
		kinds[i] = s.Kind
	}
	return kinds
}

// SpeciesName returns the displayed name of a species in language l ("" if unknown).
func SpeciesName(l i18n.Lang, kind string) string {
	for _, s := range speciesList {
		if s.Kind == kind {
			return s.name(l)
		}
	}
	return ""
}

// fighter converts the species into a fighter (full HP).
func (s species) fighter() combat.Fighter {
	return combat.Fighter{HP: s.HP, MaxHP: s.HP, Def: hero.DefBase(s.Size) + s.DexMod, Init: s.DexMod, Hit: s.Hit, Dmg: s.Dmg, Reduction: s.Reduction, Kind: hero.Melee}
}

// name returns the species name in language l (key "monster.<kind>").
func (s species) name(l i18n.Lang) string { return i18n.T(l, "monster."+s.Kind) }

// Spawn settings. The spawn area is contained within the
// retention area, otherwise a monster would be removed as soon as it is created.
const (
	spawnEvery      = 20 // ticks between two spawn attempts (2 s)
	spawnHalfW      = 60
	spawnHalfH      = 30
	keepHalfW       = 70
	keepHalfH       = 40
	spawnViewMargin = 2 // margin outside the view where a monster cannot spawn
	maxPerPlayer    = 8
	maxMonsters     = 500
	spawnTries      = 10
	wanderLeash     = 8 // maximum distance from the origin when wandering
	garrisonLeash   = 4 // same for a garrison monster
)

type monster struct {
	id       int
	zone     ZoneID // 0: main map; otherwise a dungeon instance (pos is then local)
	sp       species
	pos      world.Pos
	home     world.Pos
	cooldown int    // ticks before the next step
	calm     int    // ticks during which it stays inert and does not engage (after a flight or a defeat)
	fight    *fight // non-nil during a fight: the monster is frozen

	// Garrison monster: occupies a fortress slot, does not leave its
	// footprint and wanders little around its post. nil/0 for a biome monster.
	slot   *world.SlotKey
	bounds *world.Structure
	leash  int

	// guard: this avatar lends its sheet to a village guard for a fight (see guards.go). It is never
	// stored in g.monsters.
	guard *guard
}

// leashDist is the maximum distance the monster strays from its origin while wandering.
func (m *monster) leashDist() int {
	if m.leash > 0 {
		return m.leash
	}
	return wanderLeash
}

var dirs8 = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func cheb(a, b world.Pos) int { return max(abs(a.X-b.X), abs(a.Y-b.Y)) }

func (g *Game) sortedMonsterIDs() []int {
	ids := make([]int, 0, len(g.monsters))
	for id := range g.monsters {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (g *Game) addMonster(sp species, pos world.Pos) *monster { return g.addMonsterIn(0, sp, pos) }

// addMonsterIn creates a monster in zone z (0: main map).
func (g *Game) addMonsterIn(z ZoneID, sp species, pos world.Pos) *monster {
	m := &monster{id: g.nextMonsterID, zone: z, sp: sp, pos: pos, home: pos}
	g.nextMonsterID++
	g.monsters[m.id] = m
	g.monsterIndex(z)[pos] = m.id
	if z != 0 {
		g.dungeonMonsters++
	}
	return m
}

// biomeCount counts living biome monsters: neither garrisons nor dungeon monsters.
func (g *Game) biomeCount() int { return len(g.monsters) - g.garrisonCount - g.dungeonMonsters }

func (g *Game) removeMonster(m *monster) {
	delete(g.monsterIndex(m.zone), m.pos)
	delete(g.monsters, m.id)
	if m.zone != 0 {
		g.dungeonMonsters--
	}
	if m.slot != nil { // frees the fortress slot
		delete(g.slotMonster, *m.slot)
		g.garrisonCount--
	}
}

func (g *Game) moveMonster(m *monster, dx, dy int) bool {
	if dx == 0 && dy == 0 {
		return false
	}
	dst := world.Pos{X: m.pos.X + dx, Y: m.pos.Y + dy}
	if g.blockedIn(m.zone, dst) || (m.zone == 0 && g.World.Safe(dst.X, dst.Y)) { // safe zones are off-limits to monsters
		return false
	}
	if m.bounds != nil && !m.bounds.Contains(dst.X, dst.Y) { // a garrison does not leave its fortress
		return false
	}
	at := g.monsterIndex(m.zone)
	delete(at, m.pos)
	m.pos = dst
	at[dst] = m.id
	return true
}

// nearestPlayer returns the nearest free player within aggro range (tie: the oldest).
func (g *Game) nearestPlayer(z ZoneID, pos world.Pos, aggro int) *player {
	var best *player
	bestD := aggro + 1
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != z || p.fight != nil {
			continue // other zone, or already in a fight: no piling on
		}
		if p.calmUntil > g.tick {
			continue // it just fled: let it catch its breath
		}
		if p.safe {
			continue // a player in a safe zone is neither chased nor attacked
		}
		if d := cheb(pos, p.pos); d < bestD {
			best, bestD = p, d
		}
	}
	return best
}

// stepMonsters makes each monster act: chase if a player is in range,
// otherwise wander near the origin point.
func (g *Game) stepMonsters(ids []int) {
	// A dungeon with no player is frozen: it keeps its state without using any compute time.
	occupied := make(map[ZoneID]bool)
	for _, p := range g.players {
		occupied[p.zone] = true
	}
	for _, id := range ids {
		m := g.monsters[id]
		if m.fight != nil || (m.zone != 0 && !occupied[m.zone]) {
			continue
		}
		if m.calm > 0 {
			m.calm--
			if m.calm > 0 {
				continue
			}
		}
		if m.cooldown > 0 {
			m.cooldown--
		}
		if m.cooldown > 0 {
			continue
		}

		moved := false
		if target := g.nearestPlayer(m.zone, m.pos, m.sp.Aggro); target != nil {
			if cheb(m.pos, target.pos) <= 1 {
				g.startFight(target, m, false) // in contact: it attacks
				continue
			}
			dx, dy := sign(target.pos.X-m.pos.X), sign(target.pos.Y-m.pos.Y)
			// Direct step, otherwise go around by sliding along an axis.
			moved = g.moveMonster(m, dx, dy) || g.moveMonster(m, dx, 0) || g.moveMonster(m, 0, dy)
		} else if g.rng.IntN(3) == 0 {
			d := dirs8[g.rng.IntN(len(dirs8))]
			dst := world.Pos{X: m.pos.X + d[0], Y: m.pos.Y + d[1]}
			if cheb(dst, m.home) <= m.leashDist() {
				moved = g.moveMonster(m, d[0], d[1])
			}
		}
		if moved {
			m.cooldown = m.sp.MoveEvery
		}
	}
}

func inBox(a, b world.Pos, halfW, halfH int) bool {
	return abs(a.X-b.X) <= halfW && abs(a.Y-b.Y) <= halfH
}

// seenByAnyPlayer tells whether pos is within a player's view (margin included).
func (g *Game) seenByAnyPlayer(pos world.Pos) bool {
	for _, p := range g.players {
		if p.zone == 0 && inBox(pos, p.pos, protocol.ViewHalfW+spawnViewMargin, protocol.ViewHalfH+spawnViewMargin) {
			return true
		}
	}
	return false
}

func (g *Game) monstersAround(pos world.Pos) int {
	n := 0
	for _, m := range g.monsters {
		if m.zone == 0 && m.slot == nil && inBox(m.pos, pos, spawnHalfW, spawnHalfH) { // the garrison does not count toward the biome quota
			n++
		}
	}
	return n
}

// despawnFar removes monsters far from all players.
func (g *Game) despawnFar() {
	for _, id := range g.sortedMonsterIDs() {
		m := g.monsters[id]
		if m.fight != nil || m.zone != 0 {
			continue // never mid-fight; dungeon monsters live and die with their instance
		}
		near := false
		for _, p := range g.players {
			if p.zone == 0 && inBox(m.pos, p.pos, keepHalfW, keepHalfH) {
				near = true
				break
			}
		}
		if !near {
			g.removeMonster(m)
		}
	}
}

// spawnMonsters tries to spawn a monster around each player,
// out of any view, on the species' habitat terrain.
func (g *Game) spawnMonsters() {
	for _, id := range g.sortedIDs() {
		if g.biomeCount() >= maxMonsters {
			return
		}
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		if g.monstersAround(p.pos) >= maxPerPlayer {
			continue
		}
		for range spawnTries {
			pos := world.Pos{
				X: p.pos.X + g.rng.IntN(2*spawnHalfW+1) - spawnHalfW,
				Y: p.pos.Y + g.rng.IntN(2*spawnHalfH+1) - spawnHalfH,
			}
			if g.seenByAnyPlayer(pos) || g.blocked(pos) || g.World.Safe(pos.X, pos.Y) || g.World.POIFootprintAt(pos.X, pos.Y) != nil {
				continue
			}
			sps := speciesByTile[g.World.At(pos.X, pos.Y)]
			if len(sps) == 0 {
				continue
			}
			sp := scaledForLevel(sps[g.rng.IntN(len(sps))], world.LevelAt(pos.X, pos.Y))
			g.spawnPack(g.addMonster(sp, pos), p.pos, p.stats.Level)
			break
		}
	}
}

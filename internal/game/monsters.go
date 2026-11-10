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

// species décrit une espèce de monstre.
type species struct {
	Kind      string     // identifiant réseau ("rat")
	Habitat   world.Tile // terrain où elle apparaît
	MoveEvery int        // ticks entre deux pas (un joueur: MoveCooldown)
	Aggro     int        // rayon de détection en tuiles (Chebyshev)

	// Fiche de combat (voir internal/combat): DEF = base de la taille + DexMod, toucher = d20 + Hit,
	// dégâts Dmg moins les dés de Reduction de l'adversaire. Et la récompense.
	HP        int
	Size      hero.Size
	DexMod    int
	Hit       int
	Dmg       dice.Dice
	Reduction dice.Dice
	XP        int

	// Garrison: n'apparaît que dans une forteresse (jamais selon le biome).
	// Boss: monstre unique d'une forteresse, au butin amélioré.
	Garrison bool
	Boss     bool

	// Dungeon: n'apparaît que dans un donjon (jamais selon le biome). Les monstres de donjon sont mis à l'échelle
	// du palier du donjon (voir scaledForTier).
	Dungeon bool

	// Level est le niveau de région auquel l'espèce a été mise à l'échelle (voir scaledForLevel); 0 pour les
	// monstres de donjon, qui suivent le palier de leur donjon.
	Level int
}

// Tous les monstres sont plus lents qu'un joueur (MoveEvery > MoveCooldown):
// on peut toujours leur échapper en courant.
var speciesList = []species{
	{Kind: "rat", Habitat: world.Plain, MoveEvery: 6, Aggro: 5, HP: 8, Size: hero.Small, Hit: 1, Dmg: dice.D(1, 4, 0), XP: 4},
	{Kind: "crab", Habitat: world.Sand, MoveEvery: 5, Aggro: 3, HP: 12, Size: hero.Small, Hit: 2, Dmg: dice.D(1, 6, 0), Reduction: dice.D(1, 4, 0), XP: 6},
	{Kind: "wolf", Habitat: world.Forest, MoveEvery: 3, Aggro: 9, HP: 16, Size: hero.Medium, DexMod: 1, Hit: 3, Dmg: dice.D(1, 6, 1), XP: 10},
	{Kind: "goblin", Habitat: world.Hills, MoveEvery: 4, Aggro: 8, HP: 22, Size: hero.Medium, Hit: 3, Dmg: dice.D(1, 8, 0), XP: 15},
	{Kind: "yeti", Habitat: world.Snow, MoveEvery: 4, Aggro: 7, HP: 34, Size: hero.Large, Hit: 5, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 30},

	// Biomes du générateur v2 (voir world/terrain2.go).
	{Kind: "scorpion", Habitat: world.Desert, MoveEvery: 4, Aggro: 4, HP: 14, Size: hero.Small, Hit: 3, Dmg: dice.D(1, 6, 0), Reduction: dice.D(1, 4, 0), XP: 12},
	{Kind: "panther", Habitat: world.Jungle, MoveEvery: 3, Aggro: 9, HP: 24, Size: hero.Medium, DexMod: 2, Hit: 4, Dmg: dice.D(1, 8, 1), XP: 20},
	{Kind: "toad", Habitat: world.Swamp, MoveEvery: 5, Aggro: 4, HP: 14, Size: hero.Small, Hit: 2, Dmg: dice.D(1, 6, 0), XP: 8},
	{Kind: "bear", Habitat: world.Tundra, MoveEvery: 4, Aggro: 6, HP: 30, Size: hero.Large, Hit: 4, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 22},

	// Garnisons de forteresse: sans habitat, voir garrison.go.
	{Kind: "skeleton", Garrison: true, MoveEvery: 4, Aggro: 7, HP: 36, Size: hero.Medium, Hit: 4, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 25},
	{Kind: "knight", Garrison: true, MoveEvery: 4, Aggro: 7, HP: 90, Size: hero.Medium, DexMod: 2, Hit: 7, Dmg: dice.D(2, 8, 0), Reduction: dice.D(1, 8, 0), XP: 50},
	{Kind: "warlord", Garrison: true, Boss: true, MoveEvery: 5, Aggro: 6, HP: 180, Size: hero.Large, DexMod: 1, Hit: 8, Dmg: dice.D(3, 10, 3), Reduction: dice.D(2, 6, 0), XP: 250},

	// Donjons (voir dungeon.go): en plus des squelettes et des chevaliers.
	{Kind: "wraith", Dungeon: true, MoveEvery: 4, Aggro: 7, HP: 30, Size: hero.Medium, DexMod: 2, Hit: 5, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 4, 0), XP: 30},
	{Kind: "golem", Dungeon: true, MoveEvery: 6, Aggro: 5, HP: 110, Size: hero.Large, DexMod: -1, Hit: 6, Dmg: dice.D(2, 10, 0), Reduction: dice.D(1, 10, 0), XP: 60},
	{Kind: "lich", Dungeon: true, Boss: true, MoveEvery: 5, Aggro: 7, HP: 200, Size: hero.Medium, DexMod: 2, Hit: 8, Dmg: dice.D(3, 10, 4), Reduction: dice.D(2, 6, 0), XP: 300},
}

var speciesByTile = func() map[world.Tile][]species {
	m := make(map[world.Tile][]species)
	for _, s := range speciesList {
		if s.Garrison || s.Dungeon { // garnisons et donjons n'apparaissent pas selon le biome
			continue
		}
		m[s.Habitat] = append(m[s.Habitat], s)
	}
	return m
}()

// companions donne, pour chaque espèce, les autres espèces avec lesquelles elle se bat de façon vraisemblable
// (la relation est symétrisée par canGroup). Chaque espèce se bat aussi avec les siens.
var companions = map[string][]string{
	"wolf":     {"rat"},    // les rats rôdent dans le sillage des loups
	"goblin":   {"wolf"},   // le gobelin et ses loups
	"yeti":     {"wolf"},   // loups des neiges
	"skeleton": {"knight"}, // garnison
	"warlord":  {"knight", "skeleton"},
	"wraith":   {"skeleton"}, // donjon
	"golem":    {"knight", "wraith"},
	"lich":     {"golem", "wraith"},
}

// canGroup dit si deux espèces peuvent se battre ensemble.
func canGroup(a, b string) bool {
	return a == b || slices.Contains(companions[a], b) || slices.Contains(companions[b], a)
}

// Réglages des groupes.
const (
	maxGroup    = 3 // monstres au plus dans un même combat
	maxGarrison = 2 // idem pour une garnison de forteresse (déjà redoutable seule)
	packLevels  = 3 // un joueur ne rencontre de meutes que d'un compagnon de plus tous les packLevels niveaux
	groupRadius = 5 // distance (Chebyshev) à laquelle un monstre libre se joint à un combat
)

// packExtra donne le nombre maximal de compagnons qui apparaissent avec une espèce de biome.
var packExtra = map[string]int{"rat": 2, "crab": 1, "wolf": 2, "goblin": 1, "yeti": 0, "scorpion": 1, "panther": 1, "toad": 2, "bear": 0}

// gatherGroup renvoie m et les monstres libres et compatibles qui l'entourent (au plus maxGroup en tout, les
// plus proches d'abord).
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

// spawnPack fait apparaître autour de m des compagnons, selon l'espèce et le niveau du joueur (sans dépasser le quota autour de around), sur des cases libres de son habitat.
func (g *Game) spawnPack(m *monster, around world.Pos, level int) {
	n := g.rng.IntN(min(packExtra[m.sp.Kind], level/packLevels) + 1) // pas de meute pour un débutant
	for range n {
		if g.monstersAround(around) >= maxPerPlayer || g.biomeCount() >= maxMonsters {
			return // la meute respecte les mêmes quotas que les monstres isolés
		}
		for range spawnTries {
			d := dirs8[g.rng.IntN(len(dirs8))]
			pos := world.Pos{X: m.pos.X + d[0]*(1+g.rng.IntN(2)), Y: m.pos.Y + d[1]*(1+g.rng.IntN(2))}
			if _, taken := g.monsterAt[pos]; taken || g.blocked(pos) || g.World.Safe(pos.X, pos.Y) || g.seenByAnyPlayer(pos) {
				continue
			}
			// Même espèce, ou (rarement) une espèce compagne dont l'habitat est celui de la case.
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

// SpeciesKinds liste les identifiants d'espèces (pour vérifier que le client
// sait toutes les dessiner).
func SpeciesKinds() []string {
	kinds := make([]string, len(speciesList))
	for i, s := range speciesList {
		kinds[i] = s.Kind
	}
	return kinds
}

// SpeciesName renvoie le nom affiché d'une espèce dans la langue l ("" si inconnue).
func SpeciesName(l i18n.Lang, kind string) string {
	for _, s := range speciesList {
		if s.Kind == kind {
			return s.name(l)
		}
	}
	return ""
}

// fighter convertit l'espèce en combattant (PV pleins).
func (s species) fighter() combat.Fighter {
	return combat.Fighter{HP: s.HP, MaxHP: s.HP, Def: hero.DefBase(s.Size) + s.DexMod, Init: s.DexMod, Hit: s.Hit, Dmg: s.Dmg, Reduction: s.Reduction, Kind: hero.Melee}
}

// name renvoie le nom de l'espèce dans la langue l (clé "monster.<kind>").
func (s species) name(l i18n.Lang) string { return i18n.T(l, "monster."+s.Kind) }

// Réglages d'apparition. La zone d'apparition est incluse dans la zone de
// conservation, sinon un monstre serait supprimé dès sa création.
const (
	spawnEvery      = 20 // ticks entre deux tentatives d'apparition (2 s)
	spawnHalfW      = 60
	spawnHalfH      = 30
	keepHalfW       = 70
	keepHalfH       = 40
	spawnViewMargin = 2 // marge hors de la vue où un monstre ne peut pas apparaître
	maxPerPlayer    = 8
	maxMonsters     = 500
	spawnTries      = 10
	wanderLeash     = 8 // distance maximale à l'origine quand il erre
	garrisonLeash   = 4 // idem pour un monstre de garnison
)

type monster struct {
	id       int
	zone     ZoneID // 0: carte principale; sinon instance d'un donjon (pos est alors local)
	sp       species
	pos      world.Pos
	home     world.Pos
	cooldown int    // ticks avant le prochain pas
	calm     int    // ticks pendant lesquels il reste inerte et n'engage pas (après une fuite ou une défaite)
	fight    *fight // non nil pendant un combat: le monstre est figé

	// Monstre de garnison: occupe un emplacement de forteresse, ne quitte pas
	// son emprise et rôde peu autour de son poste. nil/0 pour un monstre de biome.
	slot   *world.SlotKey
	bounds *world.Structure
	leash  int

	// guard : cet avatar prête sa fiche à un garde de village pour un combat (voir guards.go). Il n'est jamais
	// enregistré dans g.monsters.
	guard *guard
}

// leashDist est la distance maximale à laquelle le monstre s'éloigne de son origine en rôdant.
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

// addMonsterIn crée un monstre dans la zone z (0: carte principale).
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

// biomeCount compte les monstres de biome vivants: ni garnisons, ni monstres de donjon.
func (g *Game) biomeCount() int { return len(g.monsters) - g.garrisonCount - g.dungeonMonsters }

func (g *Game) removeMonster(m *monster) {
	delete(g.monsterIndex(m.zone), m.pos)
	delete(g.monsters, m.id)
	if m.zone != 0 {
		g.dungeonMonsters--
	}
	if m.slot != nil { // libère l'emplacement de forteresse
		delete(g.slotMonster, *m.slot)
		g.garrisonCount--
	}
}

func (g *Game) moveMonster(m *monster, dx, dy int) bool {
	if dx == 0 && dy == 0 {
		return false
	}
	dst := world.Pos{X: m.pos.X + dx, Y: m.pos.Y + dy}
	if g.blockedIn(m.zone, dst) || (m.zone == 0 && g.World.Safe(dst.X, dst.Y)) { // les zones sûres sont interdites aux monstres
		return false
	}
	if m.bounds != nil && !m.bounds.Contains(dst.X, dst.Y) { // une garnison ne quitte pas sa forteresse
		return false
	}
	at := g.monsterIndex(m.zone)
	delete(at, m.pos)
	m.pos = dst
	at[dst] = m.id
	return true
}

// nearestPlayer renvoie le joueur libre le plus proche à portée aggro (ex æquo: le plus ancien).
func (g *Game) nearestPlayer(z ZoneID, pos world.Pos, aggro int) *player {
	var best *player
	bestD := aggro + 1
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != z || p.fight != nil {
			continue // autre zone, ou déjà au combat: on ne s'acharne pas à plusieurs
		}
		if p.safe {
			continue // un joueur en zone sûre n'est ni poursuivi ni attaqué
		}
		if d := cheb(pos, p.pos); d < bestD {
			best, bestD = p, d
		}
	}
	return best
}

// stepMonsters fait agir chaque monstre: poursuite si un joueur est à portée,
// sinon errance près du point d'origine.
func (g *Game) stepMonsters(ids []int) {
	// Un donjon sans joueur est figé: il garde son état sans consommer de temps de calcul.
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
				g.startFight(target, m, false) // au contact: il attaque
				continue
			}
			dx, dy := sign(target.pos.X-m.pos.X), sign(target.pos.Y-m.pos.Y)
			// Pas direct, sinon on contourne en glissant le long d'un axe.
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

// seenByAnyPlayer indique si pos est dans la vue (marge comprise) d'un joueur.
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
		if m.zone == 0 && m.slot == nil && inBox(m.pos, pos, spawnHalfW, spawnHalfH) { // la garnison n'occupe pas le quota de biome
			n++
		}
	}
	return n
}

// despawnFar supprime les monstres loin de tous les joueurs.
func (g *Game) despawnFar() {
	for _, id := range g.sortedMonsterIDs() {
		m := g.monsters[id]
		if m.fight != nil || m.zone != 0 {
			continue // jamais en plein combat; les monstres de donjon vivent et meurent avec leur instance
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

// spawnMonsters tente de faire apparaître un monstre autour de chaque joueur,
// hors de toute vue, sur le terrain d'habitat de l'espèce.
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
			if g.seenByAnyPlayer(pos) || g.blocked(pos) || g.World.Safe(pos.X, pos.Y) {
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

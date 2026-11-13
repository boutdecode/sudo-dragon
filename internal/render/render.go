// Package render draws the world in coloured ASCII (Lip Gloss).
//
// Glyphs are deliberately pure ASCII (no ♣, ≈...): these characters
// have an ambiguous width depending on the terminal and would shift the grid.
package render

import (
	"github.com/kevin/ascii-adventure/internal/resource"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Style indices: tiles occupy 0..NumTiles-1 (world.Tile value),
// player styles come next. They are defined from
// NumTiles: hard-coded values would collide as soon as a
// new tile is added (a wall would be displayed as the player).
const (
	styleSelf = int(world.NumTiles) + iota
	styleOther
	styleFight // fighters (players and engaged monsters)
	numBaseStyles
)

var baseStyles = [numBaseStyles]lipgloss.Style{
	world.DeepWater: fg("#1d4ed8"),
	world.Water:     fg("#60a5fa"),
	world.Sand:      fg("#e5c07b"),
	world.Plain:     fg("#4ade80"),
	world.Forest:    fg("#15803d").Bold(true),
	world.Hills:     fg("#b08968"),
	world.Mountain:  fg("#9ca3af"),
	world.Snow:      fg("#f9fafb").Bold(true),
	world.Road:      fg("#a8895b"),
	world.Floor:     fg("#6b7280"),
	world.Wall:      fg("#cbd5e1").Bold(true),
	world.Door:      fg("#d97706").Bold(true),
	world.Chest:     fg("#facc15").Bold(true),
	world.Stairs:    fg("#a78bfa").Bold(true),
	world.Entrance:  fg("#c084fc").Bold(true),
	world.Rock:      fg("#3b4252"), // dungeon rock: muted, so that rooms and corridors stand out
	world.Campfire:  fg("#fb923c").Bold(true),
	world.Shrine:    fg("#f0abfc").Bold(true),
	world.Tower:     fg("#fde68a").Bold(true),
	world.Stone:     fg("#7dd3fc").Bold(true),
	world.LairChest: fg("#f87171").Bold(true),
	world.Spring:    fg("#38bdf8").Bold(true),
	world.Ruin:      fg("#d6d3d1").Bold(true),
	world.River:     fg("#38bdf8"),
	world.Tundra:    fg("#a5b4a0"),
	world.Desert:    fg("#e3b95f"),
	world.Swamp:     fg("#5f8a5a"),
	world.Jungle:    fg("#059669").Bold(true),
	world.TreeNode:  fg("#4ade80").Bold(true),
	world.OreNode:   fg("#94a3b8").Bold(true),
	world.HerbNode:  fg("#bef264").Bold(true),
	world.FishNode:  fg("#7dd3fc").Bold(true),
	styleSelf:       fg("#facc15").Bold(true),
	styleOther:      fg("#22d3ee").Bold(true),
	styleFight:      fg("#ef4444").Bold(true),
}

func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

// monsterLook is the appearance of a monster species.
type monsterLook struct {
	glyph rune
	style lipgloss.Style
}

// monsterLooks is indexed by the species identifier sent by the server
// (see game.SpeciesKinds, checked by a test).
var monsterLooks = map[string]monsterLook{
	"rat":    {'r', fg("#d4a373")},
	"wolf":   {'w', fg("#cbd5e1").Bold(true)},
	"crab":   {'c', fg("#f87171")},
	"goblin": {'g', fg("#a3e635").Bold(true)},
	"yeti":   {'Y', fg("#bae6fd").Bold(true)},

	// Biomes of generator v2.
	"scorpion": {'s', fg("#f59e0b").Bold(true)},
	"panther":  {'p', fg("#a78bfa").Bold(true)},
	"toad":     {'b', fg("#84cc16")},
	"bear":     {'U', fg("#e7e5e4").Bold(true)},

	// Fortress garrisons.
	"skeleton": {'S', fg("#e5e7eb")},
	"knight":   {'K', fg("#94a3b8").Bold(true)},
	"warlord":  {'B', fg("#dc2626").Bold(true)},

	// Dungeons.
	"wraith": {'W', fg("#a5b4fc")},
	"golem":  {'O', fg("#a8a29e").Bold(true)},
	"lich":   {'L', fg("#c084fc").Bold(true)},

	// Guard of a village or town (see game/guards.go): sent as a monster, but one talks to him.
	"guard": {'G', fg("#60a5fa").Bold(true)},

	// Travelling merchant on a road (see game/caravan.go): sent as a monster, but one talks to him.
	"caravan": {'E', fg("#fbbf24").Bold(true)},
}

// npcLooks gives the appearance of NPCs, indexed by their type (see world.NPCKind).
var npcLooks = map[world.NPCKind]monsterLook{
	world.NPCMerchant: {'M', fg("#fbbf24").Bold(true)},
	world.NPCHealer:   {'H', fg("#34d399").Bold(true)},
	questGiverKind:    {'!', fg("#facc15").Bold(true)},
	questTurnInKind:   {'?', fg("#facc15").Bold(true)},
	world.NPCSmith:    {'F', fg("#f97316").Bold(true)},
	world.NPCArmorer:  {'R', fg("#94a3b8").Bold(true)},
	world.NPCChief:    {'C', fg("#d4a017").Bold(true)},
	world.NPCMayor:    {'C', fg("#d4a017").Bold(true)},
	world.NPCVillager: {'V', fg("#7dd3fc").Bold(true)},
	world.NPCGuard:    {'G', fg("#60a5fa").Bold(true)},
}

// questGiverKind is not a real NPC type: it is the appearance of a villager who
// hands out quests on the map (the real type remains NPCVillager everywhere else).
const questGiverKind world.NPCKind = "villager+quest"

// questTurnInKind: the giver to whom the player has a quest ready to hand in ("?" instead of "!").
const questTurnInKind world.NPCKind = "villager+turnin"

var unknownNPC = monsterLook{'N', fg("#f472b6").Bold(true)}

func npcLookOf(k world.NPCKind) monsterLook {
	if l, ok := npcLooks[k]; ok {
		return l
	}
	return unknownNPC
}

// NPCName returns the displayable name of an NPC type.
func NPCName(l i18n.Lang, k world.NPCKind) string {
	if _, ok := npcLooks[k]; !ok {
		return i18n.T(l, "npc.kind.unknown")
	}
	return i18n.T(l, "npc.kind."+string(k))
}

// NPCGlyph returns the glyph of an NPC type.
func NPCGlyph(k world.NPCKind) rune { return npcLookOf(k).glyph }

var unknownMonster = monsterLook{'?', fg("#f472b6").Bold(true)}

func lookOf(kind string) monsterLook {
	if l, ok := monsterLooks[kind]; ok {
		return l
	}
	return unknownMonster
}

// MonsterName returns the displayable name of a species.
func MonsterName(l i18n.Lang, kind string) string {
	if _, ok := monsterLooks[kind]; !ok {
		return i18n.T(l, "monster.unknown")
	}
	return i18n.T(l, "monster."+kind)
}

// MonsterGlyph returns the glyph of a species.
func MonsterGlyph(kind string) rune { return lookOf(kind).glyph }

// styles groups the tile, player and then monster styles; the
// monsters are indexed by monsterStyle.
var (
	styles       []lipgloss.Style
	monsterStyle = map[string]int{}
	npcStyle     = map[world.NPCKind]int{}
)

func init() {
	styles = append(styles, baseStyles[:]...)
	kinds := make([]string, 0, len(monsterLooks))
	for k := range monsterLooks {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		monsterStyle[k] = len(styles)
		styles = append(styles, monsterLooks[k].style)
	}
	monsterStyle[""] = len(styles) // unknown species
	styles = append(styles, unknownMonster.style)

	npcKinds := make([]string, 0, len(npcLooks))
	for k := range npcLooks {
		npcKinds = append(npcKinds, string(k))
	}
	slices.Sort(npcKinds)
	for _, k := range npcKinds {
		npcStyle[world.NPCKind(k)] = len(styles)
		styles = append(styles, npcLooks[world.NPCKind(k)].style)
	}
	npcStyle[""] = len(styles) // unknown NPC
	styles = append(styles, unknownNPC.style)
	addResourceStyles()
	addClimateStyles()
	buildNightStyles()
}

// variants gives several glyphs per terrain to avoid the "wall of identical
// characters" effect. The choice depends only on the position.
var variants = map[world.Tile][]rune{
	world.DeepWater: []rune("~~~~~~~"),
	world.Water:     []rune("~~~-~~"),
	world.Sand:      []rune("....,.,"),
	world.Plain:     []rune("\"\"\"\"'.\"\""),
	world.Forest:    []rune("TTTPTT"),
	world.Hills:     []rune("nnnn^n"),
	world.Mountain:  []rune("^^A^^^"),
	world.Snow:      []rune("*..*.*"),
	world.Road:      []rune("::::;:"),
	world.Floor:     []rune("______"),
	world.Wall:      []rune("######"),
	world.Door:      []rune("++++++"),
	world.Chest:     []rune("$$$$$$"),
	world.Stairs:    []rune("<<<<<<"),
	world.Entrance:  []rune(">>>>>>"),
	world.Rock:      []rune("######"),
	world.Campfire:  []rune("&&&&&&"),
	world.Shrine:    []rune("oooooo"),
	world.Tower:     []rune("IIIIII"),
	world.Stone:     []rune("||||||"),
	world.LairChest: []rune("xxxxxx"),
	world.Spring:    []rune("vvvvvv"),
	world.Ruin:      []rune("//////"),
	world.River:     []rune("--~-~-"),
	world.Tundra:    []rune(",,.,'.,"),
	world.Desert:    []rune(";;.;:;."),
	world.Swamp:     []rune("mm~m\"m,m"),
	world.Jungle:    []rune("JJTJPJJ"),
	world.TreeNode:  []rune("TTTPTT"),
	world.OreNode:   []rune("nnnnnn"),
	world.HerbNode:  []rune("\"\"\",\"\"\""),
	world.FishNode:  []rune("--~-~-"),
}

// TerrainName returns the name of a terrain in language l.
func TerrainName(l i18n.Lang, t world.Tile) string {
	switch t {
	case world.DeepWater:
		return i18n.T(l, "terrain.deep_water")
	case world.Water:
		return i18n.T(l, "terrain.water")
	case world.Sand:
		return i18n.T(l, "terrain.sand")
	case world.Plain:
		return i18n.T(l, "terrain.plain")
	case world.Forest:
		return i18n.T(l, "terrain.forest")
	case world.Hills:
		return i18n.T(l, "terrain.hills")
	case world.Mountain:
		return i18n.T(l, "terrain.mountain")
	case world.Snow:
		return i18n.T(l, "terrain.snow")
	case world.Road:
		return i18n.T(l, "terrain.road")
	case world.Floor:
		return i18n.T(l, "terrain.floor")
	case world.Wall:
		return i18n.T(l, "terrain.wall")
	case world.Door:
		return i18n.T(l, "terrain.door")
	case world.Chest:
		return i18n.T(l, "terrain.chest")
	case world.Stairs:
		return i18n.T(l, "terrain.stairs")
	case world.Entrance:
		return i18n.T(l, "terrain.entrance")
	case world.Rock:
		return i18n.T(l, "terrain.rock")
	case world.Campfire:
		return i18n.T(l, "terrain.campfire")
	case world.Shrine:
		return i18n.T(l, "terrain.shrine")
	case world.Tower:
		return i18n.T(l, "terrain.tower")
	case world.Stone:
		return i18n.T(l, "terrain.stone")
	case world.LairChest:
		return i18n.T(l, "terrain.lairchest")
	case world.Spring:
		return i18n.T(l, "terrain.spring")
	case world.Ruin:
		return i18n.T(l, "terrain.ruin")
	case world.River:
		return i18n.T(l, "terrain.river")
	case world.Tundra:
		return i18n.T(l, "terrain.tundra")
	case world.Desert:
		return i18n.T(l, "terrain.desert")
	case world.Swamp:
		return i18n.T(l, "terrain.swamp")
	case world.Jungle:
		return i18n.T(l, "terrain.jungle")
	case world.TreeNode:
		return i18n.T(l, "terrain.treenode")
	case world.OreNode:
		return i18n.T(l, "terrain.orenode")
	case world.HerbNode:
		return i18n.T(l, "terrain.herbnode")
	case world.FishNode:
		return i18n.T(l, "terrain.fishnode")
	}
	return "?"
}

func glyph(t world.Tile, x, y int) rune { return pick(variants[t], x, y) }

// pick chooses a glyph among v based on the position alone.
func pick(v []rune, x, y int) rune {
	h := uint32(x)*73856093 ^ uint32(y)*19349663
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return v[h%uint32(len(v))]
}

// Terrain is what Viewport knows how to draw: the world or a dungeon plan.
type Terrain interface {
	At(x, y int) world.Tile
	NPCsIn(x0, y0, x1, y1 int) []world.NPC
}

// dungeonTerrain shows a dungeon through the fog: cells never seen are rock, an opened chest
// is bare floor. The first layer of rock, the one touching a walkable cell (diagonals included),
// is drawn as a white wall to outline the rooms and corridors; the rest of the rock is muted.
type dungeonTerrain struct {
	m      *world.DungeonMap
	seen   func(x, y int) bool
	opened map[[2]int]bool
}

// DungeonTerrain adapts a dungeon plan to Viewport. seen says whether a cell has already been seen (nil: everything is
// visible) and opened lists the emptied chests.
func DungeonTerrain(m *world.DungeonMap, seen func(x, y int) bool, opened [][2]int) Terrain {
	t := dungeonTerrain{m: m, seen: seen, opened: make(map[[2]int]bool, len(opened))}
	for _, o := range opened {
		t.opened[o] = true
	}
	return t
}

func (d dungeonTerrain) At(x, y int) world.Tile {
	if d.seen != nil && !d.seen(x, y) {
		return world.Rock
	}
	t := d.m.TileAt(x, y)
	switch {
	case t == world.Chest && d.opened[[2]int{x, y}]:
		return world.Floor
	case t == world.Rock && d.bordersFloor(x, y):
		return world.Wall
	}
	return t
}

// bordersFloor reports whether a neighbouring cell (8 directions) is walkable.
func (d dungeonTerrain) bordersFloor(x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if (dx != 0 || dy != 0) && d.m.TileAt(x+dx, y+dy).Walkable() {
				return true
			}
		}
	}
	return false
}

// entranceTerrain shows, on the main map, the dungeon entrances the player has discovered.
type entranceTerrain struct {
	Terrain
	at map[[2]int]bool
}

// WithEntrances overlays on t the discovered dungeon entrances (keys: x, y). Without an entrance, t is rendered as is.
func WithEntrances(t Terrain, entrances [][2]int) Terrain {
	if len(entrances) == 0 {
		return t
	}
	e := entranceTerrain{Terrain: t, at: make(map[[2]int]bool, len(entrances))}
	for _, p := range entrances {
		e.at[p] = true
	}
	return e
}

// NodeAt passes on the resource node of the decorated terrain, if it has one.
func (e entranceTerrain) NodeAt(x, y int) (resource.Kind, world.Tile, bool) {
	if n, ok := e.Terrain.(noded); ok {
		return n.NodeAt(x, y)
	}
	return "", 0, false
}

func (e entranceTerrain) At(x, y int) world.Tile {
	if e.at[[2]int{x, y}] {
		return world.Entrance
	}
	return e.Terrain.At(x, y)
}

// spentTerrain draws depleted resource nodes as the bare terrain they cover (base).
type spentTerrain struct {
	Terrain
	spent map[[2]int]bool
	base  func(x, y int) world.Tile
}

// WithSpent overlays on t the depleted resource nodes (keys: x, y): they take back the raw terrain base(x, y).
// Without a depleted node, t is rendered as is.
func WithSpent(t Terrain, spent map[[2]int]bool, base func(x, y int) world.Tile) Terrain {
	if len(spent) == 0 || base == nil {
		return t
	}
	return spentTerrain{Terrain: t, spent: spent, base: base}
}

// ClimateAt and NodeAt pass on to the decorated terrain what it offers in addition; a depleted node is no longer a node.
func (s spentTerrain) ClimateAt(x, y int) world.Climate {
	if c, ok := s.Terrain.(climated); ok {
		return c.ClimateAt(x, y)
	}
	return world.Temperate
}

func (s spentTerrain) NodeAt(x, y int) (resource.Kind, world.Tile, bool) {
	if n, ok := s.Terrain.(noded); ok && !s.spent[[2]int{x, y}] {
		return n.NodeAt(x, y)
	}
	return "", 0, false
}

func (s spentTerrain) At(x, y int) world.Tile {
	t := s.Terrain.At(x, y)
	switch t {
	case world.TreeNode, world.OreNode, world.HerbNode, world.FishNode:
		if s.spent[[2]int{x, y}] {
			return s.base(x, y)
		}
	}
	return t
}

// NPCsIn: there is nobody to talk to underground.
func (dungeonTerrain) NPCsIn(int, int, int, int) []world.NPC { return nil }

// Viewport draws a window of cols x rows tiles centred on (cx, cy).
// Each player is drawn as '@' (selfID designates the local player), the
// monsters by the glyph of their species, structure NPCs by theirs;
// players take precedence over monsters, which take precedence over NPCs.
// The result contains exactly rows lines of cols visible columns.
func Viewport(w Terrain, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int) string {
	return ViewportReady(w, cx, cy, cols, rows, ents, monsters, selfID, nil, nil)
}

// ViewportReady is ViewportNight in full daylight.
func ViewportReady(w Terrain, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int, readyGivers map[world.NPCKey]bool, idle func(world.NPCKey) bool) string {
	return ViewportNight(w, cx, cy, cols, rows, ents, monsters, selfID, readyGivers, idle, 0)
}

// ViewportNight is ViewportReady with a darkness level (0 = day, see NightLevel):
// only the terrain darkens. NPCs in readyGivers
// (givers of a ready quest) are shown as "?" instead of "!", and those for whom idle is true (givers who
// have nothing to hand out right now) as plain villagers.
func ViewportNight(w Terrain, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int, readyGivers map[world.NPCKey]bool, idle func(world.NPCKey) bool, night int) string {
	night = max(0, min(night, NightLevels-1))
	type key struct{ x, y int }
	mons := make(map[key]protocol.Monster, len(monsters))
	for _, m := range monsters {
		mons[key{m.X, m.Y}] = m
	}
	fighting := make(map[key]bool)
	for _, e := range ents {
		if e.Fighting {
			fighting[key{e.X, e.Y}] = true
		}
	}
	npcs := make(map[key]world.NPCKind)
	for _, n := range w.NPCsIn(cx-cols/2, cy-rows/2, cx-cols/2+cols-1, cy-rows/2+rows-1) {
		kind := n.Kind
		if n.GivesQuests {
			switch {
			case readyGivers[n.Key()]:
				kind = questTurnInKind
			case idle == nil || !idle(n.Key()):
				kind = questGiverKind
			}
		}
		npcs[key{n.Pos.X, n.Pos.Y}] = kind
	}
	occ := make(map[key]int, len(ents))
	for _, e := range ents {
		// The local player takes precedence if sharing a cell (should not happen).
		if _, taken := occ[key{e.X, e.Y}]; !taken || e.ID == selfID {
			occ[key{e.X, e.Y}] = e.ID
		}
	}

	cl, hasClimate := w.(climated)
	nd, hasNodes := w.(noded)
	x0, y0 := cx-cols/2, cy-rows/2
	var out strings.Builder
	out.Grow(rows * (cols + 16))

	for row := 0; row < rows; row++ {
		y := y0 + row
		var run strings.Builder
		cur := -1
		flush := func() {
			if run.Len() > 0 {
				out.WriteString(styleAt(cur).Render(run.String()))
				run.Reset()
			}
		}
		for col := 0; col < cols; col++ {
			x := x0 + col
			var ch rune
			var st int
			if id, ok := occ[key{x, y}]; ok {
				ch = '@'
				st = styleOther
				if id == selfID {
					st = styleSelf
				}
				if fighting[key{x, y}] {
					st = styleFight
				}
			} else if m, ok := mons[key{x, y}]; ok {
				ch = lookOf(m.Kind).glyph
				if st, ok = monsterStyle[m.Kind]; !ok {
					st = monsterStyle[""]
				}
				if m.Fighting {
					st = styleFight
				}
			} else if kind, ok := npcs[key{x, y}]; ok {
				ch = npcLookOf(kind).glyph
				if st, ok = npcStyle[kind]; !ok {
					st = npcStyle[""]
				}
			} else {
				t := w.At(x, y)
				c := world.Temperate
				if hasClimate {
					c = cl.ClimateAt(x, y)
				}
				ch, st = glyphIn(c, t, x, y), styleFor(c, t)
				if hasNodes && (t == world.TreeNode || t == world.OreNode || t == world.HerbNode || t == world.FishNode) {
					// A node keeps the glyph of the terrain it covers; its colour says the resource.
					if kind, base, ok := nd.NodeAt(x, y); ok {
						ch = glyphIn(c, base, x, y)
						if rs, known := resourceStyle[kind]; known {
							st = rs
						}
					}
				}
				if night > 0 && !lit(t) { // night only darkens the terrain
					st += night * len(styles)
				}
			}
			if st != cur {
				flush()
				cur = st
			}
			run.WriteRune(ch)
		}
		flush()
		if row < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

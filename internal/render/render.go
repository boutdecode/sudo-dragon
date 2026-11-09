// Package render dessine le monde en ASCII coloré (Lip Gloss).
//
// Les glyphes sont volontairement en ASCII pur (pas de ♣, ≈…) : ces caractères
// ont une largeur ambiguë selon les terminaux et décaleraient la grille.
package render

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Indices de style: les tuiles occupent 0..NumTiles-1 (valeur de world.Tile),
// les styles de joueurs viennent ensuite. Ils sont définis à partir de
// NumTiles: des valeurs codées en dur entreraient en collision dès qu'une
// nouvelle tuile est ajoutée (un mur s'afficherait comme le joueur).
const (
	styleSelf = int(world.NumTiles) + iota
	styleOther
	styleFight // combattants (joueurs et monstres engagés)
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
	world.Rock:      fg("#3b4252"), // roche d'un donjon: en sourdine, pour que salles et couloirs ressortent
	styleSelf:       fg("#facc15").Bold(true),
	styleOther:      fg("#22d3ee").Bold(true),
	styleFight:      fg("#ef4444").Bold(true),
}

func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

// monsterLook est l'apparence d'une espèce de monstre.
type monsterLook struct {
	glyph rune
	style lipgloss.Style
}

// monsterLooks est indexé par l'identifiant d'espèce envoyé par le serveur
// (voir game.SpeciesKinds, vérifié par un test).
var monsterLooks = map[string]monsterLook{
	"rat":    {'r', fg("#d4a373")},
	"wolf":   {'w', fg("#cbd5e1").Bold(true)},
	"crab":   {'c', fg("#f87171")},
	"goblin": {'g', fg("#a3e635").Bold(true)},
	"yeti":   {'Y', fg("#bae6fd").Bold(true)},

	// Garnisons de forteresse.
	"skeleton": {'S', fg("#e5e7eb")},
	"knight":   {'K', fg("#94a3b8").Bold(true)},
	"warlord":  {'B', fg("#dc2626").Bold(true)},

	// Donjons.
	"wraith": {'W', fg("#a5b4fc")},
	"golem":  {'O', fg("#a8a29e").Bold(true)},
	"lich":   {'L', fg("#c084fc").Bold(true)},
}

// npcLooks donne l'apparence des PNJ, indexée par leur type (voir world.NPCKind).
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
}

// questGiverKind n'est pas un vrai type de PNJ : c'est l'apparence d'un villageois qui
// confie des quêtes sur la carte (le type réel reste NPCVillager partout ailleurs).
const questGiverKind world.NPCKind = "villager+quest"

// questTurnInKind : le donneur à qui le joueur a une quête prête à rendre (« ? » au lieu de « ! »).
const questTurnInKind world.NPCKind = "villager+turnin"

var unknownNPC = monsterLook{'N', fg("#f472b6").Bold(true)}

func npcLookOf(k world.NPCKind) monsterLook {
	if l, ok := npcLooks[k]; ok {
		return l
	}
	return unknownNPC
}

// NPCName renvoie le nom affichable d'un type de PNJ.
func NPCName(l i18n.Lang, k world.NPCKind) string {
	if _, ok := npcLooks[k]; !ok {
		return i18n.T(l, "npc.kind.unknown")
	}
	return i18n.T(l, "npc.kind."+string(k))
}

// NPCGlyph renvoie le glyphe d'un type de PNJ.
func NPCGlyph(k world.NPCKind) rune { return npcLookOf(k).glyph }

var unknownMonster = monsterLook{'?', fg("#f472b6").Bold(true)}

func lookOf(kind string) monsterLook {
	if l, ok := monsterLooks[kind]; ok {
		return l
	}
	return unknownMonster
}

// MonsterName renvoie le nom affichable d'une espèce.
func MonsterName(l i18n.Lang, kind string) string {
	if _, ok := monsterLooks[kind]; !ok {
		return i18n.T(l, "monster.unknown")
	}
	return i18n.T(l, "monster."+kind)
}

// MonsterGlyph renvoie le glyphe d'une espèce.
func MonsterGlyph(kind string) rune { return lookOf(kind).glyph }

// styles regroupe les styles de tuiles, de joueurs puis de monstres; les
// monstres sont indexés par monsterStyle.
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
	monsterStyle[""] = len(styles) // espèce inconnue
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
	npcStyle[""] = len(styles) // PNJ inconnu
	styles = append(styles, unknownNPC.style)
}

// variants donne plusieurs glyphes par terrain pour éviter l'effet « mur de
// caractères identiques ». Le choix dépend uniquement de la position.
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
}

// TerrainName renvoie le nom d'un terrain dans la langue l.
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
	}
	return "?"
}

func glyph(t world.Tile, x, y int) rune {
	v := variants[t]
	h := uint32(x)*73856093 ^ uint32(y)*19349663
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return v[h%uint32(len(v))]
}

// Terrain est ce que Viewport sait dessiner: le monde ou le plan d'un donjon.
type Terrain interface {
	At(x, y int) world.Tile
	NPCsIn(x0, y0, x1, y1 int) []world.NPC
}

// dungeonTerrain montre un donjon à travers le brouillard: les cases jamais vues sont de la roche, un coffre
// ouvert est un sol nu. La première couche de roche, celle qui touche une case praticable (diagonales comprises),
// est dessinée en mur blanc pour souligner le contour des salles et des couloirs ; le reste de la roche est en sourdine.
type dungeonTerrain struct {
	m      *world.DungeonMap
	seen   func(x, y int) bool
	opened map[[2]int]bool
}

// DungeonTerrain adapte le plan d'un donjon à Viewport. seen dit si une case a déjà été vue (nil: tout est
// visible) et opened liste les coffres vidés.
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

// bordersFloor dit si une case voisine (8 directions) est praticable.
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

// entranceTerrain montre, sur la carte principale, les entrées de donjon que le joueur a découvertes.
type entranceTerrain struct {
	Terrain
	at map[[2]int]bool
}

// WithEntrances superpose à t les entrées de donjon découvertes (clés: x, y). Sans entrée, t est rendu tel quel.
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

func (e entranceTerrain) At(x, y int) world.Tile {
	if e.at[[2]int{x, y}] {
		return world.Entrance
	}
	return e.Terrain.At(x, y)
}

// NPCsIn : il n'y a personne à qui parler sous terre.
func (dungeonTerrain) NPCsIn(int, int, int, int) []world.NPC { return nil }

// Viewport dessine une fenêtre de cols x rows tuiles centrée sur (cx, cy).
// Chaque joueur est dessiné par '@' (selfID désigne le joueur local), les
// monstres par le glyphe de leur espèce, les PNJ des structures par le leur;
// les joueurs priment sur les monstres, qui priment sur les PNJ.
// Le résultat contient exactement rows lignes de cols colonnes visibles.
func Viewport(w Terrain, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int) string {
	return ViewportReady(w, cx, cy, cols, rows, ents, monsters, selfID, nil)
}

// ViewportReady est Viewport pour un joueur qui a des quêtes à rendre : les PNJ de readyGivers
// (donneurs d'une quête prête) s'affichent « ? » au lieu de « ! ».
func ViewportReady(w Terrain, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int, readyGivers map[world.NPCKey]bool) string {
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
			kind = questGiverKind
			if readyGivers[n.Key()] {
				kind = questTurnInKind
			}
		}
		npcs[key{n.Pos.X, n.Pos.Y}] = kind
	}
	occ := make(map[key]int, len(ents))
	for _, e := range ents {
		// Le joueur local prime s'il partage une case (ne devrait pas arriver).
		if _, taken := occ[key{e.X, e.Y}]; !taken || e.ID == selfID {
			occ[key{e.X, e.Y}] = e.ID
		}
	}

	x0, y0 := cx-cols/2, cy-rows/2
	var out strings.Builder
	out.Grow(rows * (cols + 16))

	for row := 0; row < rows; row++ {
		y := y0 + row
		var run strings.Builder
		cur := -1
		flush := func() {
			if run.Len() > 0 {
				out.WriteString(styles[cur].Render(run.String()))
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
				ch, st = glyph(t, x, y), int(t)
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

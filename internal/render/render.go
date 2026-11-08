// Package render dessine le monde en ASCII coloré (Lip Gloss).
//
// Les glyphes sont volontairement en ASCII pur (pas de ♣, ≈…) : ces caractères
// ont une largeur ambiguë selon les terminaux et décaleraient la grille.
package render

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

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
	styleSelf:       fg("#facc15").Bold(true),
	styleOther:      fg("#22d3ee").Bold(true),
	styleFight:      fg("#ef4444").Bold(true),
}

func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

// monsterLook est l'apparence d'une espèce de monstre.
type monsterLook struct {
	glyph rune
	name  string
	style lipgloss.Style
}

// monsterLooks est indexé par l'identifiant d'espèce envoyé par le serveur
// (voir game.SpeciesKinds, vérifié par un test).
var monsterLooks = map[string]monsterLook{
	"rat":    {'r', "Rat", fg("#d4a373")},
	"wolf":   {'w', "Loup", fg("#cbd5e1").Bold(true)},
	"crab":   {'c', "Crabe", fg("#f87171")},
	"goblin": {'g', "Gobelin", fg("#a3e635").Bold(true)},
	"yeti":   {'Y', "Yéti", fg("#bae6fd").Bold(true)},

	// Garnisons de forteresse.
	"skeleton": {'S', "Squelette", fg("#e5e7eb")},
	"knight":   {'K', "Chevalier", fg("#94a3b8").Bold(true)},
	"warlord":  {'B', "Seigneur de guerre", fg("#dc2626").Bold(true)},
}

// npcLooks donne l'apparence des PNJ, indexée par leur type (voir world.NPCKind).
var npcLooks = map[world.NPCKind]monsterLook{
	world.NPCMerchant: {'M', "Marchand", fg("#fbbf24").Bold(true)},
	world.NPCHealer:   {'H', "Soigneur", fg("#34d399").Bold(true)},
}

var unknownNPC = monsterLook{'N', "Inconnu", fg("#f472b6").Bold(true)}

func npcLookOf(k world.NPCKind) monsterLook {
	if l, ok := npcLooks[k]; ok {
		return l
	}
	return unknownNPC
}

// NPCName renvoie le nom affichable d'un type de PNJ.
func NPCName(k world.NPCKind) string { return npcLookOf(k).name }

// NPCGlyph renvoie le glyphe d'un type de PNJ.
func NPCGlyph(k world.NPCKind) rune { return npcLookOf(k).glyph }

var unknownMonster = monsterLook{'?', "Inconnu", fg("#f472b6").Bold(true)}

func lookOf(kind string) monsterLook {
	if l, ok := monsterLooks[kind]; ok {
		return l
	}
	return unknownMonster
}

// MonsterName renvoie le nom affichable d'une espèce.
func MonsterName(kind string) string { return lookOf(kind).name }

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
}

// TerrainName renvoie le nom français d'un terrain.
func TerrainName(t world.Tile) string {
	switch t {
	case world.DeepWater:
		return "Eau profonde"
	case world.Water:
		return "Eau"
	case world.Sand:
		return "Plage"
	case world.Plain:
		return "Plaine"
	case world.Forest:
		return "Forêt"
	case world.Hills:
		return "Collines"
	case world.Mountain:
		return "Montagne"
	case world.Snow:
		return "Neige"
	case world.Road:
		return "Chemin"
	case world.Floor:
		return "Intérieur"
	case world.Wall:
		return "Mur"
	case world.Door:
		return "Porte"
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

// Viewport dessine une fenêtre de cols x rows tuiles centrée sur (cx, cy).
// Chaque joueur est dessiné par '@' (selfID désigne le joueur local), les
// monstres par le glyphe de leur espèce, les PNJ des structures par le leur;
// les joueurs priment sur les monstres, qui priment sur les PNJ.
// Le résultat contient exactement rows lignes de cols colonnes visibles.
func Viewport(w *world.World, cx, cy, cols, rows int, ents []protocol.Entity, monsters []protocol.Monster, selfID int) string {
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
		npcs[key{n.Pos.X, n.Pos.Y}] = n.Kind
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

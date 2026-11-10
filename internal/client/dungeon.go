package client

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Donjons côté client. Le serveur annonce l'entrée dans un donjon par un message zone (identifiant, seed, palier,
// coffres ouverts) ; le plan est recalculé ici d'après la seed, comme le monde, et les coordonnées des instantanés
// sont locales à ce plan. Seules les cases proches ont été vues: le reste est de la roche.

// dungeonSight est le rayon (en cases) dans lequel on voit autour de soi sous terre.
const dungeonSight = 7

// inDungeon dit si le joueur est dans un donjon.
func (m Model) inDungeon() bool { return m.dmap != nil }

// terrain est la carte à dessiner: le monde, ou le donjon vu à travers le brouillard.
func (m Model) terrain() render.Terrain {
	if m.dmap == nil {
		return render.WithEntrances(m.world, m.entranceKeys())
	}
	seen := m.dseen
	return render.DungeonTerrain(m.dmap, func(x, y int) bool { return seen[world.Pos{X: x, Y: y}] }, m.zone.Opened)
}

// entranceKeys liste les entrées de donjon découvertes, en (x, y).
func (m Model) entranceKeys() [][2]int {
	out := make([][2]int, len(m.dungeons))
	for i, d := range m.dungeons {
		out[i] = [2]int{d.X, d.Y}
	}
	return out
}

func (m Model) entrancePositions() []world.Pos {
	out := make([]world.Pos, len(m.dungeons))
	for i, d := range m.dungeons {
		out[i] = world.Pos{X: d.X, Y: d.Y}
	}
	return out
}

// applyDungeons retient la liste des donjons découverts et annonce le dernier trouvé.
func (m *Model) applyDungeons(d protocol.Dungeons) {
	m.dungeons = d.List
	for _, info := range d.List {
		if info.ID == d.New {
			m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("dungeon.discovered", info.Name)})
		}
	}
}

// applyZone change de carte à la réception d'un message zone. Un simple renvoi (coffre ouvert) ne fait que mettre
// la liste des coffres à jour.
func (m *Model) applyZone(z protocol.Zone) {
	same := m.dmap != nil && z.Dungeon == m.zone.Dungeon
	if same || (z.Dungeon == "" && m.dmap == nil) {
		m.zone.Opened = z.Opened
		return
	}
	m.zone = z
	m.stopWalk("")
	m.worldMap = false
	m.hasSnap = false // les coordonnées changent: on attend l'instantané de la nouvelle carte
	m.locSeen = false
	if z.Dungeon == "" {
		m.dmap, m.dseen = nil, nil
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("dungeon.leaving")})
		return
	}
	m.dmap = world.NewDungeonMap(&world.Dungeon{Seed: z.Seed, Tier: z.Tier})
	m.dseen = make(map[world.Pos]bool)
	m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("dungeon.entering", z.Name)})
	if z.ResetIn > 0 {
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("dungeon.cleared", z.ResetIn)})
	}
}

// revealAround marque comme vues les cases proches du joueur, et ne garde dans l'instantané que les monstres
// et les joueurs qui s'y trouvent: le serveur envoie tout ce qui est dans la fenêtre de vision, mais on ne voit
// pas à travers la roche.
func (m *Model) revealAround() {
	if m.dmap == nil {
		return
	}
	self := m.snap.Self
	defer func() {
		var mons []protocol.Monster
		for _, mo := range m.snap.Monsters {
			if m.dseen[world.Pos{X: mo.X, Y: mo.Y}] {
				mons = append(mons, mo)
			}
		}
		var others []protocol.Entity
		for _, o := range m.snap.Others {
			if m.dseen[world.Pos{X: o.X, Y: o.Y}] {
				others = append(others, o)
			}
		}
		m.snap.Monsters, m.snap.Others = mons, others
	}()
	for dy := -dungeonSight; dy <= dungeonSight; dy++ {
		for dx := -dungeonSight; dx <= dungeonSight; dx++ {
			if dx*dx+dy*dy <= dungeonSight*dungeonSight {
				m.dseen[world.Pos{X: self.X + dx, Y: self.Y + dy}] = true
			}
		}
	}
}

// dungeonTitle est le titre du panneau de carte sous terre: « nom · (x, y) », abrégé si la place manque.
func (m Model) dungeonTitle(x, y, w int) string {
	coords := fmt.Sprintf("(%d, %d)", x, y)
	for _, t := range []string{m.zone.Name + " · " + coords, m.zone.Name, coords} {
		if ansi.StringWidth(t) <= w {
			return t
		}
	}
	return ansi.Truncate(coords, w, "")
}

// dungeonMapView dessine le plan du donjon déjà exploré dans exactement w x h cellules.
func (m Model) dungeonMapView(w, h int) string {
	cols, rows := w-2, h-1-2
	self := m.snap.Self
	cx, cy := self.X, self.Y
	if cols >= m.dmap.W && rows >= m.dmap.H { // tout tient: on le centre
		cx, cy = m.dmap.W/2, m.dmap.H/2
	}
	ents := append([]protocol.Entity{self}, m.snap.Others...)
	grid := render.ViewportReady(m.terrain(), cx, cy, cols, rows, ents, nil, self.ID, nil, nil)

	left := titleStyle.Render(m.t("dungeon.map.title"))
	right := dimStyle.Render(m.t("dungeon.map.tier", m.zone.Tier) + " · " + m.t("map.you", self.X, self.Y))
	header := " " + left
	if gap := w - ansi.StringWidth(left) - ansi.StringWidth(right) - 2; gap > 0 {
		header += strings.Repeat(" ", gap) + right
	}
	header = ui.Fit(header, w)
	return lipgloss.JoinVertical(lipgloss.Left, header, ui.Panel(m.zone.Name, grid, w, h-1))
}

// dungeonTarget résout, sous terre, une destination de /goto par son nom: l'escalier de sortie ou un coffre
// pas encore ouvert déjà vu (le plus proche). Renvoie le nombre de correspondances (0 ou 1).
func (m Model) dungeonTarget(query string) (target, int) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return target{}, 0
	}
	match := func(tile world.Tile) bool {
		return strings.HasPrefix(strings.ToLower(render.TerrainName(m.lang, tile)), q)
	}
	if match(world.Stairs) {
		return target{Pos: m.dmap.Exit, Name: render.TerrainName(m.lang, world.Stairs)}, 1
	}
	if match(world.Chest) {
		opened := map[[2]int]bool{}
		for _, o := range m.zone.Opened {
			opened[o] = true
		}
		self := m.selfPos()
		var best target
		found := false
		for _, c := range m.dmap.Chests {
			if opened[[2]int{c.X, c.Y}] || !m.dseen[c] {
				continue
			}
			if !found || dist2(c, self) < dist2(best.Pos, self) {
				best, found = target{Pos: c, Name: render.TerrainName(m.lang, world.Chest)}, true
			}
		}
		if found {
			return best, 1
		}
	}
	return target{}, 0
}

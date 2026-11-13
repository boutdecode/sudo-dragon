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

// Dungeons on the client side. The server announces entering a dungeon with a zone message (id, seed, tier,
// opened chests); the layout is recomputed here from the seed, like the world, and snapshot coordinates
// are local to that layout. Only nearby tiles have been seen: the rest is rock.

// dungeonSight is the radius (in tiles) within which one sees around oneself underground.
const dungeonSight = 7

// inDungeon tells whether the player is in a dungeon.
func (m Model) inDungeon() bool { return m.dmap != nil }

// terrain is the map to draw: the world, or the dungeon seen through the fog.
func (m Model) terrain() render.Terrain {
	if m.dmap == nil {
		return render.WithSpent(render.WithEntrances(m.world, m.entranceKeys()), m.spent, m.world.Terrain)
	}
	seen := m.dseen
	return render.DungeonTerrain(m.dmap, func(x, y int) bool { return seen[world.Pos{X: x, Y: y}] }, m.zone.Opened)
}

// entranceKeys lists the discovered dungeon entrances, as (x, y).
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

// applyDungeons keeps the list of discovered dungeons and announces the latest one found.
func (m *Model) applyDungeons(d protocol.Dungeons) {
	m.dungeons = d.List
	for _, info := range d.List {
		if info.ID == d.New {
			m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("dungeon.discovered", info.Name)})
		}
	}
}

// applyZone switches map on receiving a zone message. A simple resend (chest opened) only updates
// the chest list.
func (m *Model) applyZone(z protocol.Zone) {
	same := m.dmap != nil && z.Dungeon == m.zone.Dungeon
	if same || (z.Dungeon == "" && m.dmap == nil) {
		m.zone.Opened = z.Opened
		return
	}
	m.zone = z
	m.stopWalk("")
	m.worldMap = false
	m.hasSnap = false // coordinates change: wait for the new map's snapshot
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

// revealAround marks the tiles near the player as seen, and keeps in the snapshot only the monsters
// and players located there: the server sends everything in the vision window, but one cannot see
// through rock.
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

// dungeonTitle is the map panel title underground: "name · (x, y)", shortened if space is short.
func (m Model) dungeonTitle(x, y, w int) string {
	coords := fmt.Sprintf("(%d, %d)", x, y)
	for _, t := range []string{m.zone.Name + " · " + coords, m.zone.Name, coords} {
		if ansi.StringWidth(t) <= w {
			return t
		}
	}
	return ansi.Truncate(coords, w, "")
}

// dungeonMapView draws the already explored dungeon layout in exactly w x h cells.
func (m Model) dungeonMapView(w, h int) string {
	cols, rows := w-2, h-1-2
	self := m.snap.Self
	cx, cy := self.X, self.Y
	if cols >= m.dmap.W && rows >= m.dmap.H { // everything fits: center it
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

// dungeonTarget resolves, underground, a /goto destination by name: the exit stairs or a chest
// not yet opened already seen (the nearest). It returns the number of matches (0 or 1).
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

// maxSpentNodes bounds the depleted nodes kept (the server does not send more).
const maxSpentNodes = 1024

// applyNodes replaces the list of depleted resource nodes.
func (m *Model) applyNodes(n protocol.Nodes) {
	m.spent = make(map[[2]int]bool, min(len(n.Spent), maxSpentNodes))
	for i, p := range n.Spent {
		if i >= maxSpentNodes {
			break
		}
		m.spent[p] = true
	}
}

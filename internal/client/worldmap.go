package client

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

// World map (M key): overview of the terrain already explored. The terrain is regenerated
// locally from the seed; only the blocks the player has seen (the server's vision window)
// are drawn, the rest appears as "unexplored".

// mapZooms lists the possible scales: number of tiles per map cell.
var mapZooms = [...]int{1, 2, 4, 8, 16}

const (
	defaultMapZoom = 8
	exploreBlock   = 8  // side, in tiles, of a block marked as explored
	towerReveal    = 96 // radius, in tiles, revealed by a watchtower
	maxMarkers     = 9
	legendWidth    = 30
	legendMinWidth = 90 // minimal screen width to show the legend
	landmarkRadius = 600
)

// mapState is the map state: cursor, scale, placed markers and explored areas.
type mapState struct {
	curX, curY int
	zoom       int // tiles per cell; 0 = default
	markers    []marker
	entrances  []world.Pos // discovered dungeon entrances, to draw (filled in at each draw, never saved)
	naming     bool        // marker name entry in progress
	nameIdx    int         // marker being named
	dirty      bool        // exploration modified since the last save
	lastSave   time.Time
	explored   map[[2]int]struct{}
	blink      bool      // cursor blink phase
	gen        int       // invalidates the timers of an already closed map
	towerAt    world.Pos // last watchtower climbed (avoids recomputing everything on every frame)
	onTower    bool
}

const mapBlinkEvery = 500 * time.Millisecond

// mapBlinkMsg toggles the map cursor.
type mapBlinkMsg struct{ gen int }

func mapBlinkTick(gen int) tea.Cmd {
	return tea.Tick(mapBlinkEvery, func(time.Time) tea.Msg { return mapBlinkMsg{gen} })
}

// updateMapBlink advances the blinking while the map stays open.
func (m Model) updateMapBlink(msg mapBlinkMsg) (Model, tea.Cmd) {
	if !m.worldMap || msg.gen != m.wmap.gen {
		return m, nil
	}
	m.wmap.blink = !m.wmap.blink
	return m, mapBlinkTick(msg.gen)
}

func (s mapState) z() int {
	if s.zoom == 0 {
		return defaultMapZoom
	}
	return s.zoom
}

func fdiv(a, b int) int { return int(math.Floor(float64(a) / float64(b))) }

func (s *mapState) isExplored(x, y int) bool {
	_, ok := s.explored[[2]int{fdiv(x, exploreBlock), fdiv(y, exploreBlock)}]
	return ok
}

// markExplored marks as explored the blocks of the vision window around the player.
func (m *Model) markExplored() {
	if m.wmap.explored == nil {
		m.wmap.explored = make(map[[2]int]struct{})
	}
	self := m.snap.Self
	before := len(m.wmap.explored)
	for by := fdiv(self.Y-protocol.ViewHalfH, exploreBlock); by <= fdiv(self.Y+protocol.ViewHalfH, exploreBlock); by++ {
		for bx := fdiv(self.X-protocol.ViewHalfW, exploreBlock); bx <= fdiv(self.X+protocol.ViewHalfW, exploreBlock); bx++ {
			m.wmap.explored[[2]int{bx, by}] = struct{}{}
		}
	}
	// From the top of a watchtower, one sees much farther (once per tower).
	if m.world != nil && m.world.At(self.X, self.Y) == world.Tower {
		if here := (world.Pos{X: self.X, Y: self.Y}); !m.wmap.onTower || m.wmap.towerAt != here {
			m.wmap.onTower, m.wmap.towerAt = true, here
			for by := fdiv(self.Y-towerReveal, exploreBlock); by <= fdiv(self.Y+towerReveal, exploreBlock); by++ {
				for bx := fdiv(self.X-towerReveal, exploreBlock); bx <= fdiv(self.X+towerReveal, exploreBlock); bx++ {
					m.wmap.explored[[2]int{bx, by}] = struct{}{}
				}
			}
		}
	} else {
		m.wmap.onTower = false
	}
	if len(m.wmap.explored) != before {
		m.wmap.dirty = true
	}
}

func (m Model) openMap() (Model, tea.Cmd) {
	m.worldMap = true
	if m.dmap != nil { // underground: a fixed layout, without cursor or markers
		return m, nil
	}
	m.wmap.gen++
	m.wmap.blink = true
	m.wmap.curX, m.wmap.curY = m.snap.Self.X, m.snap.Self.Y
	return m, mapBlinkTick(m.wmap.gen)
}

func (m Model) updateMapKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dmap != nil {
		if k := msg.String(); k == "esc" || k == "m" {
			m.worldMap = false
		}
		return m, nil
	}
	if m.wmap.naming {
		return m.updateNaming(msg)
	}
	z := m.wmap.z()
	switch msg.String() {
	case "esc", "m":
		m.worldMap = false
		if m.wmap.dirty {
			return m, m.saveMap()
		}
	case "up":
		m.wmap.curY -= z
	case "down":
		m.wmap.curY += z
	case "left":
		m.wmap.curX -= z
	case "right":
		m.wmap.curX += z
	case "+", "=":
		m.wmap.zoom = stepZoom(z, -1)
	case "-", "_":
		m.wmap.zoom = stepZoom(z, +1)
	case "0", " ", "space":
		m.wmap.curX, m.wmap.curY = m.snap.Self.X, m.snap.Self.Y
	case "g":
		// Automatic walk to the cursor, then closing the map.
		m.worldMap = false
		var save tea.Cmd
		if m.wmap.dirty {
			save = m.saveMap()
		}
		target := world.Pos{X: m.wmap.curX, Y: m.wmap.curY}
		if m.hasSnap && m.world != nil {
			target = m.walkableNear(target, 8)
		}
		m, walk := m.startGoto(target)
		return m, tea.Batch(save, walk)
	case "w":
		return m.placeOrRenameMarker()
	case "x":
		if i := m.markerAtCursor(); i >= 0 {
			m.wmap.markers = append(m.wmap.markers[:i:i], m.wmap.markers[i+1:]...)
			return m, m.saveMap()
		}
	}
	return m, nil
}

// newMarkerInput creates the input field for a marker's name.
func newMarkerInput() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.CharLimit = maxMarkerName
	return t
}

// updateNaming handles a key while a marker name is being entered.
func (m Model) updateNaming(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.stopNaming()
		return m, nil
	case "enter":
		if i := m.wmap.nameIdx; i >= 0 && i < len(m.wmap.markers) {
			if name := cleanMarkerName(m.markName.Value()); name != "" {
				m.wmap.markers[i].Name = m.wmap.uniqueMarkerName(name, i)
			}
		}
		m.stopNaming()
		return m, m.saveMap()
	}
	var cmd tea.Cmd
	m.markName, cmd = m.markName.Update(msg)
	return m, cmd
}

func (m *Model) stopNaming() {
	m.wmap.naming = false
	m.markName.Reset()
	m.markName.Blur()
}

// markerAtCursor returns the index of the marker located in the cursor's cell, or -1.
func (m Model) markerAtCursor() int {
	z := m.wmap.z()
	for i, p := range m.wmap.markers {
		if cellOf(p.Pos.X, m.wmap.curX, z, 0) == 0 && cellOf(p.Pos.Y, m.wmap.curY, z, 0) == 0 {
			return i
		}
	}
	return -1
}

// defaultMarkerName returns the "Marker N" name with the smallest free N.
func (m Model) defaultMarkerName() string {
	for n := 1; ; n++ {
		name := m.t("map.marker", n)
		if _, c := m.wmap.findMarker(name); c == 0 {
			return name
		}
	}
}

// placeOrRenameMarker places a marker at the cursor (default name) or renames the one in the cell,
// then opens the name entry.
func (m Model) placeOrRenameMarker() (tea.Model, tea.Cmd) {
	i := m.markerAtCursor()
	switch {
	case i >= 0:
		m.markName.SetValue(m.wmap.markers[i].Name)
		m.markName.CursorEnd()
	case len(m.wmap.markers) < maxMarkers:
		pos := world.Pos{X: m.wmap.curX, Y: m.wmap.curY}
		m.wmap.markers = append(m.wmap.markers[:len(m.wmap.markers):len(m.wmap.markers)], marker{pos, m.defaultMarkerName()})
		i = len(m.wmap.markers) - 1
		m.markName.Reset()
	default:
		return m, nil
	}
	m.wmap.naming, m.wmap.nameIdx = true, i
	return m, tea.Batch(m.markName.Focus(), m.saveMap())
}

// stepZoom moves to the neighboring scale (delta -1 = finer), bounded at the ends.
func stepZoom(z, delta int) int {
	for i, v := range mapZooms {
		if v == z {
			return mapZooms[max(0, min(len(mapZooms)-1, i+delta))]
		}
	}
	return defaultMapZoom
}

// cellOf returns the index of the cell (column or row) that contains coordinate p, the
// central cell being centered on cur.
func cellOf(p, cur, z, n int) int { return n/2 + fdiv(p-cur+z/2, z) }

// Map styles (colors aligned with those of render).
func mfg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

const (
	msUnknown = iota
	msWater
	msSand
	msPlain
	msForest
	msHills
	msMountain
	msTundra
	msDesert
	msSwamp
	msJungle
	msRoad
	msWall
	msVillage
	msFort
	msMarker
	msDungeon
	msPOI
	msOther
	msSelf
	numMapStyles
)

// mapStyles holds numMapStyles styles per climate, the temperate ones first: a style's index is thus valid
// for all climates only when shifted by climatShift(c). Climates only recolor natural terrains.
var mapStyles = func() []lipgloss.Style {
	base := baseMapStyles
	all := append([]lipgloss.Style(nil), base[:]...)
	recolor := map[int]world.Tile{msWater: world.Water, msSand: world.Sand, msPlain: world.Plain, msForest: world.Forest, msHills: world.Hills, msMountain: world.Mountain, msTundra: world.Tundra, msDesert: world.Desert, msSwamp: world.Swamp, msJungle: world.Jungle}
	for c := world.Climate(1); c < world.NumClimates; c++ {
		block := base
		for ms, tile := range recolor {
			if hex := render.TileHex(c, tile); hex != "" {
				block[ms] = block[ms].Foreground(lipgloss.Color(hex))
			}
		}
		all = append(all, block[:]...)
	}
	return all
}()

// climatShift is the offset to add to a style index to dress it in the colors of climate c.
func climatShift(c world.Climate) int { return int(c) * numMapStyles }

var baseMapStyles = [numMapStyles]lipgloss.Style{
	msUnknown:  mfg(ui.ColFaint),
	msWater:    mfg("#60a5fa"),
	msSand:     mfg("#e5c07b"),
	msPlain:    mfg("#4ade80"),
	msForest:   mfg("#15803d").Bold(true),
	msHills:    mfg("#b08968"),
	msMountain: mfg("#9ca3af"),
	msTundra:   mfg("#a5b4a0"),
	msDesert:   mfg("#e3b95f"),
	msSwamp:    mfg("#5f8a5a"),
	msJungle:   mfg("#059669").Bold(true),
	msRoad:     mfg("#d9a441"),
	msWall:     mfg("#cbd5e1"),
	msVillage:  mfg(ui.ColAccent).Bold(true),
	msFort:     mfg(ui.ColHP).Bold(true),
	msMarker:   mfg("#f472b6").Bold(true),
	msDungeon:  mfg("#c084fc").Bold(true),
	msPOI:      mfg("#fbbf24").Bold(true),
	msOther:    mfg("#22d3ee").Bold(true),
	msSelf:     lipgloss.NewStyle().Background(lipgloss.Color(ui.ColAccent)).Foreground(lipgloss.Color(ui.ColInk)).Bold(true),
}

type mapCell struct {
	r   string
	st  int
	rev bool // cursor cell, in reverse video
}

// terrainCell chooses a tile's glyph.
func terrainCell(t world.Tile) mapCell {
	switch t {
	case world.DeepWater, world.Water, world.River, world.FishNode: // a shoal shows on the river that carries it
		return mapCell{r: "~", st: msWater}
	case world.Sand:
		return mapCell{r: ".", st: msSand}
	case world.Plain:
		return mapCell{r: "\"", st: msPlain}
	case world.Forest:
		return mapCell{r: "^", st: msForest}
	case world.Hills:
		return mapCell{r: "n", st: msHills}
	case world.Mountain, world.Snow:
		return mapCell{r: "▲", st: msMountain}
	case world.Tundra:
		return mapCell{r: ",", st: msTundra}
	case world.Desert:
		return mapCell{r: ":", st: msDesert}
	case world.Swamp:
		return mapCell{r: "m", st: msSwamp}
	case world.Jungle:
		return mapCell{r: "J", st: msJungle}
	case world.TreeNode: // resource nodes are not drawn on the map: we see the terrain that carries them
		return mapCell{r: "^", st: msForest}
	case world.OreNode:
		return mapCell{r: "n", st: msHills}
	case world.HerbNode:
		return mapCell{r: "\"", st: msPlain}
	case world.Road:
		return mapCell{r: "=", st: msRoad}
	case world.Floor:
		return mapCell{r: ".", st: msWall}
	case world.Door:
		return mapCell{r: "+", st: msRoad}
	case world.Chest:
		return mapCell{r: "$", st: msRoad}
	case world.Stairs:
		return mapCell{r: "<", st: msRoad}
	case world.Entrance:
		return mapCell{r: ">", st: msDungeon}
	case world.Rock:
		return mapCell{r: "#", st: msUnknown}
	case world.Campfire:
		return mapCell{r: "&", st: msPOI}
	case world.Shrine:
		return mapCell{r: "o", st: msPOI}
	case world.Tower:
		return mapCell{r: "I", st: msPOI}
	case world.Stone:
		return mapCell{r: "|", st: msPOI}
	case world.LairChest:
		return mapCell{r: "x", st: msPOI}
	case world.Spring:
		return mapCell{r: "v", st: msPOI}
	case world.Ruin:
		return mapCell{r: "/", st: msPOI}
	default: // Wall
		return mapCell{r: "#", st: msWall}
	}
}

// mapGrid draws the cols x rows area centered on the cursor. It returns the ANSI lines and the
// number of explored / total cells (for the gauge).
func mapGrid(w *world.World, st *mapState, self protocol.Entity, others []protocol.Entity, cols, rows int) (lines []string, explored, total int) {
	z := st.z()
	cx, cy := st.curX, st.curY
	grid := make([]mapCell, cols*rows)
	// Samples taken in each cell: the center, or four points beyond 1:1.
	offs := []int{0}
	if z > 1 {
		offs = []int{-z / 4, z / 4}
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			total++
			px, py := cx+(c-cols/2)*z, cy+(r-rows/2)*z
			seen := false
			counts := map[world.Tile]int{}
			for _, oy := range offs {
				for _, ox := range offs {
					if st.isExplored(px+ox, py+oy) {
						seen = true
						counts[w.At(px+ox, py+oy)]++
					}
				}
			}
			if !seen {
				grid[r*cols+c] = mapCell{r: "░", st: msUnknown}
				continue
			}
			explored++
			best, bestN := world.Tile(0), 0
			for t := world.Tile(0); t < world.NumTiles; t++ {
				if counts[t] > bestN {
					best, bestN = t, counts[t]
				}
			}
			cell := terrainCell(best)
			cell.st += climatShift(w.ClimateAt(px, py))
			grid[r*cols+c] = cell
		}
	}
	if z > 1 {
		// A one-tile-wide road gets lost in the vote above: put it back.
		half := z / 2
		w.RoadTilesIn(cx-(cols/2)*z-half, cy-(rows/2)*z-half, cx+(cols/2+1)*z+half, cy+(rows/2+1)*z+half, func(p world.Pos) {
			if !st.isExplored(p.X, p.Y) {
				return
			}
			c, r := cellOf(p.X, cx, z, cols), cellOf(p.Y, cy, z, rows)
			if c >= 0 && c < cols && r >= 0 && r < rows {
				grid[r*cols+c] = terrainCell(world.Road)
			}
		})
	}
	put := func(p world.Pos, cell mapCell) {
		c, r := cellOf(p.X, cx, z, cols), cellOf(p.Y, cy, z, rows)
		if c >= 0 && c < cols && r >= 0 && r < rows {
			grid[r*cols+c] = cell
		}
	}
	radius := max(cols, rows)*z/2 + 1
	for _, s := range w.StructuresNear(world.Pos{X: cx, Y: cy}, radius) {
		ctr := s.Center()
		if !st.isExplored(ctr.X, ctr.Y) {
			continue
		}
		if s.Kind == world.KindFortress {
			put(ctr, mapCell{r: "X", st: msFort})
		} else {
			put(ctr, mapCell{r: "H", st: msVillage})
		}
	}
	// A point of interest is a single tile: the vote above drowns it as soon as the map is zoomed out.
	for _, poi := range w.POIsWithin(cx, cy, radius) {
		if st.isExplored(poi.Pos.X, poi.Pos.Y) {
			cell := terrainCell(poi.Tile())
			cell.st += climatShift(w.ClimateAt(poi.Pos.X, poi.Pos.Y))
			put(poi.Pos, cell)
		}
	}
	for _, p := range st.entrances {
		put(p, mapCell{r: ">", st: msDungeon})
	}
	for _, p := range st.markers {
		put(p.Pos, mapCell{r: "*", st: msMarker})
	}
	for _, o := range others {
		put(world.Pos{X: o.X, Y: o.Y}, mapCell{r: "@", st: msOther})
	}
	put(world.Pos{X: self.X, Y: self.Y}, mapCell{r: "@", st: msSelf})
	if st.blink {
		cc := &grid[(rows/2)*cols+cols/2]
		cc.rev = true
		if cc.st == msSelf {
			cc.st = msOther // the player's "@" changes color instead of disappearing
		}
	}

	lines = make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		for c := 0; c < cols; {
			first := grid[r*cols+c]
			e := c
			var run strings.Builder
			for e < cols && grid[r*cols+e].st == first.st && grid[r*cols+e].rev == first.rev {
				run.WriteString(grid[r*cols+e].r)
				e++
			}
			style := mapStyles[first.st]
			if first.rev {
				style = style.Reverse(true)
			}
			b.WriteString(style.Render(run.String()))
			c = e
		}
		lines[r] = b.String()
	}
	return lines, explored, total
}

// seedLabel writes the seed in the form XXXX-XXXX.
func seedLabel(seed int64) string {
	u := uint64(seed)
	return fmt.Sprintf("%04X-%04X", (u>>16)&0xFFFF, u&0xFFFF)
}

// arrowTo returns the arrow (8 directions) leading from (0,0) toward (dx, dy).
func arrowTo(dx, dy int) string {
	if dx == 0 && dy == 0 {
		return "·"
	}
	arrows := [8]string{"→", "↘", "↓", "↙", "←", "↖", "↑", "↗"}
	k := int(math.Round(math.Atan2(float64(dy), float64(dx))/(math.Pi/4))) % 8
	if k < 0 {
		k += 8
	}
	return arrows[k]
}

type landmark struct {
	glyph  string
	style  int
	name   string
	detail string
	dist   float64
	dx, dy int
}

// landmarks lists nearby explored places and placed markers, from nearest to farthest from the player.
func (m Model) landmarks() []landmark {
	self := m.snap.Self
	var out []landmark
	add := func(l landmark, p world.Pos) {
		l.dx, l.dy = p.X-self.X, p.Y-self.Y
		l.dist = math.Hypot(float64(l.dx), float64(l.dy))
		out = append(out, l)
	}
	for _, s := range m.world.StructuresNear(world.Pos{X: self.X, Y: self.Y}, landmarkRadius) {
		ctr := s.Center()
		if !m.wmap.isExplored(ctr.X, ctr.Y) {
			continue
		}
		l := landmark{glyph: "H", style: msVillage, name: s.NameIn(m.lang), detail: m.t("place.kind." + s.Kind.String())}
		if s.Kind == world.KindFortress {
			l.glyph, l.style = "X", msFort
		}
		add(l, ctr)
	}
	for _, d := range m.dungeons {
		add(landmark{glyph: ">", style: msDungeon, name: d.Name, detail: m.t("map.dungeon.detail", d.Tier)}, world.Pos{X: d.X, Y: d.Y})
	}
	for _, p := range m.wmap.markers {
		add(landmark{glyph: "*", style: msMarker, name: p.Name}, p.Pos)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].dist < out[j].dist })
	return out
}

func (m Model) legendLines(rows int, c world.Climate) []string {
	entry := func(glyph string, st int, key string) string {
		return " " + mapStyles[st+climatShift(c)].Render(glyph) + " " + m.t(key)
	}
	lines := []string{
		entry("@", msSelf, "map.leg.you"),
		entry("@", msOther, "map.leg.others"),
		entry("H", msVillage, "map.leg.village"),
		entry("X", msFort, "map.leg.fortress"),
		entry("^", msForest, "map.leg.forest"),
		entry("▲", msMountain, "map.leg.mountain"),
		entry("~", msWater, "map.leg.water"),
		entry(":", msDesert, "map.leg.desert"),
		entry("J", msJungle, "map.leg.jungle"),
		entry("m", msSwamp, "map.leg.swamp"),
		entry(",", msTundra, "map.leg.tundra"),
		entry("=", msRoad, "map.leg.road"),
		entry(">", msDungeon, "map.leg.dungeon"),
		entry("&", msPOI, "map.leg.campfire"),
		entry("o", msPOI, "map.leg.shrine"),
		entry("x", msPOI, "map.leg.lair"),
		entry("I", msPOI, "map.leg.tower"),
		entry("|", msPOI, "map.leg.stone"),
		entry("v", msPOI, "map.leg.spring"),
		entry("/", msPOI, "map.leg.ruin"),
		entry("*", msMarker, "map.leg.marker"),
		entry("░", msUnknown, "map.leg.unexplored"),
	}
	// Markers take the remaining space, two lines each.
	if free := rows - len(lines) - 2; free >= 2 {
		lm := m.landmarks()
		if len(lm) > 0 {
			lines = append(lines, "", " "+dimStyle.Render(m.t("map.landmarks")))
			for _, l := range lm {
				if len(lines)+2 > rows {
					break
				}
				lines = append(lines,
					" "+mapStyles[l.style].Render(l.glyph)+" "+l.name,
					"   "+dimStyle.Render(arrowTo(l.dx, l.dy)+" "+m.t("map.dist", int(math.Round(l.dist)))+extra(l.detail)))
			}
		}
	}
	return lines
}

func extra(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}

// mapView draws the map in exactly w x h cells: header, map and legend.
func (m Model) mapView(w, h int) string {
	if m.dmap != nil {
		return m.dungeonMapView(w, h)
	}
	showLegend := w >= legendMinWidth
	mapW := w
	if showLegend {
		mapW = w - legendWidth
	}
	cols, rows := mapW-2, h-1-2
	st := m.wmap
	if st.explored == nil {
		st.explored = map[[2]int]struct{}{}
	}
	st.entrances = m.entrancePositions()
	lines, explored, total := mapGrid(m.world, &st, m.snap.Self, m.snap.Others, cols, rows)

	// Header.
	self := m.snap.Self
	info := []string{}
	if st.curX != self.X || st.curY != self.Y {
		info = append(info, m.t("map.cursor", st.curX, st.curY))
	}
	info = append(info, m.t("map.you", self.X, self.Y), m.t("map.zoom", st.z()), m.t("map.seed", seedLabel(m.world.Seed)))
	right := dimStyle.Render(strings.Join(info, " · "))
	left := titleStyle.Render(m.t("map.title"))
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
	header := " " + left
	if gap > 0 {
		header += strings.Repeat(" ", gap) + right
	}
	header = ui.Fit(header, w)

	title := m.world.TerritoryAt(st.curX, st.curY).NameIn(m.lang) + " · " + m.t("climate."+m.world.ClimateAt(st.curX, st.curY).Key())
	body := ui.Panel(title, strings.Join(lines, "\n"), mapW, h-1)
	if showLegend {
		lrows := h - 1 - 2
		leg := m.legendLines(lrows-1, m.world.ClimateAt(st.curX, st.curY))
		for len(leg) < lrows-1 {
			leg = append(leg, "")
		}
		pct := 0
		if total > 0 {
			pct = explored * 100 / total
		}
		leg = append(leg[:lrows-1], " "+dimStyle.Render(m.t("map.explored"))+" "+ui.FlatBar(explored, total, 8, ui.ColOK)+fmt.Sprintf(" %d %%", pct))
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, ui.Panel(m.t("map.legend"), strings.Join(leg, "\n"), legendWidth, h-1))
	}
	return header + "\n" + body
}

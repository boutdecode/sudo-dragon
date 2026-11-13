package client

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Automatic walking (/goto x y): the client computes a path then sends
// one step at regular intervals, as if the player pressed the keys.
// The server remains the sole judge of collisions.

// walkEvery is MoveCooldown (2 ticks) × TickInterval (100 ms).
const walkEvery = 200 * time.Millisecond

const (
	walkReplanAt = 4 // ticks without progress before recomputing the path
	walkGiveUpAt = 8 // ticks without progress before giving up
)

// walkTickMsg triggers a step; gen invalidates the timers of a cancelled
// or replaced walk.
type walkTickMsg struct {
	conn *Conn
	gen  int
}

func walkTick(c *Conn, gen int) tea.Cmd {
	return tea.Tick(walkEvery, func(time.Time) tea.Msg { return walkTickMsg{c, gen} })
}

// parseGoto recognizes "/goto x y". isCmd is true if the text is the
// command (even malformed); ok is false if the arguments are invalid.
func parseGoto(text string) (target world.Pos, isCmd, ok bool) {
	f := strings.Fields(text)
	if len(f) == 0 || !strings.EqualFold(f[0], "/goto") {
		return world.Pos{}, false, false
	}
	if len(f) != 3 {
		return world.Pos{}, true, false
	}
	x, err1 := strconv.Atoi(f[1])
	y, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil {
		return world.Pos{}, true, false
	}
	return world.Pos{X: x, Y: y}, true, true
}

// parseGotoName recognizes "/goto marker name": the command with arguments that are not
// two integers. The name is all the arguments, separated by a space.
func parseGotoName(text string) (name string, ok bool) {
	f := strings.Fields(text)
	if len(f) < 2 || !strings.EqualFold(f[0], "/goto") {
		return "", false
	}
	if _, _, valid := parseGoto(text); valid {
		return "", false
	}
	return strings.Join(f[1:], " "), true
}

// target is a destination designated by name: a marker or a discovered place.
type target struct {
	Pos   world.Pos
	Name  string
	place bool // place (center to adjust toward a walkable tile)
}

// discoveredPlaces returns the places (village, town, fortress) whose center is in an
// explored area of the map.
func (m Model) discoveredPlaces() []*world.Structure {
	if m.world == nil || m.dmap != nil {
		return nil
	}
	regions := map[world.RegionPos]struct{}{}
	for b := range m.wmap.explored {
		regions[world.RegionOf(b[0]*exploreBlock, b[1]*exploreBlock)] = struct{}{}
	}
	var out []*world.Structure
	for rp := range regions {
		if s := m.world.StructureOfRegion(rp); s != nil {
			if c := s.Center(); m.wmap.isExplored(c.X, c.Y) {
				out = append(out, s)
			}
		}
	}
	return out
}

// findTarget resolves a marker or discovered place name, case-insensitively: exact
// match (marker first, then place, the nearest in case of twin places), otherwise prefix. It returns
// the number of matches (0 = unknown, >1 = ambiguous).
func (m Model) findTarget(query string) (target, int) {
	if m.dmap != nil {
		return m.dungeonTarget(query)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return target{}, 0
	}
	self := m.selfPos()
	nearest := func(ts []target) target {
		best := ts[0]
		for _, t := range ts[1:] {
			if dist2(t.Pos, self) < dist2(best.Pos, self) {
				best = t
			}
		}
		return best
	}
	for _, mk := range m.wmap.markers {
		if strings.ToLower(mk.Name) == q {
			return target{Pos: mk.Pos, Name: mk.Name}, 1
		}
	}
	var exact []target
	groups := map[string][]target{} // places whose name starts with q, grouped by name
	for _, s := range m.discoveredPlaces() {
		name := s.NameIn(m.lang)
		t := target{Pos: s.Center(), Name: name, place: true}
		low, full := strings.ToLower(name), strings.ToLower(m.placeLabel(s))
		if low == q || full == q {
			exact = append(exact, t)
		}
		if strings.HasPrefix(low, q) || strings.HasPrefix(full, q) {
			groups[low] = append(groups[low], t)
		}
	}
	for _, d := range m.dungeons { // dungeon entrances are reached like places: we stop in front of them
		t := target{Pos: world.Pos{X: d.X, Y: d.Y}, Name: d.Name, place: true}
		low := strings.ToLower(d.Name)
		if low == q {
			exact = append(exact, t)
		}
		if strings.HasPrefix(low, q) {
			groups[low] = append(groups[low], t)
		}
	}
	if len(exact) > 0 {
		return nearest(exact), 1
	}
	var markers []marker
	for _, mk := range m.wmap.markers {
		if strings.HasPrefix(strings.ToLower(mk.Name), q) {
			markers = append(markers, mk)
		}
	}
	n := len(markers) + len(groups)
	if n == 1 {
		if len(markers) == 1 {
			return target{Pos: markers[0].Pos, Name: markers[0].Name}, 1
		}
		for _, g := range groups {
			return nearest(g), 1
		}
	}
	return target{}, n
}

func dist2(a, b world.Pos) int { return (a.X-b.X)*(a.X-b.X) + (a.Y-b.Y)*(a.Y-b.Y) }

// walkableNear returns the free tile nearest to p within a given radius, preferring
// paths (a place's center often falls inside a building). Falls back to p.
func (m Model) walkableNear(p world.Pos, radius int) world.Pos {
	free := m.freeFn()
	for _, roadOnly := range []bool{true, false} {
		ok := func(c world.Pos) bool {
			return free(c) && (!roadOnly || m.terrain().At(c.X, c.Y) == world.Road)
		}
		if ok(p) {
			return p
		}
		for r := 1; r <= radius; r++ {
			best, found := world.Pos{}, false
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					if max(abs(dx), abs(dy)) != r {
						continue
					}
					c := world.Pos{X: p.X + dx, Y: p.Y + dy}
					if ok(c) && (!found || dist2(c, p) < dist2(best, p)) {
						best, found = c, true
					}
				}
			}
			if found {
				return best
			}
		}
	}
	return p
}

// gotoName starts the walk to a marker or discovered place designated by name.
func (m Model) gotoName(name string) (Model, tea.Cmd) {
	t, n := m.findTarget(name)
	switch {
	case n == 0:
		m.sysLine("cli.goto.unknown_marker", name)
		return m, nil
	case n > 1:
		m.sysLine("cli.goto.ambiguous", name)
		return m, nil
	}
	pos := t.Pos
	if t.place {
		pos = m.walkableNear(pos, 8)
	}
	return m.startGoto(pos)
}

func (m Model) selfPos() world.Pos { return world.Pos{X: m.snap.Self.X, Y: m.snap.Self.Y} }

func (m *Model) sysLine(key string, args ...any) {
	m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t(key, args...)})
}

// freeFn returns a walkable-tile test: terrain, NPCs, monsters and
// other players (positions from the current snapshot).
func (m Model) freeFn() func(world.Pos) bool { return m.freeFnTo(m.walkTarget) }

// freeFnTo is freeFn for a walk to to. Dungeon stairs and a dungeon entrance are
// passages: we do not cross them by accident, only if it is the destination.
func (m Model) freeFnTo(to world.Pos) func(world.Pos) bool {
	busy := map[world.Pos]bool{}
	for _, mo := range m.snap.Monsters {
		busy[world.Pos{X: mo.X, Y: mo.Y}] = true
	}
	for _, e := range m.snap.Others {
		busy[world.Pos{X: e.X, Y: e.Y}] = true
	}
	if m.dmap != nil {
		return func(p world.Pos) bool {
			t := m.dmap.TileAt(p.X, p.Y)
			return !busy[p] && t.Walkable() && (t != world.Stairs || p == to)
		}
	}
	return func(p world.Pos) bool {
		if busy[p] || !m.world.At(p.X, p.Y).Walkable() {
			return false
		}
		if _, npc := m.world.NPCAt(p.X, p.Y); npc {
			return false
		}
		return p == to || m.world.DungeonAt(p.X, p.Y) == nil
	}
}

// startGoto starts the walk to target.
func (m Model) startGoto(target world.Pos) (Model, tea.Cmd) {
	m.walkGen++
	m.walking, m.walkPath = false, nil
	if !m.hasSnap || m.world == nil {
		return m, nil
	}
	if m.selfPos() == target {
		m.sysLine("cli.goto.arrived")
		return m, nil
	}
	path := m.findWalkPath(m.selfPos(), target, m.freeFnTo(target))
	if path == nil {
		m.sysLine("cli.goto.unreachable", target.X, target.Y)
		return m, nil
	}
	m.walking, m.walkTarget, m.walkPath = true, target, path
	m.walkLast, m.walkStuck = world.Pos{X: 1 << 30}, 0
	m.sysLine("cli.goto.start", target.X, target.Y, len(path))
	return m, walkTick(m.conn, m.walkGen)
}

// stopWalk cancels the walk; key is the message to display ("" = none).
func (m *Model) stopWalk(key string) {
	if !m.walking {
		return
	}
	m.walkGen++
	m.walking, m.walkPath = false, nil
	if key != "" {
		m.sysLine(key)
	}
}

// updateWalk handles one step of the automatic walk.
func (m Model) updateWalk(msg walkTickMsg) (Model, tea.Cmd) {
	if !m.walking || msg.conn != m.conn || msg.gen != m.walkGen || m.state != statePlaying {
		return m, nil
	}
	if m.combat != nil || m.npc != nil {
		m.stopWalk("cli.goto.interrupted")
		return m, nil
	}
	self := m.selfPos()
	if self == m.walkTarget {
		m.stopWalk("cli.goto.arrived")
		return m, nil
	}
	again := walkTick(m.conn, m.walkGen)

	// No progress: the snapshot may not have arrived yet, or an
	// obstacle blocks us. We wait, then recompute, then give up.
	if self == m.walkLast {
		m.walkStuck++
		if m.walkStuck >= walkGiveUpAt {
			m.stopWalk("cli.goto.interrupted")
			return m, nil
		}
		if m.walkStuck < 2 {
			return m, again
		}
		if m.walkStuck == walkReplanAt {
			m.walkPath = nil
		}
	} else {
		m.walkStuck = 0
	}

	// Realign the path on the current position.
	idx := -1
	for i, p := range m.walkPath {
		if p == self {
			idx = i
		}
	}
	if idx >= 0 {
		m.walkPath = m.walkPath[idx+1:]
	}
	free := m.freeFn()
	if idx < 0 && !(len(m.walkPath) > 0 && chebyshev(self, m.walkPath[0]) == 1) ||
		len(m.walkPath) == 0 || !free(m.walkPath[0]) {
		m.walkPath = m.findWalkPath(self, m.walkTarget, free)
		if m.walkPath == nil {
			if m.walkStuck > 0 {
				return m, again // maybe a passing obstacle
			}
			m.stopWalk("cli.goto.unreachable_now")
			return m, nil
		}
	}

	next := m.walkPath[0]
	m.walkLast = self
	if m.conn != nil {
		m.conn.Send(protocol.TMove, protocol.Move{DX: next.X - self.X, DY: next.Y - self.Y})
	}
	return m, again
}

// gotoCandidates lists the names /goto can resolve (for Tab completion): markers,
// discovered places (name alone and label "Village Foo") and dungeon entrances; underground,
// the stairs and the chests.
func (m Model) gotoCandidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if m.dmap != nil {
		add(render.TerrainName(m.lang, world.Stairs))
		add(render.TerrainName(m.lang, world.Chest))
	} else {
		for _, mk := range m.wmap.markers {
			add(mk.Name)
		}
		for _, s := range m.discoveredPlaces() {
			add(s.NameIn(m.lang))
			add(m.placeLabel(s))
		}
		for _, d := range m.dungeons {
			add(d.Name)
		}
	}
	sort.Strings(out)
	return out
}

// completeGoto completes "/goto start" with the candidates whose name starts with that start (case
// ignored). It returns the new text and the matches; the text is unchanged if the input is
// not a /goto by name or if there is no match.
func completeGoto(input string, cands []string) (string, []string) {
	const cmd = "/goto "
	if len(input) < len(cmd) || !strings.EqualFold(input[:len(cmd)], cmd) {
		return input, nil
	}
	prefix := strings.TrimLeft(input[len(cmd):], " ")
	if f := strings.Fields(prefix); len(f) == 2 {
		_, e1 := strconv.Atoi(f[0])
		_, e2 := strconv.Atoi(f[1])
		if e1 == nil && e2 == nil {
			return input, nil
		}
	}
	low := strings.ToLower(prefix)
	var matches []string
	for _, c := range cands {
		if strings.HasPrefix(strings.ToLower(c), low) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return input, nil
	}
	common := []rune(matches[0])
	for _, c := range matches[1:] {
		r := []rune(c)
		n := 0
		for n < len(common) && n < len(r) && unicode.ToLower(common[n]) == unicode.ToLower(r[n]) {
			n++
		}
		common = common[:n]
	}
	if len(common) < len([]rune(prefix)) { // never shorten what the player typed
		return input, matches
	}
	return input[:len(cmd)] + string(common), matches
}

// completeChat handles Tab in the chat: completes a /goto and, if nothing progresses while
// several names fit, lists them in the chat.
func (m *Model) completeChat() {
	cur := m.chatInput.Value()
	next, matches := completeGoto(cur, m.gotoCandidates())
	if next != cur {
		m.chatInput.SetValue(next)
		m.chatInput.CursorEnd()
		return
	}
	if len(matches) > 1 {
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: strings.Join(matches, " · ")})
	}
}

const maxChatHist = 100

// pushHistory remembers a line sent from the chat (without consecutive duplicates) and resets
// recall to the input in progress.
func (m *Model) pushHistory(text string) {
	if text != "" && (len(m.chatHist) == 0 || m.chatHist[len(m.chatHist)-1] != text) {
		m.chatHist = append(m.chatHist, text)
		if len(m.chatHist) > maxChatHist {
			m.chatHist = m.chatHist[len(m.chatHist)-maxChatHist:]
		}
	}
	m.histPos, m.histDraft = len(m.chatHist), ""
}

// recallHistory goes up (-1) or down (+1) in the lines already sent; at the bottom we get back the
// input in progress.
func (m *Model) recallHistory(dir int) {
	if m.histPos > len(m.chatHist) { // history shortened in the meantime
		m.histPos = len(m.chatHist)
	}
	pos := m.histPos + dir
	if pos < 0 || pos > len(m.chatHist) {
		return
	}
	if m.histPos == len(m.chatHist) {
		m.histDraft = m.chatInput.Value()
	}
	m.histPos = pos
	if pos == len(m.chatHist) {
		m.chatInput.SetValue(m.histDraft)
	} else {
		m.chatInput.SetValue(m.chatHist[pos])
	}
	m.chatInput.CursorEnd()
}

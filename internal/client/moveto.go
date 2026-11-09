package client

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Marche automatique (/moveto x y): le client calcule un chemin puis envoie
// un pas à intervalle régulier, comme si le joueur appuyait sur les touches.
// Le serveur reste seul juge des collisions.

// walkEvery vaut MoveCooldown (2 ticks) × TickInterval (100 ms).
const walkEvery = 200 * time.Millisecond

const (
	walkReplanAt = 4 // ticks sans progrès avant de recalculer le chemin
	walkGiveUpAt = 8 // ticks sans progrès avant d'abandonner
)

// walkTickMsg déclenche un pas; gen invalide les minuteurs d'une marche
// annulée ou remplacée.
type walkTickMsg struct {
	conn *Conn
	gen  int
}

func walkTick(c *Conn, gen int) tea.Cmd {
	return tea.Tick(walkEvery, func(time.Time) tea.Msg { return walkTickMsg{c, gen} })
}

// parseMoveto reconnaît "/moveto x y". isCmd est vrai si le texte est la
// commande (même mal formée); ok est faux si les arguments sont invalides.
func parseMoveto(text string) (target world.Pos, isCmd, ok bool) {
	f := strings.Fields(text)
	if len(f) == 0 || !strings.EqualFold(f[0], "/moveto") {
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

// parseMovetoName reconnaît "/moveto nom du repère" : la commande avec des arguments qui ne sont pas
// deux entiers. Le nom est l'ensemble des arguments, séparés par une espace.
func parseMovetoName(text string) (name string, ok bool) {
	f := strings.Fields(text)
	if len(f) < 2 || !strings.EqualFold(f[0], "/moveto") {
		return "", false
	}
	if _, _, valid := parseMoveto(text); valid {
		return "", false
	}
	return strings.Join(f[1:], " "), true
}

// target est une destination désignée par son nom : un repère ou un lieu découvert.
type target struct {
	Pos   world.Pos
	Name  string
	place bool // lieu (centre à ajuster vers une case praticable)
}

// discoveredPlaces renvoie les lieux (village, ville, forteresse) dont le centre est sur une zone
// explorée de la carte.
func (m Model) discoveredPlaces() []*world.Structure {
	if m.world == nil {
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

// findTarget résout un nom de repère ou de lieu découvert, sans tenir compte de la casse : égalité
// exacte (repère d'abord, puis lieu, le plus proche en cas de lieux jumeaux), sinon préfixe. Renvoie
// le nombre de correspondances (0 = inconnu, >1 = ambigu).
func (m Model) findTarget(query string) (target, int) {
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
	groups := map[string][]target{} // lieux dont le nom commence par q, groupés par nom
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

// walkableNear renvoie la case libre la plus proche de p dans un rayon donné, en préférant les
// chemins (le centre d'un lieu tombe souvent dans un bâtiment). Repli sur p.
func (m Model) walkableNear(p world.Pos, radius int) world.Pos {
	free := m.freeFn()
	for _, roadOnly := range []bool{true, false} {
		ok := func(c world.Pos) bool {
			return free(c) && (!roadOnly || m.world.At(c.X, c.Y) == world.Road)
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

// movetoName lance la marche vers un repère ou un lieu découvert désigné par son nom.
func (m Model) movetoName(name string) (Model, tea.Cmd) {
	t, n := m.findTarget(name)
	switch {
	case n == 0:
		m.sysLine("cli.moveto.unknown_marker", name)
		return m, nil
	case n > 1:
		m.sysLine("cli.moveto.ambiguous", name)
		return m, nil
	}
	pos := t.Pos
	if t.place {
		pos = m.walkableNear(pos, 8)
	}
	return m.startMoveto(pos)
}

func (m Model) selfPos() world.Pos { return world.Pos{X: m.snap.Self.X, Y: m.snap.Self.Y} }

func (m *Model) sysLine(key string, args ...any) {
	m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t(key, args...)})
}

// freeFn renvoie un test de case praticable: terrain, PNJ, monstres et
// autres joueurs (positions de l'instantané courant).
func (m Model) freeFn() func(world.Pos) bool {
	busy := map[world.Pos]bool{}
	for _, mo := range m.snap.Monsters {
		busy[world.Pos{X: mo.X, Y: mo.Y}] = true
	}
	for _, e := range m.snap.Others {
		busy[world.Pos{X: e.X, Y: e.Y}] = true
	}
	return func(p world.Pos) bool {
		if busy[p] || !m.world.At(p.X, p.Y).Walkable() {
			return false
		}
		_, npc := m.world.NPCAt(p.X, p.Y)
		return !npc
	}
}

// startMoveto lance la marche vers target.
func (m Model) startMoveto(target world.Pos) (Model, tea.Cmd) {
	m.walkGen++
	m.walking, m.walkPath = false, nil
	if !m.hasSnap || m.world == nil {
		return m, nil
	}
	if m.selfPos() == target {
		m.sysLine("cli.moveto.arrived")
		return m, nil
	}
	path := m.findWalkPath(m.selfPos(), target, m.freeFn())
	if path == nil {
		m.sysLine("cli.moveto.unreachable", target.X, target.Y)
		return m, nil
	}
	m.walking, m.walkTarget, m.walkPath = true, target, path
	m.walkLast, m.walkStuck = world.Pos{X: 1 << 30}, 0
	m.sysLine("cli.moveto.start", target.X, target.Y, len(path))
	return m, walkTick(m.conn, m.walkGen)
}

// stopWalk annule la marche; key est le message à afficher ("" = aucun).
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

// updateWalk traite un pas de la marche automatique.
func (m Model) updateWalk(msg walkTickMsg) (Model, tea.Cmd) {
	if !m.walking || msg.conn != m.conn || msg.gen != m.walkGen || m.state != statePlaying {
		return m, nil
	}
	if m.combat != nil || m.npc != nil {
		m.stopWalk("cli.moveto.interrupted")
		return m, nil
	}
	self := m.selfPos()
	if self == m.walkTarget {
		m.stopWalk("cli.moveto.arrived")
		return m, nil
	}
	again := walkTick(m.conn, m.walkGen)

	// Pas de progrès: l'instantané n'est peut-être pas encore arrivé, ou un
	// obstacle nous bloque. On patiente, puis on recalcule, puis on abandonne.
	if self == m.walkLast {
		m.walkStuck++
		if m.walkStuck >= walkGiveUpAt {
			m.stopWalk("cli.moveto.interrupted")
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

	// Recale le chemin sur la position actuelle.
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
				return m, again // peut-être un obstacle passager
			}
			m.stopWalk("cli.moveto.unreachable_now")
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

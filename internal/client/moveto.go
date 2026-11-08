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
	path := findPath(m.selfPos(), target, m.freeFn(), maxPathNodes)
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
		m.walkPath = findPath(self, m.walkTarget, free, maxPathNodes)
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

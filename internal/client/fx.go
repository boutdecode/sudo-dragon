package client

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// Combat visual effects: shaking of the combat area when we are hit, of the enemy column when we
// hit. The animation is driven by ticks and depends only on the model state (hence testable).
const (
	fxFrameEvery = 60 * time.Millisecond
	fxFrames     = 6 // duration of a normal shake, in frames
	fxCritFrames = 9 // duration of a shake on a critical hit
	fxEnvOff     = "SUDO_DRAGON_NO_FX"
)

// fxState is the animation in progress. frame >= frames: finished.
type fxState struct {
	gen, frame, frames int
	allAmp             int         // amplitude of the whole-area shake (hit received), 0 = none
	foeAmp             int         // amplitude of the enemy column shake (hit dealt)
	flashHP            bool        // the player's HP blink red
	hits               map[int]int // damage shown per monster hit
	crits              map[int]bool
}

func (f fxState) active() bool { return f.frame < f.frames }

// shakeTickMsg advances the animation; gen discards ticks from a replaced animation.
type shakeTickMsg struct{ gen int }

func shakeTick(gen int) tea.Cmd {
	return tea.Tick(fxFrameEvery, func(time.Time) tea.Msg { return shakeTickMsg{gen} })
}

// fxDisabled tells whether effects are turned off by the environment.
func fxDisabled() bool { return os.Getenv(fxEnvOff) != "" }

// startFx starts the animation of a combat turn and returns the command for the first tick (nil: nothing to animate).
func (m *Model) startFx(c protocol.Combat) tea.Cmd {
	if m.noFX {
		return nil
	}
	fx := fxState{gen: m.fx.gen + 1, hits: map[int]int{}, crits: map[int]bool{}}
	crit := false
	for _, s := range c.Strikes {
		if !s.Hit {
			continue
		}
		amp := 1
		if s.Crit {
			amp, crit = 2, true
		}
		if s.ByPlayer {
			fx.foeAmp = max(fx.foeAmp, amp)
			fx.hits[s.Foe] += s.Dmg
			fx.crits[s.Foe] = fx.crits[s.Foe] || s.Crit
		} else {
			fx.allAmp = max(fx.allAmp, amp)
			fx.flashHP = true
		}
	}
	if fx.allAmp == 0 && fx.foeAmp == 0 {
		m.fx = fxState{gen: fx.gen, frame: 1, frames: 1} // nothing to animate, but the old ticks become stale
		return nil
	}
	fx.frames = fxFrames
	if crit {
		fx.frames = fxCritFrames
	}
	m.fx = fx
	return shakeTick(fx.gen)
}

// updateShake advances the animation by one step.
func (m Model) updateShake(msg shakeTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.fx.gen || !m.fx.active() {
		return m, nil
	}
	m.fx.frame++
	if !m.fx.active() {
		return m, nil
	}
	return m, shakeTick(m.fx.gen)
}

// shakeOffset gives the offset (columns, lines) at frame frame of a shake of amplitude amp lasting frames
// frames: it alternates in sign and decays down to 0.
func shakeOffset(frame, frames, amp int) (dx, dy int) {
	if amp == 0 || frame >= frames {
		return 0, 0
	}
	mag := (amp*(frames-frame) + frames - 1) / frames
	if frame%2 == 1 {
		mag = -mag
	}
	return mag, mag / 2
}

// allShake is the current offset of the whole combat area.
func (m Model) allShake() (dx, dy int) {
	return shakeOffset(m.fx.frame, m.fx.frames, m.fx.allAmp)
}

// foeShake is the current offset of the enemy column.
func (m Model) foeShake() (dx, dy int) {
	return shakeOffset(m.fx.frame, m.fx.frames, m.fx.foeAmp)
}

// foeHitText is the damage text to show next to a monster that was hit ("-7", "-12 !").
func (m Model) foeHitText(i int) string {
	if !m.fx.active() || m.fx.hits[i] == 0 {
		return ""
	}
	if m.fx.crits[i] {
		return fmt.Sprintf(" -%d !", m.fx.hits[i])
	}
	return fmt.Sprintf(" -%d", m.fx.hits[i])
}

// flashing: hit elements blink every other frame.
func (m Model) flashing() bool { return m.fx.active() && m.fx.frame%2 == 0 }

// shiftBlock shifts a block of lines (ANSI included) by dx columns and dy lines while keeping exactly w x h:
// what goes out is cut, what comes in is empty.
func shiftBlock(s string, dx, dy, w, h int) string {
	if dx == 0 && dy == 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		switch {
		case dx > 0:
			l = strings.Repeat(" ", dx) + ansi.Truncate(l, max(0, w-dx), "")
		case dx < 0:
			l = ansi.Cut(l, -dx, max(-dx, ansi.StringWidth(l)))
		}
		lines[i] = ui.Fit(l, w)
	}
	blank := strings.Repeat(" ", w)
	switch {
	case dy > 0:
		lines = append(make([]string, dy), lines...)
		for i := range dy {
			lines[i] = blank
		}
		lines = lines[:h]
	case dy < 0:
		lines = lines[min(-dy, len(lines)):]
		for len(lines) < h {
			lines = append(lines, blank)
		}
	}
	return strings.Join(lines[:h], "\n")
}

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

// Effets visuels du combat: secousse de la zone de combat quand on est touché, de la colonne ennemie quand on
// touche. L'animation est pilotée par des ticks et ne dépend que de l'état du modèle (donc testable).
const (
	fxFrameEvery = 60 * time.Millisecond
	fxFrames     = 6 // durée d'une secousse normale, en images
	fxCritFrames = 9 // durée d'une secousse sur coup critique
	fxEnvOff     = "SUDO_DRAGON_NO_FX"
)

// fxState est l'animation en cours. frame >= frames: terminée.
type fxState struct {
	gen, frame, frames int
	allAmp             int         // amplitude de la secousse de toute la zone (coup reçu), 0 = aucune
	foeAmp             int         // amplitude de la secousse de la colonne ennemie (coup donné)
	flashHP            bool        // les PV du joueur clignotent en rouge
	hits               map[int]int // dégâts affichés par monstre touché
	crits              map[int]bool
}

func (f fxState) active() bool { return f.frame < f.frames }

// shakeTickMsg avance l'animation; gen écarte les ticks d'une animation remplacée.
type shakeTickMsg struct{ gen int }

func shakeTick(gen int) tea.Cmd {
	return tea.Tick(fxFrameEvery, func(time.Time) tea.Msg { return shakeTickMsg{gen} })
}

// fxDisabled dit si les effets sont coupés par l'environnement.
func fxDisabled() bool { return os.Getenv(fxEnvOff) != "" }

// startFx lance l'animation d'un tour de combat et renvoie la commande du premier tick (nil: rien à animer).
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
		m.fx = fxState{gen: fx.gen, frame: 1, frames: 1} // rien à animer, mais les anciens ticks deviennent périmés
		return nil
	}
	fx.frames = fxFrames
	if crit {
		fx.frames = fxCritFrames
	}
	m.fx = fx
	return shakeTick(fx.gen)
}

// updateShake fait avancer l'animation d'un cran.
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

// shakeOffset donne le décalage (colonnes, lignes) à l'image frame d'une secousse d'amplitude amp qui dure frames
// images: il alterne de signe et décroît jusqu'à 0.
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

// allShake est le décalage courant de toute la zone de combat.
func (m Model) allShake() (dx, dy int) {
	return shakeOffset(m.fx.frame, m.fx.frames, m.fx.allAmp)
}

// foeShake est le décalage courant de la colonne ennemie.
func (m Model) foeShake() (dx, dy int) {
	return shakeOffset(m.fx.frame, m.fx.frames, m.fx.foeAmp)
}

// foeHitText est le texte de dégâts à afficher à côté d'un monstre touché ("-7", "-12 !").
func (m Model) foeHitText(i int) string {
	if !m.fx.active() || m.fx.hits[i] == 0 {
		return ""
	}
	if m.fx.crits[i] {
		return fmt.Sprintf(" -%d !", m.fx.hits[i])
	}
	return fmt.Sprintf(" -%d", m.fx.hits[i])
}

// flashing: les éléments touchés clignotent une image sur deux.
func (m Model) flashing() bool { return m.fx.active() && m.fx.frame%2 == 0 }

// shiftBlock décale un bloc de lignes (ANSI compris) de dx colonnes et dy lignes en gardant exactement w x h:
// ce qui sort est coupé, ce qui entre est vide.
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

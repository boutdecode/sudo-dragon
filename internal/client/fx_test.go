package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

func TestShiftBlockKeepsDimensions(t *testing.T) {
	block := "abcdef\n\x1b[31mghijkl\x1b[0m\nmnopqr"
	for _, d := range [][2]int{{0, 0}, {1, 0}, {-1, 0}, {2, 1}, {-2, -1}, {0, 1}, {0, -1}} {
		out := strings.Split(shiftBlock(block, d[0], d[1], 6, 3), "\n")
		if len(out) != 3 {
			t.Fatalf("%v: %d lignes", d, len(out))
		}
		for _, l := range out {
			if w := ansi.StringWidth(l); w != 6 {
				t.Fatalf("%v: largeur %d: %q", d, w, l)
			}
		}
	}
	if got := ansi.Strip(strings.Split(shiftBlock(block, 2, 0, 6, 3), "\n")[0]); got != "  abcd" {
		t.Fatalf("décalage à droite: %q", got)
	}
	if got := ansi.Strip(strings.Split(shiftBlock(block, -2, 0, 6, 3), "\n")[0]); got != "cdef  " {
		t.Fatalf("décalage à gauche: %q", got)
	}
	if got := ansi.Strip(strings.Split(shiftBlock(block, 0, 1, 6, 3), "\n")[1]); got != "abcdef" {
		t.Fatalf("décalage vers le bas: %q", got)
	}
}

func TestShakeOffsetDecaysToZero(t *testing.T) {
	prev := 99
	for f := 0; f < fxCritFrames; f++ {
		dx, _ := shakeOffset(f, fxCritFrames, 2)
		if abs(dx) > prev || abs(dx) > 2 {
			t.Fatalf("image %d: amplitude %d ne décroît pas (précédente %d)", f, abs(dx), prev)
		}
		prev = abs(dx)
	}
	if dx, dy := shakeOffset(fxCritFrames, fxCritFrames, 2); dx != 0 || dy != 0 {
		t.Fatal("la secousse doit être finie")
	}
	if dx, _ := shakeOffset(0, fxFrames, 0); dx != 0 {
		t.Fatal("amplitude nulle: pas de secousse")
	}
}

func withStrikes(c protocol.Combat, s ...protocol.Strike) protocol.Combat { c.Strikes = s; return c }

func TestBeingHitShakesTheWholeScreenThenStops(t *testing.T) {
	m := playingModel(t, 110, 34)
	m = startCombat(t, m)
	c := withStrikes(packCombat(false), protocol.Strike{Foe: 0, Dmg: 3, Hit: true})
	cmd := m.startFx(c)
	if cmd == nil || m.fx.allAmp == 0 || m.fx.foeAmp != 0 || !m.fx.flashHP {
		t.Fatalf("coup reçu: cmd=%v fx=%+v", cmd != nil, m.fx)
	}
	gen := m.fx.gen
	for i := 0; i < fxFrames; i++ {
		if !m.fx.active() {
			t.Fatalf("animation finie trop tôt (image %d)", i)
		}
		tm, next := m.updateShake(shakeTickMsg{gen})
		m = tm.(Model)
		if m.fx.active() != (next != nil) {
			t.Fatalf("image %d: la commande suivante doit exister tant que l'animation dure", i)
		}
	}
	if m.fx.active() {
		t.Fatal("l'animation devait être finie")
	}
	if tm, next := m.updateShake(shakeTickMsg{gen}); next != nil || tm.(Model).fx.frame != m.fx.frame {
		t.Fatal("un tick après la fin ne doit rien faire")
	}
}

func TestStaleTicksAreIgnoredAndCritsLastLonger(t *testing.T) {
	m := playingModel(t, 110, 34)
	m.startFx(withStrikes(packCombat(false), protocol.Strike{Foe: 0, Dmg: 3, Hit: true}))
	old := m.fx.gen
	m.startFx(withStrikes(packCombat(false), protocol.Strike{Foe: 0, Dmg: 9, Hit: true, Crit: true}))
	if m.fx.frames != fxCritFrames || m.fx.allAmp != 2 {
		t.Fatalf("un critique doit durer plus longtemps et secouer plus fort: %+v", m.fx)
	}
	frame := m.fx.frame
	if tm, next := m.updateShake(shakeTickMsg{old}); next != nil || tm.(Model).fx.frame != frame {
		t.Fatal("un tick périmé doit être ignoré")
	}
}

func TestDealingAHitShakesOnlyTheEnemyColumn(t *testing.T) {
	m := playingModel(t, 120, 40)
	tm, _ := m.Update(combatMsg(t, packCombat(false)))
	m = tm.(Model)
	m.startFx(withStrikes(packCombat(false), protocol.Strike{ByPlayer: true, Foe: 2, Dmg: 7, Hit: true}))
	if m.fx.allAmp != 0 || m.fx.foeAmp == 0 || m.fx.flashHP {
		t.Fatalf("coup donné: %+v", m.fx)
	}
	if m.foeHitText(2) != " -7" || m.foeHitText(0) != "" {
		t.Fatalf("texte de dégâts: %q / %q", m.foeHitText(2), m.foeHitText(0))
	}
	if !strings.Contains(plainView(m), "-7") {
		t.Fatal("les dégâts doivent s'afficher à côté du monstre touché")
	}
}

func TestMissesAndNoFXDoNotAnimate(t *testing.T) {
	m := playingModel(t, 110, 34)
	if cmd := m.startFx(withStrikes(packCombat(false), protocol.Strike{Foe: 0, Hit: false})); cmd != nil || m.fx.active() {
		t.Fatal("un coup raté n'anime rien")
	}
	m.noFX = true
	if cmd := m.startFx(withStrikes(packCombat(false), protocol.Strike{Foe: 0, Dmg: 3, Hit: true})); cmd != nil || m.fx.active() {
		t.Fatal("effets coupés: aucune animation")
	}
}

func TestShakingViewStaysWithinTheScreen(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {110, 34}, {200, 60}} {
		m := playingModel(t, sz[0], sz[1])
		tm, _ := m.Update(combatMsg(t, packCombat(false)))
		m = tm.(Model)
		m.startFx(withStrikes(packCombat(false), protocol.Strike{Foe: 1, Dmg: 5, Hit: true, Crit: true}, protocol.Strike{ByPlayer: true, Foe: 2, Dmg: 4, Hit: true}))
		for f := 0; f < fxCritFrames; f++ {
			m.fx.frame = f
			lines := strings.Split(plainView(m), "\n")
			if len(lines) > sz[1] {
				t.Fatalf("%v image %d: %d lignes > hauteur", sz, f, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Fatalf("%v image %d ligne %d: largeur %d > %d", sz, f, i, w, sz[0])
				}
			}
		}
	}
}

package client

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// Character creation screen: the server rolled the dice (3d6), the player may
// reroll, three times in total, an ability of their choice, then picks their class and confirms.

// createState is the current screen.
type createState struct {
	data  protocol.Create
	sel   int // chosen ability (for the reroll)
	class int // chosen class
}

// cleanCreate brings a received roll back within safe bounds: six values, known classes.
func cleanCreate(c protocol.Create) protocol.Create {
	sc := make([]int, hero.NumAbilities)
	for i := range sc {
		if i < len(c.Scores) {
			sc[i] = max(0, min(c.Scores[i], 99))
		}
	}
	c.Scores = sc
	c.Left = max(0, min(c.Left, hero.MaxRolls))
	c.Level = max(1, min(c.Level, 999))
	var cls []protocol.ClassInfo
	for _, ci := range c.Classes {
		if hero.Class(ci.ID).Valid() {
			cls = append(cls, protocol.ClassInfo{ID: ci.ID, Die: max(1, min(ci.Die, 100)), Mana: max(0, min(ci.Mana, 100))})
		}
	}
	if len(cls) == 0 {
		for _, cl := range hero.Classes {
			cls = append(cls, protocol.ClassInfo{ID: string(cl), Die: cl.HitDie(), Mana: cl.ManaBase()})
		}
	}
	c.Classes = cls
	return c
}

func newCreateState(c protocol.Create) *createState { return &createState{data: cleanCreate(c)} }

// scores returns the current roll as a hero packet.
func (c *createState) scores() hero.Scores {
	var s hero.Scores
	copy(s[:], c.data.Scores)
	return s
}

func (c *createState) classID() hero.Class {
	if c.class < 0 || c.class >= len(c.data.Classes) {
		return hero.Warrior
	}
	return hero.Class(c.data.Classes[c.class].ID)
}

// updateCreateKey handles a key on the creation screen.
func (m Model) updateCreateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := m.create
	if c == nil || m.conn == nil {
		return m, nil
	}
	nc := *c
	switch msg.String() {
	case "up", "k":
		nc.sel = (nc.sel + int(hero.NumAbilities) - 1) % int(hero.NumAbilities)
	case "down", "j":
		nc.sel = (nc.sel + 1) % int(hero.NumAbilities)
	case "left", "h":
		nc.class = (nc.class + len(nc.data.Classes) - 1) % len(nc.data.Classes)
	case "right", "l", "tab":
		nc.class = (nc.class + 1) % len(nc.data.Classes)
	case "r":
		if nc.data.Left > 0 {
			m.conn.Send(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateReroll, Index: nc.sel})
		}
	case "enter":
		m.conn.Send(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: string(nc.classID())})
	case "esc":
		m.conn.Close()
		m.conn, m.create = nil, nil
		m.state = stateLogin
		return m, nil
	}
	m.create = &nc
	return m, nil
}

// updateCreateEvent handles a server message during creation.
func (m Model) updateCreateEvent(ev Event) (tea.Model, tea.Cmd) {
	if ev.Err != nil {
		if m.conn != nil {
			m.conn.Close()
		}
		m.conn, m.create = nil, nil
		m.state = stateLogin
		m.errMsg = m.t("client.lost", ev.Err.Error())
		return m, nil
	}
	switch ev.Env.T {
	case protocol.TCreate:
		var cr protocol.Create
		if err := ev.Env.Payload(&cr); err == nil && m.create != nil {
			nc := *m.create
			nc.data = cleanCreate(cr)
			nc.class = min(nc.class, len(nc.data.Classes)-1)
			m.create = &nc
		}
	case protocol.TWelcome:
		var w protocol.Welcome
		if err := ev.Env.Payload(&w); err == nil {
			conn := m.conn
			m.create = nil
			return m.enterGame(conn, w)
		}
	case protocol.TError:
		var e protocol.Error
		if err := ev.Env.Payload(&e); err == nil {
			m.errMsg = e.Msg
		}
	}
	return m, waitEvent(m.conn)
}

// abilityLine describes an ability: "STR  Strength  14 (+2)  effect".
func (m Model) abilityLine(a hero.Ability, v, w int) string {
	mod := hero.Mod(v)
	k := a.Key()
	left := fmt.Sprintf("%s %-12s %2d (%+d)", m.t("hero.ability."+k), m.t("hero.ability."+k+".long"), v, mod)
	return ui.Fit(left+"  "+dimStyle.Render(m.t("hero.ability."+k+".desc")), w)
}

// viewCreate draws the creation screen.
func (m Model) viewCreate() string {
	c := m.create
	if c == nil {
		return ""
	}
	panelW := min(m.width, 74)
	inner := panelW - 4
	sc := c.scores()

	lines := []string{"", " " + dimStyle.Render(m.t("create.intro", c.data.Level)), ""}
	for i := hero.Ability(0); i < hero.NumAbilities; i++ {
		row := m.abilityLine(i, sc[i], inner-3)
		if int(i) == c.sel {
			lines = append(lines, " "+ui.Select.Render("› "+ui.Fit(ansi.Strip(row), inner-2)))
		} else {
			lines = append(lines, "   "+row)
		}
	}
	lines = append(lines, "", " "+m.t("create.rerolls", c.data.Left, hero.MaxRolls), "")

	var tabs []string
	for i, ci := range c.data.Classes {
		label := m.t("hero.class."+ci.ID) + fmt.Sprintf(" d%d", ci.Die)
		if i == c.class {
			tabs = append(tabs, ui.Select.Render(" › "+label+" "))
		} else {
			tabs = append(tabs, dimStyle.Render("   "+label+" "))
		}
	}
	lines = append(lines, " "+strings.Join(tabs, " "))
	cl := c.classID()
	hp := hero.FirstHP(cl, sc)
	lines = append(lines, " "+dimStyle.Render(m.t("hero.class."+string(cl)+".desc")))
	lines = append(lines, " "+goldStyle.Render(m.t("create.preview", hp, hero.MaxMana(cl, sc, 1))))
	lines = append(lines, "", ui.Center(ui.KeyBar(
		[2]string{"↑↓", m.t("create.key.stat")}, [2]string{"r", m.t("create.key.reroll")},
		[2]string{"←→", m.t("create.key.class")}, [2]string{"↵", m.t("create.key.ok")}, [2]string{"esc", m.t("create.key.back")}), inner))
	if m.errMsg != "" {
		lines = append(lines, ui.Center(errStyle.Render(m.errMsg), inner))
	}
	box := ui.Panel(m.t("create.title"), strings.Join(lines, "\n"), panelW, len(lines)+2)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

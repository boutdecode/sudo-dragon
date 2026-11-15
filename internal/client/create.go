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
// reroll, three times in total, an ability of their choice, then picks their class and race
// (+2 to one ability) and confirms.

// createState is the current screen.
type createState struct {
	data  protocol.Create
	sel   int // selected row: an ability (for the reroll), then rowClass and rowRace
	class int // chosen class
	race  int // chosen race
}

// cleanCreate brings a received roll back within safe bounds: six values, known classes and races.
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
	var races []protocol.RaceInfo
	for _, ri := range c.Races {
		if r := hero.Race(ri.ID); r.Valid() {
			races = append(races, protocol.RaceInfo{ID: ri.ID, Ability: int(r.Ability()), Bonus: hero.RaceBonus})
		}
	}
	if len(races) == 0 {
		for _, r := range hero.Races {
			races = append(races, protocol.RaceInfo{ID: string(r), Ability: int(r.Ability()), Bonus: hero.RaceBonus})
		}
	}
	c.Races = races
	return c
}

// Rows of the creation screen after the six abilities: ↑↓ moves between them all, ←→
// changes the class or race of the selected row.
const (
	rowClass = int(hero.NumAbilities) + iota
	rowRace
	numCreateRows
)

func newCreateState(c protocol.Create) *createState { return &createState{data: cleanCreate(c)} }

// rolled returns the dice rolled, without the racial bonus.
func (c *createState) rolled() hero.Scores {
	var s hero.Scores
	copy(s[:], c.data.Scores)
	return s
}

// scores returns the abilities the character will have: dice plus racial bonus.
func (c *createState) scores() hero.Scores { return c.raceID().Apply(c.rolled()) }

func (c *createState) raceID() hero.Race {
	if c.race < 0 || c.race >= len(c.data.Races) {
		return hero.Human
	}
	return hero.Race(c.data.Races[c.race].ID)
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
		nc.sel = (nc.sel + numCreateRows - 1) % numCreateRows
	case "down", "j":
		nc.sel = (nc.sel + 1) % numCreateRows
	case "left", "h", "right", "l":
		step := 1
		if k := msg.String(); k == "left" || k == "h" {
			step = -1
		}
		switch nc.sel {
		case rowClass:
			nc.class = (nc.class + len(nc.data.Classes) + step) % len(nc.data.Classes)
		case rowRace:
			nc.race = (nc.race + len(nc.data.Races) + step) % len(nc.data.Races)
		}
	case "r":
		if nc.data.Left > 0 && nc.sel < int(hero.NumAbilities) {
			m.conn.Send(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateReroll, Index: nc.sel})
		}
	case "enter":
		m.conn.Send(protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: string(nc.classID()), Race: string(nc.raceID())})
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
			nc.race = min(nc.race, len(nc.data.Races)-1)
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

// abilityLine describes an ability: "STR  Strength  16 (+3) +2  effect", where +2 is the
// racial bonus already included in the value (blank for the other abilities).
func (m Model) abilityLine(a hero.Ability, v, bonus, w int) string {
	mod := hero.Mod(v)
	k := a.Key()
	left := fmt.Sprintf("%s %-12s %2d (%+d)", m.t("hero.ability."+k), m.t("hero.ability."+k+".long"), v, mod)
	mark := "   "
	if bonus != 0 {
		mark = goldStyle.Render(fmt.Sprintf("%+d", bonus)) + " "
	}
	return ui.Fit(left+" "+mark+dimStyle.Render(m.t("hero.ability."+k+".desc")), w)
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
	race := c.raceID()

	lines := []string{"", " " + dimStyle.Render(m.t("create.intro", c.data.Level)), ""}
	for i := hero.Ability(0); i < hero.NumAbilities; i++ {
		bonus := 0
		if i == race.Ability() {
			bonus = hero.RaceBonus
		}
		row := m.abilityLine(i, sc[i], bonus, inner-3)
		if int(i) == c.sel {
			lines = append(lines, " "+ui.Select.Render("› "+ui.Fit(ansi.Strip(row), inner-2)))
		} else {
			lines = append(lines, "   "+row)
		}
	}
	lines = append(lines, "", " "+m.t("create.rerolls", c.data.Left, hero.MaxRolls))

	// choiceRow draws a class or race row: label, then the options, the chosen one highlighted
	// (in the selection colour when the row has the cursor).
	choiceRow := func(row int, label string, opts []string, chosen int) string {
		focus := c.sel == row
		cursor := "  "
		if focus {
			cursor = ui.Select.Render("›") + " "
		}
		var parts []string
		for i, o := range opts {
			switch {
			case i == chosen && focus:
				parts = append(parts, ui.Select.Render("‹ "+o+" ›"))
			case i == chosen:
				parts = append(parts, titleStyle.Render("‹ "+o+" ›"))
			default:
				parts = append(parts, dimStyle.Render(" "+o+" "))
			}
		}
		return ui.Fit(" "+cursor+fmt.Sprintf("%-6s", label)+" "+strings.Join(parts, " "), inner)
	}

	var classes []string
	for _, ci := range c.data.Classes {
		classes = append(classes, m.t("hero.class."+ci.ID)+fmt.Sprintf(" d%d", ci.Die))
	}
	cl := c.classID()
	lines = append(lines, choiceRow(rowClass, m.t("create.class"), classes, c.class),
		"          "+dimStyle.Render(m.t("hero.class."+string(cl)+".desc")))

	var races []string
	for i, ri := range c.data.Races {
		label := m.t("hero.race." + ri.ID)
		if i == c.race { // only the chosen race shows its bonus: the line stays narrow
			label += fmt.Sprintf(" %+d %s", ri.Bonus, m.t("hero.ability."+hero.Ability(ri.Ability).Key()))
		}
		races = append(races, label)
	}
	lines = append(lines, choiceRow(rowRace, m.t("create.race"), races, c.race))
	lines = append(lines, " "+goldStyle.Render(m.t("create.preview", hero.FirstHP(cl, sc), hero.MaxMana(cl, sc, 1))), "")
	lines = append(lines,
		ui.Center(ui.KeyBar([2]string{"↑↓", m.t("create.key.stat")}, [2]string{"←→", m.t("create.key.change")},
			[2]string{"r", m.t("create.key.reroll")}), inner),
		ui.Center(ui.KeyBar([2]string{"↵", m.t("create.key.ok")}, [2]string{"esc", m.t("create.key.back")}), inner))
	if m.errMsg != "" {
		lines = append(lines, ui.Center(errStyle.Render(m.errMsg), inner))
	}
	box := ui.Panel(m.t("create.title"), strings.Join(lines, "\n"), panelW, len(lines)+2)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

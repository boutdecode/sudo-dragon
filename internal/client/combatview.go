package client

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// detailMinHeight is the minimal height of the action column to show the detail panel.
const detailMinHeight = 6

// combatView draws the combat screen in exactly w x h cells: on the left the Combat box in two
// columns (player, enemy), on the right the actions and the detail of the chosen action, the log below.
func (m Model) combatView(w, h int) string {
	c := m.combat
	name := m.groupTitle()

	actionW := min(34, w/3)
	leftW := w - actionW
	logH := min(8, h-6)
	midH := h - logH

	colW := (leftW - 2 - 1) / 2 // two columns and a rule
	ih := midH - 2
	left := m.playerColumn(colW, ih)
	right := shiftBlockLines(m.enemyColumn(leftW-2-colW-1, ih), m.foeShake, leftW-2-colW-1)
	sep := dimStyle.Render("│")
	rows := make([]string, ih)
	for i := range rows {
		rows[i] = ui.Fit(left[i], colW) + sep + ui.Fit(right[i], leftW-2-colW-1)
	}
	title := m.t("fight.title", name)
	fieldPanel := ui.Panel(title, strings.Join(rows, "\n"), leftW, midH)

	var actions []string
	if c.Over {
		switch c.Result {
		case protocol.ResultWon:
			actions = append(actions, " "+ui.OK.Bold(true).Render(m.t("fight.victory")))
		case protocol.ResultLost:
			actions = append(actions, " "+errStyle.Bold(true).Render(m.t("fight.defeat")))
		default:
			actions = append(actions, " "+dimStyle.Render(m.t("fight.fled")))
		}
		actions = append(actions, " "+dimStyle.Render(m.t("fight.enter_continue")))
	} else {
		for i, it := range combatMenu {
			label := m.t(it.label)
			if it.action == "attack" {
				label = m.t("fight.act.attack." + string(hero.Kind(c.Style).OrMelee()))
			}
			if it.action == "item" {
				label = fmt.Sprintf("%s (%d)", label, c.Potions)
			}
			if i == m.combatSel {
				actions = append(actions, ui.Select.Render(ui.Fit(" › ["+it.key+"] "+label, actionW-2)))
			} else {
				actions = append(actions, "   ["+it.key+"] "+label)
			}
		}
	}

	var side string
	if actionsH := len(combatMenu) + 2; !c.Over && midH >= actionsH+detailMinHeight {
		detail := strings.Join(m.actionDetail(combatMenu[m.combatSel].action, actionW-3), "\n")
		side = lipgloss.JoinVertical(lipgloss.Left,
			ui.Panel(m.t("fight.actions"), strings.Join(actions, "\n"), actionW, actionsH),
			ui.Panel(m.t("fight.detail"), detail, actionW, midH-actionsH),
		)
	} else {
		side = ui.Panel(m.t("fight.actions"), strings.Join(actions, "\n"), actionW, midH)
	}
	mid := lipgloss.JoinHorizontal(lipgloss.Top, fieldPanel, side)
	dx, dy := m.allShake()
	return shiftBlock(mid+"\n"+journalPanel(m.t("fight.journal"), m.combatLog, w, logH), dx, dy, w, h)
}

// playerColumn: the player's information, over h lines of w cells.
func (m Model) playerColumn(w, h int) []string {
	c, st := m.combat, m.snap.Stats
	barW := max(4, min(30, w-12))
	hp := fmt.Sprintf(" %s %3d/%d", ui.Bar(c.PlayerHP, c.PlayerMaxHP, barW), c.PlayerHP, c.PlayerMaxHP)
	if m.fx.flashHP && m.flashing() { // hit received: HP blink red
		hp = " " + errStyle.Bold(true).Render(ansi.Strip(hp[1:]))
	}
	lines := []string{
		m.t("fight.you") + " " + dimStyle.Render(m.t("fight.def", c.PlayerDef)),
		hp,
	}
	if c.PlayerMaxMP > 0 {
		lines = append(lines, fmt.Sprintf(" %s %3d/%d %s", ui.Bar(c.PlayerMana, c.PlayerMaxMP, barW), c.PlayerMana, c.PlayerMaxMP, dimStyle.Render(m.t("hud.mana_short"))))
	}
	lines = append(lines, "",
		" "+m.t("fight.hit", st.Hit),
		" "+m.t("fight.dmg", st.Dmg),
		" "+m.t("bag.potions", c.Potions),
		"", " "+titleStyle.Render(m.t("bag.worn")))
	for _, slot := range []gear.Slot{gear.Weapon, gear.Armor, gear.Shield, gear.Helm} {
		if it, ok := m.wornIn(slot); ok {
			lines = append(lines, " "+dimStyle.Render(m.t("gear.slot."+it.Slot)+" ")+it.Name)
		}
	}
	return padLines(lines, h)
}

// groupTitle summarizes the enemies for the box title: "Wolf ×2, Rat".
func (m Model) groupTitle() string {
	var order []string
	count := map[string]int{}
	for _, e := range m.combat.Enemies {
		if count[e.Kind] == 0 {
			order = append(order, e.Kind)
		}
		count[e.Kind]++
	}
	for i, k := range order {
		order[i] = render.MonsterName(m.lang, k)
		if count[k] > 1 {
			order[i] += fmt.Sprintf(" ×%d", count[k])
		}
	}
	return strings.Join(order, ", ")
}

// enemyColumn: the portrait of the strongest monster (half of the available space), then the list of enemies.
func (m Model) enemyColumn(w, h int) []string {
	c := m.combat
	if len(c.Enemies) == 0 {
		return padLines(nil, h)
	}
	barW := max(4, min(30, w-17)) // room for the damage shown alongside
	redStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColErr))

	// One name line and one HP line per enemy; the target adds its sheet if space allows.
	statsRoom := h-2*len(c.Enemies)-6 >= 1
	var info []string
	for i, e := range c.Enemies {
		dead := e.HP <= 0
		mark, nameStyle := "  ", redStyle
		if dead {
			nameStyle = dimStyle
		}
		if i == m.combatTarget && !dead && !c.Over {
			mark = ui.Select.Render("› ")
		}
		head := " " + mark + nameStyle.Render(e.Name) + " " + dimStyle.Render(m.t("fight.def", e.Def))
		if dead {
			head = " " + mark + dimStyle.Render(e.Name+" ✝")
		}
		hpLine := fmt.Sprintf("   %s %3d/%d", ui.Bar(e.HP, e.MaxHP, barW), e.HP, e.MaxHP)
		if txt := m.foeHitText(i); txt != "" { // hit dealt: damage next to the bar, name highlighted every other frame
			hpLine += errStyle.Bold(true).Render(txt)
			if m.flashing() && !dead {
				head = " " + mark + ui.Select.Render(e.Name)
			}
		}
		info = append(info, head, hpLine)
		if statsRoom && i == m.combatTarget && !dead && e.Dmg != "" {
			stat := m.t("fight.hit", e.Hit) + " · " + m.t("fight.dmg", e.Dmg)
			if e.Red != "" && e.Red != "0" {
				stat += " · " + m.t("bag.item.red", e.Red)
			}
			info = append(info, "   "+dimStyle.Render(stat))
		}
	}

	var lines []string
	boss := c.Enemies[min(max(0, c.Boss), len(c.Enemies)-1)].Kind
	room := h - len(info) - 1
	if img := render.MonsterImage(boss, (w-2)/2, room/2); img != nil {
		for _, l := range img {
			lines = append(lines, ui.Center(l, w))
		}
		lines = append(lines, "")
	} else if art := render.MonsterArt(boss); room >= len(art)+1 {
		style := render.ArtStyle(boss)
		for _, l := range art {
			lines = append(lines, ui.Center(style.Render(l), w))
		}
		lines = append(lines, "")
	}
	return padLines(append(lines, info...), h)
}

// shiftBlockLines shifts the lines of a w-cell column by the offset given by off.
func shiftBlockLines(lines []string, off func() (int, int), w int) []string {
	dx, dy := off()
	if dx == 0 && dy == 0 {
		return lines
	}
	return strings.Split(shiftBlock(strings.Join(lines, "\n"), dx, dy, w, len(lines)), "\n")
}

// padLines pads (or cuts) the lines to exactly h.
func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// actionDetail describes the action chosen in the combat menu (w cells wide).
func (m Model) actionDetail(action string, w int) []string {
	c, st := m.combat, m.snap.Stats
	var out []string
	switch action {
	case "attack":
		style := hero.Kind(c.Style).OrMelee()
		wpn, ok := m.wornIn(gear.Weapon)
		if !ok {
			out = append(out, " "+titleStyle.Render(m.t("fight.detail.bare")))
		} else {
			out = append(out, " "+titleStyle.Render(wpn.Name), " "+goldStyle.Render(m.bonusText(wpn)))
			for _, l := range wrap(wpn.Desc, w-1) {
				out = append(out, " "+dimStyle.Render(l))
			}
		}
		out = append(out, "", " "+m.t("fight.detail.stats", st.Hit, st.Dmg))
		if t := m.combatTarget; t >= 0 && t < len(c.Enemies) && len(c.Enemies) > 1 {
			out = append(out, " "+m.t("fight.detail.target", c.Enemies[t].Name))
		}
		if style == hero.Magic {
			out = append(out, " "+m.t("fight.detail.spell", hero.SpellCost, c.PlayerMana, c.PlayerMaxMP), " "+dimStyle.Render(m.t("fight.detail.club")))
		}
	case "item":
		out = append(out, " "+m.t("fight.detail.item", combat.HealAmount, c.Potions))
	case "flee":
		out = append(out, " "+m.t("fight.detail.flee", int(combat.FleeChance*100)))
	}
	return out
}

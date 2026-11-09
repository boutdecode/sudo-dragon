package client

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// detailMinHeight est la hauteur minimale de la colonne d'actions pour afficher le panneau de détail.
const detailMinHeight = 6

// combatView dessine l'écran de combat dans exactement w x h cellules: à gauche l'encart Combat en deux
// colonnes (joueur, ennemi), à droite les actions et le détail de l'action choisie, dessous le journal.
func (m Model) combatView(w, h int) string {
	c := m.combat
	name := m.groupTitle()

	actionW := min(34, w/3)
	leftW := w - actionW
	logH := min(8, h-6)
	midH := h - logH

	colW := (leftW - 2 - 1) / 2 // deux colonnes et un filet
	ih := midH - 2
	left := m.playerColumn(colW, ih)
	right := m.enemyColumn(leftW-2-colW-1, ih)
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
	return mid + "\n" + journalPanel(m.t("fight.journal"), m.combatLog, w, logH)
}

// playerColumn: les informations du joueur, sur h lignes de w cellules.
func (m Model) playerColumn(w, h int) []string {
	c, st := m.combat, m.snap.Stats
	barW := max(4, min(30, w-12))
	lines := []string{
		m.t("fight.you") + " " + dimStyle.Render(m.t("fight.def", c.PlayerDef)),
		fmt.Sprintf(" %s %3d/%d", ui.Bar(c.PlayerHP, c.PlayerMaxHP, barW), c.PlayerHP, c.PlayerMaxHP),
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

// groupTitle résume les ennemis pour le titre de l'encart: "Loup ×2, Rat".
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

// enemyColumn: le portrait du monstre le plus fort (la moitié de la place disponible), puis la liste des ennemis.
func (m Model) enemyColumn(w, h int) []string {
	c := m.combat
	if len(c.Enemies) == 0 {
		return padLines(nil, h)
	}
	barW := max(4, min(30, w-12))
	redStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColErr))

	// Une ligne de nom et une de PV par ennemi; la cible ajoute sa fiche si la place le permet.
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
		info = append(info, head, fmt.Sprintf("   %s %3d/%d", ui.Bar(e.HP, e.MaxHP, barW), e.HP, e.MaxHP))
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

// padLines complète (ou coupe) les lignes à exactement h.
func padLines(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// actionDetail décrit l'action choisie dans le menu de combat (w cellules de large).
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

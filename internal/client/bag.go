package client

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// Sac d'équipement : emplacements portés et objets, dans l'écran du sac (touche I).

const maxBagRows = 64

func cleanItem(it protocol.ItemInfo) protocol.ItemInfo {
	it.Name = textsafe.Clean(it.Name, gear.MaxName)
	it.Desc = textsafe.Clean(it.Desc, gear.MaxDesc)
	it.Origin = textsafe.Clean(it.Origin, 60)
	it.Dice, it.Reduction = cleanDice(it.Dice), cleanDice(it.Reduction)
	it.Family = textsafe.Clean(it.Family, 20)
	if _, ok := gear.FamilyByID(gear.Slot(it.Slot), it.Family); !ok {
		it.Family = gear.DefaultFamily(gear.Slot(it.Slot), hero.Kind(it.Kind))
	}
	if it.Slot == string(gear.Weapon) {
		it.Kind = string(hero.Kind(it.Kind).OrMelee())
	} else {
		it.Kind = ""
	}
	if !gear.Slot(it.Slot).Valid() {
		it.Slot = string(gear.Weapon)
	}
	return it
}

// applyBag enregistre le sac reçu et affiche ses notifications dans le journal.
func (m *Model) applyBag(b protocol.Bag) {
	items := make([]protocol.ItemInfo, 0, min(len(b.Items), maxBagRows))
	for i, it := range b.Items {
		if i >= maxBagRows {
			break
		}
		items = append(items, cleanItem(it))
	}
	b.Items = items
	b.Class, b.Style = string(hero.Class(b.Class).OrDefault()), string(hero.Kind(b.Style).OrMelee())
	if len(b.Scores) != int(hero.NumAbilities) {
		b.Scores = nil
	} else {
		sc := make([]int, len(b.Scores))
		for i, v := range b.Scores {
			sc[i] = max(0, min(v, 99))
		}
		b.Scores = sc
	}
	m.bag = b
	m.bagSel = max(0, min(m.bagSel, len(items)-1))
	for _, n := range b.Notice {
		if n = textsafe.Clean(n, maxNPCText); n != "" {
			m.addChat(chatLine{Channel: protocol.ChanSystem, Text: n})
		}
	}
	m.bag.Notice = nil
}

// wornIn renvoie l'objet porté dans un emplacement.
func (m Model) wornIn(slot gear.Slot) (protocol.ItemInfo, bool) {
	for _, it := range m.bag.Items {
		if it.Worn && it.Slot == string(slot) {
			return it, true
		}
	}
	return protocol.ItemInfo{}, false
}

// bonusText résume les bonus d'un objet.
func (m Model) bonusText(it protocol.ItemInfo) string {
	if it.Slot == string(gear.Weapon) {
		return m.t("bag.item.atk", it.Dice, m.t("gear.family."+it.Family)) + " · " + m.t("hero.kind."+it.Kind)
	}
	if it.Reduction != "" {
		return m.t("bag.item.def", it.Def) + " · " + m.t("bag.item.red", it.Reduction)
	}
	return m.t("bag.item.def", it.Def)
}

// updateBagKey traite une touche dans l'écran du sac.
func (m Model) updateBagKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "i":
		m.inventory = false
	case "up", "k":
		m.bagSel = max(0, m.bagSel-1)
	case "down", "j":
		m.bagSel = min(max(0, len(m.bag.Items)-1), m.bagSel+1)
	case "e", "enter":
		if m.bagSel < len(m.bag.Items) && m.conn != nil {
			it := m.bag.Items[m.bagSel]
			if it.Worn {
				m.conn.Send(protocol.TGearAction, protocol.GearAction{Action: protocol.GearUnequip, Slot: it.Slot})
			} else {
				m.conn.Send(protocol.TGearAction, protocol.GearAction{Action: protocol.GearEquip, ID: it.ID})
			}
		}
	}
	return m, nil
}

// inventoryView dessine le sac : à gauche potions, or, emplacements portés et objets ; à droite le détail.
func (m Model) inventoryView(w, h int) string {
	st := m.snap.Stats
	detailW := min(44, w/2)
	listW := w - detailW
	iw := listW - 4

	left := []string{
		" " + dimStyle.Render(m.t("bag.gold_label")) + goldStyle.Render(m.t("bag.gold", st.Gold)) + "   " + m.t("bag.potions", st.Potions),
		"",
		" " + titleStyle.Render(m.t("bag.worn")),
	}
	for _, s := range gear.Slots {
		name := dimStyle.Render("—")
		if it, ok := m.wornIn(s); ok {
			name = it.Name
		}
		left = append(left, " "+ui.Fit(dimStyle.Render(m.t("gear.slot."+string(s))+":"), 11)+" "+name)
	}
	left = append(left, "", " "+titleStyle.Render(m.t("bag.items", len(m.bag.Items), m.bag.Capacity)))
	if len(m.bag.Items) == 0 {
		left = append(left, " "+dimStyle.Render(m.t("bag.empty")))
	}
	for i, it := range m.bag.Items {
		mark := "  "
		if it.Worn {
			mark = ui.OK.Render("✓ ")
		}
		line := mark + ui.Fit(it.Name, iw-2)
		if i == m.bagSel {
			line = ui.Select.Render(" " + ui.Fit(mark+it.Name, iw))
		} else {
			line = " " + line
		}
		left = append(left, line)
	}

	var detail []string
	if m.bagSel < len(m.bag.Items) {
		it := m.bag.Items[m.bagSel]
		dw := detailW - 4
		detail = append(detail, "", " "+titleStyle.Render(it.Name), " "+dimStyle.Render(m.t("gear.slot."+it.Slot)+" · "+m.t("bag.item.tier", it.Tier)), "")
		for _, l := range wrap(it.Desc, dw) {
			detail = append(detail, " "+l)
		}
		detail = append(detail, "", " "+goldStyle.Render(m.bonusText(it)), " "+m.t("bag.item.level", it.MinLevel))
		if it.Origin != "" {
			detail = append(detail, " "+dimStyle.Render(m.t("bag.item.origin", it.Origin)))
		}
		detail = append(detail, " "+dimStyle.Render(m.t("bag.item.sell", it.Sell)))
		if cur, ok := m.wornIn(gear.Slot(it.Slot)); ok && !it.Worn {
			diff := itemPower(it) - itemPower(cur)
			style := ui.OK
			if diff < 0 {
				style = errStyle
			}
			detail = append(detail, " "+style.Render(m.t("bag.item.vs", diff)))
		}
		if it.Worn {
			detail = append(detail, "", " "+ui.OK.Render(m.t("bag.item.worn")))
		}
		detail = append(detail, "", " "+dimStyle.Render(m.t("bag.item.hint")))
	} else {
		detail = []string{"", " " + titleStyle.Render(m.t("bag.potion_name")), " " + dimStyle.Render(m.t("bag.consumable")), "",
			m.t("bag.potion_line1"), m.t("bag.potion_line2"), "", " " + dimStyle.Render(m.t("bag.potion_line3")), " " + dimStyle.Render(m.t("bag.potion_line4"))}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.t("bag.title", m.snap.Self.Name), strings.Join(left, "\n"), listW, h),
		ui.Panel(m.t("bag.detail"), strings.Join(detail, "\n"), detailW, h),
	)
}

var _ = fmt.Sprintf

// scoresLine est la ligne compacte des caractéristiques du panneau latéral (vide tant que le sac n'est pas reçu).
func (m Model) scoresLine() string {
	if len(m.bag.Scores) != int(hero.NumAbilities) {
		return ""
	}
	var parts []string
	for a := hero.Ability(0); a < hero.NumAbilities; a++ {
		parts = append(parts, fmt.Sprintf("%s%d", m.t("hero.ability."+a.Key()), m.bag.Scores[a]))
	}
	return " " + strings.Join(parts[:3], " ") + "\n " + strings.Join(parts[3:], " ")
}

// cleanDice ne garde qu'une notation de dés valide reçue du serveur ("" sinon).
func cleanDice(s string) string {
	if d, ok := dice.Parse(s); ok && !d.IsZero() {
		return d.String()
	}
	return ""
}

// itemPower compare deux objets du même emplacement: dégâts moyens, réduction moyenne et DEF (1 point de DEF vaut 4).
func itemPower(it protocol.ItemInfo) int {
	p := 4 * it.Def
	if d, ok := dice.Parse(it.Dice); ok {
		p += d.Avg()
	}
	if d, ok := dice.Parse(it.Reduction); ok {
		p += d.Avg()
	}
	return p
}

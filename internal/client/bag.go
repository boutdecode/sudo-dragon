package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// Equipment bag: worn slots and items, in the bag screen (I key).

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

// applyBag records the bag received and shows its notifications in the log.
func (m *Model) applyBag(b protocol.Bag) {
	m.bagAt = time.Now()
	items := make([]protocol.ItemInfo, 0, min(len(b.Items), maxBagRows))
	for i, it := range b.Items {
		if i >= maxBagRows {
			break
		}
		items = append(items, cleanItem(it))
	}
	b.Items = items
	b.Class, b.Style = string(hero.Class(b.Class).OrDefault()), string(hero.Kind(b.Style).OrMelee())
	b.Race = string(hero.Race(b.Race).OrDefault())
	if len(b.Scores) != int(hero.NumAbilities) {
		b.Scores = nil
	} else {
		sc := make([]int, len(b.Scores))
		for i, v := range b.Scores {
			sc[i] = max(0, min(v, 99))
		}
		b.Scores = sc
	}
	jobs := make([]protocol.JobInfo, 0, min(len(b.Jobs), int(resource.NumCategories)))
	for _, j := range b.Jobs {
		if len(jobs) >= int(resource.NumCategories) {
			break
		}
		j.Job = textsafe.Clean(j.Job, 8)
		j.Level, j.XP, j.Next = max(1, min(j.Level, 99)), max(0, min(j.XP, 1<<20)), max(0, min(j.Next, 1<<20))
		j.Used, j.Cap, j.Chance = max(0, min(j.Used, 9999)), max(0, min(j.Cap, 9999)), max(0, min(j.Chance, 100))
		j.Tool, j.Yield = max(0, min(j.Tool, resource.NumToolTiers)), max(0, min(j.Yield, 99))
		jobs = append(jobs, j)
	}
	b.Jobs = jobs
	m.bag = b
	m.bagSel = max(0, min(m.bagSel, len(items)-1))
	for _, n := range b.Notice {
		if n = textsafe.Clean(n, maxNPCText); n != "" {
			m.addChat(chatLine{Channel: protocol.ChanSystem, Text: n})
		}
	}
	m.bag.Notice = nil
}

// wornIn returns the item worn in a slot.
func (m Model) wornIn(slot gear.Slot) (protocol.ItemInfo, bool) {
	for _, it := range m.bag.Items {
		if it.Worn && it.Slot == string(slot) {
			return it, true
		}
	}
	return protocol.ItemInfo{}, false
}

// bonusText summarizes an item's bonuses.
func (m Model) bonusText(it protocol.ItemInfo) string {
	if it.Slot == string(gear.Weapon) {
		return m.t("bag.item.atk", it.Dice, m.t("gear.family."+it.Family)) + " · " + m.t("hero.kind."+it.Kind)
	}
	if it.Reduction != "" {
		return m.t("bag.item.def", it.Def) + " · " + m.t("bag.item.red", it.Reduction)
	}
	return m.t("bag.item.def", it.Def)
}

// itemDetailLines is an item's sheet (bag and shops). In compact mode, no blank lines and no description
// beyond two lines, to fit in a small panel.
func (m Model) itemDetailLines(it protocol.ItemInfo, dw int, compact bool) []string {
	gap := func() []string {
		if compact {
			return nil
		}
		return []string{""}
	}
	out := []string{" " + titleStyle.Render(it.Name), " " + dimStyle.Render(m.t("gear.slot."+it.Slot)+" · "+m.t("bag.item.tier", it.Tier))}
	out = append(out, gap()...)
	desc := wrap(it.Desc, dw)
	if compact && len(desc) > 2 {
		desc = desc[:2]
	}
	for _, l := range desc {
		out = append(out, " "+l)
	}
	out = append(out, gap()...)
	out = append(out, " "+goldStyle.Render(m.bonusText(it)), " "+m.t("bag.item.level", it.MinLevel))
	if it.Origin != "" {
		out = append(out, " "+dimStyle.Render(m.t("bag.item.origin", it.Origin)))
	}
	out = append(out, " "+dimStyle.Render(m.t("bag.item.sell", it.Sell)))
	if cur, ok := m.wornIn(gear.Slot(it.Slot)); ok && !it.Worn {
		diff := itemPower(it) - itemPower(cur)
		style := ui.OK
		if diff < 0 {
			style = errStyle
		}
		out = append(out, " "+style.Render(m.t("bag.item.vs", diff)))
	}
	return out
}

// updateBagKey handles a key in the bag screen.
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

// inventoryView draws the bag: on the left potions, gold, worn slots and items; on the right the detail.
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
	left = append(left, m.resourceLines()...)

	var detail []string
	if m.bagSel < len(m.bag.Items) {
		it := m.bag.Items[m.bagSel]
		detail = append(detail, "")
		detail = append(detail, m.itemDetailLines(it, detailW-4, false)...)
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

// maxResourceRows bounds the materials displayed (the server does not send more than the catalogue).
const maxResourceRows = 64

// jobOf returns the job of a category received from the server (the zero value if missing).
func (m Model) jobOf(c resource.Category) protocol.JobInfo {
	for _, j := range m.bag.Jobs {
		if j.Job == c.Key() {
			return j
		}
	}
	return protocol.JobInfo{Job: c.Key(), Level: 1}
}

// resourceLines lists the harvested materials, grouped by category, each under its job's header
// (level and bag fill). The four categories are always present, even when empty.
func (m Model) resourceLines() []string {
	if len(m.bag.Jobs) == 0 && len(m.bag.Resources) == 0 {
		return nil
	}
	lines := []string{"", " " + titleStyle.Render(m.t("bag.materials"))}
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		j := m.jobOf(c)
		lines = append(lines, " "+dimStyle.Render(m.t("bag.job_line", m.t("job."+c.Key()), j.Level, j.Used, j.Cap)))
		if resource.NeedsTool(c) {
			tool := m.t("tool.none")
			if j.Tool > 0 {
				tool = fmt.Sprintf("%s ×%d", m.t("tool."+c.Key()+"."+strconv.Itoa(j.Tool)), j.Yield)
			}
			lines = append(lines, "   "+dimStyle.Render(m.t("bag.tool", tool)))
		}
		for i, r := range m.bag.Resources {
			if i >= maxResourceRows {
				break
			}
			info, ok := resource.Lookup(resource.Kind(r.ID))
			if !ok || info.Cat != c || r.Qty <= 0 {
				continue
			}
			lines = append(lines, "   "+m.t("resource."+r.ID)+dimStyle.Render(" ×"+strconv.Itoa(min(r.Qty, 9999))))
		}
	}
	return lines
}

// scoresLine is the compact line of ability scores in the side panel (empty until the bag is received).
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

// cleanDice keeps only a valid dice notation received from the server ("" otherwise).
func cleanDice(s string) string {
	if d, ok := dice.Parse(s); ok && !d.IsZero() {
		return d.String()
	}
	return ""
}

// itemPower compares two items of the same slot: average damage, average reduction and DEF (1 point of DEF is worth 4).
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

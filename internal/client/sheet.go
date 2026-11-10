package client

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/ui"
)

// Fiche de personnage (touche P) : tout ce que le serveur établit sur le joueur. Les valeurs viennent
// de l'instantané (PV, mana, Atq/Déf effectives), du sac (classe, caractéristiques, palmarès) et du
// livre de quêtes (réputation).

func (m Model) updateSheetKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "p":
		m.sheet = false
	}
	return m, nil
}

// sheetView dessine la fiche dans exactement w x h cellules.
func (m Model) sheetView(w, h int) string {
	rightW := w / 2
	leftW := w - rightW
	return lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.t("sheet.title", m.snap.Self.Name), strings.Join(m.sheetLeft(leftW-4), "\n"), leftW, h),
		ui.Panel(m.t("sheet.abilities"), strings.Join(m.sheetRight(rightW-4), "\n"), rightW, h),
	)
}

func (m Model) sheetLeft(w int) []string {
	st, self := m.snap.Stats, m.snap.Self
	cl := hero.Class(m.bag.Class).OrDefault()
	row := func(label, value string) string {
		return " " + ui.Fit(dimStyle.Render(label), 13) + " " + value
	}
	bar := func(v, max int) string { return ui.Bar(v, max, 12) + fmt.Sprintf(" %d/%d", v, max) }

	lines := []string{"", " " + titleStyle.Render(m.t("hero.class."+string(cl))) + " " + dimStyle.Render(m.t("sheet.die", cl.HitDie())),
		row(m.t("sheet.level"), fmt.Sprintf("%d", st.Level)),
		row(m.t("sheet.xp"), ui.FlatBar(st.XP, st.XPNext, 12, ui.ColOK)+fmt.Sprintf(" %d/%d", st.XP, st.XPNext)),
		row(m.t("sheet.hp"), bar(st.HP, st.MaxHP)),
	}
	if st.MaxMana > 0 {
		lines = append(lines, row(m.t("sheet.mana"), bar(st.Mana, st.MaxMana)))
	}

	style := hero.Kind(m.bag.Style).OrMelee()
	var atkMod int
	var chaMod, agiMod = 0, 0
	if len(m.bag.Scores) == int(hero.NumAbilities) {
		agiMod = hero.Mod(m.bag.Scores[hero.AGI])
		atkMod = hero.Mod(m.bag.Scores[style.Stat()])
		chaMod = hero.Mod(m.bag.Scores[hero.CHA])
	}
	lines = append(lines, "",
		row(m.t("sheet.defense"), fmt.Sprintf("%d  %s", st.Def, dimStyle.Render(m.t("sheet.def_detail", st.Def-m.bag.DefBonus-agiMod-m.blessingDef(), agiMod, m.bag.DefBonus)))),
		row(m.t("sheet.style"), fmt.Sprintf("%s (%s %+d)", m.t("hero.kind."+string(style)), m.t("hero.ability."+style.Stat().Key()), atkMod)),
		row(m.t("sheet.accuracy"), m.t("sheet.hit_detail", st.Hit, int(hero.HitChance(st.Hit, hero.DefBase(hero.Medium))*100+0.5))),
		row(m.t("sheet.attack"), textsafe.Clean(st.Dmg, 16)),
		row(m.t("sheet.reduction"), reductionText(m.bag.Reduction)),
		row(m.t("sheet.prices"), fmt.Sprintf("%+d %%  %s", hero.PriceAdjust(chaMod), dimStyle.Render("("+m.t("hero.impression."+hero.Impression(chaMod))+")"))),
		"",
		row(m.t("sheet.gold"), goldStyle.Render(fmt.Sprintf("%d", st.Gold))),
		row(m.t("sheet.potions"), fmt.Sprintf("%d", st.Potions)),
		row(m.t("sheet.position"), fmt.Sprintf("%d, %d", self.X, self.Y)),
		"",
		" "+titleStyle.Render(m.t("sheet.record")),
		row(m.t("sheet.kills"), fmt.Sprintf("%d", m.bag.Kills)),
		row(m.t("sheet.bosses"), fmt.Sprintf("%d", m.bag.BossKills)),
		row(m.t("sheet.deaths"), fmt.Sprintf("%d", m.bag.Deaths)),
	)
	for i, l := range lines {
		lines[i] = ui.Fit(l, w+2)
	}
	return lines
}

func (m Model) sheetRight(w int) []string {
	lines := []string{""}
	if len(m.bag.Scores) == int(hero.NumAbilities) {
		for a := hero.Ability(0); a < hero.NumAbilities; a++ {
			k, v := a.Key(), m.bag.Scores[a]
			lines = append(lines, " "+ui.Fit(fmt.Sprintf("%s %-12s %2d (%+d)  %s", m.t("hero.ability."+k), m.t("hero.ability."+k+".long"), v, hero.Mod(v),
				dimStyle.Render(m.t("hero.ability."+k+".desc"))), w))
		}
	}
	lines = append(lines, "", " "+titleStyle.Render(m.t("bag.worn")))
	for _, s := range gear.Slots {
		name := dimStyle.Render("—")
		if it, ok := m.wornIn(s); ok {
			name = it.Name
		}
		lines = append(lines, " "+ui.Fit(dimStyle.Render(m.t("gear.slot."+string(s))+":"), 11)+" "+ui.Fit(name, w-12))
	}
	if bl := m.blessingLines(w); len(bl) > 0 {
		lines = append(lines, "", " "+titleStyle.Render(m.t("sheet.blessings")))
		lines = append(lines, bl...)
	}
	if len(m.bag.Maps) > 0 {
		lines = append(lines, "", " "+titleStyle.Render(m.t("sheet.maps")))
		self := m.snap.Self
		for _, mp := range m.bag.Maps {
			lines = append(lines, " "+ui.Fit(m.t("sheet.map_line", mp.X, mp.Y, max(abs(mp.X-self.X), abs(mp.Y-self.Y))), w))
		}
	}
	open, ready := m.readyQuests()
	lines = append(lines, "", " "+titleStyle.Render(m.t("sheet.reputation")))
	if len(m.quests.Standing) == 0 {
		lines = append(lines, " "+dimStyle.Render(m.t("quest.book.rep_none")))
	}
	for _, s := range m.quests.Standing {
		lines = append(lines, " "+ui.Fit(fmt.Sprintf("%s · %s (%d)", s.Place, s.Tier, s.Points), w))
	}
	lines = append(lines, "", " "+goldStyle.Render(m.t("quest.hud", open, ready)))
	return lines
}

// blessingLines liste les bénédictions de sanctuaire encore actives, avec leur temps restant.
func (m Model) blessingLines(w int) []string {
	var out []string
	elapsed := int(time.Since(m.bagAt).Seconds())
	for _, b := range m.bag.Blessings {
		left := b.Secs - elapsed
		if left <= 0 {
			continue
		}
		out = append(out, " "+ui.Fit(m.t("blessing."+b.Kind, b.Bonus)+" "+dimStyle.Render(fmt.Sprintf("(%d:%02d)", left/60, left%60)), w))
	}
	return out
}

// blessingDef est le bonus de DEF d'une bénédiction de sanctuaire : il fait partie de la DEF affichée, mais ni de la
// base, ni de l'équipement.
func (m Model) blessingDef() int {
	n := 0
	elapsed := int(time.Since(m.bagAt).Seconds())
	for _, b := range m.bag.Blessings {
		if b.Kind == protocol.BlessDef && b.Secs > elapsed {
			n += b.Bonus
		}
	}
	return n
}

// reductionText présente les dés de réduction de l'armure ("—" sans armure).
func reductionText(r string) string {
	if r = cleanDice(r); r == "" {
		return dimStyle.Render("—")
	}
	return "−" + r
}

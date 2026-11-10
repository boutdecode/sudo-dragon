package render

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// NightLevels est le nombre de niveaux d'obscurité: 0 = plein jour,
// NightLevels-1 = pleine nuit. Les styles assombris sont pré-calculés par
// niveau, ce qui garde le regroupement des cellules en séquences de même style.
const NightLevels = 6

// nightMax est la part de la couleur de nuit mélangée à pleine nuit: assez
// sombre pour qu'on sente la nuit, assez clair pour rester jouable.
const nightMax = 0.62

// nightTint est la couleur vers laquelle le terrain s'assombrit (un bleu nuit).
var nightTint = [3]float64{0x0b, 0x10, 0x26}

// GameHour donne l'heure de jeu fractionnaire (0 <= h < 24) pour un tick.
func GameHour(tick uint64) float64 {
	const day = 24 * protocol.TicksPerGameHour
	t := (tick + protocol.StartGameHour*protocol.TicksPerGameHour) % day
	return float64(t) / protocol.TicksPerGameHour
}

// Daylight donne la luminosité du jour: 1 en plein jour, 0 en pleine nuit.
// Nuit de 21 h à 5 h, aube de 5 h à 8 h, jour de 8 h à 19 h, crépuscule de
// 19 h à 21 h.
func Daylight(tick uint64) float64 {
	h := GameHour(tick)
	switch {
	case h < 5 || h >= 21:
		return 0
	case h < 8:
		return (h - 5) / 3
	case h < 19:
		return 1
	default:
		return (21 - h) / 2
	}
}

// NightLevel quantifie Daylight: 0 = plein jour ... NightLevels-1 = pleine nuit.
func NightLevel(tick uint64) int {
	return int((1-Daylight(tick))*float64(NightLevels-1) + 0.5)
}

// nightStyles[n] est la copie assombrie au niveau n de la table styles.
var nightStyles [NightLevels][]lipgloss.Style

func buildNightStyles() {
	for n := 1; n < NightLevels; n++ {
		k := nightMax * float64(n) / float64(NightLevels-1)
		out := make([]lipgloss.Style, len(styles))
		for i, s := range styles {
			out[i] = s.Foreground(lipgloss.Color(dimHex(s.GetForeground(), k)))
		}
		nightStyles[n] = out
	}
}

// dimHex mélange c avec la couleur de nuit à hauteur de k (0..1).
func dimHex(c color.Color, k float64) string {
	if c == nil {
		return "#808080"
	}
	r, g, b, _ := c.RGBA()
	mix := func(v uint32, t float64) int { return int(float64(v>>8)*(1-k) + t*k + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", mix(r, nightTint[0]), mix(g, nightTint[1]), mix(b, nightTint[2]))
}

// lit dit si une tuile reste à pleine luminosité la nuit (elle éclaire).
func lit(t world.Tile) bool { return t == world.Campfire }

// styleAt renvoie le style d'un index éventuellement codé avec un niveau de
// nuit (index + n*len(styles)).
func styleAt(st int) lipgloss.Style {
	if n := st / len(styles); n > 0 {
		return nightStyles[n][st%len(styles)]
	}
	return styles[st]
}

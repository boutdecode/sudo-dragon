package render

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// NightLevels is the number of darkness levels: 0 = full day,
// NightLevels-1 = full night. The darkened styles are precomputed per
// level, which keeps the grouping of cells into runs of the same style.
const NightLevels = 6

// nightMax is the share of the night colour mixed in at full night: dark
// enough to feel the night, light enough to stay playable.
const nightMax = 0.62

// nightTint is the colour the terrain darkens towards (a midnight blue).
var nightTint = [3]float64{0x0b, 0x10, 0x26}

// GameHour gives the fractional game hour (0 <= h < 24) for a tick.
func GameHour(tick uint64) float64 {
	const day = 24 * protocol.TicksPerGameHour
	t := (tick + protocol.StartGameHour*protocol.TicksPerGameHour) % day
	return float64(t) / protocol.TicksPerGameHour
}

// Daylight gives the brightness of the day: 1 in full day, 0 in full night.
// Night from 9 pm to 5 am, dawn from 5 to 8 am, day from 8 am to 7 pm, dusk from
// 7 to 9 pm.
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

// NightLevel quantises Daylight: 0 = full day ... NightLevels-1 = full night.
func NightLevel(tick uint64) int {
	return int((1-Daylight(tick))*float64(NightLevels-1) + 0.5)
}

// nightStyles[n] is the copy of the styles table darkened to level n.
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

// dimHex mixes c with the night colour up to k (0..1).
func dimHex(c color.Color, k float64) string {
	if c == nil {
		return "#808080"
	}
	r, g, b, _ := c.RGBA()
	mix := func(v uint32, t float64) int { return int(float64(v>>8)*(1-k) + t*k + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", mix(r, nightTint[0]), mix(g, nightTint[1]), mix(b, nightTint[2]))
}

// lit reports whether a tile stays at full brightness at night (it gives light).
func lit(t world.Tile) bool { return t == world.Campfire }

// styleAt returns the style of an index possibly encoded with a night level
// (index + n*len(styles)).
func styleAt(st int) lipgloss.Style {
	if n := st / len(styles); n > 0 {
		return nightStyles[n][st%len(styles)]
	}
	return styles[st]
}

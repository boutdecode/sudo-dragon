package ui

import "strings"

var (
	barFull = fg("#7fcf8a")
	barMid  = fg("#e8c25a")
	barLow  = fg("#ef6b5f")
	barBack = fg(ColFaint)
)

const (
	barFill  = "█"
	barEmpty = "░"
)

// Bar draws a gauge width columns wide for cur/max. The colour shifts from
// green to red. At least one cell is filled while cur > 0, and it is never
// completely full while points are missing.
func Bar(cur, maxV, width int) string {
	if width < 1 {
		return ""
	}
	if maxV < 1 {
		maxV = 1
	}
	cur = max(0, min(cur, maxV))
	filled := cur * width / maxV
	if cur > 0 && filled == 0 {
		filled = 1
	}
	style := barFull
	switch {
	case cur*4 <= maxV: // 25% or less
		style = barLow
	case cur*2 <= maxV: // 50% or less
		style = barMid
	}
	return style.Render(strings.Repeat(barFill, filled)) + barBack.Render(strings.Repeat(barEmpty, width-filled))
}

// FlatBar is a fixed-colour gauge (XP, load...).
func FlatBar(cur, maxV, width int, color string) string {
	if width < 1 {
		return ""
	}
	if maxV < 1 {
		maxV = 1
	}
	cur = max(0, min(cur, maxV))
	filled := cur * width / maxV
	return fg(color).Render(strings.Repeat(barFill, filled)) + barBack.Render(strings.Repeat(barEmpty, width-filled))
}

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

// Bar dessine une jauge de largeur width (colonnes) pour cur/max. La couleur
// passe du vert au rouge. Au moins une case est pleine tant que cur > 0 et
// elle n'est jamais complète tant qu'il manque des points.
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
	case cur*4 <= maxV: // 25 % ou moins
		style = barLow
	case cur*2 <= maxV: // 50 % ou moins
		style = barMid
	}
	return style.Render(strings.Repeat(barFill, filled)) + barBack.Render(strings.Repeat(barEmpty, width-filled))
}

// FlatBar est une jauge d'une couleur fixe (XP, charge…).
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

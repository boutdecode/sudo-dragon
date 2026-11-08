package ui

import "github.com/charmbracelet/x/ansi"

// logoLines est le mot DRAGON en grandes lettres (5 lignes de même largeur).
var logoLines = []string{
	"████   ████    ███    ████   ███   █   █",
	"█   █  █   █  █   █  █      █   █  ██  █",
	"█   █  ████   █████  █  ██  █   █  █ █ █",
	"█   █  █  █   █   █  █   █  █   █  █  ██",
	"████   █   █  █   █   ████   ███   █   █",
}

// Dégradé vertical ambre → rouge, une couleur par ligne.
var logoColors = []string{"#f6c76a", "#f0b04a", "#f09a4a", "#ee8650", "#ef6b5f"}

// LogoWidth est la largeur du logo en colonnes.
func LogoWidth() int { return ansi.StringWidth(logoLines[0]) }

// Logo renvoie le logo coloré, une chaîne par ligne.
func Logo() []string {
	out := make([]string, len(logoLines))
	for i, l := range logoLines {
		out[i] = fg(logoColors[i]).Bold(true).Render(l)
	}
	return out
}

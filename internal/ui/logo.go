package ui

import "github.com/charmbracelet/x/ansi"

// logoLines is the word DRAGON in large letters (5 lines of equal width).
var logoLines = []string{
	"████   ████    ███    ████   ███   █   █",
	"█   █  █   █  █   █  █      █   █  ██  █",
	"█   █  ████   █████  █  ██  █   █  █ █ █",
	"█   █  █  █   █   █  █   █  █   █  █  ██",
	"████   █   █  █   █   ████   ███   █   █",
}

// Vertical gradient from amber to red, one colour per line.
var logoColors = []string{"#f6c76a", "#f0b04a", "#f09a4a", "#ee8650", "#ef6b5f"}

// LogoWidth is the logo width in columns.
func LogoWidth() int { return ansi.StringWidth(logoLines[0]) }

// Logo returns the coloured logo, one string per line.
func Logo() []string {
	out := make([]string, len(logoLines))
	for i, l := range logoLines {
		out[i] = fg(logoColors[i]).Bold(true).Render(l)
	}
	return out
}

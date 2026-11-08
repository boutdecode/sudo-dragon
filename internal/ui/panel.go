package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Panel dessine body dans un cadre arrondi de w x h cellules (bordures
// comprises) avec le titre incrusté dans le bord haut: ╭─ Titre ───╮.
// Les lignes du corps sont tronquées ou complétées; les lignes en trop sont
// coupées. w < 4 ou h < 2 donnent une chaîne vide.
func Panel(title, body string, w, h int) string {
	if w < 4 || h < 2 {
		return ""
	}
	inner := w - 2
	head := Frame.Render("─")
	if title != "" {
		head += Frame.Render(" ") + Title.Render(title) + " "
	}
	head = ansi.Truncate(head, inner, "")
	fill := max(0, inner-ansi.StringWidth(head))

	var b strings.Builder
	b.WriteString(Frame.Render("╭") + head + Frame.Render(strings.Repeat("─", fill)+"╮"))

	lines := strings.Split(body, "\n")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString("\n" + Frame.Render("│") + Fit(line, inner) + Frame.Render("│"))
	}
	b.WriteString("\n" + Frame.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return b.String()
}

// Fit tronque ou complète s pour qu'il occupe exactement w colonnes.
func Fit(s string, w int) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if n := w - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// Center centre s dans une largeur de w colonnes.
func Center(s string, w int) string {
	left := max(0, (w-ansi.StringWidth(s))/2)
	return Fit(strings.Repeat(" ", left)+s, w)
}

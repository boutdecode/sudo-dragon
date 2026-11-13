package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Panel draws body inside a rounded frame of w x h cells (borders
// included) with the title inlaid in the top edge: ╭─ Title ───╮.
// Body lines are truncated or padded; extra lines are
// cut off. w < 4 or h < 2 yield an empty string.
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

// Fit truncates or pads s so that it occupies exactly w columns.
func Fit(s string, w int) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if n := w - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// Center centers s within a width of w columns.
func Center(s string, w int) string {
	left := max(0, (w-ansi.StringWidth(s))/2)
	return Fit(strings.Repeat(" ", left)+s, w)
}

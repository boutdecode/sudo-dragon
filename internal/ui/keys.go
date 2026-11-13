package ui

import "strings"

// KeyBar formats a shortcut bar: each pair is (key, action).
func KeyBar(pairs ...[2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = Key.Render(p[0]) + Dim.Render(" "+p[1])
	}
	return strings.Join(parts, "  ")
}

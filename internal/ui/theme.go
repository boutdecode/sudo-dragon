// Package ui regroupe le thème « sudo Dragon » et les petits composants
// d'interface (panneaux, jauges, barre de raccourcis) partagés par le client.
package ui

import "charm.land/lipgloss/v2"

// Palette de la maquette: fond sombre violacé, accent ambre.
const (
	ColAccent = "#f0b04a"
	ColText   = "#d9d3c4"
	ColMuted  = "#8f89a0"
	ColFrame  = "#6f6885"
	ColFaint  = "#4f4963"
	ColHP     = "#ef6b5f"
	ColOK     = "#7fcf8a"
	ColGold   = "#e8c25a"
	ColInk    = "#15131b"
	ColKeyBg  = "#2a2636"
	ColErr    = "#ff7a6e"
)

func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

// Styles de l'interface.
var (
	Title  = fg(ColAccent).Bold(true)
	Dim    = fg(ColMuted)
	Faint  = fg(ColFaint)
	Frame  = fg(ColFrame)
	Err    = fg(ColErr)
	Name   = fg("#6fd6d0").Bold(true)
	Gold   = fg(ColGold)
	OK     = fg(ColOK)
	HP     = fg(ColHP)
	Global = fg("#ff9f43").Bold(true)
	Emote  = fg("#b497f0").Italic(true)
	Whisp  = fg("#f472b6").Italic(true)
	// Select met une ligne en surbrillance ambre (entrée de menu choisie).
	Select = lipgloss.NewStyle().Background(lipgloss.Color(ColAccent)).Foreground(lipgloss.Color(ColInk)).Bold(true)
	// Key dessine une touche dans la barre de raccourcis.
	Key = lipgloss.NewStyle().Background(lipgloss.Color(ColKeyBg)).Foreground(lipgloss.Color(ColAccent)).Bold(true).Padding(0, 1)
)

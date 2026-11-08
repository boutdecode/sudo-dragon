// Commande client : lance le client TUI.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/client"
)

func main() {
	addr := flag.String("addr", "ws://localhost:8080/ws", "adresse du serveur")
	name := flag.String("name", "", "pseudo (pré-rempli)")
	transparent := flag.Bool("transparent", false, "garde le fond du terminal au lieu de peindre le fond sombre du thème")
	flag.Parse()

	m := client.New(*addr, *name).WithTransparent(*transparent)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
}

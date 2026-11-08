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
	flag.Parse()

	if _, err := tea.NewProgram(client.New(*addr, *name)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
}

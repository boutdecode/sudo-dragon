// Commande client : lance le client TUI.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/client"
	"github.com/kevin/ascii-adventure/internal/i18n"
)

func main() {
	// La langue du système (LC_ALL, LC_MESSAGES, LANGUAGE, LANG) sert aussi à rédiger
	// l'aide des options; -lang la remplace pour la partie.
	sys := i18n.Detect()
	addr := flag.String("addr", "ws://localhost:8080/ws", i18n.T(sys, "cli.addr"))
	name := flag.String("name", "", i18n.T(sys, "cli.name"))
	transparent := flag.Bool("transparent", false, i18n.T(sys, "cli.transparent"))
	langFlag := flag.String("lang", "", i18n.T(sys, "cli.lang"))
	flag.Parse()

	lang := sys
	if *langFlag != "" {
		l, ok := i18n.Parse(*langFlag)
		if !ok {
			fmt.Fprintln(os.Stderr, i18n.T(sys, "cli.bad_lang", *langFlag))
			os.Exit(2)
		}
		lang = l
	}

	m := client.NewLang(*addr, *name, lang).WithTransparent(*transparent)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T(lang, "cli.error"), err)
		os.Exit(1)
	}
}

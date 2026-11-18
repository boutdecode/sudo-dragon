// Command client: runs the TUI client.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/client"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/version"
)

func main() {
	// The system language (LC_ALL, LC_MESSAGES, LANGUAGE, LANG) is also used to write
	// the option help; -lang overrides it for the game.
	sys := i18n.Detect()
	addr := flag.String("addr", "ws://localhost:8080/ws", i18n.T(sys, "cli.addr"))
	name := flag.String("name", "", i18n.T(sys, "cli.name"))
	transparent := flag.Bool("transparent", false, i18n.T(sys, "cli.transparent"))
	langFlag := flag.String("lang", "", i18n.T(sys, "cli.lang"))
	showVersion := flag.Bool("version", false, i18n.T(sys, "cli.version"))
	flag.Parse()

	if *showVersion {
		fmt.Println("sudo-dragon", version.String())
		return
	}

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

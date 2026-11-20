package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/kevin/ascii-adventure/internal/worldlore"
)

// setupWorldLore loads the canon files (world-lore.<lang>.md) from dir. Absent
// files are fine; an unusable file refuses startup, so a typo is noticed at once.
func setupWorldLore(dir string, log *slog.Logger) (*worldlore.Canon, error) {
	c, err := worldlore.Load(dir, log)
	if err != nil {
		return nil, err
	}
	if langs := c.Langs(); len(langs) > 0 {
		log.Info("world lore loaded", "dir", dir, "langs", langs)
	} else {
		log.Info("no world lore found (optional)", "dir", dir, "expected", "world-lore.<lang>.md")
	}
	return c, nil
}

// reloader is what a reload signal triggers.
type reloader interface{ Reload() error }

// reloadOnSignal reloads the canon each time sig delivers (SIGHUP in production), until
// ctx ends. A failed reload keeps the previous version (see worldlore.Canon.Reload) and
// the server runs on. Blocking: to be run in a goroutine. The signal is injected so that
// tests do not have to send a real one.
func reloadOnSignal(ctx context.Context, sig <-chan os.Signal, r reloader, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			if err := r.Reload(); err != nil {
				log.Warn("world lore reload failed, previous version kept", "err", err)
				continue
			}
			log.Info("world lore reloaded")
		}
	}
}

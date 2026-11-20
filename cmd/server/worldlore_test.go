package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakeReloader struct {
	calls chan struct{}
	fail  atomic.Bool
}

func (f *fakeReloader) Reload() error {
	f.calls <- struct{}{}
	if f.fail.Load() {
		return errors.New("invalid file")
	}
	return nil
}

func TestReloadOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal)
	fr := &fakeReloader{calls: make(chan struct{}, 4)}
	done := make(chan struct{})
	go func() { reloadOnSignal(ctx, sig, fr, slog.New(slog.NewTextHandler(io.Discard, nil))); close(done) }()

	sig <- syscall.SIGHUP
	select {
	case <-fr.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("the signal did not trigger a reload")
	}
	// A failing reload must not stop the loop.
	fr.fail.Store(true)
	sig <- syscall.SIGHUP
	sig <- syscall.SIGHUP
	for i := 0; i < 2; i++ {
		select {
		case <-fr.calls:
		case <-time.After(2 * time.Second):
			t.Fatal("the loop stopped after a failed reload")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not stop with the context")
	}
}

func TestSetupWorldLore(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	if c, err := setupWorldLore(dir, log); err != nil || len(c.Langs()) != 0 {
		t.Fatalf("no file must not be an error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world-lore.en.md"), []byte("Dragons sleep."), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := setupWorldLore(dir, log); err != nil || len(c.Langs()) != 1 {
		t.Fatalf("one file expected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world-lore.fr.md"), []byte("<!-- jamais fermé"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := setupWorldLore(dir, log); err == nil {
		t.Fatal("an unusable file must refuse startup")
	}
}

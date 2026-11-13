// Package storetest provides a temporary database for the tests of other packages.
package storetest

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kevin/ascii-adventure/internal/store"
)

// Password is a valid password for test accounts.
const Password = "motdepasse-test"

// NewBackend creates a temporary SQLite database (bcrypt at minimum cost) and its
// writer, closed at the end of the test. flushEvery sets the write interval
// (a long value allows testing the reading of pending saves).
func NewBackend(t testing.TB, flushEvery time.Duration) store.Backend {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), store.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	p := store.NewPersister(st, flushEvery, nil)
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("closing the writer: %v", err)
		}
		st.Close()
	})
	return store.Backend{Store: st, Persister: p}
}

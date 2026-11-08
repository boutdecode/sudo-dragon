// Package storetest fournit une base temporaire pour les tests des autres paquets.
package storetest

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kevin/ascii-adventure/internal/store"
)

// Password est un mot de passe valide pour les comptes de test.
const Password = "motdepasse-test"

// NewBackend crée une base SQLite temporaire (bcrypt au coût minimal) et son
// écrivain, fermés à la fin du test. flushEvery règle l'intervalle d'écriture
// (une valeur longue permet de tester la lecture des sauvegardes en attente).
func NewBackend(t testing.TB, flushEvery time.Duration) store.Backend {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), store.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	p := store.NewPersister(st, flushEvery, nil)
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("fermeture de l'écrivain: %v", err)
		}
		st.Close()
	})
	return store.Backend{Store: st, Persister: p}
}

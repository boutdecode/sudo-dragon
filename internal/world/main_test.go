package world

import (
	"fmt"
	"os"
	"testing"
)

// testGen est la version du générateur de terrain avec laquelle newTestWorld construit les mondes de test.
var testGen = GenV1

// newTestWorld crée un monde de test avec la version de générateur en cours d'essai (voir TestMain).
func newTestWorld(seed int64) *World { return NewVersion(seed, testGen) }

// TestMain joue toute la suite une fois par version du générateur: les règles du monde (structures, routes,
// points d'intérêt, donjons, spawn…) doivent tenir quel que soit le terrain. Les tests qui visent une
// version précise construisent leur monde avec NewVersion.
func TestMain(m *testing.M) {
	code := 0
	for gen := GenV1; gen <= GenV2; gen++ {
		testGen = gen
		fmt.Printf("=== générateur de terrain v%d ===\n", gen)
		if c := m.Run(); c != 0 {
			code = c
		}
	}
	os.Exit(code)
}

package world

import (
	"fmt"
	"os"
	"testing"
)

// testGen is the terrain generator version with which newTestWorld builds the test worlds.
var testGen = GenV1

// newTestWorld creates a test world with the generator version under test (see TestMain).
func newTestWorld(seed int64) *World { return NewVersion(seed, testGen) }

// TestMain runs the whole suite once per generator version: the world rules (structures, roads,
// points of interest, dungeons, spawn...) must hold whatever the terrain. Tests that target a
// specific version build their world with NewVersion.
func TestMain(m *testing.M) {
	code := 0
	for gen := GenV1; gen <= GenV3; gen++ {
		testGen = gen
		fmt.Printf("=== terrain generator v%d ===\n", gen)
		if c := m.Run(); c != 0 {
			code = c
		}
	}
	os.Exit(code)
}

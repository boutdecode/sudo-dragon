package world

import (
	"fmt"
	"os"
	"testing"
)

// testGen is the terrain generator version with which newTestWorld builds the test worlds.
var testGen = GenLatest

// newTestWorld creates a test world with the generator version under test (see TestMain).
func newTestWorld(seed int64) *World { return NewVersion(seed, testGen) }

// TestMain runs the suite with the latest terrain generator only (GenLatest): running every version
// tripled the duration. Tests that target a specific version build their world with NewVersion.
func TestMain(m *testing.M) {
	testGen = GenLatest
	fmt.Printf("=== terrain generator v%d ===\n", testGen)
	os.Exit(m.Run())
}

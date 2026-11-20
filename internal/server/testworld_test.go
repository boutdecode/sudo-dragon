package server

import (
	"sync"

	"github.com/kevin/ascii-adventure/internal/world"
)

var (
	sharedWorldsMu sync.Mutex
	sharedWorlds   = map[int64]*world.World{}
)

// testWorld returns a world shared between the tests of this package, for a given seed.
// World generation is deterministic and cached inside the world, so sharing it avoids
// recomputing the same chunks, structures and roads in every test. Tests that need a
// pristine world must call world.New directly.
func testWorld(seed int64) *world.World {
	sharedWorldsMu.Lock()
	defer sharedWorldsMu.Unlock()
	w, ok := sharedWorlds[seed]
	if !ok {
		w = world.New(seed)
		sharedWorlds[seed] = w
	}
	return w
}

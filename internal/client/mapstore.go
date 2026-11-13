package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Local map persistence: markers and explored areas are written to a JSON file in the
// user's configuration directory, one entry per world seed. The server knows nothing about it.

const (
	exploreSaveEvery = 15 * time.Second // deferred exploration write
	maxMarkerName    = 24
)

// marker is a marker placed on the map.
type marker struct {
	Pos  world.Pos
	Name string
}

type storedMarker struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Name string `json:"name"`
}

type storedWorld struct {
	Markers  []storedMarker `json:"markers"`
	Explored [][2]int       `json:"explored"`
}

// mapStorePath returns the save file ("" = no saving). A variable so tests can override it.
var mapStorePath = defaultMapStorePath

func defaultMapStorePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "sudo-dragon", "map.json")
}

var (
	mapStoreMu   sync.Mutex
	mapStoreSeq  atomic.Uint64 // numbers the write requests
	mapStoreDone uint64        // last request written (under mapStoreMu)
)

// seedKey is a world's key in the file: the seed, followed by the terrain generator version beyond
// the first (the same seed does not give the same terrain from one version to another; keys from before
// versioning remain valid).
func seedKey(seed int64, gen int) string {
	k := strconv.FormatUint(uint64(seed), 16)
	if gen > 1 {
		k += "-g" + strconv.Itoa(gen)
	}
	return k
}

// readStore reads the whole file; missing or corrupt = empty.
func readStore(path string) map[string]storedWorld {
	all := map[string]storedWorld{}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &all) != nil || all == nil {
			all = map[string]storedWorld{}
		}
	}
	return all
}

// loadMapState loads the markers and exploration saved for this seed.
func loadMapState(seed int64, gen int) mapState {
	st := mapState{explored: map[[2]int]struct{}{}}
	path := mapStorePath()
	if path == "" {
		return st
	}
	mapStoreMu.Lock()
	sw := readStore(path)[seedKey(seed, gen)]
	mapStoreMu.Unlock()
	for _, b := range sw.Explored {
		st.explored[b] = struct{}{}
	}
	for _, sm := range sw.Markers {
		if len(st.markers) < maxMarkers && sm.Name != "" {
			st.markers = append(st.markers, marker{world.Pos{X: sm.X, Y: sm.Y}, sm.Name})
		}
	}
	return st
}

// snapshotStore copies the state to write (sorted for a stable file).
func (m *Model) snapshotStore() (key string, sw storedWorld) {
	key = seedKey(m.world.Seed, m.world.GenVersion)
	sw.Markers = make([]storedMarker, len(m.wmap.markers))
	for i, mk := range m.wmap.markers {
		sw.Markers[i] = storedMarker{mk.Pos.X, mk.Pos.Y, mk.Name}
	}
	sw.Explored = make([][2]int, 0, len(m.wmap.explored))
	for b := range m.wmap.explored {
		sw.Explored = append(sw.Explored, b)
	}
	sort.Slice(sw.Explored, func(i, j int) bool {
		a, b := sw.Explored[i], sw.Explored[j]
		return a[1] < b[1] || (a[1] == b[1] && a[0] < b[0])
	})
	return key, sw
}

// writeStore writes sw to the file atomically; a request older than a request
// already written is dropped. Errors are ignored: the map works without saving.
func writeStore(path, key string, sw storedWorld, seq uint64) {
	mapStoreMu.Lock()
	defer mapStoreMu.Unlock()
	if seq < mapStoreDone {
		return
	}
	all := readStore(path)
	all[key] = sw
	data, err := json.Marshal(all)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "map-*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
		return
	}
	mapStoreDone = seq
}

// saveMap prepares the asynchronous map write (nil if nothing to write).
func (m *Model) saveMap() tea.Cmd {
	path := mapStorePath()
	if path == "" || m.world == nil {
		return nil
	}
	key, sw := m.snapshotStore()
	seq := mapStoreSeq.Add(1)
	m.wmap.dirty, m.wmap.lastSave = false, time.Now()
	return func() tea.Msg {
		writeStore(path, key, sw, seq)
		return nil
	}
}

// saveMapNow writes immediately (client shutdown).
func (m *Model) saveMapNow() {
	if cmd := m.saveMap(); cmd != nil {
		cmd()
	}
}

// maybeSaveExplored writes the exploration if it changed at least exploreSaveEvery ago.
func (m *Model) maybeSaveExplored() tea.Cmd {
	if !m.wmap.dirty || time.Since(m.wmap.lastSave) < exploreSaveEvery {
		return nil
	}
	return m.saveMap()
}

// cleanMarkerName cleans up an entered name.
func cleanMarkerName(s string) string {
	return strings.TrimSpace(textsafe.Clean(s, maxMarkerName))
}

// uniqueMarkerName makes name unique (case-insensitive) among the markers, except the one at index skip.
func (s mapState) uniqueMarkerName(name string, skip int) string {
	taken := func(n string) bool {
		for i, mk := range s.markers {
			if i != skip && strings.EqualFold(mk.Name, n) {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := name + " " + strconv.Itoa(i); !taken(n) {
			return n
		}
	}
}

// findMarker looks up a marker by name: exact match (case-insensitive), otherwise prefix. It returns the
// number of matches (0 = unknown, >1 = ambiguous).
func (s mapState) findMarker(name string) (marker, int) {
	name = strings.TrimSpace(name)
	var pre []marker
	for _, mk := range s.markers {
		if strings.EqualFold(mk.Name, name) {
			return mk, 1
		}
		if name != "" && strings.HasPrefix(strings.ToLower(mk.Name), strings.ToLower(name)) {
			pre = append(pre, mk)
		}
	}
	if len(pre) == 1 {
		return pre[0], 1
	}
	return marker{}, len(pre)
}

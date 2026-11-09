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

// Persistance locale de la carte : repères et zones explorées sont écrits dans un fichier JSON du
// dossier de configuration de l'utilisateur, une entrée par graine de monde. Le serveur n'en sait rien.

const (
	exploreSaveEvery = 15 * time.Second // écriture différée de l'exploration
	maxMarkerName    = 24
)

// marker est un repère posé sur la carte.
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

// mapStorePath renvoie le fichier de sauvegarde ("" = pas de sauvegarde). Variable pour les tests.
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
	mapStoreSeq  atomic.Uint64 // numérote les demandes d'écriture
	mapStoreDone uint64        // dernière demande écrite (sous mapStoreMu)
)

func seedKey(seed int64) string { return strconv.FormatUint(uint64(seed), 16) }

// readStore lit le fichier complet ; absent ou corrompu = vide.
func readStore(path string) map[string]storedWorld {
	all := map[string]storedWorld{}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &all) != nil || all == nil {
			all = map[string]storedWorld{}
		}
	}
	return all
}

// loadMapState charge les repères et l'exploration enregistrés pour cette graine.
func loadMapState(seed int64) mapState {
	st := mapState{explored: map[[2]int]struct{}{}}
	path := mapStorePath()
	if path == "" {
		return st
	}
	mapStoreMu.Lock()
	sw := readStore(path)[seedKey(seed)]
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

// snapshotStore copie l'état à écrire (triée pour un fichier stable).
func (m *Model) snapshotStore() (key string, sw storedWorld) {
	key = seedKey(m.world.Seed)
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

// writeStore écrit sw dans le fichier de façon atomique ; une demande plus ancienne qu'une demande
// déjà écrite est abandonnée. Les erreurs sont ignorées : la carte fonctionne sans sauvegarde.
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

// saveMap prépare l'écriture asynchrone de la carte (nil si rien à écrire).
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

// saveMapNow écrit tout de suite (fermeture du client).
func (m *Model) saveMapNow() {
	if cmd := m.saveMap(); cmd != nil {
		cmd()
	}
}

// maybeSaveExplored écrit l'exploration si elle a changé depuis au moins exploreSaveEvery.
func (m *Model) maybeSaveExplored() tea.Cmd {
	if !m.wmap.dirty || time.Since(m.wmap.lastSave) < exploreSaveEvery {
		return nil
	}
	return m.saveMap()
}

// cleanMarkerName nettoie un nom saisi.
func cleanMarkerName(s string) string {
	return strings.TrimSpace(textsafe.Clean(s, maxMarkerName))
}

// uniqueMarkerName rend name unique (sans tenir compte de la casse) parmi les repères, sauf celui d'indice skip.
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

// findMarker cherche un repère par son nom : égalité exacte (sans casse), sinon préfixe. Renvoie le
// nombre de correspondances (0 = inconnu, >1 = ambigu).
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

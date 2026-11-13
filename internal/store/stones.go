package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/world"
)

// maxStoredStones bounds the standing stones remembered per account (there is about one per ten regions).
const maxStoredStones = 500

// encodeStones writes the activated stones in the form "x,y;x,y".
func encodeStones(list []world.RegionPos) string {
	var b strings.Builder
	for i, rp := range list {
		if i >= maxStoredStones {
			break
		}
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%d,%d", rp.X, rp.Y)
	}
	return b.String()
}

// decodeStones reads back encodeStones; unreadable elements are ignored.
func decodeStones(s string) []world.RegionPos {
	var out []world.RegionPos
	for _, f := range strings.Split(s, ";") {
		var rp world.RegionPos
		if n, err := fmt.Sscanf(f, "%d,%d", &rp.X, &rp.Y); err == nil && n == 2 && len(out) < maxStoredStones {
			out = append(out, rp)
		}
	}
	return out
}

// maxStoredMaps bounds the treasure maps remembered per account.
const maxStoredMaps = 16

// encodeMaps writes the tiles to dig in the form "x,y;x,y".
func encodeMaps(list []world.Pos) string {
	var b strings.Builder
	for i, p := range list {
		if i >= maxStoredMaps {
			break
		}
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%d,%d", p.X, p.Y)
	}
	return b.String()
}

// decodeMaps reads back encodeMaps; unreadable elements are ignored.
func decodeMaps(s string) []world.Pos {
	var out []world.Pos
	for _, f := range strings.Split(s, ";") {
		var p world.Pos
		if n, err := fmt.Sscanf(f, "%d,%d", &p.X, &p.Y); err == nil && n == 2 && len(out) < maxStoredMaps {
			out = append(out, p)
		}
	}
	return out
}

// Bounds on the resources remembered per account.
const (
	maxStoredResources = 64   // distinct types
	maxStoredQty       = 9999 // quantity per type
)

// encodeResources writes the materials bag in the form "iron:3;oak:12", sorted for a stable text.
func encodeResources(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k, n := range m {
		if n > 0 && !strings.ContainsAny(k, ";:") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i >= maxStoredResources {
			break
		}
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%s:%d", k, min(m[k], maxStoredQty))
	}
	return b.String()
}

// decodeResources reads back encodeResources; unreadable elements are ignored.
func decodeResources(s string) map[string]int {
	var out map[string]int
	for _, f := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(f, ":")
		n, err := strconv.Atoi(v)
		if ok && k != "" && err == nil && n > 0 && len(out) < maxStoredResources {
			if out == nil {
				out = map[string]int{}
			}
			out[k] = min(n, maxStoredQty)
		}
	}
	return out
}

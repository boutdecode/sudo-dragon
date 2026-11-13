package store

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/world"
)

// maxStoredStones borne les pierres dressées mémorisées par compte (il y en a environ une par dix régions).
const maxStoredStones = 500

// encodeStones écrit les pierres activées sous la forme « x,y;x,y ».
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

// decodeStones relit encodeStones ; les éléments illisibles sont ignorés.
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

// maxStoredMaps borne les cartes au trésor mémorisées par compte.
const maxStoredMaps = 16

// encodeMaps écrit les cases à creuser sous la forme « x,y;x,y ».
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

// decodeMaps relit encodeMaps ; les éléments illisibles sont ignorés.
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

// Bornes des ressources mémorisées par compte.
const (
	maxStoredResources = 64   // types distincts
	maxStoredQty       = 9999 // quantité par type
)

// encodeResources écrit le sac de matières sous la forme « iron:3;oak:12 », trié pour un texte stable.
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

// decodeResources relit encodeResources ; les éléments illisibles sont ignorés.
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

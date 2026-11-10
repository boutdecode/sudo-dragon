package store

import (
	"fmt"
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

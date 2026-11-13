package world

import (
	"fmt"
	"hash/fnv"
	"testing"
)

// layoutFingerprint summarises what defines the plans of 81 regions: type,
// location, tiles, original NPCs (type and position, without the villagers) and garrison posts. The
// names and personalities deliberately do not appear in it.
func layoutFingerprint(seed int64, gen int) uint64 {
	h := fnv.New64a()
	w := NewVersion(seed, gen)
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			s := w.structureForRegion(RegionPos{rx, ry})
			if s == nil {
				fmt.Fprintf(h, "%d,%d:none;", rx, ry)
				continue
			}
			fmt.Fprintf(h, "%d,%d:%d@%d,%d %dx%d;", rx, ry, s.Kind, s.Origin.X, s.Origin.Y, s.W, s.H)
			b := make([]byte, len(s.tiles))
			for i, t := range s.tiles {
				b[i] = byte(t)
			}
			h.Write(b)
			for _, n := range s.NPCs {
				if n.Kind != NPCMerchant && n.Kind != NPCHealer {
					continue // added afterwards: change neither the plans nor the original NPCs
				}
				fmt.Fprintf(h, "npc:%s@%d,%d;", n.Kind, n.Pos.X, n.Pos.Y)
			}
			for _, sl := range s.Slots {
				fmt.Fprintf(h, "slot:%d@%d,%d;", sl.Role, sl.Pos.X, sl.Pos.Y)
			}
		}
	}
	return h.Sum64()
}

// goldenLayouts are the plan fingerprints as shipped with the
// first version of the places. Already played worlds depend on these plans
// (saved positions, garrisons...): this test fails if a change to the
// generator modifies them. Adding fields (names, personalities) is allowed
// as long as these fingerprints remain unchanged; drawing new values from
// the generation random stream is not.
var goldenLayouts = map[int64]uint64{
	1:    0xc0018379d3525092,
	2:    0xea12344f9eb1917f,
	7:    0x9ed045bf21b2cd93,
	42:   0x366cbe705db12055,
	1234: 0xe348716afb567060,
}

// goldenLayoutsV2 likewise freezes the plans of generator v2, from its release.
var goldenLayoutsV2 = map[int64]uint64{
	1:    0x1b75386754ae80d4,
	2:    0x26d720c375a07de4,
	7:    0xd279261500a54d6f,
	42:   0xe059af7fba55c68e,
	1234: 0xe6abf3c16efdb858,
}

func TestExistingLayoutsAreFrozen(t *testing.T) {
	for seed, want := range goldenLayouts {
		if got := layoutFingerprint(seed, GenV1); got != want {
			t.Errorf("seed %d: the plans changed (fingerprint 0x%016x, expected 0x%016x): "+
				"already played worlds would be modified", seed, got, want)
		}
	}
}

func TestExistingLayoutsAreFrozenV2(t *testing.T) {
	for seed, want := range goldenLayoutsV2 {
		if got := layoutFingerprint(seed, GenV2); got != want {
			t.Errorf("seed %d: the v2 plans changed (fingerprint 0x%016x, expected 0x%016x)", seed, got, want)
		}
	}
}

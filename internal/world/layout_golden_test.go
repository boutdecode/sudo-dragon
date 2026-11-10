package world

import (
	"fmt"
	"hash/fnv"
	"testing"
)

// layoutFingerprint résume ce qui définit les plans de 81 régions: type,
// emplacement, tuiles, PNJ d'origine (type et position, sans les villageois) et postes de garnison. Les
// noms et personnalités n'y figurent volontairement pas.
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
					continue // ajoutés après coup : ne modifient ni les plans ni les PNJ d'origine
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

// goldenLayouts sont les empreintes des plans telles que livrées avec la
// première version des lieux. Les mondes déjà joués dépendent de ces plans
// (positions sauvegardées, garnisons…): ce test échoue si une évolution du
// générateur les modifie. Ajouter des champs (noms, personnalités) est permis
// tant que ces empreintes restent inchangées; tirer de nouvelles valeurs dans
// le flux aléatoire de la génération ne l'est pas.
var goldenLayouts = map[int64]uint64{
	1:    0xc0018379d3525092,
	2:    0xea12344f9eb1917f,
	7:    0x9ed045bf21b2cd93,
	42:   0x366cbe705db12055,
	1234: 0xe348716afb567060,
}

// goldenLayoutsV2 fige de même les plans du générateur v2, dès sa mise en service.
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
			t.Errorf("seed %d: les plans ont changé (empreinte 0x%016x, attendue 0x%016x): "+
				"les mondes déjà joués seraient modifiés", seed, got, want)
		}
	}
}

func TestExistingLayoutsAreFrozenV2(t *testing.T) {
	for seed, want := range goldenLayoutsV2 {
		if got := layoutFingerprint(seed, GenV2); got != want {
			t.Errorf("seed %d: les plans v2 ont changé (empreinte 0x%016x, attendue 0x%016x)", seed, got, want)
		}
	}
}

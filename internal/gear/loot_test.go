package gear

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestDungeonLootIsValidAndDeterministic(t *testing.T) {
	rp := world.RegionPos{X: 3, Y: -2}
	slots := map[Slot]bool{}
	for tier := 0; tier <= MaxTier+2; tier++ {
		for roll := uint64(0); roll < 400; roll++ {
			for _, l := range []i18n.Lang{i18n.FR, i18n.EN} {
				it := DungeonLoot(l, rp, tier, roll*2654435761)
				if it != DungeonLoot(l, rp, tier, roll*2654435761) {
					t.Fatal("same roll, different items")
				}
				if it.Name == "" || it.Desc == "" || it.Name[0] == '?' {
					t.Fatalf("texts: %+v", it)
				}
				if it.Tier < 1 || it.Tier > MaxTier {
					t.Fatalf("tier %d out of bounds", it.Tier)
				}
				if it.Origin != OriginDungeon(rp) || it.Origin.Index != -1 {
					t.Fatalf("origin %+v", it.Origin)
				}
				lo, hi := Bounds(it.Slot, it.Tier)
				if it.Slot == Weapon {
					if it.Quality < lo || it.Quality > hi || it.Damage().IsZero() || !it.Kind.Valid() {
						t.Fatalf("invalid weapon: %+v", it)
					}
				} else if it.Def < lo || it.Def > hi {
					t.Fatalf("invalid DEF: %+v", it)
				}
				plo, phi := PriceBounds(it.Tier, it.Power())
				if it.Price < plo || it.Price > phi {
					t.Fatalf("price %d outside [%d, %d]: %+v", it.Price, plo, phi, it)
				}
				// An item read back from a save must stay identical (except the identifier).
				it.ID = 1
				back, ok := Sanitize(it)
				if !ok || back != it {
					t.Fatalf("Sanitize modifies the item: %+v -> %+v", it, back)
				}
				slots[it.Slot] = true
			}
		}
	}
	if len(slots) != len(Slots) {
		t.Errorf("only %d slots rolled: %v", len(slots), slots)
	}
}

func TestTierForLevelAndMonsterLoot(t *testing.T) {
	for level, want := range map[int]int{1: 1, 4: 1, 5: 2, 9: 2, 10: 3, 15: 4, 19: 4, 20: 5, 99: 5} {
		if got := TierForLevel(level); got != want {
			t.Errorf("level %d: tier %d, expected %d", level, got, want)
		}
	}
	for tier := 1; tier <= MaxTier; tier++ {
		it := MonsterLoot(i18n.FR, world.RegionPos{X: 2, Y: -1}, tier, uint64(tier)*7919)
		if it.Tier != tier || it.MinLevel() != MinLevelOf[tier] || it.Origin != OriginDungeon(world.RegionPos{X: 2, Y: -1}) {
			t.Errorf("tier %d: %+v", tier, it)
		}
	}
}

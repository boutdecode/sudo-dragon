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
					t.Fatal("même tirage, objets différents")
				}
				if it.Name == "" || it.Desc == "" || it.Name[0] == '?' {
					t.Fatalf("textes: %+v", it)
				}
				if it.Tier < 1 || it.Tier > MaxTier {
					t.Fatalf("palier %d hors bornes", it.Tier)
				}
				if it.Origin != OriginDungeon(rp) || it.Origin.Index != -1 {
					t.Fatalf("origine %+v", it.Origin)
				}
				lo, hi := Bounds(it.Slot, it.Tier)
				if it.Slot == Weapon {
					if it.Quality < lo || it.Quality > hi || it.Damage().IsZero() || !it.Kind.Valid() {
						t.Fatalf("arme invalide: %+v", it)
					}
				} else if it.Def < lo || it.Def > hi {
					t.Fatalf("DEF invalide: %+v", it)
				}
				plo, phi := PriceBounds(it.Tier, it.Power())
				if it.Price < plo || it.Price > phi {
					t.Fatalf("prix %d hors de [%d, %d]: %+v", it.Price, plo, phi, it)
				}
				// Un objet relu d'une sauvegarde doit rester identique (hors identifiant).
				it.ID = 1
				back, ok := Sanitize(it)
				if !ok || back != it {
					t.Fatalf("Sanitize modifie l'objet: %+v -> %+v", it, back)
				}
				slots[it.Slot] = true
			}
		}
	}
	if len(slots) != len(Slots) {
		t.Errorf("seulement %d emplacements tirés: %v", len(slots), slots)
	}
}

func TestTierForLevelAndMonsterLoot(t *testing.T) {
	for level, want := range map[int]int{1: 1, 4: 1, 5: 2, 9: 2, 10: 3, 15: 4, 19: 4, 20: 5, 99: 5} {
		if got := TierForLevel(level); got != want {
			t.Errorf("niveau %d: palier %d, attendu %d", level, got, want)
		}
	}
	for tier := 1; tier <= MaxTier; tier++ {
		it := MonsterLoot(i18n.FR, world.RegionPos{X: 2, Y: -1}, tier, uint64(tier)*7919)
		if it.Tier != tier || it.MinLevel() != MinLevelOf[tier] || it.Origin != OriginDungeon(world.RegionPos{X: 2, Y: -1}) {
			t.Errorf("palier %d: %+v", tier, it)
		}
	}
}

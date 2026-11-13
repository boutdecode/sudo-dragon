package game

import (
	"math"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestCompassAndDistanceBuckets(t *testing.T) {
	// North is toward decreasing y.
	for _, c := range []struct {
		dx, dy int
		want   string
	}{
		{10, 0, "e"}, {-10, 0, "w"}, {0, -10, "n"}, {0, 10, "s"},
		{10, -10, "ne"}, {-10, -10, "nw"}, {10, 10, "se"}, {-10, 10, "sw"},
		{100, -20, "e"}, {20, -100, "n"},
	} {
		if got := compass(c.dx, c.dy); got != c.want {
			t.Errorf("(%d,%d) → %s, expected %s", c.dx, c.dy, got, c.want)
		}
	}
	for d, want := range map[float64]string{0: "near", 299: "near", 300: "walk", 699: "walk", 700: "far", 5000: "far"} {
		if got := distanceKey(d); got != want {
			t.Errorf("%v → %s, expected %s", d, got, want)
		}
	}
}

func TestChiefKnowsOnlyNearbyPlacesSortedByDistance(t *testing.T) {
	r := newNPCRig(t, world.NPCChief)
	lms := r.g.landmarksFor(r.npc, i18n.FR)
	if len(lms) == 0 || len(lms) > maxLandmarks {
		t.Fatalf("%d known places", len(lms))
	}
	// Comparison with the world: exactly the places within 3 regions, excluding its own.
	want := map[string]bool{}
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			rp := world.RegionPos{X: r.npc.Region.X + dx, Y: r.npc.Region.Y + dy}
			if s := r.g.World.StructureOfRegion(rp); s != nil && rp != r.npc.Region {
				want[s.NameIn(i18n.FR)] = true
			}
		}
	}
	prev := -1.0
	for _, lm := range lms {
		if !want[lm.Name] {
			t.Errorf("%q should not be known", lm.Name)
		}
		if lm.Name == "" || lm.Kind == "" || lm.Direction == "" || lm.Distance == "" {
			t.Errorf("incomplete entry: %+v", lm)
		}
		_ = prev
	}
	if len(want) >= maxLandmarks && len(lms) != maxLandmarks {
		t.Errorf("%d possible places, %d kept", len(want), len(lms))
	}
	// The nearest comes first.
	best, bestD := "", math.MaxFloat64
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			rp := world.RegionPos{X: r.npc.Region.X + dx, Y: r.npc.Region.Y + dy}
			if s := r.g.World.StructureOfRegion(rp); s != nil && rp != r.npc.Region {
				c := s.Center()
				if d := math.Hypot(float64(c.X-r.npc.Pos.X), float64(c.Y-r.npc.Pos.Y)); d < bestD {
					best, bestD = s.NameIn(i18n.FR), d
				}
			}
		}
	}
	if lms[0].Name != best {
		t.Errorf("first place %q, the nearest is %q", lms[0].Name, best)
	}
	// Cached, and localised.
	if again := r.g.landmarksFor(r.npc, i18n.FR); &again[0] != &lms[0] {
		t.Error("list recomputed instead of cached")
	}
	en := r.g.landmarksFor(r.npc, i18n.EN)
	if !strings.HasPrefix(en[0].Direction, "to the ") {
		t.Errorf("English direction: %q", en[0].Direction)
	}
}

func TestOnlyChiefsAndMayorsGetLandmarks(t *testing.T) {
	for kind, want := range map[world.NPCKind]bool{world.NPCChief: true, world.NPCMayor: true, world.NPCVillager: false, world.NPCHealer: false, world.NPCMerchant: false} {
		r := newNPCRig(t, kind)
		ft := newFakeTalker()
		r.g.Talker, r.p.consent = ft, true
		r.talk()
		r.act(protocol.NPCTalk)
		got := len(ft.req(0).Landmarks) > 0
		if got != want {
			t.Errorf("%s: known places = %v, expected %v", kind, got, want)
		}
	}
}

func TestChiefGreetsWithNeighboursWhenNoModelIsAvailable(t *testing.T) {
	r := newNPCRig(t, world.NPCChief) // no Talker
	r.p.lang = i18n.FR                // the greeting below is asserted on French catalogue text
	r.talk()
	d := r.last()
	lms := r.g.landmarksFor(r.npc, i18n.FR)
	if len(d.Log) != 1 || !strings.Contains(d.Log[0], lms[0].Name) || !strings.Contains(d.Log[0], "plus proches voisins") {
		t.Fatalf("greeting without AI: %+v", d.Log)
	}
	if len(d.Offers) != 0 {
		t.Fatalf("a chief neither sells nor heals: %+v", d.Offers)
	}
	// With a model: a simple greeting.
	r2 := newNPCRig(t, world.NPCChief)
	r2.g.Talker = newFakeTalker()
	r2.p.lang = i18n.FR
	r2.talk()
	if l := r2.last().Log; len(l) != 1 || strings.Contains(l[0], "voisins") {
		t.Fatalf("greeting with AI: %+v", l)
	}
	if _, has := r2.offer(protocol.NPCTalk); !has {
		t.Fatal("\"Talk\" missing")
	}
}

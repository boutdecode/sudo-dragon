package game

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// nodeGame returns a game in world v3 and the position of a resource node of the wanted category.
func nodeGame(t *testing.T, tile world.Tile) (*Game, world.Pos) {
	t.Helper()
	g := New(world.New(1))
	for r := 20; r < 400; r++ {
		for x := -r; x <= r; x++ {
			for _, y := range []int{-r, r} {
				if g.World.At(x, y) == tile && g.World.At(x+1, y).Walkable() {
					return g, world.Pos{X: x, Y: y}
				}
			}
		}
	}
	t.Fatalf("no node %d found", tile)
	return nil, world.Pos{}
}

// forceRoll sets the game's randomness so that the next gathering roll succeeds (or fails) at the given level.
func forceRoll(g *Game, level int, success bool) {
	for seed := uint64(0); ; seed++ {
		if (rand.New(rand.NewPCG(seed, 1)).IntN(100) < resource.Chance(level)) == success {
			g.rng = rand.New(rand.NewPCG(seed, 1))
			return
		}
	}
}

func total(p *player) int {
	n := 0
	for _, q := range p.resources {
		n += q
	}
	return n
}

func TestGatherSuccessGivesOneUnitAndXP(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	var bag protocol.Bag
	p.sendBagMsg = func(b protocol.Bag) { bag = b }
	forceRoll(g, 1, true)
	g.gather(p.id)
	if total(p) != 1 {
		t.Fatalf("%d units, expected 1 (%v)", total(p), p.resources)
	}
	kind, _ := g.World.ResourceAt(at.X, at.Y)
	info, _ := resource.Lookup(kind)
	if p.skills[resource.Wood] != resource.GainXP(info.Tier) {
		t.Errorf("XP %d, expected %d", p.skills[resource.Wood], resource.GainXP(info.Tier))
	}
	if len(bag.Resources) != 1 || len(bag.Jobs) != int(resource.NumCategories) {
		t.Fatalf("bag sent %+v", bag)
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("the node is not depleted")
	}
	// Depleted: no second gathering; then it grows back.
	p.gatherUntil = 0
	g.gather(p.id)
	if total(p) != 1 {
		t.Fatal("a depleted node gave materials")
	}
	g.tick += nodeRespawn
	g.expireNodes()
	if len(g.spent) != 0 {
		t.Fatalf("node not regrown: %v", g.spent)
	}
}

func TestGatherFailureSpendsNodeWithoutXP(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	forceRoll(g, 1, false)
	g.gather(p.id)
	if total(p) != 0 || p.skills[resource.Wood] != 0 {
		t.Fatalf("failure rewarded: %v %v", p.resources, p.skills)
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("a failure must deplete the node")
	}
}

func TestGatherLevelUp(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	p.skills = map[resource.Category]int{resource.Wood: resource.XPToNext(1) - 1}
	forceRoll(g, 1, true)
	g.gather(p.id)
	if resource.LevelOf(p.skills[resource.Wood]) != 2 {
		t.Fatalf("level %d, expected 2", resource.LevelOf(p.skills[resource.Wood]))
	}
	if n := notices(p); !strings.Contains(n, "2") {
		t.Errorf("no level announcement: %q", n)
	}
}

func TestGatherFromNeighbourTile(t *testing.T) {
	g, at := nodeGame(t, world.OreNode)
	p, _ := joinRec(g, "a")
	place(g, p, world.Pos{X: at.X + 1, Y: at.Y})
	forceRoll(g, 1, true)
	g.gather(p.id)
	if len(p.resources) != 1 {
		t.Fatalf("nothing gathered from the neighbouring tile: %v", p.resources)
	}
	for k := range p.resources {
		if info, _ := resource.Lookup(k); info.Cat != resource.Ore {
			t.Errorf("%s is not an ore", k)
		}
	}
}

func TestGatherNothingAndCooldown(t *testing.T) {
	g := New(world.New(1))
	p, _ := joinRec(g, "a")
	g.gather(p.id)
	if len(p.resources) != 0 || len(g.spent) != 0 {
		t.Fatal("gathering without a node")
	}
	if g.tick >= p.gatherUntil {
		t.Fatal("no delay after an attempt")
	}
}

// A full category bag refuses gathering without touching the node; the job's level enlarges it.
func TestGatherBagCapacityFollowsLevel(t *testing.T) {
	g, at := nodeGame(t, world.HerbNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	p.resources = map[resource.Kind]int{"sage": resource.Capacity(1)}
	forceRoll(g, 1, true)
	g.gather(p.id)
	if total(p) != resource.Capacity(1) {
		t.Fatalf("the full bag grew: %d", total(p))
	}
	if _, spent := g.spent[at]; spent {
		t.Fatal("a full bag must not deplete the node")
	}
	// Another job is not affected; and the next level enlarges the bag.
	p.skills = map[resource.Category]int{resource.Herb: resource.XPToNext(1)}
	p.gatherUntil = 0
	forceRoll(g, 2, true)
	g.gather(p.id)
	if total(p) != resource.Capacity(1)+1 {
		t.Fatalf("bag %d after the level up, expected %d", total(p), resource.Capacity(1)+1)
	}
}

func TestSyncNodesSendsSpentList(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	var got []protocol.Nodes
	p.sendNodesMsg = func(n protocol.Nodes) { got = append(got, n) }
	g.syncNodes(p)
	if len(got) != 0 {
		t.Fatal("useless message without a depleted node")
	}
	g.gather(p.id)
	g.syncNodes(p)
	g.syncNodes(p) // unchanged: no repetition
	if len(got) != 1 || len(got[0].Spent) != 1 || got[0].Spent[0] != [2]int{at.X, at.Y} {
		t.Fatalf("messages %v", got)
	}
}

func TestResourcesSaveLoad(t *testing.T) {
	in := map[resource.Kind]int{"iron": 3, "sage": 120}
	out := loadResources(saveResources(in))
	if out["iron"] != 3 || out["sage"] != maxResourceQty || len(out) != 2 {
		t.Fatalf("reloaded %v", out)
	}
	if got := loadResources(map[string]int{"unobtainium": 5, "oak": -1}); len(got) != 0 {
		t.Fatalf("invalid resources kept: %v", got)
	}
	if saveResources(nil) != nil {
		t.Fatal("empty bag: nil expected")
	}
}

func TestSkillsSaveLoad(t *testing.T) {
	in := map[resource.Category]int{resource.Wood: 120, resource.Fish: 5}
	out := loadSkills(saveSkills(in))
	if out[resource.Wood] != 120 || out[resource.Fish] != 5 || len(out) != 2 {
		t.Fatalf("reloaded %v", out)
	}
	if saveSkills(nil) != nil || len(loadSkills(map[string]int{"unknown": 9})) != 0 {
		t.Fatal("empty or unknown entries mishandled")
	}
}

func TestJobInfosReportBagAndChance(t *testing.T) {
	g := New(world.New(1))
	p, _ := joinRec(g, "a")
	p.resources = map[resource.Kind]int{"oak": 2, "pine": 1}
	jobs := jobInfos(p)
	if len(jobs) != int(resource.NumCategories) {
		t.Fatalf("%d jobs", len(jobs))
	}
	if jobs[resource.Wood].Used != 3 || jobs[resource.Wood].Cap != resource.Capacity(1) || jobs[resource.Wood].Chance != resource.Chance(1) {
		t.Errorf("woodcutter %+v", jobs[resource.Wood])
	}
}

func TestFishingSpotInRiver(t *testing.T) {
	g, at := nodeGame(t, world.FishNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	forceRoll(g, 1, true)
	g.gather(p.id)
	if len(p.resources) != 1 {
		t.Fatalf("nothing fished: %v", p.resources)
	}
	for k := range p.resources {
		if info, _ := resource.Lookup(k); info.Cat != resource.Fish {
			t.Errorf("%s is not a fish", k)
		}
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("the shoal is not depleted")
	}
	p.gatherUntil = 0
	g.gather(p.id)
	if total(p) != 1 {
		t.Fatal("a depleted shoal gave again")
	}
}

// A river without a shoal gives nothing: one cannot fish endlessly on every tile.
func TestNoFishingOnPlainRiver(t *testing.T) {
	g := New(world.New(1))
	for r := 20; r < 400; r++ {
		for x := -r; x <= r; x++ {
			if g.World.At(x, r) == world.River && g.World.At(x+1, r) != world.FishNode && g.World.At(x-1, r) != world.FishNode {
				p, _ := joinRec(g, "a")
				place(g, p, world.Pos{X: x, Y: r})
				g.gather(p.id)
				if len(p.resources) != 0 {
					t.Fatalf("fishing without a shoal: %v", p.resources)
				}
				return
			}
		}
	}
	t.Skip("no river found")
}

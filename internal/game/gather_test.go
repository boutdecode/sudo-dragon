package game

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// nodeGame renvoie un jeu en monde v3 et la position d'un nœud de ressource de la catégorie voulue.
func nodeGame(t *testing.T, tile world.Tile) (*Game, world.Pos) {
	t.Helper()
	g := New(world.NewVersion(1, world.GenV3))
	for r := 20; r < 400; r++ {
		for x := -r; x <= r; x++ {
			for _, y := range []int{-r, r} {
				if g.World.At(x, y) == tile && g.World.At(x+1, y).Walkable() {
					return g, world.Pos{X: x, Y: y}
				}
			}
		}
	}
	t.Fatalf("aucun nœud %d trouvé", tile)
	return nil, world.Pos{}
}

// forceRoll règle l'aléa du jeu pour que le prochain jet de récolte réussisse (ou échoue) au niveau donné.
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
		t.Fatalf("%d unités, attendu 1 (%v)", total(p), p.resources)
	}
	kind, _ := g.World.ResourceAt(at.X, at.Y)
	info, _ := resource.Lookup(kind)
	if p.skills[resource.Wood] != resource.GainXP(info.Tier) {
		t.Errorf("XP %d, attendu %d", p.skills[resource.Wood], resource.GainXP(info.Tier))
	}
	if len(bag.Resources) != 1 || len(bag.Jobs) != int(resource.NumCategories) {
		t.Fatalf("sac envoyé %+v", bag)
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("le nœud n'est pas épuisé")
	}
	// Épuisé: pas de seconde récolte; puis il repousse.
	p.gatherUntil = 0
	g.gather(p.id)
	if total(p) != 1 {
		t.Fatal("un nœud épuisé a donné des matières")
	}
	g.tick += nodeRespawn
	g.expireNodes()
	if len(g.spent) != 0 {
		t.Fatalf("nœud non repoussé: %v", g.spent)
	}
}

func TestGatherFailureSpendsNodeWithoutXP(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	forceRoll(g, 1, false)
	g.gather(p.id)
	if total(p) != 0 || p.skills[resource.Wood] != 0 {
		t.Fatalf("échec récompensé: %v %v", p.resources, p.skills)
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("un échec doit épuiser le nœud")
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
		t.Fatalf("niveau %d, attendu 2", resource.LevelOf(p.skills[resource.Wood]))
	}
	if n := notices(p); !strings.Contains(n, "2") {
		t.Errorf("pas d'annonce de niveau: %q", n)
	}
}

func TestGatherFromNeighbourTile(t *testing.T) {
	g, at := nodeGame(t, world.OreNode)
	p, _ := joinRec(g, "a")
	place(g, p, world.Pos{X: at.X + 1, Y: at.Y})
	forceRoll(g, 1, true)
	g.gather(p.id)
	if len(p.resources) != 1 {
		t.Fatalf("rien récolté depuis la case voisine: %v", p.resources)
	}
	for k := range p.resources {
		if info, _ := resource.Lookup(k); info.Cat != resource.Ore {
			t.Errorf("%s n'est pas un minerai", k)
		}
	}
}

func TestGatherNothingAndCooldown(t *testing.T) {
	g := New(world.NewVersion(1, world.GenV3))
	p, _ := joinRec(g, "a")
	g.gather(p.id)
	if len(p.resources) != 0 || len(g.spent) != 0 {
		t.Fatal("récolte sans nœud")
	}
	if g.tick >= p.gatherUntil {
		t.Fatal("pas de délai après une tentative")
	}
}

// Le sac d'une catégorie plein refuse la récolte sans toucher au nœud; le niveau du métier l'agrandit.
func TestGatherBagCapacityFollowsLevel(t *testing.T) {
	g, at := nodeGame(t, world.HerbNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	p.resources = map[resource.Kind]int{"sage": resource.Capacity(1)}
	forceRoll(g, 1, true)
	g.gather(p.id)
	if total(p) != resource.Capacity(1) {
		t.Fatalf("le sac plein a grossi: %d", total(p))
	}
	if _, spent := g.spent[at]; spent {
		t.Fatal("un sac plein ne doit pas épuiser le nœud")
	}
	// Un autre métier n'est pas concerné; et le niveau suivant agrandit le sac.
	p.skills = map[resource.Category]int{resource.Herb: resource.XPToNext(1)}
	p.gatherUntil = 0
	forceRoll(g, 2, true)
	g.gather(p.id)
	if total(p) != resource.Capacity(1)+1 {
		t.Fatalf("sac %d après la montée de niveau, attendu %d", total(p), resource.Capacity(1)+1)
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
		t.Fatal("message inutile sans nœud épuisé")
	}
	g.gather(p.id)
	g.syncNodes(p)
	g.syncNodes(p) // inchangé: pas de répétition
	if len(got) != 1 || len(got[0].Spent) != 1 || got[0].Spent[0] != [2]int{at.X, at.Y} {
		t.Fatalf("messages %v", got)
	}
}

func TestResourcesSaveLoad(t *testing.T) {
	in := map[resource.Kind]int{"iron": 3, "sage": 120}
	out := loadResources(saveResources(in))
	if out["iron"] != 3 || out["sage"] != maxResourceQty || len(out) != 2 {
		t.Fatalf("relu %v", out)
	}
	if got := loadResources(map[string]int{"unobtainium": 5, "oak": -1}); len(got) != 0 {
		t.Fatalf("ressources invalides conservées: %v", got)
	}
	if saveResources(nil) != nil {
		t.Fatal("sac vide: attendu nil")
	}
}

func TestSkillsSaveLoad(t *testing.T) {
	in := map[resource.Category]int{resource.Wood: 120, resource.Fish: 5}
	out := loadSkills(saveSkills(in))
	if out[resource.Wood] != 120 || out[resource.Fish] != 5 || len(out) != 2 {
		t.Fatalf("relu %v", out)
	}
	if saveSkills(nil) != nil || len(loadSkills(map[string]int{"inconnu": 9})) != 0 {
		t.Fatal("entrées vides ou inconnues mal traitées")
	}
}

func TestJobInfosReportBagAndChance(t *testing.T) {
	g := New(world.NewVersion(1, world.GenV3))
	p, _ := joinRec(g, "a")
	p.resources = map[resource.Kind]int{"oak": 2, "pine": 1}
	jobs := jobInfos(p)
	if len(jobs) != int(resource.NumCategories) {
		t.Fatalf("%d métiers", len(jobs))
	}
	if jobs[resource.Wood].Used != 3 || jobs[resource.Wood].Cap != resource.Capacity(1) || jobs[resource.Wood].Chance != resource.Chance(1) {
		t.Errorf("bûcheron %+v", jobs[resource.Wood])
	}
}

func TestFishingSpotInRiver(t *testing.T) {
	g, at := nodeGame(t, world.FishNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	forceRoll(g, 1, true)
	g.gather(p.id)
	if len(p.resources) != 1 {
		t.Fatalf("rien pêché: %v", p.resources)
	}
	for k := range p.resources {
		if info, _ := resource.Lookup(k); info.Cat != resource.Fish {
			t.Errorf("%s n'est pas un poisson", k)
		}
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("le banc n'est pas épuisé")
	}
	p.gatherUntil = 0
	g.gather(p.id)
	if total(p) != 1 {
		t.Fatal("un banc épuisé a encore donné")
	}
}

// Une rivière sans banc ne donne rien: on ne pêche pas à l'infini sur toutes les cases.
func TestNoFishingOnPlainRiver(t *testing.T) {
	g := New(world.NewVersion(1, world.GenV3))
	for r := 20; r < 400; r++ {
		for x := -r; x <= r; x++ {
			if g.World.At(x, r) == world.River && g.World.At(x+1, r) != world.FishNode && g.World.At(x-1, r) != world.FishNode {
				p, _ := joinRec(g, "a")
				place(g, p, world.Pos{X: x, Y: r})
				g.gather(p.id)
				if len(p.resources) != 0 {
					t.Fatalf("pêche sans banc: %v", p.resources)
				}
				return
			}
		}
	}
	t.Skip("aucune rivière trouvée")
}

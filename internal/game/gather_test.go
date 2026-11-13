package game

import (
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

func TestGatherAddsResourceAndSpendsNode(t *testing.T) {
	g, at := nodeGame(t, world.TreeNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	var bag protocol.Bag
	p.sendBagMsg = func(b protocol.Bag) { bag = b }

	g.gather(p.id)
	total := 0
	for _, n := range p.resources {
		total += n
	}
	if total < 1 || total > 3 {
		t.Fatalf("récolte de %d unités, attendu 1 à 3 (%v)", total, p.resources)
	}
	if len(bag.Resources) != 1 || bag.Resources[0].Qty != total {
		t.Fatalf("sac envoyé %v", bag.Resources)
	}
	if _, spent := g.spent[at]; !spent {
		t.Fatal("le nœud n'est pas épuisé")
	}

	// Épuisé: une seconde récolte ne donne rien.
	p.gatherUntil = 0
	g.gather(p.id)
	again := 0
	for _, n := range p.resources {
		again += n
	}
	if again != total {
		t.Fatalf("un nœud épuisé a donné des matières (%d -> %d)", total, again)
	}

	// Il repousse.
	g.tick += nodeRespawn
	g.expireNodes()
	if len(g.spent) != 0 {
		t.Fatalf("nœud non repoussé: %v", g.spent)
	}
	p.gatherUntil = 0
	g.gather(p.id)
	if _, spent := g.spent[at]; !spent {
		t.Fatal("le nœud repoussé n'a pas pu être récolté")
	}
}

func TestGatherFromNeighbourTile(t *testing.T) {
	g, at := nodeGame(t, world.OreNode)
	p, _ := joinRec(g, "a")
	place(g, p, world.Pos{X: at.X + 1, Y: at.Y})
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

func TestGatherCapsQuantity(t *testing.T) {
	g, at := nodeGame(t, world.HerbNode)
	p, _ := joinRec(g, "a")
	place(g, p, at)
	kind, _ := g.World.ResourceAt(at.X, at.Y)
	p.resources = map[resource.Kind]int{kind: maxResourceQty}
	g.gather(p.id)
	if p.resources[kind] != maxResourceQty {
		t.Fatalf("quantité %d au-delà du plafond", p.resources[kind])
	}
	if _, spent := g.spent[at]; spent {
		t.Fatal("un nœud a été épuisé sans rien donner")
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

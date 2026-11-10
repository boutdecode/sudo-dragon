package game

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestGiveMapPicksAFarOrdinarySpotAndCapsTheCount(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	from := world.Pos{X: 10, Y: 10}
	for i := range maxMaps {
		line := g.giveMap(p, from)
		if line == "" {
			t.Fatalf("carte %d refusée", i+1)
		}
		spot := p.maps[len(p.maps)-1]
		if !strings.Contains(line, "/goto") || !strings.Contains(line, "("+itoa(spot.X)+", "+itoa(spot.Y)+")") {
			t.Errorf("ligne sans les coordonnées %v: %q", spot, line)
		}
		if d := max(abs(spot.X-from.X), abs(spot.Y-from.Y)); d < mapMinDistance {
			t.Errorf("case à %d cases de la source, moins que %d", d, mapMinDistance)
		}
		if g.World.POIAt(spot.X, spot.Y) != nil || g.World.StructureAt(spot.X, spot.Y) != nil {
			t.Errorf("case %v occupée", spot)
		}
	}
	seen := map[world.Pos]bool{}
	for _, m := range p.maps {
		if seen[m] {
			t.Errorf("carte en double: %v", m)
		}
		seen[m] = true
	}
	if g.giveMap(p, from) != "" || len(p.maps) != maxMaps {
		t.Fatalf("plus de %d cartes: %d", maxMaps, len(p.maps))
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// walkTo fait marcher p sur une case ordinaire, par l'ouest.
func walkTo(g *Game, p *player, dst world.Pos) {
	g.setPos(p, world.Pos{X: dst.X - 1, Y: dst.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
}

func TestDigUnearthsTheTreasureOnlyOnTheExactSpot(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	if g.giveMap(p, world.Pos{X: 10, Y: 10}) == "" {
		t.Fatal("pas de carte")
	}
	spot := p.maps[0]
	gold, items := p.stats.Gold, len(p.inv.Items)

	// Une case voisine ne creuse rien.
	g.setPos(p, world.Pos{X: spot.X - 2, Y: spot.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
	if len(p.maps) != 1 || p.stats.Gold != gold {
		t.Fatalf("creusé à côté de la case: %v", p.maps)
	}

	walkTo(g, p, spot)
	if p.pos != spot {
		t.Fatalf("pas sur la case: %v, attendu %v", p.pos, spot)
	}
	if len(p.maps) != 0 || p.stats.Gold <= gold || len(p.inv.Items) != items+1 {
		t.Fatalf("trésor non déterré: cartes %v, or %d -> %d, objets %d -> %d (%s)", p.maps, gold, p.stats.Gold, items, len(p.inv.Items), notices(p))
	}
	if !strings.Contains(notices(p), "trésor") {
		t.Errorf("pas de message: %s", notices(p))
	}
}

func TestDigWithFullBagKeepsTheMap(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	g.giveMap(p, world.Pos{X: 10, Y: 10})
	spot := p.maps[0]
	p.inv.Items = make([]gear.Item, gear.BagCapacity)
	gold := p.stats.Gold
	walkTo(g, p, spot)
	if len(p.maps) != 1 || p.stats.Gold != gold {
		t.Fatalf("carte consommée avec un sac plein: %v", p.maps)
	}
	if !strings.Contains(notices(p), "sac est plein") {
		t.Errorf("pas d'avis de sac plein: %s", notices(p))
	}
}

func TestMapsAreSavedAndSentToTheSheet(t *testing.T) {
	g := newGame()
	var saved Saved
	g.Persist = func(_ string, s Saved) { saved = s }
	var bag protocol.Bag
	p, _ := joinRec(g, "a")
	p.sendBagMsg = func(b protocol.Bag) { bag = b }
	g.giveMap(p, world.Pos{X: 10, Y: 10})
	if len(saved.Maps) != 1 || saved.Maps[0] != p.maps[0] {
		t.Fatalf("carte non sauvegardée: %v", saved.Maps)
	}
	g.sendBag(p)
	if len(bag.Maps) != 1 || bag.Maps[0].X != p.maps[0].X || bag.Maps[0].Y != p.maps[0].Y {
		t.Fatalf("carte absente du sac envoyé: %+v", bag.Maps)
	}
	q := g.joinWith("b", func(protocol.Snapshot) {}, &Saved{Maps: saved.Maps, Pos: g.World.FindSpawn(), HP: 5})
	if len(q.maps) != 1 || q.maps[0] != p.maps[0] {
		t.Fatalf("carte non restaurée: %v", q.maps)
	}
}

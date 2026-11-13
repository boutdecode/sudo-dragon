package game

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func TestGiveMapPicksAFarOrdinarySpotAndCapsTheCount(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	from := world.Pos{X: 10, Y: 10}
	for i := range maxMaps {
		line := g.giveMap(p, from)
		if line == "" {
			t.Fatalf("map %d refused", i+1)
		}
		spot := p.maps[len(p.maps)-1]
		if !strings.Contains(line, "/goto") || !strings.Contains(line, "("+itoa(spot.X)+", "+itoa(spot.Y)+")") {
			t.Errorf("line without the coordinates %v: %q", spot, line)
		}
		if d := max(abs(spot.X-from.X), abs(spot.Y-from.Y)); d < mapMinDistance {
			t.Errorf("tile %d tiles from the source, less than %d", d, mapMinDistance)
		}
		if g.World.POIAt(spot.X, spot.Y) != nil || g.World.StructureAt(spot.X, spot.Y) != nil {
			t.Errorf("tile %v occupied", spot)
		}
	}
	seen := map[world.Pos]bool{}
	for _, m := range p.maps {
		if seen[m] {
			t.Errorf("duplicate map: %v", m)
		}
		seen[m] = true
	}
	if g.giveMap(p, from) != "" || len(p.maps) != maxMaps {
		t.Fatalf("more than %d maps: %d", maxMaps, len(p.maps))
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// walkTo walks p onto an ordinary tile, from the west.
func walkTo(g *Game, p *player, dst world.Pos) {
	g.setPos(p, world.Pos{X: dst.X - 1, Y: dst.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
}

func TestDigUnearthsTheTreasureOnlyOnTheExactSpot(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	if g.giveMap(p, world.Pos{X: 10, Y: 10}) == "" {
		t.Fatal("no map")
	}
	spot := p.maps[0]
	gold, items := p.stats.Gold, len(p.inv.Items)

	// A neighbouring tile digs nothing.
	g.setPos(p, world.Pos{X: spot.X - 2, Y: spot.Y})
	p.cooldown = 0
	g.move(p.id, 1, 0)
	g.step()
	if len(p.maps) != 1 || p.stats.Gold != gold {
		t.Fatalf("dug next to the tile: %v", p.maps)
	}

	walkTo(g, p, spot)
	if p.pos != spot {
		t.Fatalf("not on the tile: %v, expected %v", p.pos, spot)
	}
	if len(p.maps) != 0 || p.stats.Gold <= gold || len(p.inv.Items) != items+1 {
		t.Fatalf("treasure not unearthed: maps %v, gold %d -> %d, items %d -> %d (%s)", p.maps, gold, p.stats.Gold, items, len(p.inv.Items), notices(p))
	}
	if !strings.Contains(notices(p), "trésor") {
		t.Errorf("no message: %s", notices(p))
	}
}

func TestDigWithFullBagKeepsTheMap(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	g.giveMap(p, world.Pos{X: 10, Y: 10})
	spot := p.maps[0]
	p.inv.Items = make([]gear.Item, gear.BagCapacity)
	gold := p.stats.Gold
	walkTo(g, p, spot)
	if len(p.maps) != 1 || p.stats.Gold != gold {
		t.Fatalf("map consumed with a full bag: %v", p.maps)
	}
	if !strings.Contains(notices(p), "sac est plein") {
		t.Errorf("no full-bag notice: %s", notices(p))
	}
}

func TestMapsAreSavedAndSentToTheSheet(t *testing.T) {
	g := newGame()
	var saved Saved
	g.Persist = func(_ string, s Saved) { saved = s }
	var bag protocol.Bag
	p, _ := joinRec(g, "a")
	p.lang = i18n.FR // assertions below are on French catalogue text
	p.sendBagMsg = func(b protocol.Bag) { bag = b }
	g.giveMap(p, world.Pos{X: 10, Y: 10})
	if len(saved.Maps) != 1 || saved.Maps[0] != p.maps[0] {
		t.Fatalf("map not saved: %v", saved.Maps)
	}
	g.sendBag(p)
	if len(bag.Maps) != 1 || bag.Maps[0].X != p.maps[0].X || bag.Maps[0].Y != p.maps[0].Y {
		t.Fatalf("map missing from the bag sent: %+v", bag.Maps)
	}
	q := g.joinWith("b", func(protocol.Snapshot) {}, &Saved{Maps: saved.Maps, Pos: g.World.FindSpawn(), HP: 5})
	if len(q.maps) != 1 || q.maps[0] != p.maps[0] {
		t.Fatalf("map not restored: %v", q.maps)
	}
}

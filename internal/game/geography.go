package game

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Geography of village chiefs and mayors: they know the places in neighbouring regions.
// These are facts established by the server (name, kind, direction, distance), not player speech.

const (
	landmarkRange  = 3 // in regions: range of chiefs' and mayors' geography (independent of rumours)
	maxLandmarks   = 10
	distNear       = 300 // tiles: below this, "very close"
	distWalk       = 700 // tiles: below this, "a good walk"
	maxGeoCacheLen = 512
)

type geoKey struct {
	npc  world.NPCKey
	lang i18n.Lang
}

// knowsNeighbours says whether this NPC type knows the neighbouring places.
func knowsNeighbours(k world.NPCKind) bool {
	return k == world.NPCChief || k == world.NPCMayor || k == world.NPCGuard
}

// compass gives the catalogue key of the direction (north is toward decreasing y, as on screen).
func compass(dx, dy int) string {
	angle := math.Atan2(float64(-dy), float64(dx)) // 0 = east, π/2 = north
	sector := int(math.Round(angle/(math.Pi/4))+8) % 8
	return [8]string{"e", "ne", "n", "nw", "w", "sw", "s", "se"}[sector]
}

// distanceKey classifies a distance in tiles.
func distanceKey(d float64) string {
	switch {
	case d < distNear:
		return "near"
	case d < distWalk:
		return "walk"
	}
	return "far"
}

// landmarksFor lists, from nearest to farthest, the other places at most landmarkRange regions from the NPC.
// The result is cached per NPC and per language (the world does not change).
func (g *Game) landmarksFor(npc world.NPC, l i18n.Lang) []npcai.Landmark {
	key := geoKey{npc.Key(), l}
	if v, ok := g.geo[key]; ok {
		return v
	}
	own := g.World.TerritoryAt(npc.Pos.X, npc.Pos.Y).ID
	type found struct {
		lm   npcai.Landmark
		dist float64
	}
	var all []found
	for ry := npc.Region.Y - landmarkRange; ry <= npc.Region.Y+landmarkRange; ry++ {
		for rx := npc.Region.X - landmarkRange; rx <= npc.Region.X+landmarkRange; rx++ {
			rp := world.RegionPos{X: rx, Y: ry}
			s := g.World.StructureOfRegion(rp)
			if s == nil || rp == npc.Region {
				continue
			}
			c := s.Center()
			dx, dy := c.X-npc.Pos.X, c.Y-npc.Pos.Y
			d := math.Hypot(float64(dx), float64(dy))
			lm := npcai.Landmark{
				Name:      s.NameIn(l),
				Kind:      i18n.T(l, "place.kind."+s.Kind.String()),
				Direction: i18n.T(l, "compass."+compass(dx, dy)),
				Distance:  i18n.T(l, "distance."+distanceKey(d)),
			}
			if t := g.World.TerritoryAt(c.X, c.Y); t.ID != own {
				lm.Region = t.NameIn(l)
			}
			if s.Kind == world.KindFortress {
				lm.Note = i18n.T(l, "geo.fortress_note")
			}
			all = append(all, found{lm, d})
		}
	}
	slices.SortStableFunc(all, func(a, b found) int {
		switch {
		case a.dist < b.dist:
			return -1
		case a.dist > b.dist:
			return 1
		}
		return strings.Compare(a.lm.Name, b.lm.Name)
	})
	out := make([]npcai.Landmark, 0, min(len(all), maxLandmarks))
	for i := 0; i < len(all) && i < maxLandmarks; i++ {
		out = append(out, all[i].lm)
	}
	if g.geo == nil || len(g.geo) >= maxGeoCacheLen {
		g.geo = map[geoKey][]npcai.Landmark{}
	}
	g.geo[key] = out
	return out
}

// neighboursGreeting is the greeting of a chief or mayor when the language model is unavailable:
// its three nearest neighbours, as the server knows them. "" if it knows none.
func (g *Game) neighboursGreeting(npc world.NPC, l i18n.Lang) string {
	lms := g.landmarksFor(npc, l)
	if len(lms) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, lm := range lms[:min(3, len(lms))] {
		parts = append(parts, fmt.Sprintf(i18n.T(l, "npc.neighbors.entry"), lm.Name, lm.Kind, lm.Direction, lm.Distance))
	}
	return i18n.T(l, "npc.neighbors.intro", strings.Join(parts, " ; "))
}

// landmarksIfKnown returns the neighbouring places for a chief or mayor, nothing for other NPCs.
func (g *Game) landmarksIfKnown(npc world.NPC, l i18n.Lang) []npcai.Landmark {
	if !knowsNeighbours(npc.Kind) {
		return nil
	}
	return g.landmarksFor(npc, l)
}

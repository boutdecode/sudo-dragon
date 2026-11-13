package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Where am I? The world is computed on both sides from the seed: the region and the place are
// deduced from the position, without any message from the server.

// placeLabel gives "village Tourelle" (kind then name) in the player's language.
func (m Model) placeLabel(s *world.Structure) string {
	return m.t("place.kind."+s.Kind.String()) + " " + s.NameIn(m.lang)
}

// mapTitle builds the map panel title: "region · place · terrain (x, y)". The least useful elements
// are dropped first when space is short (terrain, then region), never exceeding w.
func (m Model) mapTitle(x, y, w int) string {
	coords := fmt.Sprintf("(%d, %d)", x, y)
	level := m.t("region.level", world.LevelAt(x, y))
	terrain := render.TerrainName(m.lang, m.world.At(x, y))
	region := m.world.TerritoryAt(x, y).NameIn(m.lang)
	var place string
	if s := m.world.PlaceAt(x, y); s != nil {
		place = m.placeLabel(s)
	}
	join := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, " · ")
	}
	for _, t := range []string{
		join(region, level, place, terrain+" "+coords),
		join(region, level, place, coords),
		join(level, place, coords),
		join(region, place, coords),
		join(place, coords),
		join(region, coords),
		coords,
	} {
		if ansi.StringWidth(t) <= w {
			return t
		}
	}
	return ansi.Truncate(coords, w, "")
}

// locationTitle is the map panel title: the world's, or the dungeon's.
func (m Model) locationTitle(x, y, w int) string {
	if m.dmap != nil {
		return m.dungeonTitle(x, y, w)
	}
	return m.mapTitle(x, y, w)
}

// trackLocation announces in the log a change of region or place. The first position
// received is remembered silently.
func (m *Model) trackLocation() {
	if m.world == nil {
		return
	}
	self := m.snap.Self
	t := m.world.TerritoryAt(self.X, self.Y)
	s := m.world.PlaceAt(self.X, self.Y)
	if !m.locSeen {
		m.locSeen = true
		m.locTerritory, m.locInPlace = t.ID, s != nil
		m.locBand = world.LevelBand(world.LevelAt(self.X, self.Y))
		if s != nil {
			m.locPlace = s.Region
		}
		return
	}
	if t.ID != m.locTerritory {
		m.locTerritory = t.ID
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("region.entering", t.NameIn(m.lang), m.t("climate."+m.world.ClimateAt(self.X, self.Y).Key()))})
	}
	if lvl := world.LevelAt(self.X, self.Y); world.LevelBand(lvl) != m.locBand {
		up := world.LevelBand(lvl) > m.locBand
		m.locBand = world.LevelBand(lvl)
		lo, hi := world.LevelRange(lvl)
		key := "region.level_down"
		if up {
			key = "region.level_up"
		}
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t(key, lo, hi)})
	}
	switch {
	case s == nil:
		m.locInPlace = false
	case !m.locInPlace || s.Region != m.locPlace:
		m.locInPlace, m.locPlace = true, s.Region
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("place.arriving", m.t("place.kind."+s.Kind.String()), s.NameIn(m.lang))})
	}
}

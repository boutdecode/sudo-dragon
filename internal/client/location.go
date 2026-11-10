package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Où suis-je ? Le monde est calculé des deux côtés depuis la seed : la contrée et le lieu se
// déduisent de la position, sans aucun message du serveur.

// placeLabel donne « village Tourelle » (genre puis nom) dans la langue du joueur.
func (m Model) placeLabel(s *world.Structure) string {
	return m.t("place.kind."+s.Kind.String()) + " " + s.NameIn(m.lang)
}

// mapTitle compose le titre du panneau de carte : « contrée · lieu · terrain (x, y) ». Les éléments
// les moins utiles sautent d'abord quand la place manque (terrain, puis contrée), sans jamais dépasser w.
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

// locationTitle est le titre du panneau de carte: celui du monde, ou celui du donjon.
func (m Model) locationTitle(x, y, w int) string {
	if m.dmap != nil {
		return m.dungeonTitle(x, y, w)
	}
	return m.mapTitle(x, y, w)
}

// trackLocation annonce dans le journal un changement de contrée ou de lieu. La première position
// reçue est mémorisée sans bruit.
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
		m.addChat(chatLine{Channel: protocol.ChanSystem, Text: m.t("region.entering", t.NameIn(m.lang))})
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

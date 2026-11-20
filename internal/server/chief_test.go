package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

// chiefSpots returns the chief of the starting village and two free access tiles.
func chiefSpots(t *testing.T) (world.NPC, []spot) {
	t.Helper()
	w := testWorld(1)
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCChief {
				continue
			}
			var spots []spot
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if _, isNPC := w.NPCAt(p.X, p.Y); w.At(p.X, p.Y).Walkable() && !isNPC {
					spots = append(spots, spot{p, d[0], d[1]})
				}
			}
			if len(spots) >= 1 {
				return n, spots
			}
		}
	}
	t.Skip("no chief with a free access tile near the start")
	return world.NPC{}, nil
}

func TestChiefPromptNamesNearbyPlacesOnly(t *testing.T) {
	e := startTalkServer(t)
	chief, spots := chiefSpots(t)
	prompts := &seenPrompts{}
	e.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		prompts.add(req.Messages[0].Content)
		b, _ := json.Marshal(map[string]any{"reply": "Bienvenue.", "intro": nil, "memory": ""})
		return llm.Response{Content: string(b), Usage: llm.Usage{Total: 50}}, nil
	}

	w := testWorld(1)
	var near, far string
	for ry := chief.Region.Y - 7; ry <= chief.Region.Y+7; ry++ {
		for rx := chief.Region.X - 7; rx <= chief.Region.X+7; rx++ {
			rp := world.RegionPos{X: rx, Y: ry}
			s := w.StructureOfRegion(rp)
			if s == nil || rp == chief.Region {
				continue
			}
			d := max(abs(rx-chief.Region.X), abs(ry-chief.Region.Y))
			if d <= 3 && near == "" {
				near = s.NameIn(i18n.FR)
			}
			if d >= 6 && far == "" {
				far = s.NameIn(i18n.FR)
			}
		}
	}
	if near == "" || far == "" {
		t.Skip("no nearby place and distant place in this sample")
	}

	p := e.joinChar(t, "aldric", spots[0], store.Character{Level: 5, HP: 30, LLMConsent: true})
	menu := p.approach()
	if !hasOffer(menu, protocol.NPCTalk) || hasOffer(menu, protocol.NPCHeal) || hasOffer(menu, protocol.NPCBuy) {
		t.Fatalf("chief menu: %+v", menu.Offers)
	}
	p.startChat(false)
	if !prompts.any("lieux_connus") || !prompts.any(`"nom":"`+near+`"`) {
		t.Fatalf("the chief should know %q", near)
	}
	if prompts.any(`"nom":"` + far + `"`) {
		t.Fatalf("the chief knows %q, too far away", far)
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

var _ = strings.Contains

// worldview affiche une zone du monde en ASCII (outil de debug).
package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

// printTerritories dessine une carte grossière des contrées : une lettre par contrée, w × h cellules
// de step tuiles de côté centrées sur (cx, cy), puis la légende.
func printTerritories(wd *world.World, cx, cy, w, h, step int) {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	ids := map[world.TerritoryID]rune{}
	var order []*world.Territory
	var sb strings.Builder
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			t := wd.TerritoryAt(cx+(i-w/2)*step, cy+(j-h/2)*step)
			r, ok := ids[t.ID]
			if !ok {
				r = '?'
				if len(ids) < len(letters) {
					r = rune(letters[len(ids)])
				}
				ids[t.ID] = r
				order = append(order, t)
			}
			sb.WriteRune(r)
		}
		sb.WriteByte('\n')
	}
	fmt.Print(sb.String())
	fmt.Printf("une cellule = %d tuiles\n", step)
	for _, t := range order {
		fmt.Printf("  %c  %s  (germe %d,%d)\n", ids[t.ID], t.NameIn(i18n.FR), t.Site.X, t.Site.Y)
	}
}

func main() {
	seed := flag.Int64("seed", 1, "seed du monde")
	cx := flag.Int("x", 0, "centre X (tuiles)")
	cy := flag.Int("y", 0, "centre Y (tuiles)")
	w := flag.Int("w", 100, "largeur")
	h := flag.Int("h", 40, "hauteur")
	sites := flag.Bool("sites", false, "liste les structures de la zone et marque PNJ (M marchand, H soigneur) et garnisons (S soldat, K élite, B seigneur)")
	territories := flag.Bool("territories", false, "dessine les contrées (une lettre chacune, une cellule = -step tuiles) au lieu du terrain, puis les nomme")
	step := flag.Int("step", 40, "avec -territories : tuiles par cellule")
	flag.Parse()

	wd := world.New(*seed)
	x0, y0 := *cx-*w/2, *cy-*h/2

	if *territories {
		printTerritories(wd, *cx, *cy, *w, *h, max(1, *step))
		return
	}

	marks := map[world.Pos]rune{}
	if *sites {
		for _, s := range wd.StructuresNear(world.Pos{X: *cx, Y: *cy}, max(*w, *h)) {
			for _, n := range s.NPCs {
				marks[n.Pos] = map[world.NPCKind]rune{world.NPCMerchant: 'M', world.NPCHealer: 'H', world.NPCVillager: 'V', world.NPCChief: 'C', world.NPCMayor: 'C', world.NPCSmith: 'F', world.NPCArmorer: 'R'}[n.Kind]
			}
			for _, sl := range s.Slots {
				marks[sl.Pos] = map[world.SlotRole]rune{world.RoleGuard: 'S', world.RoleElite: 'K', world.RoleBoss: 'B'}[sl.Role]
			}
		}
	}

	var sb strings.Builder
	for y := y0; y < y0+*h; y++ {
		for x := x0; x < x0+*w; x++ {
			if m, ok := marks[world.Pos{X: x, Y: y}]; ok {
				sb.WriteRune(m)
				continue
			}
			sb.WriteRune(wd.At(x, y).Glyph())
		}
		sb.WriteByte('\n')
	}
	fmt.Print(sb.String())
	fmt.Printf("spawn: %+v\n", wd.FindSpawn())

	if *sites {
		fmt.Println("structures:")
		for _, s := range wd.StructuresNear(world.Pos{X: *cx, Y: *cy}, max(*w, *h)) {
			c := s.Center()
			fmt.Printf("  %-9s %-18s centre (%d,%d)  emprise %dx%d en (%d,%d)  PNJ: %d  garnison: %d  contrée: %s\n",
				s.Kind, s.Name, c.X, c.Y, s.W, s.H, s.Origin.X, s.Origin.Y, len(s.NPCs), len(s.Slots), wd.TerritoryAt(c.X, c.Y).NameIn(i18n.FR))
			for _, n := range s.NPCs {
				fmt.Printf("      %-9s %-16s %s; %s; %s\n", n.Kind, n.Name, n.Persona.Temperament, n.Persona.Quirk, n.Persona.Speech)
			}
		}
	}
}

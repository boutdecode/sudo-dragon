// worldview affiche une zone du monde en ASCII (outil de debug).
package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/world"
)

func main() {
	seed := flag.Int64("seed", 1, "seed du monde")
	cx := flag.Int("x", 0, "centre X (tuiles)")
	cy := flag.Int("y", 0, "centre Y (tuiles)")
	w := flag.Int("w", 100, "largeur")
	h := flag.Int("h", 40, "hauteur")
	sites := flag.Bool("sites", false, "liste les structures de la zone et marque PNJ (M marchand, H soigneur) et garnisons (S soldat, K élite, B seigneur)")
	flag.Parse()

	wd := world.New(*seed)
	x0, y0 := *cx-*w/2, *cy-*h/2

	marks := map[world.Pos]rune{}
	if *sites {
		for _, s := range wd.StructuresNear(world.Pos{X: *cx, Y: *cy}, max(*w, *h)) {
			for _, n := range s.NPCs {
				marks[n.Pos] = map[world.NPCKind]rune{world.NPCMerchant: 'M', world.NPCHealer: 'H'}[n.Kind]
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
			fmt.Printf("  %-9s %-18s centre (%d,%d)  emprise %dx%d en (%d,%d)  PNJ: %d  garnison: %d\n",
				s.Kind, s.Name, c.X, c.Y, s.W, s.H, s.Origin.X, s.Origin.Y, len(s.NPCs), len(s.Slots))
			for _, n := range s.NPCs {
				fmt.Printf("      %-9s %-16s %s; %s; %s\n", n.Kind, n.Name, n.Persona.Temperament, n.Persona.Quirk, n.Persona.Speech)
			}
		}
	}
}

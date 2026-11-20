// worldview prints an area of the world in ASCII (debug tool).
package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/version"
	"github.com/kevin/ascii-adventure/internal/world"
)

// printTerritories draws a coarse map of the territories: one letter per territory, w × h cells
// of step tiles per side centered on (cx, cy), then the legend.
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
	fmt.Printf("one cell = %d tiles\n", step)
	for _, t := range order {
		fmt.Printf("  %c  %s  (seed %d,%d)\n", ids[t.ID], t.NameIn(i18n.FR), t.Site.X, t.Site.Y)
	}
}

func main() {
	seed := flag.Int64("seed", 1, "world seed")
	cx := flag.Int("x", 0, "center X (tiles)")
	cy := flag.Int("y", 0, "center Y (tiles)")
	w := flag.Int("w", 100, "width")
	h := flag.Int("h", 40, "height")
	sites := flag.Bool("sites", false, "list the structures of the area and mark NPCs (M merchant, H healer) and garrisons (S soldier, K elite, B lord)")
	territories := flag.Bool("territories", false, "draw the territories (one letter each, one cell = -step tiles) instead of the terrain, then name them")
	step := flag.Int("step", 40, "with -territories: tiles per cell")
	zoom := flag.Int("zoom", 1, "with -raw: tiles per cell (terrain overview)")
	raw := flag.Bool("raw", false, "raw terrain only (no structures or roads), sampled every -zoom tiles")
	showVersion := flag.Bool("version", false, "print the version, then exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("sudo-dragon-worldview", version.String())
		return
	}

	wd := world.New(*seed)
	x0, y0 := *cx-*w/2, *cy-*h/2

	if *territories {
		printTerritories(wd, *cx, *cy, *w, *h, max(1, *step))
		return
	}

	if *raw {
		z := max(1, *zoom)
		var sb strings.Builder
		counts := map[world.Tile]int{}
		for j := 0; j < *h; j++ {
			for i := 0; i < *w; i++ {
				t := wd.Terrain(*cx+(i-*w/2)*z, *cy+(j-*h/2)*z)
				counts[t]++
				sb.WriteRune(t.Glyph())
			}
			sb.WriteByte('\n')
		}
		fmt.Print(sb.String())
		total := float64(*w * *h)
		for t := world.Tile(0); t < world.NumTiles; t++ {
			if counts[t] == 0 {
				continue
			}
			fmt.Printf("%c %.1f%%  ", t.Glyph(), 100*float64(counts[t])/total)
		}
		fmt.Println()
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
			fmt.Printf("  %-9s %-18s center (%d,%d)  footprint %dx%d at (%d,%d)  NPCs: %d  garrison: %d  territory: %s\n",
				s.Kind, s.Name, c.X, c.Y, s.W, s.H, s.Origin.X, s.Origin.Y, len(s.NPCs), len(s.Slots), wd.TerritoryAt(c.X, c.Y).NameIn(i18n.FR))
			for _, n := range s.NPCs {
				fmt.Printf("      %-9s %-16s %s; %s; %s\n", n.Kind, n.Name, n.Persona.Temperament, n.Persona.Quirk, n.Persona.Speech)
			}
		}
	}
}

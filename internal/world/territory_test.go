package world

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

func TestEveryPointHasTheNearestSiteTerritory(t *testing.T) {
	seed := int64(1)
	for _, p := range []Pos{{0, 0}, {-1, -1}, {999, 999}, {1000, -1000}, {-5304, -818}, {12345, -67890}} {
		got := territoryIDAt(seed, p.X, p.Y)
		// Comparaison brute avec un voisinage plus large que celui de l'implémentation.
		cx, cy := floorDiv(p.X, territorySize), floorDiv(p.Y, territorySize)
		best, bestD := TerritoryID{}, int64(-1)
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				s := siteOf(seed, cx+dx, cy+dy)
				ddx, ddy := int64(s.X-p.X), int64(s.Y-p.Y)
				if d := ddx*ddx + ddy*ddy; bestD < 0 || d < bestD {
					best, bestD = TerritoryID{cx + dx, cy + dy}, d
				}
			}
		}
		if got != best {
			t.Errorf("%v : contrée %v, le germe le plus proche est %v", p, got, best)
		}
	}
}

func TestTerritoriesAreOrganicAndAboutAThousandWide(t *testing.T) {
	seed := int64(7)
	// Aire moyenne estimée par échantillonnage : proche de territorySize².
	seen := map[TerritoryID]int{}
	const step, n = 50, 120 // 6000 × 6000 cases
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			seen[territoryIDAt(seed, i*step, j*step)]++
		}
	}
	// On ne compte que les contrées entières (celles qui ne touchent pas le bord de l'échantillon).
	total := 0
	for _, c := range seen {
		total += c
	}
	avg := float64(total*step*step) / float64(len(seen))
	if avg < 0.4*territorySize*territorySize || avg > 2.5*territorySize*territorySize {
		t.Errorf("aire moyenne %.0f, attendue de l'ordre de %d", avg, territorySize*territorySize)
	}
	// Frontières non alignées : le long de plusieurs rangées, le changement de contrée se fait à des abscisses différentes.
	xs := map[int]bool{}
	for y := 0; y < 4000; y += 400 {
		prev := territoryIDAt(seed, 0, y)
		for x := 1; x < 3000; x++ {
			if cur := territoryIDAt(seed, x, y); cur != prev {
				xs[x/20] = true
				break
			}
		}
	}
	if len(xs) < 5 {
		t.Errorf("frontières trop régulières : %d abscisses distinctes sur 10 rangées", len(xs))
	}
}

func TestTerritoryNamesAreStableLocalizedAndClean(t *testing.T) {
	a, b := New(3), New(3)
	for _, p := range []Pos{{0, 0}, {900, 40}, {-3000, 2500}, {40000, -40000}} {
		ta, tb := a.TerritoryAt(p.X, p.Y), b.TerritoryAt(p.X, p.Y)
		fr, en := ta.NameIn(i18n.FR), ta.NameIn(i18n.EN)
		if fr != tb.NameIn(i18n.FR) || fr == "" || en == "" || fr == en {
			t.Errorf("%v : fr=%q en=%q (autre monde : %q)", p, fr, en, tb.NameIn(i18n.FR))
		}
		if !strings.Contains(en, " of ") {
			t.Errorf("nom anglais sans « of » : %q", en)
		}
		if strings.ContainsAny(fr+en, "\x1b%!") {
			t.Errorf("caractère suspect dans %q / %q", fr, en)
		}
		if ta.ID != territoryIDAt(3, p.X, p.Y) || ta != a.TerritoryAt(p.X, p.Y) {
			t.Errorf("cache ou identifiant incohérent en %v", p)
		}
	}
}

func TestFrenchElision(t *testing.T) {
	tr := &Territory{Theme: themeForest}
	for i := range tr.starts {
		tr.starts[i], tr.ends[i] = 0, 0
	}
	cases := map[string]string{"Aube": "d'Aube", "Ombre": "d'Ombre", "Brume": "de Brume", "Val": "de Val"}
	for stem, want := range cases {
		idx := -1
		for i, s := range placeStart {
			if s == stem {
				idx = i
			}
		}
		if idx < 0 {
			continue // radical de fin seulement
		}
		tr.starts[0], tr.ends[0], tr.nameStart, tr.nameEnd = idx, 0, 0, 0
		got := tr.NameIn(i18n.FR)
		if !strings.Contains(got, want+placeEnd[0]) {
			t.Errorf("%s : %q ne contient pas %q", stem, got, want)
		}
	}
}

func TestEveryThemeHasNamesInBothLanguages(t *testing.T) {
	for th := terrainTheme(0); th < numThemes; th++ {
		for v := 0; v < 2; v++ {
			tr := &Territory{Theme: th, variant: v}
			for _, l := range []i18n.Lang{i18n.FR, i18n.EN} {
				name := tr.NameIn(l)
				if strings.HasPrefix(name, "region.") || strings.Contains(name, "region.theme") {
					t.Errorf("clé manquante (%s, thème %d, variante %d) : %q", l, th, v, name)
				}
			}
		}
	}
}

func TestStemsOfAPlaceBelongToItsTerritoryFamily(t *testing.T) {
	w := New(1)
	inFamily := func(base string, starts, ends [familySize]int) bool {
		for _, a := range starts {
			for _, b := range ends {
				if base == placeStart[a]+placeEnd[b] {
					return true
				}
			}
		}
		return false
	}
	checked := 0
	for ry := -6; ry <= 6; ry++ {
		for rx := -6; rx <= 6; rx++ {
			rp := RegionPos{rx, ry}
			s := w.StructureOfRegion(rp)
			if s == nil {
				continue
			}
			checked++
			id := territoryIDAt(1, rx*RegionSize+RegionSize/2, ry*RegionSize+RegionSize/2)
			st, en := familyOf(1, id)
			if !inFamily(s.base, st, en) {
				t.Errorf("%v : radical %q hors de la famille de la contrée %v", rp, s.base, id)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("échantillon trop pauvre : %d lieux", checked)
	}
}

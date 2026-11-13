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
		// Brute-force comparison with a wider neighbourhood than the implementation's.
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
			t.Errorf("%v : district %v, the nearest seed is %v", p, got, best)
		}
	}
}

func TestTerritoriesAreOrganicAndAboutAThousandWide(t *testing.T) {
	seed := int64(7)
	// Mean area estimated by sampling: close to territorySize².
	seen := map[TerritoryID]int{}
	const step, n = 50, 120 // 6000 x 6000 tiles
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			seen[territoryIDAt(seed, i*step, j*step)]++
		}
	}
	// Only whole districts are counted (those that do not touch the edge of the sample).
	total := 0
	for _, c := range seen {
		total += c
	}
	avg := float64(total*step*step) / float64(len(seen))
	if avg < 0.4*territorySize*territorySize || avg > 2.5*territorySize*territorySize {
		t.Errorf("mean area %.0f, expected of the order of %d", avg, territorySize*territorySize)
	}
	// Non-aligned borders: along several rows, the change of district happens at different x positions.
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
		t.Errorf("borders too regular: %d distinct x positions over 10 rows", len(xs))
	}
}

func TestTerritoryNamesAreStableLocalizedAndClean(t *testing.T) {
	a, b := newTestWorld(3), newTestWorld(3)
	for _, p := range []Pos{{0, 0}, {900, 40}, {-3000, 2500}, {40000, -40000}} {
		ta, tb := a.TerritoryAt(p.X, p.Y), b.TerritoryAt(p.X, p.Y)
		fr, en := ta.NameIn(i18n.FR), ta.NameIn(i18n.EN)
		if fr != tb.NameIn(i18n.FR) || fr == "" || en == "" || fr == en {
			t.Errorf("%v : fr=%q en=%q (other world: %q)", p, fr, en, tb.NameIn(i18n.FR))
		}
		if !strings.Contains(en, " of ") {
			t.Errorf("English name without \"of\": %q", en)
		}
		if strings.ContainsAny(fr+en, "\x1b%!") {
			t.Errorf("suspicious character in %q / %q", fr, en)
		}
		if ta.ID != territoryIDAt(3, p.X, p.Y) || ta != a.TerritoryAt(p.X, p.Y) {
			t.Errorf("inconsistent cache or identifier at %v", p)
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
			continue // end stem only
		}
		tr.starts[0], tr.ends[0], tr.nameStart, tr.nameEnd = idx, 0, 0, 0
		got := tr.NameIn(i18n.FR)
		if !strings.Contains(got, want+placeEnd[0]) {
			t.Errorf("%s : %q does not contain %q", stem, got, want)
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
					t.Errorf("missing key (%s, theme %d, variant %d): %q", l, th, v, name)
				}
			}
		}
	}
}

func TestStemsOfAPlaceBelongToItsTerritoryFamily(t *testing.T) {
	w := newTestWorld(1)
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
				t.Errorf("%v : stem %q outside the family of district %v", rp, s.base, id)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("sample too poor: %d places", checked)
	}
}

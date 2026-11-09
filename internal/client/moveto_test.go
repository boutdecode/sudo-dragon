package client

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/world"
)

func TestFindPathAroundWall(t *testing.T) {
	// Mur vertical en x=2 sauf une ouverture en y=5.
	free := func(p world.Pos) bool {
		if p.X < -3 || p.X > 8 || p.Y < -3 || p.Y > 8 {
			return false
		}
		return !(p.X == 2 && p.Y != 5)
	}
	path := findPath(world.Pos{X: 0, Y: 0}, world.Pos{X: 4, Y: 0}, free, 1000)
	if path == nil {
		t.Fatal("chemin attendu")
	}
	if path[len(path)-1] != (world.Pos{X: 4, Y: 0}) {
		t.Fatalf("mauvaise arrivée: %v", path)
	}
	prev := world.Pos{}
	for _, p := range path {
		if !free(p) || chebyshev(prev, p) != 1 {
			t.Fatalf("pas invalide %v -> %v", prev, p)
		}
		prev = p
	}
}

func TestFindPathUnreachable(t *testing.T) {
	free := func(p world.Pos) bool { return p.X != 3 || p.Y == 99 }
	if findPath(world.Pos{}, world.Pos{X: 5}, free, 500) != nil {
		t.Fatal("aucun chemin attendu (plafond de nœuds)")
	}
	if findPath(world.Pos{}, world.Pos{X: 3}, free, 500) != nil {
		t.Fatal("cible occupée: aucun chemin")
	}
}

func TestParseMoveto(t *testing.T) {
	cases := []struct {
		in      string
		cmd, ok bool
		x, y    int
	}{
		{"/moveto 3 -4", true, true, 3, -4},
		{"/MOVETO 1 2", true, true, 1, 2},
		{"/moveto a 2", true, false, 0, 0},
		{"/moveto 1", true, false, 0, 0},
		{"bonjour", false, false, 0, 0},
		{"/g hello", false, false, 0, 0},
	}
	for _, c := range cases {
		p, cmd, ok := parseMoveto(c.in)
		if cmd != c.cmd || ok != c.ok || (ok && (p.X != c.x || p.Y != c.y)) {
			t.Errorf("%q: got %v %v %v", c.in, p, cmd, ok)
		}
	}
}

// Une route parallèle à la ligne droite vaut un petit détour; une route trop
// éloignée n'en vaut pas.
func TestFindPathCostPrefersRoad(t *testing.T) {
	free := func(p world.Pos) bool { return p.X >= -1 && p.X <= 40 && p.Y >= -10 && p.Y <= 10 }
	road := func(y int) func(world.Pos) int {
		return func(p world.Pos) int {
			if p.Y == y {
				return roadStepCost
			}
			return offRoadCost
		}
	}
	from, to := world.Pos{X: 0, Y: 0}, world.Pos{X: 30, Y: 0}
	near := findPathCost(from, to, free, road(3), roadStepCost, 60000)
	onRoad := 0
	for _, p := range near {
		if p.Y == 3 {
			onRoad++
		}
	}
	if onRoad < 20 {
		t.Fatalf("la route proche devrait être suivie (%d cases sur la route)", onRoad)
	}
	far := findPathCost(from, to, free, road(9), roadStepCost, 60000)
	if len(far) != 30 {
		t.Fatalf("une route trop éloignée ne doit pas causer de détour (len=%d)", len(far))
	}
}

// Une longue marche sans aucune route doit quand même aboutir.
func TestFindWalkPathLongOffRoad(t *testing.T) {
	m := Model{world: world.New(1)}
	free := func(p world.Pos) bool { return p.X >= -5 && p.X <= 400 && p.Y >= -5 && p.Y <= 400 }
	if m.findWalkPath(world.Pos{}, world.Pos{X: 300, Y: 300}, free) == nil {
		t.Fatal("marche hors route de 300 cases introuvable")
	}
}

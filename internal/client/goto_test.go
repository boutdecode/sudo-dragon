package client

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/world"
)

func TestFindPathAroundWall(t *testing.T) {
	// Vertical wall at x=2 except an opening at y=5.
	free := func(p world.Pos) bool {
		if p.X < -3 || p.X > 8 || p.Y < -3 || p.Y > 8 {
			return false
		}
		return !(p.X == 2 && p.Y != 5)
	}
	path := findPath(world.Pos{X: 0, Y: 0}, world.Pos{X: 4, Y: 0}, free, 1000)
	if path == nil {
		t.Fatal("path expected")
	}
	if path[len(path)-1] != (world.Pos{X: 4, Y: 0}) {
		t.Fatalf("wrong arrival: %v", path)
	}
	prev := world.Pos{}
	for _, p := range path {
		if !free(p) || chebyshev(prev, p) != 1 {
			t.Fatalf("invalid step %v -> %v", prev, p)
		}
		prev = p
	}
}

func TestFindPathUnreachable(t *testing.T) {
	free := func(p world.Pos) bool { return p.X != 3 || p.Y == 99 }
	if findPath(world.Pos{}, world.Pos{X: 5}, free, 500) != nil {
		t.Fatal("no path expected (node limit)")
	}
	if findPath(world.Pos{}, world.Pos{X: 3}, free, 500) != nil {
		t.Fatal("occupied target: no path")
	}
}

func TestParseGoto(t *testing.T) {
	cases := []struct {
		in      string
		cmd, ok bool
		x, y    int
	}{
		{"/goto 3 -4", true, true, 3, -4},
		{"/GOTO 1 2", true, true, 1, 2},
		{"/goto a 2", true, false, 0, 0},
		{"/goto 1", true, false, 0, 0},
		{"bonjour", false, false, 0, 0},
		{"/g hello", false, false, 0, 0},
	}
	for _, c := range cases {
		p, cmd, ok := parseGoto(c.in)
		if cmd != c.cmd || ok != c.ok || (ok && (p.X != c.x || p.Y != c.y)) {
			t.Errorf("%q: got %v %v %v", c.in, p, cmd, ok)
		}
	}
}

// A road parallel to the straight line is worth a small detour; a road too
// far away is not.
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
		t.Fatalf("the nearby road should be followed (%d tiles on the road)", onRoad)
	}
	far := findPathCost(from, to, free, road(9), roadStepCost, 60000)
	if len(far) != 30 {
		t.Fatalf("a road too far away must not cause a detour (len=%d)", len(far))
	}
}

// A long walk without any road must still succeed.
func TestFindWalkPathLongOffRoad(t *testing.T) {
	m := Model{world: testWorld(1)}
	free := func(p world.Pos) bool { return p.X >= -5 && p.X <= 400 && p.Y >= -5 && p.Y <= 400 }
	if m.findWalkPath(world.Pos{}, world.Pos{X: 300, Y: 300}, free) == nil {
		t.Fatal("300-tile off-road walk not found")
	}
}

func TestCompleteGoto(t *testing.T) {
	cands := []string{"Puits", "Village Foo", "Village Fou", "mon camp"}
	cases := []struct {
		in, out string
		n       int
	}{
		{"/goto pu", "/goto Puits", 1},
		{"/GOTO MON", "/GOTO mon camp", 1},
		{"/goto vill", "/goto Village Fo", 2},
		{"/goto Village Fo", "/goto Village Fo", 2},
		{"/goto zzz", "/goto zzz", 0},
		{"/goto 3 4", "/goto 3 4", 0},
		{"hello pu", "hello pu", 0},
	}
	for _, c := range cases {
		out, matches := completeGoto(c.in, cands)
		if out != c.out || len(matches) != c.n {
			t.Errorf("%q -> %q (%d), attendu %q (%d)", c.in, out, len(matches), c.out, c.n)
		}
	}
}

func TestChatHistory(t *testing.T) {
	m := playingModel(t, 100, 30)
	m.pushHistory("/goto Puits")
	m.pushHistory("bonjour")
	m.pushHistory("bonjour") // consecutive duplicate ignored
	m.chatInput.SetValue("brouillon")
	m.recallHistory(-1)
	if v := m.chatInput.Value(); v != "bonjour" {
		t.Fatalf("up: %q", v)
	}
	m.recallHistory(-1)
	if v := m.chatInput.Value(); v != "/goto Puits" {
		t.Fatalf("up x2: %q", v)
	}
	m.recallHistory(-1) // at the oldest: unchanged
	if v := m.chatInput.Value(); v != "/goto Puits" {
		t.Fatalf("upper limit: %q", v)
	}
	m.recallHistory(+1)
	m.recallHistory(+1)
	if v := m.chatInput.Value(); v != "brouillon" {
		t.Fatalf("back to the draft: %q", v)
	}
}

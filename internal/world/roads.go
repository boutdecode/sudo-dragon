package world

import (
	"container/heap"
	"fmt"
)

// Roads between places.
//
// Two neighbouring regions (side by side, not diagonal) that each contain
// a structure are linked by a road. The route is an A* over the raw terrain
// (terrainAt), confined to the two regions: so a region depends only on
// its four neighbours, and the A<->B link is the same whatever the generation
// order. Roads never touch the footprint of a structure (structures
// stay frozen); they stop at its edge, on the clear terrain
// around it, and join its inner path.
//
// A road crosses shallow water (a bridge: the tile is simply Road),
// but never deep water or mountains.

// roadKey identifies a link; a is always the west or north region.
type roadKey struct{ a, b RegionPos }

// roadLink is the route of a link (nil if there is none).
type roadLink struct {
	tiles map[Pos]struct{}
	path  []Pos // the same tiles in order, from region a to region b (see RoadRoutesNear)
	key   roadKey
}

// roadCacheMax bounds the road cache; beyond it, it is emptied and rebuilt.
const roadCacheMax = 2048

// roadCost is the cost of a terrain tile for the route (0: impassable).
func roadCost(t Tile) int {
	switch t {
	case Plain, Sand:
		return 1
	case Forest:
		return 3
	case Hills:
		return 4
	case Snow:
		return 6
	case Water:
		return 10
	case River:
		return 5
	case Tundra, Desert:
		return 2
	case Swamp:
		return 3
	case Jungle:
		return 4
	}
	return 0
}

// roadCostOf is roadCost for this world. A road no longer crosses water (sea, lakes, ponds):
// only rivers can be crossed, by a bridge (ports will come later).
func (w *World) roadCostOf(t Tile) int {
	if t == Water {
		return 0
	}
	return roadCost(t)
}

// roadsAround returns the links touching region rp (at most four).
func (w *World) roadsAround(rp RegionPos) []*roadLink {
	w.rmu.Lock()
	defer w.rmu.Unlock()
	var out []*roadLink
	for _, n := range [4]RegionPos{
		{rp.X - 1, rp.Y}, {rp.X + 1, rp.Y}, {rp.X, rp.Y - 1}, {rp.X, rp.Y + 1},
	} {
		key := roadKey{rp, n}
		if n.X < rp.X || n.Y < rp.Y {
			key = roadKey{n, rp}
		}
		l, ok := w.roads[key]
		if !ok {
			if len(w.roads) >= roadCacheMax {
				clear(w.roads)
			}
			l = w.buildRoad(key)
			w.roads[key] = l
		}
		if l != nil {
			out = append(out, l)
		}
	}
	return out
}

// onRoad tells whether (x, y) belongs to one of the links.
func onRoad(links []*roadLink, x, y int) bool {
	p := Pos{x, y}
	for _, l := range links {
		if _, ok := l.tiles[p]; ok {
			return true
		}
	}
	return false
}

// outsideReach returns the footprint tiles reachable from the outside
// (through Plain or Road tiles), and the possible anchor points:
// the paths for a village or a town, the tiles in front of a door for a
// fortress.
func (s *Structure) outsideReach() (reach map[Pos]bool, anchors []Pos) {
	reach = make(map[Pos]bool)
	open := func(p Pos) bool {
		if !s.Contains(p.X, p.Y) || reach[p] {
			return false
		}
		t := s.TileAt(p.X, p.Y)
		return t == Plain || t == Road
	}
	var queue []Pos
	for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
		for _, y := range [2]int{s.Origin.Y, s.Origin.Y + s.H - 1} {
			if p := (Pos{x, y}); open(p) {
				reach[p] = true
				queue = append(queue, p)
			}
		}
	}
	for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
		for _, x := range [2]int{s.Origin.X, s.Origin.X + s.W - 1} {
			if p := (Pos{x, y}); open(p) {
				reach[p] = true
				queue = append(queue, p)
			}
		}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if q := (Pos{p.X + d[0], p.Y + d[1]}); open(q) {
				reach[q] = true
				queue = append(queue, q)
			}
		}
	}
	for p := range reach {
		if s.Kind != KindFortress {
			if s.TileAt(p.X, p.Y) == Road {
				anchors = append(anchors, p)
			}
			continue
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if q := (Pos{p.X + d[0], p.Y + d[1]}); s.Contains(q.X, q.Y) && s.TileAt(q.X, q.Y) == Door {
				anchors = append(anchors, p)
				break
			}
		}
	}
	return reach, anchors
}

// pickAnchor chooses, among the anchor points, the one closest to to
// (and, on a tie, the smallest, to stay deterministic).
func pickAnchor(reach map[Pos]bool, anchors []Pos, to Pos) (Pos, bool) {
	if len(anchors) == 0 {
		for p := range reach {
			anchors = append(anchors, p)
		}
	}
	best, bestD, ok := Pos{}, 0, false
	for _, p := range anchors {
		d := abs(p.X-to.X) + abs(p.Y-to.Y)
		if !ok || d < bestD || (d == bestD && (p.Y < best.Y || (p.Y == best.Y && p.X < best.X))) {
			best, bestD, ok = p, d, true
		}
	}
	return best, ok
}

// buildRoad draws the link between two neighbouring regions, or returns nil. Called
// with rmu held.
func (w *World) buildRoad(key roadKey) *roadLink {
	sa, sb := w.structureForRegion(key.a), w.structureForRegion(key.b)
	if sa == nil || sb == nil {
		return nil
	}
	reachA, anchorsA := sa.outsideReach()
	reachB, anchorsB := sb.outsideReach()
	from, ok1 := pickAnchor(reachA, anchorsA, sb.Center())
	to, ok2 := pickAnchor(reachB, anchorsB, sa.Center())
	if !ok1 || !ok2 {
		return nil
	}

	// Search box: the two regions.
	x0, y0 := key.a.X*RegionSize, key.a.Y*RegionSize
	bw, bh := RegionSize, RegionSize
	if key.b.X != key.a.X {
		bw *= 2
	} else {
		bh *= 2
	}

	// A road never touches a point of interest: it goes around its footprint and a tile of margin (see poi.go). Only
	// the bounds count (first phase): the result does not depend on the door, which itself depends on the roads.
	var poiRects []Rect
	for _, rp := range [2]RegionPos{key.a, key.b} {
		for _, p := range w.POIsOfRegion(rp) {
			poiRects = append(poiRects, p.Bounds.grow(1))
		}
	}
	terrain := make([]int8, bw*bh) // 0: not computed yet, -1: impassable
	cost := func(x, y int) int {
		p := Pos{x, y}
		switch {
		case sa.Contains(x, y):
			if reachA[p] {
				return 1
			}
			return 0
		case sb.Contains(x, y):
			if reachB[p] {
				return 1
			}
			return 0
		}
		for _, pr := range poiRects {
			if pr.Contains(x, y) {
				return 0
			}
		}
		i := (y-y0)*bw + (x - x0)
		if terrain[i] == 0 {
			if c := w.roadCostOf(w.terrainAt(x, y)); c > 0 {
				terrain[i] = int8(c)
			} else {
				terrain[i] = -1
			}
		}
		return max(int(terrain[i]), 0)
	}

	path := astarRoad(from, to, x0, y0, bw, bh, cost)
	if path == nil {
		return nil
	}
	l := &roadLink{tiles: make(map[Pos]struct{}, len(path)), path: make([]Pos, 0, len(path)), key: key}
	for _, p := range path {
		if !sa.Contains(p.X, p.Y) && !sb.Contains(p.X, p.Y) {
			l.tiles[p] = struct{}{}
			l.path = append(l.path, p)
		}
	}
	return l
}

type roadNode struct {
	pos  Pos
	f, g int
}

type roadHeap []roadNode

func (h roadHeap) Len() int { return len(h) }
func (h roadHeap) Less(i, j int) bool {
	if h[i].f != h[j].f {
		return h[i].f < h[j].f
	}
	return h[i].g > h[j].g // on equal f, favour the most advanced
}
func (h roadHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *roadHeap) Push(x any)   { *h = append(*h, x.(roadNode)) }
func (h *roadHeap) Pop() any {
	old := *h
	n := old[len(old)-1]
	*h = old[:len(old)-1]
	return n
}

// astarRoad looks for the least-cost path (4 directions) from from to to within
// the box (x0, y0, bw, bh). cost returns 0 for an impassable tile.
func astarRoad(from, to Pos, x0, y0, bw, bh int, cost func(x, y int) int) []Pos {
	idx := func(p Pos) int { return (p.Y-y0)*bw + (p.X - x0) }
	inBox := func(p Pos) bool { return p.X >= x0 && p.X < x0+bw && p.Y >= y0 && p.Y < y0+bh }
	if !inBox(from) || !inBox(to) {
		return nil
	}
	const unseen = 1 << 30
	g := make([]int32, bw*bh)
	for i := range g {
		g[i] = unseen
	}
	prev := make([]int32, bw*bh)
	closed := make([]bool, bw*bh)
	h := func(p Pos) int { return abs(p.X-to.X) + abs(p.Y-to.Y) }

	open := &roadHeap{}
	g[idx(from)] = 0
	prev[idx(from)] = -1
	heap.Push(open, roadNode{from, h(from), 0})
	for open.Len() > 0 {
		n := heap.Pop(open).(roadNode)
		i := idx(n.pos)
		if closed[i] {
			continue
		}
		closed[i] = true
		if n.pos == to {
			var path []Pos
			for j := int32(i); j >= 0; j = prev[j] {
				path = append(path, Pos{x0 + int(j)%bw, y0 + int(j)/bw})
			}
			return path
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := Pos{n.pos.X + d[0], n.pos.Y + d[1]}
			if !inBox(q) {
				continue
			}
			qi := idx(q)
			if closed[qi] {
				continue
			}
			c := cost(q.X, q.Y)
			if c == 0 {
				continue
			}
			ng := int(g[i]) + c
			if ng < int(g[qi]) {
				g[qi] = int32(ng)
				prev[qi] = int32(i)
				heap.Push(open, roadNode{q, ng + h(q), ng})
			}
		}
	}
	return nil
}

// RoadTilesIn calls fn for each road tile between regions whose
// coordinates are within [x0, x1] x [y0, y1]. Used by the world map, where sparse
// sampling would miss roads one tile wide.
func (w *World) RoadTilesIn(x0, y0, x1, y1 int, fn func(Pos)) {
	seen := make(map[*roadLink]bool)
	a, b := RegionOf(x0, y0), RegionOf(x1, y1)
	for ry := a.Y; ry <= b.Y; ry++ {
		for rx := a.X; rx <= b.X; rx++ {
			for _, l := range w.roadsAround(RegionPos{rx, ry}) {
				if seen[l] {
					continue
				}
				seen[l] = true
				for p := range l.tiles {
					if p.X >= x0 && p.X <= x1 && p.Y >= y0 && p.Y <= y1 {
						fn(p)
					}
				}
			}
		}
	}
}

// RoadRoute is a road link between two neighbouring places, with its route in order.
type RoadRoute struct {
	ID   string    // stable identifier "ax,ay>bx,by"
	A, B RegionPos // A is the west or north region
	Path []Pos     // route tiles outside the footprint of the places, from A to B; do not modify

	// Orient is 0 for an east-west link and 1 for a north-south link: with A, it tells apart the two
	// links leaving the same region.
	Orient int
}

// RoadRoutesNear returns the road links one of whose tiles is at most r tiles (Chebyshev) from (x, y).
func (w *World) RoadRoutesNear(x, y, r int) []RoadRoute {
	seen := make(map[*roadLink]bool)
	lo, hi := RegionOf(x-r, y-r), RegionOf(x+r, y+r)
	var out []RoadRoute
	for ry := lo.Y; ry <= hi.Y; ry++ {
		for rx := lo.X; rx <= hi.X; rx++ {
			for _, l := range w.roadsAround(RegionPos{rx, ry}) {
				if seen[l] || len(l.path) < 2 {
					continue
				}
				seen[l] = true
				near := false
				for _, p := range l.path {
					if abs(p.X-x) <= r && abs(p.Y-y) <= r {
						near = true
						break
					}
				}
				if !near {
					continue
				}
				orient := 0
				if l.key.b.X == l.key.a.X {
					orient = 1
				}
				out = append(out, RoadRoute{
					ID: fmt.Sprintf("%d,%d>%d,%d", l.key.a.X, l.key.a.Y, l.key.b.X, l.key.b.Y),
					A:  l.key.a, B: l.key.b, Path: l.path, Orient: orient,
				})
			}
		}
	}
	return out
}

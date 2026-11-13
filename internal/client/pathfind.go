package client

import (
	"container/heap"

	"github.com/kevin/ascii-adventure/internal/world"
)

type pathNode struct {
	pos  world.Pos
	g, f int
}

type pathHeap []pathNode

func (h pathHeap) Len() int { return len(h) }
func (h pathHeap) Less(i, j int) bool {
	return h[i].f < h[j].f || (h[i].f == h[j].f && h[i].g > h[j].g)
}
func (h pathHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *pathHeap) Push(x any)   { *h = append(*h, x.(pathNode)) }
func (h *pathHeap) Pop() any     { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

func chebyshev(a, b world.Pos) int { return max(absInt(a.X-b.X), absInt(a.Y-b.Y)) }

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// findPath looks for a path from from to to (8 directions, like the keyboard).
// free tells whether a tile is walkable. The result excludes from and ends
// with to; nil if no path was found within the maxNodes limit.
// As on the server, diagonals are not blocked by corners.
func findPath(from, to world.Pos, free func(world.Pos) bool, maxNodes int) []world.Pos {
	return findPathCost(from, to, free, func(world.Pos) int { return 1 }, 1, maxNodes)
}

// Walking costs of /goto: a road tile costs less than an off-road tile,
// so the path takes a detour (up to 1.5 times longer) to follow it.
const (
	roadStepCost = 2
	offRoadCost  = 3
	walkMaxNodes = 60000 // the heuristic is looser with costs: explore more
	// fallbackMaxNodes bounds the search without road preference, used
	// when the weighted search fails (long off-road walks).
	fallbackMaxNodes = 250000
)

// findWalkPath looks for a /goto path that prefers roads.
func (m Model) findWalkPath(from, to world.Pos, free func(world.Pos) bool) []world.Pos {
	cost := func(p world.Pos) int {
		if m.dmap == nil && m.world.At(p.X, p.Y) == world.Road {
			return roadStepCost
		}
		return offRoadCost
	}
	if path := findPathCost(from, to, free, cost, roadStepCost, walkMaxNodes); path != nil {
		return path
	}
	// Budget exceeded (long off-road walk): shortest path, ignoring roads.
	return findPath(from, to, free, fallbackMaxNodes)
}

// findPathCost is findPath with a per-tile entry cost. minStep is the minimal
// cost of one step (it keeps the heuristic admissible).
func findPathCost(from, to world.Pos, free func(world.Pos) bool, cost func(world.Pos) int, minStep, maxNodes int) []world.Pos {
	if from == to || !free(to) {
		return nil
	}
	best := map[world.Pos]int{from: 0}
	prev := map[world.Pos]world.Pos{}
	h := &pathHeap{{from, 0, minStep * chebyshev(from, to)}}
	for h.Len() > 0 && len(best) <= maxNodes {
		cur := heap.Pop(h).(pathNode)
		if cur.pos == to {
			var path []world.Pos
			for p := to; p != from; p = prev[p] {
				path = append(path, p)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		if cur.g > best[cur.pos] {
			continue // stale entry
		}
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}
				n := world.Pos{X: cur.pos.X + dx, Y: cur.pos.Y + dy}
				if !free(n) {
					continue
				}
				g := cur.g + cost(n)
				if old, seen := best[n]; seen && old <= g {
					continue
				}
				best[n] = g
				prev[n] = cur.pos
				heap.Push(h, pathNode{n, g, g + minStep*chebyshev(n, to)})
			}
		}
	}
	return nil
}

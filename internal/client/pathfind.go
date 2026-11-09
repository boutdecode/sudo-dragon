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

func (h pathHeap) Len() int           { return len(h) }
func (h pathHeap) Less(i, j int) bool { return h[i].f < h[j].f || (h[i].f == h[j].f && h[i].g > h[j].g) }
func (h pathHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *pathHeap) Push(x any)        { *h = append(*h, x.(pathNode)) }
func (h *pathHeap) Pop() any          { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

func chebyshev(a, b world.Pos) int { return max(absInt(a.X-b.X), absInt(a.Y-b.Y)) }

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// findPath cherche un chemin de from à to (8 directions, comme le clavier).
// free indique si une case est praticable. Le résultat exclut from et finit
// par to; nil si aucun chemin n'a été trouvé dans la limite maxNodes.
// Comme côté serveur, les diagonales ne sont pas gênées par les coins.
func findPath(from, to world.Pos, free func(world.Pos) bool, maxNodes int) []world.Pos {
	return findPathCost(from, to, free, func(world.Pos) int { return 1 }, 1, maxNodes)
}

// Coûts de marche de /moveto: une case de route coûte moins qu'une case hors
// route, donc le chemin fait un détour (jusqu'à 1,5 fois plus long) pour la suivre.
const (
	roadStepCost = 2
	offRoadCost  = 3
	walkMaxNodes = 60000 // la heuristique est moins serrée avec des coûts: on explore plus
	// fallbackMaxNodes borne la recherche sans préférence de route, utilisée
	// quand la recherche pondérée échoue (longues marches hors route).
	fallbackMaxNodes = 250000
)

// findWalkPath cherche un chemin de /moveto qui préfère les routes.
func (m Model) findWalkPath(from, to world.Pos, free func(world.Pos) bool) []world.Pos {
	cost := func(p world.Pos) int {
		if m.world.At(p.X, p.Y) == world.Road {
			return roadStepCost
		}
		return offRoadCost
	}
	if path := findPathCost(from, to, free, cost, roadStepCost, walkMaxNodes); path != nil {
		return path
	}
	// Budget dépassé (longue marche hors route): chemin le plus court, sans route.
	return findPath(from, to, free, fallbackMaxNodes)
}

// findPathCost est findPath avec un coût d'entrée par case. minStep est le coût
// minimal d'un pas (il garde l'heuristique admissible).
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
			continue // entrée périmée
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

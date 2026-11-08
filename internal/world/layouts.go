package world

import "math/rand/v2"

// rect est un rectangle de tuiles en coordonnées locales à la structure.
type rect struct{ x, y, w, h int }

func (r rect) grow(n int) rect { return rect{r.x - n, r.y - n, r.w + 2*n, r.h + 2*n} }

func (r rect) intersects(o rect) bool {
	return r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// building est un bâtiment avec sa porte; front est la case extérieure devant
// la porte, inside la case intérieure derrière elle.
type building struct {
	r                   rect
	door, front, inside Pos
}

// interior renvoie le rectangle intérieur (sans les murs).
func (b building) interior() rect { return rect{b.r.x + 1, b.r.y + 1, b.r.w - 2, b.r.h - 2} }

// canvas est la grille sur laquelle on dessine une structure. NPC et
// emplacements sont en coordonnées locales; generate les convertit.
type canvas struct {
	w, h  int
	t     []Tile
	rng   *rand.Rand
	npcs  []NPC
	slots []Slot
}

func newCanvas(w, h int, rng *rand.Rand) *canvas {
	c := &canvas{w: w, h: h, t: make([]Tile, w*h), rng: rng}
	for i := range c.t {
		c.t[i] = Plain
	}
	return c
}

func (c *canvas) in(x, y int) bool { return x >= 0 && x < c.w && y >= 0 && y < c.h }

func (c *canvas) at(x, y int) Tile { return c.t[y*c.w+x] }

func (c *canvas) set(x, y int, t Tile) {
	if c.in(x, y) {
		c.t[y*c.w+x] = t
	}
}

func (c *canvas) fill(r rect, t Tile) {
	for y := r.y; y < r.y+r.h; y++ {
		for x := r.x; x < r.x+r.w; x++ {
			c.set(x, y, t)
		}
	}
}

// walledBox dessine un bâtiment: anneau de murs et sol intérieur.
func (c *canvas) walledBox(r rect) {
	c.fill(r, Wall)
	c.fill(rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}, Floor)
}

// anyRoad indique si une tuile de r est déjà un chemin.
func (c *canvas) anyRoad(r rect) bool {
	for y := r.y; y < r.y+r.h; y++ {
		for x := r.x; x < r.x+r.w; x++ {
			if c.in(x, y) && c.at(x, y) == Road {
				return true
			}
		}
	}
	return false
}

// nearestRoad renvoie le chemin le plus proche de (x, y) en distance de Manhattan.
func (c *canvas) nearestRoad(x, y int) (Pos, bool) {
	best, bestD := Pos{}, -1
	for ty := 0; ty < c.h; ty++ {
		for tx := 0; tx < c.w; tx++ {
			if c.at(tx, ty) != Road {
				continue
			}
			if d := abs(tx-x) + abs(ty-y); bestD < 0 || d < bestD {
				best, bestD = Pos{tx, ty}, d
			}
		}
	}
	return best, bestD >= 0
}

func (c *canvas) ground(x, y int, avoid rect) bool {
	if !c.in(x, y) || avoid.contains(x, y) {
		return false
	}
	t := c.at(x, y)
	return t == Plain || t == Road
}

// connect trace un chemin de from jusqu'au chemin existant le plus proche, sans
// traverser avoid. Essaie d'abord deux tracés en L (plus nets), puis une
// recherche en largeur. Renvoie false si aucun chemin n'existe.
func (c *canvas) connect(from Pos, avoid rect) bool {
	target, ok := c.nearestRoad(from.X, from.Y)
	if !ok {
		return false
	}
	if !c.ground(from.X, from.Y, avoid) {
		return false
	}
	for _, horizontalFirst := range []bool{true, false} {
		var path []Pos
		x, y := from.X, from.Y
		step := func(dx, dy, n int) {
			for range n {
				path = append(path, Pos{x, y})
				x, y = x+dx, y+dy
			}
		}
		sx, sy := sign(target.X-from.X), sign(target.Y-from.Y)
		if horizontalFirst {
			step(sx, 0, abs(target.X-from.X))
			step(0, sy, abs(target.Y-from.Y))
		} else {
			step(0, sy, abs(target.Y-from.Y))
			step(sx, 0, abs(target.X-from.X))
		}
		path = append(path, target)
		clear := true
		for _, p := range path {
			if !c.ground(p.X, p.Y, avoid) {
				clear = false
				break
			}
		}
		if clear {
			for _, p := range path {
				c.set(p.X, p.Y, Road)
			}
			return true
		}
	}
	return c.connectBFS(from, avoid)
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// connectBFS cherche le plus court chemin de from à un chemin existant.
func (c *canvas) connectBFS(from Pos, avoid rect) bool {
	prev := make(map[Pos]Pos)
	prev[from] = from
	queue := []Pos{from}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p != from && c.at(p.X, p.Y) == Road {
			for q := p; q != from; q = prev[q] {
				c.set(q.X, q.Y, Road)
			}
			c.set(from.X, from.Y, Road)
			return true
		}
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := Pos{p.X + d[0], p.Y + d[1]}
			if _, seen := prev[q]; seen || !c.ground(q.X, q.Y, avoid) {
				continue
			}
			prev[q] = p
			queue = append(queue, q)
		}
	}
	return false
}

// doorFacing choisit la porte de r sur le côté tourné vers (tx, ty).
func (c *canvas) doorFacing(r rect, tx, ty int) building {
	cx, cy := r.x+r.w/2, r.y+r.h/2
	dx, dy := tx-cx, ty-cy
	b := building{r: r}
	if abs(dx)*r.h >= abs(dy)*r.w { // côté est ou ouest
		y := r.y + 1 + c.rng.IntN(r.h-2)
		if dx >= 0 {
			x := r.x + r.w - 1
			b.door, b.front, b.inside = Pos{x, y}, Pos{x + 1, y}, Pos{x - 1, y}
		} else {
			x := r.x
			b.door, b.front, b.inside = Pos{x, y}, Pos{x - 1, y}, Pos{x + 1, y}
		}
		return b
	}
	x := r.x + 1 + c.rng.IntN(r.w-2)
	if dy >= 0 {
		y := r.y + r.h - 1
		b.door, b.front, b.inside = Pos{x, y}, Pos{x, y + 1}, Pos{x, y - 1}
	} else {
		y := r.y
		b.door, b.front, b.inside = Pos{x, y}, Pos{x, y - 1}, Pos{x, y + 1}
	}
	return b
}

// placeBuildings pose jusqu'à count bâtiments dans bounds, chacun relié par un
// chemin à un chemin existant. Les bâtiments restent à deux cases les uns des
// autres et ne recouvrent aucun chemin existant.
func (c *canvas) placeBuildings(count, minW, maxW, minH, maxH int, bounds rect) []building {
	var placed []building
	for attempt := 0; attempt < count*80 && len(placed) < count; attempt++ {
		w := minW + c.rng.IntN(maxW-minW+1)
		h := minH + c.rng.IntN(maxH-minH+1)
		if bounds.w < w || bounds.h < h {
			continue
		}
		r := rect{bounds.x + c.rng.IntN(bounds.w-w+1), bounds.y + c.rng.IntN(bounds.h-h+1), w, h}

		clash := c.anyRoad(r.grow(1))
		for _, p := range placed {
			if r.grow(2).intersects(p.r) {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		target, ok := c.nearestRoad(r.x+r.w/2, r.y+r.h/2)
		if !ok {
			continue
		}
		b := c.doorFacing(r, target.X, target.Y)
		if !c.connect(b.front, r) {
			continue
		}
		c.walledBox(r)
		c.set(b.door.X, b.door.Y, Door)
		placed = append(placed, b)
	}
	return placed
}

// npcSpot renvoie la case intérieure la plus éloignée de la porte: un PNJ qui
// s'y tient ne gêne jamais l'entrée.
func (b building) npcSpot() Pos {
	in := b.interior()
	best, bestD := Pos{}, -1
	for y := in.y; y < in.y+in.h; y++ {
		for x := in.x; x < in.x+in.w; x++ {
			if d := abs(x-b.inside.X) + abs(y-b.inside.Y); d > bestD {
				best, bestD = Pos{x, y}, d
			}
		}
	}
	return best
}

// closestTo trie par distance au point donné (indices dans bs).
func closestTo(bs []building, x, y int) []int {
	idx := make([]int, len(bs))
	for i := range idx {
		idx[i] = i
	}
	dist := func(i int) int {
		b := bs[i].r
		return abs(b.x+b.w/2-x) + abs(b.y+b.h/2-y)
	}
	for i := 1; i < len(idx); i++ { // tri par insertion: les listes sont courtes
		for j := i; j > 0 && dist(idx[j]) < dist(idx[j-1]); j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	return idx
}

// layoutVillage: une place centrale, 5 à 9 maisons reliées par des chemins, un soigneur.
func layoutVillage(w, h int, rng *rand.Rand) *canvas {
	c := newCanvas(w, h, rng)
	square := rect{w/2 - 3, h/2 - 2, 6, 4}
	c.fill(square, Road)

	houses := c.placeBuildings(5+rng.IntN(5), 5, 7, 4, 5, rect{3, 3, w - 6, h - 6})
	if len(houses) == 0 {
		c.npcs = append(c.npcs, NPC{Kind: NPCHealer, Pos: Pos{w / 2, h / 2}})
		return c
	}
	healer := houses[closestTo(houses, w/2, h/2)[0]]
	c.npcs = append(c.npcs, NPC{Kind: NPCHealer, Pos: healer.npcSpot()})
	return c
}

// layoutTown: muraille à quatre portes, deux avenues en croix, place du marché,
// une vingtaine de bâtiments (on en demande plus qu'il n'en tient: les chemins
// déjà tracés écartent des emplacements) dont une boutique et un temple.
func layoutTown(w, h int, rng *rand.Rand) *canvas {
	c := newCanvas(w, h, rng)
	const inset = 2
	wall := rect{inset, inset, w - 2*inset, h - 2*inset}
	c.fill(wall, Wall)
	c.fill(wall.grow(-1), Plain)

	// Avenues sur toute l'emprise, qui traversent la muraille par quatre portes de trois cases.
	midX, midY := w/2, h/2
	for i := -1; i <= 1; i++ {
		for x := 0; x < w; x++ {
			c.set(x, midY+i, Road)
		}
		for y := 0; y < h; y++ {
			c.set(midX+i, y, Road)
		}
	}
	for i := -1; i <= 1; i++ {
		c.set(midX+i, inset, Door)
		c.set(midX+i, h-1-inset, Door)
		c.set(inset, midY+i, Door)
		c.set(w-1-inset, midY+i, Door)
	}
	c.fill(rect{midX - 6, midY - 4, 12, 8}, Road)

	bs := c.placeBuildings(24+rng.IntN(11), 5, 9, 4, 7, rect{inset + 3, inset + 3, w - 2*inset - 6, h - 2*inset - 6})

	// Boutique et temple: parmi les bâtiments proches du centre, les deux plus spacieux.
	order := closestTo(bs, midX, midY)
	if len(order) > 8 {
		order = order[:8]
	}
	area := func(i int) int { in := bs[i].interior(); return in.w * in.h }
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && area(order[j]) > area(order[j-1]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	kinds := []NPCKind{NPCMerchant, NPCHealer}
	for i := 0; i < len(order) && i < len(kinds); i++ {
		c.npcs = append(c.npcs, NPC{Kind: kinds[i], Pos: bs[order[i]].npcSpot()})
	}
	return c
}

// layoutFortress: enceinte épaisse à tours d'angle et une porte, cour, deux
// casernes et un donjon cloisonné dont la salle centrale abrite le seigneur.
// Le plan est ensuite retourné au hasard (horizontalement, verticalement).
func layoutFortress(w, h int, rng *rand.Rand) *canvas {
	c := newCanvas(w, h, rng)

	// Enceinte, cour et tours d'angle.
	c.fill(rect{2, 2, 44, 36}, Wall)
	c.fill(rect{4, 4, 40, 32}, Floor)
	for _, t := range []rect{{2, 2, 6, 6}, {40, 2, 6, 6}, {2, 32, 6, 6}, {40, 32, 6, 6}} {
		c.fill(t, Wall)
	}
	c.fill(rect{23, 36, 3, 2}, Door) // porte sud

	// Donjon: trois salles (cellule, salle centrale, cellule) et une porte sud.
	c.walledBox(rect{16, 6, 16, 12})
	for y := 7; y <= 16; y++ {
		c.set(21, y, Wall)
		c.set(26, y, Wall)
	}
	c.set(21, 12, Door)
	c.set(26, 12, Door)
	c.fill(rect{23, 17, 2, 1}, Door)

	// Casernes ouvertes sur la cour.
	c.walledBox(rect{5, 20, 12, 7})
	c.set(16, 23, Door)
	c.walledBox(rect{31, 20, 12, 7})
	c.set(31, 23, Door)

	// Garnison: 2 élites dans la salle centrale, 2 soldats par zone (portail, cour, casernes) et le seigneur.
	c.slots = []Slot{
		{Pos{24, 10}, RoleBoss},
		{Pos{22, 13}, RoleElite}, {Pos{25, 13}, RoleElite},
		{Pos{9, 23}, RoleGuard}, {Pos{38, 23}, RoleGuard},
		{Pos{10, 10}, RoleGuard}, {Pos{37, 10}, RoleGuard},
		{Pos{22, 34}, RoleGuard}, {Pos{26, 34}, RoleGuard},
	}

	c.mirror(rng.IntN(2) == 0, rng.IntN(2) == 0)
	return c
}

// mirror retourne le plan (tuiles, PNJ et emplacements).
func (c *canvas) mirror(flipX, flipY bool) {
	if !flipX && !flipY {
		return
	}
	tx := func(x int) int {
		if flipX {
			return c.w - 1 - x
		}
		return x
	}
	ty := func(y int) int {
		if flipY {
			return c.h - 1 - y
		}
		return y
	}
	out := make([]Tile, len(c.t))
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			out[ty(y)*c.w+tx(x)] = c.t[y*c.w+x]
		}
	}
	c.t = out
	for i := range c.npcs {
		c.npcs[i].Pos = Pos{tx(c.npcs[i].Pos.X), ty(c.npcs[i].Pos.Y)}
	}
	for i := range c.slots {
		c.slots[i].Pos = Pos{tx(c.slots[i].Pos.X), ty(c.slots[i].Pos.Y)}
	}
}

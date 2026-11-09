package world

import (
	"math/rand/v2"
	"sort"
)

// Dimensions de la carte d'un donjon, en tuiles. Ses coordonnées sont locales : (0,0) est
// son coin haut-gauche, quelle que soit la position de l'entrée sur la carte principale.
const (
	DungeonW = 64
	DungeonH = 40
)

// DungeonMap est le plan d'un donjon : des salles reliées par des couloirs dans la roche.
// Tout est tiré de la seed du donjon, le client le recalcule donc à l'identique.
type DungeonMap struct {
	W, H  int
	Spawn Pos    // case où arrivent les joueurs, à côté de l'escalier
	Exit  Pos    // escalier de sortie
	Slots []Slot // emplacements des monstres : RoleGuard, RoleElite, et un RoleBoss
	// Chests est la liste des coffres (la tuile correspondante est Chest).
	Chests []Pos
	// Boss est la case du boss, au centre de la dernière salle de la chaîne ; BossChest est son trésor (aussi dans Chests).
	Boss      Pos
	BossChest Pos

	tiles []Tile
}

// TileAt renvoie la tuile de la case (x, y); hors de la carte, c'est de la roche (Rock).
func (m *DungeonMap) TileAt(x, y int) Tile {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return Rock
	}
	return m.tiles[y*m.W+x]
}

func (m *DungeonMap) set(x, y int, t Tile) { m.tiles[y*m.W+x] = t }

// NewDungeonMap génère le plan de d. Le résultat ne dépend que de d.Seed et d.Tier.
func NewDungeonMap(d *Dungeon) *DungeonMap {
	rng := rand.New(rand.NewPCG(uint64(d.Seed), mix64(uint64(d.Seed)^0xD06E0)))
	m := &DungeonMap{W: DungeonW, H: DungeonH, tiles: make([]Tile, DungeonW*DungeonH)}
	for i := range m.tiles {
		m.tiles[i] = Rock
	}

	// Salles : tirées au hasard sans se toucher, puis triées de gauche à droite ; l'entrée est
	// dans la première, le boss dans la dernière, le chemin de l'une à l'autre les traverse toutes.
	want := 5 + d.Tier + rng.IntN(2)
	var rooms []rect
	for range 400 {
		if len(rooms) == want {
			break
		}
		w, h := 5+rng.IntN(6), 4+rng.IntN(4)
		r := rect{1 + rng.IntN(DungeonW-w-2), 1 + rng.IntN(DungeonH-h-2), w, h}
		clash := false
		for _, o := range rooms {
			if r.grow(2).intersects(o) {
				clash = true
				break
			}
		}
		if !clash {
			rooms = append(rooms, r)
		}
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].x+rooms[i].w/2 < rooms[j].x+rooms[j].w/2 })
	// Garde-fou : 400 essais sur une grille de cette taille donnent toujours plus de trois salles.
	for len(rooms) < 3 {
		rooms = append(rooms, rect{4 + 20*len(rooms), 10, 8, 6})
	}

	for _, r := range rooms {
		for y := r.y; y < r.y+r.h; y++ {
			for x := r.x; x < r.x+r.w; x++ {
				m.set(x, y, Floor)
			}
		}
	}
	center := func(r rect) Pos { return Pos{r.x + r.w/2, r.y + r.h/2} }
	for i := 1; i < len(rooms); i++ {
		a, b := center(rooms[i-1]), center(rooms[i])
		m.corridor(a, b, rng.IntN(2) == 0)
	}

	first, last := rooms[0], rooms[len(rooms)-1]
	m.Exit = Pos{first.x, first.y + first.h/2}
	m.Spawn = Pos{first.x + 1, first.y + first.h/2}
	m.set(m.Exit.X, m.Exit.Y, Stairs)
	m.Boss = center(last)

	used := map[Pos]bool{m.Exit: true, m.Spawn: true, m.Boss: true}
	// free tire une case libre de la salle r (ni escalier, ni emplacement déjà pris).
	free := func(r rect) (Pos, bool) {
		for range 40 {
			p := Pos{r.x + rng.IntN(r.w), r.y + rng.IntN(r.h)}
			if !used[p] && m.tiles[p.Y*m.W+p.X] == Floor {
				used[p] = true
				return p, true
			}
		}
		return Pos{}, false
	}

	m.Slots = append(m.Slots, Slot{m.Boss, RoleBoss})
	for range 1 + d.Tier/3 {
		if p, ok := free(last); ok {
			m.Slots = append(m.Slots, Slot{p, RoleElite})
		}
	}
	if p, ok := free(last); ok { // le trésor du boss
		m.BossChest = p
		m.set(p.X, p.Y, Chest)
		m.Chests = append(m.Chests, p)
	}
	for _, r := range rooms[1 : len(rooms)-1] {
		for range 1 + rng.IntN(2) + d.Tier/3 {
			if p, ok := free(r); ok {
				m.Slots = append(m.Slots, Slot{p, RoleGuard})
			}
		}
		if rng.IntN(2) == 0 {
			if p, ok := free(r); ok {
				m.set(p.X, p.Y, Chest)
				m.Chests = append(m.Chests, p)
			}
		}
	}
	// La salle d'entrée reste sûre : un coffre au plus, aucun monstre.
	if rng.IntN(3) == 0 {
		if p, ok := free(first); ok {
			m.set(p.X, p.Y, Chest)
			m.Chests = append(m.Chests, p)
		}
	}
	return m
}

// corridor creuse un couloir d'une case de large en L entre a et b.
func (m *DungeonMap) corridor(a, b Pos, horizontalFirst bool) {
	x, y := a.X, a.Y
	carve := func() {
		if m.tiles[y*m.W+x] == Rock {
			m.set(x, y, Floor)
		}
	}
	stepX := func() {
		for x != b.X {
			x += sign(b.X - x)
			carve()
		}
	}
	stepY := func() {
		for y != b.Y {
			y += sign(b.Y - y)
			carve()
		}
	}
	if horizontalFirst {
		stepX()
		stepY()
	} else {
		stepY()
		stepX()
	}
}

// Reachable renvoie les cases praticables atteignables depuis from (parcours en largeur).
func (m *DungeonMap) Reachable(from Pos) map[Pos]bool {
	seen := map[Pos]bool{from: true}
	queue := []Pos{from}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range [4]Pos{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := Pos{p.X + d.X, p.Y + d.Y}
			if !seen[q] && m.TileAt(q.X, q.Y).Walkable() {
				seen[q] = true
				queue = append(queue, q)
			}
		}
	}
	return seen
}

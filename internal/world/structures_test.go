package world

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

var testSeeds = []int64{1, 2, 7, 42, 1234}

// sharedWorld renvoie un monde partagé entre les tests en lecture seule: les
// régions déjà générées ne le sont pas une seconde fois.
var (
	sharedMu     sync.Mutex
	sharedWorlds = map[int64]*World{}
)

func sharedWorld(seed int64) *World {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if w, ok := sharedWorlds[seed]; ok {
		return w
	}
	w := New(seed)
	sharedWorlds[seed] = w
	return w
}

// allStructures renvoie les structures des régions [-n, n]² d'un monde.
func allStructures(w *World, n int) []*Structure {
	var out []*Structure
	for ry := -n; ry <= n; ry++ {
		for rx := -n; rx <= n; rx++ {
			if s := w.structureForRegion(RegionPos{rx, ry}); s != nil {
				out = append(out, s)
			}
		}
	}
	return out
}

func sameStructure(a, b *Structure) bool {
	return a.Kind == b.Kind && a.Origin == b.Origin && a.W == b.W && a.H == b.H &&
		reflect.DeepEqual(a.tiles, b.tiles) && reflect.DeepEqual(a.NPCs, b.NPCs) && reflect.DeepEqual(a.Slots, b.Slots)
}

func TestStructuresAreDeterministic(t *testing.T) {
	for _, seed := range testSeeds {
		a, b := New(seed), New(seed)
		for ry := -4; ry <= 4; ry++ {
			for rx := -4; rx <= 4; rx++ {
				sa, sb := a.structureForRegion(RegionPos{rx, ry}), b.structureForRegion(RegionPos{rx, ry})
				if (sa == nil) != (sb == nil) || (sa != nil && !sameStructure(sa, sb)) {
					t.Fatalf("seed %d région (%d,%d): deux mondes identiques diffèrent", seed, rx, ry)
				}
			}
		}
	}
	// Une autre seed donne un autre monde.
	a, b := allStructures(New(1), 4), allStructures(New(2), 4)
	if len(a) == len(b) {
		same := true
		for i := range a {
			same = same && sameStructure(a[i], b[i])
		}
		if same {
			t.Fatal("deux seeds différentes donnent les mêmes structures")
		}
	}
}

// Le cache ne doit pas changer le résultat: vidé, il se reconstruit à l'identique.
func TestStructureCacheRebuildsIdentically(t *testing.T) {
	w := New(7)
	before := allStructures(w, 3)
	w.smu.Lock()
	clear(w.structs)
	w.smu.Unlock()
	after := allStructures(w, 3)
	if len(before) != len(after) {
		t.Fatalf("%d structures puis %d après vidage du cache", len(before), len(after))
	}
	for i := range before {
		if !sameStructure(before[i], after[i]) {
			t.Fatalf("structure %d différente après reconstruction", i)
		}
	}
}

func TestStructuresStayInsideTheirRegion(t *testing.T) {
	for _, seed := range testSeeds {
		w := sharedWorld(seed)
		for _, s := range allStructures(w, 5) {
			lx, ly := s.Origin.X-s.Region.X*RegionSize, s.Origin.Y-s.Region.Y*RegionSize
			if lx < regionMargin || ly < regionMargin || lx+s.W > RegionSize-regionMargin || ly+s.H > RegionSize-regionMargin {
				t.Fatalf("seed %d: %v en (%d,%d) %dx%d sort de sa région ou de sa marge", seed, s.Kind, lx, ly, s.W, s.H)
			}
			if RegionOf(s.Origin.X, s.Origin.Y) != s.Region || RegionOf(s.Origin.X+s.W-1, s.Origin.Y+s.H-1) != s.Region {
				t.Fatalf("seed %d: l'emprise chevauche deux régions", seed)
			}
		}
	}
}

func TestSafeMarginStaysInsideRegion(t *testing.T) {
	if safeMargin >= regionMargin {
		t.Fatalf("safeMargin (%d) doit rester inférieure à regionMargin (%d)", safeMargin, regionMargin)
	}
}

func TestStructureSizesMatchLayouts(t *testing.T) {
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			want := kindSize[s.Kind]
			if s.W != want[0] || s.H != want[1] || len(s.tiles) != s.W*s.H {
				t.Fatalf("seed %d: %v de %dx%d (%d tuiles), attendu %v", seed, s.Kind, s.W, s.H, len(s.tiles), want)
			}
		}
	}
}

// reachable calcule, par parcours en largeur à 4 voisins depuis le coin de
// l'emprise (toujours du terrain dégagé), les cases praticables atteignables.
// Les PNJ comptent comme des obstacles: ils occupent leur case.
func reachable(s *Structure) map[Pos]bool {
	blocked := map[Pos]bool{}
	for _, n := range s.NPCs {
		blocked[n.Pos] = true
	}
	start := s.Origin
	seen := map[Pos]bool{start: true}
	queue := []Pos{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			q := Pos{p.X + d[0], p.Y + d[1]}
			if seen[q] || !s.Contains(q.X, q.Y) || blocked[q] || !s.TileAt(q.X, q.Y).Walkable() {
				continue
			}
			seen[q] = true
			queue = append(queue, q)
		}
	}
	return seen
}

func TestEveryOpenTileIsReachableFromOutside(t *testing.T) {
	total := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			total++
			seen := reachable(s)
			if !s.TileAt(s.Origin.X, s.Origin.Y).Walkable() {
				t.Fatalf("seed %d %v: le coin de l'emprise n'est pas dégagé", seed, s.Kind)
			}
			blocked := map[Pos]bool{}
			for _, n := range s.NPCs {
				blocked[n.Pos] = true
			}
			for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
				for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
					p := Pos{x, y}
					tile := s.TileAt(x, y)
					if !tile.Walkable() || blocked[p] {
						continue
					}
					if !seen[p] {
						t.Fatalf("seed %d %v: %v (tuile %d) inaccessible depuis l'extérieur (en %v)", seed, s.Kind, p, tile, s.Origin)
					}
				}
			}
		}
	}
	if total < 20 {
		t.Fatalf("seulement %d structures testées: l'échantillon est trop pauvre", total)
	}
}

func TestNPCsStandOnFloorAndNeverBlockEntrance(t *testing.T) {
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			seen := reachable(s) // les PNJ sont des obstacles dans ce parcours
			for _, n := range s.NPCs {
				// Les villageois peuvent se tenir sur la place (route) ; les autres PNJ restent à l'intérieur.
				if tile := s.TileAt(n.Pos.X, n.Pos.Y); tile != Floor && !(n.Kind == NPCVillager && tile == Road) {
					t.Fatalf("seed %d %v: PNJ %s hors d'un sol intérieur (tuile %d)", seed, s.Kind, n.Kind, tile)
				}
				// Au moins un voisin atteignable, sinon le joueur ne peut pas lui parler.
				ok := false
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if seen[Pos{n.Pos.X + d[0], n.Pos.Y + d[1]}] {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("seed %d %v: personne ne peut atteindre le PNJ %s en %v", seed, s.Kind, n.Kind, n.Pos)
				}
			}
		}
	}
}

func TestNPCsPerKind(t *testing.T) {
	kinds := map[Kind]int{}
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			kinds[s.Kind]++
			counts := map[NPCKind]int{}
			for _, n := range s.NPCs {
				counts[n.Kind]++
			}
			switch s.Kind {
			case KindVillage:
				if counts[NPCHealer] != 1 || counts[NPCVillager] != 3 || len(s.NPCs) != 4 {
					t.Fatalf("village: PNJ %v, attendu un soigneur et 3 villageois", counts)
				}
			case KindTown:
				if counts[NPCHealer] != 1 || counts[NPCMerchant] != 1 || counts[NPCVillager] != 5 || len(s.NPCs) != 7 {
					t.Fatalf("ville: PNJ %v, attendu un marchand, un soigneur et 5 villageois", counts)
				}
				if s.NPCs[0].Kind != NPCMerchant || s.NPCs[1].Kind != NPCHealer {
					t.Fatalf("ville: les indices des PNJ d'origine ont bougé : %v", s.NPCs)
				}
			case KindFortress:
				if len(s.NPCs) != 0 {
					t.Fatalf("forteresse avec des PNJ: %v", counts)
				}
			}
		}
	}
	for _, k := range []Kind{KindVillage, KindTown, KindFortress} {
		if kinds[k] == 0 {
			t.Errorf("aucune structure de type %v sur l'échantillon", k)
		}
	}
}

// Les PNJ d'origine gardent leur indice (les souvenirs en base y sont rattachés) ; les villageois suivent.
func TestVillagersComeAfterOriginalNPCsAndAreDistinct(t *testing.T) {
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			seenPos, seenName := map[Pos]bool{}, map[string]bool{}
			sawVillager := false
			for i, n := range s.NPCs {
				if n.Index != i {
					t.Fatalf("indice %d au rang %d", n.Index, i)
				}
				if n.Kind == NPCVillager {
					sawVillager = true
				} else if sawVillager {
					t.Fatalf("PNJ %s après un villageois", n.Kind)
				}
				if seenPos[n.Pos] || seenName[n.Name] {
					t.Fatalf("doublon de position ou de nom : %v %q", n.Pos, n.Name)
				}
				seenPos[n.Pos], seenName[n.Name] = true, true
				if !s.Contains(n.Pos.X, n.Pos.Y) {
					t.Fatalf("PNJ hors de l'emprise : %v", n.Pos)
				}
			}
		}
	}
}

func TestFortressGarrison(t *testing.T) {
	found := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			if s.Kind != KindFortress {
				continue
			}
			found++
			roles := map[SlotRole]int{}
			seenPos := map[Pos]bool{}
			reach := reachable(s)
			for _, sl := range s.Slots {
				roles[sl.Role]++
				if seenPos[sl.Pos] {
					t.Fatalf("deux emplacements sur %v", sl.Pos)
				}
				seenPos[sl.Pos] = true
				if s.TileAt(sl.Pos.X, sl.Pos.Y) != Floor {
					t.Fatalf("emplacement %v hors d'un sol intérieur", sl.Pos)
				}
				if !reach[sl.Pos] {
					t.Fatalf("emplacement %v (rôle %d) inaccessible: on ne pourrait pas affronter ce monstre", sl.Pos, sl.Role)
				}
			}
			if roles[RoleBoss] != 1 || roles[RoleElite] != 2 || roles[RoleGuard] != 6 {
				t.Fatalf("garnison %v, attendu 1 seigneur, 2 élites, 6 soldats", roles)
			}
		}
	}
	if found == 0 {
		t.Fatal("aucune forteresse trouvée sur l'échantillon")
	}
}

func TestOnlyFortressesHaveSlots(t *testing.T) {
	for _, s := range allStructures(sharedWorld(3), 5) {
		if s.Kind != KindFortress && len(s.Slots) != 0 {
			t.Fatalf("%v avec des emplacements de garnison", s.Kind)
		}
	}
}

func TestBuildingsHaveDoors(t *testing.T) {
	min := map[Kind]int{KindVillage: 4, KindTown: 12 + 8, KindFortress: 12}
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			doors := 0
			for _, tile := range s.tiles {
				if tile == Door {
					doors++
				}
			}
			if doors < min[s.Kind] {
				t.Fatalf("seed %d %v: %d portes, au moins %d attendues", seed, s.Kind, doors, min[s.Kind])
			}
		}
	}
}

// Le point de départ ne doit jamais être recouvert ni encombré par une structure.
func TestNoStructureNearSpawn(t *testing.T) {
	for seed := int64(1); seed <= 60; seed++ {
		w := sharedWorld(seed)
		sp := w.FindSpawn()
		if near := w.StructuresNear(sp, startClearance-1); len(near) != 0 {
			t.Fatalf("seed %d: %v à moins de %d cases du point de départ %v", seed, near[0].Kind, startClearance, sp)
		}
		if tile := w.At(sp.X, sp.Y); tile != Plain && tile != Forest {
			t.Fatalf("seed %d: le point de départ est sur %d", seed, tile)
		}
	}
}

// Un nouveau joueur doit trouver un village à une marche raisonnable.
func TestStartVillageIsNear(t *testing.T) {
	const seeds = 60
	near, villages := 0, 0
	for seed := int64(1); seed <= seeds; seed++ {
		w := sharedWorld(seed)
		sp := w.FindSpawn()
		list := w.StructuresNear(sp, 160)
		for _, s := range list {
			if s.Kind == KindVillage {
				villages++
				near++
				break
			}
		}
	}
	// Quelques mondes peuvent démarrer sur une île ou entre des montagnes: on tolère 10 %.
	if near < seeds*9/10 {
		t.Fatalf("seulement %d/%d mondes ont un village à moins de 160 cases du départ", near, seeds)
	}
	_ = villages
}

func TestSafeZones(t *testing.T) {
	w := sharedWorld(1)
	var village, town, fortress *Structure
	for _, s := range allStructures(w, 8) {
		switch {
		case s.Kind == KindVillage && village == nil:
			village = s
		case s.Kind == KindTown && town == nil:
			town = s
		case s.Kind == KindFortress && fortress == nil:
			fortress = s
		}
	}
	if village == nil || fortress == nil {
		t.Skip("échantillon sans village ou sans forteresse")
	}

	for _, s := range []*Structure{village, town} {
		if s == nil {
			continue
		}
		c := s.Center()
		if !w.Safe(c.X, c.Y) {
			t.Errorf("%v: le centre n'est pas en zone sûre", s.Kind)
		}
		// Limite exacte: safeMargin cases autour de l'emprise.
		if !w.Safe(s.Origin.X-safeMargin, c.Y) || w.Safe(s.Origin.X-safeMargin-1, c.Y) {
			t.Errorf("%v: la lisière ouest de la zone sûre est mal placée", s.Kind)
		}
		if !w.Safe(c.X, s.Origin.Y+s.H-1+safeMargin) || w.Safe(c.X, s.Origin.Y+s.H+safeMargin) {
			t.Errorf("%v: la lisière sud de la zone sûre est mal placée", s.Kind)
		}
	}
	fc := fortress.Center()
	if w.Safe(fc.X, fc.Y) {
		t.Error("une forteresse ne doit pas être une zone sûre")
	}
	sp := w.FindSpawn()
	if w.Safe(sp.X, sp.Y) {
		t.Error("le point de départ ne doit pas être en zone sûre")
	}
}

func TestStructuresNearSemantics(t *testing.T) {
	w := sharedWorld(1)
	all := allStructures(w, 4)
	if len(all) == 0 {
		t.Fatal("aucune structure")
	}
	s := all[0]
	c := s.Center()

	if got := w.StructuresNear(c, 0); len(got) == 0 || got[0] != s {
		t.Fatalf("depuis son centre, la structure doit être la première (rayon 0): %v", got)
	}
	// Juste à l'extérieur de l'emprise, rayon trop court: absente; assez long: présente.
	out := Pos{s.Origin.X - 10, c.Y}
	for _, o := range w.StructuresNear(out, 9) {
		if o == s {
			t.Fatal("trouvée à 10 cases avec un rayon de 9")
		}
	}
	found := false
	for _, o := range w.StructuresNear(out, 10) {
		found = found || o == s
	}
	if !found {
		t.Fatal("non trouvée à 10 cases avec un rayon de 10")
	}
	// Tri par proximité.
	list := w.StructuresNear(Pos{}, 600)
	for i := 1; i < len(list); i++ {
		a, b := list[i-1].Center(), list[i].Center()
		if abs(a.X)+abs(a.Y) > abs(b.X)+abs(b.Y) {
			t.Fatal("StructuresNear n'est pas triée de la plus proche à la plus lointaine")
		}
	}
}

func TestNPCLookups(t *testing.T) {
	w := sharedWorld(1)
	checked := 0
	for _, s := range allStructures(w, 5) {
		for _, n := range s.NPCs {
			checked++
			got, ok := w.NPCAt(n.Pos.X, n.Pos.Y)
			if !ok || got != n {
				t.Fatalf("NPCAt(%v) = %v, %v; attendu %v", n.Pos, got, ok, n)
			}
			if _, ok := w.NPCAt(n.Pos.X+1, n.Pos.Y); ok {
				t.Fatalf("NPCAt trouve un PNJ à côté de %v", n.Pos)
			}
			in := w.NPCsIn(n.Pos.X-3, n.Pos.Y-3, n.Pos.X+3, n.Pos.Y+3)
			has := false
			for _, o := range in {
				has = has || o == n
			}
			if !has {
				t.Fatalf("NPCsIn ne renvoie pas le PNJ en %v", n.Pos)
			}
			if far := w.NPCsIn(n.Pos.X+1, n.Pos.Y+1, n.Pos.X+5, n.Pos.Y+5); len(far) != 0 {
				for _, o := range far {
					if o == n {
						t.Fatalf("NPCsIn renvoie un PNJ hors du rectangle")
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("aucun PNJ vérifié")
	}
	if _, ok := w.NPCAt(1_000_003, -777); ok {
		t.Fatal("PNJ trouvé en pleine nature")
	}
}

// Les chunks (chemin rapide) et TileAt (chemin direct) doivent donner les mêmes tuiles.
func TestChunksAgreeWithTileAt(t *testing.T) {
	w := sharedWorld(1)
	for _, s := range allStructures(w, 3) {
		for _, p := range []Pos{s.Origin, s.Center(), {s.Origin.X + s.W - 1, s.Origin.Y + s.H - 1}, {s.Origin.X - 2, s.Origin.Y - 2}} {
			if got, want := w.At(p.X, p.Y), w.TileAt(p.X, p.Y); got != want {
				t.Fatalf("%v: At=%d, TileAt=%d", p, got, want)
			}
		}
		// Tout l'emprise, tuile par tuile.
		for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
			for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
				if w.At(x, y) != s.TileAt(x, y) {
					t.Fatalf("(%d,%d): le chunk ne reflète pas la structure", x, y)
				}
			}
		}
	}
}

// Hors des structures, le terrain est inchangé.
func TestTerrainOutsideStructuresIsUnchanged(t *testing.T) {
	w := sharedWorld(1)
	for _, p := range []Pos{{-500, 300}, {0, 0}, {900, -40}, {123, 456}} {
		if w.StructureAt(p.X, p.Y) != nil {
			continue
		}
		if w.TileAt(p.X, p.Y) != w.terrainAt(p.X, p.Y) {
			t.Fatalf("%v: TileAt diffère du terrain brut hors structure", p)
		}
	}
}

func TestStructureTilesAreStructureTiles(t *testing.T) {
	for _, s := range allStructures(sharedWorld(1), 5) {
		for _, tile := range s.tiles {
			if tile >= NumTiles {
				t.Fatalf("tuile %d hors de l'énumération", tile)
			}
		}
	}
}

func TestStructureDistribution(t *testing.T) {
	counts := map[Kind]int{}
	regions, withStruct := 0, 0
	for _, seed := range testSeeds {
		w := sharedWorld(seed)
		for ry := -8; ry < 8; ry++ {
			for rx := -8; rx < 8; rx++ {
				regions++
				if s := w.structureForRegion(RegionPos{rx, ry}); s != nil {
					withStruct++
					counts[s.Kind]++
				}
			}
		}
	}
	frac := float64(withStruct) / float64(regions)
	if frac < 0.20 || frac > 0.60 {
		t.Fatalf("%.0f%% des régions ont une structure (attendu entre 20 et 60 %%)", frac*100)
	}
	if counts[KindVillage] <= counts[KindTown] || counts[KindVillage] <= counts[KindFortress]/2 {
		t.Fatalf("répartition inattendue: %v (les villages devraient dominer, les villes être rares)", counts)
	}
	t.Logf("%d régions, %.0f%% occupées: %v", regions, frac*100, counts)
}

func TestFirstChunkGenerationIsFast(t *testing.T) {
	w := New(1)
	start := time.Now()
	for cy := -5; cy < 5; cy++ {
		for cx := -5; cx < 5; cx++ {
			w.Chunk(ChunkPos{cx, cy})
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("100 chunks en %v: la recherche de structure ralentit la génération", d)
	}
}

func TestWallsBlockAndDoorsOpen(t *testing.T) {
	if Wall.Walkable() {
		t.Fatal("un mur doit bloquer")
	}
	for _, tile := range []Tile{Door, Floor, Road} {
		if !tile.Walkable() {
			t.Fatalf("la tuile %d devrait être praticable", tile)
		}
	}
}

func BenchmarkChunkWithStructures(b *testing.B) {
	for i := 0; i < b.N; i++ {
		w := New(int64(i % 5))
		for cy := 0; cy < 8; cy++ {
			for cx := 0; cx < 8; cx++ {
				w.Chunk(ChunkPos{cx, cy})
			}
		}
	}
}

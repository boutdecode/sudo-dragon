package render

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/game"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func plain(s string) []string { return strings.Split(ansi.Strip(s), "\n") }

func TestViewportDimensions(t *testing.T) {
	w := world.New(1)
	for _, d := range [][2]int{{81, 41}, {10, 5}, {1, 1}, {40, 12}} {
		lines := plain(Viewport(w, 3, -7, d[0], d[1], nil, nil, 0))
		if len(lines) != d[1] {
			t.Fatalf("%v: %d lines, expected %d", d, len(lines), d[1])
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != d[0] {
				t.Fatalf("%v line %d: width %d, expected %d", d, i, got, d[0])
			}
		}
	}
}

func TestViewportEntities(t *testing.T) {
	w := world.New(1)
	cols, rows := 21, 11
	self := protocol.Entity{ID: 1, Name: "me", X: 100, Y: 100}
	near := protocol.Entity{ID: 2, Name: "n", X: 103, Y: 98}
	far := protocol.Entity{ID: 3, Name: "f", X: 200, Y: 200}

	lines := plain(Viewport(w, self.X, self.Y, cols, rows, []protocol.Entity{self, near, far}, nil, self.ID))

	// Centre = (cols/2, rows/2)
	if lines[rows/2][cols/2] != '@' {
		t.Fatalf("local player missing from the centre: %q", lines[rows/2])
	}
	if lines[rows/2-2][cols/2+3] != '@' {
		t.Fatalf("neighbour misplaced:\n%s", strings.Join(lines, "\n"))
	}
	if n := strings.Count(strings.Join(lines, ""), "@"); n != 2 {
		t.Fatalf("%d '@' drawn, expected 2 (the distant entity must not appear)", n)
	}
}

func TestViewportDeterministic(t *testing.T) {
	a := Viewport(world.New(5), 0, 0, 60, 20, nil, nil, 0)
	b := Viewport(world.New(5), 0, 0, 60, 20, nil, nil, 0)
	if a != b {
		t.Fatal("non-deterministic rendering")
	}
}

func TestGlyphsAreASCII(t *testing.T) {
	for tile, vs := range variants {
		for _, r := range vs {
			if r > 127 {
				t.Errorf("non-ASCII glyph %q for %v", r, tile)
			}
		}
	}
	// Every world tile must have a style and variants.
	for tile := world.DeepWater; tile < world.NumTiles; tile++ {
		if len(variants[tile]) == 0 {
			t.Errorf("no glyphs for tile %d", tile)
		}
		if TerrainName(i18n.FR, tile) == "?" {
			t.Errorf("no name for tile %d", tile)
		}
	}
}

// Every species the server can create must have a look on the client side.
func TestEverySpeciesHasALook(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		l, ok := monsterLooks[kind]
		if !ok {
			t.Errorf("species %q without a look", kind)
			continue
		}
		if l.glyph > 127 || l.glyph == '@' || l.glyph == ' ' {
			t.Errorf("%s: invalid glyph %q", kind, l.glyph)
		}
		if _, ok := monsterStyle[kind]; !ok {
			t.Errorf("%s: no style", kind)
		}
	}
	for kind := range monsterLooks {
		if !slices.Contains(game.SpeciesKinds(), kind) && kind != "guard" && kind != "caravan" { // neither the guard nor the caravan are biome species
			t.Errorf("look for the nonexistent species %q", kind)
		}
	}
}

func TestViewportDrawsMonsters(t *testing.T) {
	w := world.New(1)
	cols, rows := 21, 11
	cx, cy := 100, 100
	mons := []protocol.Monster{
		{ID: 1, Kind: "wolf", X: cx + 4, Y: cy - 1},
		{ID: 2, Kind: "dragon-inconnu", X: cx - 5, Y: cy + 2},
		{ID: 3, Kind: "rat", X: cx + 500, Y: cy}, // off-screen
	}
	lines := plain(Viewport(w, cx, cy, cols, rows, nil, mons, 0))
	if got := lines[rows/2-1][cols/2+4]; got != 'w' {
		t.Errorf("wolf expected at (+4,-1), found %q", got)
	}
	if got := lines[rows/2+2][cols/2-5]; got != '?' {
		t.Errorf("unknown species expected as '?', found %q", got)
	}
}

func TestViewportIgnoresOffscreenMonsters(t *testing.T) {
	w := world.New(1)
	offscreen := []protocol.Monster{{ID: 1, Kind: "rat", X: 600, Y: 100}, {ID: 2, Kind: "wolf", X: 100, Y: -400}}
	with := Viewport(w, 100, 100, 21, 11, nil, offscreen, 0)
	without := Viewport(w, 100, 100, 21, 11, nil, nil, 0)
	if with != without {
		t.Fatal("off-screen monsters changed the rendering")
	}
}

func TestPlayerDrawnOverMonster(t *testing.T) {
	w := world.New(1)
	self := protocol.Entity{ID: 1, Name: "me", X: 50, Y: 50}
	mons := []protocol.Monster{{ID: 1, Kind: "wolf", X: 50, Y: 50}}
	lines := plain(Viewport(w, 50, 50, 11, 5, []protocol.Entity{self}, mons, 1))
	if lines[2][5] != '@' {
		t.Fatalf("the player must take precedence over the monster, found %q", lines[2][5])
	}
}

// A terrain glyph identical to that of a monster or a player would be
// confusing (only the colour would tell them apart).
func TestTerrainGlyphsDoNotCollideWithEntities(t *testing.T) {
	reserved := map[rune]string{'@': "joueur"}
	for kind, l := range monsterLooks {
		reserved[l.glyph] = kind
	}
	for kind, l := range npcLooks {
		reserved[l.glyph] = "PNJ " + string(kind)
	}
	for tile, vs := range variants {
		for _, r := range vs {
			if who, clash := reserved[r]; clash {
				t.Errorf("terrain %d uses %q, already taken by %s", tile, r, who)
			}
		}
	}
}

func TestMonsterNamesMatchServer(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		if got, want := MonsterName(i18n.FR, kind), game.SpeciesName(i18n.FR, kind); got != want {
			t.Errorf("%s: client name %q, server name %q", kind, got, want)
		}
	}
}

func TestFightingEntitiesAreHighlighted(t *testing.T) {
	w := world.New(1)
	self := protocol.Entity{ID: 1, Name: "me", X: 10, Y: 10}
	mon := protocol.Monster{ID: 1, Kind: "rat", X: 11, Y: 10}

	calm := Viewport(w, 10, 10, 15, 7, []protocol.Entity{self}, []protocol.Monster{mon}, 1)
	self.Fighting, mon.Fighting = true, true
	hot := Viewport(w, 10, 10, 15, 7, []protocol.Entity{self}, []protocol.Monster{mon}, 1)

	if calm == hot {
		t.Fatal("fighters are not visually distinguishable")
	}
	// Only the colour changes, never the characters.
	if strings.Join(plain(calm), "\n") != strings.Join(plain(hot), "\n") {
		t.Fatal("the combat marking changed the characters")
	}
}

// Player style indices must never overlap those of tiles:
// otherwise a wall would be displayed with the player's style.
func TestStyleIndicesDoNotCollideWithTiles(t *testing.T) {
	for _, idx := range []int{styleSelf, styleOther, styleFight} {
		if idx < int(world.NumTiles) {
			t.Errorf("style index %d overlaps a tile (NumTiles=%d)", idx, world.NumTiles)
		}
	}
	if styleSelf == styleOther || styleOther == styleFight || styleSelf == styleFight {
		t.Error("two player styles share the same index")
	}
	seen := map[int]string{}
	record := func(i int, what string) {
		if prev, dup := seen[i]; dup {
			t.Errorf("index %d is shared by %s and %s", i, prev, what)
		}
		seen[i] = what
		if i >= len(styles) {
			t.Errorf("%s: index %d outside the styles table (%d)", what, i, len(styles))
		}
	}
	for tile := world.DeepWater; tile < world.NumTiles; tile++ {
		record(int(tile), "tuile")
	}
	record(styleSelf, "joueur local")
	record(styleOther, "autre joueur")
	record(styleFight, "combattant")
	for k, i := range monsterStyle {
		record(i, "monstre "+k)
	}
	for k, i := range npcStyle {
		record(i, "PNJ "+string(k))
	}
}

func TestEveryTileHasStyleGlyphAndName(t *testing.T) {
	for tile := world.DeepWater; tile < world.NumTiles; tile++ {
		if len(variants[tile]) == 0 {
			t.Errorf("tile %d: no glyphs", tile)
		}
		if TerrainName(i18n.FR, tile) == "?" {
			t.Errorf("tile %d: no name", tile)
		}
	}
}

func TestStructuresAreDrawnWithWallsDoorsAndNPCs(t *testing.T) {
	w := world.New(1)
	var village *world.Structure
	for _, s := range w.StructuresNear(w.FindSpawn(), 200) {
		if s.Kind == world.KindVillage {
			village = s
			break
		}
	}
	if village == nil {
		t.Skip("no village near the start")
	}
	c := village.Center()
	lines := plain(Viewport(w, c.X, c.Y, village.W, village.H, nil, nil, 0))
	all := strings.Join(lines, "")
	for _, want := range []string{"#", "+", ":", "_", "H"} {
		if !strings.Contains(all, want) {
			t.Errorf("the drawn village does not contain %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// The healer is drawn exactly in place.
	n := village.NPCs[0]
	row, col := n.Pos.Y-(c.Y-village.H/2), n.Pos.X-(c.X-village.W/2)
	if got := lines[row][col]; got != 'H' {
		t.Errorf("the healer should be at (%d,%d) row %d column %d, found %q", n.Pos.X, n.Pos.Y, row, col, got)
	}
}

func TestNPCNamesAndGlyphs(t *testing.T) {
	if NPCName(i18n.FR, world.NPCMerchant) != "Marchand" || NPCName(i18n.FR, world.NPCHealer) != "Soigneur" {
		t.Error("unexpected NPC names")
	}
	if NPCGlyph(world.NPCMerchant) == NPCGlyph(world.NPCHealer) {
		t.Error("two NPCs share the same glyph")
	}
	if NPCGlyph("inconnu") != 'N' {
		t.Error("unknown NPC: fallback glyph expected")
	}
}

func TestPlayerAndMonsterDrawnOverNPC(t *testing.T) {
	w := world.New(1)
	var npc world.NPC
	for _, s := range w.StructuresNear(w.FindSpawn(), 200) {
		if len(s.NPCs) > 0 {
			npc = s.NPCs[0]
			break
		}
	}
	if npc.Kind == "" {
		t.Skip("no NPC near the start")
	}
	me := protocol.Entity{ID: 1, X: npc.Pos.X, Y: npc.Pos.Y}
	lines := plain(Viewport(w, npc.Pos.X, npc.Pos.Y, 11, 5, []protocol.Entity{me}, nil, 1))
	if lines[2][5] != '@' {
		t.Fatalf("the player must be drawn above the NPC, found %q", lines[2][5])
	}
	mon := protocol.Monster{ID: 1, Kind: "rat", X: npc.Pos.X, Y: npc.Pos.Y}
	lines = plain(Viewport(w, npc.Pos.X, npc.Pos.Y, 11, 5, nil, []protocol.Monster{mon}, 0))
	if lines[2][5] != 'r' {
		t.Fatalf("the monster must be drawn above the NPC, found %q", lines[2][5])
	}
}

func TestReadyQuestGiverShowsQuestionMark(t *testing.T) {
	w := world.New(1)
	var giver world.NPC
	found := false
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.GivesQuests && !found {
				giver, found = n, true
			}
		}
	}
	if !found {
		t.Skip("no giver near the start")
	}
	draw := func(ready map[world.NPCKey]bool) string {
		return ansi.Strip(ViewportReady(w, giver.Pos.X, giver.Pos.Y, 9, 5, nil, nil, 0, ready, nil))
	}
	center := func(s string) rune { return []rune(strings.Split(s, "\n")[2])[4] }
	if got := center(draw(nil)); got != '!' {
		t.Fatalf("giver without a ready quest: %q", got)
	}
	if got := center(draw(map[world.NPCKey]bool{giver.Key(): true})); got != '?' {
		t.Fatalf("giver with a ready quest: %q", got)
	}
	idle := func(k world.NPCKey) bool { return k == giver.Key() }
	if got := center(ansi.Strip(ViewportReady(w, giver.Pos.X, giver.Pos.Y, 9, 5, nil, nil, 0, nil, idle))); got == '!' || got == '?' {
		t.Fatalf("giver with no work: %q", got)
	}
	if got := center(ansi.Strip(ViewportReady(w, giver.Pos.X, giver.Pos.Y, 9, 5, nil, nil, 0, map[world.NPCKey]bool{giver.Key(): true}, idle))); got != '?' {
		t.Fatalf("the ready quest takes precedence over having no work: %q", got)
	}
	other := map[world.NPCKey]bool{{Region: giver.Region, Index: giver.Index + 50}: true}
	if got := center(draw(other)); got != '!' {
		t.Fatalf("another ready quest changes this NPC: %q", got)
	}
}

func TestDungeonWallsOutlineRoomsAndTheRestOfTheRockIsDim(t *testing.T) {
	m := world.NewDungeonMap(&world.Dungeon{Seed: 987654321, Tier: 3})
	terr := DungeonTerrain(m, nil, nil)
	walls, rock := 0, 0
	for y := -2; y < m.H+2; y++ {
		for x := -2; x < m.W+2; x++ {
			got := terr.At(x, y)
			raw := m.TileAt(x, y)
			if raw.Walkable() {
				if got != raw {
					t.Fatalf("(%d,%d): a walkable cell became %v", x, y, got)
				}
				continue
			}
			near := false
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					near = near || m.TileAt(x+dx, y+dy).Walkable()
				}
			}
			switch {
			case near && got != world.Wall:
				t.Fatalf("(%d,%d): rock touching a room should be a wall, not %v", x, y, got)
			case !near && got != world.Rock:
				t.Fatalf("(%d,%d): deep rock should stay muted, not %v", x, y, got)
			}
			if near {
				walls++
			} else {
				rock++
			}
		}
	}
	if walls == 0 || rock == 0 {
		t.Errorf("%d walls, %d rock: both should exist", walls, rock)
	}
	// A never-seen cell stays rock, even touching a room: the fog does not reveal the outline.
	hidden := DungeonTerrain(m, func(int, int) bool { return false }, nil)
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if hidden.At(x, y) != world.Wall && hidden.At(x, y) != world.Rock {
				t.Fatalf("(%d,%d): %v visible in the fog", x, y, hidden.At(x, y))
			}
			if hidden.At(x, y) != world.Wall {
				continue
			}
			t.Fatalf("(%d,%d): a wall appears in a never-seen area", x, y)
		}
	}
}

// Every climate dresses all terrains, in ASCII, with no glyph reserved for an entity.
func TestClimatesAreCompleteAndDoNotCollide(t *testing.T) {
	reserved := map[rune]bool{'@': true}
	for _, l := range monsterLooks {
		reserved[l.glyph] = true
	}
	for _, l := range npcLooks {
		reserved[l.glyph] = true
	}
	seen := map[int]bool{}
	for c := world.Climate(0); c < world.NumClimates; c++ {
		for tile := world.Tile(0); tile < world.NumTiles; tile++ {
			st := styleFor(c, tile)
			if st < 0 || st >= len(styles) {
				t.Fatalf("climate %s, tile %d: style %d outside the table", c.Key(), tile, st)
			}
			if c != world.Temperate && st < int(world.NumTiles) {
				t.Errorf("climate %s, tile %d: base style reused", c.Key(), tile)
			}
			if seen[st] && c != world.Temperate {
				t.Errorf("style %d shared between two climates", st)
			}
			seen[st] = true
		}
		for tile, vs := range climateVariants[c] {
			for _, r := range vs {
				if r > 126 || r < 32 || reserved[r] {
					t.Errorf("climate %s, tile %d: forbidden glyph %q", c.Key(), tile, r)
				}
			}
		}
	}
}

// A tropical forest does not have the same colour as an arid one; temperate keeps the original rendering.
func TestClimateChangesColoursNotTheMap(t *testing.T) {
	tropical, arid := styleFor(world.Tropical, world.Forest), styleFor(world.Arid, world.Forest)
	if styles[tropical].Render("T") == styles[arid].Render("T") {
		t.Error("two climates render the forest the same")
	}
	if styleFor(world.Temperate, world.Forest) != int(world.Forest) {
		t.Error("temperate must keep the original style")
	}
	if TileHex(world.Temperate, world.Forest) != "" || TileHex(world.Arid, world.Plain) == "" {
		t.Error("TileHex")
	}
	w := world.New(7)
	out := Viewport(w, 0, 0, 40, 10, nil, nil, 0)
	if len(plain(out)) != 10 {
		t.Fatalf("%d lines", len(plain(out)))
	}
}

func TestWithSpentShowsBareTerrain(t *testing.T) {
	w := world.New(1)
	var node world.Pos
	found := false
	for y := -150; y < 150 && !found; y++ {
		for x := -150; x < 150; x++ {
			if w.At(x, y) == world.TreeNode {
				node, found = world.Pos{X: x, Y: y}, true
				break
			}
		}
	}
	if !found {
		t.Skip("no tree near the origin")
	}
	spent := map[[2]int]bool{{node.X, node.Y}: true}
	tt := WithSpent(w, spent, w.Terrain)
	if got, want := tt.At(node.X, node.Y), w.Terrain(node.X, node.Y); got != want {
		t.Errorf("depleted node: %d, expected bare terrain %d", got, want)
	}
	if WithSpent(w, nil, w.Terrain) != Terrain(w) {
		t.Error("without a depleted node, the terrain must stay as is")
	}
}

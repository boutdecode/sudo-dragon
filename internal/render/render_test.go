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
			t.Fatalf("%v: %d lignes, attendu %d", d, len(lines), d[1])
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != d[0] {
				t.Fatalf("%v ligne %d: largeur %d, attendu %d", d, i, got, d[0])
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
		t.Fatalf("joueur local absent du centre: %q", lines[rows/2])
	}
	if lines[rows/2-2][cols/2+3] != '@' {
		t.Fatalf("voisin mal placé:\n%s", strings.Join(lines, "\n"))
	}
	if n := strings.Count(strings.Join(lines, ""), "@"); n != 2 {
		t.Fatalf("%d '@' dessinés, attendu 2 (l'entité lointaine ne doit pas apparaître)", n)
	}
}

func TestViewportDeterministic(t *testing.T) {
	a := Viewport(world.New(5), 0, 0, 60, 20, nil, nil, 0)
	b := Viewport(world.New(5), 0, 0, 60, 20, nil, nil, 0)
	if a != b {
		t.Fatal("rendu non déterministe")
	}
}

func TestGlyphsAreASCII(t *testing.T) {
	for tile, vs := range variants {
		for _, r := range vs {
			if r > 127 {
				t.Errorf("glyphe non ASCII %q pour %v", r, tile)
			}
		}
	}
	// Chaque tuile du monde doit avoir un style et des variantes.
	for tile := world.DeepWater; tile < world.NumTiles; tile++ {
		if len(variants[tile]) == 0 {
			t.Errorf("pas de glyphes pour la tuile %d", tile)
		}
		if TerrainName(i18n.FR, tile) == "?" {
			t.Errorf("pas de nom pour la tuile %d", tile)
		}
	}
}

// Chaque espèce que le serveur peut créer doit avoir une apparence côté client.
func TestEverySpeciesHasALook(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		l, ok := monsterLooks[kind]
		if !ok {
			t.Errorf("espèce %q sans apparence", kind)
			continue
		}
		if l.glyph > 127 || l.glyph == '@' || l.glyph == ' ' {
			t.Errorf("%s: glyphe %q invalide", kind, l.glyph)
		}
		if _, ok := monsterStyle[kind]; !ok {
			t.Errorf("%s: pas de style", kind)
		}
	}
	for kind := range monsterLooks {
		if !slices.Contains(game.SpeciesKinds(), kind) {
			t.Errorf("apparence pour l'espèce inexistante %q", kind)
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
		{ID: 3, Kind: "rat", X: cx + 500, Y: cy}, // hors cadre
	}
	lines := plain(Viewport(w, cx, cy, cols, rows, nil, mons, 0))
	if got := lines[rows/2-1][cols/2+4]; got != 'w' {
		t.Errorf("loup attendu en (+4,-1), trouvé %q", got)
	}
	if got := lines[rows/2+2][cols/2-5]; got != '?' {
		t.Errorf("espèce inconnue attendue sous la forme '?', trouvé %q", got)
	}
}

func TestViewportIgnoresOffscreenMonsters(t *testing.T) {
	w := world.New(1)
	offscreen := []protocol.Monster{{ID: 1, Kind: "rat", X: 600, Y: 100}, {ID: 2, Kind: "wolf", X: 100, Y: -400}}
	with := Viewport(w, 100, 100, 21, 11, nil, offscreen, 0)
	without := Viewport(w, 100, 100, 21, 11, nil, nil, 0)
	if with != without {
		t.Fatal("des monstres hors cadre ont modifié le rendu")
	}
}

func TestPlayerDrawnOverMonster(t *testing.T) {
	w := world.New(1)
	self := protocol.Entity{ID: 1, Name: "me", X: 50, Y: 50}
	mons := []protocol.Monster{{ID: 1, Kind: "wolf", X: 50, Y: 50}}
	lines := plain(Viewport(w, 50, 50, 11, 5, []protocol.Entity{self}, mons, 1))
	if lines[2][5] != '@' {
		t.Fatalf("le joueur doit primer sur le monstre, trouvé %q", lines[2][5])
	}
}

// Un glyphe de terrain identique à celui d'un monstre ou d'un joueur prêterait
// à confusion (seule la couleur les distinguerait).
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
				t.Errorf("le terrain %d utilise %q, déjà pris par %s", tile, r, who)
			}
		}
	}
}

func TestMonsterNamesMatchServer(t *testing.T) {
	for _, kind := range game.SpeciesKinds() {
		if got, want := MonsterName(i18n.FR, kind), game.SpeciesName(i18n.FR, kind); got != want {
			t.Errorf("%s: nom client %q, nom serveur %q", kind, got, want)
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
		t.Fatal("les combattants ne se distinguent pas visuellement")
	}
	// Seule la couleur change, jamais les caractères.
	if strings.Join(plain(calm), "\n") != strings.Join(plain(hot), "\n") {
		t.Fatal("le marquage de combat a modifié les caractères")
	}
}

// Les indices de style des joueurs ne doivent jamais recouvrir ceux des tuiles:
// sinon un mur s'afficherait avec le style du joueur.
func TestStyleIndicesDoNotCollideWithTiles(t *testing.T) {
	for _, idx := range []int{styleSelf, styleOther, styleFight} {
		if idx < int(world.NumTiles) {
			t.Errorf("l'indice de style %d recouvre une tuile (NumTiles=%d)", idx, world.NumTiles)
		}
	}
	if styleSelf == styleOther || styleOther == styleFight || styleSelf == styleFight {
		t.Error("deux styles de joueurs partagent le même indice")
	}
	seen := map[int]string{}
	record := func(i int, what string) {
		if prev, dup := seen[i]; dup {
			t.Errorf("l'indice %d est partagé par %s et %s", i, prev, what)
		}
		seen[i] = what
		if i >= len(styles) {
			t.Errorf("%s: indice %d hors de la table de styles (%d)", what, i, len(styles))
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
			t.Errorf("tuile %d: pas de glyphes", tile)
		}
		if TerrainName(i18n.FR, tile) == "?" {
			t.Errorf("tuile %d: pas de nom", tile)
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
		t.Skip("pas de village près du départ")
	}
	c := village.Center()
	lines := plain(Viewport(w, c.X, c.Y, village.W, village.H, nil, nil, 0))
	all := strings.Join(lines, "")
	for _, want := range []string{"#", "+", ":", "_", "H"} {
		if !strings.Contains(all, want) {
			t.Errorf("le village dessiné ne contient pas %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// Le soigneur est dessiné exactement à sa place.
	n := village.NPCs[0]
	row, col := n.Pos.Y-(c.Y-village.H/2), n.Pos.X-(c.X-village.W/2)
	if got := lines[row][col]; got != 'H' {
		t.Errorf("le soigneur devrait être en (%d,%d) ligne %d colonne %d, trouvé %q", n.Pos.X, n.Pos.Y, row, col, got)
	}
}

func TestNPCNamesAndGlyphs(t *testing.T) {
	if NPCName(i18n.FR, world.NPCMerchant) != "Marchand" || NPCName(i18n.FR, world.NPCHealer) != "Soigneur" {
		t.Error("noms de PNJ inattendus")
	}
	if NPCGlyph(world.NPCMerchant) == NPCGlyph(world.NPCHealer) {
		t.Error("deux PNJ partagent le même glyphe")
	}
	if NPCGlyph("inconnu") != 'N' {
		t.Error("PNJ inconnu: glyphe de secours attendu")
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
		t.Skip("pas de PNJ près du départ")
	}
	me := protocol.Entity{ID: 1, X: npc.Pos.X, Y: npc.Pos.Y}
	lines := plain(Viewport(w, npc.Pos.X, npc.Pos.Y, 11, 5, []protocol.Entity{me}, nil, 1))
	if lines[2][5] != '@' {
		t.Fatalf("le joueur doit être dessiné au-dessus du PNJ, trouvé %q", lines[2][5])
	}
	mon := protocol.Monster{ID: 1, Kind: "rat", X: npc.Pos.X, Y: npc.Pos.Y}
	lines = plain(Viewport(w, npc.Pos.X, npc.Pos.Y, 11, 5, nil, []protocol.Monster{mon}, 0))
	if lines[2][5] != 'r' {
		t.Fatalf("le monstre doit être dessiné au-dessus du PNJ, trouvé %q", lines[2][5])
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
		t.Skip("pas de donneur près du départ")
	}
	draw := func(ready map[world.NPCKey]bool) string {
		return ansi.Strip(ViewportReady(w, giver.Pos.X, giver.Pos.Y, 9, 5, nil, nil, 0, ready))
	}
	center := func(s string) rune { return []rune(strings.Split(s, "\n")[2])[4] }
	if got := center(draw(nil)); got != '!' {
		t.Fatalf("donneur sans quête prête : %q", got)
	}
	if got := center(draw(map[world.NPCKey]bool{giver.Key(): true})); got != '?' {
		t.Fatalf("donneur avec quête prête : %q", got)
	}
	other := map[world.NPCKey]bool{{Region: giver.Region, Index: giver.Index + 50}: true}
	if got := center(draw(other)); got != '!' {
		t.Fatalf("une autre quête prête change ce PNJ : %q", got)
	}
}

package world

import "testing"

func TestDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	for _, cp := range []ChunkPos{{0, 0}, {-3, 5}, {100, -100}} {
		if *a.Chunk(cp) != *b.Chunk(cp) {
			t.Fatalf("chunk %v diffère entre deux mondes de même seed", cp)
		}
	}
}

func TestSeedsDiffer(t *testing.T) {
	if *New(1).Chunk(ChunkPos{0, 0}) == *New(2).Chunk(ChunkPos{0, 0}) {
		t.Fatal("deux seeds différentes donnent le même chunk")
	}
}

func TestChunkOfNegative(t *testing.T) {
	cases := map[Pos]ChunkPos{
		{0, 0}:                   {0, 0},
		{ChunkSize - 1, 0}:       {0, 0},
		{ChunkSize, 0}:           {1, 0},
		{-1, -1}:                 {-1, -1},
		{-ChunkSize, -ChunkSize}: {-1, -1},
		{-ChunkSize - 1, 0}:      {-2, 0},
	}
	for p, want := range cases {
		if got := ChunkOf(p.X, p.Y); got != want {
			t.Errorf("ChunkOf(%v) = %v, want %v", p, got, want)
		}
	}
}

func TestAtMatchesTileAt(t *testing.T) {
	w := New(7)
	for _, p := range []Pos{{0, 0}, {-5, 17}, {-40, -33}, {123, 456}} {
		if w.At(p.X, p.Y) != w.TileAt(p.X, p.Y) {
			t.Errorf("At/TileAt divergent en %v", p)
		}
	}
}

func TestSpawnWalkable(t *testing.T) {
	w := New(99)
	p := w.FindSpawn()
	if !w.At(p.X, p.Y).Walkable() {
		t.Fatalf("spawn %v non praticable", p)
	}
}

func TestGlyphsAreDistinctPrintableASCII(t *testing.T) {
	seen := map[rune]Tile{}
	for tile := DeepWater; tile < NumTiles; tile++ {
		g := tile.Glyph()
		if g < 33 || g > 126 || g == '?' {
			t.Errorf("tuile %d: glyphe %q non ASCII imprimable (ou inconnu)", tile, g)
		}
		if other, dup := seen[g]; dup {
			t.Errorf("les tuiles %d et %d partagent le glyphe %q", other, tile, g)
		}
		seen[g] = tile
	}
}

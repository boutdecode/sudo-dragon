package world

import "testing"

func TestLevelAtGrowsWithDistanceAndIsBounded(t *testing.T) {
	t.Parallel()
	if LevelAt(0, 0) != 1 || LevelAt(LevelBandSize-1, 0) != LevelsPerBand {
		t.Errorf("band 1: %d, %d", LevelAt(0, 0), LevelAt(LevelBandSize-1, 0))
	}
	if LevelAt(LevelBandSize, 0) != LevelsPerBand+1 {
		t.Errorf("start of band 2: %d", LevelAt(LevelBandSize, 0))
	}
	if LevelAt(100000, -100000) != MaxLevel {
		t.Errorf("ceiling: %d", LevelAt(100000, -100000))
	}
	prev := 0
	for d := 0; d < 4*LevelBandSize; d += 7 {
		l := LevelAt(d, 0)
		if l < prev || l != LevelAt(-d, d/2) {
			t.Fatalf("d=%d: level %d after %d", d, l, prev)
		}
		prev = l
	}
}

func TestLevelBands(t *testing.T) {
	t.Parallel()
	for l, want := range map[int][2]int{1: {1, 5}, 5: {1, 5}, 6: {6, 10}, 12: {11, 15}, 20: {16, 20}} {
		lo, hi := LevelRange(l)
		if lo != want[0] || hi != want[1] {
			t.Errorf("level %d: %d-%d, want %v", l, lo, hi, want)
		}
	}
	if LevelBand(1) != 0 || LevelBand(16) != 3 || LevelBand(20) != 3 {
		t.Error("bands")
	}
}

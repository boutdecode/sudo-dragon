package resource

import "testing"

func TestJobCurveIsMonotoneAndBounded(t *testing.T) {
	prevCap, prevChance := 0, 0
	for l := 1; l <= MaxJobLevel; l++ {
		if c := Capacity(l); c <= prevCap {
			t.Errorf("capacity %d at level %d, not increasing", c, l)
		} else {
			prevCap = c
		}
		if c := Chance(l); c <= prevChance || c > maxChance {
			t.Errorf("chance %d at level %d", c, l)
		} else {
			prevChance = c
		}
	}
	if Capacity(1) != 5 || Chance(1) != 40 {
		t.Errorf("start: capacity %d, chance %d", Capacity(1), Chance(1))
	}
	if Capacity(99) != Capacity(MaxJobLevel) || Chance(0) != Chance(1) {
		t.Error("out-of-range level not clamped")
	}
}

func TestLevelOfMatchesProgress(t *testing.T) {
	if LevelOf(0) != 1 || LevelOf(XPToNext(1)-1) != 1 || LevelOf(XPToNext(1)) != 2 {
		t.Fatal("first tier")
	}
	cum := 0
	for l := 1; l < MaxJobLevel; l++ {
		cum += XPToNext(l)
		if LevelOf(cum-1) != l || LevelOf(cum) != l+1 {
			t.Errorf("threshold of level %d", l+1)
		}
	}
	lv, in, next := Progress(XPToNext(1) + 7)
	if lv != 2 || in != 7 || next != XPToNext(2) {
		t.Errorf("progress %d %d %d", lv, in, next)
	}
	if lv, _, next := Progress(1 << 30); lv != MaxJobLevel || next != 0 {
		t.Errorf("max level: %d, next %d", lv, next)
	}
}

func TestGainXPScalesWithTier(t *testing.T) {
	if GainXP(1) >= GainXP(4) || GainXP(0) != GainXP(1) {
		t.Error("XP per tier")
	}
}

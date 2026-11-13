package resource

import "testing"

func TestJobCurveIsMonotoneAndBounded(t *testing.T) {
	prevCap, prevChance := 0, 0
	for l := 1; l <= MaxJobLevel; l++ {
		if c := Capacity(l); c <= prevCap {
			t.Errorf("capacité %d au niveau %d, pas croissante", c, l)
		} else {
			prevCap = c
		}
		if c := Chance(l); c <= prevChance || c > maxChance {
			t.Errorf("chance %d au niveau %d", c, l)
		} else {
			prevChance = c
		}
	}
	if Capacity(1) != 5 || Chance(1) != 40 {
		t.Errorf("départ: capacité %d, chance %d", Capacity(1), Chance(1))
	}
	if Capacity(99) != Capacity(MaxJobLevel) || Chance(0) != Chance(1) {
		t.Error("niveau hors bornes non ramené")
	}
}

func TestLevelOfMatchesProgress(t *testing.T) {
	if LevelOf(0) != 1 || LevelOf(XPToNext(1)-1) != 1 || LevelOf(XPToNext(1)) != 2 {
		t.Fatal("premier palier")
	}
	cum := 0
	for l := 1; l < MaxJobLevel; l++ {
		cum += XPToNext(l)
		if LevelOf(cum-1) != l || LevelOf(cum) != l+1 {
			t.Errorf("seuil du niveau %d", l+1)
		}
	}
	lv, in, next := Progress(XPToNext(1) + 7)
	if lv != 2 || in != 7 || next != XPToNext(2) {
		t.Errorf("progression %d %d %d", lv, in, next)
	}
	if lv, _, next := Progress(1 << 30); lv != MaxJobLevel || next != 0 {
		t.Errorf("niveau max: %d, suivant %d", lv, next)
	}
}

func TestGainXPScalesWithTier(t *testing.T) {
	if GainXP(1) >= GainXP(4) || GainXP(0) != GainXP(1) {
		t.Error("XP par palier")
	}
}

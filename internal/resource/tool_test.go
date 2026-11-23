package resource

import "testing"

func TestToolCatalogue(t *testing.T) {
	prev := Tool{}
	for _, tl := range Tools() {
		if tl.Tier != prev.Tier+1 {
			t.Fatalf("tiers must be consecutive: %d after %d", tl.Tier, prev.Tier)
		}
		if tl.MinLevel < 1 || tl.MinLevel > MaxJobLevel {
			t.Errorf("tier %d: required level %d outside 1..%d", tl.Tier, tl.MinLevel, MaxJobLevel)
		}
		if tl.MinLevel < prev.MinLevel || tl.Price <= prev.Price || tl.Bonus < prev.Bonus {
			t.Errorf("tier %d must not be worse than tier %d", tl.Tier, prev.Tier)
		}
		prev = tl
	}
	if first, _ := ToolTier(1); first.Bonus != 0 || first.MinLevel != 1 {
		t.Errorf("the basic tool must give no bonus and need no level: %+v", first)
	}
}

func TestYield(t *testing.T) {
	if Yield(0) != 0 || Yield(NumToolTiers+1) != 0 || Yield(-1) != 0 {
		t.Error("no tool, no yield")
	}
	if Yield(1) != 1 {
		t.Errorf("basic tool yields 1, got %d", Yield(1))
	}
	if Yield(NumToolTiers) != 4 {
		t.Errorf("top tool yields 4, got %d", Yield(NumToolTiers))
	}
}

func TestNeedsTool(t *testing.T) {
	for _, c := range []Category{Wood, Ore, Herb} {
		if !NeedsTool(c) {
			t.Errorf("%s needs a tool", c.Key())
		}
	}
	if NeedsTool(Fish) {
		t.Error("fishing needs no tool")
	}
}

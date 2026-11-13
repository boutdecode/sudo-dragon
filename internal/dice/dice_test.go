package dice

import (
	"math/rand/v2"
	"testing"
)

func TestStringAndParseRoundTrip(t *testing.T) {
	for _, d := range []Dice{D(1, 6, 0), D(2, 6, 2), D(3, 4, -1), D(0, 0, 5), D(5, 12, 5)} {
		got, ok := Parse(d.String())
		if !ok || got != d {
			t.Errorf("%v -> %q -> %v (%v)", d, d.String(), got, ok)
		}
	}
	if D(2, 6, 2).String() != "2d6+2" || D(3, 4, 0).String() != "3d4" || D(1, 8, -1).String() != "1d8-1" {
		t.Fatal("notation")
	}
}

func TestParseIsTolerantAndBounded(t *testing.T) {
	for in, want := range map[string]Dice{" 2D6 + 2 ": D(2, 6, 2), "d8": D(1, 8, 0), "3d4-1": D(3, 4, -1), "7": D(0, 0, 7), "999d999+999": D(MaxCount, MaxSides, MaxBonus)} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %v %v, expected %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "d", "2d", "d0", "abc", "2d6+", "1d6+x", "-d6", "1d6d6"} {
		if _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestRollStaysInRangeAndCritDoublesDiceNotBonus(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	d := D(2, 6, 3)
	sum := 0
	for range 4000 {
		v := d.Roll(rng)
		if v < d.Min() || v > d.Max() {
			t.Fatalf("out of bounds: %d", v)
		}
		sum += v
		c := d.RollCrit(rng)
		if c < 4+3 || c > 24+3 {
			t.Fatalf("critical out of bounds: %d", c)
		}
	}
	if avg := float64(sum) / 4000; avg < 9.8 || avg > 10.2 {
		t.Fatalf("average %.2f, expected 10", avg)
	}
	if d.Avg() != 10 || d.Min() != 5 || d.Max() != 15 {
		t.Fatalf("min/avg/max: %d %d %d", d.Min(), d.Avg(), d.Max())
	}
	if (Dice{}).Roll(rng) != 0 || !(Dice{}).IsZero() {
		t.Fatal("zero")
	}
}

func FuzzParse(f *testing.F) {
	f.Add("2d6+2")
	f.Add("\x00d\xff")
	f.Fuzz(func(t *testing.T, s string) {
		d, ok := Parse(s)
		if !ok {
			return
		}
		if d.N < 0 || d.N > MaxCount || d.Sides < 0 || d.Sides > MaxSides || d.Bonus < -MaxBonus || d.Bonus > MaxBonus {
			t.Fatalf("out of bounds: %+v", d)
		}
		if back, ok := Parse(d.String()); !ok || back != d {
			t.Fatalf("round trip: %+v -> %q -> %+v", d, d.String(), back)
		}
	})
}

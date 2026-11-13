package protocol

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// namePool: valid names (Unicode letters, digits, _ and -).
var namePool = []string{"alice", "Bob_2", "zoé", "Zoë-9", "日本語", "x-y_z", "Ünïcödé", "a1b2c3"}

func randSnapshot(r *rand.Rand) *Snapshot {
	ent := func() Entity {
		return Entity{
			ID: r.IntN(100000), Name: namePool[r.IntN(len(namePool))],
			X: r.IntN(2_000_001) - 1_000_000, Y: r.IntN(2_000_001) - 1_000_000,
			Fighting: r.IntN(4) == 0,
		}
	}
	s := &Snapshot{
		Tick: r.Uint64N(1 << 40),
		Self: ent(),
		Stats: Stats{
			Level: r.IntN(60), XP: r.IntN(900), XPNext: r.IntN(900), HP: r.IntN(500),
			MaxHP: r.IntN(500), Def: r.IntN(200), Hit: r.IntN(20) - 5, Dmg: []string{"1d4", "2d6+3", "5d12+5", "1d4-1", "\"q\\n"}[r.IntN(5)], Potions: r.IntN(10),
			Gold: r.IntN(1_000_000),
		},
		Online: r.IntN(500),
	}
	for i, n := 0, r.IntN(40); i < n; i++ {
		s.Others = append(s.Others, ent())
	}
	for i, n := 0, r.IntN(20); i < n; i++ {
		s.Monsters = append(s.Monsters, Monster{
			ID: r.IntN(5000), Kind: []string{"rat", "wolf", "crab", "goblin", "yeti"}[r.IntN(5)],
			X: r.IntN(2001) - 1000, Y: r.IntN(2001) - 1000, Fighting: r.IntN(3) == 0,
		})
	}
	return s
}

// The hand-written encoder must produce exactly the same bytes as encoding/json.
func TestFastSnapshotMatchesEncodingJSON(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < 3000; i++ {
		s := randSnapshot(r)
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		got := s.AppendJSON(nil)
		if !bytes.Equal(got, want) {
			t.Fatalf("divergence (case %d)\n manual  : %s\n standard: %s", i, got, want)
		}
	}
}

// Same for the full envelope produced by Encode.
func TestEncodeEnvelopeMatchesEncodingJSON(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	for i := 0; i < 500; i++ {
		s := randSnapshot(r)
		got, err := Encode(TSnapshot, s)
		if err != nil {
			t.Fatal(err)
		}
		inner, _ := json.Marshal(s)
		want, _ := json.Marshal(Envelope{T: TSnapshot, D: inner})
		if !bytes.Equal(got, want) {
			t.Fatalf("envelope differs (case %d)\n manual  : %s\n standard: %s", i, got, want)
		}
	}
}

// And the result reads back into a Snapshot identical to the original.
func TestFastSnapshotRoundTrips(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 500; i++ {
		s := randSnapshot(r)
		data, err := Encode(TSnapshot, s)
		if err != nil {
			t.Fatal(err)
		}
		env, err := Decode(data)
		if err != nil || env.T != TSnapshot {
			t.Fatalf("decoding the envelope: %v (%q)", err, env.T)
		}
		var back Snapshot
		if err := env.Payload(&back); err != nil {
			t.Fatal(err)
		}
		// Empty slices are omitted: normalize before comparing.
		if len(s.Others) == 0 {
			s.Others = nil
		}
		if len(s.Monsters) == 0 {
			s.Monsters = nil
		}
		if !reflect.DeepEqual(*s, back) {
			t.Fatalf("round trip differs (case %d)\n before: %+v\n after: %+v", i, *s, back)
		}
	}
}

func TestSnapshotWithoutOthersOrMonsters(t *testing.T) {
	s := &Snapshot{Tick: 1, Self: Entity{ID: 1, Name: "a"}}
	want, _ := json.Marshal(s)
	if got := s.AppendJSON(nil); !bytes.Equal(got, want) {
		t.Fatalf("manual  : %s\nstandard: %s", got, want)
	}
	if strings.Contains(string(want), "others") || strings.Contains(string(want), "monsters") {
		t.Fatal("wrong test assumption: empty slices must be omitted")
	}
}

// Adversarial strings: escaping must remain valid JSON that reads back
// identically (the name is validated upstream, but the encoder must not depend on it).
func TestAppendStringEscaping(t *testing.T) {
	cases := []string{
		"", "simple", `guillemet " et \ antislash`, "ligne\nsuivante\ttab\rcr",
		"<script>&amp;</script>", "\x00\x01\x1f contrôles", "é à ü 日本 😀",
		"séparateurs    ", "octet invalide \xff\xfe fin", "\u007f",
	}
	for _, in := range cases {
		out := appendString(nil, in)
		var back string
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("%q: invalid JSON %s: %v", in, out, err)
		}
		// encoding/json replaces invalid bytes with U+FFFD; we expect the same.
		wantBytes, _ := json.Marshal(in)
		var want string
		_ = json.Unmarshal(wantBytes, &want)
		if back != want {
			t.Errorf("%q: read back %q, expected %q", in, back, want)
		}
		if !bytes.Equal(out, wantBytes) {
			t.Errorf("%q: bytes differ\n manual  : %s\n standard: %s", in, out, wantBytes)
		}
	}
}

func BenchmarkEncodeSnapshot250(b *testing.B) {
	s := &Snapshot{Tick: 1, Self: Entity{ID: 1, Name: "bot001"}}
	for i := 0; i < 250; i++ {
		s.Others = append(s.Others, Entity{ID: i, Name: "bot042", X: i, Y: -i})
	}
	b.Run("manuel", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := Encode(TSnapshot, s); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reflexion", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			inner, _ := json.Marshal(s)
			if _, err := json.Marshal(Envelope{T: TSnapshot, D: inner}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestCombatStrikesRoundTrip(t *testing.T) {
	in := Combat{Enemies: []Enemy{{Kind: "rat", Name: "Rat", HP: 3, MaxHP: 8}}, Strikes: []Strike{{ByPlayer: true, Foe: 0, Dmg: 5, Hit: true, Crit: true}, {Foe: 0}}}
	data, err := Encode(TCombat, in)
	if err != nil {
		t.Fatal(err)
	}
	env, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	var out Combat
	if err := env.Payload(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Strikes) != 2 || out.Strikes[0] != in.Strikes[0] || out.Strikes[1] != in.Strikes[1] {
		t.Fatalf("round trip: %+v", out.Strikes)
	}
}

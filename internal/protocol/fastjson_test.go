package protocol

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// namePool: pseudos valides (lettres Unicode, chiffres, _ et -).
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
			MaxHP: r.IntN(500), Atk: r.IntN(200), Def: r.IntN(200), Potions: r.IntN(10),
			Gold: r.IntN(1_000_000),
		},
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

// L'encodeur manuel doit produire exactement les mêmes octets qu'encoding/json.
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
			t.Fatalf("divergence (cas %d)\n manuel : %s\n standard: %s", i, got, want)
		}
	}
}

// Idem pour l'enveloppe complète produite par Encode.
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
			t.Fatalf("enveloppe différente (cas %d)\n manuel : %s\n standard: %s", i, got, want)
		}
	}
}

// Et le résultat se relit en un Snapshot identique à l'original.
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
			t.Fatalf("décodage de l'enveloppe: %v (%q)", err, env.T)
		}
		var back Snapshot
		if err := env.Payload(&back); err != nil {
			t.Fatal(err)
		}
		// Les tranches vides sont omises: on normalise avant de comparer.
		if len(s.Others) == 0 {
			s.Others = nil
		}
		if len(s.Monsters) == 0 {
			s.Monsters = nil
		}
		if !reflect.DeepEqual(*s, back) {
			t.Fatalf("aller-retour différent (cas %d)\n avant: %+v\n après: %+v", i, *s, back)
		}
	}
}

func TestSnapshotWithoutOthersOrMonsters(t *testing.T) {
	s := &Snapshot{Tick: 1, Self: Entity{ID: 1, Name: "a"}}
	want, _ := json.Marshal(s)
	if got := s.AppendJSON(nil); !bytes.Equal(got, want) {
		t.Fatalf("manuel : %s\nstandard: %s", got, want)
	}
	if strings.Contains(string(want), "others") || strings.Contains(string(want), "monsters") {
		t.Fatal("hypothèse du test fausse: les tranches vides doivent être omises")
	}
}

// Chaînes adverses: l'échappement doit rester du JSON valide qui se relit à
// l'identique (le pseudo est validé en amont, mais l'encodeur ne doit pas en dépendre).
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
			t.Fatalf("%q: JSON invalide %s: %v", in, out, err)
		}
		// encoding/json remplace les octets invalides par U+FFFD; on attend la même chose.
		wantBytes, _ := json.Marshal(in)
		var want string
		_ = json.Unmarshal(wantBytes, &want)
		if back != want {
			t.Errorf("%q: relu %q, attendu %q", in, back, want)
		}
		if !bytes.Equal(out, wantBytes) {
			t.Errorf("%q: octets différents\n manuel : %s\n standard: %s", in, out, wantBytes)
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

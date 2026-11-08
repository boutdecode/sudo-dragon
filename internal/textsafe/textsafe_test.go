package textsafe

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestCleanKeepsOrdinaryText(t *testing.T) {
	for _, in := range []string{
		"Bonjour, voyageur !",
		"Je viens de Valdurne, où l'hiver dure sept mois.",
		"Éloïse ôte son chapeau — très élégant ✓",
		"日本語のテキスト",
		"emoji 😀 ok",
		"a",
	} {
		if got := Clean(in, 0); got != in {
			t.Errorf("Clean(%q) = %q, aucune modification attendue", in, got)
		}
	}
}

func TestCleanStripsEscapeSequencesCompletely(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mrouge\x1b[0m":                  "rouge",
		"avant\x1b[2Japrès":                     "avantaprès",
		"\x1b[1;38;5;196mtexte":                 "texte",
		"titre\x1b]0;PIRATÉ\x07fin":             "titrefin",
		"titre\x1b]0;PIRATÉ\x1b\\fin":           "titrefin",
		"presse-papiers\x1b]52;c;SGVsbG8=\x07!": "presse-papiers!",
		"\x1bc reset":                           "reset",
		"fin\x1b":                               "fin",
		"x\x1b[":                                "x",
		"\u009b31mrouge":                        "rouge",
		"a\x1bPdcs data\x1b\\b":                 "ab",
	}
	for in, want := range cases {
		if got := Clean(in, 0); got != want {
			t.Errorf("Clean(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestCleanRemovesControlsAndInvisibles(t *testing.T) {
	cases := map[string]string{
		"a\x00b\x07c\x7fd":                               "abcd",
		"ligne1\nligne2\r\nligne3":                       "ligne1 ligne2 ligne3",
		"tab\tsépare":                                    "tab sépare",
		"sép\u2028aré\u2029":                             "sép aré",
		"RTL\u202eabc\u202c":                             "RTLabc", // inversion de sens d'écriture
		"iso\u2066lé\u2069":                              "isolé",
		"zéro\u200blargeur\u200d" + string(rune(0xFEFF)): "zérolargeur",
		"trait\u00adunion":                               "traitunion", // trait d'union conditionnel
		"privé\ue000usage":                               "privéusage",
		"espace\u00a0insécable":                          "espace insécable",
		"\u0085nel":                                      "nel",
	}
	for in, want := range cases {
		if got := Clean(in, 0); got != want {
			t.Errorf("Clean(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestCleanCollapsesAndTrimsSpaces(t *testing.T) {
	if got := Clean("   un    deux \t\n  trois   ", 0); got != "un deux trois" {
		t.Fatalf("got %q", got)
	}
	if got := Clean("   \n\t  ", 0); got != "" {
		t.Fatalf("texte vide attendu, got %q", got)
	}
	if got := Clean("", 10); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanDropsInvalidUTF8(t *testing.T) {
	got := Clean("ok\xff\xfe bon\xc3", 0)
	if !utf8.ValidString(got) || got != "ok bon" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanLimitsCombiningMarks(t *testing.T) {
	zalgo := "a" + strings.Repeat("\u0301\u0302\u0303\u0304", 20) + "b"
	got := Clean(zalgo, 0)
	if want := "a\u0301\u0302b"; got != want {
		t.Fatalf("got %q, attendu %q", got, want)
	}
	// Des signes isolés en tête n'ont pas de caractère de base.
	if got := Clean("\u0301\u0302abc", 0); got != "abc" {
		t.Fatalf("got %q", got)
	}
	// Un accent normal (e + accent combinant) reste intact.
	if got := Clean("e\u0301te\u0301", 0); got != "e\u0301te\u0301" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanTruncatesOnRuneBoundaries(t *testing.T) {
	if got := Clean("éèêëàâäôö", 4); got != "éèêë" {
		t.Fatalf("got %q", got)
	}
	if got := Clean("日本語のテキスト", 3); got != "日本語" {
		t.Fatalf("got %q", got)
	}
	// Ne se termine jamais sur une espace.
	if got := Clean("abc def ghi", 4); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := Clean("abc def", 7); got != "abc def" {
		t.Fatalf("got %q", got)
	}
	if got := Clean("abc def", 6); strings.HasSuffix(got, " ") || utf8.RuneCountInString(got) > 6 {
		t.Fatalf("got %q", got)
	}
	// maxRunes <= 0: pas de limite.
	long := strings.Repeat("x", 5000)
	if got := Clean(long, 0); got != long {
		t.Fatal("texte long tronqué sans limite demandée")
	}
}

func TestValid(t *testing.T) {
	if !Valid("tout va bien", 20) {
		t.Error("texte propre jugé invalide")
	}
	if Valid("sale\x1b[0m", 20) || Valid("  espaces  ", 20) || Valid("trop long ici", 5) {
		t.Error("texte à nettoyer jugé valide")
	}
}

// propriétés vérifiées sur n'importe quelle entrée
func checkProperties(t *testing.T, in string, n int) string {
	t.Helper()
	out := Clean(in, n)
	if !utf8.ValidString(out) {
		t.Fatalf("Clean(%q, %d) = %q n'est pas de l'UTF-8 valide", in, n, out)
	}
	if n > 0 && utf8.RuneCountInString(out) > n {
		t.Fatalf("Clean(%q, %d) = %q dépasse %d caractères", in, n, out, n)
	}
	if out != strings.TrimSpace(out) || strings.Contains(out, "  ") {
		t.Fatalf("Clean(%q, %d) = %q: espaces de bord ou doubles", in, n, out)
	}
	for _, r := range out {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("Clean(%q, %d) = %q contient U+%04X", in, n, out, r)
		}
	}
	if Clean(out, n) != out {
		t.Fatalf("Clean n'est pas idempotent sur %q (n=%d): %q puis %q", in, n, out, Clean(out, n))
	}
	return out
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{
		"", "bonjour", "\x1b[31mrouge", "\x1b]0;x\x07", "a\u202eb", "\u0301\u0301a", "日本語", "\xff\xfe",
		"tab\there", "a\u200bb", strings.Repeat("\u0301", 50), "x\x1b", "\u009b1;2H",
	} {
		f.Add(s, 8)
	}
	f.Fuzz(func(t *testing.T, s string, n int) {
		checkProperties(t, s, n%200)
	})
}

// Rejoue quelques milliers d'entrées sans lancer le fuzzer, pour que `go test` les couvre.
func TestCleanPropertiesOnGeneratedInputs(t *testing.T) {
	alphabet := []string{"a", "é", "日", "😀", " ", "\n", "\t", "\x1b", "[", "]", "3", "1", "m", ";", "\x07", "\\",
		"\u202e", "\u200b", "\u0301", "\x00", "\xff", "\u2028", "\u009b", string(rune(0xFEFF)), "x"}
	state := uint32(12345)
	next := func() int { state = state*1664525 + 1013904223; return int(state >> 16) }
	for i := 0; i < 5000; i++ {
		var b strings.Builder
		for j, n := 0, next()%40; j < n; j++ {
			b.WriteString(alphabet[next()%len(alphabet)])
		}
		checkProperties(t, b.String(), next()%30)
	}
}

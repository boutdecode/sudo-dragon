package i18n

import (
	"regexp"
	"sort"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Lang
	}{
		{"français", map[string]string{"LANG": "fr_FR.UTF-8"}, FR},
		{"anglais", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"LC_ALL prime", map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "fr_FR.UTF-8"}, EN},
		{"LANGUAGE liste", map[string]string{"LANGUAGE": "de:fr:en", "LANG": "en_US"}, FR},
		{"C ignorée", map[string]string{"LC_ALL": "C", "LANG": "fr_CA.UTF-8"}, FR},
		{"POSIX ignorée", map[string]string{"LANG": "POSIX"}, EN},
		{"langue non gérée", map[string]string{"LANG": "de_DE.UTF-8"}, EN},
		{"locale explicite non gérée prime", map[string]string{"LC_ALL": "de_DE", "LANG": "fr_FR"}, EN},
		{"vide", map[string]string{}, EN},
		{"tiret", map[string]string{"LANG": "fr-CA"}, FR},
	}
	for _, c := range cases {
		if got := detect(env(c.env)); got != c.want {
			t.Errorf("%s: %q, attendu %q", c.name, got, c.want)
		}
	}
}

func TestNormalizeAndFallback(t *testing.T) {
	if Lang("").Normalize() != FR || Lang("xx").Normalize() != FR || Lang("EN").Normalize() != EN {
		t.Fatal("Normalize")
	}
	if got := T(EN, "inconnue"); got != "inconnue" {
		t.Fatalf("clé absente: %q", got)
	}
}

// Les deux catalogues doivent avoir exactement les mêmes clés et les mêmes
// directives de mise en forme: sinon une langue afficherait « %!d(MISSING) ».
func TestCatalogsAreConsistent(t *testing.T) {
	verb := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)
	sig := func(s string) string {
		v := verb.FindAllString(s, -1)
		sort.Strings(v)
		out := ""
		for _, x := range v {
			out += x
		}
		return out
	}
	for k, f := range fr {
		e, ok := en[k]
		if !ok {
			t.Errorf("clé %q absente de l'anglais", k)
			continue
		}
		if sig(f) != sig(e) {
			t.Errorf("clé %q: directives différentes (fr %q, en %q)", k, f, e)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("clé %q absente du français", k)
		}
	}
}

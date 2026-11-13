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
		{"french", map[string]string{"LANG": "fr_FR.UTF-8"}, FR},
		{"english", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"LC_ALL wins", map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "fr_FR.UTF-8"}, EN},
		{"LANGUAGE list", map[string]string{"LANGUAGE": "de:fr:en", "LANG": "en_US"}, FR},
		{"C ignored", map[string]string{"LC_ALL": "C", "LANG": "fr_CA.UTF-8"}, FR},
		{"POSIX ignored", map[string]string{"LANG": "POSIX"}, EN},
		{"unsupported language", map[string]string{"LANG": "de_DE.UTF-8"}, EN},
		{"explicit unsupported locale wins", map[string]string{"LC_ALL": "de_DE", "LANG": "fr_FR"}, EN},
		{"empty", map[string]string{}, EN},
		{"dash", map[string]string{"LANG": "fr-CA"}, FR},
	}
	for _, c := range cases {
		if got := detect(env(c.env)); got != c.want {
			t.Errorf("%s: %q, expected %q", c.name, got, c.want)
		}
	}
}

func TestNormalizeAndFallback(t *testing.T) {
	if Lang("").Normalize() != EN || Lang("xx").Normalize() != EN || Lang("EN").Normalize() != EN {
		t.Fatal("Normalize")
	}
	if got := T(EN, "inconnue"); got != "inconnue" {
		t.Fatalf("missing key: %q", got)
	}
}

// Both catalogues must have exactly the same keys and the same
// formatting directives: otherwise a language would display "%!d(MISSING)".
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
			t.Errorf("key %q missing from English", k)
			continue
		}
		if sig(f) != sig(e) {
			t.Errorf("key %q: different directives (fr %q, en %q)", k, f, e)
		}
	}
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("key %q missing from French", k)
		}
	}
}

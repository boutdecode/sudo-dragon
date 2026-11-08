// Package i18n traduit les textes du jeu en français et en anglais.
//
// Le français est la langue source: une langue inconnue ou vide (le zéro de Lang)
// s'y résout, ce qui garde les anciens tests et les anciens clients valides. La
// langue « par défaut » d'un utilisateur dont le système n'est ni français ni
// anglais est l'anglais (voir Detect).
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang est un code de langue: "fr" ou "en".
type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"
)

// Langs liste les langues prises en charge.
var Langs = []Lang{FR, EN}

// Parse reconnaît un code de langue ou un nom de locale POSIX ("fr_FR.UTF-8",
// "en-US", "French"…). Le second résultat est faux si la langue n'est pas gérée.
func Parse(s string) (Lang, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "._@"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.SplitN(s, "-", 2)[0], " ")
	switch s {
	case "fr", "fra", "fre", "french", "français", "francais":
		return FR, true
	case "en", "eng", "english", "anglais":
		return EN, true
	}
	return "", false
}

// Detect déduit la langue du système, dans l'ordre de priorité des variables
// POSIX (LC_ALL, LC_MESSAGES, LANGUAGE, LANG). LANGUAGE peut lister plusieurs
// langues séparées par « : »: la première gérée l'emporte. Les locales neutres
// « C » et « POSIX » sont ignorées. Sans correspondance, l'anglais.
func Detect() Lang { return detect(os.Getenv) }

func detect(getenv func(string) string) Lang {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		for _, v := range strings.Split(getenv(key), ":") {
			if v == "" || v == "C" || v == "POSIX" {
				continue
			}
			if l, ok := Parse(v); ok {
				return l
			}
			if key != "LANGUAGE" {
				// Une locale explicite mais non gérée (de_DE): on n'écoute pas les
				// variables moins prioritaires.
				return EN
			}
		}
	}
	return EN
}

// Normalize renvoie l'une des langues gérées: FR pour le zéro de Lang et pour
// tout code inconnu envoyé par un client.
func (l Lang) Normalize() Lang {
	if p, ok := Parse(string(l)); ok {
		return p
	}
	return FR
}

// T renvoie le texte de key dans la langue l, mis en forme avec args (fmt.Sprintf)
// si des arguments sont fournis. Une clé absente de la langue retombe sur le
// français puis sur la clé elle-même: un oubli reste visible sans planter.
func T(l Lang, key string, args ...any) string {
	s, ok := catalogs[l.Normalize()][key]
	if !ok {
		if s, ok = catalogs[FR][key]; !ok {
			s = key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Has indique si la clé existe dans la langue.
func Has(l Lang, key string) bool { _, ok := catalogs[l.Normalize()][key]; return ok }

// Pick renvoie l'élément i (modulo la taille) de la liste key.0, key.1… jusqu'au
// premier trou. Sert aux listes de phrases ou de traits tirés par indice.
func Pick(l Lang, prefix string, i int) string {
	n := Count(l, prefix)
	if n == 0 {
		return prefix
	}
	if i %= n; i < 0 {
		i += n
	}
	return T(l, fmt.Sprintf("%s.%d", prefix, i))
}

// Count compte les entrées prefix.0, prefix.1… de la langue.
func Count(l Lang, prefix string) int {
	n := 0
	for Has(l, fmt.Sprintf("%s.%d", prefix, n)) {
		n++
	}
	return n
}

var catalogs = map[Lang]map[string]string{FR: fr, EN: en}

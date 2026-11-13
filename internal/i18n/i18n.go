// Package i18n translates the game's texts into English and French.
//
// English is the reference language: an unknown or empty language (the zero value
// of Lang) resolves to it, and a key missing from a catalogue falls back to it.
// The default language of a user whose system is neither English nor French is
// also English (see Detect).
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang is a language code: "fr" or "en".
type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"
)

// Langs lists the supported languages.
var Langs = []Lang{FR, EN}

// Parse recognises a language code or a POSIX locale name ("fr_FR.UTF-8",
// "en-US", "French"…). The second result is false if the language is unsupported.
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

// Detect infers the system language, in priority order of the POSIX variables
// (LC_ALL, LC_MESSAGES, LANGUAGE, LANG). LANGUAGE may list several
// languages separated by ":": the first supported one wins. The neutral locales
// "C" and "POSIX" are ignored. With no match, English.
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
				// An explicit but unsupported locale (de_DE): we do not listen to the
				// lower-priority variables.
				return EN
			}
		}
	}
	return EN
}

// Normalize returns one of the supported languages: EN for the zero Lang and for
// any unknown code sent by a client.
func (l Lang) Normalize() Lang {
	if p, ok := Parse(string(l)); ok {
		return p
	}
	return EN
}

// T returns the text of key in language l, formatted with args (fmt.Sprintf) if
// any are given. A key missing from the language falls back to English and then
// to the key itself: an omission stays visible without crashing.
func T(l Lang, key string, args ...any) string {
	s, ok := catalogs[l.Normalize()][key]
	if !ok {
		if s, ok = catalogs[EN][key]; !ok {
			s = key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Has reports whether the key exists in the language.
func Has(l Lang, key string) bool { _, ok := catalogs[l.Normalize()][key]; return ok }

// Pick returns element i (modulo the size) of the list key.0, key.1... up to the
// first gap. Used for lists of phrases or traits chosen by index.
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

// Count counts the entries prefix.0, prefix.1... of the language.
func Count(l Lang, prefix string) int {
	n := 0
	for Has(l, fmt.Sprintf("%s.%d", prefix, n)) {
		n++
	}
	return n
}

var catalogs = map[Lang]map[string]string{FR: fr, EN: en}

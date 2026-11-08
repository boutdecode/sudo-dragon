// Package textsafe nettoie les textes écrits par des joueurs ou par un modèle
// de langage avant de les afficher dans un terminal ou de les stocker.
//
// Un terminal interprète certains caractères comme des commandes: séquences
// d'échappement (déplacer le curseur, effacer l'écran, changer le titre de la
// fenêtre, écrire dans le presse-papiers…), marques bidirectionnelles qui
// inversent l'ordre d'affichage, caractères invisibles qui cachent du texte.
// Un texte non nettoyé permettrait à un joueur d'abîmer l'écran des autres ou
// de falsifier ce qu'ils lisent.
package textsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxCombining est le nombre maximal de signes combinants consécutifs conservés
// après un caractère de base: au-delà, c'est du « zalgo » qui déborde sur les
// lignes voisines.
const maxCombining = 2

// Clean renvoie s en une seule ligne de texte affichable sans risque:
//   - séquences d'échappement ANSI (CSI, OSC…) entièrement retirées;
//   - caractères de contrôle supprimés, tabulations et retours à la ligne
//     (et séparateurs de ligne Unicode) remplacés par une espace;
//   - caractères de mise en forme Unicode (bidirectionnels, largeur nulle,
//     BOM…), usage privé et octets invalides UTF-8 supprimés;
//   - signes combinants limités, espaces multiples réduites, bords rognés;
//   - tronqué à maxRunes caractères si maxRunes > 0.
//
// Clean est idempotent: Clean(Clean(s, n), n) == Clean(s, n).
func Clean(s string, maxRunes int) string {
	s = stripEscapes(s)

	var b strings.Builder
	b.Grow(len(s))
	runes, combining := 0, 0
	pendingSpace := false

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size <= 1 {
			continue // octet invalide
		}

		switch {
		case isSpace(r):
			pendingSpace = b.Len() > 0
			combining = 0
			continue
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Co, r), unicode.Is(unicode.Cs, r):
			continue
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Mc, r):
			if b.Len() == 0 || combining >= maxCombining {
				continue
			}
			combining++
		default:
			combining = 0
		}

		if pendingSpace {
			if maxRunes > 0 && runes+1 >= maxRunes {
				break
			}
			b.WriteByte(' ')
			runes++
			pendingSpace = false
		}
		if maxRunes > 0 && runes >= maxRunes {
			break
		}
		b.WriteRune(r)
		runes++
	}
	return b.String()
}

// isSpace indique si r doit devenir une espace simple.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0x85, '\u2028', '\u2029':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// stripEscapes retire les séquences d'échappement ANSI complètes (ESC suivi de
// CSI, OSC, DCS…), pour ne pas laisser de « [31m » visible une fois l'ESC supprimé.
func stripEscapes(s string) string {
	if !strings.ContainsAny(s, "\x1b\u0090\u009b\u009d\u009e\u009f") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case s[i] == 0x1b && i+1 < len(s):
			i = skipEscape(s, i)
		case s[i] == 0x1b:
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == '\u009b' { // CSI sur 8 bits
				i = skipCSI(s, i+size)
				continue
			}
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// skipEscape renvoie l'index qui suit la séquence commençant par ESC en s[i].
func skipEscape(s string, i int) int {
	switch s[i+1] {
	case '[': // CSI: paramètres, intermédiaires, octet final
		return skipCSI(s, i+2)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: jusqu'à BEL ou ST (ESC \)
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	default: // séquence à deux octets (ESC c, ESC 7…)
		return i + 2
	}
}

// skipCSI saute les octets d'une séquence CSI à partir de s[i] (après « ESC [ »).
func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f { // paramètres
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f { // intermédiaires
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e { // octet final
		i++
	}
	return i
}

// Valid indique si s est déjà propre pour la limite donnée (Clean ne change rien).
func Valid(s string, maxRunes int) bool { return Clean(s, maxRunes) == s }

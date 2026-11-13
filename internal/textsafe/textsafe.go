// Package textsafe cleans texts written by players or by a language model
// before they are displayed in a terminal or stored.
//
// A terminal interprets some characters as commands: escape sequences
// (move the cursor, clear the screen, change the window title, write to the
// clipboard...), bidirectional marks that reverse the display order,
// invisible characters that hide text. An uncleaned text would let a player
// mess up other players' screens or falsify what they read.
package textsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxCombining is the maximum number of consecutive combining marks kept
// after a base character: beyond that it is "zalgo" text that spills onto
// neighboring lines.
const maxCombining = 2

// Clean returns s as a single line of text that is safe to display:
//   - ANSI escape sequences (CSI, OSC...) removed entirely;
//   - control characters removed, tabs and line breaks
//     (and Unicode line separators) replaced by a space;
//   - Unicode formatting characters (bidirectional, zero width,
//     BOM...), private use and invalid UTF-8 bytes removed;
//   - combining marks limited, multiple spaces collapsed, edges trimmed;
//   - truncated to maxRunes characters if maxRunes > 0.
//
// Clean is idempotent: Clean(Clean(s, n), n) == Clean(s, n).
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
			continue // invalid byte
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

// isSpace reports whether r must become a plain space.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0x85, '\u2028', '\u2029':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// stripEscapes removes complete ANSI escape sequences (ESC followed by
// CSI, OSC, DCS...), so as not to leave a visible "[31m" once the ESC is removed.
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
			if r == '\u009b' { // 8-bit CSI
				i = skipCSI(s, i+size)
				continue
			}
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// skipEscape returns the index following the sequence starting with ESC at s[i].
func skipEscape(s string, i int) int {
	switch s[i+1] {
	case '[': // CSI: parameters, intermediates, final byte
		return skipCSI(s, i+2)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: up to BEL or ST (ESC \)
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	default: // two-byte sequence (ESC c, ESC 7...)
		return i + 2
	}
}

// skipCSI skips the bytes of a CSI sequence starting at s[i] (after "ESC [").
func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f { // parameters
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f { // intermediates
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e { // final byte
		i++
	}
	return i
}

// Valid reports whether s is already clean for the given limit (Clean changes nothing).
func Valid(s string, maxRunes int) bool { return Clean(s, maxRunes) == s }

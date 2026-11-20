// Package worldlore loads the world's canon: the background knowledge every
// NPC shares (peoples, history, beliefs, tone...), written by the operator in
// one Markdown file per language, world-lore.<lang>.md.
//
// The canon is trusted operator text, unlike player text. It is cleaned (HTML
// comments removed, empty sections dropped, size capped) and can be reloaded
// while the server runs: a file that cannot be read keeps its previous version.
package worldlore

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

// MaxRunes caps one language's canon. It is sent with every dialogue request,
// so it costs tokens each time: about 2,000 tokens.
const MaxRunes = 8000

// FileName is the canon's file name for a language, e.g. "world-lore.fr.md".
func FileName(l i18n.Lang) string { return "world-lore." + string(l) + ".md" }

// Text is the canon given to a prompt.
type Text struct {
	Body string
	// Lang is the language Body is written in. It differs from the requested
	// language when that language's file is missing and another one is used.
	Lang i18n.Lang
}

// Empty reports whether there is no canon.
func (t Text) Empty() bool { return t.Body == "" }

type snapshot map[i18n.Lang]string

// Canon holds the canon of every language. The zero value and a nil *Canon
// are valid and empty. Safe for concurrent use.
type Canon struct {
	dir string
	log *slog.Logger

	mu   sync.Mutex // serialises reloads
	snap atomic.Pointer[snapshot]
}

// Load reads dir's canon files. Missing files are fine (no canon for that
// language); a file that exists but is unusable is an error.
func Load(dir string, log *slog.Logger) (*Canon, error) {
	if log == nil {
		log = slog.Default()
	}
	c := &Canon{dir: dir, log: log}
	empty := snapshot{}
	c.snap.Store(&empty)
	return c, c.Reload()
}

// Reload rereads the files. For each language, a file that cannot be read or
// is invalid leaves the previous version in place and is reported in the
// returned error; a file that has disappeared removes that language's canon.
func (c *Canon) Reload() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	old := *c.snap.Load()
	next := snapshot{}
	var errs []error
	for _, l := range i18n.Langs {
		path := filepath.Join(c.dir, FileName(l))
		raw, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			keep(next, old, l)
			continue
		}
		body, truncated, err := Clean(string(raw))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			keep(next, old, l)
			continue
		}
		if truncated {
			c.log.Warn("world lore truncated", "file", path, "max_runes", MaxRunes)
		}
		if body != "" {
			next[l] = body
		}
	}
	for _, l := range i18n.Langs {
		if _, ok := next[l]; !ok && len(next) > 0 {
			c.log.Warn("world lore missing for a language: another language's file will stand in", "lang", l, "file", FileName(l))
		}
	}
	c.snap.Store(&next)
	for _, e := range errs {
		c.log.Warn("world lore not reloaded, previous version kept", "err", e)
	}
	return errors.Join(errs...)
}

func keep(next, old snapshot, l i18n.Lang) {
	if b, ok := old[l]; ok {
		next[l] = b
	}
}

// For returns the canon for a language: that language's file, otherwise the
// first other language that has one, otherwise an empty Text.
func (c *Canon) For(l i18n.Lang) Text {
	if c == nil {
		return Text{}
	}
	snap := *c.snap.Load()
	if b, ok := snap[l]; ok {
		return Text{Body: b, Lang: l}
	}
	for _, o := range i18n.Langs {
		if b, ok := snap[o]; ok {
			return Text{Body: b, Lang: o}
		}
	}
	return Text{}
}

// Langs lists the languages that have a canon, for logs.
func (c *Canon) Langs() []i18n.Lang {
	if c == nil {
		return nil
	}
	snap := *c.snap.Load()
	var out []i18n.Lang
	for _, l := range i18n.Langs {
		if _, ok := snap[l]; ok {
			out = append(out, l)
		}
	}
	return out
}

var (
	comment = regexp.MustCompile(`(?s)<!--.*?-->`)
	blanks  = regexp.MustCompile(`\n{3,}`)
)

// Clean prepares a file's text for a prompt: removes HTML comments (the
// guidance of the templates), drops headings with nothing under them, collapses
// blank lines and caps the size. It rejects invalid UTF-8 and unterminated
// comments (the rest of the file would be silently lost).
func Clean(raw string) (body string, truncated bool, err error) {
	if !utf8.ValidString(raw) {
		return "", false, errors.New("not valid UTF-8")
	}
	raw = strings.TrimPrefix(raw, "\uFEFF")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = comment.ReplaceAllString(raw, "")
	if strings.Contains(raw, "<!--") {
		return "", false, errors.New("unterminated HTML comment")
	}
	raw = dropEmptySections(raw)
	raw = strings.TrimSpace(blanks.ReplaceAllString(raw, "\n\n"))
	if r := []rune(raw); len(r) > MaxRunes {
		raw, truncated = strings.TrimSpace(string(r[:MaxRunes])), true
	}
	return raw, truncated, nil
}

// headingLevel returns the level of a Markdown ATX heading line, or 0.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// dropEmptySections removes headings that have no text anywhere below them
// (before the next heading of the same or a higher level), so an unfilled
// template section is not sent to the model.
func dropEmptySections(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		lvl := headingLevel(ln)
		if lvl == 0 {
			out = append(out, ln)
			continue
		}
		filled := false
		for _, next := range lines[i+1:] {
			if l := headingLevel(next); l != 0 && l <= lvl {
				break
			} else if l == 0 && strings.TrimSpace(next) != "" {
				filled = true
				break
			}
		}
		if filled {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

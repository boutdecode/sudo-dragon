package worldlore

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func write(t *testing.T, dir string, l i18n.Lang, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName(l)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageChoiceAndFallback(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, i18n.FR, "# Monde\nLes Nains chantent.")
	c, err := Load(dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.For(i18n.FR); got.Lang != i18n.FR || !strings.Contains(got.Body, "Nains") {
		t.Errorf("fr: %+v", got)
	}
	if got := c.For(i18n.EN); got.Lang != i18n.FR || got.Empty() {
		t.Errorf("en must fall back to fr, got %+v", got)
	}
	write(t, dir, i18n.EN, "# World\nDwarves sing.")
	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := c.For(i18n.EN); got.Lang != i18n.EN || !strings.Contains(got.Body, "Dwarves") {
		t.Errorf("en: %+v", got)
	}
}

func TestMissingFilesAndNilCanon(t *testing.T) {
	c, err := Load(t.TempDir(), quiet)
	if err != nil {
		t.Fatalf("absent files are not an error: %v", err)
	}
	if !c.For(i18n.FR).Empty() || len(c.Langs()) != 0 {
		t.Error("no file, no canon")
	}
	var nilCanon *Canon
	if !nilCanon.For(i18n.EN).Empty() || nilCanon.Reload() != nil {
		t.Error("a nil canon is empty and harmless")
	}
}

func TestInvalidFileKeepsPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, i18n.FR, "Version un.")
	c, err := Load(dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"utf8":    "ok\xff\xfe",
		"comment": "Texte <!-- jamais fermé",
	} {
		write(t, dir, i18n.FR, bad)
		if err := c.Reload(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if got := c.For(i18n.FR).Body; got != "Version un." {
			t.Errorf("%s: previous version lost, got %q", name, got)
		}
	}
	write(t, dir, i18n.FR, "Version deux.")
	if err := c.Reload(); err != nil || c.For(i18n.FR).Body != "Version deux." {
		t.Errorf("valid reload: err=%v body=%q", err, c.For(i18n.FR).Body)
	}
}

func TestInvalidAtStartupIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, i18n.EN, "bad \xff")
	if _, err := Load(dir, quiet); err == nil {
		t.Error("an unusable file at startup must be reported")
	}
}

func TestRemovedFileRemovesLanguage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, i18n.FR, "Texte.")
	c, _ := Load(dir, quiet)
	if err := os.Remove(filepath.Join(dir, FileName(i18n.FR))); err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(); err != nil || !c.For(i18n.FR).Empty() {
		t.Errorf("removal: err=%v text=%+v", err, c.For(i18n.FR))
	}
}

func TestCleanStripsCommentsAndEmptySections(t *testing.T) {
	raw := "\uFEFF# Titre\r\n<!-- consigne\non plusieurs lignes -->\r\n## Vide\n<!-- rien -->\n\n## Rempli\nUn fait.\n\n\n\n### Sous-vide\n\n## Parent\n### Enfant\nTexte enfant.\n"
	got, truncated, err := Clean(raw)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	want := "# Titre\n\n## Rempli\nUn fait.\n\n## Parent\n### Enfant\nTexte enfant."
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if body, _, _ := Clean("# Titre\n<!-- que des consignes -->\n## A\n"); body != "" {
		t.Errorf("an unfilled template must give no canon, got %q", body)
	}
}

func TestCleanCapsSize(t *testing.T) {
	got, truncated, err := Clean(strings.Repeat("é", MaxRunes+500))
	if err != nil || !truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if n := len([]rune(got)); n != MaxRunes {
		t.Errorf("%d runes, want %d", n, MaxRunes)
	}
}

func TestConcurrentReloadAndRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, i18n.FR, "A")
	c, _ := Load(dir, quiet)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.Reload()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if c.For(i18n.FR).Body != "A" {
					t.Error("torn read")
					return
				}
			}
		}()
	}
	wg.Wait()
}

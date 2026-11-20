package worldlore

import (
	"os"
	"path/filepath"
	"testing"
)

// The shipped templates are only guidance: unfilled, they must produce no canon, so
// copying one without writing in it adds nothing to the prompts.
func TestShippedTemplatesAreEmptyUntilFilled(t *testing.T) {
	for _, name := range []string{"world-lore.fr.template.md", "world-lore.en.template.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil {
			t.Fatal(err)
		}
		body, truncated, err := Clean(string(raw))
		if err != nil || truncated || body != "" {
			t.Errorf("%s: body=%q truncated=%v err=%v", name, body, truncated, err)
		}
	}
}

package render

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMonsterImages(t *testing.T) {
	for _, kind := range []string{"rat", "wolf", "crab"} {
		img := MonsterImage(kind, 40, 12)
		if len(img) < 4 || len(img) > 12 {
			t.Fatalf("%s: %d lignes", kind, len(img))
		}
		for _, l := range img {
			if w := ansi.StringWidth(l); w != ansi.StringWidth(img[0]) || w > 40 {
				t.Errorf("%s: largeur %d inattendue", kind, w)
			}
		}
	}
	if MonsterImage("goblin", 40, 12) != nil {
		t.Error("le gobelin n'a pas d'illustration")
	}
	if MonsterImage("wolf", 4, 12) != nil || MonsterImage("wolf", 40, 2) != nil {
		t.Error("pas d'image quand la place manque")
	}
}

package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// tickAt donne le tick où l'horloge de jeu marque l'heure h (jour 1 ou suivant).
func tickAt(day, h int) uint64 {
	return uint64((day*24 + h - protocol.StartGameHour) * protocol.TicksPerGameHour)
}

func TestDaylightCurve(t *testing.T) {
	for _, c := range []struct {
		h    int
		want float64
	}{{12, 1}, {8, 1}, {18, 1}, {0, 0}, {3, 0}, {21, 0}, {23, 0}} {
		if got := Daylight(tickAt(1, c.h)); got != c.want {
			t.Errorf("Daylight(%dh) = %v, want %v", c.h, got, c.want)
		}
	}
	prev := -1.0
	for h := 5; h <= 8; h++ { // l'aube éclaircit
		d := Daylight(tickAt(1, h))
		if d < prev {
			t.Errorf("l'aube s'assombrit à %dh", h)
		}
		prev = d
	}
	prev = 2
	for h := 19; h <= 21; h++ { // le crépuscule assombrit
		d := Daylight(tickAt(1, h))
		if d > prev {
			t.Errorf("le crépuscule s'éclaircit à %dh", h)
		}
		prev = d
	}
	if Daylight(tickAt(1, 6)) != Daylight(tickAt(5, 6)) {
		t.Error("le cycle doit être périodique sur 24 h")
	}
}

func TestNightLevelBounds(t *testing.T) {
	if got := NightLevel(tickAt(1, 12)); got != 0 {
		t.Errorf("midi: niveau %d, want 0", got)
	}
	if got := NightLevel(tickAt(1, 0)); got != NightLevels-1 {
		t.Errorf("minuit: niveau %d, want %d", got, NightLevels-1)
	}
	for tk := uint64(0); tk < 24*protocol.TicksPerGameHour; tk += 37 {
		if n := NightLevel(tk); n < 0 || n >= NightLevels {
			t.Fatalf("niveau %d hors bornes au tick %d", n, tk)
		}
	}
}

func TestViewportNightDimsTerrainOnly(t *testing.T) {
	w := world.New(1)
	ents := []protocol.Entity{{ID: 1, X: 0, Y: 0}}
	day := ViewportNight(w, 0, 0, 20, 8, ents, nil, 1, nil, nil, 0)
	if day != ViewportReady(w, 0, 0, 20, 8, ents, nil, 1, nil, nil) {
		t.Error("niveau 0 doit égaler ViewportReady")
	}
	night := ViewportNight(w, 0, 0, 20, 8, ents, nil, 1, nil, nil, NightLevels-1)
	if day == night {
		t.Error("la nuit doit changer les couleurs")
	}
	strip := func(s string) string { return ansiRe.ReplaceAllString(s, "") }
	if strip(day) != strip(night) {
		t.Error("la nuit ne doit changer que les couleurs, pas les glyphes")
	}
	if strings.Count(night, "\n") != strings.Count(day, "\n") {
		t.Error("hauteur différente")
	}
	if n := ViewportNight(w, 0, 0, 20, 8, ents, nil, 1, nil, nil, 99); n != night {
		t.Error("le niveau doit être borné")
	}
}

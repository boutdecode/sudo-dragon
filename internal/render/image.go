package render

import (
	"embed"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
)

// Les illustrations des monstres sont embarquées dans le binaire et dessinées
// en demi-blocs (▀): chaque cellule du terminal porte deux pixels, celui du
// haut en couleur d'avant-plan, celui du bas en couleur de fond (24 bits).
//
//go:embed assets/monsters/*.png
var monsterImages embed.FS

// frameMargin est la part de chaque côté rognée avant la réduction. Elle est
// nulle: le cadre décoratif des illustrations est conservé.
var frameMargin = map[string]float64{
	"rat":  0,
	"wolf": 0,
	"crab": 0,
}

type imageKey struct {
	kind       string
	cols, rows int
}

var (
	imgMu     sync.Mutex
	imgSource = map[string]image.Image{}
	imgCache  = map[imageKey][]string{}
)

// HasMonsterImage dit si une espèce a une illustration.
func HasMonsterImage(kind string) bool {
	_, ok := frameMargin[kind]
	return ok
}

// MonsterImage dessine l'illustration d'une espèce dans au plus maxCols
// colonnes et maxRows lignes de cellules (proportions conservées). Toutes les
// lignes ont la même largeur. Renvoie nil si l'espèce n'a pas d'illustration
// ou si la place est trop petite pour qu'elle reste lisible.
func MonsterImage(kind string, maxCols, maxRows int) []string {
	if !HasMonsterImage(kind) || maxCols < 8 || maxRows < 4 {
		return nil
	}
	imgMu.Lock()
	defer imgMu.Unlock()

	src, ok := imgSource[kind]
	if !ok {
		f, err := monsterImages.Open("assets/monsters/" + kind + ".png")
		if err == nil {
			src, _ = png.Decode(f)
			f.Close()
		}
		imgSource[kind] = src
	}
	if src == nil {
		return nil
	}

	b := src.Bounds()
	m := frameMargin[kind]
	crop := image.Rect(
		b.Min.X+int(float64(b.Dx())*m), b.Min.Y+int(float64(b.Dy())*m),
		b.Max.X-int(float64(b.Dx())*m), b.Max.Y-int(float64(b.Dy())*m))

	// Une cellule = 1 pixel de large, 2 de haut: on ajuste en pixels.
	pw, ph := maxCols, maxRows*2
	if crop.Dx()*ph > crop.Dy()*pw { // limité par la largeur
		ph = max(2, crop.Dy()*pw/crop.Dx())
	} else {
		pw = max(1, crop.Dx()*ph/crop.Dy())
	}
	ph -= ph % 2
	key := imageKey{kind, pw, ph / 2}
	if lines, ok := imgCache[key]; ok {
		return lines
	}

	lines := make([]string, ph/2)
	for row := range lines {
		var sb strings.Builder
		for x := 0; x < pw; x++ {
			tr, tg, tb := boxAverage(src, crop, x, 2*row, pw, ph)
			br, bg, bb := boxAverage(src, crop, x, 2*row+1, pw, ph)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		sb.WriteString("\x1b[0m")
		lines[row] = sb.String()
	}
	imgCache[key] = lines
	return lines
}

// boxAverage moyenne les pixels de la source qui tombent dans le pixel (px, py)
// d'une grille pw x ph couvrant la zone crop.
func boxAverage(src image.Image, crop image.Rectangle, px, py, pw, ph int) (r, g, b uint8) {
	x0 := crop.Min.X + px*crop.Dx()/pw
	x1 := max(x0+1, crop.Min.X+(px+1)*crop.Dx()/pw)
	y0 := crop.Min.Y + py*crop.Dy()/ph
	y1 := max(y0+1, crop.Min.Y+(py+1)*crop.Dy()/ph)
	var sr, sg, sb, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cr, cg, cb, _ := src.At(x, y).RGBA()
			sr, sg, sb, n = sr+uint64(cr>>8), sg+uint64(cg>>8), sb+uint64(cb>>8), n+1
		}
	}
	return uint8(sr / n), uint8(sg / n), uint8(sb / n)
}

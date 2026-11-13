package render

import (
	"embed"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
)

// Monster illustrations are embedded in the binary and drawn
// in half blocks (▀): each terminal cell carries two pixels, the upper
// one in foreground colour, the lower one in background colour (24 bits).
//
//go:embed assets/monsters/*.png
var monsterImages embed.FS

// frameMargin is the share of each side cropped before downscaling. It is
// zero: the decorative frame of the illustrations is kept.
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

// HasMonsterImage reports whether a species has an illustration.
func HasMonsterImage(kind string) bool {
	_, ok := frameMargin[kind]
	return ok
}

// MonsterImage draws the illustration of a species in at most maxCols
// columns and maxRows rows of cells (proportions kept). All the
// lines have the same width. It returns nil if the species has no illustration
// or if the room is too small for it to remain legible.
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

	// One cell = 1 pixel wide, 2 tall: we adjust in pixels.
	pw, ph := maxCols, maxRows*2
	if crop.Dx()*ph > crop.Dy()*pw { // limited by width
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

// boxAverage averages the source pixels that fall within pixel (px, py)
// of a pw x ph grid covering the crop area.
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

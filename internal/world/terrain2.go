package world

import (
	"math"

	opensimplex "github.com/ojrac/opensimplex-go"
)

// Générateur de terrain v2: continents, océans et îles.
//
// Le terrain reste une fonction pure (seed, x, y) → Tile, calculable tuile par tuile: le serveur et
// le client la recalculent à l'identique, et le monde reste infini. Tout part d'une « élévation »
// elev2, mer à 0.

// Échelles du générateur v2, en tuiles.
const (
	continentScale = 600.0 // taille d'un continent ou d'un océan
	islandScale    = 100.0 // taille d'un archipel
	warpScale      = 150.0 // ondulation des côtes
	warpAmount     = 50.0  // déformation maximale des côtes, en tuiles
	detailScale    = 35.0  // relief local
	originRadius   = 350.0 // rayon du continent garanti autour de (0, 0)
	grainScale     = 6.0   // taille des bosquets et des clairières
	patchScale     = 18.0  // taille des bosquets, clairières, dunes et mares à l'intérieur d'un biome
)

// noise2 regroupe les bruits propres à la v2, tous dérivés de la seed.
type noise2 struct {
	continent, island, warpX, warpY, detail, hills, temp, wet, ridge, ridgeMask, river, riverWarp, riverMask, lake opensimplex.Noise
}

func newNoise2(seed int64) noise2 {
	return noise2{
		continent: opensimplex.New(seed ^ 0x2C1B3C6D),
		island:    opensimplex.New(seed ^ 0x297A2D39),
		warpX:     opensimplex.New(seed ^ 0x1B873593),
		warpY:     opensimplex.New(seed ^ 0x6A09E667),
		detail:    opensimplex.New(seed ^ 0x3C6EF372),
		hills:     opensimplex.New(seed ^ 0x510E527F),
		temp:      opensimplex.New(seed ^ 0x3956C25B),
		wet:       opensimplex.New(seed ^ 0x59F111F1),
		ridge:     opensimplex.New(seed ^ 0x9B05688C),
		ridgeMask: opensimplex.New(seed ^ 0x1F83D9AB),
		river:     opensimplex.New(seed ^ 0x5BE0CD19),
		riverWarp: opensimplex.New(seed ^ 0x428A2F98),
		riverMask: opensimplex.New(seed ^ 0x71374491),
		lake:      opensimplex.New(seed ^ 0xB5C0FBCF),
	}
}

func smoothstep(a, b, v float64) float64 {
	t := math.Max(0, math.Min(1, (v-a)/(b-a)))
	return t * t * (3 - 2*t)
}

// elev2 est l'élévation v2, à peu près dans [-1, 1]; la mer est sous 0.
func (w *World) elev2(x, y int) float64 {
	n := &w.noise2
	fx, fy := float64(x), float64(y)

	// Côtes irrégulières: on déforme les coordonnées avant de lire le continent.
	wx := fx + warpAmount*fbm(n.warpX, fx/warpScale, fy/warpScale)
	wy := fy + warpAmount*fbm(n.warpY, fx/warpScale+50, fy/warpScale+50)

	// Continents: bruit à très grande échelle. Une courbe l'étire pour des masses de terre
	// bien séparées par de larges océans plutôt qu'un damier.
	c := fbm(n.continent, wx/continentScale, wy/continentScale)
	c = c*1.6 - 0.05

	// Un continent garanti autour de l'origine: le joueur n'apparaît jamais en pleine mer.
	d := math.Hypot(fx, fy)
	c += 0.9 * (1 - smoothstep(0, originRadius, d))

	// Îles: là où l'océan est peu profond, un second bruit fait surgir des archipels.
	isl := fbm(n.island, wx/islandScale, wy/islandScale)
	shallow := 1 - smoothstep(-0.5, -0.05, c) // 1 en mer peu profonde, 0 au large
	shallow *= smoothstep(-0.95, -0.55, c)    // pas d'îles au cœur d'un océan profond
	c += shallow * math.Max(0, isl-0.05) * 2.2

	// Relief local, qui découpe un peu les rivages et les plaines.
	return c + 0.12*fbm(n.detail, fx/detailScale, fy/detailScale)
}

// Chaînes de montagnes, rivières et lacs.
const (
	ridgeScale       = 200.0 // distance entre deux chaînes
	ridgeWarpScale   = 90.0  // déformation des chaînes
	ridgeWarpAmount  = 45.0
	riverScale       = 160.0 // sinuosité des rivières
	riverWarpScale   = 110.0
	riverWarpAmount  = 75.0
	riverWarpScale2  = 55.0 // petites ondulations
	riverWarpAmount2 = 34.0
	riverHalfBase    = 1.3 // demi-largeur d'une rivière, en cases
	riverHalfCoast   = 1.6 // demi-largeur ajoutée vers les côtes
	lakeScale        = 45.0
	hillScale        = 70.0 // taille des collines isolées
)

// mountain2 est la hauteur des montagnes en (x, y), dans [0, 1]: des crêtes linéaires (là où un bruit
// passe par zéro), interrompues par un second bruit pour former des chaînes plutôt qu'un quadrillage, et
// qui n'apparaissent qu'à l'intérieur des terres.
func (w *World) mountain2(x, y int, e float64) float64 {
	n := &w.noise2
	fx, fy := float64(x), float64(y)
	// Les crêtes sont déformées comme les rivières: sans cela, une chaîne est une bande aux bords presque droits.
	wx := fx + ridgeWarpAmount*n.riverWarp.Eval2(fx/ridgeWarpScale+800, fy/ridgeWarpScale+800) +
		ridgeWarpAmount/3*n.riverWarp.Eval2(fx/(ridgeWarpScale/3)+900, fy/(ridgeWarpScale/3)+900)
	wy := fy + ridgeWarpAmount*n.riverWarp.Eval2(fx/ridgeWarpScale+1100, fy/ridgeWarpScale+1100) +
		ridgeWarpAmount/3*n.riverWarp.Eval2(fx/(ridgeWarpScale/3)+1200, fy/(ridgeWarpScale/3)+1200)
	r := 1 - math.Abs(n.ridge.Eval2(wx/ridgeScale, wy/ridgeScale)+0.3*n.ridge.Eval2(wx/ridgeScale*2.3+9, wy/ridgeScale*2.3+9))
	r = math.Max(0, (r-0.6)/0.4) // crête étroite: 0 hors chaîne, 1 sur la ligne de crête
	mask := smoothstep(-0.12, 0.05, fbm(n.ridgeMask, fx/(ridgeScale*1.7)+300, fy/(ridgeScale*1.7)+300))
	inland := smoothstep(0.08, 0.4, e)
	return r * mask * inland
}

// riverField est le champ dont les rivières sont la ligne de niveau zéro. Le point est d'abord déformé à deux
// échelles (grands coudes, puis petites ondulations) et le champ somme plusieurs octaves (méandres et petites
// sinuosités): la ligne n'est donc pas un grand trait droit.
func (w *World) riverField(fx, fy float64) float64 {
	n := &w.noise2
	wx := fx + riverWarpAmount*n.riverWarp.Eval2(fx/riverWarpScale, fy/riverWarpScale) +
		riverWarpAmount2*n.riverWarp.Eval2(fx/riverWarpScale2+70, fy/riverWarpScale2+70)
	wy := fy + riverWarpAmount*n.riverWarp.Eval2(fx/riverWarpScale+300, fy/riverWarpScale+300) +
		riverWarpAmount2*n.riverWarp.Eval2(fx/riverWarpScale2+370, fy/riverWarpScale2+370)
	return n.river.Eval2(wx/riverScale, wy/riverScale) +
		0.4*n.river.Eval2(wx/(riverScale/2.5)+30, wy/(riverScale/2.5)+30) +
		0.15*n.river.Eval2(wx/(riverScale/6)+60, wy/(riverScale/6)+60)
}

// riverAt dit si (x, y) est une rivière: une ligne de niveau de riverField, sans simulation de l'écoulement (le
// monde est infini). La largeur est donnée en cases: on mesure la distance à la ligne de niveau avec le gradient du
// champ (sans quoi la bande s'élargit en grands traits là où le champ est presque plat), et elle varie le long du
// cours. Un bruit de masque fait naître et mourir la rivière; elle s'élargit vers les côtes et ne grimpe pas sur
// les hauts sommets.
func (w *World) riverAt(x, y int, e, mount float64) bool {
	if e < 0.06 || mount > 0.35 {
		return false
	}
	fx, fy := float64(x), float64(y)
	v := w.riverField(fx, fy)
	if math.Abs(v) > 0.25 {
		return false // trop loin d'une ligne de niveau: évite le calcul du gradient
	}
	n := &w.noise2
	life := smoothstep(-0.2, 0.2, fbm(n.riverMask, fx/(riverScale*2.2)+500, fy/(riverScale*2.2)+500))
	// Largeur variable le long du cours: tantôt un filet, tantôt un lit plus large.
	vary := 0.65 + 0.7*smoothstep(-0.5, 0.5, n.detail.Eval2(fx/55+1900, fy/55+1900))
	half := (riverHalfBase + riverHalfCoast*(1-smoothstep(0.1, 0.8, e))) * life * vary
	if half < 0.5 {
		return false
	}
	const h = 1.5
	gx := (w.riverField(fx+h, fy) - v) / h
	gy := (w.riverField(fx, fy+h) - v) / h
	g := math.Hypot(gx, gy)
	return g > 1e-6 && math.Abs(v)/g < half
}

// Climat: température et humidité, dans [-1, 1] environ.
const (
	tempScale     = 800.0  // taille des zones climatiques
	latitudeSpan  = 3200.0 // période des bandes de latitude (nord-sud)
	midWetScale   = 140.0  // taille des taches d'humidité locales
	temperateZone = 400.0  // rayon autour de (0, 0) où le climat est ramené vers le tempéré
)

// climate2 renvoie la température et l'humidité de (x, y). La température suit une latitude (des bandes
// froides et chaudes le long de y, avec une phase propre à la seed) perturbée par un bruit; l'humidité vient d'un
// bruit, plus forte près de la mer. Le voisinage de l'origine est ramené vers le tempéré: on y apparaît toujours
// dans un pays de plaines et de forêts.
func (w *World) climate2(x, y int, e float64) (temp, wet float64) {
	n := &w.noise2
	fx, fy := float64(x), float64(y)
	phase := float64(uint64(w.Seed)&0xFFFF) / 0xFFFF * 2 * math.Pi
	temp = 0.7*math.Sin(2*math.Pi*fy/latitudeSpan+phase) + 0.5*fbm(n.temp, fx/tempScale, fy/tempScale)
	// Humidité à deux échelles: les zones climatiques, et des taches de quelques centaines de cases qui mêlent
	// forêts et plaines à l'intérieur d'une même zone.
	wet = 1.4*fbm(n.wet, fx/tempScale+200, fy/tempScale+200) + 1.0*fbm(n.wet, fx/midWetScale+900, fy/midWetScale+900) +
		0.3*(1-smoothstep(0.05, 0.6, e))
	k := smoothstep(0, temperateZone, math.Hypot(fx, fy)) // 0 à l'origine, 1 au loin
	// Vers le tempéré et le mi-humide (forêts et plaines mêlées), sans effacer toute variation.
	return temp * (0.3 + 0.7*k), 0.12 + (wet-0.12)*(0.6+0.4*k)
}

// terrainAtV2 est le générateur de terrain v2.
func (w *World) terrainAtV2(x, y int) Tile {
	e := w.elev2(x, y)
	switch {
	case e < -0.38:
		return DeepWater
	case e < -0.02:
		return Water
	case e < 0.04:
		return Sand
	}
	temp, wet := w.climate2(x, y, e)
	mount := w.mountain2(x, y, e)
	cold := -temp // plus c'est grand, plus il fait froid
	if mount > 0.55 {
		// Neige sur les sommets des régions froides, et sur les plus hauts partout.
		if mount > 0.95 || cold+0.5*mount > 0.8 {
			return Snow
		}
		return Mountain
	}
	if w.riverAt(x, y, e, mount) {
		return River
	}
	// Lacs: cuvettes isolées à l'intérieur des terres.
	if e > 0.25 && mount < 0.1 && fbm(w.noise2.lake, float64(x)/lakeScale, float64(y)/lakeScale) > 0.5 {
		return Water
	}
	if mount > 0.3 || fbm(w.noise2.hills, float64(x)/hillScale, float64(y)/hillScale) > 0.25 {
		return Hills
	}
	// Motifs locaux à l'intérieur d'un biome: bosquets et clairières, dunes, mares, taches de neige.
	patch := fbm(w.noise2.detail, float64(x)/patchScale+700, float64(y)/patchScale+700)
	// Grain fin: bosquets isolés dans une plaine, clairières dans une forêt, lisières déchiquetées.
	grain := fbm(w.noise2.hills, float64(x)/grainScale+2300, float64(y)/grainScale+2300)
	switch {
	case cold > 0.95:
		return Snow
	case cold > 0.62:
		switch {
		case patch > 0.28:
			return Hills
		case patch < -0.28:
			return Snow
		}
		return Tundra
	case temp > 0.4 && wet < -0.1:
		switch {
		case patch > 0.25:
			return Hills // dune
		case patch < -0.25:
			return Sand
		}
		return Desert
	case wet > 0.35 && e < 0.3 && temp > -0.1:
		if patch > 0.3 {
			return Water // mare
		}
		return Swamp
	case temp > 0.4 && wet > 0.15:
		if patch < -0.3 {
			return Forest // clairière
		}
		return Jungle
	case 0.35*wet+1.4*patch+3.2*grain > 0.05:
		return Forest
	}
	return Plain
}

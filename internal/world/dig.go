package world

// Cases à creuser des cartes au trésor. Comme le reste du monde, elles sont ordinaires : rien n'indique la case sur la
// carte. C'est la carte du joueur (voir game/treasure.go) qui en retient les coordonnées.

const (
	digSalt      = 8500
	digMargin    = 6 // distance minimale de la case au bord de sa région
	digTries     = 32
	digClearance = 3 // distance minimale à un point d'intérêt ou à une entrée de donjon
)

// DigSpot cherche une case à creuser dans la région rp, tirée de nonce : du terrain naturel praticable sur 3×3, hors
// de l'emprise d'un lieu (zone sûre comprise), du point de départ, d'un point d'intérêt et d'une entrée de donjon.
// Seul le terrain brut compte, comme pour les points d'intérêt : la case peut se trouver sur une route.
func (w *World) DigSpot(rp RegionPos, nonce uint64) (Pos, bool) {
	rng := identityRNG(w.Seed, rp, digSalt+nonce%1_000_003)
	anchor := w.spawnAnchor()
	poi := w.POIOfRegion(rp)
	var dun *Dungeon
	if mayHideDungeon(w.Seed, rp) {
		dun = w.DungeonOfRegion(rp)
	}
	for range digTries {
		x := rp.X*RegionSize + digMargin + rng.IntN(RegionSize-2*digMargin)
		y := rp.Y*RegionSize + digMargin + rng.IntN(RegionSize-2*digMargin)
		if abs(x-anchor.X) <= startClearance && abs(y-anchor.Y) <= startClearance {
			continue
		}
		if poi != nil && max(abs(x-poi.Pos.X), abs(y-poi.Pos.Y)) <= digClearance {
			continue
		}
		if dun != nil && max(abs(x-dun.Entrance.X), abs(y-dun.Entrance.Y)) <= digClearance {
			continue
		}
		if w.digSpotOK(rp, x, y) {
			return Pos{x, y}, true
		}
	}
	return Pos{}, false
}

func (w *World) digSpotOK(rp RegionPos, x, y int) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			switch w.terrainAt(x+dx, y+dy) {
			case Plain, Forest, Hills, Sand:
			default:
				return false
			}
		}
	}
	const clear = safeMargin + 2
	if s := w.structureForRegion(rp); s != nil &&
		x >= s.Origin.X-clear && x < s.Origin.X+s.W+clear && y >= s.Origin.Y-clear && y < s.Origin.Y+s.H+clear {
		return false
	}
	return true
}

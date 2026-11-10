package game

import (
	"slices"
	"strings"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Gardes des villages et des villes.
//
// Un garde est une entité du serveur, distincte des monstres comme des PNJ postés : il patrouille sur les chemins
// de son lieu, on peut lui parler (il connaît les habitants et les environs) et il attaque les joueurs que le lieu
// tient pour hors-la-loi (réputation <= quest.Wanted). Le client le voit comme un monstre de genre "guard" : même
// dessin, même évitement, mais marcher dessus ouvre la conversation (ou le combat, pour un hors-la-loi).
//
// Pour le combat, il prête son « avatar », un monstre qui n'est jamais enregistré dans g.monsters : il n'apparaît
// pas dans la carte des monstres et ne bouge pas seul.

const (
	guardStepEvery  = 5  // ticks entre deux pas de patrouille
	guardChaseEvery = 3  // ticks entre deux pas de poursuite : plus lent qu'un joueur (MoveCooldown), on peut le distancer
	guardAggro      = 12 // portée de détection d'un hors-la-loi
	guardLeash      = 8  // cases au-delà de l'emprise du lieu où un garde peut poursuivre
	guardRestMax    = 30 // pause maximale à un point de passage

	townGuardRespawn = 3000 // ticks (5 min) avant qu'un garde vaincu soit remplacé
	guardIDBase      = 1 << 28

	guardKillRep = -5 // réputation perdue en tuant un garde
	stealFailRep = -3 // réputation perdue en se faisant prendre à voler

	stealCooldown = 1800 // ticks (3 min) avant de pouvoir refaire les poches du même marchand
	maxStealMemo  = 2048
)

// guardSpecies est la fiche de combat d'un garde, mise à l'échelle du niveau de la région (scaledForLevel).
var guardSpecies = species{
	Kind: "guard", MoveEvery: guardChaseEvery, Aggro: guardAggro,
	HP: 60, Size: hero.Medium, DexMod: 1, Hit: 5, Dmg: dice.D(2, 6, 0), Reduction: dice.D(1, 6, 0),
}

type guard struct {
	id     int
	npc    world.NPC // identité ; npc.Pos est la position courante
	place  *world.Structure
	avatar *monster // prête sa fiche au combat

	path                  []world.Pos
	cooldown, rest, stuck int
}

type stealKey struct {
	player int
	npc    world.NPCKey
}

// guardState regroupe tout ce qui concerne les gardes. Sa valeur nulle est utilisable (voir guards()).
type guardState struct {
	byKey    map[world.NPCKey]*guard
	at       map[world.Pos]*guard
	defeated map[world.NPCKey]uint64
	roads    map[world.RegionPos][]world.Pos
	stolen   map[stealKey]uint64
	next     int
}

func (g *Game) guards() *guardState {
	if g.gd.byKey == nil {
		g.gd = guardState{
			byKey:    map[world.NPCKey]*guard{},
			at:       map[world.Pos]*guard{},
			defeated: map[world.NPCKey]uint64{},
			roads:    map[world.RegionPos][]world.Pos{},
			stolen:   map[stealKey]uint64{},
		}
	}
	return &g.gd
}

// guardAt renvoie le garde posté sur la case pos de la carte principale, s'il y en a un.
func (g *Game) guardAt(pos world.Pos) *guard {
	if g.gd.at == nil {
		return nil
	}
	return g.gd.at[pos]
}

// sortedGuards renvoie les gardes par identifiant croissant (ordre déterministe).
func (g *Game) sortedGuards() []*guard {
	gs := make([]*guard, 0, len(g.gd.byKey))
	for _, gd := range g.gd.byKey {
		gs = append(gs, gd)
	}
	slices.SortFunc(gs, func(a, b *guard) int { return a.id - b.id })
	return gs
}

// wanted indique que le lieu de ce garde tient p pour un hors-la-loi.
func (gd *guard) wanted(p *player) bool { return quest.IsWanted(p.rep[gd.npc.Region]) }

// nearLeash indique si pos est assez proche du lieu pour qu'un garde s'y aventure.
func (gd *guard) nearLeash(pos world.Pos) bool {
	s := gd.place
	return pos.X >= s.Origin.X-guardLeash && pos.X < s.Origin.X+s.W+guardLeash &&
		pos.Y >= s.Origin.Y-guardLeash && pos.Y < s.Origin.Y+s.H+guardLeash
}

// activateGuards place les gardes des villages et des villes proches d'un joueur et retire ceux qui sont loin de
// tous. Appelée en même temps que l'apparition des monstres (spawnEvery).
func (g *Game) activateGuards() {
	st := g.guards()
	for _, gd := range g.sortedGuards() {
		if gd.avatar.fight != nil || g.guardWatched(gd) {
			continue
		}
		g.removeGuard(gd, false)
	}
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		for _, s := range g.World.StructuresNear(p.pos, garrisonActivation) {
			for _, npc := range s.Guards {
				g.fillGuard(st, s, npc)
			}
		}
	}
}

// guardWatched indique qu'un joueur est assez près du garde pour que celui-ci reste en place.
func (g *Game) guardWatched(gd *guard) bool {
	for _, p := range g.players {
		if p.zone == 0 && inBox(gd.npc.Pos, p.pos, keepHalfW, keepHalfH) {
			return true
		}
	}
	return false
}

func (g *Game) fillGuard(st *guardState, s *world.Structure, npc world.NPC) {
	key := npc.Key()
	if _, alive := st.byKey[key]; alive {
		return
	}
	if at, beaten := st.defeated[key]; beaten {
		if g.tick-at < townGuardRespawn {
			return
		}
		delete(st.defeated, key)
	}
	if g.blocked(npc.Pos) { // quelqu'un se tient sur son poste : on réessaie au prochain passage
		return
	}
	st.next++
	gd := &guard{id: st.next, npc: npc, place: s}
	gd.avatar = &monster{
		id: -gd.id, sp: scaledForLevel(guardSpecies, world.LevelAt(npc.Pos.X, npc.Pos.Y)),
		pos: npc.Pos, home: npc.Pos, guard: gd,
	}
	st.byKey[key] = gd
	st.at[npc.Pos] = gd
}

// removeGuard retire un garde du monde ; slain le marque comme vaincu (il revient après townGuardRespawn).
func (g *Game) removeGuard(gd *guard, slain bool) {
	st := g.guards()
	delete(st.byKey, gd.npc.Key())
	delete(st.at, gd.npc.Pos)
	if slain {
		st.defeated[gd.npc.Key()] = g.tick
	}
}

// stepGuards fait agir chaque garde : poursuite d'un hors-la-loi à portée, sinon patrouille.
func (g *Game) stepGuards() {
	if len(g.gd.byKey) == 0 {
		return
	}
	talking := map[world.NPCKey]bool{}
	for _, p := range g.players {
		if p.talk != nil {
			talking[p.talk.npc.Key()] = true // un garde à qui l'on parle reste là
		}
	}
	for _, gd := range g.sortedGuards() {
		av := gd.avatar
		if av.fight != nil || talking[gd.npc.Key()] {
			continue
		}
		if av.calm > 0 {
			av.calm--
			continue
		}
		if gd.cooldown > 0 {
			gd.cooldown--
			continue
		}
		if t := g.outlawNear(gd); t != nil {
			gd.path = nil
			if cheb(gd.npc.Pos, t.pos) <= 1 {
				g.engageGuard(t, gd)
				continue
			}
			dx, dy := sign(t.pos.X-gd.npc.Pos.X), sign(t.pos.Y-gd.npc.Pos.Y)
			if g.moveGuard(gd, dx, dy) || g.moveGuard(gd, dx, 0) || g.moveGuard(gd, 0, dy) {
				gd.cooldown = guardChaseEvery
			}
			continue
		}
		g.patrol(gd)
	}
}

// outlawNear renvoie le hors-la-loi libre le plus proche à portée du garde (ex æquo : le plus ancien joueur).
func (g *Game) outlawNear(gd *guard) *player {
	var best *player
	bestD := guardAggro + 1
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 || p.fight != nil || p.talk != nil || !gd.wanted(p) || !gd.nearLeash(p.pos) {
			continue
		}
		if d := cheb(gd.npc.Pos, p.pos); d < bestD {
			best, bestD = p, d
		}
	}
	return best
}

// moveGuard avance le garde d'un pas ; faux si la case est prise, infranchissable ou trop loin du lieu.
// Contrairement aux monstres, un garde circule librement dans les zones sûres.
func (g *Game) moveGuard(gd *guard, dx, dy int) bool {
	if dx == 0 && dy == 0 {
		return false
	}
	dst := world.Pos{X: gd.npc.Pos.X + dx, Y: gd.npc.Pos.Y + dy}
	if g.blocked(dst) || !gd.nearLeash(dst) {
		return false
	}
	st := g.guards()
	delete(st.at, gd.npc.Pos)
	gd.npc.Pos = dst
	gd.avatar.pos = dst
	st.at[dst] = gd
	return true
}

// patrol suit un itinéraire de chemin en chemin, avec des pauses.
func (g *Game) patrol(gd *guard) {
	if gd.rest > 0 {
		gd.rest--
		return
	}
	if len(gd.path) == 0 {
		gd.path = g.routeFor(gd)
		if len(gd.path) == 0 {
			gd.rest = guardRestMax
			return
		}
	}
	next := gd.path[0]
	if g.moveGuard(gd, next.X-gd.npc.Pos.X, next.Y-gd.npc.Pos.Y) {
		gd.path, gd.stuck, gd.cooldown = gd.path[1:], 0, guardStepEvery
		if len(gd.path) == 0 {
			gd.rest = 5 + g.rng.IntN(guardRestMax)
		}
		return
	}
	if gd.stuck++; gd.stuck > 6 { // bloqué (un joueur sur le chemin) : on repart ailleurs
		gd.path, gd.stuck, gd.rest = nil, 0, 5
	}
}

// roadsOf liste les cases de chemin du lieu (mémorisé).
func (g *Game) roadsOf(s *world.Structure) []world.Pos {
	st := g.guards()
	if r, ok := st.roads[s.Region]; ok {
		return r
	}
	var roads []world.Pos
	for y := s.Origin.Y; y < s.Origin.Y+s.H; y++ {
		for x := s.Origin.X; x < s.Origin.X+s.W; x++ {
			if s.RoadAt(x, y) {
				roads = append(roads, world.Pos{X: x, Y: y})
			}
		}
	}
	st.roads[s.Region] = roads
	return roads
}

// routeFor choisit un point de passage sur les chemins du lieu et renvoie le plus court trajet pour y aller
// (sans la case de départ), ou rien s'il n'y en a pas.
func (g *Game) routeFor(gd *guard) []world.Pos {
	roads := g.roadsOf(gd.place)
	if len(roads) < 2 {
		return nil
	}
	goal := roads[g.rng.IntN(len(roads))]
	start := gd.npc.Pos
	if goal == start {
		return nil
	}
	prev := map[world.Pos]world.Pos{start: start}
	queue := []world.Pos{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == goal {
			break
		}
		for _, d := range dirs8 {
			if (d[0] != 0 && d[1] != 0) && cur != start {
				continue // en diagonale seulement au départ : on rejoint le chemin depuis n'importe quelle case
			}
			nb := world.Pos{X: cur.X + d[0], Y: cur.Y + d[1]}
			if _, seen := prev[nb]; seen || !gd.place.RoadAt(nb.X, nb.Y) {
				continue
			}
			prev[nb] = cur
			queue = append(queue, nb)
		}
	}
	if _, ok := prev[goal]; !ok {
		return nil
	}
	var path []world.Pos
	for at := goal; at != start; at = prev[at] {
		path = append(path, at)
	}
	slices.Reverse(path)
	return path
}

// engageGuard lance le combat d'un garde contre un hors-la-loi.
func (g *Game) engageGuard(p *player, gd *guard) {
	if p.fight != nil || gd.avatar.fight != nil {
		return
	}
	intro := i18n.T(p.lang, "guard.attacks", gd.npc.NameIn(p.lang), g.placeName(p.lang, gd.npc.Region))
	g.engage(p, []*monster{gd.avatar}, false, intro)
}

// bumpGuard : le joueur marche sur un garde. Un hors-la-loi se bat, les autres lui parlent.
func (g *Game) bumpGuard(p *player, gd *guard) {
	if gd.wanted(p) {
		g.engageGuard(p, gd)
		return
	}
	g.startTalk(p, gd.npc)
}

// guardSlain règle la mort d'un garde : il quitte le monde (et reviendra), le lieu ne pardonne pas.
func (g *Game) guardSlain(p *player, gd *guard) []string {
	g.removeGuard(gd, true)
	log := []string{i18n.T(p.lang, "guard.slain", g.placeName(p.lang, gd.npc.Region))}
	return append(log, g.addRep(p, gd.npc.Region, guardKillRep)...)
}

// appendGuards ajoute à la liste des monstres visibles de p les gardes de sa vue. Ils portent des identifiants
// hors de la plage des monstres (guardIDBase).
func (g *Game) appendGuards(ms []protocol.Monster, p *player) []protocol.Monster {
	if p.zone != 0 {
		return ms
	}
	for _, gd := range g.sortedGuards() {
		if inBox(gd.npc.Pos, p.pos, protocol.ViewHalfW, protocol.ViewHalfH) {
			ms = append(ms, protocol.Monster{ID: guardIDBase + gd.id, Kind: guardSpecies.Kind,
				X: gd.npc.Pos.X, Y: gd.npc.Pos.Y, Fighting: gd.avatar.fight != nil})
		}
	}
	return ms
}

// ------------------------------------------------------------------ dialogue

// whereKinds sont les métiers qu'un garde sait situer.
var whereKinds = []world.NPCKind{world.NPCHealer, world.NPCMerchant, world.NPCSmith, world.NPCArmorer}

// guardOffers est le menu d'un garde : où trouver chaque métier, puis les lieux voisins.
func (g *Game) guardOffers(p *player, npc world.NPC) []protocol.NPCOffer {
	kinds := slices.Clone(whereKinds)
	leader := world.NPCChief
	if s := g.World.StructureOfRegion(npc.Region); s != nil && s.Kind == world.KindTown {
		leader = world.NPCMayor
	}
	kinds = append(kinds, leader)
	offers := make([]protocol.NPCOffer, 0, len(kinds)+1)
	for _, k := range kinds {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCWhere, Arg: string(k), Available: true,
			Label: i18n.T(p.lang, "guard.offer.where", i18n.T(p.lang, "npc.kind."+string(k)))})
	}
	return append(offers, protocol.NPCOffer{Action: protocol.NPCPlaces, Label: i18n.T(p.lang, "guard.offer.places"), Available: true})
}

// guardAnswer répond à une question du menu d'un garde, sans modèle de langage.
func (g *Game) guardAnswer(p *player, npc world.NPC, action, arg string) []string {
	if npc.Kind != world.NPCGuard {
		return notOffered(p.lang)
	}
	l := p.lang
	if action == protocol.NPCPlaces {
		if line := g.neighboursGreeting(npc, l); line != "" {
			return []string{line}
		}
		return []string{i18n.T(l, "guard.places.none")}
	}
	kind := world.NPCKind(arg)
	if !slices.Contains(append(slices.Clone(whereKinds), world.NPCChief, world.NPCMayor), kind) {
		return notOffered(l)
	}
	label := strings.ToLower(i18n.T(l, "npc.kind."+arg))
	if s := g.World.StructureOfRegion(npc.Region); s != nil {
		for _, n := range s.NPCs {
			if n.Kind != kind {
				continue
			}
			dx, dy := n.Pos.X-npc.Pos.X, n.Pos.Y-npc.Pos.Y
			steps := cheb(npc.Pos, n.Pos)
			if steps <= 2 {
				return []string{i18n.T(l, "guard.where.near", n.NameIn(l))}
			}
			return []string{i18n.T(l, "guard.where.found", n.NameIn(l), i18n.T(l, "compass."+compass(dx, dy)), steps)}
		}
	}
	// Absent d'ici : le plus proche lieu des environs qui en a un.
	for _, s := range g.World.StructuresNear(npc.Pos, landmarkRange*world.RegionSize) {
		if s.Region == npc.Region {
			continue
		}
		for _, n := range s.NPCs {
			if n.Kind != kind {
				continue
			}
			c := s.Center()
			dx, dy := c.X-npc.Pos.X, c.Y-npc.Pos.Y
			return []string{i18n.T(l, "guard.where.elsewhere", label, s.NameIn(l),
				i18n.T(l, "place.kind."+s.Kind.String()), i18n.T(l, "compass."+compass(dx, dy)),
				i18n.T(l, "distance."+distanceKey(float64(max(abs(dx), abs(dy)))))),
			}
		}
	}
	return []string{i18n.T(l, "guard.where.nowhere", label)}
}

// residentsFor liste, pour le modèle de langage, les habitants du lieu d'un garde (vide pour un autre PNJ).
func (g *Game) residentsFor(npc world.NPC, l i18n.Lang) []npcai.Resident {
	if npc.Kind != world.NPCGuard {
		return nil
	}
	s := g.World.StructureOfRegion(npc.Region)
	if s == nil {
		return nil
	}
	out := make([]npcai.Resident, 0, len(s.NPCs))
	for _, n := range s.NPCs {
		dx, dy := n.Pos.X-npc.Pos.X, n.Pos.Y-npc.Pos.Y
		out = append(out, npcai.Resident{
			Name: n.NameIn(l), Job: strings.ToLower(i18n.T(l, "npc.kind."+string(n.Kind))),
			Direction: i18n.T(l, "compass."+compass(dx, dy)), Steps: cheb(npc.Pos, n.Pos),
		})
	}
	return out
}

// ------------------------------------------------------------------ vol

// steal : le joueur fait les poches du marchand. Réussite selon le charisme ; un échec coûte de la réputation
// dans le lieu. Un même marchand ne se laisse pas voler deux fois de suite. Renvoie le journal et si l'état du
// joueur a changé (à sauvegarder).
func (g *Game) steal(p *player, npc world.NPC) ([]string, bool) {
	st := g.guards()
	key := stealKey{p.id, npc.Key()}
	if at, ok := st.stolen[key]; ok && g.tick-at < stealCooldown {
		return []string{i18n.T(p.lang, "npc.steal.empty")}, false
	}
	if len(st.stolen) >= maxStealMemo {
		clear(st.stolen)
	}
	st.stolen[key] = g.tick
	chance := max(0.2, min(0.8, 0.5+0.05*float64(p.stats.Scores.Mod(hero.CHA))))
	if g.rng.Float64() < chance {
		gold := p.stats.AddGold(5 + 3*p.stats.Level + g.rng.IntN(6))
		return []string{i18n.T(p.lang, "npc.steal.ok", gold)}, true
	}
	log := []string{i18n.T(p.lang, "npc.steal.fail", npc.NameIn(p.lang))}
	return append(log, g.addRep(p, npc.Region, stealFailRep)...), true
}

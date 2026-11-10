package game

import (
	"cmp"
	"slices"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Donjons : zones à part, instanciées pour un groupe de joueurs (voir world/dungeon.go pour la
// génération). La carte principale est la zone 0 ; chaque instance a son identifiant, son plan,
// et ses propres index de joueurs et de monstres : les coordonnées d'un donjon sont locales à son
// plan, deux instances ou la carte principale peuvent donc se recouvrir sans jamais se heurter.

// ZoneID identifie une zone : 0 pour la carte principale, sinon une instance de donjon.
type ZoneID int

// instance est l'état d'un donjon: ses monstres vivants, ses coffres ouverts. Un donjon n'a qu'une instance, partagée
// par tous les joueurs, qui garde son état d'une visite à l'autre (en mémoire: un redémarrage du serveur la refait à
// neuf). Elle repart à neuf quand son boss meurt: voir bossSlain.
type instance struct {
	id   ZoneID
	d    *world.Dungeon
	dmap *world.DungeonMap

	byPos     map[world.Pos]int // joueurs
	monsterAt map[world.Pos]int

	opened   map[world.Pos]bool // coffres déjà ouverts
	bossDown bool               // le boss est vaincu: son coffre n'est plus verrouillé, et l'instance est « épuisée »
	resetAt  uint64             // tick à partir duquel un épuisé peut être remplacé par un donjon neuf (bossDown seulement)
}

// dungeonResetTicks est le délai, après la mort du boss, avant que le donjon puisse repartir à neuf: un jour de jeu,
// pour qu'on n'enchaîne pas les mêmes donjons.
const dungeonResetTicks = 24 * protocol.TicksPerGameHour

// resettable dit si l'instance, épuisée, peut être remplacée par un donjon neuf.
func (g *Game) resettable(inst *instance) bool { return inst.bossDown && g.tick >= inst.resetAt }

// coolingDown dit si le boss du donjon d est vaincu depuis moins d'un jour de jeu.
func (g *Game) coolingDown(d *world.Dungeon) bool {
	inst := g.instances[g.dungeonZone[d.ID]]
	return inst != nil && inst.bossDown && !g.resettable(inst)
}

// playerIndex renvoie l'index des joueurs de la zone z (la carte principale, ou une instance).
func (g *Game) playerIndex(z ZoneID) map[world.Pos]int {
	if z == 0 {
		return g.byPos
	}
	if inst := g.instances[z]; inst != nil {
		return inst.byPos
	}
	return map[world.Pos]int{} // zone disparue: un index jetable évite de salir les autres
}

// monsterIndex est playerIndex pour les monstres.
func (g *Game) monsterIndex(z ZoneID) map[world.Pos]int {
	if z == 0 {
		return g.monsterAt
	}
	if inst := g.instances[z]; inst != nil {
		return inst.monsterAt
	}
	return map[world.Pos]int{}
}

// surfacePos est la position du joueur sur la carte principale: la sienne, ou l'entrée du donjon
// où il se trouve. C'est celle qu'on sauvegarde.
func (p *player) surfacePos() world.Pos {
	if p.zone != 0 {
		return p.ret
	}
	return p.pos
}

// discoverRadius est la distance (en cases) à laquelle on remarque l'entrée secrète d'un donjon en passant.
const discoverRadius = 3

// discoveredList renvoie les régions des donjons découverts, dans un ordre stable (Y puis X).
func (p *player) discoveredList() []world.RegionPos {
	out := make([]world.RegionPos, 0, len(p.discovered))
	for rp := range p.discovered {
		out = append(out, rp)
	}
	slices.SortFunc(out, func(a, b world.RegionPos) int { return cmp.Or(a.Y-b.Y, a.X-b.X) })
	return out
}

// noticeDungeons découvre les donjons dont l'entrée est à portée de p, sans le dire à personne d'autre.
func (g *Game) noticeDungeons(p *player) {
	if !g.Dungeons {
		return
	}
	for _, d := range g.World.DungeonsWithin(p.pos.X, p.pos.Y, discoverRadius) {
		g.discover(p, d)
	}
}

// discover note que p connaît l'entrée de d, l'en informe et sauvegarde. Sans effet s'il la connaissait déjà.
func (g *Game) discover(p *player, d *world.Dungeon) {
	if p.discovered[d.Region] {
		return
	}
	p.discovered[d.Region] = true
	p.discoveredDirty = true
	g.sendDungeons(p, d.ID)
	g.save(p)
}

// sendDungeons envoie à p la liste des donjons qu'il a découverts ; newID, non vide, est celui qu'il vient de trouver.
func (g *Game) sendDungeons(p *player, newID string) {
	if p.sendDungeonsMsg == nil {
		return
	}
	msg := protocol.Dungeons{List: []protocol.DungeonInfo{}, New: newID}
	for _, rp := range p.discoveredList() {
		if d := g.World.DungeonOfRegion(rp); d != nil {
			msg.List = append(msg.List, protocol.DungeonInfo{ID: d.ID, Name: d.NameIn(p.lang), X: d.Entrance.X, Y: d.Entrance.Y, Tier: d.Tier})
		}
	}
	p.sendDungeonsMsg(msg)
}

// enterDungeon fait entrer p dans le donjon d, avec les joueurs libres qui l'entourent (même rayon que l'attraction
// d'un combat). Tous retrouvent l'instance du donjon dans l'état où elle est, ou en ouvrent une neuve s'il n'y en a
// pas (premier visiteur, ou boss vaincu depuis).
func (g *Game) enterDungeon(p *player, d *world.Dungeon) {
	party := []*player{p}
	for _, id := range g.sortedIDs() {
		o := g.players[id]
		if o != p && o.zone == 0 && o.fight == nil && o.talk == nil && cheb(o.pos, p.pos) <= groupRadius {
			party = append(party, o)
		}
	}
	inst := g.instanceOf(d)
	for _, m := range party {
		g.discover(m, d) // on connaît le chemin une fois qu'on l'a pris
		m.ret = d.Entrance
		g.teleport(m, inst.id, inst.dmap.Spawn, 0)
	}
}

// leaveDungeon ramène p devant l'entrée du donjon, sur la carte principale.
func (g *Game) leaveDungeon(p *player) {
	inst := g.instances[p.zone]
	g.teleport(p, 0, p.ret, 1) // pas sur l'entrée même, qui le ferait rentrer en la quittant
	if inst != nil {
		g.closeIfEmpty(inst)
	}
}

// teleport place p dans la zone z, sur la case libre la plus proche de c à au moins minR.
func (g *Game) teleport(p *player, z ZoneID, c world.Pos, minR int) {
	at := g.playerIndex(p.zone)
	if at[p.pos] == p.id {
		delete(at, p.pos)
	}
	p.zone = z
	p.hasPending = false
	p.lastSent = nil // le client doit recevoir une vue neuve
	g.setPos(p, g.freeNearIn(z, c, minR))
	g.sendZone(p)
}

// sendZone dit au client dans quelle carte il est. Appelée avant l'instantané qui suit un changement de zone.
func (g *Game) sendZone(p *player) {
	if p.sendZoneMsg == nil {
		return
	}
	z := protocol.Zone{}
	if inst := g.instances[p.zone]; inst != nil {
		z = protocol.Zone{Dungeon: inst.d.ID, Name: inst.d.NameIn(p.lang), Seed: inst.d.Seed, Tier: inst.d.Tier}
		if inst.bossDown && g.tick < inst.resetAt {
			z.ResetIn = int((inst.resetAt - g.tick + protocol.TicksPerGameHour - 1) / protocol.TicksPerGameHour)
		}
		for pos := range inst.opened {
			z.Opened = append(z.Opened, [2]int{pos.X, pos.Y})
		}
		slices.SortFunc(z.Opened, func(a, b [2]int) int { return cmp.Or(a[1]-b[1], a[0]-b[0]) })
	}
	p.sendZoneMsg(z)
}

// sendZoneToAll renvoie la zone à tous les joueurs de l'instance (un coffre vient de s'ouvrir).
func (g *Game) sendZoneToAll(inst *instance) {
	for _, id := range g.sortedIDs() {
		if p := g.players[id]; p.zone == inst.id {
			g.sendZone(p)
		}
	}
}

// instanceOf renvoie l'instance en cours du donjon d, ou en ouvre une neuve (premier visiteur, ou boss vaincu depuis
// plus d'un jour de jeu). Pendant ce délai, le donjon reste tel que l'a laissé la victoire: boss mort, coffres vidés.
func (g *Game) instanceOf(d *world.Dungeon) *instance {
	old := g.instances[g.dungeonZone[d.ID]]
	if old != nil && !g.resettable(old) {
		return old
	}
	inst := g.newInstance(d)
	if old != nil {
		g.closeIfEmpty(old) // l'ancienne ne sert plus qu'à ceux qui y sont encore
	}
	return inst
}

// bossSlain note la mort du boss de l'instance: son coffre se déverrouille, et le donjon pourra repartir à neuf un jour
// de jeu plus tard, pour le premier visiteur suivant. D'ici là, il reste tel quel pour tous. Plus tard, ceux qui sont
// encore dans l'ancienne instance finissent tranquillement, et elle se ferme avec son dernier joueur.
func (g *Game) bossSlain(inst *instance) {
	inst.bossDown = true
	inst.resetAt = g.tick + dungeonResetTicks
}

// newInstance ouvre une instance du donjon d et la peuple.
func (g *Game) newInstance(d *world.Dungeon) *instance {
	inst := &instance{
		id:        g.nextZone,
		d:         d,
		dmap:      world.NewDungeonMap(d),
		byPos:     make(map[world.Pos]int),
		monsterAt: make(map[world.Pos]int),
		opened:    make(map[world.Pos]bool),
	}
	g.nextZone++
	g.instances[inst.id] = inst
	g.dungeonZone[d.ID] = inst.id
	for i, s := range inst.dmap.Slots {
		sp, ok := dungeonSpecies(s.Role, i)
		if !ok {
			continue
		}
		m := g.addMonsterIn(inst.id, scaledForTier(sp, d.Tier), s.Pos)
		m.leash = garrisonLeash
	}
	return inst
}

// dungeonSpecies choisit l'espèce qui occupe l'emplacement n° i du plan : gardes et élites alternent entre
// les monstres de garnison et ceux du donjon, le boss est toujours une liche.
func dungeonSpecies(r world.SlotRole, i int) (species, bool) {
	kind := ""
	switch r {
	case world.RoleGuard:
		kind = [...]string{"skeleton", "wraith"}[i%2]
	case world.RoleElite:
		kind = [...]string{"knight", "golem"}[i%2]
	case world.RoleBoss:
		kind = "lich"
	}
	for _, s := range speciesList {
		if s.Kind == kind {
			return s, true
		}
	}
	return species{}, false
}

// scaledForTier adapte un monstre au palier du donjon (1 à 5) : 80 % de ses PV et de son XP au palier 1, jusqu'à
// 160 % au palier 5, et un point de toucher de plus tous les deux paliers.
func scaledForTier(sp species, tier int) species {
	pct := 60 + 20*tier
	sp.HP = max(1, sp.HP*pct/100)
	sp.XP = max(1, sp.XP*pct/100)
	sp.Hit += (tier - 1) / 2
	return sp
}

// scaledForLevel adapte un monstre de surface au niveau de sa région (voir world.LevelAt), de 1 à 20 : PV et XP
// augmentent de 25 % par niveau au-delà du premier, et le toucher, les dégâts et la DEF suivent à un rythme plus
// lent. Au niveau 1, l'espèce reste telle quelle.
func scaledForLevel(sp species, level int) species {
	level = max(1, level)
	pct := 100 + 25*(level-1)
	sp.HP = max(1, sp.HP*pct/100)
	sp.XP = max(1, sp.XP*pct/100)
	sp.Hit += (level - 1) / 3
	sp.Dmg = sp.Dmg.WithBonus((level - 1) / 4)
	sp.DexMod += (level - 1) / 5
	sp.Level = level
	return sp
}

// closeIfEmpty détruit l'instance, avec ses monstres, si elle peut être remplacée (boss mort depuis plus d'un jour de
// jeu) et que plus aucun joueur ne s'y trouve. Tant que le boss vit, ou que le délai court, un donjon vide garde
// son état: sinon on referait le donjon aussitôt en sortant.
func (g *Game) closeIfEmpty(inst *instance) {
	if !g.resettable(inst) {
		return
	}
	for _, p := range g.players {
		if p.zone == inst.id {
			return
		}
	}
	g.closeInstance(inst)
}

// closeInstance détruit l'instance et ses monstres, quels que soient les joueurs.
func (g *Game) closeInstance(inst *instance) {
	if g.dungeonZone[inst.d.ID] == inst.id {
		delete(g.dungeonZone, inst.d.ID)
	}
	for _, id := range g.sortedMonsterIDs() {
		if m := g.monsters[id]; m.zone == inst.id {
			g.removeMonster(m)
		}
	}
	delete(g.instances, inst.id)
}

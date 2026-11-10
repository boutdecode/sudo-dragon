package game

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Points d'intérêt de la carte principale (voir world/poi.go) : on interagit en marchant sur leur case.
//
//   - feu de camp : soigne à moitié et devient le point de réapparition après une défaite ;
//   - sanctuaire : bonus temporaire, selon le climat de la contrée ;
//   - repaire : trois monstres gardent un coffre ;
//   - tour de guet : révèle les entrées de donjon alentour (le client révèle aussi la carte) ;
//   - pierre dressée : s'active au premier passage ; deux pierres activées se relient (/menhir).
//
// L'état qui change (cooldowns, bonus, coffres de repaire) reste en mémoire ; seuls le feu de camp et les pierres
// activées sont sauvegardés avec le personnage.

const (
	campfireCooldown = 3000 // ticks (5 min) entre deux repos au même feu
	shrineCooldown   = 6000 // ticks (10 min) entre deux bénédictions du même sanctuaire
	shrineDuration   = 3000 // ticks (5 min) de bonus
	springCooldown   = 1200 // ticks (2 min) entre deux gorgées à la même source
	towerRadius      = 12   // rayon (en cases) dans lequel une tour révèle les entrées de donjon

	shrineDefBonus = 2 // DEF
	shrineHitBonus = 2 // toucher

	lairRespawn    = 18000 // ticks (30 min) avant que les gardiens reviennent et que le coffre se remplisse
	lairLeash      = 6     // distance maximale à laquelle un gardien s'éloigne de son poste
	lairLevelBonus = 2     // niveaux de région de plus pour les gardiens (un repaire est un défi)
	ruinLevelBonus = 3     // idem pour le gardien d'une ruine
	lairGearChance = 0.5   // part des coffres de repaire qui contiennent un objet
	ruinGearChance = 0.8   // idem pour une ruine
	lairSlotBase   = 100   // les emplacements d'un repaire sont numérotés à partir de là (ceux d'une forteresse, de 0)
	lairGold       = 12    // or minimal par niveau du coffre ; le maximum est le double

	stoneMinCost   = 20 // or pour la plus courte traversée
	stoneRegionFee = 15 // or par région de distance
	maxStones      = 500
)

// buffKind est le genre d'un bonus de sanctuaire.
type buffKind uint8

const (
	buffDef   buffKind = iota + 1 // + DEF
	buffHit                       // + toucher
	buffXP                        // XP ×1,5
	buffRegen                     // 1 PV de plus par seconde hors combat
)

// buff est un bonus actif jusqu'au tick until.
type buff struct {
	kind  buffKind
	until uint64
}

// shrineBuff donne le bonus d'un sanctuaire selon le climat de sa contrée.
func shrineBuff(c world.Climate) buffKind {
	switch c {
	case world.Tropical:
		return buffRegen
	case world.Arid:
		return buffHit
	case world.Boreal:
		return buffXP
	}
	return buffDef
}

// info décrit le bonus pour la fiche de personnage, avec le temps restant à l'instant now.
func (b buff) info(now uint64) protocol.Blessing {
	bl := protocol.Blessing{Secs: int((b.until - min(now, b.until) + 9) / 10)} // 10 ticks par seconde
	switch b.kind {
	case buffDef:
		bl.Kind, bl.Bonus = protocol.BlessDef, shrineDefBonus
	case buffHit:
		bl.Kind, bl.Bonus = protocol.BlessHit, shrineHitBonus
	case buffXP:
		bl.Kind, bl.Bonus = protocol.BlessXP, 50
	default:
		bl.Kind, bl.Bonus = protocol.BlessRegen, 1
	}
	return bl
}

func (p *player) hasBuff(k buffKind) bool {
	for _, b := range p.buffs {
		if b.kind == k {
			return true
		}
	}
	return false
}

// expireBuffs retire les bonus échus (fighter() ne regarde jamais l'horloge) et dit s'il y en avait.
func (p *player) expireBuffs(now uint64) bool {
	if len(p.buffs) == 0 {
		return false
	}
	n := len(p.buffs)
	p.buffs = slices.DeleteFunc(p.buffs, func(b buff) bool { return now >= b.until })
	return len(p.buffs) != n
}

// stoneList renvoie les régions des pierres activées, dans un ordre stable (Y puis X).
func (p *player) stoneList() []world.RegionPos {
	out := make([]world.RegionPos, 0, len(p.stones))
	for rp := range p.stones {
		out = append(out, rp)
	}
	slices.SortFunc(out, func(a, b world.RegionPos) int { return cmp.Or(a.Y-b.Y, a.X-b.X) })
	return out
}

// poiNotice affiche un message au joueur (même canal que les coffres).
func (g *Game) poiNotice(p *player, key string, args ...any) {
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, key, args...))
	g.sendBag(p)
}

// ticksToMinutes arrondit à la minute supérieure (une minute = 600 ticks).
func ticksToMinutes(t uint64) int { return int((t + 599) / 600) }

// cooling dit si le point de la région rp est encore sans effet pour p. Sinon, il arme le délai d.
func (g *Game) cooling(p *player, rp world.RegionPos, d uint64) (left uint64, cooling bool) {
	if until := p.poiCooling[rp]; g.tick < until {
		return until - g.tick, true
	}
	if len(p.poiCooling) > 256 {
		clear(p.poiCooling)
	}
	p.poiCooling[rp] = g.tick + d
	return 0, false
}

// poiStep applique l'effet du point d'intérêt sur lequel p vient d'arriver, s'il y en a un.
func (g *Game) poiStep(p *player, pos world.Pos) {
	if len(p.maps) > 0 {
		g.dig(p, pos) // une case de carte au trésor est une case ordinaire
	}
	poi := g.World.POIAt(pos.X, pos.Y)
	if poi == nil {
		return
	}
	switch poi.Kind {
	case world.POICampfire:
		g.campfire(p, poi)
	case world.POIShrine:
		g.shrine(p, poi)
	case world.POILair, world.POIRuin:
		g.openLairChest(p, poi)
	case world.POITower:
		g.tower(p, poi)
	case world.POIStone:
		g.stone(p, poi)
	case world.POISpring:
		g.spring(p, poi)
	}
}

func (g *Game) campfire(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if p.respawn == nil || *p.respawn != poi.Pos {
		at := poi.Pos
		p.respawn = &at
		g.poiNotice(p, "poi.camp.set", name)
		g.save(p)
	}
	if _, wait := g.cooling(p, poi.Region, campfireCooldown); wait {
		return
	}
	hp := min(p.stats.MaxHP-p.hp, (p.stats.MaxHP+1)/2)
	mana := min(p.stats.MaxMana-p.mana, (p.stats.MaxMana+1)/2)
	if hp <= 0 && mana <= 0 {
		delete(p.poiCooling, poi.Region) // rien à soigner : le feu reste disponible
		return
	}
	p.hp += hp
	p.mana += mana
	g.poiNotice(p, "poi.camp.rest", hp, mana)
}

// respawnSpot renvoie la case où réapparaît p après une défaite et si c'est un feu de camp.
func (g *Game) respawnSpot(p *player) (world.Pos, bool) {
	if p.respawn != nil {
		return g.freeNear(*p.respawn), true
	}
	return g.freeSpawn(), false
}

// spring soigne 30 % des PV et 50 % du mana (une oasis, en climat aride : 60 % et tout le mana). Le délai ne court que
// si l'on a bu : on ne perd rien à passer à pleine forme.
func (g *Game) spring(p *player, poi *world.POI) {
	hpFrac, manaFrac, key := 0.3, 0.5, "poi.spring.drink"
	if g.World.ClimateAt(poi.Pos.X, poi.Pos.Y) == world.Arid {
		hpFrac, manaFrac, key = 0.6, 1.0, "poi.spring.oasis"
	}
	hp := min(p.stats.MaxHP-p.hp, int(float64(p.stats.MaxHP)*hpFrac+0.5))
	mana := min(p.stats.MaxMana-p.mana, int(float64(p.stats.MaxMana)*manaFrac+0.5))
	if hp <= 0 && mana <= 0 {
		return
	}
	if _, wait := g.cooling(p, poi.Region, springCooldown); wait {
		return
	}
	p.hp += hp
	p.mana += mana
	g.poiNotice(p, key, poi.NameIn(p.lang), hp, mana)
}

func (g *Game) shrine(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if left, wait := g.cooling(p, poi.Region, shrineCooldown); wait {
		g.poiNotice(p, "poi.shrine.cooling", name, ticksToMinutes(left))
		return
	}
	kind := shrineBuff(g.World.ClimateAt(poi.Pos.X, poi.Pos.Y))
	p.buffs = slices.DeleteFunc(p.buffs, func(b buff) bool { return b.kind == kind })
	p.buffs = append(p.buffs, buff{kind: kind, until: g.tick + shrineDuration})
	g.poiNotice(p, fmt.Sprintf("poi.shrine.buff.%d", kind), name, shrineDuration/600)
}

// applyBuffs ajoute les bonus de sanctuaire à une fiche de combat.
func (p *player) applyBuffs(def, hit *int) {
	if p.hasBuff(buffDef) {
		*def += shrineDefBonus
	}
	if p.hasBuff(buffHit) {
		*hit += shrineHitBonus
	}
}

// xpWithBuff applique le bonus d'XP d'un sanctuaire.
func (p *player) xpWithBuff(xp int) int {
	if p.hasBuff(buffXP) {
		return xp * 3 / 2
	}
	return xp
}

// regenBuffs rend 1 PV de plus par seconde aux joueurs qui ont la bénédiction de régénération.
func (g *Game) regenBuffs(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.fight == nil && p.hp < p.stats.MaxHP && p.hasBuff(buffRegen) {
			p.hp++
		}
	}
}

func (g *Game) tower(p *player, poi *world.POI) {
	g.poiNotice(p, "poi.tower.climb", poi.NameIn(p.lang))
	if !g.Dungeons {
		return
	}
	for _, d := range g.World.DungeonsWithin(poi.Pos.X, poi.Pos.Y, towerRadius) {
		g.discover(p, d)
	}
}

// --- Repaires ---

// lairKey est la clé de l'emplacement i du repaire poi.
func lairKey(poi *world.POI, i int) world.SlotKey {
	return world.SlotKey{Region: poi.Region, Index: lairSlotBase + i}
}

// activateLairs peuple les repaires proches d'un joueur, comme activateGarrisons pour les forteresses.
func (g *Game) activateLairs() {
	for _, id := range g.sortedIDs() {
		p := g.players[id]
		if p.zone != 0 {
			continue
		}
		for _, poi := range g.World.POIsWithin(p.pos.X, p.pos.Y, garrisonActivation) {
			if poi.Kind != world.POILair && poi.Kind != world.POIRuin {
				continue
			}
			for i, pos := range poi.Slots {
				g.fillLairSlot(poi, i, pos)
			}
		}
	}
}

func (g *Game) fillLairSlot(poi *world.POI, i int, pos world.Pos) {
	key := lairKey(poi, i)
	if _, alive := g.slotMonster[key]; alive {
		return
	}
	if at, beaten := g.slotDefeated[key]; beaten {
		if g.tick-at < lairRespawn {
			return
		}
		delete(g.slotDefeated, key)
	}
	if g.blocked(pos) {
		return
	}
	// Les gardiens sont des bêtes du biome, un peu plus fortes que la région.
	list := speciesByTile[g.World.TileAt(pos.X, pos.Y)]
	if len(list) == 0 {
		list = speciesByTile[world.Plain]
	}
	sp := list[int((uint64(poi.Seed)>>8+uint64(i))%uint64(len(list)))]
	bonus := lairLevelBonus
	if poi.Kind == world.POIRuin {
		bonus = ruinLevelBonus // le gardien d'une ruine est une élite
	}
	level := min(world.LevelAt(pos.X, pos.Y)+bonus, 20)
	m := g.addMonster(scaledForLevel(sp, level), pos)
	m.slot = &key
	m.leash = lairLeash
	g.slotMonster[key] = m.id
	g.garrisonCount++
}

// lairGuarded dit si un gardien du repaire est encore en vie.
func (g *Game) lairGuarded(poi *world.POI) bool {
	for i := range poi.Slots {
		if _, alive := g.slotMonster[lairKey(poi, i)]; alive {
			return true
		}
	}
	return false
}

// openLairChest ouvre le coffre d'un repaire ou d'une ruine dont les gardiens sont tombés. Le coffre se remplit de
// nouveau lairRespawn ticks après son ouverture ; un sac plein le laisse fermé. Une ruine donne en plus un fragment
// de lore et, avec les donjons, l'entrée du plus proche.
func (g *Game) openLairChest(p *player, poi *world.POI) {
	ruin := poi.Kind == world.POIRuin
	prefix := "poi.lair"
	if ruin {
		prefix = "poi.ruin"
	}
	if g.lairGuarded(poi) {
		g.poiNotice(p, prefix+".guarded")
		return
	}
	if at, opened := g.lairOpened[poi.Region]; opened && g.tick-at < lairRespawn {
		g.poiNotice(p, prefix+".empty")
		return
	}
	level := world.LevelAt(poi.Pos.X, poi.Pos.Y)
	gearChance, goldBase := lairGearChance, lairGold*level
	if ruin {
		gearChance, goldBase = ruinGearChance, lairGold*level*3/2
	}
	var loot *gear.Item
	if g.rng.Float64() < gearChance {
		if len(p.inv.Items) >= gear.BagCapacity {
			g.poiNotice(p, "dungeon.chest.bag_full")
			return
		}
		it := gear.MonsterLoot(p.lang, poi.Region, gear.TierForLevel(level)+1, g.rng.Uint64())
		loot = &it
	}
	g.lairOpened[poi.Region] = g.tick

	gold := p.stats.AddGold(goldBase + g.rng.IntN(goldBase+1))
	potions := p.stats.AddPotions(1 + g.rng.IntN(2))
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.open", gold, potions))
	if loot != nil {
		added, _ := p.inv.Add(*loot)
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "dungeon.chest.item", added.Name))
		p.gearDirty = true
	}
	if ruin {
		g.ruinLore(p, poi)
	}
	chance := mapChestLair
	if ruin {
		chance = mapChestRuin
	}
	if g.rng.Float64() < chance {
		if line := g.giveMap(p, poi.Pos); line != "" {
			p.gearNotices = append(p.gearNotices, line)
		}
	}
	g.sendBag(p)
	g.save(p)
}

// ruinLore ajoute au journal le fragment de lore de la ruine (texte statique tiré de sa graine) et, si le joueur ne
// la connaît pas encore, révèle l'entrée du donjon le plus proche (3 régions au plus).
func (g *Game) ruinLore(p *player, poi *world.POI) {
	p.gearNotices = append(p.gearNotices, i18n.Pick(p.lang, "poi.lore", int(uint64(poi.Seed)>>8)))
	if !g.Dungeons {
		return
	}
	var best *world.Dungeon
	bestDist := 0
	for _, d := range g.World.DungeonsNear(poi.Pos.X, poi.Pos.Y, 3) {
		if dist := max(abs(d.Entrance.X-poi.Pos.X), abs(d.Entrance.Y-poi.Pos.Y)); best == nil || dist < bestDist {
			best, bestDist = d, dist
		}
	}
	if best == nil || p.discovered[best.Region] {
		return
	}
	g.discover(p, best)
	p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "poi.ruin.hint", best.NameIn(p.lang)))
}

// --- Pierres dressées ---

func (g *Game) stone(p *player, poi *world.POI) {
	name := poi.NameIn(p.lang)
	if p.stones[poi.Region] {
		g.poiNotice(p, "poi.stone.known", name)
		return
	}
	if len(p.stones) >= maxStones {
		return
	}
	p.stones[poi.Region] = true
	g.poiNotice(p, "poi.stone.activated", name)
	g.save(p)
}

// stoneCost est le prix du trajet entre deux pierres : proportionnel à la distance en régions.
func stoneCost(a, b world.RegionPos) int {
	return max(stoneMinCost, stoneRegionFee*max(abs(a.X-b.X), abs(a.Y-b.Y)))
}

// Menhir traite la commande /menhir : sans argument, liste les pierres activées avec le prix du trajet ; avec un
// numéro ou un début de nom, y emmène le joueur, qui doit se tenir sur une pierre activée. Renvoie le texte à lui
// montrer, dans sa langue.
func (g *Game) Menhir(id int, arg string) (string, error) {
	reply := make(chan string, 1)
	if !g.do(func() { reply <- g.menhir(id, arg) }) {
		return "", ErrStopped
	}
	select {
	case s := <-reply:
		return s, nil
	case <-g.done:
		return "", ErrStopped
	}
}

func (g *Game) menhir(id int, arg string) string {
	p, ok := g.players[id]
	if !ok {
		return ""
	}
	t := func(key string, args ...any) string { return i18n.T(p.lang, key, args...) }
	here := g.World.POIAt(p.pos.X, p.pos.Y)
	if p.zone != 0 || here == nil || here.Kind != world.POIStone || !p.stones[here.Region] {
		return t("poi.menhir.not_on_stone")
	}
	type dest struct {
		poi  *world.POI
		cost int
	}
	var dests []dest
	for _, rp := range p.stoneList() {
		if rp == here.Region {
			continue
		}
		if o := g.World.POIOfRegion(rp); o != nil && o.Kind == world.POIStone {
			dests = append(dests, dest{o, stoneCost(here.Region, rp)})
		}
	}
	slices.SortStableFunc(dests, func(a, b dest) int { return a.cost - b.cost })
	if len(dests) == 0 {
		return t("poi.menhir.none")
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		var b strings.Builder
		b.WriteString(t("poi.menhir.list"))
		for i, d := range dests {
			fmt.Fprintf(&b, "\n %d. %s (%d, %d) · %d %s", i+1, d.poi.NameIn(p.lang), d.poi.Pos.X, d.poi.Pos.Y, d.cost, t("poi.menhir.gold"))
		}
		return b.String()
	}
	var pick *dest
	if n, err := strconv.Atoi(arg); err == nil {
		if n >= 1 && n <= len(dests) {
			pick = &dests[n-1]
		}
	} else {
		low := strings.ToLower(arg)
		for i := range dests {
			if strings.Contains(strings.ToLower(dests[i].poi.NameIn(p.lang)), low) {
				if pick != nil {
					return t("poi.menhir.ambiguous", arg)
				}
				pick = &dests[i]
			}
		}
	}
	if pick == nil {
		return t("poi.menhir.unknown", arg)
	}
	if p.fight != nil || p.talk != nil {
		return t("poi.menhir.busy")
	}
	if !p.stats.SpendGold(pick.cost) {
		return t("poi.menhir.poor", pick.cost)
	}
	g.setPos(p, g.freeNear(pick.poi.Pos))
	g.save(p)
	return t("poi.menhir.done", pick.poi.NameIn(p.lang), pick.cost)
}

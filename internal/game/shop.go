package game

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Équipement : boutique des forgerons et armuriers, sac, équipement porté.
// Le modèle de langage n'invente que l'étalage (voir npcai.Stock, validé par gear.Validate) ; achats,
// reventes, niveau requis et bonus de combat sont décidés ici.

// Stocker est l'interface facultative d'un Talker capable de fournir l'assortiment d'un vendeur.
type Stocker interface {
	Stock(req npcai.StockRequest, done func(npcai.StockReply))
}

const (
	maxShopLines    = 9   // les achats se choisissent par un chiffre de 1 à 9
	stockRetryTicks = 600 // 60 s avant de redemander un étalage qui a échoué
	maxStockedWares = 256
)

// shopWares est l'étalage généré (ou relu) d'un vendeur, avec un numéro de révision.
type shopWares struct {
	wares gear.Wares
	rev   int
}

func isVendor(k world.NPCKind) bool { return k == world.NPCSmith || k == world.NPCArmorer }

// vendorContext donne ce que le serveur autorise à vendre à ce vendeur dans son lieu.
func (g *Game) vendorContext(npc world.NPC) gear.Context {
	c := gear.Context{Slots: gear.SlotsOfSmith, MaxTier: 3, Origin: npc.Key()}
	if npc.Kind == world.NPCArmorer {
		c.Slots = gear.SlotsOfArmorer
	}
	// Plus la région est lointaine, plus l'assortiment monte en rang: le plancher suit la tranche de niveaux.
	// La tranche de départ garde son plafond d'avant (3).
	rp := npc.Region
	band := world.LevelBand(world.LevelAt(rp.X*world.RegionSize+world.RegionSize/2, rp.Y*world.RegionSize+world.RegionSize/2))
	c.MaxTier = min(c.MaxTier+band, gear.MaxTier)
	c.MinTier = 1 + band
	if s := g.World.StructureOfRegion(rp); s != nil && s.Kind == world.KindTown {
		c.MaxTier = gear.MaxTier
	}
	return c
}

// waresFor renvoie l'étalage à montrer au joueur : celui généré s'il existe, sinon le repli déterministe,
// en demandant (une fois) la génération au modèle si possible. rev vaut 0 pour le repli.
func (g *Game) waresFor(p *player, npc world.NPC) (w gear.Wares, rev int) {
	if npc.Index >= world.CaravanIndexBase && npc.Index < world.CaravanIndexBase+2 { // marchand ambulant
		return g.caravanWares(p, npc)
	}
	key := npc.Key()
	if st, ok := g.shops[key]; ok {
		return st.wares, st.rev
	}
	g.requestStock(p, npc)
	return gear.Fallback(key, g.vendorContext(npc), p.lang), 0
}

// requestStock demande l'étalage au service de langage (jamais bloquant, une demande à la fois par vendeur).
func (g *Game) requestStock(p *player, npc world.NPC) {
	st, ok := g.Talker.(Stocker)
	key := npc.Key()
	if !ok || !g.talkerReady() || g.stockPending[key] || g.tick < g.stockRetryAt[key] {
		return
	}
	if g.stockPending == nil {
		g.stockPending, g.stockRetryAt = map[world.NPCKey]bool{}, map[world.NPCKey]uint64{}
	}
	g.stockPending[key] = true
	place, kind := g.placeName(p.lang, npc.Region), world.KindVillage
	if s := g.World.StructureOfRegion(npc.Region); s != nil {
		kind = s.Kind
	}
	req := npcai.StockRequest{
		NPC: npc, Lang: p.lang, Place: place, PlaceKind: kind,
		Region:  g.World.TerritoryAt(npc.Pos.X, npc.Pos.Y).NameIn(p.lang),
		Context: g.vendorContext(npc),
	}
	st.Stock(req, func(rep npcai.StockReply) {
		g.do(func() { g.stockArrived(key, rep) })
	})
}

// stockArrived enregistre l'étalage reçu et met à jour les joueurs qui regardent ce vendeur.
func (g *Game) stockArrived(key world.NPCKey, rep npcai.StockReply) {
	delete(g.stockPending, key)
	if rep.Failure != npcai.OK || len(rep.Wares.Items) == 0 {
		g.stockRetryAt[key] = g.tick + stockRetryTicks
		return
	}
	if len(g.shops) >= maxStockedWares {
		clear(g.shops)
	}
	if g.shops == nil {
		g.shops = map[world.NPCKey]shopWares{}
	}
	g.shops[key] = shopWares{wares: rep.Wares, rev: 1}
	for _, p := range g.players {
		if p.talk != nil && p.talk.npc.Key() == key && p.talk.mode == protocol.NPCModeMenu {
			g.sendTalk(p, p.talk.npc.Kind, nil, false)
		}
	}
}

// ---------------------------------------------------------------- calculs et présentation

// weaponKind est le style d'attaque de l'arme portée (mêlée à mains nues).
func (p *player) weaponKind() hero.Kind {
	if w := p.inv.Equipped().Weapon; w != nil {
		return w.Kind.OrMelee()
	}
	return hero.Melee
}

func (g *Game) itemInfo(l i18n.Lang, p *player, it gear.Item) protocol.ItemInfo {
	info := protocol.ItemInfo{
		ID: it.ID, Name: it.Name, Desc: it.Desc, Slot: string(it.Slot), Kind: string(it.Kind), Tier: it.Tier, Family: it.Family, Def: it.Def,
		MinLevel: it.MinLevel(), Sell: it.SellPrice(), Worn: p.inv.IsWorn(it.ID),
	}
	if s := g.World.StructureOfRegion(it.Origin.Region); s != nil && it.Origin.Index >= 0 && it.Origin.Index < len(s.NPCs) {
		info.Origin = s.NPCs[it.Origin.Index].NameIn(l)
	}
	if d := it.Damage(); !d.IsZero() {
		info.Dice = d.String()
	}
	if r := it.Reduction(); !r.IsZero() {
		info.Reduction = r.String()
	}
	return info
}

// sendBag envoie le sac complet (et les notifications en attente).
func (g *Game) sendBag(p *player) {
	if p.sendBagMsg == nil {
		return
	}
	eq := p.inv.Equipped()
	b := protocol.Bag{Items: make([]protocol.ItemInfo, 0, len(p.inv.Items)), Capacity: gear.BagCapacity, DefBonus: eq.Defense(), Reduction: eq.Reduction().String(),
		Class: string(p.stats.Class), Scores: p.stats.Scores[:], Style: string(p.weaponKind()), HitDie: p.stats.Class.HitDie(),
		Kills: p.stats.Kills, BossKills: p.stats.BossKills, Deaths: p.stats.Deaths}
	for _, it := range p.inv.Items {
		b.Items = append(b.Items, g.itemInfo(p.lang, p, it))
	}
	for _, bf := range p.buffs {
		b.Blessings = append(b.Blessings, bf.info(g.tick))
	}
	for _, mp := range p.maps {
		b.Maps = append(b.Maps, protocol.TreasureMap{X: mp.X, Y: mp.Y})
	}
	b.Resources = resourceStacks(p.resources)
	b.Notice, p.gearNotices = p.gearNotices, nil
	p.sendBagMsg(b)
}

// gearChanged note un changement durable et met le sac à jour.
func (g *Game) gearChanged(p *player) {
	p.gearDirty = true
	g.sendBag(p)
}

// itemDetail décrit un objet à vendre : bonus (avec la différence par rapport à l'objet porté) et niveau requis.
func (g *Game) itemDetail(l i18n.Lang, p *player, it gear.Item) string {
	cur := p.inv.Equipped().Get(it.Slot)
	if it.Slot == gear.Weapon {
		avg, delta := it.Damage().Avg(), it.Damage().Avg()
		if cur != nil {
			delta -= cur.Damage().Avg()
		} else {
			delta -= combat.BareHands.Avg()
		}
		return i18n.T(l, "gear.detail.atk", it.Damage().String(), i18n.T(l, "gear.family."+it.Family), avg, delta, it.MinLevel())
	}
	delta := it.Def
	if cur != nil {
		delta -= cur.Def
	}
	red := ""
	if r := it.Reduction(); !r.IsZero() {
		red = i18n.T(l, "gear.detail.red", r.String())
	}
	return i18n.T(l, "gear.detail.def", i18n.T(l, "gear.slot."+string(it.Slot)), it.Def, delta, red, it.MinLevel())
}

// shopOffers construit les offres d'un vendeur : l'étalage, puis la revente.
func (g *Game) shopOffers(p *player, npc world.NPC) []protocol.NPCOffer {
	w, rev := g.waresFor(p, npc)
	var offers []protocol.NPCOffer
	for i, it := range w.Items {
		if i >= maxShopLines {
			break
		}
		price := g.priceFor(p, npc, it.Price)
		info := g.itemInfo(p.lang, p, it)
		offers = append(offers, protocol.NPCOffer{
			Action: protocol.NPCBuyItem, Label: it.Name, Detail: g.itemDetail(p.lang, p, it), Price: price, Info: &info,
			Arg:       fmt.Sprintf("%d.%d", rev, it.ID),
			Available: p.stats.Gold >= price && len(p.inv.Items) < gear.BagCapacity,
		})
	}
	if len(g.sellable(p)) > 0 {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCSellMenu, Label: i18n.T(p.lang, "npc.offer.sell_item"), Available: true})
	}
	return offers
}

// sellable renvoie les objets du sac qui peuvent être revendus (non portés), les plus chers d'abord.
func (g *Game) sellable(p *player) []gear.Item {
	var out []gear.Item
	for _, it := range p.inv.Items {
		if !p.inv.IsWorn(it.ID) {
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(a, b gear.Item) int { return b.Price - a.Price })
	if len(out) > maxShopLines {
		out = out[:maxShopLines]
	}
	return out
}

func (g *Game) sellOffers(p *player) []protocol.NPCOffer {
	var offers []protocol.NPCOffer
	for _, it := range g.sellable(p) {
		info := g.itemInfo(p.lang, p, it)
		offers = append(offers, protocol.NPCOffer{
			Action: protocol.NPCSellItem, Label: it.Name, Detail: g.itemDetail(p.lang, p, it), Price: it.SellPrice(), Gain: true, Info: &info,
			Arg: strconv.FormatInt(it.ID, 10), Available: p.stats.Gold <= combat.MaxGold-it.SellPrice(),
		})
	}
	offers = append(offers, protocol.NPCOffer{Action: protocol.NPCBack, Label: i18n.T(p.lang, "npc.offer.back"), Available: true})
	return offers
}

// ---------------------------------------------------------------- achats et reventes

// buyItem achète l'objet de l'étalage désigné par arg ("révision.identifiant").
func (g *Game) buyItem(p *player, npc world.NPC, arg string) []string {
	revStr, idStr, ok := strings.Cut(arg, ".")
	rev, errR := strconv.Atoi(revStr)
	id, errI := strconv.ParseInt(idStr, 10, 64)
	if !ok || errR != nil || errI != nil {
		return notOffered(p.lang)
	}
	w, curRev := g.waresFor(p, npc)
	if rev != curRev {
		return []string{i18n.T(p.lang, "shop.changed")} // l'étalage a été renouvelé pendant la visite
	}
	var item *gear.Item
	for i := range w.Items {
		if w.Items[i].ID == id {
			item = &w.Items[i]
		}
	}
	if item == nil {
		return notOffered(p.lang)
	}
	price := g.priceFor(p, npc, item.Price)
	switch {
	case len(p.inv.Items) >= gear.BagCapacity:
		return []string{i18n.T(p.lang, "shop.bag_full")}
	case !p.stats.SpendGold(price):
		return []string{i18n.T(p.lang, "npc.missing_gold", price-p.stats.Gold)}
	}
	bought := *item
	bought.Origin = npc.Key()
	added, err := p.inv.Add(bought)
	if err != nil { // ne devrait pas arriver : la place a été vérifiée
		p.stats.AddGold(price)
		return []string{i18n.T(p.lang, "shop.bag_full")}
	}
	g.gearChanged(p)
	g.save(p)
	return []string{i18n.T(p.lang, "shop.bought", added.Name, price)}
}

// sellItem revend un objet non porté du sac.
func (g *Game) sellItem(p *player, arg string) []string {
	id, err := strconv.ParseInt(arg, 10, 64)
	it := p.inv.Find(id)
	switch {
	case err != nil || it == nil:
		return notOffered(p.lang)
	case p.inv.IsWorn(id):
		return []string{i18n.T(p.lang, "shop.worn")}
	case p.stats.Gold > combat.MaxGold-it.SellPrice():
		return []string{i18n.T(p.lang, "npc.purse_full")}
	}
	gone, _ := p.inv.Remove(id)
	p.stats.AddGold(gone.SellPrice())
	g.gearChanged(p)
	g.save(p)
	return []string{i18n.T(p.lang, "shop.sold", gone.Name, gone.SellPrice())}
}

// ---------------------------------------------------------------- équiper

// GearAct applique une action sur le sac (appelée depuis la session réseau).
func (g *Game) GearAct(id int, action string, itemID int64, slot string) {
	g.do(func() { g.gearAct(id, action, itemID, slot) })
}

func (g *Game) gearAct(id int, action string, itemID int64, slot string) {
	{
		p, ok := g.players[id]
		if !ok {
			return
		}
		if p.fight != nil {
			p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "gear.not_in_fight"))
			g.sendBag(p)
			return
		}
		switch action {
		case protocol.GearEquip:
			it := p.inv.Find(itemID)
			if it == nil {
				return
			}
			if err := p.inv.Equip(itemID, p.stats.Level); err != nil {
				key := "gear.level_too_low"
				if errors.Is(err, gear.ErrTwoHanded) {
					key = "gear.two_handed"
				}
				p.gearNotices = append(p.gearNotices, i18n.T(p.lang, key, it.Name, it.MinLevel()))
				g.sendBag(p)
				return
			}
			p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "gear.equipped", it.Name))
		case protocol.GearUnequip:
			s := gear.Slot(slot)
			cur := p.inv.Equipped().Get(s)
			if !s.Valid() || cur == nil {
				return
			}
			p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "gear.unequipped", cur.Name))
			p.inv.Unequip(s)
		default:
			return
		}
		g.gearChanged(p)
		g.save(p)
	}
}

// GearActNow est GearAct appliqué directement (goroutine de jeu, tests).
func (g *Game) GearActNow(p *player, action string, itemID int64, slot string) {
	g.gearAct(p.id, action, itemID, slot)
}

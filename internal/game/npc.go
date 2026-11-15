package game

import (
	"strconv"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// NPC prices, in gold. A potion resells for much less than it costs to buy.
const (
	potionBuyPrice    = 20
	potionSellPrice   = 8
	healPricePerLevel = 4
)

// healPrice is the healer's rate: free at level 1 (we do not block a
// beginner with no money), then 4 gold per level.
// buyPrice and healCost apply the reputation discount of the NPC's place. The menu and the
// transaction both go through these two functions, to stay consistent.
func (g *Game) buyPrice(p *player, npc world.NPC) int { return g.priceFor(p, npc, potionBuyPrice) }

func (g *Game) healCost(p *player, npc world.NPC) int {
	return g.priceFor(p, npc, healPrice(p.stats.Level))
}

// priceFor applies to a price the reputation discount of the NPC's place, then the effect of the
// player's charisma (NPCs judge them by their looks).
func (g *Game) priceFor(p *player, npc world.NPC, base int) int {
	return hero.AdjustPrice(quest.Discount(base, p.rep[npc.Region]), p.stats.Scores.Mod(hero.CHA))
}

func healPrice(level int) int {
	if level <= 1 {
		return 0
	}
	return healPricePerLevel * level
}

// talk is an ongoing conversation between a player and an NPC. As with a
// fight, the player stays still while it lasts.
type talk struct {
	npc     world.NPC
	lastAct uint64 // tick of the player's last action

	// Free discussion (see talk.go).
	gen     uint64             // identifier of this conversation: a late reply from another one is ignored
	mode    string             // protocol.NPCMode*
	lines   []protocol.NPCLine // displayed transcript
	history []npcai.Turn       // exchanges handed to the model
	pending bool               // an NPC reply is awaited
	opened  bool               // the NPC has already opened the discussion (only once per conversation)

	// Quest offered by the model and not yet accepted (quest mode).
	proposal *quest.Quest
	prevMode string
}

// ValidNPCAction tells whether action is a known dialogue action.
func ValidNPCAction(action string) bool {
	switch action {
	case protocol.NPCBuy, protocol.NPCSell, protocol.NPCHeal, protocol.NPCLeave,
		protocol.NPCTalk, protocol.NPCSay, protocol.NPCConsent, protocol.NPCDecline, protocol.NPCBack,
		protocol.NPCQuestAccept, protocol.NPCQuestRefuse, protocol.NPCTurnIn, protocol.NPCAskWork,
		protocol.NPCBuyItem, protocol.NPCSellItem, protocol.NPCSellMenu,
		protocol.NPCWhere, protocol.NPCPlaces, protocol.NPCSteal:
		return true
	}
	return false
}

// TalkAct applies a player's action during a conversation (text is only read for
// NPCSay). No effect outside a conversation or if the action is unknown.
func (g *Game) TalkAct(id int, action, text string) {
	if !ValidNPCAction(action) {
		return
	}
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.talkAct(p, action, text)
		}
	})
}

// startTalk opens a conversation if the player is free.
func (g *Game) startTalk(p *player, npc world.NPC) {
	if p.fight != nil || p.talk != nil {
		return
	}
	g.talkSeq++
	p.talk = &talk{npc: npc, lastAct: g.tick, gen: g.talkSeq, mode: protocol.NPCModeMenu}
	p.hasPending = false
	greeting := i18n.T(p.lang, "npc.greet.merchant")
	switch npc.Kind {
	case world.NPCHealer:
		greeting = i18n.T(p.lang, "npc.greet.healer")
	case world.NPCSmith, world.NPCArmorer:
		greeting = i18n.T(p.lang, "npc.greet."+string(npc.Kind))
	case world.NPCGuard:
		greeting = i18n.T(p.lang, "npc.greet.guard")
	case world.NPCChief, world.NPCMayor:
		greeting = i18n.T(p.lang, "npc.greet."+string(npc.Kind))
		if !g.talkerReady() {
			// Without a language model, the chief still informs: their nearest neighbours.
			if line := g.neighboursGreeting(npc, p.lang); line != "" {
				greeting = line
			} else {
				greeting = ambientLine(p.lang, npc)
			}
		}
	case world.NPCVillager:
		// Without a trade, a villager only has conversation. When it is unavailable
		// (no key, outage), they say a fixed ambient sentence so the world stays alive.
		greeting = i18n.T(p.lang, "npc.greet.villager")
		if !g.talkerReady() {
			greeting = ambientLine(p.lang, npc)
		}
	}
	if npc.Index >= world.CaravanIndexBase && npc.Index < world.CaravanIndexBase+2 {
		greeting = i18n.T(p.lang, "npc.greet.caravan")
	}
	g.sendTalk(p, npc.Kind, []string{greeting}, false)
}

// closeTalk ends the conversation and notifies the client.
func (g *Game) closeTalk(p *player, farewell string) {
	if p.talk == nil {
		return
	}
	kind := p.talk.npc.Kind
	p.talk = nil
	g.sendTalk(p, kind, []string{farewell}, true)
}

func (g *Game) talkAct(p *player, action, text string) {
	t := p.talk
	if t == nil {
		return
	}
	t.lastAct = g.tick

	switch action {
	case protocol.NPCLeave:
		g.closeTalk(p, i18n.T(p.lang, "npc.farewell"))
		return
	case protocol.NPCTalk, protocol.NPCSay, protocol.NPCConsent, protocol.NPCDecline, protocol.NPCBack:
		g.chatAct(p, action, text)
		return
	case protocol.NPCQuestAccept, protocol.NPCQuestRefuse:
		for _, l := range g.questAct(p, action, text) {
			t.lines = appendLine(t.lines, protocol.NPCLine{Who: "sys", Text: l})
		}
		g.sendTalk(p, t.npc.Kind, nil, false)
		return
	}
	if t.mode == protocol.NPCModeSell && action == protocol.NPCSellItem && isVendor(t.npc.Kind) {
		log := g.sellItem(p, text)
		if len(g.sellable(p)) == 0 {
			t.mode = protocol.NPCModeMenu
		}
		g.sendTalk(p, t.npc.Kind, log, false)
		return
	}
	if t.mode != protocol.NPCModeMenu {
		return // trade and healing are only done from the menu
	}

	var log []string
	changed := false
	buyPrice, repairPrice := g.buyPrice(p, t.npc), g.healCost(p, t.npc)
	switch action {

	case protocol.NPCTurnIn:
		log = g.questAct(p, action, text) // the hand-in records itself

	case protocol.NPCAskWork:
		log = g.askWork(p)

	case protocol.NPCWhere, protocol.NPCPlaces:
		log = g.guardAnswer(p, t.npc, action, text)

	case protocol.NPCSteal:
		if t.npc.Kind != world.NPCMerchant {
			log = notOffered(p.lang)
		} else {
			log, changed = g.steal(p, t.npc)
		}

	case protocol.NPCBuyItem:
		if !isVendor(t.npc.Kind) {
			log = notOffered(p.lang)
		} else {
			log = g.buyItem(p, t.npc, text) // the purchase records itself
		}

	case protocol.NPCSellMenu:
		if !isVendor(t.npc.Kind) || len(g.sellable(p)) == 0 {
			log = notOffered(p.lang)
		} else {
			t.mode = protocol.NPCModeSell
		}

	case protocol.NPCSellItem:
		log = notOffered(p.lang) // resale is done from the resale list

	case protocol.NPCBuy:
		switch {
		case t.npc.Kind != world.NPCMerchant:
			log = notOffered(p.lang)
		case p.stats.Potions >= combat.MaxPotions:
			log = []string{i18n.T(p.lang, "npc.bag_full")}
		case !p.stats.SpendGold(buyPrice):
			log = []string{i18n.T(p.lang, "npc.missing_gold", buyPrice-p.stats.Gold)}
		default:
			p.stats.AddPotions(1)
			changed = true
			log = []string{i18n.T(p.lang, "npc.bought", buyPrice)}
		}

	case protocol.NPCSell:
		switch {
		case t.npc.Kind != world.NPCMerchant:
			log = notOffered(p.lang)
		case p.stats.Potions <= 0:
			log = []string{i18n.T(p.lang, "npc.no_potion_to_sell")}
		case p.stats.Gold > combat.MaxGold-potionSellPrice:
			log = []string{i18n.T(p.lang, "npc.purse_full")} // we do not lose a potion for nothing
		default:
			p.stats.Potions--
			p.stats.AddGold(potionSellPrice)
			changed = true
			log = []string{i18n.T(p.lang, "npc.sold", potionSellPrice)}
		}

	case protocol.NPCHeal:
		price := repairPrice
		switch {
		case t.npc.Kind != world.NPCHealer:
			log = notOffered(p.lang)
		case p.hp >= p.stats.MaxHP && p.mana >= p.stats.MaxMana:
			log = []string{i18n.T(p.lang, "npc.already_healthy")} // nothing is debited
		case !p.stats.SpendGold(price):
			log = []string{i18n.T(p.lang, "npc.missing_gold", price-p.stats.Gold)}
		default:
			p.hp, p.mana = p.stats.MaxHP, p.stats.MaxMana
			changed = true
			if price == 0 {
				log = []string{i18n.T(p.lang, "npc.healed_free")}
			} else {
				log = []string{i18n.T(p.lang, "npc.healed", price)}
			}
		}
	}

	if changed {
		g.save(p) // a transaction changes progression: we do not wait for the periodic save
	}
	g.sendTalk(p, t.npc.Kind, log, false)
}

func notOffered(l i18n.Lang) []string { return []string{i18n.T(l, "npc.not_offered")} }

// expireIdleTalks closes abandoned conversations.
func (g *Game) expireIdleTalks(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.talk != nil && g.tick-p.talk.lastAct > p.talk.idleLimit() {
			g.closeTalk(p, i18n.T(p.lang, "npc.idle_farewell"))
		}
	}
}

// offers computes the NPC's menu for the player's current state.
func (g *Game) offers(p *player, npc world.NPC) []protocol.NPCOffer {
	kind := npc.Kind
	var offers []protocol.NPCOffer
	switch kind {
	case world.NPCMerchant:
		offers = []protocol.NPCOffer{
			{Action: protocol.NPCBuy, Label: i18n.T(p.lang, "npc.offer.buy"), Price: g.buyPrice(p, npc),
				Available: p.stats.Gold >= g.buyPrice(p, npc) && p.stats.Potions < combat.MaxPotions},
			{Action: protocol.NPCSell, Label: i18n.T(p.lang, "npc.offer.sell"), Price: potionSellPrice, Gain: true,
				Available: p.stats.Potions > 0 && p.stats.Gold <= combat.MaxGold-potionSellPrice},
			{Action: protocol.NPCSteal, Label: i18n.T(p.lang, "npc.offer.steal"), Available: true},
		}
	case world.NPCGuard:
		offers = g.guardOffers(p, npc)
	case world.NPCSmith, world.NPCArmorer:
		offers = g.shopOffers(p, npc)
	case world.NPCHealer:
		price := g.healCost(p, npc)
		offers = []protocol.NPCOffer{
			{Action: protocol.NPCHeal, Label: i18n.T(p.lang, "npc.offer.heal"), Price: price,
				Available: (p.hp < p.stats.MaxHP || p.mana < p.stats.MaxMana) && p.stats.Gold >= price},
		}
	}
	if npc.Index >= world.CaravanIndexBase && npc.Index < world.CaravanIndexBase+2 {
		return offers // a travelling merchant only sells what it carries: no quest, no work, no discussion
	}
	// Quests ready to hand in to this NPC: works without a language model.
	for _, q := range p.quests {
		if q.State == quest.Ready && q.Giver == npc.Key() {
			offers = append(offers, protocol.NPCOffer{Action: protocol.NPCTurnIn, Arg: strconv.FormatInt(q.ID, 10),
				Label: i18n.T(p.lang, "quest.offer.turnin", q.Title), Available: true})
		}
	}
	// Work to hand out: a secret dungeon within reach, offered without a language model.
	if g.hasWork(p, npc) {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCAskWork, Label: i18n.T(p.lang, "npc.offer.work"), Available: true})
	}
	// Free discussion is only offered if a language model is available.
	if g.talkerReady() {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCTalk, Label: i18n.T(p.lang, "npc.offer.talk"), Available: true})
	}
	return offers
}

// sendTalk sends the conversation state to the player.
func (g *Game) sendTalk(p *player, kind world.NPCKind, log []string, over bool) {
	if p.sendNPC == nil {
		return
	}
	d := protocol.NPCDialog{
		Kind: string(kind), Gold: p.stats.Gold, Potions: p.stats.Potions,
		HP: p.hp, MaxHP: p.stats.MaxHP, Log: log, Over: over, Mode: protocol.NPCModeMenu,
	}
	if over {
		p.sendNPC(d)
		return
	}
	t := p.talk
	if t == nil { // should not happen: keep the menu behaviour
		d.Offers = g.offers(p, world.NPC{Kind: kind})
		p.sendNPC(d)
		return
	}
	d.Name, d.Place, d.Mode, d.Thinking = t.npc.NameIn(p.lang), g.placeName(p.lang, t.npc.Region), t.mode, t.pending
	d.Race = t.npc.RaceIn(p.lang)
	switch t.mode {
	case protocol.NPCModeNotice:
		d.Notice = g.noticeText(p.lang)
		d.Offers = []protocol.NPCOffer{
			{Action: protocol.NPCConsent, Label: i18n.T(p.lang, "npc.offer.consent"), Available: true},
			{Action: protocol.NPCDecline, Label: i18n.T(p.lang, "npc.offer.decline"), Available: true},
		}
	case protocol.NPCModeChat:
		d.Lines = append([]protocol.NPCLine(nil), t.lines...)
	case protocol.NPCModeSell:
		d.Offers = g.sellOffers(p)
	case protocol.NPCModeQuest:
		if t.proposal != nil {
			info := g.questInfo(p.lang, *t.proposal)
			info.Giver = t.npc.NameIn(p.lang)
			d.Proposal = &info
		}
		d.Offers = []protocol.NPCOffer{
			{Action: protocol.NPCQuestAccept, Label: i18n.T(p.lang, "quest.offer.accept"), Available: true},
			{Action: protocol.NPCQuestRefuse, Label: i18n.T(p.lang, "quest.offer.refuse"), Available: true},
		}
	default:
		d.Offers = g.offers(p, t.npc)
		if isVendor(t.npc.Kind) {
			w, _ := g.waresFor(p, t.npc)
			d.Specialty = w.Specialty
		}
	}
	p.sendNPC(d)
}

// ambientLine picks, stably for a given NPC, an ambient sentence
// (list "npc.ambient.N" of the catalogue).
func ambientLine(l i18n.Lang, npc world.NPC) string {
	return i18n.Pick(l, "npc.ambient", npc.Region.X*31+npc.Region.Y*17+npc.Index*7)
}

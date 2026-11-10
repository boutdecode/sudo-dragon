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

// Prix des PNJ, en or. Une potion se revend bien moins cher qu'elle ne s'achète.
const (
	potionBuyPrice    = 20
	potionSellPrice   = 8
	healPricePerLevel = 4
)

// healPrice est le tarif du soigneur: gratuit au niveau 1 (on ne bloque pas un
// débutant sans le sou), puis 4 or par niveau.
// buyPrice et healCost appliquent la réduction de réputation du lieu du PNJ. Le menu et la
// transaction passent par ces deux fonctions, pour rester d'accord.
func (g *Game) buyPrice(p *player, npc world.NPC) int { return g.priceFor(p, npc, potionBuyPrice) }

func (g *Game) healCost(p *player, npc world.NPC) int {
	return g.priceFor(p, npc, healPrice(p.stats.Level))
}

// priceFor applique à un tarif la réduction de réputation du lieu du PNJ, puis l'effet du
// charisme du joueur (les PNJ le jugent sur sa mine).
func (g *Game) priceFor(p *player, npc world.NPC, base int) int {
	return hero.AdjustPrice(quest.Discount(base, p.rep[npc.Region]), p.stats.Scores.Mod(hero.CHA))
}

func healPrice(level int) int {
	if level <= 1 {
		return 0
	}
	return healPricePerLevel * level
}

// talk est une conversation en cours entre un joueur et un PNJ. Comme pour un
// combat, le joueur reste immobile tant qu'elle dure.
type talk struct {
	npc     world.NPC
	lastAct uint64 // tick de la dernière action du joueur

	// Discussion libre (voir talk.go).
	gen     uint64             // identifiant de cette conversation: une réponse tardive d'une autre est ignorée
	mode    string             // protocol.NPCMode*
	lines   []protocol.NPCLine // transcription affichée
	history []npcai.Turn       // échanges confiés au modèle
	pending bool               // une réponse du PNJ est attendue
	opened  bool               // le PNJ a déjà ouvert la discussion (une seule fois par conversation)

	// Quête proposée par le modèle et pas encore acceptée (mode quest).
	proposal *quest.Quest
	prevMode string
}

// ValidNPCAction indique si action est une action de dialogue connue.
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

// TalkAct applique l'action d'un joueur en conversation (text n'est lu que pour
// NPCSay). Sans effet hors conversation ou si l'action est inconnue.
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

// startTalk ouvre une conversation si le joueur est libre.
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
			// Sans modèle de langage, le chef renseigne quand même : ses plus proches voisins.
			if line := g.neighboursGreeting(npc, p.lang); line != "" {
				greeting = line
			} else {
				greeting = ambientLine(p.lang, npc)
			}
		}
	case world.NPCVillager:
		// Sans métier, un villageois n'a que sa conversation. Quand elle est indisponible
		// (pas de clé, panne), il dit une phrase d'ambiance fixe pour que le monde reste vivant.
		greeting = i18n.T(p.lang, "npc.greet.villager")
		if !g.talkerReady() {
			greeting = ambientLine(p.lang, npc)
		}
	}
	g.sendTalk(p, npc.Kind, []string{greeting}, false)
}

// closeTalk met fin à la conversation et prévient le client.
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
		return // le commerce et les soins ne se font que depuis le menu
	}

	var log []string
	changed := false
	buyPrice, repairPrice := g.buyPrice(p, t.npc), g.healCost(p, t.npc)
	switch action {

	case protocol.NPCTurnIn:
		log = g.questAct(p, action, text) // la remise enregistre elle-même

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
			log = g.buyItem(p, t.npc, text) // l'achat enregistre lui-même
		}

	case protocol.NPCSellMenu:
		if !isVendor(t.npc.Kind) || len(g.sellable(p)) == 0 {
			log = notOffered(p.lang)
		} else {
			t.mode = protocol.NPCModeSell
		}

	case protocol.NPCSellItem:
		log = notOffered(p.lang) // la revente se fait depuis la liste de revente

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
			log = []string{i18n.T(p.lang, "npc.purse_full")} // on ne perd pas une potion contre rien
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
			log = []string{i18n.T(p.lang, "npc.already_healthy")} // rien n'est débité
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
		g.save(p) // une transaction change la progression: on n'attend pas la sauvegarde périodique
	}
	g.sendTalk(p, t.npc.Kind, log, false)
}

func notOffered(l i18n.Lang) []string { return []string{i18n.T(l, "npc.not_offered")} }

// expireIdleTalks ferme les conversations abandonnées.
func (g *Game) expireIdleTalks(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.talk != nil && g.tick-p.talk.lastAct > p.talk.idleLimit() {
			g.closeTalk(p, i18n.T(p.lang, "npc.idle_farewell"))
		}
	}
}

// offers calcule le menu du PNJ pour l'état actuel du joueur.
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
	// Quêtes prêtes à rendre à ce PNJ : fonctionne sans modèle de langage.
	for _, q := range p.quests {
		if q.State == quest.Ready && q.Giver == npc.Key() {
			offers = append(offers, protocol.NPCOffer{Action: protocol.NPCTurnIn, Arg: strconv.FormatInt(q.ID, 10),
				Label: i18n.T(p.lang, "quest.offer.turnin", q.Title), Available: true})
		}
	}
	// Du travail à confier: un donjon secret à portée, proposé sans modèle de langage.
	if g.hasWork(p, npc) {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCAskWork, Label: i18n.T(p.lang, "npc.offer.work"), Available: true})
	}
	// La discussion libre n'est proposée que si un modèle de langage est disponible.
	if g.talkerReady() {
		offers = append(offers, protocol.NPCOffer{Action: protocol.NPCTalk, Label: i18n.T(p.lang, "npc.offer.talk"), Available: true})
	}
	return offers
}

// sendTalk envoie l'état de la conversation au joueur.
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
	if t == nil { // ne devrait pas arriver: on garde le comportement du menu
		d.Offers = g.offers(p, world.NPC{Kind: kind})
		p.sendNPC(d)
		return
	}
	d.Name, d.Place, d.Mode, d.Thinking = t.npc.NameIn(p.lang), g.placeName(p.lang, t.npc.Region), t.mode, t.pending
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

// ambientLine choisit, de façon stable pour un PNJ donné, une phrase d'ambiance
// (liste "npc.ambient.N" du catalogue).
func ambientLine(l i18n.Lang, npc world.NPC) string {
	return i18n.Pick(l, "npc.ambient", npc.Region.X*31+npc.Region.Y*17+npc.Index*7)
}

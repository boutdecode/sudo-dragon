package game

import (
	"fmt"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
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
}

// ValidNPCAction indique si action est une action de dialogue connue.
func ValidNPCAction(action string) bool {
	switch action {
	case protocol.NPCBuy, protocol.NPCSell, protocol.NPCHeal, protocol.NPCLeave:
		return true
	}
	return false
}

// TalkAct applique l'action d'un joueur en conversation. Sans effet hors
// conversation ou si l'action est inconnue.
func (g *Game) TalkAct(id int, action string) {
	if !ValidNPCAction(action) {
		return
	}
	g.do(func() {
		if p, ok := g.players[id]; ok {
			g.talkAct(p, action)
		}
	})
}

// startTalk ouvre une conversation si le joueur est libre.
func (g *Game) startTalk(p *player, npc world.NPC) {
	if p.fight != nil || p.talk != nil {
		return
	}
	p.talk = &talk{npc: npc, lastAct: g.tick}
	p.hasPending = false
	greeting := "Bienvenue, voyageur ! Que puis-je pour vous ?"
	if npc.Kind == world.NPCHealer {
		greeting = "Que la lumière vous protège. Vous avez besoin de soins ?"
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

func (g *Game) talkAct(p *player, action string) {
	t := p.talk
	if t == nil {
		return
	}
	t.lastAct = g.tick

	var log []string
	changed := false
	switch action {
	case protocol.NPCLeave:
		g.closeTalk(p, "À bientôt !")
		return

	case protocol.NPCBuy:
		switch {
		case t.npc.Kind != world.NPCMerchant:
			log = notOffered()
		case p.stats.Potions >= combat.MaxPotions:
			log = []string{"Votre sac est plein."}
		case !p.stats.SpendGold(potionBuyPrice):
			log = []string{fmt.Sprintf("Il vous manque %d or.", potionBuyPrice-p.stats.Gold)}
		default:
			p.stats.AddPotions(1)
			changed = true
			log = []string{fmt.Sprintf("Vous achetez une potion pour %d or.", potionBuyPrice)}
		}

	case protocol.NPCSell:
		switch {
		case t.npc.Kind != world.NPCMerchant:
			log = notOffered()
		case p.stats.Potions <= 0:
			log = []string{"Vous n'avez aucune potion à vendre."}
		case p.stats.Gold > combat.MaxGold-potionSellPrice:
			log = []string{"Votre bourse est pleine."} // on ne perd pas une potion contre rien
		default:
			p.stats.Potions--
			p.stats.AddGold(potionSellPrice)
			changed = true
			log = []string{fmt.Sprintf("Vous vendez une potion pour %d or.", potionSellPrice)}
		}

	case protocol.NPCHeal:
		price := healPrice(p.stats.Level)
		switch {
		case t.npc.Kind != world.NPCHealer:
			log = notOffered()
		case p.hp >= p.stats.MaxHP:
			log = []string{"Vous êtes déjà en pleine forme."} // rien n'est débité
		case !p.stats.SpendGold(price):
			log = []string{fmt.Sprintf("Il vous manque %d or.", price-p.stats.Gold)}
		default:
			p.hp = p.stats.MaxHP
			changed = true
			if price == 0 {
				log = []string{"Vous voilà guéri. C'est offert !"}
			} else {
				log = []string{fmt.Sprintf("Vous voilà guéri, pour %d or.", price)}
			}
		}
	}

	if changed {
		g.save(p) // une transaction change la progression: on n'attend pas la sauvegarde périodique
	}
	g.sendTalk(p, t.npc.Kind, log, false)
}

func notOffered() []string { return []string{"Ce n'est pas ce que je propose."} }

// expireIdleTalks ferme les conversations abandonnées.
func (g *Game) expireIdleTalks(ids []int) {
	for _, id := range ids {
		p := g.players[id]
		if p.talk != nil && g.tick-p.talk.lastAct > fightIdleTicks {
			g.closeTalk(p, "Vous semblez ailleurs… à une prochaine fois.")
		}
	}
}

// offers calcule le menu du PNJ pour l'état actuel du joueur.
func (g *Game) offers(p *player, kind world.NPCKind) []protocol.NPCOffer {
	switch kind {
	case world.NPCMerchant:
		return []protocol.NPCOffer{
			{Action: protocol.NPCBuy, Label: "Acheter une potion", Price: potionBuyPrice,
				Available: p.stats.Gold >= potionBuyPrice && p.stats.Potions < combat.MaxPotions},
			{Action: protocol.NPCSell, Label: "Vendre une potion", Price: potionSellPrice, Gain: true,
				Available: p.stats.Potions > 0 && p.stats.Gold <= combat.MaxGold-potionSellPrice},
		}
	case world.NPCHealer:
		price := healPrice(p.stats.Level)
		return []protocol.NPCOffer{
			{Action: protocol.NPCHeal, Label: "Être soigné", Price: price,
				Available: p.hp < p.stats.MaxHP && p.stats.Gold >= price},
		}
	}
	return nil
}

// sendTalk envoie l'état de la conversation au joueur.
func (g *Game) sendTalk(p *player, kind world.NPCKind, log []string, over bool) {
	if p.sendNPC == nil {
		return
	}
	d := protocol.NPCDialog{
		Kind: string(kind), Gold: p.stats.Gold, Potions: p.stats.Potions,
		HP: p.hp, MaxHP: p.stats.MaxHP, Log: log, Over: over,
	}
	if !over {
		d.Offers = g.offers(p, kind)
	}
	p.sendNPC(d)
}

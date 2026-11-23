package game

import (
	"strconv"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Gathering tools (see resource/tool.go): a tool is mandatory to gather wood, ore and herbs. It is bought from the
// vendor of a trade cabin (see world/cabin.go), who only sells the tools of their own trade. The purchase is
// revalidated here whatever the client sends: tier, trade, job level, gold, and a tier strictly above the one owned.

// toolCategory is the gathering category whose tools a vendor of kind k sells.
func toolCategory(k world.NPCKind) (resource.Category, bool) {
	switch k {
	case world.NPCLumberjack:
		return resource.Wood, true
	case world.NPCMiner:
		return resource.Ore, true
	case world.NPCHerbalist:
		return resource.Herb, true
	}
	return 0, false
}

// isToolVendor tells whether k is a trade vendor.
func isToolVendor(k world.NPCKind) bool {
	_, ok := toolCategory(k)
	return ok
}

// toolName is the name of the tool of category c and tier t, in the player's language.
func toolName(p *player, c resource.Category, tier int) string {
	return i18n.T(p.lang, "tool."+c.Key()+"."+strconv.Itoa(tier))
}

// toolPrice is the price of a tool for this player at this vendor (charisma and reputation apply, as for any
// purchase).
func (g *Game) toolPrice(p *player, npc world.NPC, t resource.Tool) int {
	return g.priceFor(p, npc, t.Price)
}

// toolOffers lists the tiers the player does not own yet, from the lowest. A tier whose job level is not reached is
// shown but unavailable.
func (g *Game) toolOffers(p *player, npc world.NPC) []protocol.NPCOffer {
	cat, ok := toolCategory(npc.Kind)
	if !ok {
		return nil
	}
	level := resource.LevelOf(p.skills[cat])
	var offers []protocol.NPCOffer
	for _, t := range resource.Tools() {
		if t.Tier <= p.tools[cat] {
			continue
		}
		price := g.toolPrice(p, npc, t)
		offers = append(offers, protocol.NPCOffer{
			Action: protocol.NPCBuyTool, Label: toolName(p, cat, t.Tier), Price: price,
			Detail:    i18n.T(p.lang, "tool.detail", t.MinLevel, resource.Yield(t.Tier)),
			Arg:       strconv.Itoa(t.Tier),
			Available: p.stats.Gold >= price && level >= t.MinLevel,
		})
	}
	return offers
}

// buyTool buys the tool tier designated by arg.
func (g *Game) buyTool(p *player, npc world.NPC, arg string) []string {
	cat, ok := toolCategory(npc.Kind)
	tier, err := strconv.Atoi(arg)
	if !ok || err != nil {
		return notOffered(p.lang)
	}
	t, ok := resource.ToolTier(tier)
	if !ok {
		return notOffered(p.lang)
	}
	if tier <= p.tools[cat] {
		return []string{i18n.T(p.lang, "tool.owned")}
	}
	if level := resource.LevelOf(p.skills[cat]); level < t.MinLevel {
		return []string{i18n.T(p.lang, "tool.level_low", jobName(p, cat), t.MinLevel)}
	}
	price := g.toolPrice(p, npc, t)
	if !p.stats.SpendGold(price) {
		return []string{i18n.T(p.lang, "npc.missing_gold", price-p.stats.Gold)}
	}
	if p.tools == nil {
		p.tools = map[resource.Category]int{}
	}
	p.tools[cat] = tier
	g.gearChanged(p) // updates the bag (jobs and yield)
	g.save(p)
	return []string{i18n.T(p.lang, "tool.bought", toolName(p, cat, tier), price)}
}

// loadTools reloads the saved tools, discarding unknown categories and tiers outside the catalogue.
func loadTools(saved map[string]int) map[resource.Category]int {
	out := map[resource.Category]int{}
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		if t := saved[c.Key()]; resource.NeedsTool(c) && t >= 1 {
			out[c] = min(t, resource.NumToolTiers)
		}
	}
	return out
}

// saveTools is the inverse of loadTools.
func saveTools(m map[resource.Category]int) map[string]int {
	var out map[string]int
	for c, t := range m {
		if t > 0 {
			if out == nil {
				out = map[string]int{}
			}
			out[c.Key()] = t
		}
	}
	return out
}

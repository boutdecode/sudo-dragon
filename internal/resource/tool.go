package resource

// Gathering tools: an axe, a pickaxe and a sickle. A tool is mandatory to gather wood, ore and herbs (fishing needs
// none). There are four tiers, bought from the trade vendors (see world/cabin.go): the first one gives no bonus, each
// higher one needs a higher job level and yields more units per gather.

// NumToolTiers is the number of tiers of a tool (tier 0 means "no tool").
const NumToolTiers = 4

// Tool is one tier of the tool catalogue (the same one for every category).
type Tool struct {
	Tier     int // 1 to NumToolTiers
	MinLevel int // job level required to buy it
	Price    int // base price, in gold
	Bonus    int // extra units per gather
}

// tools is the catalogue, by increasing tier. Balancing settings: the first tier is cheap (a new character can
// afford it from its first fights) and gives nothing; the last one is reserved for a master of the job.
var tools = [NumToolTiers]Tool{
	{Tier: 1, MinLevel: 1, Price: 30, Bonus: 0},
	{Tier: 2, MinLevel: 4, Price: 120, Bonus: 1},
	{Tier: 3, MinLevel: 7, Price: 400, Bonus: 2},
	{Tier: 4, MinLevel: MaxJobLevel, Price: 1200, Bonus: 3},
}

// Tools returns the tool catalogue (copy).
func Tools() []Tool { return append([]Tool(nil), tools[:]...) }

// ToolTier returns the catalogue entry of a tier.
func ToolTier(tier int) (Tool, bool) {
	if tier < 1 || tier > NumToolTiers {
		return Tool{}, false
	}
	return tools[tier-1], true
}

// NeedsTool tells whether gathering a category requires a tool.
func NeedsTool(c Category) bool { return c == Wood || c == Ore || c == Herb }

// Yield is the number of units gathered with a tool of the given tier: 1 plus its bonus. Without a tool (tier 0 or
// unknown), nothing can be gathered.
func Yield(tier int) int {
	t, ok := ToolTier(tier)
	if !ok {
		return 0
	}
	return 1 + t.Bonus
}

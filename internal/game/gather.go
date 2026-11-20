package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Gathering raw materials (see internal/resource and world/resources.go). A node (tree, vein, tuft) is
// gathered with the action key, from its tile or an adjacent tile. It is then depleted for everyone
// for nodeRespawn ticks, then grows back.
const (
	nodeRespawn    = 6000 // 10 min (10 ticks per second)
	gatherCooldown = 8    // minimum delay between two gatherings by a player
	maxResourceQty = 99   // cap of a stack reloaded from a save (the bag is bounded by the job)
	nodeNearby     = 160  // radius (in tiles) of depleted nodes sent to the client
	nodeResync     = 40   // distance travelled that forces a new send
	maxNodesSent   = 512
)

// gatherReach lists the tiles where a node can be gathered: under the player, then their four neighbours.
var gatherReach = [...][2]int{{0, 0}, {0, -1}, {0, 1}, {-1, 0}, {1, 0}}

// Gather makes the player gather the resource node within reach.
func (g *Game) Gather(id int) { g.do(func() { g.gather(id) }) }

func (g *Game) gather(id int) {
	p, ok := g.players[id]
	if !ok || p.zone != 0 || p.fight != nil || p.talk != nil {
		return
	}
	if g.tick < p.gatherUntil {
		return
	}
	p.gatherUntil = g.tick + gatherCooldown
	sawSpent := false
	for _, d := range gatherReach {
		pos := world.Pos{X: p.pos.X + d[0], Y: p.pos.Y + d[1]}
		kind, ok := g.World.ResourceAt(pos.X, pos.Y)
		if !ok {
			continue
		}
		if _, spent := g.spent[pos]; spent {
			sawSpent = true
			continue
		}
		info, _ := resource.Lookup(kind)
		level := resource.LevelOf(p.skills[info.Cat])
		if resource.NeedsTool(info.Cat) && p.tools[info.Cat] == 0 { // no tool: the node is untouched
			g.poiNotice(p, "gather.no_tool", toolName(p, info.Cat, 1))
			return
		}
		used, capacity := usedIn(p.resources, info.Cat), resource.Capacity(level)
		if used >= capacity { // category bag full: the node is untouched
			g.poiNotice(p, "gather.full", jobName(p, info.Cat))
			return
		}
		g.spent[pos] = g.tick // successful or not, the attempt depletes the node
		g.spentVer++
		if g.rng.IntN(100) >= resource.Chance(level) {
			g.poiNotice(p, "gather.fail", resourceName(p, kind))
			return
		}
		if p.resources == nil {
			p.resources = map[resource.Kind]int{}
		}
		n := 1 // fishing needs no tool and gives a single unit
		if resource.NeedsTool(info.Cat) {
			n = min(resource.Yield(p.tools[info.Cat]), capacity-used) // the bag has room for at least one
		}
		p.resources[kind] += n
		if p.skills == nil {
			p.skills = map[resource.Category]int{}
		}
		p.skills[info.Cat] += resource.GainXP(info.Tier)
		now := resource.LevelOf(p.skills[info.Cat])
		key := "gather.got"
		args := []any{resourceName(p, kind), usedIn(p.resources, info.Cat), resource.Capacity(now)}
		if n > 1 {
			key, args = "gather.got_n", []any{resourceName(p, kind), n, args[1], args[2]}
		}
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, key, args...))
		if now > level {
			p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "gather.levelup", jobName(p, info.Cat), now))
		}
		g.sendBag(p)
		return
	}
	if sawSpent {
		g.poiNotice(p, "gather.spent")
		return
	}
	g.poiNotice(p, "gather.none")
}

// expireNodes restores the nodes whose delay has elapsed.
func (g *Game) expireNodes() {
	if g.tick%50 != 0 || len(g.spent) == 0 {
		return
	}
	for pos, at := range g.spent {
		if g.tick-at >= nodeRespawn {
			delete(g.spent, pos)
			g.spentVer++
		}
	}
}

// syncNodes sends the client the list of depleted nodes around them when it has changed, or when they have
// moved away from the point where it was computed.
func (g *Game) syncNodes(p *player) {
	if p.sendNodesMsg == nil || p.zone != 0 {
		return
	}
	moved := max(abs(p.pos.X-p.nodesAt.X), abs(p.pos.Y-p.nodesAt.Y)) >= nodeResync
	if p.nodesVer == g.spentVer && !moved {
		return
	}
	// An empty list already announced need not be repeated.
	empty := len(g.spent) == 0 && p.nodesVer == g.spentVer
	p.nodesVer, p.nodesAt = g.spentVer, p.pos
	if empty {
		return
	}
	var n protocol.Nodes
	for pos := range g.spent {
		if max(abs(pos.X-p.pos.X), abs(pos.Y-p.pos.Y)) <= nodeNearby && len(n.Spent) < maxNodesSent {
			n.Spent = append(n.Spent, [2]int{pos.X, pos.Y})
		}
	}
	slices.SortFunc(n.Spent, func(a, b [2]int) int {
		if a[1] != b[1] {
			return a[1] - b[1]
		}
		return a[0] - b[0]
	})
	p.sendNodesMsg(n)
}

// resourceName is the name of a resource in the player's language.
func resourceName(p *player, k resource.Kind) string { return i18n.T(p.lang, "resource."+string(k)) }

// resourceStacks formats the materials bag for the client: by category, then by identifier.
func resourceStacks(m map[resource.Kind]int) []protocol.ResourceStack {
	var out []protocol.ResourceStack
	for _, info := range resource.All() {
		if n := m[info.Kind]; n > 0 {
			out = append(out, protocol.ResourceStack{ID: string(info.Kind), Qty: n})
		}
	}
	return out
}

// loadResources reloads the saved bag, discarding unknown resources and absurd quantities.
func loadResources(saved map[string]int) map[resource.Kind]int {
	out := map[resource.Kind]int{}
	for id, n := range saved {
		if _, ok := resource.Lookup(resource.Kind(id)); ok && n > 0 {
			out[resource.Kind(id)] = min(n, maxResourceQty)
		}
	}
	return out
}

// saveResources is the inverse of loadResources.
func saveResources(m map[resource.Kind]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, n := range m {
		out[string(k)] = n
	}
	return out
}

// usedIn is the number of units carried for a category, all variants combined.
func usedIn(m map[resource.Kind]int, cat resource.Category) int {
	n := 0
	for k, q := range m {
		if info, ok := resource.Lookup(k); ok && info.Cat == cat {
			n += q
		}
	}
	return n
}

// jobName is the name of the job associated with a category, in the player's language.
func jobName(p *player, c resource.Category) string { return i18n.T(p.lang, "job."+c.Key()) }

// jobInfos describes the player's jobs for the client, in category order.
func jobInfos(p *player) []protocol.JobInfo {
	out := make([]protocol.JobInfo, 0, resource.NumCategories)
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		level, in, next := resource.Progress(p.skills[c])
		out = append(out, protocol.JobInfo{Job: c.Key(), Level: level, XP: in, Next: next,
			Used: usedIn(p.resources, c), Cap: resource.Capacity(level), Chance: resource.Chance(level),
			Tool: p.tools[c], Yield: jobYield(p, c)})
	}
	return out
}

// loadSkills reloads the saved experience, discarding unknown categories.
func loadSkills(saved map[string]int) map[resource.Category]int {
	out := map[resource.Category]int{}
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		if xp := saved[c.Key()]; xp > 0 {
			out[c] = xp
		}
	}
	return out
}

// saveSkills is the inverse of loadSkills.
func saveSkills(m map[resource.Category]int) map[string]int {
	var out map[string]int
	for c, xp := range m {
		if xp > 0 {
			if out == nil {
				out = map[string]int{}
			}
			out[c.Key()] = xp
		}
	}
	return out
}

// jobYield is the number of units a successful gather of category c gives the player.
func jobYield(p *player, c resource.Category) int {
	if !resource.NeedsTool(c) {
		return 1
	}
	return resource.Yield(p.tools[c])
}

package game

import (
	"slices"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/resource"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Récolte de matières premières (voir internal/resource et world/resources.go). Un nœud (arbre, filon, touffe) se
// récolte avec la touche d'action, depuis sa case ou une case voisine. Il est ensuite épuisé pour tout le monde
// pendant nodeRespawn ticks, puis repousse.
const (
	nodeRespawn    = 6000 // 10 min (10 ticks par seconde)
	gatherCooldown = 8    // délai minimal entre deux récoltes d'un joueur
	maxResourceQty = 99   // plafond d'une pile relue d'une sauvegarde (le sac est borné par le métier)
	nodeNearby     = 160  // rayon (en cases) des nœuds épuisés envoyés au client
	nodeResync     = 40   // distance parcourue qui force un nouvel envoi
	maxNodesSent   = 512
)

// gatherReach liste les cases où un nœud peut être récolté: sous le joueur, puis ses quatre voisines.
var gatherReach = [...][2]int{{0, 0}, {0, -1}, {0, 1}, {-1, 0}, {1, 0}}

// Gather fait récolter au joueur le nœud de ressource à sa portée.
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
		if usedIn(p.resources, info.Cat) >= resource.Capacity(level) { // sac de la catégorie plein: le nœud n'est pas touché
			g.poiNotice(p, "gather.full", jobName(p, info.Cat))
			return
		}
		g.spent[pos] = g.tick // réussie ou non, la tentative épuise le nœud
		g.spentVer++
		if g.rng.IntN(100) >= resource.Chance(level) {
			g.poiNotice(p, "gather.fail", resourceName(p, kind))
			return
		}
		if p.resources == nil {
			p.resources = map[resource.Kind]int{}
		}
		p.resources[kind]++
		if p.skills == nil {
			p.skills = map[resource.Category]int{}
		}
		p.skills[info.Cat] += resource.GainXP(info.Tier)
		now := resource.LevelOf(p.skills[info.Cat])
		p.gearNotices = append(p.gearNotices, i18n.T(p.lang, "gather.got", resourceName(p, kind),
			usedIn(p.resources, info.Cat), resource.Capacity(now)))
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

// expireNodes remet en place les nœuds dont le délai est écoulé.
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

// syncNodes envoie au client la liste des nœuds épuisés autour de lui quand elle a changé, ou quand il s'est
// éloigné du point où elle a été calculée.
func (g *Game) syncNodes(p *player) {
	if p.sendNodesMsg == nil || p.zone != 0 {
		return
	}
	moved := max(abs(p.pos.X-p.nodesAt.X), abs(p.pos.Y-p.nodesAt.Y)) >= nodeResync
	if p.nodesVer == g.spentVer && !moved {
		return
	}
	// Une liste vide déjà annoncée n'a pas besoin d'être répétée.
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

// resourceName est le nom d'une ressource dans la langue du joueur.
func resourceName(p *player, k resource.Kind) string { return i18n.T(p.lang, "resource."+string(k)) }

// resourceStacks met le sac de matières en forme pour le client: par catégorie, puis par identifiant.
func resourceStacks(m map[resource.Kind]int) []protocol.ResourceStack {
	var out []protocol.ResourceStack
	for _, info := range resource.All() {
		if n := m[info.Kind]; n > 0 {
			out = append(out, protocol.ResourceStack{ID: string(info.Kind), Qty: n})
		}
	}
	return out
}

// loadResources relit le sac sauvegardé en écartant les ressources inconnues et les quantités absurdes.
func loadResources(saved map[string]int) map[resource.Kind]int {
	out := map[resource.Kind]int{}
	for id, n := range saved {
		if _, ok := resource.Lookup(resource.Kind(id)); ok && n > 0 {
			out[resource.Kind(id)] = min(n, maxResourceQty)
		}
	}
	return out
}

// saveResources est l'inverse de loadResources.
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

// usedIn est le nombre d'unités portées pour une catégorie, toutes déclinaisons confondues.
func usedIn(m map[resource.Kind]int, cat resource.Category) int {
	n := 0
	for k, q := range m {
		if info, ok := resource.Lookup(k); ok && info.Cat == cat {
			n += q
		}
	}
	return n
}

// jobName est le nom du métier associé à une catégorie, dans la langue du joueur.
func jobName(p *player, c resource.Category) string { return i18n.T(p.lang, "job."+c.Key()) }

// jobInfos décrit les métiers du joueur pour le client, dans l'ordre des catégories.
func jobInfos(p *player) []protocol.JobInfo {
	out := make([]protocol.JobInfo, 0, resource.NumCategories)
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		level, in, next := resource.Progress(p.skills[c])
		out = append(out, protocol.JobInfo{Job: c.Key(), Level: level, XP: in, Next: next,
			Used: usedIn(p.resources, c), Cap: resource.Capacity(level), Chance: resource.Chance(level)})
	}
	return out
}

// loadSkills relit l'expérience sauvegardée en écartant les catégories inconnues.
func loadSkills(saved map[string]int) map[resource.Category]int {
	out := map[resource.Category]int{}
	for c := resource.Category(0); c < resource.NumCategories; c++ {
		if xp := saved[c.Key()]; xp > 0 {
			out[c] = xp
		}
	}
	return out
}

// saveSkills est l'inverse de loadSkills.
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

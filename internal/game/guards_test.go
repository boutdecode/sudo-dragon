package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

type guardRig struct {
	g  *Game
	s  *world.Structure
	p  *player
	gd *guard
	nd []protocol.NPCDialog
}

// newGuardRig place un joueur au centre d'un village ou d'une ville proche du départ, et fait apparaître ses gardes.
func newGuardRig(t *testing.T, kind world.Kind) *guardRig {
	t.Helper()
	g := newGame()
	var s *world.Structure
	for _, c := range g.World.StructuresNear(g.World.FindSpawn(), 1500) {
		if c.Kind == kind {
			s = c
			break
		}
	}
	if s == nil {
		t.Skipf("pas de %v près du départ", kind)
	}
	p, _ := joinRec(g, "hero")
	r := &guardRig{g: g, s: s, p: p}
	p.sendNPC = func(d protocol.NPCDialog) { r.nd = append(r.nd, d) }
	place(g, p, g.freeNear(world.Pos{X: s.Guards[0].Pos.X + 4, Y: s.Guards[0].Pos.Y + 1}))
	g.activateGuards()
	r.gd = g.guards().byKey[s.Guards[0].Key()]
	if r.gd == nil {
		t.Fatal("le garde n'est pas apparu")
	}
	return r
}

func (r *guardRig) lastDialog() protocol.NPCDialog { return r.nd[len(r.nd)-1] }

// stepGuards fait agir les gardes n ticks, sans le reste du monde.
func (r *guardRig) stepGuards(n int) {
	for range n {
		r.g.tick++
		r.g.stepGuards()
	}
}

func TestGuardsAppearNearPlayerAndLeaveWithIt(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	if got := len(r.g.guards().byKey); got < len(r.s.Guards) {
		t.Fatalf("%d gardes, attendu au moins %d", got, len(r.s.Guards))
	}
	if r.g.guardAt(r.gd.npc.Pos) != r.gd || !r.g.blocked(r.gd.npc.Pos) {
		t.Fatal("un garde occupe sa case")
	}
	place(r.g, r.p, world.Pos{X: r.p.pos.X + 3000, Y: r.p.pos.Y})
	r.g.activateGuards()
	if len(r.g.guards().byKey) != 0 || len(r.g.guards().at) != 0 {
		t.Fatalf("des gardes restent loin de tout joueur: %d", len(r.g.guards().byKey))
	}
}

func TestGuardsAreSeenAsMonstersOfKindGuard(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	snap := r.g.snapshotFor(r.p, nil)
	found := false
	for _, m := range snap.Monsters {
		if m.Kind == "guard" && m.ID >= guardIDBase && m.X == r.gd.npc.Pos.X && m.Y == r.gd.npc.Pos.Y {
			found = true
		}
	}
	if !found {
		t.Fatalf("garde absent de l'instantané: %+v", snap.Monsters)
	}
	if len(r.g.monsters) != 0 {
		t.Fatal("un garde ne doit pas être enregistré parmi les monstres")
	}
}

func TestGuardsPatrolOnRoads(t *testing.T) {
	r := newGuardRig(t, world.KindTown)
	start := r.gd.npc.Pos
	moved := false
	for range 600 {
		r.stepGuards(1)
		pos := r.gd.npc.Pos
		if pos != start {
			moved = true
		}
		if !r.gd.nearLeash(pos) {
			t.Fatalf("le garde sort de son lieu: %v", pos)
		}
		if r.gd.avatar.pos != pos || r.g.guardAt(pos) != r.gd {
			t.Fatal("position du garde, de son avatar et de l'index incohérentes")
		}
		if r.p.fight != nil {
			t.Fatal("un garde n'attaque pas un joueur en règle")
		}
	}
	if !moved {
		t.Fatal("le garde ne patrouille pas")
	}
}

func TestWalkingIntoAGuardTalksAndAnswersDirections(t *testing.T) {
	r := newGuardRig(t, world.KindTown)
	r.g.bumpGuard(r.p, r.gd)
	if r.p.talk == nil || r.p.talk.npc.Kind != world.NPCGuard {
		t.Fatal("la conversation avec le garde n'a pas commencé")
	}
	d := r.lastDialog()
	if d.Kind != "guard" || len(d.Offers) < 5 {
		t.Fatalf("menu du garde: %+v", d)
	}
	for _, o := range d.Offers {
		if o.Action != protocol.NPCWhere && o.Action != protocol.NPCPlaces {
			t.Errorf("offre inattendue: %+v", o)
		}
	}
	// Le forgeron de la ville est localisé avec une direction et une distance.
	r.g.talkAct(r.p, protocol.NPCWhere, string(world.NPCSmith))
	log := strings.Join(r.lastDialog().Log, " ")
	if !strings.Contains(log, "pas") || !strings.Contains(log, " au ") && !strings.Contains(log, " à l'") {
		t.Fatalf("réponse sans direction: %q", log)
	}
	// Une action inconnue ou un métier hors liste est refusé.
	r.g.talkAct(r.p, protocol.NPCWhere, "dragon")
	if got := strings.Join(r.lastDialog().Log, " "); got == "" {
		t.Fatal("pas de réponse")
	}
	r.g.talkAct(r.p, protocol.NPCPlaces, "")
	if len(r.lastDialog().Log) == 0 {
		t.Fatal("pas de lieux voisins")
	}
}

func TestGuardPointsToAnotherPlaceWhenTradeIsMissing(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	r.g.bumpGuard(r.p, r.gd)
	// Un village n'a pas de marchand : le garde renvoie vers un lieu voisin, ou avoue ne pas en connaître.
	r.g.talkAct(r.p, protocol.NPCWhere, string(world.NPCMerchant))
	log := strings.Join(r.lastDialog().Log, " ")
	if !strings.Contains(strings.ToLower(log), "marchand") || !strings.HasPrefix(log, "Pas de marchand ici") {
		t.Fatalf("réponse: %q", log)
	}
}

func TestOutlawIsHuntedByGuards(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	r.p.rep[r.s.Region] = quest.Wanted
	place(r.g, r.p, r.g.freeNear(world.Pos{X: r.gd.npc.Pos.X + 5, Y: r.gd.npc.Pos.Y}))
	for range 400 {
		r.stepGuards(1)
		if r.p.fight != nil {
			break
		}
	}
	if r.p.fight == nil || r.p.fight.ms[0].guard == nil {
		t.Fatal("le garde n'a pas attaqué le hors-la-loi")
	}
	if r.p.fight.ms[0].guard.avatar.fight == nil { // c'est peut-être l'autre garde du village qui a attaqué
		t.Fatal("le garde doit être figé pendant le combat")
	}
	// Au premier tour, le journal annonce le garde.
	if r.p.fight.b.Foes[0].Kind != "guard" {
		t.Fatalf("adversaire: %+v", r.p.fight.b.Foes[0])
	}
}

func TestGuardsLeaveGoodPeopleAlone(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	for _, rep := range []int{0, quest.Suspect, quest.Wanted + 1, 12} {
		r.p.rep[r.s.Region] = rep
		place(r.g, r.p, r.g.freeNear(world.Pos{X: r.gd.npc.Pos.X + 2, Y: r.gd.npc.Pos.Y}))
		r.stepGuards(300)
		if r.p.fight != nil {
			t.Fatalf("attaqué avec %d points de réputation", rep)
		}
	}
}

func TestOutlawInAnotherPlaceIsNotAttacked(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	other := world.RegionPos{X: r.s.Region.X + 40, Y: r.s.Region.Y}
	r.p.rep[other] = quest.Wanted // hors-la-loi ailleurs seulement
	place(r.g, r.p, r.g.freeNear(world.Pos{X: r.gd.npc.Pos.X + 2, Y: r.gd.npc.Pos.Y}))
	r.stepGuards(300)
	if r.p.fight != nil {
		t.Fatal("la réputation est propre à chaque lieu")
	}
}

func TestSlayingAGuardCostsReputationAndHeWillBeReplaced(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	r.p.rep[r.s.Region] = quest.Wanted
	r.gd.avatar.sp.HP = 1
	r.p.stats.Level = 20
	r.g.bumpGuard(r.p, r.gd) // hors-la-loi : le combat remplace la conversation
	if r.p.fight == nil {
		t.Fatal("le combat n'a pas commencé")
	}
	key := r.gd.npc.Key()
	for i := 0; i < 100 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Attack, 0)
	}
	if r.p.fight != nil {
		t.Fatal("combat sans fin")
	}
	if r.p.hp <= 0 {
		t.Skip("joueur vaincu par le tirage")
	}
	if _, alive := r.g.guards().byKey[key]; alive || r.g.guardAt(r.gd.npc.Pos) != nil {
		t.Fatal("le garde vaincu est toujours là")
	}
	if got := r.p.rep[r.s.Region]; got != quest.Wanted+guardKillRep {
		t.Fatalf("réputation %d, attendu %d", got, quest.Wanted+guardKillRep)
	}
	// Il ne revient pas tout de suite, mais plus tard.
	r.g.activateGuards()
	if _, back := r.g.guards().byKey[key]; back {
		t.Fatal("le garde est revenu aussitôt")
	}
	r.g.tick += townGuardRespawn
	r.g.activateGuards()
	if _, back := r.g.guards().byKey[key]; !back {
		t.Fatal("le garde n'est pas remplacé")
	}
}

func TestReputationIsBoundedBelow(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	rp := world.RegionPos{X: 4, Y: 4}
	g.addRep(p, rp, -1000)
	if p.rep[rp] != quest.MinReputation {
		t.Fatalf("réputation %d", p.rep[rp])
	}
	g.addRep(p, rp, -quest.MinReputation)
	if _, kept := p.rep[rp]; kept {
		t.Fatal("une réputation revenue à zéro ne doit pas rester dans la table")
	}
}

func TestAbandoningAQuestCostsReputation(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	q := r.p.quests[0]
	r.g.abandonQuest(r.p, q.ID)
	if got := r.p.rep[q.Place()]; got != -abandonRepLoss {
		t.Fatalf("réputation %d après abandon", got)
	}
}

func TestStealingRisksReputationAndHasACooldown(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()
	o, ok := r.offer(protocol.NPCSteal)
	if !ok || !o.Available {
		t.Fatal("pas d'offre de vol chez le marchand")
	}
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	first := strings.Join(r.last().Log, " ")
	rep := r.p.rep[r.npc.Region]
	if rep != 0 && rep != -stealFailRep*-1 && rep != stealFailRep {
		t.Fatalf("réputation inattendue: %d", rep)
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if second := strings.Join(r.last().Log, " "); second == first || r.p.stats.Gold != gold || r.p.rep[r.npc.Region] != rep {
		t.Fatalf("second vol immédiat: %q", second)
	}
	r.g.tick += stealCooldown
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if strings.Join(r.last().Log, " ") == "" {
		t.Fatal("pas de réponse après le délai")
	}
}

func TestOnlyMerchantsCanBeRobbed(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	if _, ok := r.offer(protocol.NPCSteal); ok {
		t.Fatal("on ne vole pas le soigneur")
	}
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if r.p.rep[r.npc.Region] != 0 {
		t.Fatal("aucune conséquence attendue")
	}
}

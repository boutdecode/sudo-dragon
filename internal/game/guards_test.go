package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/i18n"
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

// newGuardRig places a player at the centre of a village or town near the start, and spawns its guards.
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
		t.Skipf("no %v near the start", kind)
	}
	p, _ := joinRec(g, "hero")
	p.lang = i18n.FR // the answers are asserted on French catalogue text
	r := &guardRig{g: g, s: s, p: p}
	p.sendNPC = func(d protocol.NPCDialog) { r.nd = append(r.nd, d) }
	place(g, p, g.freeNear(world.Pos{X: s.Guards[0].Pos.X + 4, Y: s.Guards[0].Pos.Y + 1}))
	g.activateGuards()
	r.gd = g.guards().byKey[s.Guards[0].Key()]
	if r.gd == nil {
		t.Fatal("the guard did not appear")
	}
	return r
}

func (r *guardRig) lastDialog() protocol.NPCDialog { return r.nd[len(r.nd)-1] }

// stepGuards makes the guards act n ticks, without the rest of the world.
func (r *guardRig) stepGuards(n int) {
	for range n {
		r.g.tick++
		r.g.stepGuards()
	}
}

func TestGuardsAppearNearPlayerAndLeaveWithIt(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	if got := len(r.g.guards().byKey); got < len(r.s.Guards) {
		t.Fatalf("%d guards, expected at least %d", got, len(r.s.Guards))
	}
	if r.g.guardAt(r.gd.npc.Pos) != r.gd || !r.g.blocked(r.gd.npc.Pos) {
		t.Fatal("a guard occupies its tile")
	}
	place(r.g, r.p, world.Pos{X: r.p.pos.X + 3000, Y: r.p.pos.Y})
	r.g.activateGuards()
	if len(r.g.guards().byKey) != 0 || len(r.g.guards().at) != 0 {
		t.Fatalf("guards remain far from any player: %d", len(r.g.guards().byKey))
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
		t.Fatalf("guard missing from the snapshot: %+v", snap.Monsters)
	}
	if len(r.g.monsters) != 0 {
		t.Fatal("a guard must not be registered among the monsters")
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
			t.Fatalf("the guard leaves its place: %v", pos)
		}
		if r.gd.avatar.pos != pos || r.g.guardAt(pos) != r.gd {
			t.Fatal("inconsistent positions of the guard, its avatar and the index")
		}
		if r.p.fight != nil {
			t.Fatal("a guard does not attack a law-abiding player")
		}
	}
	if !moved {
		t.Fatal("the guard does not patrol")
	}
}

func TestWalkingIntoAGuardTalksAndAnswersDirections(t *testing.T) {
	r := newGuardRig(t, world.KindTown)
	r.g.bumpGuard(r.p, r.gd)
	if r.p.talk == nil || r.p.talk.npc.Kind != world.NPCGuard {
		t.Fatal("the conversation with the guard did not start")
	}
	d := r.lastDialog()
	if d.Kind != "guard" || len(d.Offers) < 5 {
		t.Fatalf("guard menu: %+v", d)
	}
	for _, o := range d.Offers {
		if o.Action != protocol.NPCWhere && o.Action != protocol.NPCPlaces {
			t.Errorf("unexpected offer: %+v", o)
		}
	}
	// The town's blacksmith is located with a direction and a distance.
	r.g.talkAct(r.p, protocol.NPCWhere, string(world.NPCSmith))
	log := strings.Join(r.lastDialog().Log, " ")
	if !strings.Contains(log, "pas") || !strings.Contains(log, " au ") && !strings.Contains(log, " à l'") {
		t.Fatalf("answer without direction: %q", log)
	}
	// An unknown action or a job outside the list is refused.
	r.g.talkAct(r.p, protocol.NPCWhere, "dragon")
	if got := strings.Join(r.lastDialog().Log, " "); got == "" {
		t.Fatal("no answer")
	}
	r.g.talkAct(r.p, protocol.NPCPlaces, "")
	if len(r.lastDialog().Log) == 0 {
		t.Fatal("no neighbouring places")
	}
}

func TestGuardPointsToAnotherPlaceWhenTradeIsMissing(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	r.g.bumpGuard(r.p, r.gd)
	// A village has no merchant: the guard points to a neighbouring place, or admits not knowing any.
	r.g.talkAct(r.p, protocol.NPCWhere, string(world.NPCMerchant))
	log := strings.Join(r.lastDialog().Log, " ")
	if !strings.Contains(strings.ToLower(log), "marchand") || !strings.HasPrefix(log, "Pas de marchand ici") {
		t.Fatalf("answer: %q", log)
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
		t.Fatal("the guard did not attack the outlaw")
	}
	if r.p.fight.ms[0].guard.avatar.fight == nil { // it may be the village's other guard that attacked
		t.Fatal("the guard must be frozen during the fight")
	}
	// On the first turn, the log announces the guard.
	if r.p.fight.b.Foes[0].Kind != "guard" {
		t.Fatalf("opponent: %+v", r.p.fight.b.Foes[0])
	}
}

func TestGuardsLeaveGoodPeopleAlone(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	for _, rep := range []int{0, quest.Suspect, quest.Wanted + 1, 12} {
		r.p.rep[r.s.Region] = rep
		place(r.g, r.p, r.g.freeNear(world.Pos{X: r.gd.npc.Pos.X + 2, Y: r.gd.npc.Pos.Y}))
		r.stepGuards(300)
		if r.p.fight != nil {
			t.Fatalf("attacked with %d reputation points", rep)
		}
	}
}

func TestOutlawInAnotherPlaceIsNotAttacked(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	other := world.RegionPos{X: r.s.Region.X + 40, Y: r.s.Region.Y}
	r.p.rep[other] = quest.Wanted // outlaw elsewhere only
	place(r.g, r.p, r.g.freeNear(world.Pos{X: r.gd.npc.Pos.X + 2, Y: r.gd.npc.Pos.Y}))
	r.stepGuards(300)
	if r.p.fight != nil {
		t.Fatal("reputation is specific to each place")
	}
}

func TestSlayingAGuardCostsReputationAndHeWillBeReplaced(t *testing.T) {
	r := newGuardRig(t, world.KindVillage)
	r.p.rep[r.s.Region] = quest.Wanted
	r.gd.avatar.sp.HP = 1
	r.p.stats.Level = 20
	r.g.bumpGuard(r.p, r.gd) // outlaw: the fight replaces the conversation
	if r.p.fight == nil {
		t.Fatal("the fight did not start")
	}
	key := r.gd.npc.Key()
	for i := 0; i < 100 && r.p.fight != nil; i++ {
		r.g.act(r.p, combat.Attack, 0)
	}
	if r.p.fight != nil {
		t.Fatal("endless fight")
	}
	if r.p.hp <= 0 {
		t.Skip("player defeated by the roll")
	}
	if _, alive := r.g.guards().byKey[key]; alive || r.g.guardAt(r.gd.npc.Pos) != nil {
		t.Fatal("the defeated guard is still there")
	}
	if got := r.p.rep[r.s.Region]; got != quest.Wanted+guardKillRep {
		t.Fatalf("reputation %d, expected %d", got, quest.Wanted+guardKillRep)
	}
	// It does not come back right away, but later.
	r.g.activateGuards()
	if _, back := r.g.guards().byKey[key]; back {
		t.Fatal("the guard came back at once")
	}
	r.g.tick += townGuardRespawn
	r.g.activateGuards()
	if _, back := r.g.guards().byKey[key]; !back {
		t.Fatal("the guard is not replaced")
	}
}

func TestReputationIsBoundedBelow(t *testing.T) {
	g := newGame()
	p, _ := joinRec(g, "hero")
	rp := world.RegionPos{X: 4, Y: 4}
	g.addRep(p, rp, -1000)
	if p.rep[rp] != quest.MinReputation {
		t.Fatalf("reputation %d", p.rep[rp])
	}
	g.addRep(p, rp, -quest.MinReputation)
	if _, kept := p.rep[rp]; kept {
		t.Fatal("a reputation back to zero must not stay in the table")
	}
}

func TestAbandoningAQuestCostsReputation(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	q := r.p.quests[0]
	r.g.abandonQuest(r.p, q.ID)
	if got := r.p.rep[q.Place()]; got != -abandonRepLoss {
		t.Fatalf("reputation %d after giving up", got)
	}
}

func TestStealingRisksReputationAndHasACooldown(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()
	o, ok := r.offer(protocol.NPCSteal)
	if !ok || !o.Available {
		t.Fatal("no theft offer at the merchant")
	}
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	first := strings.Join(r.last().Log, " ")
	rep := r.p.rep[r.npc.Region]
	if rep != 0 && rep != -stealFailRep*-1 && rep != stealFailRep {
		t.Fatalf("unexpected reputation: %d", rep)
	}
	gold := r.p.stats.Gold
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if second := strings.Join(r.last().Log, " "); second == first || r.p.stats.Gold != gold || r.p.rep[r.npc.Region] != rep {
		t.Fatalf("immediate second theft: %q", second)
	}
	r.g.tick += stealCooldown
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if strings.Join(r.last().Log, " ") == "" {
		t.Fatal("no answer after the delay")
	}
}

func TestOnlyMerchantsCanBeRobbed(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	if _, ok := r.offer(protocol.NPCSteal); ok {
		t.Fatal("the healer cannot be robbed")
	}
	r.g.talkAct(r.p, protocol.NPCSteal, "")
	if r.p.rep[r.npc.Region] != 0 {
		t.Fatal("no consequence expected")
	}
}

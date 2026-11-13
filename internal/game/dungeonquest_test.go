package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// workRig places a level 5 player in front of a villager who gives quests, with a dungeon within range.
func workRig(t *testing.T) (*npcRig, *world.Dungeon) {
	t.Helper()
	r := newNPCRig(t, world.NPCVillager)
	r.p.lang = i18n.FR // dungeon names (d.Name) are the French ones
	if !r.npc.GivesQuests {
		t.Skip("first villager without quests")
	}
	r.g.Dungeons = true
	r.p.stats.Level = 5
	r.talk()
	ds := r.g.dungeonsFor(r.p, r.npc)
	if len(ds) == 0 {
		t.Skip("no dungeon within range of the starting village")
	}
	return r, ds[0]
}

func offered(d protocol.NPCDialog, action string) bool {
	for _, o := range d.Offers {
		if o.Action == action {
			return true
		}
	}
	return false
}

func TestAVillagerWithADungeonNearbyOffersWorkWithoutAModel(t *testing.T) {
	r, d := workRig(t)
	if r.g.Talker != nil {
		t.Fatal("this test targets the case without a language model")
	}
	if !offered(r.last(), protocol.NPCAskWork) {
		t.Fatalf("no work offer: %+v", r.last().Offers)
	}
	r.act(protocol.NPCAskWork)
	got := r.last()
	if got.Mode != protocol.NPCModeQuest || got.Proposal == nil {
		t.Fatalf("a proposal was expected: %+v", got)
	}
	pr := got.Proposal
	if !strings.Contains(pr.Goal, d.Name) || !strings.Contains(pr.Title, d.Name) || pr.Need != 1 {
		t.Errorf("proposal: %+v", pr)
	}
	if pr.Gold <= 0 || pr.XP <= 0 {
		t.Errorf("empty reward: %+v", pr)
	}
	if len(r.p.quests) != 0 {
		t.Fatal("the quest exists before acceptance")
	}
	// It reveals nothing until accepted.
	if r.p.discovered[d.Region] {
		t.Fatal("entrance revealed before acceptance")
	}

	var dungeons []protocol.Dungeons
	r.p.sendDungeonsMsg = func(m protocol.Dungeons) { dungeons = append(dungeons, m) }
	r.act(protocol.NPCQuestAccept)
	if len(r.p.quests) != 1 {
		t.Fatalf("%d quests after acceptance", len(r.p.quests))
	}
	q := r.p.quests[0]
	if q.Objective != (quest.Objective{Kind: quest.Dungeon, Species: "lich", Count: 1, Target: d.Region}) || q.State != quest.Active {
		t.Errorf("quest: %+v", q)
	}
	if !r.p.discovered[d.Region] || len(dungeons) != 1 || dungeons[0].New != d.ID {
		t.Errorf("the entrance should be revealed on acceptance: discovered=%v, messages %+v", r.p.discovered[d.Region], dungeons)
	}
	// The same dungeon is not entrusted twice.
	r.act(protocol.NPCBack)
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		if dd == d {
			t.Error("the dungeon in progress is still offered")
		}
	}
}

func TestNoWorkForALowLevelHero(t *testing.T) {
	r, d := workRig(t)
	r.p.stats.Level = quest.DungeonLevel(d.Tier) - 1
	if r.g.hasWork(r.p, r.npc) {
		t.Fatal("work for a player who is too weak")
	}
	r.g.sendTalk(r.p, r.npc.Kind, nil, false)
	if offered(r.last(), protocol.NPCAskWork) {
		t.Error("the work offer should not appear")
	}
	// Even when forcing the action, nothing is offered.
	r.act(protocol.NPCAskWork)
	if r.last().Proposal != nil || r.last().Mode == protocol.NPCModeQuest {
		t.Errorf("quest offered to a player who is too weak: %+v", r.last())
	}
}

func TestDungeonQuestIsFinishedByKillingItsLich(t *testing.T) {
	g, d := dungeonGame(t)
	p, _ := joinRec(g, "a")
	p.sendCombat = func(protocol.Combat) {}
	other := g.World.DungeonsNear(0, 0, 6)
	var elsewhere world.RegionPos
	for _, o := range other {
		if o != d {
			elsewhere = o.Region
			break
		}
	}
	p.quests = []quest.Quest{
		{ID: 1, State: quest.Active, Objective: quest.Objective{Kind: quest.Dungeon, Species: "lich", Count: 1, Target: d.Region}},
		{ID: 2, State: quest.Active, Objective: quest.Objective{Kind: quest.Dungeon, Species: "lich", Count: 1, Target: elsewhere}},
		{ID: 3, State: quest.Active, Objective: quest.Objective{Kind: quest.Boss, Species: "warlord", Count: 1}},
	}
	p.nextQuestID = 4
	walkIn(g, p, d)

	lich, _ := dungeonSpecies(world.RoleBoss, 0)
	lich.HP, lich.Hit, lich.Dmg = 1, -50, dice.D(1, 1, 0)
	boss := g.addMonsterIn(p.zone, lich, g.freeNearIn(p.zone, p.pos, 1))
	g.startFight(p, boss, true)
	for i := 0; i < 100 && p.fight != nil; i++ {
		g.act(p, 1, 0)
	}
	if p.fight != nil {
		t.Fatal("fight not finished")
	}
	if p.quests[0].State != quest.Ready || p.quests[0].Progress != 1 {
		t.Errorf("this dungeon's quest should be ready: %+v", p.quests[0])
	}
	if elsewhere != (world.RegionPos{}) && p.quests[1].State != quest.Active {
		t.Errorf("another dungeon's quest advanced: %+v", p.quests[1])
	}
	if p.quests[2].State != quest.Active {
		t.Errorf("the warlord quest advanced: %+v", p.quests[2])
	}
}

func TestTheModelMayProposeADungeonFromTheAllowedList(t *testing.T) {
	r, ft := giverRig(t)
	r.p.lang = i18n.FR // dungeon names (d.Name) are the French ones
	r.g.Dungeons = true
	ds := r.g.dungeonsFor(r.p, r.npc)
	if len(ds) == 0 {
		t.Skip("no dungeon within range")
	}
	d := ds[0]
	r.act(protocol.NPCTalk)
	ctx := ft.req(ft.count() - 1).Offer
	if ctx == nil {
		t.Fatal("no offer context")
	}
	ref, ok := ctx.Quest.Dungeons[strings.ToLower(d.Name)]
	if !ok || ref.Region != d.Region || ref.Tier != d.Tier {
		t.Fatalf("the dungeon is not allowed: %+v", ctx.Quest.Dungeons)
	}
	// A proposal outside the list or above one's level is refused.
	for name, p := range map[string]quest.Proposal{
		"unknown": {Title: "t", Story: "s", Type: "dungeon", Place: "Donjon Imaginaire"},
		"a place": {Title: "t", Story: "s", Type: "dungeon", Place: ""},
		"level 1": {Title: "t", Story: "s", Type: "dungeon", Place: d.Name},
	} {
		c := ctx.Quest
		if name == "level 1" {
			c.Level = 1
		}
		if _, err := quest.Validate(p, c); err == nil {
			t.Errorf("%s: proposal accepted", name)
		}
	}
	q, err := quest.Validate(quest.Proposal{Title: "The master", Story: "Go.", Type: "dungeon", Place: d.Name, Gold: 999999, XP: 999999, Potions: 99}, ctx.Quest)
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := quest.RewardBounds(150+50*d.Tier, true)
	if q.Reward.Gold > hi.Gold || q.Reward.XP > hi.XP || q.Reward.Potions > hi.Potions || q.Reward.Gold < lo.Gold {
		t.Errorf("reward out of bounds: %+v", q.Reward)
	}
}

func TestADungeonOnCooldownIsNotOfferedForWork(t *testing.T) {
	r, d := workRig(t)
	other, _ := joinRec(r.g, "b")
	walkIn(r.g, other, d)
	killTheLich(t, r.g, other)
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		if dd == d {
			t.Fatal("a dungeon whose boss has just fallen cannot be entrusted")
		}
	}
	r.g.tick += dungeonResetTicks
	found := false
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		found = found || dd == d
	}
	if !found {
		t.Error("the dungeon, returned, should be entrustable again")
	}
}

package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// workRig place un joueur de niveau 5 devant un villageois qui donne des quêtes, avec un donjon à portée.
func workRig(t *testing.T) (*npcRig, *world.Dungeon) {
	t.Helper()
	r := newNPCRig(t, world.NPCVillager)
	if !r.npc.GivesQuests {
		t.Skip("premier villageois sans quêtes")
	}
	r.g.Dungeons = true
	r.p.stats.Level = 5
	r.talk()
	ds := r.g.dungeonsFor(r.p, r.npc)
	if len(ds) == 0 {
		t.Skip("aucun donjon à portée du village de départ")
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
		t.Fatal("ce test vise le cas sans modèle de langage")
	}
	if !offered(r.last(), protocol.NPCAskWork) {
		t.Fatalf("pas d'offre de travail: %+v", r.last().Offers)
	}
	r.act(protocol.NPCAskWork)
	got := r.last()
	if got.Mode != protocol.NPCModeQuest || got.Proposal == nil {
		t.Fatalf("une proposition était attendue: %+v", got)
	}
	pr := got.Proposal
	if !strings.Contains(pr.Goal, d.Name) || !strings.Contains(pr.Title, d.Name) || pr.Need != 1 {
		t.Errorf("proposition: %+v", pr)
	}
	if pr.Gold <= 0 || pr.XP <= 0 {
		t.Errorf("récompense vide: %+v", pr)
	}
	if len(r.p.quests) != 0 {
		t.Fatal("la quête existe avant l'acceptation")
	}
	// Elle ne révèle rien tant qu'on n'a pas accepté.
	if r.p.discovered[d.Region] {
		t.Fatal("entrée révélée avant l'acceptation")
	}

	var dungeons []protocol.Dungeons
	r.p.sendDungeonsMsg = func(m protocol.Dungeons) { dungeons = append(dungeons, m) }
	r.act(protocol.NPCQuestAccept)
	if len(r.p.quests) != 1 {
		t.Fatalf("%d quêtes après acceptation", len(r.p.quests))
	}
	q := r.p.quests[0]
	if q.Objective != (quest.Objective{Kind: quest.Dungeon, Species: "lich", Count: 1, Target: d.Region}) || q.State != quest.Active {
		t.Errorf("quête: %+v", q)
	}
	if !r.p.discovered[d.Region] || len(dungeons) != 1 || dungeons[0].New != d.ID {
		t.Errorf("l'entrée devait être révélée à l'acceptation: découvert=%v, messages %+v", r.p.discovered[d.Region], dungeons)
	}
	// Le même donjon n'est pas confié deux fois.
	r.act(protocol.NPCBack)
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		if dd == d {
			t.Error("le donjon en cours est encore offert")
		}
	}
}

func TestNoWorkForALowLevelHero(t *testing.T) {
	r, d := workRig(t)
	r.p.stats.Level = quest.DungeonLevel(d.Tier) - 1
	if r.g.hasWork(r.p, r.npc) {
		t.Fatal("du travail pour un joueur trop faible")
	}
	r.g.sendTalk(r.p, r.npc.Kind, nil, false)
	if offered(r.last(), protocol.NPCAskWork) {
		t.Error("l'offre de travail ne devrait pas apparaître")
	}
	// Même en forçant l'action, rien n'est proposé.
	r.act(protocol.NPCAskWork)
	if r.last().Proposal != nil || r.last().Mode == protocol.NPCModeQuest {
		t.Errorf("quête proposée à un joueur trop faible: %+v", r.last())
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
		t.Fatal("combat non terminé")
	}
	if p.quests[0].State != quest.Ready || p.quests[0].Progress != 1 {
		t.Errorf("la quête de ce donjon devrait être prête: %+v", p.quests[0])
	}
	if elsewhere != (world.RegionPos{}) && p.quests[1].State != quest.Active {
		t.Errorf("la quête d'un autre donjon a avancé: %+v", p.quests[1])
	}
	if p.quests[2].State != quest.Active {
		t.Errorf("la quête de seigneur de guerre a avancé: %+v", p.quests[2])
	}
}

func TestTheModelMayProposeADungeonFromTheAllowedList(t *testing.T) {
	r, ft := giverRig(t)
	r.g.Dungeons = true
	ds := r.g.dungeonsFor(r.p, r.npc)
	if len(ds) == 0 {
		t.Skip("aucun donjon à portée")
	}
	d := ds[0]
	r.act(protocol.NPCTalk)
	ctx := ft.req(ft.count() - 1).Offer
	if ctx == nil {
		t.Fatal("pas de contexte d'offre")
	}
	ref, ok := ctx.Quest.Dungeons[strings.ToLower(d.Name)]
	if !ok || ref.Region != d.Region || ref.Tier != d.Tier {
		t.Fatalf("le donjon n'est pas autorisé: %+v", ctx.Quest.Dungeons)
	}
	// Une proposition hors liste ou au-dessus de son niveau est refusée.
	for name, p := range map[string]quest.Proposal{
		"inconnu":  {Title: "t", Story: "s", Type: "dungeon", Place: "Donjon Imaginaire"},
		"un lieu":  {Title: "t", Story: "s", Type: "dungeon", Place: ""},
		"niveau 1": {Title: "t", Story: "s", Type: "dungeon", Place: d.Name},
	} {
		c := ctx.Quest
		if name == "niveau 1" {
			c.Level = 1
		}
		if _, err := quest.Validate(p, c); err == nil {
			t.Errorf("%s: proposition acceptée", name)
		}
	}
	q, err := quest.Validate(quest.Proposal{Title: "Le maître", Story: "Allez-y.", Type: "dungeon", Place: d.Name, Gold: 999999, XP: 999999, Potions: 99}, ctx.Quest)
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := quest.RewardBounds(150+50*d.Tier, true)
	if q.Reward.Gold > hi.Gold || q.Reward.XP > hi.XP || q.Reward.Potions > hi.Potions || q.Reward.Gold < lo.Gold {
		t.Errorf("récompense hors bornes: %+v", q.Reward)
	}
}

func TestADungeonOnCooldownIsNotOfferedForWork(t *testing.T) {
	r, d := workRig(t)
	other, _ := joinRec(r.g, "b")
	walkIn(r.g, other, d)
	killTheLich(t, r.g, other)
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		if dd == d {
			t.Fatal("un donjon dont le boss vient de tomber ne peut pas être confié")
		}
	}
	r.g.tick += dungeonResetTicks
	found := false
	for _, dd := range r.g.dungeonsFor(r.p, r.npc) {
		found = found || dd == d
	}
	if !found {
		t.Error("le donjon, revenu, devrait de nouveau pouvoir être confié")
	}
}

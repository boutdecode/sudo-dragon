package game

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// giverRig place un joueur déjà d'accord à côté d'un villageois qui donne des quêtes.
func giverRig(t *testing.T) (*npcRig, *fakeTalker) {
	t.Helper()
	r := newNPCRig(t, world.NPCVillager)
	if !r.npc.GivesQuests {
		t.Skip("premier villageois sans quêtes") // ne devrait pas arriver : voir les tests du monde
	}
	ft := newFakeTalker()
	r.g.Talker = ft
	r.p.consent = true
	r.p.stats.Level = 5
	r.talk()
	return r, ft
}

func wolfProposal() quest.Proposal {
	return quest.Proposal{Title: "Les loups du bois", Story: "Ils rôdent près des champs.", Type: "kill", Species: "wolf", Count: 3, Gold: 40, XP: 6, Potions: 1}
}

// propose fait parler le joueur, répond avec la quête proposée et renvoie l'état du dialogue.
func (r *npcRig) propose(ft *fakeTalker, p quest.Proposal) protocol.NPCDialog {
	r.tt.Helper()
	r.act(protocol.NPCTalk) // ouvre la discussion: le PNJ salue
	n := ft.count() - 1
	ctx := ft.req(n).Offer
	if ctx == nil {
		r.tt.Fatal("le jeu n'a pas autorisé de quête")
	}
	q, err := quest.Validate(p, ctx.Quest)
	if err != nil {
		r.tt.Fatal(err)
	}
	ft.answer(n, npcai.Reply{Text: "Il me faudrait de l'aide.", Quest: &q})
	r.pump()
	return r.last()
}

func TestOnlyAGiverWithRoomGetsAnOffer(t *testing.T) {
	r, ft := giverRig(t)
	r.act(protocol.NPCTalk)
	if ft.req(0).Offer == nil {
		t.Fatal("un donneur libre devait pouvoir proposer une quête")
	}
	o := ft.req(0).Offer
	if len(o.Quest.Species) == 0 || o.Quest.Level != 5 {
		t.Fatalf("contexte: %+v", o.Quest)
	}
	for _, sp := range o.Quest.Species {
		if sp.Kind == "warlord" || sp.Kind == "skeleton" || sp.Kind == "knight" {
			t.Fatalf("espèce de garnison proposée: %s", sp.Kind)
		}
	}

	// Un marchand ou un soigneur ne propose rien.
	h := newNPCRig(t, world.NPCHealer)
	hf := newFakeTalker()
	h.g.Talker, h.p.consent = hf, true
	h.talk()
	h.act(protocol.NPCTalk)
	if hf.req(0).Offer != nil {
		t.Fatal("un soigneur ne donne pas de quêtes")
	}
}

func TestProposalNeedsExplicitAcceptance(t *testing.T) {
	r, ft := giverRig(t)
	d := r.propose(ft, wolfProposal())
	if d.Mode != protocol.NPCModeQuest || d.Proposal == nil || d.Proposal.Title != "Les loups du bois" {
		t.Fatalf("carte de proposition attendue: %+v", d)
	}
	if len(r.p.quests) != 0 {
		t.Fatal("la quête existe avant l'acceptation")
	}
	if d.Proposal.Gold != 40 || d.Proposal.Need != 3 || !strings.Contains(d.Proposal.Goal, "Loup") {
		t.Fatalf("carte: %+v", d.Proposal)
	}

	r.act(protocol.NPCQuestRefuse)
	if len(r.p.quests) != 0 || r.last().Mode != protocol.NPCModeChat {
		t.Fatalf("refus: %d quêtes, mode %s", len(r.p.quests), r.last().Mode)
	}
	// Accepter sans proposition en attente ne crée rien.
	r.act(protocol.NPCQuestAccept)
	if len(r.p.quests) != 0 {
		t.Fatal("acceptation sans proposition")
	}
}

func TestFullQuestLifecycle(t *testing.T) {
	r, ft := giverRig(t)
	var saved []Saved
	r.g.Persist = func(_ string, s Saved) { saved = append(saved, s) }
	var books []protocol.Quests
	r.p.sendQuestBook = func(q protocol.Quests) { books = append(books, q) }

	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	if len(r.p.quests) != 1 || r.p.quests[0].State != quest.Active || r.p.quests[0].ID == 0 || r.p.quests[0].Account != "hero" {
		t.Fatalf("quêtes: %+v", r.p.quests)
	}
	if len(books) == 0 || len(books[len(books)-1].Open) != 1 {
		t.Fatalf("livre: %+v", books)
	}
	if n := len(saved); n == 0 || !saved[n-1].QuestsDirty || len(saved[n-1].Quests) != 1 {
		t.Fatalf("la quête acceptée doit être sauvegardée tout de suite: %+v", saved)
	}

	wolf := speciesByKind(t, "wolf")
	rat := speciesByKind(t, "rat")
	if log := r.g.questKill(r.p, rat); len(log) != 0 {
		t.Fatalf("un rat ne compte pas: %v", log)
	}
	r.g.questKill(r.p, wolf)
	r.g.questKill(r.p, wolf)
	if q := r.p.quests[0]; q.State != quest.Active || q.Progress != 2 {
		t.Fatalf("%+v", q)
	}
	log := r.g.questKill(r.p, wolf)
	if r.p.quests[0].State != quest.Ready || len(log) != 1 || !strings.Contains(log[0], "accomplie") {
		t.Fatalf("log=%v quête=%+v", log, r.p.quests[0])
	}
	if r.p.stats.Gold != 0 {
		t.Fatal("rien ne doit être versé avant la remise")
	}

	// Retour chez le donneur : l'offre de remise existe, même sans modèle de langage.
	r.act(protocol.NPCLeave)
	r.g.Talker = nil
	r.talk()
	o, has := r.offer(protocol.NPCTurnIn)
	if !has || o.Arg != strconv.FormatInt(r.p.quests[0].ID, 10) || !strings.Contains(o.Label, "Les loups du bois") {
		t.Fatalf("offre de remise: %+v (%v)", o, has)
	}
	potionsBefore := r.p.stats.Potions
	r.g.talkAct(r.p, protocol.NPCTurnIn, o.Arg)

	q := r.p.quests[0]
	if q.State != quest.Done || q.DoneAt.IsZero() {
		t.Fatalf("%+v", q)
	}
	if r.p.stats.Gold != 40 || r.p.stats.Potions != potionsBefore+1 {
		t.Fatalf("récompense: or %d, potions %d", r.p.stats.Gold, r.p.stats.Potions)
	}
	if r.p.rep[r.npc.Region] != q.ReputationGain() {
		t.Fatalf("réputation: %v", r.p.rep)
	}
	last := saved[len(saved)-1]
	if !last.QuestsDirty || last.Rep[r.npc.Region] == 0 || last.Stats.Gold != 40 {
		t.Fatalf("la remise doit être sauvegardée: %+v", last)
	}

	// Rendre deux fois ne paie pas deux fois.
	r.g.talkAct(r.p, protocol.NPCTurnIn, o.Arg)
	if r.p.stats.Gold != 40 {
		t.Fatal("double paiement")
	}
}

func TestWrongGiverCannotCollect(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	r.p.quests[0].State, r.p.quests[0].Progress = quest.Ready, 3
	r.p.quests[0].Giver.Index += 7 // un autre PNJ
	r.act(protocol.NPCLeave)
	r.talk()
	r.g.talkAct(r.p, protocol.NPCTurnIn, strconv.FormatInt(r.p.quests[0].ID, 10))
	if r.p.stats.Gold != 0 || r.p.quests[0].State != quest.Ready {
		t.Fatal("une quête a été rendue au mauvais PNJ")
	}
}

func TestSendQuestsTellsWhichGiversAreIdle(t *testing.T) {
	r, _ := giverRig(t)
	var book protocol.Quests
	r.p.sendQuestBook = func(q protocol.Quests) { book = q }
	now := time.Unix(2_000_000, 0)
	r.g.Clock = func() time.Time { return now }

	r.g.sendQuests(r.p)
	if book.Full || len(book.Pauses) != 0 {
		t.Fatalf("rien à signaler au départ: %+v", book)
	}
	r.p.quests = append(r.p.quests, quest.Quest{ID: 7, State: quest.Done, DoneAt: now.Add(-10 * time.Minute), Giver: r.npc.Key()})
	r.g.sendQuests(r.p)
	want := protocol.GiverPause{RX: r.npc.Region.X, RY: r.npc.Region.Y, Idx: r.npc.Index, Secs: int((quest.GiverPause - 10*time.Minute).Seconds()) + 1}
	if len(book.Pauses) != 1 || book.Pauses[0] != want {
		t.Fatalf("pause attendue %+v : %+v", want, book.Pauses)
	}
	now = now.Add(time.Hour)
	r.g.sendQuests(r.p)
	if len(book.Pauses) != 0 {
		t.Fatalf("pause écoulée: %+v", book.Pauses)
	}
	for i := 0; i < quest.MaxActive; i++ {
		r.p.quests = append(r.p.quests, quest.Quest{ID: int64(100 + i), State: quest.Active, Giver: world.NPCKey{Index: 90 + i}})
	}
	r.g.sendQuests(r.p)
	if !book.Full {
		t.Fatal("quota atteint: Full attendu")
	}
}

func TestDroppedQuestIsAnnouncedToThePlayer(t *testing.T) {
	r, ft := giverRig(t)
	r.act(protocol.NPCTalk)
	ft.answer(ft.count()-1, npcai.Reply{Text: "J'ai une mission pour toi !", QuestDropped: true})
	r.pump()
	lines := r.last().Lines
	if len(lines) == 0 || lines[len(lines)-1].Who != "sys" || !strings.Contains(lines[len(lines)-1].Text, "rien") {
		t.Fatalf("le joueur doit être prévenu: %+v", lines)
	}
}

func TestQuestLimits(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)

	// Une seule quête ouverte par donneur : plus d'offre, et une proposition tardive est ignorée.
	if r.g.offerContext(r.p, r.npc) != nil {
		t.Fatal("offre alors qu'une quête de ce PNJ est en cours")
	}

	// Après la remise, délai avant la suivante.
	now := time.Unix(2_000_000, 0)
	r.g.Clock = func() time.Time { return now }
	r.p.quests[0].State, r.p.quests[0].DoneAt = quest.Done, now
	if r.g.canOffer(r.p, r.npc) {
		t.Fatal("pas de nouvelle quête pendant le délai")
	}
	now = now.Add(quest.GiverPause + time.Second)
	if !r.g.canOffer(r.p, r.npc) {
		t.Fatal("le délai est écoulé")
	}

	// Au plus MaxActive quêtes ouvertes.
	for i := 0; i < quest.MaxActive; i++ {
		r.p.quests = append(r.p.quests, quest.Quest{ID: int64(100 + i), State: quest.Active, Giver: world.NPCKey{Index: 90 + i}})
	}
	if r.g.canOffer(r.p, r.npc) {
		t.Fatal("trop de quêtes ouvertes")
	}
}

func TestIdenticalObjectiveIsNotProposedTwice(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	q, err := quest.Validate(wolfProposal(), ft.req(0).Offer.Quest)
	if err != nil {
		t.Fatal(err)
	}
	// La quête en cours est la même : une nouvelle proposition identique est écartée.
	r.p.talk.proposal, r.p.talk.mode = nil, protocol.NPCModeChat
	r.p.quests[0].State = quest.Done // libère le donneur…
	r.p.quests[0].DoneAt = time.Now().Add(-2 * quest.GiverPause)
	r.p.quests = append(r.p.quests, quest.Quest{ID: 77, Giver: q.Giver, Objective: q.Objective, State: quest.Active})
	r.g.proposeQuest(r.p, &q)
	if r.p.talk.proposal != nil {
		t.Fatal("un objectif déjà en cours a été proposé de nouveau")
	}
}

func TestProposalIsLostWhenLeaving(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCLeave)
	if len(r.p.quests) != 0 {
		t.Fatal("la quête s'est créée sans acceptation")
	}
	r.act(protocol.NPCQuestAccept)
	if len(r.p.quests) != 0 {
		t.Fatal("acceptation après avoir quitté")
	}
}

func TestBossAndReachQuests(t *testing.T) {
	r, _ := giverRig(t)
	r.p.quests = []quest.Quest{
		{ID: 1, Giver: r.npc.Key(), Title: "Le seigneur", Story: "x", State: quest.Active,
			Objective: quest.Objective{Kind: quest.Boss, Species: "warlord", Count: 1}},
	}
	if log := r.g.questKill(r.p, speciesByKind(t, "wolf")); len(log) != 0 {
		t.Fatal("un loup n'est pas le seigneur")
	}
	r.g.questKill(r.p, speciesByKind(t, "warlord"))
	if r.p.quests[0].State != quest.Ready {
		t.Fatalf("%+v", r.p.quests[0])
	}

	// Atteindre un lieu : on y entre pour de bon.
	var target *world.Structure
	for _, s := range r.g.World.StructuresNear(r.g.World.FindSpawn(), 900) {
		if s.Region != r.npc.Region && s.Kind != world.KindFortress {
			target = s
			break
		}
	}
	if target == nil {
		t.Skip("pas d'autre lieu à portée")
	}
	r.p.quests = []quest.Quest{{ID: 2, Giver: r.npc.Key(), Title: "Message", Story: "x", State: quest.Active,
		Objective: quest.Objective{Kind: quest.Reach, Count: 1, Target: target.Region}}}
	r.p.refreshQuestFlags()
	r.g.setPos(r.p, world.Pos{X: target.Origin.X - 40, Y: target.Origin.Y - 40}) // hors de l'emprise
	if r.p.quests[0].State != quest.Active {
		t.Fatal("quête accomplie sans être entré")
	}
	inside := world.Pos{X: target.Origin.X + target.W/2, Y: target.Origin.Y + target.H/2}
	r.g.setPos(r.p, inside)
	if r.p.quests[0].State != quest.Ready {
		t.Fatalf("lieu atteint mais quête %+v", r.p.quests[0])
	}
}

func TestReputationDiscountsMenuAndTransactionAgree(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats.Level, r.p.stats.Gold, r.p.hp = 5, 1000, 1
	r.p.rep = map[world.RegionPos]int{r.npc.Region: quest.Esteemed}
	r.talk()
	o, has := r.offer(protocol.NPCHeal)
	want := quest.Discount(healPrice(5), quest.Esteemed)
	if !has || o.Price != want || want >= healPrice(5) {
		t.Fatalf("prix affiché %d, attendu %d (plein tarif %d)", o.Price, want, healPrice(5))
	}
	r.act(protocol.NPCHeal)
	if r.p.stats.Gold != 1000-want {
		t.Fatalf("débité %d, annoncé %d", 1000-r.p.stats.Gold, want)
	}
	// La réputation d'un autre lieu ne compte pas.
	r2 := newNPCRig(t, world.NPCHealer)
	r2.p.stats.Level = 5
	r2.p.rep = map[world.RegionPos]int{{X: 999, Y: 999}: quest.Honored}
	r2.talk()
	if o, _ := r2.offer(protocol.NPCHeal); o.Price != healPrice(5) {
		t.Fatalf("remise venue d'un autre lieu: %d", o.Price)
	}
}

func TestQuestsSurviveReconnectionAndExpire(t *testing.T) {
	r, ft := giverRig(t)
	var saved Saved
	r.g.Persist = func(_ string, s Saved) { saved = s }
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	r.p.rep[r.npc.Region] = 4
	r.g.save(r.p)

	p2 := r.g.joinWith("hero2", func(protocol.Snapshot) {}, &saved)
	if len(p2.quests) != 1 || p2.quests[0].Title != "Les loups du bois" || p2.quests[0].Account != "hero2" || p2.rep[r.npc.Region] != 4 {
		t.Fatalf("après reconnexion: %+v %v", p2.quests, p2.rep)
	}
	if p2.nextQuestID <= p2.quests[0].ID {
		t.Fatal("identifiant réutilisable")
	}

	// Une quête ouverte depuis plus d'une semaine expire à la reconnexion.
	r.g.Clock = func() time.Time { return time.Now().Add(quest.Lifetime + time.Hour) }
	p3 := r.g.joinWith("hero3", func(protocol.Snapshot) {}, &saved)
	if len(p3.quests) != 0 || !p3.questsDirty {
		t.Fatalf("la quête devait expirer: %+v", p3.quests)
	}
}

func TestAbandonAndTamperedState(t *testing.T) {
	r, ft := giverRig(t)
	r.propose(ft, wolfProposal())
	r.act(protocol.NPCQuestAccept)
	id := r.p.quests[0].ID
	r.g.abandonQuest(r.p, id+99) // inconnue : sans effet
	if len(r.p.quests) != 1 {
		t.Fatal("quête inconnue abandonnée")
	}
	r.g.abandonQuest(r.p, id)
	if len(r.p.quests) != 0 {
		t.Fatal("abandon sans effet")
	}

	// Un état sauvegardé incohérent est ramené à la raison.
	sv := Saved{Stats: r.p.stats, Quests: []quest.Quest{
		{ID: 5, State: quest.Active, Progress: 99, Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 3}, CreatedAt: time.Now()},
		{ID: 0, State: quest.Active},
		{ID: 6, State: quest.Abandoned},
	}, Rep: map[world.RegionPos]int{{X: 1, Y: 1}: -4}}
	p := r.g.joinWith("autre", func(protocol.Snapshot) {}, &sv)
	if len(p.quests) != 1 || p.quests[0].State != quest.Ready || p.quests[0].Progress != 3 || len(p.rep) != 0 {
		t.Fatalf("%+v %v", p.quests, p.rep)
	}
}

func speciesByKind(t *testing.T, kind string) species {
	t.Helper()
	for _, s := range speciesList {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("espèce %s inconnue", kind)
	return species{}
}

func TestAmbiguousPlaceNamesAreNeverOffered(t *testing.T) {
	r, _ := giverRig(t)
	ctx := r.g.offerContext(r.p, r.npc)
	if ctx == nil {
		t.Fatal("pas d'offre")
	}
	// Les noms proposés désignent chacun un seul lieu : pas de clé en double possible dans la table,
	// et aucun nom n'est partagé par deux structures à portée.
	seen := map[string]world.RegionPos{}
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			rp := world.RegionPos{X: r.npc.Region.X + dx, Y: r.npc.Region.Y + dy}
			if s := r.g.World.StructureOfRegion(rp); s != nil && rp != r.npc.Region {
				name := lowerName(s.NameIn(r.p.lang))
				if prev, dup := seen[name]; dup {
					if _, offered := ctx.Quest.Places[name]; offered {
						t.Fatalf("%q proposé alors que %v et %v le portent", name, prev, rp)
					}
				}
				seen[name] = rp
			}
		}
	}
}

func TestAskGivesTheNPCTheRegionName(t *testing.T) {
	r, ft := giverRig(t)
	r.act(protocol.NPCTalk)
	want := r.g.World.TerritoryAt(r.npc.Pos.X, r.npc.Pos.Y).NameIn(r.p.lang)
	if got := ft.req(0).Region; got == "" || got != want {
		t.Fatalf("contrée transmise : %q, attendue %q", got, want)
	}
}

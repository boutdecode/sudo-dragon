package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// giverSpots renvoie le villageois donneur de quêtes du village de départ et deux accès libres.
func giverSpots(t *testing.T) (world.NPC, []spot) {
	t.Helper()
	w := world.New(1)
	for _, s := range w.StructuresNear(w.FindSpawn(), 300) {
		for _, n := range s.NPCs {
			if n.Kind != world.NPCVillager || !n.GivesQuests {
				continue
			}
			var spots []spot
			for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				p := world.Pos{X: n.Pos.X - d[0], Y: n.Pos.Y - d[1]}
				if _, isNPC := w.NPCAt(p.X, p.Y); w.At(p.X, p.Y).Walkable() && !isNPC {
					spots = append(spots, spot{p, d[0], d[1]})
				}
			}
			if len(spots) >= 2 {
				return n, spots
			}
		}
	}
	t.Skip("pas de villageois donneur avec deux accès libres près du départ")
	return world.NPC{}, nil
}

// joinChar est join avec un personnage complet (quêtes, réputation…).
func (e *talkEnv) joinChar(t *testing.T, name string, sp spot, ch store.Character) *player {
	t.Helper()
	_, _ = e.be.Store.CreateAccount(name, storetest.Password)
	ch.X, ch.Y = sp.stand.X, sp.stand.Y
	e.be.SaveCharacter(name, ch)
	var c *websocket.Conn
	waitUntil(t, "connexion de "+name, func() bool {
		c = dial(t, e.url)
		send(t, c, protocol.TLogin, signIn(name, storetest.Password))
		return recv(t, c).T == protocol.TWelcome
	})
	return &player{t: t, name: name, c: c, sp: sp}
}

// book lit les messages jusqu'à un livre de quêtes satisfaisant pred.
func (p *player) book(what string, pred func(protocol.Quests) bool) protocol.Quests {
	p.t.Helper()
	for range 80 {
		env := recv(p.t, p.c)
		if env.T != protocol.TQuests {
			continue
		}
		var q protocol.Quests
		if err := env.Payload(&q); err != nil {
			p.t.Fatal(err)
		}
		if pred(q) {
			return q
		}
	}
	p.t.Fatalf("%s %s: livre de quêtes attendu non reçu", p.name, what)
	return protocol.Quests{}
}

type seenPrompts struct {
	mu sync.Mutex
	s  []string
}

func (sp *seenPrompts) add(v string) { sp.mu.Lock(); sp.s = append(sp.s, v); sp.mu.Unlock() }
func (sp *seenPrompts) any(sub string) bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for _, x := range sp.s {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

func TestQuestProposalAcceptanceAndPersistenceEndToEnd(t *testing.T) {
	e := startTalkServer(t)
	npc, spots := giverSpots(t)
	e.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		last := req.Messages[len(req.Messages)-1].Content
		out := map[string]any{"reply": "Je vous écoute.", "intro": nil, "memory": ""}
		switch {
		case strings.HasPrefix(last, "(Le voyageur s'approche"):
			out["reply"] = "Bonjour."
		case strings.Contains(last, "travail"):
			out["reply"] = "Justement, il me faudrait de l'aide."
			out["quest"] = map[string]any{
				"title": "Les loups du bois", "story": "Ils rôdent près des champs.",
				"objective": map[string]any{"type": "kill", "species": "wolf", "count": 3},
				"reward":    map[string]any{"gold": 1000000, "xp": 1000000, "potions": 99, "note": "Merci"},
			}
		}
		b, _ := json.Marshal(out)
		return llm.Response{Content: string(b), Usage: llm.Usage{Total: 50}}, nil
	}

	a := e.joinChar(t, "aldric", spots[0], store.Character{Level: 5, HP: 30, LLMConsent: true})
	a.approach()
	a.startChat(false)
	a.act(protocol.NPCSay, "Avez-vous du travail pour moi ?")
	card := a.until("proposition", func(d protocol.NPCDialog) bool { return d.Mode == protocol.NPCModeQuest })
	p := card.Proposal
	if p == nil || p.Title != "Les loups du bois" || p.Need != 3 {
		t.Fatalf("proposition: %+v", card)
	}
	if p.Gold > 60 || p.XP > 7 || p.Potions > 2 {
		t.Fatalf("récompense non plafonnée par le serveur: %+v", p)
	}
	if !hasOffer(card, protocol.NPCQuestAccept) || !hasOffer(card, protocol.NPCQuestRefuse) {
		t.Fatalf("offres: %+v", card.Offers)
	}

	a.act(protocol.NPCQuestAccept, "")
	b := a.book("quête acceptée", func(q protocol.Quests) bool { return len(q.Open) == 1 })
	if q := b.Open[0]; q.State != "active" || q.Need != 3 || q.Gold != p.Gold || q.Giver != npc.Name {
		t.Fatalf("livre: %+v (PNJ %s)", b.Open[0], npc.Name)
	}
	a.until("retour à la discussion", func(d protocol.NPCDialog) bool { return d.Mode == protocol.NPCModeChat })

	// Elle est en base, et revient à la reconnexion.
	waitUntil(t, "quête enregistrée", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && len(ch.Quests) == 1 && ch.Quests[0].Title == "Les loups du bois"
	})
	a.c.Close(websocket.StatusNormalClosure, "")
	a2 := e.joinChar(t, "aldric", spots[0], func() store.Character { ch, _ := e.be.LoadCharacter("aldric"); return *ch }())
	a2.book("quête après reconnexion", func(q protocol.Quests) bool { return len(q.Open) == 1 && q.Open[0].Title == "Les loups du bois" })
}

func TestQuestTurnInGivesRewardReputationAndFeedsTheLocalLore(t *testing.T) {
	e := startTalkServer(t)
	npc, spots := giverSpots(t)
	prompts := &seenPrompts{}
	e.fake.ChatFunc = func(_ context.Context, req llm.Request) (llm.Response, error) {
		prompts.add(req.Messages[0].Content)
		b, _ := json.Marshal(map[string]any{"reply": "Hm.", "intro": nil, "memory": ""})
		return llm.Response{Content: string(b), Usage: llm.Usage{Total: 50}}, nil
	}

	ready := quest.Quest{
		ID: 1, Giver: npc.Key(), Title: "Les loups du bois", Story: "Ils rôdent.", State: quest.Ready, Progress: 3, Tier: 2,
		Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 3}, Reward: quest.Reward{Gold: 40, XP: 2, Potions: 1},
		CreatedAt: time.Now().Add(-time.Hour),
	}
	a := e.joinChar(t, "aldric", spots[0], store.Character{Level: 5, HP: 30, Gold: 10, Quests: []quest.Quest{ready}, QuestsDirty: true})
	menu := a.approach()
	var turn *protocol.NPCOffer
	for i := range menu.Offers {
		if menu.Offers[i].Action == protocol.NPCTurnIn {
			turn = &menu.Offers[i]
		}
	}
	if turn == nil || turn.Arg != "1" || !strings.Contains(turn.Label, "Les loups du bois") {
		t.Fatalf("offre de remise absente: %+v", menu.Offers)
	}
	a.act(protocol.NPCTurnIn, turn.Arg)
	// Le livre arrive avant le dialogue mis à jour.
	b := a.book("réputation", func(q protocol.Quests) bool { return len(q.Standing) == 1 && len(q.History) == 1 })
	if b.Standing[0].Points != 3 || len(b.Open) != 0 {
		t.Fatalf("livre: %+v", b)
	}
	done := a.until("remise", func(d protocol.NPCDialog) bool { return d.Gold == 50 })
	if !strings.Contains(strings.Join(done.Log, " "), "Les loups du bois") {
		t.Fatalf("journal: %+v", done.Log)
	}
	waitUntil(t, "remise enregistrée", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && ch.Gold == 50 && ch.Rep[npc.Region] == 3 && len(ch.Quests) == 1 && ch.Quests[0].State == quest.Done
	})

	// Rendre une seconde fois ne rapporte rien.
	a.act(protocol.NPCTurnIn, "1")
	time.Sleep(100 * time.Millisecond)
	if ch, _ := e.be.LoadCharacter("aldric"); ch.Gold != 50 {
		t.Fatalf("double paiement: %d", ch.Gold)
	}

	// Un autre joueur, dans le même lieu, entend parler de l'exploit — mais pas du reste.
	bren := e.joinChar(t, "brenna", spots[1], store.Character{Level: 3, HP: 20, LLMConsent: true})
	bren.approach()
	bren.startChat(false)
	if !prompts.any("Les loups du bois") || !prompts.any("aldric") {
		t.Fatal("la quête rendue par aldric devrait circuler dans le lieu")
	}
	if prompts.any(`"reputation_ici"`) && !prompts.any(`"palier":"Connu"`) {
		t.Fatal("la réputation de l'interlocuteur n'est pas celle d'aldric")
	}
}

func TestQuestAbandonFromTheBook(t *testing.T) {
	e := startTalkServer(t)
	npc, spots := giverSpots(t)
	q := quest.Quest{ID: 4, Giver: npc.Key(), Title: "À abandonner", Story: "x", State: quest.Active, Tier: 1,
		Objective: quest.Objective{Kind: quest.Kill, Species: "rat", Count: 3}, Reward: quest.Reward{Gold: 5}, CreatedAt: time.Now()}
	a := e.joinChar(t, "aldric", spots[0], store.Character{Level: 5, HP: 30, Quests: []quest.Quest{q}, QuestsDirty: true})
	a.book("quête chargée", func(b protocol.Quests) bool { return len(b.Open) == 1 })

	send(t, a.c, protocol.TQuestAction, protocol.QuestAction{Action: "autre", ID: 4}) // action inconnue : ignorée
	send(t, a.c, protocol.TQuestAction, protocol.QuestAction{Action: protocol.QuestAbandon, ID: 99})
	send(t, a.c, protocol.TQuestAction, protocol.QuestAction{Action: protocol.QuestAbandon, ID: 4})
	b := a.book("abandon", func(b protocol.Quests) bool { return len(b.Open) == 0 })
	if len(b.Notice) == 0 || !strings.Contains(b.Notice[0], "À abandonner") {
		t.Fatalf("notification: %+v", b.Notice)
	}
	waitUntil(t, "abandon enregistré", func() bool {
		ch, _ := e.be.LoadCharacter("aldric")
		return ch != nil && len(ch.Quests) == 0
	})
}

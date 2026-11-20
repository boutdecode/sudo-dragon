package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

func questsMsg(t *testing.T, b protocol.Quests) eventMsg {
	t.Helper()
	data, err := protocol.Encode(protocol.TQuests, b)
	if err != nil {
		t.Fatal(err)
	}
	env, err := protocol.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return eventMsg{Event{Env: env}}
}

func sampleBook() protocol.Quests {
	return protocol.Quests{
		Open: []protocol.QuestInfo{
			{ID: 7, Title: "Les loups du bois", Story: "Ils rôdent près des champs.", Giver: "Doran le Rieur", Place: "Tourelle",
				Goal: "Vaincre : Loup × 3", Progress: 1, Need: 3, Gold: 40, XP: 6, Potions: 1, State: "active", Gain: 3},
			{ID: 9, Title: "Un message", Story: "Portez-le.", Giver: "Osric", Place: "Tourelle", Goal: "Atteindre : Solmont",
				Progress: 1, Need: 1, Gold: 20, State: "ready", Gain: 2},
		},
		History:  []protocol.QuestInfo{{ID: 3, Title: "Les rats", Giver: "Doran", Place: "Tourelle", State: "done"}},
		Standing: []protocol.Standing{{Place: "Tourelle", Points: 5, Tier: "Connu", Next: 3}},
	}
}

func withBook(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(questsMsg(t, sampleBook()))
	return tm.(Model)
}

func sentQuestActions(out chan []byte) (acts []protocol.QuestAction) {
	for {
		select {
		case data := <-out:
			env, _ := protocol.Decode(data)
			if env.T == protocol.TQuestAction {
				var a protocol.QuestAction
				_ = env.Payload(&a)
				acts = append(acts, a)
			}
		default:
			return
		}
	}
}

func TestQuestBookShowsQuestsAndSidebarSummary(t *testing.T) {
	m := withBook(t, playingModel(t, 120, 36))
	if side := ansi.Strip(m.View().Content); !strings.Contains(side, "Quêtes : 2 (1 à rendre)") {
		t.Fatalf("summary missing from the panel:\n%s", side)
	}
	m = press(m, "q")
	out := ansi.Strip(m.View().Content)
	for _, want := range []string{"Livre de quêtes", "Les loups du bois", "Un message", "Vaincre : Loup × 3", "Progression : 1/3",
		"Récompense : 40 or · 6 XP · 1 potion(s)", "Doran le Rieur", "Réputation : +3"} {
		if !strings.Contains(out, want) {
			t.Errorf("book without %q:\n%s", want, out)
		}
	}
	m = press(m, "down")
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "Prête !") {
		t.Errorf("the ready quest should prompt to hand it in:\n%s", out)
	}
	m = press(m, "tab")
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "Connu · 5") || !strings.Contains(out, "Les rats") {
		t.Errorf("reputation tab:\n%s", out)
	}
	m = press(m, "esc")
	if m.questBook {
		t.Fatal("Escape should close the book")
	}
}

func TestQuestAbandonNeedsConfirmation(t *testing.T) {
	m, out := withConn(playingModel(t, 120, 36))
	m = withBook(t, m)
	m = press(m, "q")
	m = press(m, "x")
	if acts := sentQuestActions(out); len(acts) != 0 {
		t.Fatalf("abandon without confirmation: %v", acts)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Appuyez de nouveau") {
		t.Fatal("confirmation should be asked")
	}
	m = press(m, "j") // other key: cancels
	m = press(m, "x")
	m = press(m, "x")
	acts := sentQuestActions(out)
	if len(acts) != 1 || acts[0].Action != protocol.QuestAbandon || acts[0].ID != 9 {
		t.Fatalf("abandon of quest 9 (selected) expected: %v", acts)
	}
}

func TestQuestBookFitsAllSizesAndIsSanitized(t *testing.T) {
	hostile := sampleBook()
	hostile.Open[0].Title = "Loups\x1b[2J\x1b]0;pwn\x07 \u202edu bois"
	hostile.Open[0].Story = strings.Repeat("très long ", 80) + "\x1b[31m"
	hostile.Notice = []string{"Quête \x1b[2J accomplie"}
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {120, 36}, {200, 60}} {
		tm, _ := playingModel(t, sz[0], sz[1]).Update(questsMsg(t, hostile))
		m := press(tm.(Model), "q")
		view := m.View().Content
		if strings.Contains(view, "pwn") && strings.Contains(view, "\x1b]0;") {
			t.Fatalf("%v: OSC sequence not neutralized", sz)
		}
		lines := strings.Split(ansi.Strip(view), "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines > height", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v line %d: width %d > %d", sz, i, w, sz[0])
			}
		}
		if strings.ContainsAny(ansi.Strip(view), "\u202e") {
			t.Errorf("%v: bidirectional mark kept", sz)
		}
	}
	// The notification goes into the log, cleaned.
	tm, _ := playingModel(t, 120, 36).Update(questsMsg(t, hostile))
	last := tm.(Model).chat[len(tm.(Model).chat)-1]
	if strings.Contains(last.Text, "\x1b") || !strings.Contains(last.Text, "accomplie") {
		t.Fatalf("notification: %q", last.Text)
	}
}

func questProposal() protocol.NPCDialog {
	return protocol.NPCDialog{
		Kind: "villager", Name: "Doran le Rieur", Place: "Tourelle", Mode: protocol.NPCModeQuest,
		Proposal: &protocol.QuestInfo{Title: "Les loups du bois", Story: "Ils rôdent.", Goal: "Vaincre : Loup × 3", Need: 3, Gold: 40, XP: 6, Potions: 1, Gain: 3, State: "active"},
		Offers: []protocol.NPCOffer{
			{Action: protocol.NPCQuestAccept, Label: "Accepter la quête", Available: true},
			{Action: protocol.NPCQuestRefuse, Label: "Refuser", Available: true},
		},
	}
}

func TestQuestProposalCardAndKeys(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	m = openNPC(t, m, questProposal())
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Proposition de quête", "Les loups du bois", "Récompense : 40 or · 6 XP · 1 potion(s)", "[A] Accepter la quête", "[R] Refuser"} {
		if !strings.Contains(view, want) {
			t.Errorf("card without %q:\n%s", want, view)
		}
	}
	m = press(m, "enter") // Enter never accepts
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("Enter sent %v", got)
	}
	m = press(m, "a")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCQuestAccept {
		t.Fatalf("accept: %v", got)
	}
	m = press(m, "r")
	m = press(m, "esc")
	if got := sentNPCActions(out); len(got) != 2 || got[0] != protocol.NPCQuestRefuse || got[1] != protocol.NPCQuestRefuse {
		t.Fatalf("decline: %v", got)
	}
}

func TestTurnInOffersUseNumberKeys(t *testing.T) {
	m, out := withConn(playingModel(t, 110, 34))
	d := healerDialog()
	d.Kind = "villager"
	d.Offers = []protocol.NPCOffer{
		{Action: protocol.NPCTurnIn, Label: "Rendre : Les loups", Arg: "7", Available: true},
		{Action: protocol.NPCTurnIn, Label: "Rendre : Un message", Arg: "9", Available: true},
	}
	m = openNPC(t, m, d)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "[1] Rendre : Les loups") || !strings.Contains(view, "[2] Rendre : Un message") {
		t.Fatalf("hand-ins badly numbered:\n%s", view)
	}
	m = press(m, "2")
	data := <-out
	env, _ := protocol.Decode(data)
	var a protocol.NPCAction
	_ = env.Payload(&a)
	if a.Action != protocol.NPCTurnIn || a.Text != "9" {
		t.Fatalf("hand-in: %+v", a)
	}
	press(m, "3") // no third quest
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("nonexistent hand-in sent: %v", got)
	}
}

func TestQuestsResetOnReconnect(t *testing.T) {
	m := withBook(t, playingModel(t, 120, 36))
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	tm, _ = tm.Update(connectedMsg{welcome: protocol.Welcome{ID: 1, Name: "bob", Seed: 1}})
	if got := tm.(Model); len(got.quests.Open) != 0 || got.questBook {
		t.Fatalf("the book from a previous session stayed: %+v", got.quests)
	}
}

func TestGiverIdleFollowsOpenQuestsPausesAndTheCap(t *testing.T) {
	a := world.NPCKey{Region: world.RegionPos{X: 1, Y: 2}, Index: 3}
	b := world.NPCKey{Region: world.RegionPos{X: 4, Y: 5}, Index: 0}
	var m Model
	m.questsAt = time.Now()
	if m.giverIdle(a) {
		t.Fatal("without a quest, nobody is out of work")
	}
	m.quests.Open = []protocol.QuestInfo{{GiverRX: 1, GiverRY: 2, GiverIdx: 3}}
	if !m.giverIdle(a) || m.giverIdle(b) {
		t.Fatal("only the giver of an open quest is out of work")
	}
	m.quests.Open = nil
	m.quests.Pauses = []protocol.GiverPause{{RX: 4, RY: 5, Idx: 0, Secs: 60}}
	if !m.giverIdle(b) {
		t.Fatal("giver on pause")
	}
	m.questsAt = time.Now().Add(-2 * time.Minute)
	if m.giverIdle(b) {
		t.Fatal("the pause is over: the \"!\" must come back without a new message")
	}
	m.quests.Full = true
	if !m.giverIdle(a) || !m.giverIdle(b) {
		t.Fatal("quota reached: nobody has work")
	}
}

// A giver who hates the player's race loses their "!" (same deterministic draw as the server).
func TestGiverIdleForAHatedRace(t *testing.T) {
	w := testWorld(3)
	for y := -12; y <= 12; y++ {
		for x := -12; x <= 12; x++ {
			s := w.StructureOfRegion(world.RegionPos{X: x, Y: y})
			if s == nil {
				continue
			}
			for _, n := range s.NPCs {
				if !n.GivesQuests {
					continue
				}
				for _, r := range hero.Races {
					if n.HatredOf(r) < world.HateNoQuest {
						continue
					}
					m := Model{world: w, questsAt: time.Now()}
					m.bag.Race = string(r)
					if !m.giverIdle(n.Key()) {
						t.Fatalf("%s hates the %s but keeps their \"!\"", n.Name, r)
					}
					m.bag.Race = string(n.Race)
					if m.giverIdle(n.Key()) {
						t.Fatalf("%s is idle for their own race", n.Name)
					}
					return
				}
			}
		}
	}
	t.Fatal("no hateful giver found")
}

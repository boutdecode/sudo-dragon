package server

import (
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/store"
)

func readCreate(t *testing.T, c *websocket.Conn) protocol.Create {
	t.Helper()
	env := recvRaw(t, c)
	if env.T != protocol.TCreate {
		t.Fatalf("TCreate expected, got %q", env.T)
	}
	var cr protocol.Create
	if err := env.Payload(&cr); err != nil {
		t.Fatal(err)
	}
	return cr
}

func TestNewAccountRollsRerollsAtMostThreeTimesAndChoosesAClass(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	c := dial(t, url)
	send(t, c, protocol.TLogin, reg("alice"))

	cr := readCreate(t, c)
	if len(cr.Scores) != 6 || cr.Left != 3 || len(cr.Classes) != 3 || cr.Level != 1 {
		t.Fatalf("initial roll: %+v", cr)
	}
	for _, v := range cr.Scores {
		if v < 3 || v > 18 {
			t.Fatalf("score outside 3-18: %v", cr.Scores)
		}
	}
	dice := map[string]int{}
	for _, ci := range cr.Classes {
		dice[ci.ID] = ci.Die
	}
	if dice["warrior"] != 12 || dice["rogue"] != 8 || dice["mage"] != 6 {
		t.Fatalf("hit dice: %v", dice)
	}

	// An invalid index costs nothing.
	send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateReroll, Index: 9})
	if again := readCreate(t, c); again.Left != 3 {
		t.Fatalf("invalid reroll counted: %+v", again)
	}

	prev := cr
	for left := 2; left >= 0; left-- {
		send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateReroll, Index: 3})
		next := readCreate(t, c)
		if next.Left != left {
			t.Fatalf("rerolls left %d, expected %d", next.Left, left)
		}
		for i := range next.Scores {
			if i != 3 && next.Scores[i] != prev.Scores[i] {
				t.Fatalf("ability %d changed", i)
			}
		}
		prev = next
	}
	send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateReroll, Index: 0})
	last := readCreate(t, c)
	if last.Left != 0 || last.Scores[0] != prev.Scores[0] {
		t.Fatalf("a 4th reroll was accepted: %+v", last)
	}

	// An unknown class gives the roll again without saving anything.
	send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "paladin"})
	readCreate(t, c)
	send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "mage"})
	recvType(t, c, protocol.TWelcome)

	var sc hero.Scores
	copy(sc[:], last.Scores)
	// The character is saved on the player's departure, with their starting weapon equipped (mage's staff).
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "created character saved", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.Class == "mage" && hero.Scores(ch.Scores) == sc && ch.MaxHP == hero.FirstHP(hero.Mage, sc) && ch.Potions == 2 && ch.Level == 1 &&
			len(ch.Gear.Items) == 1 && ch.Gear.Items[0].Family == "staff" && ch.Gear.Worn["weapon"] == ch.Gear.Items[0].ID
	})

	// The same account no longer goes through creation.
	c2, w := signInEventually(t, url, "alice")
	defer c2.Close(websocket.StatusNormalClosure, "")
	if w.Name != "alice" {
		t.Fatalf("welcome: %+v", w)
	}
	if hp := readSnapshot(t, c2).Stats; hp.MaxHP != hero.FirstHP(hero.Mage, sc) {
		t.Fatalf("HP after reconnection: %d", hp.MaxHP)
	}
}

func TestDroppingDuringCreationSavesNothingAndRollsAgainNextTime(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	c := dial(t, url)
	send(t, c, protocol.TLogin, reg("alice"))
	readCreate(t, c)
	c.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond)
	if ch, _ := be.LoadCharacter("alice"); ch != nil {
		t.Fatalf("character saved before confirmation: %+v", ch)
	}
	c = dial(t, url)
	send(t, c, protocol.TLogin, signIn("alice", testPass))
	if cr := readCreate(t, c); cr.Left != 3 {
		t.Fatalf("new roll expected: %+v", cr)
	}
}

func TestExistingCharacterWithoutSheetKeepsItsProgress(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")

	sp := spawn()
	giver, _ := giverSpots(t)
	q := quest.Quest{ID: 2, Giver: giver.Key(), Title: "Les loups", Story: "x", State: quest.Active, Tier: 1,
		Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 3}, Reward: quest.Reward{Gold: 5}, CreatedAt: time.Now()}
	old := store.Character{Level: 6, XP: 4, Potions: 3, HP: 20, X: sp.X, Y: sp.Y, Gold: 250, Kills: 12, Quests: []quest.Quest{q}, QuestsDirty: true, GearDirty: true} // GearDirty: empties the bag left by register
	be.SaveCharacter("alice", old)                                                                                                                                    // no sheet: creation is still to be done

	c := dial(t, url)
	send(t, c, protocol.TLogin, signIn("alice", testPass))
	cr := readCreate(t, c)
	if cr.Level != 6 {
		t.Fatalf("kept level expected: %+v", cr)
	}
	send(t, c, protocol.TCreateAction, protocol.CreateAction{Action: protocol.CreateConfirm, Class: "rogue"})
	var w protocol.Welcome
	if err := recvType(t, c, protocol.TWelcome).Payload(&w); err != nil {
		t.Fatal(err)
	}
	if w.X != sp.X || w.Y != sp.Y {
		t.Fatalf("position lost: (%d,%d)", w.X, w.Y)
	}
	st := readSnapshot(t, c).Stats
	if st.Level != 6 || st.Gold != 250 || st.Potions != 3 {
		t.Fatalf("progress lost: %+v", st)
	}
	// 6 levels of d8: between 6 and 8+5*8 (CON >= 3 gives at least 1 per level).
	if st.MaxHP < 6 || st.MaxHP > 8+4+5*(8+4) || st.HP != st.MaxHP {
		t.Fatalf("HP recomputed: %d/%d", st.HP, st.MaxHP)
	}
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "sheet saved with the progress", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.Class == "rogue" && ch.Gold == 250 && len(ch.Quests) == 1 && ch.Kills == 12 && len(ch.Gear.Items) == 1 && ch.Gear.Items[0].Family == "shortbow"
	})
}

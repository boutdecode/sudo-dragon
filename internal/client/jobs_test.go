package client

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

func TestBagShowsEveryJobEvenWhenEmpty(t *testing.T) {
	m := Model{}
	m.applyBag(protocol.Bag{Capacity: 24, Jobs: []protocol.JobInfo{
		{Job: "wood", Level: 2, Used: 3, Cap: 8, Chance: 46},
		{Job: "ore", Level: 1, Cap: 5, Chance: 40},
		{Job: "herb", Level: 1, Cap: 5, Chance: 40},
		{Job: "fish", Level: 1, Cap: 5, Chance: 40},
	}, Resources: []protocol.ResourceStack{{ID: "oak", Qty: 3}}})
	out := strings.Join(m.resourceLines(), "\n")
	for _, want := range []string{"3/8", "0/5"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from the materials list:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "/5"); n != 3 {
		t.Errorf("%d empty jobs displayed, expected 3", n)
	}
}

func TestApplyBagClampsJobs(t *testing.T) {
	m := Model{}
	m.applyBag(protocol.Bag{Jobs: []protocol.JobInfo{{Job: "wood", Level: -4, Chance: 900, Used: -1, Cap: 1 << 30}}})
	j := m.bag.Jobs[0]
	if j.Level != 1 || j.Chance != 100 || j.Used != 0 || j.Cap != 9999 {
		t.Errorf("unbounded job: %+v", j)
	}
}

func TestBagShowsToolAndYield(t *testing.T) {
	m := Model{}
	m.applyBag(protocol.Bag{Jobs: []protocol.JobInfo{
		{Job: "wood", Level: 4, Cap: 14, Tool: 2, Yield: 2},
		{Job: "ore", Level: 1, Cap: 5},
		{Job: "herb", Level: 1, Cap: 5},
		{Job: "fish", Level: 1, Cap: 5, Yield: 1},
	}})
	out := strings.Join(m.resourceLines(), "\n")
	if !strings.Contains(out, m.t("tool.wood.2")) || !strings.Contains(out, "×2") {
		t.Errorf("owned tool missing:\n%s", out)
	}
	if n := strings.Count(out, m.t("tool.none")); n != 2 {
		t.Errorf("%d trades without a tool displayed, expected 2 (fishing needs none):\n%s", n, out)
	}
}

func TestApplyBagClampsTool(t *testing.T) {
	m := Model{}
	m.applyBag(protocol.Bag{Jobs: []protocol.JobInfo{{Job: "wood", Level: 1, Tool: 99, Yield: 1 << 30}}})
	if j := m.bag.Jobs[0]; j.Tool != 4 || j.Yield != 99 {
		t.Errorf("unbounded tool: %+v", j)
	}
}

// Tool offers are chosen by a digit, like equipment.
func TestToolOffersAreNumbered(t *testing.T) {
	m := Model{}
	m.npc = &protocol.NPCDialog{Offers: []protocol.NPCOffer{
		{Action: protocol.NPCBuyTool, Label: "a", Arg: "1", Available: true},
		{Action: protocol.NPCBuyTool, Label: "b", Arg: "2"},
	}}
	if n := len(m.numberedOffers()); n != 2 {
		t.Fatalf("%d numbered offers, expected 2", n)
	}
	if k := m.npcOfferKey(m.npc.Offers[1], 1); k != "2" {
		t.Errorf("key %q, expected 2", k)
	}
}

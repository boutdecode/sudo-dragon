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

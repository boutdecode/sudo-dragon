package lore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

var base = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func rp(x, y int) world.RegionPos { return world.RegionPos{X: x, Y: y} }

// clock is an adjustable clock.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

type env struct {
	t     *testing.T
	st    *store.Store
	book  *lore.Book
	clk   *clock
	ctx   context.Context
	moder lore.Moderator
	mod   [][]string // batches of texts submitted to moderation
}

func newEnv(t *testing.T, accounts ...string) *env {
	t.Helper()
	be := storetest.NewBackend(t, time.Hour)
	e := &env{t: t, st: be.Store, clk: &clock{t: base}, ctx: context.Background()}
	e.book = &lore.Book{Repo: be.Store, Now: e.clk.Now}
	e.moder = func(_ context.Context, texts []string) error {
		e.mod = append(e.mod, append([]string(nil), texts...))
		return nil
	}
	for _, a := range accounts {
		if _, err := be.Store.CreateAccount(a, storetest.Password); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func intro(alias, role string) lore.Intro { return lore.Intro{Alias: alias, Role: role} }

func (e *env) remember(account string, at world.RegionPos, in lore.Intro) (lore.Card, error) {
	return e.book.Remember(e.ctx, account, at, in, lore.Facts{Level: 5}, e.moder)
}

func (e *env) heard(at world.RegionPos, speaker string) []lore.Heard {
	e.t.Helper()
	h, err := e.book.Heard(e.ctx, at, speaker)
	if err != nil {
		e.t.Fatal(err)
	}
	return h
}

func TestRememberThenHeardLocallyAndRemotely(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	if _, err := e.remember("aldric", rp(0, 0), lore.Intro{Alias: "Aldric le Rouge", Role: "forgeron", Origin: "le Nord", Goal: "trouver son frère"}); err != nil {
		t.Fatal(err)
	}

	// On the spot, immediately and in full.
	got := e.heard(rp(0, 0), "brenna")
	if len(got) != 1 || got[0].Account != "aldric" || !got[0].Local() || got[0].Intro.Goal != "trouver son frère" {
		t.Fatalf("place of origin: %+v", got)
	}

	// One region away: nothing before 1 hour, then everything except deaths.
	if got := e.heard(rp(1, 0), "brenna"); len(got) != 0 {
		t.Fatalf("the rumor arrived instantly: %+v", got)
	}
	e.clk.Advance(lore.HopDelay)
	got = e.heard(rp(1, 0), "brenna")
	if len(got) != 1 || got[0].Distance != 1 {
		t.Fatalf("after the delay: %+v", got)
	}
	if got[0].Intro.Role != "forgeron" || got[0].Intro.Origin != "le Nord" || got[0].Intro.Goal != "trouver son frère" {
		t.Fatalf("fidelity at distance 1: %+v", got[0].Intro)
	}

	// Outside the radius, never.
	e.clk.Advance(48 * time.Hour)
	if got := e.heard(rp(lore.RumorRadius+1, 0), "brenna"); len(got) != 0 {
		t.Fatalf("outside the radius: %+v", got)
	}
	// And everything is forgotten at the end of the forgetting period.
	e.clk.Advance(lore.Forgetting)
	if got := e.heard(rp(0, 0), "brenna"); len(got) != 0 {
		t.Fatalf("after forgetting: %+v", got)
	}
}

func TestSpeakerCardIsFlaggedSelf(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	_, _ = e.remember("aldric", rp(0, 0), intro("Aldric", "forgeron"))
	_, _ = e.remember("brenna", rp(0, 0), intro("Brenna", "archère"))
	got := e.heard(rp(0, 0), "Aldric")
	selves := 0
	for _, h := range got {
		if h.Self {
			selves++
			if h.Account != "aldric" {
				t.Fatalf("Self sur %q", h.Account)
			}
		}
	}
	if len(got) != 2 || selves != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestRememberMergesFieldsAndKeepsCreationDate(t *testing.T) {
	e := newEnv(t, "aldric")
	first, err := e.remember("aldric", rp(0, 0), lore.Intro{Alias: "Aldric", Role: "forgeron", Origin: "le Nord"})
	if err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(time.Hour)
	second, err := e.remember("aldric", rp(0, 0), lore.Intro{Goal: "trouver son frère", Role: "maître forgeron"})
	if err != nil {
		t.Fatal(err)
	}

	if second.Alias != "Aldric" || second.Origin != "le Nord" {
		t.Fatalf("fields not given again were erased: %+v", second.Intro)
	}
	if second.Role != "maître forgeron" || second.Goal != "trouver son frère" {
		t.Fatalf("fields given again were not updated: %+v", second.Intro)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("dates: creation %v (expected %v), update %v", second.CreatedAt, first.CreatedAt, second.UpdatedAt)
	}
	stored, found, _ := e.st.GetCard(e.ctx, "aldric", rp(0, 0))
	if !found || stored.Goal != "trouver son frère" || stored.Alias != "Aldric" {
		t.Fatalf("stored card: %+v", stored)
	}
}

func TestRememberRejections(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")

	cases := []struct {
		name    string
		account string
		in      lore.Intro
		want    error
	}{
		{"empty", "aldric", lore.Intro{}, lore.ErrEmptyIntro},
		{"link", "aldric", lore.Intro{Role: "voir https://exemple.fr"}, lore.ErrUnsafeText},
		{"address", "aldric", lore.Intro{Trait: "moi@mail.fr"}, lore.ErrUnsafeText},
		{"impersonation", "aldric", lore.Intro{Alias: "Brenna"}, lore.ErrImpersonation},
		{"impersonation, other case", "aldric", lore.Intro{Alias: "BRENNA"}, lore.ErrImpersonation},
	}
	for _, c := range cases {
		if _, err := e.remember(c.account, rp(0, 0), c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.want)
		}
	}
	if cards, _ := e.st.ListCards(e.ctx, "", 10); len(cards) != 0 {
		t.Fatalf("refused cards were written: %+v", cards)
	}

	// Their own nickname as alias is allowed (case included).
	if _, err := e.remember("aldric", rp(0, 0), lore.Intro{Alias: "ALDRIC"}); err != nil {
		t.Fatalf("alias identical to their own nickname: %v", err)
	}
}

func TestRememberModerationFailsClosed(t *testing.T) {
	e := newEnv(t, "aldric")
	boom := errors.New("moderation unavailable")
	e.moder = func(context.Context, []string) error { return boom }

	if _, err := e.remember("aldric", rp(0, 0), intro("Aldric", "forgeron")); !errors.Is(err, boom) {
		t.Fatalf("error: %v", err)
	}
	if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(0, 0)); found {
		t.Fatal("a card was written although moderation failed")
	}
	// No moderator at all: refusal as well.
	if _, err := e.book.Remember(e.ctx, "aldric", rp(0, 0), intro("Aldric", "forgeron"), lore.Facts{}, nil); err == nil {
		t.Fatal("write accepted without a moderator")
	}
}

func TestModerationReceivesTheCleanedMergedFields(t *testing.T) {
	e := newEnv(t, "aldric")
	_, _ = e.remember("aldric", rp(0, 0), lore.Intro{Alias: "Aldric", Role: "forgeron"})
	_, err := e.remember("aldric", rp(0, 0), lore.Intro{Goal: "  retrouver\x1b[31m son frère "})
	if err != nil {
		t.Fatal(err)
	}
	last := e.mod[len(e.mod)-1]
	joined := strings.Join(last, "|")
	if !strings.Contains(joined, "Aldric") || !strings.Contains(joined, "forgeron") || !strings.Contains(joined, "retrouver son frère") {
		t.Fatalf("moderated texts: %v", last)
	}
	if strings.Contains(joined, "\x1b") {
		t.Fatalf("escape sequence passed to moderation: %q", joined)
	}
}

func TestRememberUnknownAccount(t *testing.T) {
	e := newEnv(t)
	if _, err := e.remember("fantome", rp(0, 0), intro("Spectre", "rôdeur")); !errors.Is(err, store.ErrNoAccount) {
		t.Fatalf("error: %v", err)
	}
}

func TestCardsPerAccountAreCapped(t *testing.T) {
	e := newEnv(t, "aldric")
	for i := 0; i < lore.MaxCardsPerAccount+3; i++ {
		e.clk.Advance(time.Minute)
		if _, err := e.remember("aldric", rp(i, 0), intro("Aldric", "voyageur")); err != nil {
			t.Fatal(err)
		}
	}
	cards, _ := e.st.ListCards(e.ctx, "aldric", 100)
	if len(cards) != lore.MaxCardsPerAccount {
		t.Fatalf("%d cards, cap %d", len(cards), lore.MaxCardsPerAccount)
	}
	// The most recent ones remain.
	for i := 0; i < 3; i++ {
		if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(i, 0)); found {
			t.Errorf("the oldest card (place %d) should have been discarded", i)
		}
	}
	if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(lore.MaxCardsPerAccount+2, 0)); !found {
		t.Error("the most recent card disappeared")
	}
}

func TestRefreshFactsKeepsTheRumorClock(t *testing.T) {
	e := newEnv(t, "aldric")
	_, _ = e.remember("aldric", rp(0, 0), intro("Aldric", "forgeron"))
	_, _ = e.remember("aldric", rp(1, 0), intro("Aldric", "forgeron"))
	e.clk.Advance(30 * time.Minute)

	if err := e.book.RefreshFacts(e.ctx, "aldric", lore.Facts{Level: 20, BossKills: 4, Deaths: 1}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []world.RegionPos{rp(0, 0), rp(1, 0)} {
		c, _, _ := e.st.GetCard(e.ctx, "aldric", p)
		if c.Level != 20 || c.BossKills != 4 || c.Deaths != 1 {
			t.Errorf("place %v: facts not updated: %+v", p, c.Facts)
		}
		if !c.UpdatedAt.Equal(base) && !c.UpdatedAt.Equal(base.Add(0)) {
			t.Errorf("place %v: the update date moved (%v): the rumor would restart from zero", p, c.UpdatedAt)
		}
	}
}

func TestMemoriesArePrivatePerPlayerAndPerNPC(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	npcA := world.NPCKey{Region: rp(0, 0), Index: 0}
	npcB := world.NPCKey{Region: rp(0, 0), Index: 1}

	if err := e.book.Memorize(e.ctx, npcA, "aldric", "Il cherche son frère disparu."); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.book.Recall(e.ctx, npcA, "aldric"); got != "Il cherche son frère disparu." {
		t.Fatalf("memory: %q", got)
	}
	// Another player, or another NPC, sees nothing.
	if got, _ := e.book.Recall(e.ctx, npcA, "brenna"); got != "" {
		t.Fatalf("leak to another player: %q", got)
	}
	if got, _ := e.book.Recall(e.ctx, npcB, "aldric"); got != "" {
		t.Fatalf("leak to another NPC: %q", got)
	}
	// The memory gets replaced.
	_ = e.book.Memorize(e.ctx, npcA, "ALDRIC", "Il a retrouvé son frère.")
	if got, _ := e.book.Recall(e.ctx, npcA, "aldric"); got != "Il a retrouvé son frère." {
		t.Fatalf("replacement: %q", got)
	}
}

func TestMemorizeSanitizesAndIgnoresEmpty(t *testing.T) {
	e := newEnv(t, "aldric")
	npc := world.NPCKey{Region: rp(0, 0)}
	_ = e.book.Memorize(e.ctx, npc, "aldric", "Bon souvenir.")
	for _, bad := range []string{"", "  \n ", "écrivez à moi@mail.fr"} {
		if err := e.book.Memorize(e.ctx, npc, "aldric", bad); err != nil {
			t.Fatal(err)
		}
		if got, _ := e.book.Recall(e.ctx, npc, "aldric"); got != "Bon souvenir." {
			t.Fatalf("%q overwrote the memory: %q", bad, got)
		}
	}
	_ = e.book.Memorize(e.ctx, npc, "aldric", "A\x1b[31mB\nC "+strings.Repeat("x", 1000))
	got, _ := e.book.Recall(e.ctx, npc, "aldric")
	if strings.ContainsAny(got, "\x1b\n") || len([]rune(got)) > lore.MaxSummary {
		t.Fatalf("memory not cleaned: %q", got)
	}
	if err := e.book.Memorize(e.ctx, npc, "inconnu", "x"); !errors.Is(err, store.ErrNoAccount) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestPurgeByAccountAndByPlace(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	_, _ = e.remember("aldric", rp(0, 0), intro("Aldric", "forgeron"))
	_, _ = e.remember("aldric", rp(5, 5), intro("Aldric", "forgeron"))
	_, _ = e.remember("brenna", rp(0, 0), intro("Brenna", "archère"))
	_ = e.book.Memorize(e.ctx, world.NPCKey{Region: rp(0, 0)}, "aldric", "souvenir a")
	_ = e.book.Memorize(e.ctx, world.NPCKey{Region: rp(0, 0)}, "brenna", "souvenir b")

	cards, mems, _, err := e.st.PurgeAccount(e.ctx, "ALDRIC")
	if err != nil || cards != 2 || mems != 1 {
		t.Fatalf("account purge: %d cards, %d memories, %v", cards, mems, err)
	}
	if left, _ := e.st.ListCards(e.ctx, "", 10); len(left) != 1 || left[0].Account != "brenna" {
		t.Fatalf("after purge: %+v", left)
	}

	cards, mems, _, err = e.st.PurgePlace(e.ctx, rp(0, 0))
	if err != nil || cards != 1 || mems != 1 {
		t.Fatalf("place purge: %d cards, %d memories, %v", cards, mems, err)
	}
	if left, _ := e.st.ListCards(e.ctx, "", 10); len(left) != 0 {
		t.Fatalf("remaining: %+v", left)
	}
	if cards, mems, _, _ := e.st.PurgeAccount(e.ctx, "personne"); cards != 0 || mems != 0 {
		t.Fatal("purge of an account without data")
	}
}

func TestListCardsFiltersAndOrders(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	_, _ = e.remember("aldric", rp(0, 0), intro("A", "x"))
	e.clk.Advance(time.Minute)
	_, _ = e.remember("brenna", rp(1, 0), intro("B", "y"))
	all, _ := e.st.ListCards(e.ctx, "", 10)
	if len(all) != 2 || all[0].Account != "brenna" {
		t.Fatalf("sorting by recency: %+v", all)
	}
	if one, _ := e.st.ListCards(e.ctx, "aldric", 10); len(one) != 1 || one[0].Account != "aldric" {
		t.Fatalf("filter by account: %+v", one)
	}
	if lim, _ := e.st.ListCards(e.ctx, "", 1); len(lim) != 1 {
		t.Fatalf("limit: %+v", lim)
	}
}

func TestCardsNearBoxAndCutoff(t *testing.T) {
	e := newEnv(t, "a", "b", "c")
	_, _ = e.remember("a", rp(2, 2), intro("dedans", "x"))
	_, _ = e.remember("b", rp(4, 0), intro("dehors", "x")) // 4 away from the center (0,0)
	e.clk.Advance(time.Hour)
	_, _ = e.remember("c", rp(-3, 3), intro("bord", "x"))

	got, err := e.st.CardsNear(e.ctx, rp(0, 0), 3, base.Add(-time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Alias] = true
	}
	if !names["dedans"] || !names["bord"] || names["dehors"] {
		t.Fatalf("square of radius 3: %v", names)
	}
	// Cutoff by date.
	got, _ = e.st.CardsNear(e.ctx, rp(0, 0), 3, base.Add(30*time.Minute), 10)
	if len(got) != 1 || got[0].Alias != "bord" {
		t.Fatalf("cutoff by date: %+v", got)
	}
	// Limit.
	got, _ = e.st.CardsNear(e.ctx, rp(0, 0), 3, base.Add(-time.Minute), 1)
	if len(got) != 1 || got[0].Alias != "bord" {
		t.Fatalf("limit (most recent first): %+v", got)
	}
}

func TestNegativeRegionCoordinatesWork(t *testing.T) {
	e := newEnv(t, "aldric")
	if _, err := e.remember("aldric", rp(-7, -2), intro("Aldric", "forgeron")); err != nil {
		t.Fatal(err)
	}
	if got := e.heard(rp(-8, -1), "x"); len(got) != 0 {
		t.Fatalf("not arrived yet: %+v", got)
	}
	e.clk.Advance(lore.HopDelay)
	if got := e.heard(rp(-8, -1), "x"); len(got) != 1 {
		t.Fatalf("after the delay: %+v", got)
	}
}

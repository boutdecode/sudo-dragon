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

// clock est une horloge réglable.
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
	mod   [][]string // lots de textes soumis à la modération
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

	// Sur place, tout de suite et en entier.
	got := e.heard(rp(0, 0), "brenna")
	if len(got) != 1 || got[0].Account != "aldric" || !got[0].Local() || got[0].Intro.Goal != "trouver son frère" {
		t.Fatalf("lieu d'origine: %+v", got)
	}

	// Deux régions plus loin: rien avant 10 minutes, puis une version lacunaire.
	if got := e.heard(rp(2, 0), "brenna"); len(got) != 0 {
		t.Fatalf("la rumeur est arrivée instantanément: %+v", got)
	}
	e.clk.Advance(2 * lore.HopDelay)
	got = e.heard(rp(2, 0), "brenna")
	if len(got) != 1 || got[0].Distance != 2 {
		t.Fatalf("après le délai: %+v", got)
	}
	if got[0].Intro.Role != "forgeron" || got[0].Intro.Origin != "le Nord" || got[0].Intro.Goal != "" {
		t.Fatalf("fidélité à distance 2: %+v", got[0].Intro)
	}

	// Hors du rayon, jamais.
	e.clk.Advance(48 * time.Hour)
	if got := e.heard(rp(lore.RumorRadius+1, 0), "brenna"); len(got) != 0 {
		t.Fatalf("hors du rayon: %+v", got)
	}
	// Et tout est oublié au bout de la durée d'oubli.
	e.clk.Advance(lore.Forgetting)
	if got := e.heard(rp(0, 0), "brenna"); len(got) != 0 {
		t.Fatalf("après l'oubli: %+v", got)
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
		t.Fatalf("les champs non redonnés ont été effacés: %+v", second.Intro)
	}
	if second.Role != "maître forgeron" || second.Goal != "trouver son frère" {
		t.Fatalf("les champs redonnés n'ont pas été mis à jour: %+v", second.Intro)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("dates: création %v (attendue %v), mise à jour %v", second.CreatedAt, first.CreatedAt, second.UpdatedAt)
	}
	stored, found, _ := e.st.GetCard(e.ctx, "aldric", rp(0, 0))
	if !found || stored.Goal != "trouver son frère" || stored.Alias != "Aldric" {
		t.Fatalf("fiche stockée: %+v", stored)
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
		{"vide", "aldric", lore.Intro{}, lore.ErrEmptyIntro},
		{"lien", "aldric", lore.Intro{Role: "voir https://exemple.fr"}, lore.ErrUnsafeText},
		{"adresse", "aldric", lore.Intro{Trait: "moi@mail.fr"}, lore.ErrUnsafeText},
		{"usurpation", "aldric", lore.Intro{Alias: "Brenna"}, lore.ErrImpersonation},
		{"usurpation, autre casse", "aldric", lore.Intro{Alias: "BRENNA"}, lore.ErrImpersonation},
	}
	for _, c := range cases {
		if _, err := e.remember(c.account, rp(0, 0), c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: erreur %v, attendue %v", c.name, err, c.want)
		}
	}
	if cards, _ := e.st.ListCards(e.ctx, "", 10); len(cards) != 0 {
		t.Fatalf("des fiches refusées ont été écrites: %+v", cards)
	}

	// Son propre pseudo comme alias est permis (casse comprise).
	if _, err := e.remember("aldric", rp(0, 0), lore.Intro{Alias: "ALDRIC"}); err != nil {
		t.Fatalf("alias identique à son propre pseudo: %v", err)
	}
}

func TestRememberModerationFailsClosed(t *testing.T) {
	e := newEnv(t, "aldric")
	boom := errors.New("modération indisponible")
	e.moder = func(context.Context, []string) error { return boom }

	if _, err := e.remember("aldric", rp(0, 0), intro("Aldric", "forgeron")); !errors.Is(err, boom) {
		t.Fatalf("erreur: %v", err)
	}
	if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(0, 0)); found {
		t.Fatal("une fiche a été écrite alors que la modération a échoué")
	}
	// Pas de modérateur du tout: refus également.
	if _, err := e.book.Remember(e.ctx, "aldric", rp(0, 0), intro("Aldric", "forgeron"), lore.Facts{}, nil); err == nil {
		t.Fatal("écriture acceptée sans modérateur")
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
		t.Fatalf("textes modérés: %v", last)
	}
	if strings.Contains(joined, "\x1b") {
		t.Fatalf("séquence d'échappement transmise à la modération: %q", joined)
	}
}

func TestRememberUnknownAccount(t *testing.T) {
	e := newEnv(t)
	if _, err := e.remember("fantome", rp(0, 0), intro("Spectre", "rôdeur")); !errors.Is(err, store.ErrNoAccount) {
		t.Fatalf("erreur: %v", err)
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
		t.Fatalf("%d fiches, plafond %d", len(cards), lore.MaxCardsPerAccount)
	}
	// Ce sont les plus récentes qui restent.
	for i := 0; i < 3; i++ {
		if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(i, 0)); found {
			t.Errorf("la fiche la plus ancienne (lieu %d) aurait dû être écartée", i)
		}
	}
	if _, found, _ := e.st.GetCard(e.ctx, "aldric", rp(lore.MaxCardsPerAccount+2, 0)); !found {
		t.Error("la fiche la plus récente a disparu")
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
			t.Errorf("lieu %v: faits non mis à jour: %+v", p, c.Facts)
		}
		if !c.UpdatedAt.Equal(base) && !c.UpdatedAt.Equal(base.Add(0)) {
			t.Errorf("lieu %v: la date de mise à jour a bougé (%v): la rumeur repartirait de zéro", p, c.UpdatedAt)
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
		t.Fatalf("souvenir: %q", got)
	}
	// Un autre joueur, ou un autre PNJ, ne voit rien.
	if got, _ := e.book.Recall(e.ctx, npcA, "brenna"); got != "" {
		t.Fatalf("fuite vers un autre joueur: %q", got)
	}
	if got, _ := e.book.Recall(e.ctx, npcB, "aldric"); got != "" {
		t.Fatalf("fuite vers un autre PNJ: %q", got)
	}
	// Le souvenir se remplace.
	_ = e.book.Memorize(e.ctx, npcA, "ALDRIC", "Il a retrouvé son frère.")
	if got, _ := e.book.Recall(e.ctx, npcA, "aldric"); got != "Il a retrouvé son frère." {
		t.Fatalf("remplacement: %q", got)
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
			t.Fatalf("%q a écrasé le souvenir: %q", bad, got)
		}
	}
	_ = e.book.Memorize(e.ctx, npc, "aldric", "A\x1b[31mB\nC "+strings.Repeat("x", 1000))
	got, _ := e.book.Recall(e.ctx, npc, "aldric")
	if strings.ContainsAny(got, "\x1b\n") || len([]rune(got)) > lore.MaxSummary {
		t.Fatalf("souvenir non nettoyé: %q", got)
	}
	if err := e.book.Memorize(e.ctx, npc, "inconnu", "x"); !errors.Is(err, store.ErrNoAccount) {
		t.Fatalf("compte inconnu: %v", err)
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
		t.Fatalf("purge du compte: %d fiches, %d souvenirs, %v", cards, mems, err)
	}
	if left, _ := e.st.ListCards(e.ctx, "", 10); len(left) != 1 || left[0].Account != "brenna" {
		t.Fatalf("après purge: %+v", left)
	}

	cards, mems, _, err = e.st.PurgePlace(e.ctx, rp(0, 0))
	if err != nil || cards != 1 || mems != 1 {
		t.Fatalf("purge du lieu: %d fiches, %d souvenirs, %v", cards, mems, err)
	}
	if left, _ := e.st.ListCards(e.ctx, "", 10); len(left) != 0 {
		t.Fatalf("reste: %+v", left)
	}
	if cards, mems, _, _ := e.st.PurgeAccount(e.ctx, "personne"); cards != 0 || mems != 0 {
		t.Fatal("purge d'un compte sans données")
	}
}

func TestListCardsFiltersAndOrders(t *testing.T) {
	e := newEnv(t, "aldric", "brenna")
	_, _ = e.remember("aldric", rp(0, 0), intro("A", "x"))
	e.clk.Advance(time.Minute)
	_, _ = e.remember("brenna", rp(1, 0), intro("B", "y"))
	all, _ := e.st.ListCards(e.ctx, "", 10)
	if len(all) != 2 || all[0].Account != "brenna" {
		t.Fatalf("tri par récence: %+v", all)
	}
	if one, _ := e.st.ListCards(e.ctx, "aldric", 10); len(one) != 1 || one[0].Account != "aldric" {
		t.Fatalf("filtre par compte: %+v", one)
	}
	if lim, _ := e.st.ListCards(e.ctx, "", 1); len(lim) != 1 {
		t.Fatalf("limite: %+v", lim)
	}
}

func TestCardsNearBoxAndCutoff(t *testing.T) {
	e := newEnv(t, "a", "b", "c")
	_, _ = e.remember("a", rp(2, 2), intro("dedans", "x"))
	_, _ = e.remember("b", rp(4, 0), intro("dehors", "x")) // à 4 du centre (0,0)
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
		t.Fatalf("carré de rayon 3: %v", names)
	}
	// Coupure par date.
	got, _ = e.st.CardsNear(e.ctx, rp(0, 0), 3, base.Add(30*time.Minute), 10)
	if len(got) != 1 || got[0].Alias != "bord" {
		t.Fatalf("coupure par date: %+v", got)
	}
	// Limite.
	got, _ = e.st.CardsNear(e.ctx, rp(0, 0), 3, base.Add(-time.Minute), 1)
	if len(got) != 1 || got[0].Alias != "bord" {
		t.Fatalf("limite (la plus récente d'abord): %+v", got)
	}
}

func TestNegativeRegionCoordinatesWork(t *testing.T) {
	e := newEnv(t, "aldric")
	if _, err := e.remember("aldric", rp(-7, -2), intro("Aldric", "forgeron")); err != nil {
		t.Fatal(err)
	}
	if got := e.heard(rp(-8, -1), "x"); len(got) != 0 {
		t.Fatalf("pas encore arrivée: %+v", got)
	}
	e.clk.Advance(lore.HopDelay)
	if got := e.heard(rp(-8, -1), "x"); len(got) != 1 {
		t.Fatalf("après le délai: %+v", got)
	}
}

package lore

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func rp(x, y int) world.RegionPos { return world.RegionPos{X: x, Y: y} }

func card(account string, place world.RegionPos, updated time.Time) Card {
	return Card{
		Account: account, Place: place, UpdatedAt: updated, CreatedAt: updated,
		Intro: Intro{Alias: "Aldric le Rouge", Role: "forgeron", Origin: "les monts du Nord", Goal: "retrouver son frère", Trait: "boite de la jambe gauche"},
		Facts: Facts{Level: 14, BossKills: 2, Deaths: 3},
	}
}

func TestDistanceIsChebyshev(t *testing.T) {
	for _, c := range []struct {
		a, b world.RegionPos
		want int
	}{
		{rp(0, 0), rp(0, 0), 0}, {rp(0, 0), rp(1, 0), 1}, {rp(0, 0), rp(1, 1), 1},
		{rp(0, 0), rp(3, -2), 3}, {rp(-2, -2), rp(2, 1), 4},
	} {
		if got := Distance(c.a, c.b); got != c.want || Distance(c.b, c.a) != c.want {
			t.Errorf("Distance(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRumorTravelsAtHopDelayPerRegion(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	for d := 0; d <= RumorRadius; d++ {
		at := rp(d, 0)
		delay := time.Duration(d) * HopDelay
		if d > 0 && Reaches(c, at, t0.Add(delay-time.Second)) {
			t.Errorf("distance %d: the rumor arrives a second too early", d)
		}
		if !Reaches(c, at, t0.Add(delay)) {
			t.Errorf("distance %d: the rumor should have arrived after %v", d, delay)
		}
	}
	// The place of origin knows it immediately.
	if !Reaches(c, rp(0, 0), t0) {
		t.Error("the place of origin must know the card right away")
	}
}

func TestRumorStopsAtRadius(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	late := t0.Add(HopDelay + time.Hour)
	if !Reaches(c, rp(RumorRadius, RumorRadius), late) {
		t.Error("maximum distance: the rumor should arrive")
	}
	if Reaches(c, rp(RumorRadius+1, 0), late) || Reaches(c, rp(0, -RumorRadius-1), late) {
		t.Error("the rumor must not exceed its radius, even long afterwards")
	}
}

func TestRumorIsForgotten(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	if !Reaches(c, rp(0, 0), t0.Add(Forgetting)) {
		t.Error("right at the limit: still known")
	}
	if Reaches(c, rp(0, 0), t0.Add(Forgetting+time.Second)) {
		t.Error("after forgetting: must no longer be known")
	}
	// A card "from the future" (clock going backwards) is not known.
	if Reaches(c, rp(0, 0), t0.Add(-time.Hour)) {
		t.Error("card later than the current instant")
	}
}

func TestFidelityDecreasesWithDistance(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	type shown struct {
		alias, role, origin, goal, trait bool
		level, boss, deaths              bool
	}
	read := func(d int) shown {
		in, f := Fidelity(c, d)
		return shown{in.Alias != "", in.Role != "", in.Origin != "", in.Goal != "", in.Trait != "", f.Level > 0, f.BossKills > 0, f.Deaths > 0}
	}
	want := map[int]shown{
		0: {true, true, true, true, true, true, true, true},
		1: {true, true, true, true, true, true, true, false},
	}
	for d, w := range want {
		if got := read(d); got != w {
			t.Errorf("distance %d: %+v, want %+v", d, got, w)
		}
	}
}

func TestHearFiltersSortsAndLimits(t *testing.T) {
	now := t0.Add(HopDelay + 2*time.Hour)
	here := rp(0, 0)
	cards := []Card{
		card("voisin", rp(1, 0), t0),                        // d=1, arrived
		card("deux", rp(2, 0), t0),                          // outside radius
		card("ici-vieux", rp(0, 0), t0),                     // d=0, older
		card("ici-recent", rp(0, 0), now.Add(-time.Minute)), // d=0, more recent
		card("trop-loin", rp(4, 0), t0),                     // outside radius
		card("oublie", rp(0, 0), now.Add(-Forgetting-time.Hour)),
		card("pas-encore", rp(1, 0), now.Add(-HopDelay/2)), // d=1 but too recent to have arrived
	}
	got := Hear(cards, here, "moi", now)
	var names []string
	for _, h := range got {
		names = append(names, h.Account)
	}
	want := []string{"ici-recent", "ici-vieux", "voisin"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("rumors %v, want %v (proximity then recency; outside radius, forgotten and not yet arrived excluded)", names, want)
	}
	if got[0].Distance != 0 || !got[0].Local() || got[2].Distance != 1 || got[2].Local() {
		t.Fatalf("distances: %+v", got)
	}
	if got[2].Facts.Deaths != 0 {
		t.Fatalf("deaths are not told outside the place: %+v", got[2].Facts)
	}
}

func TestHearCapsRumorsButAlwaysKeepsTheSpeaker(t *testing.T) {
	now := t0.Add(HopDelay + time.Hour)
	var cards []Card
	for i := 0; i < MaxHeard+5; i++ {
		cards = append(cards, card("autre"+string(rune('a'+i)), rp(0, 0), t0.Add(time.Duration(i)*time.Second)))
	}
	cards = append(cards, card("Moi", rp(1, 1), t0)) // the interlocutor's card
	got := Hear(cards, rp(0, 0), "moi", now)

	rumors, selves := 0, 0
	for _, h := range got {
		if h.Self {
			selves++
			if h.Account != "Moi" {
				t.Errorf("Self sur %q", h.Account)
			}
		} else {
			rumors++
		}
	}
	if rumors != MaxHeard {
		t.Errorf("%d rumors, cap %d", rumors, MaxHeard)
	}
	if selves != 1 {
		t.Errorf("the interlocutor's card must always be passed on (%d)", selves)
	}
}

func TestHearKeepsOneCardPerAccountTheClosest(t *testing.T) {
	now := t0.Add(HopDelay + time.Hour)
	far := card("aldric", rp(1, 1), t0)
	far.Alias = "Version lointaine"
	near := card("ALDRIC", rp(0, 0), t0)
	near.Alias = "Version proche"
	got := Hear([]Card{far, near}, rp(0, 0), "x", now)
	if len(got) != 1 || got[0].Intro.Alias != "Version proche" {
		t.Fatalf("account duplicates: %+v", got)
	}
}

func TestHearSpeakerMatchIsCaseInsensitive(t *testing.T) {
	got := Hear([]Card{card("Aldric", rp(0, 0), t0)}, rp(0, 0), "aLdRiC", t0)
	if len(got) != 1 || !got[0].Self {
		t.Fatalf("%+v", got)
	}
}

func TestHearEmpty(t *testing.T) {
	if got := Hear(nil, rp(0, 0), "x", t0); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// --- Validation ---

func TestIntroCleanBoundsAndSanitizes(t *testing.T) {
	in, err := Intro{
		Alias:  "  Aldric\x1b[31m  le\nRouge " + strings.Repeat("x", 100),
		Role:   "forgeron\u202e",
		Origin: strings.Repeat("très loin ", 30),
	}.Clean()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(in.Alias, "\x1b\n") || strings.Contains(in.Role, "\u202e") {
		t.Fatalf("dangerous characters kept: %+v", in)
	}
	if n := len([]rune(in.Alias)); n > MaxAlias {
		t.Fatalf("alias of %d characters, cap %d", n, MaxAlias)
	}
	if n := len([]rune(in.Origin)); n > MaxField {
		t.Fatalf("origin of %d characters, cap %d", n, MaxField)
	}
	if !strings.HasPrefix(in.Alias, "Aldric") {
		t.Fatalf("alias = %q", in.Alias)
	}
}

func TestIntroCleanRejectsEmpty(t *testing.T) {
	for _, in := range []Intro{{}, {Alias: "  \n "}, {Role: "\x1b[0m"}, {Goal: "\u200b\u200b"}} {
		if _, err := in.Clean(); err != ErrEmptyIntro {
			t.Errorf("%+v: error %v", in, err)
		}
	}
	if _, err := (Intro{Trait: "borgne"}).Clean(); err != nil {
		t.Errorf("a single field is enough: %v", err)
	}
}

func TestIntroCleanRejectsLinksAndAddresses(t *testing.T) {
	for _, bad := range []string{
		"visitez https://exemple.fr", "http://truc", "ecrivez a moi@mail.fr", "mon site: www.exemple", "rejoins discord.gg/abc",
		"boutique.com", "pseudo@serveur", "x://y",
	} {
		for _, field := range []string{"alias", "role", "origin", "goal", "trait"} {
			in := Intro{Alias: "Ok"}
			switch field {
			case "alias":
				in.Alias = bad
			case "role":
				in.Role = bad
			case "origin":
				in.Origin = bad
			case "goal":
				in.Goal = bad
			case "trait":
				in.Trait = bad
			}
			if _, err := in.Clean(); err != ErrUnsafeText {
				t.Errorf("%s=%q accepted (%v)", field, bad, err)
			}
		}
	}
	// Normal text that looks vaguely similar is not rejected.
	if _, err := (Intro{Goal: "retrouver la comtesse de Fer-Rive", Trait: "rit comme un tonnerre"}).Clean(); err != nil {
		t.Errorf("legitimate text rejected: %v", err)
	}
}

func TestCleanSummary(t *testing.T) {
	if got := CleanSummary("  Il\x1b[31m cherche\nson frère.  "); got != "Il cherche son frère." {
		t.Fatalf("got %q", got)
	}
	if got := CleanSummary(strings.Repeat("a", 1000)); len([]rune(got)) != MaxSummary {
		t.Fatalf("memory of %d characters", len([]rune(got)))
	}
	if got := CleanSummary("écrivez à moi@mail.fr"); got != "" {
		t.Fatalf("address kept: %q", got)
	}
	if CleanSummary("") != "" {
		t.Fatal("empty memory")
	}
}

func TestFieldsSkipsEmpty(t *testing.T) {
	if f := (Intro{Alias: "a", Goal: "g"}).Fields(); len(f) != 2 || f[0] != "a" || f[1] != "g" {
		t.Fatalf("%v", f)
	}
	if f := (Intro{}).Fields(); len(f) != 0 {
		t.Fatalf("%v", f)
	}
}

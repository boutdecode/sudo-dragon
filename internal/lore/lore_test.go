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
			t.Errorf("Distance(%v,%v) = %d, attendu %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRumorTravelsAtHopDelayPerRegion(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	for d := 0; d <= RumorRadius; d++ {
		at := rp(d, 0)
		delay := time.Duration(d) * HopDelay
		if d > 0 && Reaches(c, at, t0.Add(delay-time.Second)) {
			t.Errorf("distance %d: la rumeur arrive une seconde trop tôt", d)
		}
		if !Reaches(c, at, t0.Add(delay)) {
			t.Errorf("distance %d: la rumeur devrait être arrivée après %v", d, delay)
		}
	}
	// Le lieu d'origine la connaît immédiatement.
	if !Reaches(c, rp(0, 0), t0) {
		t.Error("le lieu d'origine doit connaître la fiche tout de suite")
	}
}

func TestRumorStopsAtRadius(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	late := t0.Add(24 * time.Hour)
	if !Reaches(c, rp(RumorRadius, RumorRadius), late) {
		t.Error("distance maximale: la rumeur devrait arriver")
	}
	if Reaches(c, rp(RumorRadius+1, 0), late) || Reaches(c, rp(0, -RumorRadius-1), late) {
		t.Error("la rumeur ne doit pas dépasser son rayon, même longtemps après")
	}
}

func TestRumorIsForgotten(t *testing.T) {
	c := card("aldric", rp(0, 0), t0)
	if !Reaches(c, rp(0, 0), t0.Add(Forgetting)) {
		t.Error("pile à la limite: encore connue")
	}
	if Reaches(c, rp(0, 0), t0.Add(Forgetting+time.Second)) {
		t.Error("après l'oubli: ne doit plus être connue")
	}
	// Une fiche « du futur » (horloge qui recule) n'est pas connue.
	if Reaches(c, rp(0, 0), t0.Add(-time.Hour)) {
		t.Error("fiche postérieure à l'instant courant")
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
		2: {true, true, true, false, false, false, true, false},
		3: {true, true, false, false, false, false, true, false},
	}
	for d, w := range want {
		if got := read(d); got != w {
			t.Errorf("distance %d: %+v, attendu %+v", d, got, w)
		}
	}
	// Les exploits notoires voyagent plus loin que le reste.
	if _, f := Fidelity(c, 3); f.BossKills != 2 {
		t.Error("les seigneurs de guerre vaincus doivent rester connus à distance 3")
	}
}

func TestHearFiltersSortsAndLimits(t *testing.T) {
	now := t0.Add(2 * time.Hour)
	here := rp(0, 0)
	cards := []Card{
		card("lointain", rp(3, 0), t0),                      // d=3, arrivée
		card("voisin", rp(1, 0), t0),                        // d=1
		card("ici-vieux", rp(0, 0), t0),                     // d=0, plus ancien
		card("ici-recent", rp(0, 0), now.Add(-time.Minute)), // d=0, plus récent
		card("trop-loin", rp(4, 0), t0),                     // hors rayon
		card("oublie", rp(0, 0), now.Add(-Forgetting-time.Hour)),
		card("pas-encore", rp(2, 0), now.Add(-HopDelay)), // d=2 mais trop récent pour être arrivé
	}
	got := Hear(cards, here, "moi", now)
	var names []string
	for _, h := range got {
		names = append(names, h.Account)
	}
	want := []string{"ici-recent", "ici-vieux", "voisin", "lointain"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("rumeurs %v, attendu %v (proximité puis récence; hors rayon, oubliées et pas encore arrivées exclues)", names, want)
	}
	if got[0].Distance != 0 || !got[0].Local() || got[3].Distance != 3 || got[3].Local() {
		t.Fatalf("distances: %+v", got)
	}
	if got[3].Intro.Goal != "" || got[3].Intro.Role == "" {
		t.Fatalf("la rumeur lointaine devrait être lacunaire: %+v", got[3].Intro)
	}
}

func TestHearCapsRumorsButAlwaysKeepsTheSpeaker(t *testing.T) {
	now := t0.Add(time.Hour)
	var cards []Card
	for i := 0; i < MaxHeard+5; i++ {
		cards = append(cards, card("autre"+string(rune('a'+i)), rp(0, 0), t0.Add(time.Duration(i)*time.Second)))
	}
	cards = append(cards, card("Moi", rp(2, 2), t0)) // la fiche de l'interlocuteur, plus loin que toutes les autres
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
		t.Errorf("%d rumeurs, plafond %d", rumors, MaxHeard)
	}
	if selves != 1 {
		t.Errorf("la fiche de l'interlocuteur doit toujours être transmise (%d)", selves)
	}
}

func TestHearKeepsOneCardPerAccountTheClosest(t *testing.T) {
	now := t0.Add(time.Hour)
	far := card("aldric", rp(3, 0), t0)
	far.Alias = "Version lointaine"
	near := card("ALDRIC", rp(1, 0), t0)
	near.Alias = "Version proche"
	got := Hear([]Card{far, near}, rp(0, 0), "x", now)
	if len(got) != 1 || got[0].Intro.Alias != "Version proche" {
		t.Fatalf("doublons de compte: %+v", got)
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
		t.Fatalf("caractères dangereux conservés: %+v", in)
	}
	if n := len([]rune(in.Alias)); n > MaxAlias {
		t.Fatalf("alias de %d caractères, plafond %d", n, MaxAlias)
	}
	if n := len([]rune(in.Origin)); n > MaxField {
		t.Fatalf("origine de %d caractères, plafond %d", n, MaxField)
	}
	if !strings.HasPrefix(in.Alias, "Aldric") {
		t.Fatalf("alias = %q", in.Alias)
	}
}

func TestIntroCleanRejectsEmpty(t *testing.T) {
	for _, in := range []Intro{{}, {Alias: "  \n "}, {Role: "\x1b[0m"}, {Goal: "\u200b\u200b"}} {
		if _, err := in.Clean(); err != ErrEmptyIntro {
			t.Errorf("%+v: erreur %v", in, err)
		}
	}
	if _, err := (Intro{Trait: "borgne"}).Clean(); err != nil {
		t.Errorf("un seul champ suffit: %v", err)
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
				t.Errorf("%s=%q accepté (%v)", field, bad, err)
			}
		}
	}
	// Du texte normal qui ressemble de loin n'est pas rejeté.
	if _, err := (Intro{Goal: "retrouver la comtesse de Fer-Rive", Trait: "rit comme un tonnerre"}).Clean(); err != nil {
		t.Errorf("texte légitime rejeté: %v", err)
	}
}

func TestCleanSummary(t *testing.T) {
	if got := CleanSummary("  Il\x1b[31m cherche\nson frère.  "); got != "Il cherche son frère." {
		t.Fatalf("got %q", got)
	}
	if got := CleanSummary(strings.Repeat("a", 1000)); len([]rune(got)) != MaxSummary {
		t.Fatalf("souvenir de %d caractères", len([]rune(got)))
	}
	if got := CleanSummary("écrivez à moi@mail.fr"); got != "" {
		t.Fatalf("adresse conservée: %q", got)
	}
	if CleanSummary("") != "" {
		t.Fatal("souvenir vide")
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

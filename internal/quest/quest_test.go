package quest

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/world"
)

var home = world.RegionPos{X: 0, Y: 0}

func ctx() Context {
	return Context{
		Level: 5, Giver: world.NPCKey{Region: home, Index: 2},
		Species: []Species{{"rat", 4}, {"wolf", 10}},
		Places:  map[string]world.RegionPos{"solmont": {X: 2, Y: -1}, "tourelle": home},
		Now:     time.Unix(1_000_000, 0),
	}
}

func killProposal() Proposal {
	return Proposal{Title: "Les loups du bois", Story: "Les loups rôdent près des champs.", Type: "kill", Species: "wolf", Count: 5, Gold: 60, XP: 10, Potions: 1, Note: "Mes remerciements"}
}

func TestValidKillQuest(t *testing.T) {
	q, err := Validate(killProposal(), ctx())
	if err != nil {
		t.Fatal(err)
	}
	if q.Objective != (Objective{Kind: Kill, Species: "wolf", Count: 5}) || q.State != Active || q.Tier != 2 {
		t.Fatalf("%+v", q)
	}
	if q.Reward.Gold != 60 || q.Reward.XP != 10 || q.Reward.Potions != 1 || q.Reward.Note != "Mes remerciements" {
		t.Fatalf("récompense: %+v", q.Reward)
	}
}

func TestRewardIsClampedWhateverTheModelSays(t *testing.T) {
	for _, tc := range []struct{ gold, xp, potions int }{
		{1_000_000, 1_000_000, 99}, {-5, -5, -5}, {0, 0, 0}, {1 << 40, 1 << 40, 1 << 40},
	} {
		p := killProposal()
		p.Gold, p.XP, p.Potions = tc.gold, tc.xp, tc.potions
		q, err := Validate(p, ctx())
		if err != nil {
			t.Fatal(err)
		}
		lo, hi := RewardBounds(50, false) // 5 loups × 10 XP
		r := q.Reward
		if r.Gold < lo.Gold || r.Gold > hi.Gold || r.XP > hi.XP || r.Potions > hi.Potions || r.XP < 0 || r.Potions < 0 {
			t.Errorf("%+v → %+v hors bornes [%+v, %+v]", tc, r, lo, hi)
		}
	}
}

func TestRejectedProposals(t *testing.T) {
	bad := map[string]func(*Proposal, *Context){
		"espèce inventée":      func(p *Proposal, _ *Context) { p.Species = "dragon" },
		"espèce de garnison":   func(p *Proposal, _ *Context) { p.Species = "warlord" },
		"trop peu":             func(p *Proposal, _ *Context) { p.Count = 2 },
		"trop":                 func(p *Proposal, _ *Context) { p.Count = 9999 },
		"négatif":              func(p *Proposal, _ *Context) { p.Count = -3 },
		"type inconnu":         func(p *Proposal, _ *Context) { p.Type = "livrer" },
		"titre vide":           func(p *Proposal, _ *Context) { p.Title = "  " },
		"histoire vide":        func(p *Proposal, _ *Context) { p.Story = "" },
		"lien":                 func(p *Proposal, _ *Context) { p.Story = "Allez sur http://exemple.net" },
		"adresse dans la note": func(p *Proposal, _ *Context) { p.Note = "écrivez à moi@exemple.fr" },
		"boss niveau 1":        func(p *Proposal, c *Context) { p.Type = "boss"; c.BossAround = true; c.Level = 1 },
		"boss sans forteresse": func(p *Proposal, c *Context) { p.Type = "boss"; c.Level = 20 },
		"lieu inconnu":         func(p *Proposal, _ *Context) { p.Type = "reach"; p.Place = "Atlantide" },
		"lieu du donneur":      func(p *Proposal, _ *Context) { p.Type = "reach"; p.Place = "Tourelle" },
		"type vide":            func(p *Proposal, _ *Context) { p.Type = "" },
	}
	for name, mutate := range bad {
		p, c := killProposal(), ctx()
		mutate(&p, &c)
		if _, err := Validate(p, c); err == nil {
			t.Errorf("%s : proposition acceptée", name)
		}
	}
}

func TestReachAndBossQuests(t *testing.T) {
	p := Proposal{Title: "Message pour Solmont", Story: "Portez-y mes salutations.", Type: "reach", Place: "  SOLMONT ", Gold: 1}
	q, err := Validate(p, ctx())
	if err != nil || q.Objective.Kind != Reach || q.Objective.Target != (world.RegionPos{X: 2, Y: -1}) || q.Need() != 1 {
		t.Fatalf("reach: %+v %v", q, err)
	}
	c := ctx()
	c.Level, c.BossAround = 12, true
	q, err = Validate(Proposal{Title: "Le seigneur", Story: "Abattez-le.", Type: "boss", Gold: 1 << 30, Potions: 50}, c)
	if err != nil || q.Objective.Species != "warlord" || q.Tier != 3 || q.Reward.Potions > 3 || q.Reward.Gold > 500 {
		t.Fatalf("boss: %+v %v", q, err)
	}
}

func TestTextIsSanitized(t *testing.T) {
	p := killProposal()
	p.Title = "Loups\x1b[2J‮ du bois"
	p.Story = "Histoire​ avec\x00 des\tcaractères"
	q, err := Validate(p, ctx())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{q.Title, q.Story} {
		if strings.ContainsAny(s, "\x1b\x00\t‮​") {
			t.Errorf("texte non nettoyé : %q", s)
		}
	}
}

func TestAdvanceReadyAndExpiry(t *testing.T) {
	q, _ := Validate(killProposal(), ctx())
	if q.Advance(2); q.State != Active || q.Progress != 2 {
		t.Fatalf("%+v", q)
	}
	q.Advance(10)
	if q.State != Ready || q.Progress != 5 {
		t.Fatalf("la progression doit s'arrêter à l'objectif : %+v", q)
	}
	if q.Advance(1) {
		t.Fatal("une quête prête ne progresse plus")
	}
	if q.Expired(q.CreatedAt.Add(Lifetime-time.Minute)) || !q.Expired(q.CreatedAt.Add(Lifetime+time.Minute)) {
		t.Fatal("expiration")
	}
	q.State = Done
	if q.Expired(q.CreatedAt.Add(2 * Lifetime)) {
		t.Fatal("une quête rendue n'expire pas")
	}
}

func TestReputationTiersAndDiscount(t *testing.T) {
	for pts, want := range map[int]string{0: "Inconnu", 2: "Inconnu", 3: "Connu", 8: "Estimé", 15: "Honoré", 99: "Honoré"} {
		if TierName(pts) != want {
			t.Errorf("%d points → %s, attendu %s", pts, TierName(pts), want)
		}
	}
	if NextTier(0) != 3 || NextTier(7) != 1 || NextTier(15) != 0 {
		t.Error("NextTier")
	}
	if Discount(100, 0) != 100 || Discount(100, 8) != 80 || Discount(100, 15) != 60 || Discount(1, 15) != 1 || Discount(0, 15) != 0 {
		t.Errorf("remises: %d %d %d", Discount(100, 0), Discount(100, 8), Discount(100, 15))
	}
	q := Quest{Tier: 3}
	if q.ReputationGain() != 4 || (Quest{Tier: 0}).ReputationGain() != 2 {
		t.Error("gain de réputation")
	}
}

func FuzzValidate(f *testing.F) {
	f.Add("t", "s", "kill", "wolf", "", 5, 10, 10, 1, "n")
	f.Add("\x1b[0m", "‮", "reach", "", "Solmont", -1, 1<<60, -1, 1<<30, "@")
	f.Fuzz(func(t *testing.T, title, story, typ, sp, place string, count, gold, xp, potions int, note string) {
		q, err := Validate(Proposal{title, story, typ, sp, place, count, gold, xp, potions, note}, ctx())
		if err != nil {
			return
		}
		if q.Title == "" || q.Story == "" || strings.ContainsAny(q.Title+q.Story+q.Reward.Note, "\x1b\x00") {
			t.Fatalf("quête incohérente : %+v", q)
		}
		if q.Reward.Gold < 0 || q.Reward.XP < 0 || q.Reward.Potions < 0 || q.Reward.Potions > 3 || q.Reward.Gold > 2000 {
			t.Fatalf("récompense hors bornes : %+v", q.Reward)
		}
		if q.Need() < 1 || q.Need() > MaxCount {
			t.Fatalf("objectif incohérent : %+v", q.Objective)
		}
	})
}

func TestDungeonQuests(t *testing.T) {
	g := world.NPCKey{Region: world.RegionPos{X: 0, Y: 0}}
	target := world.RegionPos{X: 1, Y: -1}
	c := Context{Level: 5, Giver: g, Now: time.Now(), Dungeons: map[string]DungeonRef{
		"crypte de test": {Region: target, Tier: 2},
	}}
	p := Proposal{Title: "Le maître", Story: "Allez-y.", Type: "dungeon", Place: " Crypte de TEST ", Gold: 100000, XP: 100000, Potions: 50}
	q, err := Validate(p, c)
	if err != nil {
		t.Fatal(err)
	}
	if q.Objective != (Objective{Kind: Dungeon, Species: "lich", Count: 1, Target: target}) || q.State != Active || q.Need() != 1 {
		t.Errorf("objectif: %+v", q)
	}
	diff := 150 + 50*2
	lo, hi := RewardBounds(diff, true)
	if q.Reward.Gold != hi.Gold || q.Reward.XP != hi.XP || q.Reward.Potions != hi.Potions || hi.Potions != 3 || lo.Gold != diff/2 {
		t.Errorf("récompense %+v, bornes %+v / %+v", q.Reward, lo, hi)
	}
	if q.Tier != 3 {
		t.Errorf("palier de quête %d", q.Tier)
	}

	for name, mutate := range map[string]func(*Proposal, *Context){
		"nom inconnu":     func(p *Proposal, _ *Context) { p.Place = "autre" },
		"sans nom":        func(p *Proposal, _ *Context) { p.Place = "" },
		"niveau trop bas": func(_ *Proposal, c *Context) { c.Level = DungeonLevel(2) - 1 },
		"aucun donjon":    func(_ *Proposal, c *Context) { c.Dungeons = nil },
	} {
		pp, cc := p, c
		mutate(&pp, &cc)
		if _, err := Validate(pp, cc); err == nil {
			t.Errorf("%s: acceptée", name)
		}
	}
	// Un lieu ordinaire n'est pas un donjon, et inversement.
	reach := Proposal{Title: "t", Story: "s", Type: "reach", Place: "crypte de test"}
	if _, err := Validate(reach, c); err == nil {
		t.Error("un donjon ne se rejoint pas comme un lieu")
	}
}

func TestDungeonLevelGrowsWithTier(t *testing.T) {
	prev := 0
	for tier := 1; tier <= 5; tier++ {
		if l := DungeonLevel(tier); l <= prev {
			t.Errorf("palier %d: niveau %d", tier, l)
		} else {
			prev = l
		}
	}
	if DungeonLevel(0) != DungeonLevel(1) || DungeonLevel(9) != DungeonLevel(5) {
		t.Error("les paliers hors bornes devraient être ramenés dans [1, 5]")
	}
}

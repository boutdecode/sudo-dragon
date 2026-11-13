package npcai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/world"
)

func smithReq() StockRequest {
	npc := testNPC()
	npc.Kind, npc.Index = world.NPCSmith, 5
	return StockRequest{
		NPC: npc, Lang: i18n.FR, Place: "Valdurne", PlaceKind: world.KindVillage, Region: "Forêts d'Aube",
		Context: gear.Context{Slots: gear.SlotsOfSmith, MaxTier: 3, Origin: npc.Key()},
	}
}

func stockJSON(items ...map[string]any) llm.Response {
	b, _ := json.Marshal(map[string]any{"specialty": "lames courtes et dagues", "items": items})
	return llm.Response{Content: string(b), Usage: llm.Usage{Total: 300}}
}

func item(name string, tier int) map[string]any {
	return map[string]any{"slot": "weapon", "name": name, "desc": "Une lame.", "tier": tier, "family": "longsword", "quality": 1, "price": 90}
}

func (r *rig) stock(req StockRequest) StockReply {
	r.t.Helper()
	ch := make(chan StockReply, 1)
	r.svc.Stock(req, func(rep StockReply) { ch <- rep })
	select {
	case rep := <-ch:
		return rep
	}
}

func TestStockPromptHasNoPlayerTextAndLists(t *testing.T) {
	msgs := BuildStockMessages(smithReq())
	sys := msgs[0].Content
	for _, want := range []string{"forgeron du village de Valdurne", "weapon", "1 à 3", `"specialty"`, "Forêts d'Aube"} {
		if !strings.Contains(sys, want) {
			t.Errorf("stock prompt lacks %q", want)
		}
	}
	for _, fam := range []string{"weapon: dagger", "shortsword", "greatsword", "crossbow", `"family"`, `"quality"`} {
		if !strings.Contains(sys, fam) {
			t.Errorf("stock prompt lacks %q", fam)
		}
	}
	if strings.Contains(sys, `"atk"`) {
		t.Error("the prompt must no longer ask for a numeric attack")
	}
	if strings.Contains(sys, "DONNÉES") {
		t.Error("no player data must enter the stock request")
	}
}

func TestStockIsValidatedModeratedStoredAndGeneratedOnce(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.wares = r.be.Store
	calls := 0
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		calls++
		return stockJSON(item("Dague", 1), item("Épée courte", 2), item("Épée longue", 3), item("Rapière", 2), item("Épée du roi", 5),
			map[string]any{"slot": "armor", "name": "Cotte", "tier": 1, "def": 1}, // not sold here
			item("Dague", 1), // duplicate
		), nil
	}
	rep := r.stock(smithReq())
	if rep.Failure != OK || rep.Wares.Specialty != "lames courtes et dagues" || len(rep.Wares.Items) != 5 {
		t.Fatalf("%+v", rep)
	}
	for i, it := range rep.Wares.Items {
		if it.ID != int64(i+1) || it.Slot != gear.Weapon || it.Tier > 3 || it.Family != "longsword" || it.Damage().IsZero() {
			t.Errorf("item %d: %+v", i, it)
		}
		if _, hi := gear.Bounds(gear.Weapon, it.Tier); it.Quality > hi {
			t.Errorf("quality %d above the cap", it.Quality)
		}
	}
	// Saved: a second call rereads the database without querying the model.
	rep2 := r.stock(smithReq())
	if !rep2.FromStore || len(rep2.Wares.Items) != 5 || calls != 1 {
		t.Fatalf("FromStore=%v, %d items, %d calls", rep2.FromStore, len(rep2.Wares.Items), calls)
	}
	if st := r.svc.Stats(); st.StockGenerated != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestBadStockIsRejected(t *testing.T) {
	cases := map[string]llm.Response{
		"too few items": stockJSON(item("Dague", 1), item("Épée", 2)),
		"no JSON":       {Content: "voici mon étalage : des épées !", Usage: llm.Usage{Total: 10}},
		"no specialty": func() llm.Response {
			b, _ := json.Marshal(map[string]any{"items": []any{item("A", 1), item("B", 1), item("C", 2), item("D", 2)}})
			return llm.Response{Content: string(b)}
		}(),
		"link in the specialty": func() llm.Response {
			b, _ := json.Marshal(map[string]any{"specialty": "voir www.exemple.com", "items": []any{item("A", 1), item("B", 1), item("C", 2), item("D", 2)}})
			return llm.Response{Content: string(b)}
		}(),
	}
	for name, resp := range cases {
		r := newRig(t, Config{})
		r.svc.wares = r.be.Store
		r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return resp, nil }
		if rep := r.stock(smithReq()); rep.Failure == OK {
			t.Errorf("%s: stock accepted", name)
		}
		if _, ok, _ := r.be.Store.GetWares(context.Background(), smithReq().NPC.Key()); ok {
			t.Errorf("%s: stock rejected but saved", name)
		}
	}
}

func TestStockFailsClosedOnModeration(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.wares = r.be.Store
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return stockJSON(item("Dague", 1), item("Épée", 2), item("Hache", 2), item("Masse", 3)), nil
	}
	r.fake.ModerateFunc = func(_ context.Context, texts []string) ([]llm.Verdict, error) {
		out := make([]llm.Verdict, len(texts))
		out[2].Flagged = []string{"hate_and_discrimination"}
		return out, nil
	}
	if rep := r.stock(smithReq()); rep.Failure != Blocked {
		t.Fatalf("flagged text accepted: %+v", rep)
	}
	r.fake.ModerateFunc = func(context.Context, []string) ([]llm.Verdict, error) { return nil, llm.ErrUnavailable }
	if rep := r.stock(smithReq()); rep.Failure == OK {
		t.Fatal("moderation down: stock shown unchecked")
	}
}

func TestStockWithTheModelDown(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.wares = r.be.Store
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) { return llm.Response{}, llm.ErrUnavailable }
	if rep := r.stock(smithReq()); rep.Failure == OK {
		t.Fatal("stock without a model")
	}
}

func TestParseStockIsTolerant(t *testing.T) {
	sp, props, ok := parseStock("```json\n{\"specialty\":\"haches\",\"items\":[{\"slot\":\"weapon\",\"name\":\"Hache\",\"tier\":\"2\",\"quality\":\"4\",\"family\":\"axe\",\"price\":null},{\"name\":5}]}\n```")
	if !ok || sp != "haches" || len(props) != 2 || props[0].Tier != 2 || props[0].Quality != 4 || props[0].Family != "axe" {
		t.Fatalf("%q %+v %v", sp, props, ok)
	}
	for _, bad := range []string{"", "rien", `{"items":[]}`, `{"items":"non"}`} {
		if _, _, ok := parseStock(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestStockPromptAsksForWeaponStyleAndParsesIt(t *testing.T) {
	if sys := BuildStockMessages(smithReq())[0].Content; !strings.Contains(sys, "family") {
		t.Error("the stock prompt must ask for the weapons' style")
	}
	_, props, ok := parseStock(`{"specialty":"arcs","items":[{"slot":"weapon","name":"Arc","tier":1,"family":"crossbow","price":30,"kind":"ranged"}]}`)
	if !ok || len(props) != 1 || props[0].Family != "crossbow" {
		t.Fatalf("%+v %v", props, ok)
	}
}

func TestStaleStockIsRegeneratedButStillServedWhenTheModelIsDown(t *testing.T) {
	r := newRig(t, Config{})
	r.svc.wares = r.be.Store
	old := gear.Wares{NPC: smithReq().NPC.Key(), Specialty: "anciennes lames", Version: 1,
		Items: []gear.Item{{ID: 1, Name: "Vieille épée", Slot: gear.Weapon, Tier: 1, Family: "dagger", Price: 30}}}
	if err := r.be.Store.PutWares(context.Background(), old); err != nil {
		t.Fatal(err)
	}

	// Model down: the old stock is served.
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return llm.Response{}, context.DeadlineExceeded
	}
	if rep := r.stock(smithReq()); rep.Failure != OK || rep.Wares.Specialty != "anciennes lames" {
		t.Fatalf("fallback to the old stock expected: %+v", rep)
	}

	// Model available: new stock, saved at the current version.
	r.fake.ChatFunc = func(context.Context, llm.Request) (llm.Response, error) {
		return stockJSON(item("Dague", 1), item("Épée courte", 2), item("Épée longue", 3), item("Rapière", 2)), nil
	}
	rep := r.stock(smithReq())
	if rep.Failure != OK || rep.FromStore || rep.Wares.Version != StockVersion || rep.Wares.Specialty != "lames courtes et dagues" {
		t.Fatalf("%+v", rep)
	}
	if rep2 := r.stock(smithReq()); !rep2.FromStore || rep2.Wares.Version != StockVersion {
		t.Fatalf("%+v", rep2)
	}
}

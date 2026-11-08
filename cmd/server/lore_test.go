package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

func seededStore(t *testing.T) *store.Store {
	t.Helper()
	be := storetest.NewBackend(t, time.Hour)
	ctx := context.Background()
	for _, a := range []string{"aldric", "brenna"} {
		if _, err := be.Store.CreateAccount(a, storetest.Password); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for _, c := range []lore.Card{
		{Account: "aldric", Place: world.RegionPos{X: 0, Y: 0}, Intro: lore.Intro{Alias: "Aldric le Rouge", Role: "forgeron"}, CreatedAt: now, UpdatedAt: now},
		{Account: "aldric", Place: world.RegionPos{X: 2, Y: -1}, Intro: lore.Intro{Alias: "Aldric", Role: "voyageur"}, CreatedAt: now, UpdatedAt: now.Add(-time.Hour)},
		{Account: "brenna", Place: world.RegionPos{X: 0, Y: 0}, Intro: lore.Intro{Alias: "Brenna", Role: "archère"}, CreatedAt: now, UpdatedAt: now.Add(-time.Minute)},
	} {
		if err := be.Store.UpsertCard(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	_ = be.Store.SaveCharacters(map[string]store.Character{"aldric": {Level: 2, HP: 5, QuestsDirty: true, Quests: []quest.Quest{
		{ID: 1, Title: "Les loups", Story: "x", State: quest.Done, CreatedAt: now, DoneAt: now, Giver: world.NPCKey{Region: world.RegionPos{X: 0, Y: 0}}},
	}}})
	_ = be.Store.PutMemory(ctx, lore.Memory{NPC: world.NPCKey{Region: world.RegionPos{X: 0, Y: 0}}, Account: "aldric", Summary: "souvenir", UpdatedAt: now})
	return be.Store
}

func TestLoreListShowsCardsAndFilters(t *testing.T) {
	st := seededStore(t)
	var out bytes.Buffer
	if err := runLoreList(context.Background(), st, "", &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"aldric", "brenna", "Aldric le Rouge", "forgeron", "région (0,0)", "région (2,-1)", "quête rendue : « Les loups »"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("liste sans %q :\n%s", want, out.String())
		}
	}
	out.Reset()
	_ = runLoreList(context.Background(), st, "brenna", &out)
	if strings.Contains(out.String(), "aldric") || !strings.Contains(out.String(), "Brenna") {
		t.Fatalf("filtre par compte :\n%s", out.String())
	}
	out.Reset()
	_ = runLoreList(context.Background(), seededEmpty(t), "", &out)
	if !strings.Contains(out.String(), "aucune présentation") {
		t.Fatalf("base vide : %q", out.String())
	}
}

func seededEmpty(t *testing.T) *store.Store {
	return storetest.NewBackend(t, time.Hour).Store
}

func TestLorePurgeByAccountAndPlace(t *testing.T) {
	st := seededStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	if err := runLorePurge(ctx, st, "account:aldric", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 présentation(s), 1 souvenir(s) et 1 quête(s)") {
		t.Fatalf("compte rendu : %q", out.String())
	}
	if left, _ := st.ListCards(ctx, "", 10); len(left) != 1 || left[0].Account != "brenna" {
		t.Fatalf("après purge du compte : %+v", left)
	}

	out.Reset()
	if err := runLorePurge(ctx, st, "place:0,0", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 présentation(s)") {
		t.Fatalf("compte rendu : %q", out.String())
	}
	if left, _ := st.ListCards(ctx, "", 10); len(left) != 0 {
		t.Fatalf("après purge du lieu : %+v", left)
	}
}

func TestLorePurgeRejectsBadTargetsWithoutDeletingAnything(t *testing.T) {
	st := seededStore(t)
	ctx := context.Background()
	for _, bad := range []string{"", "aldric", "account:", ":aldric", "joueur:aldric", "place:", "place:1", "place:a,b", "place:1,2,3x", "place:,"} {
		var out bytes.Buffer
		if err := runLorePurge(ctx, st, bad, &out); err == nil {
			t.Errorf("%q accepté", bad)
		}
	}
	if left, _ := st.ListCards(ctx, "", 10); len(left) != 3 {
		t.Fatalf("une cible invalide a supprimé des données : %d fiches restantes", len(left))
	}
}

func TestParseRegion(t *testing.T) {
	for in, want := range map[string][2]int{"0,0": {0, 0}, "3,-4": {3, -4}, " -7 , 2 ": {-7, 2}} {
		x, y, err := parseRegion(in)
		if err != nil || x != want[0] || y != want[1] {
			t.Errorf("parseRegion(%q) = %d,%d,%v", in, x, y, err)
		}
	}
}

func TestWaresListAndReset(t *testing.T) {
	st := seededStore(t)
	ctx := context.Background()
	var out bytes.Buffer
	if err := runWaresList(ctx, st, &out); err != nil || !strings.Contains(out.String(), "aucun étalage") {
		t.Fatalf("%v %q", err, out.String())
	}
	key := world.NPCKey{Region: world.RegionPos{X: 2, Y: -1}, Index: 7}
	if err := st.PutWares(ctx, gear.Wares{NPC: key, Specialty: "haches", Version: 1, CreatedAt: time.Now(),
		Items: []gear.Item{{ID: 1, Name: "Hache en fer", Slot: gear.Weapon, Tier: 1, Atk: 2, Price: 40}}}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = runWaresList(ctx, st, &out)
	for _, want := range []string{"vendeur 2,-1,7", "haches", "Hache en fer", "ATQ 2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("liste sans %q:\n%s", want, out.String())
		}
	}
	for _, bad := range []string{"", "1,2", "a,b,c", "1,2,3,4"} {
		if err := runWaresReset(ctx, st, bad, &out); err == nil {
			t.Errorf("%q accepté", bad)
		}
	}
	out.Reset()
	if err := runWaresReset(ctx, st, "2,-1,7", &out); err != nil || !strings.Contains(out.String(), "effacé") {
		t.Fatalf("%v %q", err, out.String())
	}
	if _, ok, _ := st.GetWares(ctx, key); ok {
		t.Fatal("étalage encore présent")
	}
	out.Reset()
	_ = runWaresReset(ctx, st, "2,-1,7", &out)
	if !strings.Contains(out.String(), "aucun étalage") {
		t.Fatalf("%q", out.String())
	}
}

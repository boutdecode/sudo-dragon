package gear

import (
	"github.com/kevin/ascii-adventure/internal/hero"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/world"
)

func ctx(slots ...Slot) Context {
	return Context{Slots: slots, MaxTier: 3, Origin: world.NPCKey{Region: world.RegionPos{X: 1, Y: 2}, Index: 4}}
}

func TestValidWeaponAndArmor(t *testing.T) {
	w, err := Validate(Proposal{Slot: "weapon", Name: "Épée longue", Desc: "Une lame bien équilibrée.", Tier: 2, Atk: 4, Def: 99, Price: 130}, ctx(Weapon))
	if err != nil {
		t.Fatal(err)
	}
	if w.Slot != Weapon || w.Atk != 4 || w.Def != 0 || w.Tier != 2 || w.Origin.Index != 4 {
		t.Fatalf("%+v", w)
	}
	a, err := Validate(Proposal{Slot: " ARMOR ", Name: "Cotte de mailles", Tier: 3, Atk: 50, Def: 2, Price: 1}, ctx(Armor, Shield))
	if err != nil {
		t.Fatal(err)
	}
	if a.Atk != 0 || a.Def != 2 || a.Price < 5 {
		t.Fatalf("%+v", a)
	}
}

func TestEveryStatIsClamped(t *testing.T) {
	for _, slot := range Slots {
		for tier := 1; tier <= MaxTier; tier++ {
			for _, v := range []int{-5, 0, 1, 1 << 30} {
				it, err := Validate(Proposal{Slot: string(slot), Name: "x", Tier: tier, Atk: v, Def: v, Price: v}, Context{Slots: Slots, MaxTier: MaxTier})
				if err != nil {
					t.Fatal(err)
				}
				lo, hi := Bounds(slot, tier)
				main := it.Def
				if slot == Weapon {
					main = it.Atk
				}
				if main < lo || main > hi {
					t.Errorf("%s palier %d, valeur %d : bonus %d hors de [%d, %d]", slot, tier, v, main, lo, hi)
				}
				plo, phi := PriceBounds(tier, main)
				if it.Price < plo || it.Price > phi {
					t.Errorf("prix %d hors de [%d, %d]", it.Price, plo, phi)
				}
			}
		}
	}
}

func TestTierIsBoundedByThePlace(t *testing.T) {
	it, err := Validate(Proposal{Slot: "weapon", Name: "Épée du roi", Tier: 5, Atk: 99, Price: 99999}, ctx(Weapon))
	if err != nil || it.Tier != 3 {
		t.Fatalf("%+v %v", it, err)
	}
	if _, hi := Bounds(Weapon, 3); it.Atk > hi {
		t.Fatalf("bonus %d au-delà du plafond du palier 3", it.Atk)
	}
	low, _ := Validate(Proposal{Slot: "weapon", Name: "x", Tier: -3}, ctx(Weapon))
	if low.Tier != 1 {
		t.Fatalf("palier négatif: %d", low.Tier)
	}
}

func TestRejectedProposals(t *testing.T) {
	bad := map[string]Proposal{
		"emplacement inconnu":  {Slot: "ring", Name: "x"},
		"emplacement vide":     {Slot: "", Name: "x"},
		"non vendu ici":        {Slot: "armor", Name: "x"},
		"nom vide":             {Slot: "weapon", Name: "  "},
		"lien dans le nom":     {Slot: "weapon", Name: "Épée www.exemple.com"},
		"adresse dans la desc": {Slot: "weapon", Name: "Épée", Desc: "écrire à moi@exemple.fr"},
	}
	for name, p := range bad {
		if _, err := Validate(p, ctx(Weapon)); err == nil {
			t.Errorf("%s : accepté", name)
		}
	}
}

func TestTextIsSanitized(t *testing.T) {
	it, err := Validate(Proposal{Slot: "weapon", Name: "Lame\x1b[2J‮ rouge", Desc: strings.Repeat("longue ", 60) + "\x00"}, ctx(Weapon))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(it.Name+it.Desc, "\x1b\x00‮") || len([]rune(it.Desc)) > MaxDesc || len([]rune(it.Name)) > MaxName {
		t.Fatalf("%q %q", it.Name, it.Desc)
	}
}

func TestEquippedBonus(t *testing.T) {
	var e Equipped
	w, a := &Item{Slot: Weapon, Atk: 5}, &Item{Slot: Armor, Def: 3}
	e.Set(Weapon, w)
	e.Set(Armor, a)
	e.Set(Helm, &Item{Slot: Helm, Def: 1})
	if atk, def := e.Bonus(); atk != 5 || def != 4 {
		t.Fatalf("bonus %d/%d", atk, def)
	}
	e.Set(Weapon, nil)
	if atk, _ := e.Bonus(); atk != 0 || e.Get(Weapon) != nil || e.Get(Armor) != a {
		t.Fatal("retrait")
	}
}

func TestSanitizeBringsStoredItemsBack(t *testing.T) {
	it, ok := Sanitize(Item{ID: 3, Slot: Weapon, Name: "Épée", Tier: 99, Atk: 9999, Def: 50, Price: 1 << 40})
	if !ok || it.Tier != MaxTier || it.Atk != 12 || it.Def != 0 || it.Price > 10*12*6*3/2 {
		t.Fatalf("%+v %v", it, ok)
	}
	for _, bad := range []Item{{ID: 0, Slot: Weapon, Name: "x"}, {ID: 1, Slot: "ring", Name: "x"}, {ID: 1, Slot: Weapon, Name: " "}} {
		if _, ok := Sanitize(bad); ok {
			t.Errorf("%+v accepté", bad)
		}
	}
}

func TestSellPriceAndLevels(t *testing.T) {
	if (Item{Price: 100}).SellPrice() != 40 || (Item{Price: 1}).SellPrice() != 1 {
		t.Fatal("prix de revente")
	}
	prev := 0
	for tier := 1; tier <= MaxTier; tier++ {
		if l := (Item{Tier: tier}).MinLevel(); l <= prev {
			t.Fatalf("niveaux requis non croissants: %d", l)
		} else {
			prev = l
		}
	}
}

func FuzzValidate(f *testing.F) {
	f.Add("weapon", "Épée", "d", 2, 3, 4, 50)
	f.Add("armor", "\x1b[0m", "‮", -1, 1<<60, -1, 1<<30)
	f.Fuzz(func(t *testing.T, slot, name, desc string, tier, atk, def, price int) {
		it, err := Validate(Proposal{Slot: slot, Name: name, Desc: desc, Tier: tier, Atk: atk, Def: def, Price: price}, Context{Slots: Slots, MaxTier: MaxTier})
		if err != nil {
			return
		}
		lo, hi := Bounds(it.Slot, it.Tier)
		main := it.Def
		if it.Slot == Weapon {
			main = it.Atk
		}
		if main < lo || main > hi || it.Tier < 1 || it.Tier > MaxTier || it.Name == "" || strings.ContainsAny(it.Name+it.Desc, "\x1b\x00") {
			t.Fatalf("objet incohérent: %+v", it)
		}
		if plo, phi := PriceBounds(it.Tier, main); it.Price < plo || it.Price > phi {
			t.Fatalf("prix hors bornes: %+v", it)
		}
	})
}

func sample(id int64, slot Slot, tier int) Item {
	it := Item{ID: id, Name: "Objet", Slot: slot, Tier: tier, Price: 50}
	if slot == Weapon {
		it.Atk = 2
	} else {
		it.Def = 1
	}
	return it
}

func TestInventoryEquipRemoveAndCapacity(t *testing.T) {
	var inv Inventory
	sword, err := inv.Add(sample(0, Weapon, 3))
	if err != nil || sword.ID != 1 {
		t.Fatalf("%+v %v", sword, err)
	}
	helm, _ := inv.Add(sample(0, Helm, 1))
	if inv.Equip(sword.ID, 5) != ErrLevelTooLow || inv.Equip(99, 50) != ErrNoSuchItem {
		t.Fatal("niveau ou objet inconnu")
	}
	if err := inv.Equip(sword.ID, 8); err != nil {
		t.Fatal(err)
	}
	inv.Equip(helm.ID, 1)
	if atk, def := inv.Equipped().Bonus(); atk != 2 || def != 1 {
		t.Fatalf("bonus %d/%d", atk, def)
	}
	if _, err := inv.Remove(sword.ID); err != ErrWorn {
		t.Fatalf("un objet équipé ne se retire pas du sac: %v", err)
	}
	second, _ := inv.Add(sample(0, Weapon, 1))
	inv.Equip(second.ID, 1) // remplace la première arme
	if inv.IsWorn(sword.ID) || !inv.IsWorn(second.ID) {
		t.Fatal("remplacement")
	}
	if _, err := inv.Remove(sword.ID); err != nil {
		t.Fatal(err)
	}
	inv.Unequip(Weapon)
	if atk, _ := inv.Equipped().Bonus(); atk != 0 {
		t.Fatal("retrait")
	}
	for len(inv.Items) < BagCapacity {
		if _, err := inv.Add(sample(0, Armor, 1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := inv.Add(sample(0, Armor, 1)); err != ErrBagFull {
		t.Fatalf("sac plein attendu: %v", err)
	}
	ids := map[int64]bool{}
	for _, it := range inv.Items {
		if ids[it.ID] {
			t.Fatalf("identifiant en double %d", it.ID)
		}
		ids[it.ID] = true
	}
}

func TestSanitizeInventory(t *testing.T) {
	in := Inventory{
		Items: []Item{sample(1, Weapon, 2), sample(1, Armor, 1), {ID: 2, Slot: "ring", Name: "x"}, sample(3, Helm, 99), sample(-4, Helm, 1)},
		Worn:  map[Slot]int64{Weapon: 1, Armor: 1, Helm: 77, Shield: 3},
	}
	out := SanitizeInventory(in)
	if len(out.Items) != 2 || out.Items[0].ID != 1 || out.Items[1].ID != 3 || out.Items[1].Tier != MaxTier {
		t.Fatalf("%+v", out.Items)
	}
	if len(out.Worn) != 1 || out.Worn[Weapon] != 1 {
		t.Fatalf("seule l'arme est valablement portée : %v", out.Worn)
	}
	var big Inventory
	for i := 1; i <= BagCapacity+10; i++ {
		big.Items = append(big.Items, sample(int64(i), Armor, 1))
	}
	if got := SanitizeInventory(big); len(got.Items) != BagCapacity {
		t.Fatalf("%d objets", len(got.Items))
	}
	c := out.Clone()
	c.Items[0].Name = "autre"
	c.Worn[Weapon] = 9
	if out.Items[0].Name == "autre" || out.Worn[Weapon] != 1 {
		t.Fatal("copie partagée")
	}
}

func TestFallbackWaresAreDeterministicValidAndWithinCaps(t *testing.T) {
	npc := world.NPCKey{Region: world.RegionPos{X: 3, Y: -2}, Index: 7}
	for _, ctx := range []Context{
		{Slots: SlotsOfSmith, MaxTier: 3, Origin: npc},
		{Slots: SlotsOfArmorer, MaxTier: 5, Origin: npc},
	} {
		for _, l := range []i18n.Lang{i18n.FR, i18n.EN} {
			a, b := Fallback(npc, ctx, l), Fallback(npc, ctx, l)
			if a.Specialty == "" || len(a.Items) < 6 {
				t.Fatalf("assortiment pauvre: %+v", a)
			}
			names := map[string]bool{}
			tiers := map[int]bool{}
			for i, it := range a.Items {
				if it.Name != b.Items[i].Name || it.Price != b.Items[i].Price {
					t.Fatal("repli non déterministe")
				}
				if names[it.Name] || strings.HasPrefix(it.Name, "gear.") {
					t.Errorf("nom en double ou clé manquante: %q", it.Name)
				}
				names[it.Name] = true
				tiers[it.Tier] = true
				// Un objet de repli respecte exactement les règles de Validate.
				back, err := Validate(Proposal{Slot: string(it.Slot), Name: it.Name, Desc: it.Desc, Tier: it.Tier, Atk: it.Atk, Def: it.Def, Price: it.Price}, ctx)
				if err != nil || back.Atk != it.Atk || back.Def != it.Def || back.Price != it.Price || back.Tier != it.Tier {
					t.Errorf("objet de repli hors règles: %+v → %+v (%v)", it, back, err)
				}
				if it.Tier > ctx.MaxTier {
					t.Errorf("palier %d > %d", it.Tier, ctx.MaxTier)
				}
			}
			if len(tiers) < 2 {
				t.Error("le repli devrait couvrir plusieurs paliers")
			}
		}
	}
	other := Fallback(world.NPCKey{Region: world.RegionPos{X: 9, Y: 9}, Index: 1}, Context{Slots: SlotsOfSmith, MaxTier: 3}, i18n.FR)
	same := Fallback(npc, Context{Slots: SlotsOfSmith, MaxTier: 3}, i18n.FR)
	if other.Specialty == same.Specialty && other.Items[0].Name == same.Items[0].Name && other.Items[1].Name == same.Items[1].Name {
		t.Log("deux vendeurs ont le même assortiment (possible mais rare)")
	}
}

func TestWeaponKindIsValidatedAndOnlyForWeapons(t *testing.T) {
	ctx := Context{Slots: Slots, MaxTier: 3}
	for in, want := range map[string]hero.Kind{"ranged": hero.Ranged, " MAGIC ": hero.Magic, "melee": hero.Melee, "": hero.Melee, "laser": hero.Melee} {
		it, err := Validate(Proposal{Slot: "weapon", Name: "Arme", Tier: 1, Atk: 2, Price: 30, Kind: in}, ctx)
		if err != nil || it.Kind != want {
			t.Errorf("style %q: %q (%v), attendu %q", in, it.Kind, err, want)
		}
	}
	it, err := Validate(Proposal{Slot: "armor", Name: "Cotte", Tier: 1, Def: 1, Price: 30, Kind: "magic"}, ctx)
	if err != nil || it.Kind != "" {
		t.Fatalf("une armure n'a pas de style: %+v %v", it, err)
	}
	if s, ok := Sanitize(Item{ID: 1, Name: "x", Slot: Weapon, Tier: 1, Atk: 2, Price: 10, Kind: "x"}); !ok || s.Kind != hero.Melee {
		t.Fatalf("Sanitize: %+v", s)
	}
}

func TestFallbackSmithSellsAllThreeStyles(t *testing.T) {
	for idx := range 12 {
		w := Fallback(world.NPCKey{Index: idx}, Context{Slots: SlotsOfSmith, MaxTier: 3}, i18n.FR)
		kinds := map[hero.Kind]bool{}
		for _, it := range w.Items {
			kinds[it.Kind] = true
		}
		if !kinds[hero.Melee] || !kinds[hero.Ranged] || !kinds[hero.Magic] {
			t.Fatalf("vendeur %d: styles %v", idx, kinds)
		}
	}
}

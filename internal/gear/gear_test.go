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
	w, err := Validate(Proposal{Slot: "weapon", Name: "Épée longue", Desc: "Une lame bien équilibrée.", Tier: 2, Family: "longsword", Quality: 2, Def: 99, Price: 130}, ctx(Weapon))
	if err != nil {
		t.Fatal(err)
	}
	if w.Slot != Weapon || w.Quality != 2 || w.Def != 0 || w.Tier != 2 || w.Origin.Index != 4 || w.Kind != hero.Melee {
		t.Fatalf("%+v", w)
	}
	// Rang 2, tranchant (d6), qualité 2 : 2d6+2.
	if got := w.Damage().String(); got != "2d6+2" {
		t.Fatalf("dégâts %s", got)
	}
	a, err := Validate(Proposal{Slot: " ARMOR ", Name: "Cotte de mailles", Tier: 3, Family: "medium", Quality: 50, Def: 2, Price: 1}, ctx(Armor, Shield))
	if err != nil {
		t.Fatal(err)
	}
	if a.Quality != 0 || a.Def != 2 || a.Price < 5 || a.Reduction().String() != "3d6" || !a.Damage().IsZero() {
		t.Fatalf("%+v réduction %s", a, a.Reduction())
	}
}

func TestEveryStatIsClamped(t *testing.T) {
	for _, slot := range Slots {
		for tier := 1; tier <= MaxTier; tier++ {
			for _, v := range []int{-5, 0, 1, 1 << 30} {
				it, err := Validate(Proposal{Slot: string(slot), Name: "x", Tier: tier, Quality: v, Def: v, Price: v}, Context{Slots: Slots, MaxTier: MaxTier})
				if err != nil {
					t.Fatal(err)
				}
				lo, hi := Bounds(slot, tier)
				main := it.Def
				if slot == Weapon {
					main = it.Quality
				}
				if main < lo || main > hi {
					t.Errorf("%s palier %d, valeur %d : bonus %d hors de [%d, %d]", slot, tier, v, main, lo, hi)
				}
				plo, phi := PriceBounds(tier, it.Power())
				if it.Price < plo || it.Price > phi {
					t.Errorf("prix %d hors de [%d, %d]", it.Price, plo, phi)
				}
			}
		}
	}
}

func TestTierIsBoundedByThePlace(t *testing.T) {
	it, err := Validate(Proposal{Slot: "weapon", Name: "Épée du roi", Tier: 5, Quality: 99, Price: 99999}, ctx(Weapon))
	if err != nil || it.Tier != 3 {
		t.Fatalf("%+v %v", it, err)
	}
	if _, hi := Bounds(Weapon, 3); it.Quality > hi {
		t.Fatalf("qualité %d au-delà du plafond du palier 3", it.Quality)
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

func TestEquippedDefenseAndReduction(t *testing.T) {
	var e Equipped
	w, a := &Item{Slot: Weapon, Tier: 2, Family: "mace", Quality: 1}, &Item{Slot: Armor, Tier: 3, Family: "heavy", Def: 3}
	e.Set(Weapon, w)
	e.Set(Armor, a)
	e.Set(Helm, &Item{Slot: Helm, Def: 1})
	if e.Defense() != 4 || e.Reduction().String() != "3d8" {
		t.Fatalf("DEF %d, réduction %s", e.Defense(), e.Reduction())
	}
	if w.Damage().String() != "2d8+1" {
		t.Fatalf("masse de rang 2: %s", w.Damage())
	}
	e.Set(Armor, nil)
	if !e.Reduction().IsZero() || e.Get(Weapon) != w || e.Get(Armor) != nil {
		t.Fatal("retrait")
	}
}

func TestFamiliesFixTheDie(t *testing.T) {
	want := map[string]int{"dagger": 4, "spear": 4, "shortsword": 6, "longsword": 6, "axe": 6, "mace": 8, "greatsword": 12, "greataxe": 12, "shortbow": 6, "crossbow": 8, "wand": 4, "staff": 6}
	for id, die := range want {
		f, ok := FamilyByID(Weapon, id)
		if !ok || f.Die != die {
			t.Errorf("%s: %+v %v", id, f, ok)
		}
	}
	for id, die := range map[string]int{"light": 4, "medium": 6, "heavy": 8} {
		if f, ok := FamilyByID(Armor, id); !ok || f.Die != die {
			t.Errorf("armure %s: %+v", id, f)
		}
	}
	if _, ok := FamilyByID(Shield, "light"); ok {
		t.Error("un bouclier n'a pas de famille")
	}
	if len(weaponNameFamilies) != namesPerSlot[Weapon] || len(armorNameFamilies) != namesPerSlot[Armor] {
		t.Fatal("chaque nom du catalogue doit avoir une famille")
	}
}

func TestUnknownFamilyFallsBackToTheStyleDefault(t *testing.T) {
	for _, c := range []struct{ fam, kind, want string }{{"railgun", "", "shortsword"}, {"", "ranged", "shortbow"}, {"LONGSWORD ", "magic", "longsword"}, {"nope", "magic", "staff"}} {
		it, err := Validate(Proposal{Slot: "weapon", Name: "x", Tier: 1, Family: c.fam, Kind: c.kind}, ctx(Weapon))
		if err != nil || it.Family != c.want {
			t.Errorf("%+v: famille %q (%v), attendu %q", c, it.Family, err, c.want)
		}
	}
	if it, _ := Validate(Proposal{Slot: "armor", Name: "x", Tier: 1, Family: "weapon"}, ctx(Armor)); it.Family != "medium" {
		t.Errorf("armure: %q", it.Family)
	}
	if it, _ := Validate(Proposal{Slot: "helm", Name: "x", Tier: 1, Family: "heavy"}, ctx(Helm)); it.Family != "" {
		t.Errorf("un casque n'a pas de famille: %q", it.Family)
	}
}

func TestTwoHandedWeaponsExcludeShields(t *testing.T) {
	var inv Inventory
	sword, _ := inv.Add(Item{Name: "Espadon", Slot: Weapon, Tier: 1, Family: "greatsword", Price: 50})
	short, _ := inv.Add(Item{Name: "Épée", Slot: Weapon, Tier: 1, Family: "shortsword", Price: 50})
	shield, _ := inv.Add(Item{Name: "Rondache", Slot: Shield, Tier: 1, Def: 1, Price: 50})
	if err := inv.Equip(short.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := inv.Equip(shield.ID, 1); err != nil {
		t.Fatalf("une épée courte se porte avec un bouclier: %v", err)
	}
	if err := inv.Equip(sword.ID, 1); err != ErrTwoHanded {
		t.Fatalf("espadon avec bouclier: %v", err)
	}
	inv.Unequip(Shield)
	if err := inv.Equip(sword.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := inv.Equip(shield.ID, 1); err != ErrTwoHanded {
		t.Fatalf("bouclier avec espadon: %v", err)
	}
}

func TestStarterWeaponsMatchTheClass(t *testing.T) {
	for class, want := range map[hero.Class]struct {
		kind hero.Kind
		dice string
	}{hero.Warrior: {hero.Melee, "1d6"}, hero.Rogue: {hero.Ranged, "1d6"}, hero.Mage: {hero.Magic, "1d6"}} {
		for _, l := range []i18n.Lang{i18n.FR, i18n.EN} {
			it := Starter(l, class)
			clean, ok := Sanitize(Item{ID: 1, Name: it.Name, Slot: it.Slot, Tier: it.Tier, Family: it.Family, Price: it.Price, Kind: it.Kind})
			if !ok || it.Kind != want.kind || it.Damage().String() != want.dice || it.MinLevel() != 1 || it.Name == "" || strings.HasPrefix(it.Name, "gear.") || clean.Family != it.Family {
				t.Errorf("%s/%s: %+v", class, l, it)
			}
		}
	}
}

func TestSanitizeBringsStoredItemsBack(t *testing.T) {
	it, ok := Sanitize(Item{ID: 3, Slot: Weapon, Name: "Épée", Tier: 99, Quality: 9999, Def: 50, Price: 1 << 40})
	if _, phi := PriceBounds(MaxTier, it.Power()); !ok || it.Tier != MaxTier || it.Quality != MaxTier || it.Def != 0 || it.Price > phi {
		t.Fatalf("%+v %v", it, ok)
	}
	// Un objet d'avant les familles (sans famille) reçoit la famille par défaut de son style.
	old, ok := Sanitize(Item{ID: 4, Slot: Weapon, Name: "Arc", Tier: 2, Kind: hero.Ranged, Price: 50})
	if !ok || old.Family != "shortbow" || old.Kind != hero.Ranged {
		t.Fatalf("ancien objet: %+v", old)
	}
	oldArmor, ok := Sanitize(Item{ID: 5, Slot: Armor, Name: "Cotte", Tier: 2, Def: 2, Price: 50})
	if !ok || oldArmor.Family != "medium" || oldArmor.Reduction().String() != "2d6" {
		t.Fatalf("ancienne armure: %+v", oldArmor)
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
		it, err := Validate(Proposal{Slot: slot, Name: name, Desc: desc, Tier: tier, Quality: atk, Def: def, Price: price, Family: desc}, Context{Slots: Slots, MaxTier: MaxTier})
		if err != nil {
			return
		}
		lo, hi := Bounds(it.Slot, it.Tier)
		main := it.Def
		if it.Slot == Weapon {
			main = it.Quality
		}
		if main < lo || main > hi || it.Tier < 1 || it.Tier > MaxTier || it.Name == "" || strings.ContainsAny(it.Name+it.Desc, "\x1b\x00") {
			t.Fatalf("objet incohérent: %+v", it)
		}
		if plo, phi := PriceBounds(it.Tier, it.Power()); it.Price < plo || it.Price > phi {
			t.Fatalf("prix hors bornes: %+v", it)
		}
	})
}

func sample(id int64, slot Slot, tier int) Item {
	it := Item{ID: id, Name: "Objet", Slot: slot, Tier: tier, Price: 50}
	switch slot {
	case Weapon:
		it.Family = "shortsword"
	case Armor:
		it.Family, it.Def = "medium", 1
	default:
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
	if err := inv.Equip(sword.ID, 10); err != nil {
		t.Fatal(err)
	}
	inv.Equip(helm.ID, 1)
	if inv.Equipped().Defense() != 1 || inv.Equipped().Weapon == nil {
		t.Fatalf("équipement porté: %+v", inv.Equipped())
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
	if inv.Equipped().Weapon != nil {
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
				back, err := Validate(Proposal{Slot: string(it.Slot), Name: it.Name, Desc: it.Desc, Tier: it.Tier, Family: it.Family, Quality: it.Quality, Def: it.Def, Price: it.Price}, ctx)
				if err != nil || back.Quality != it.Quality || back.Def != it.Def || back.Price != it.Price || back.Tier != it.Tier || back.Family != it.Family || it.Family == "" && it.Slot != Shield && it.Slot != Helm {
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
		it, err := Validate(Proposal{Slot: "weapon", Name: "Arme", Tier: 1, Quality: 1, Price: 30, Kind: in}, ctx)
		if err != nil || it.Kind != want {
			t.Errorf("style %q: %q (%v), attendu %q", in, it.Kind, err, want)
		}
	}
	it, err := Validate(Proposal{Slot: "armor", Name: "Cotte", Tier: 1, Def: 1, Price: 30, Kind: "magic"}, ctx)
	if err != nil || it.Kind != "" {
		t.Fatalf("une armure n'a pas de style: %+v %v", it, err)
	}
	if s, ok := Sanitize(Item{ID: 1, Name: "x", Slot: Weapon, Tier: 1, Quality: 1, Price: 10, Kind: "x"}); !ok || s.Kind != hero.Melee {
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

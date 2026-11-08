package world

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
)

func TestEveryStructureAndNPCHasAnIdentity(t *testing.T) {
	checked := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			if s.Name == "" || !unicode.IsUpper([]rune(s.Name)[0]) {
				t.Fatalf("seed %d %v: nom de lieu %q invalide", seed, s.Kind, s.Name)
			}
			for _, r := range s.Name {
				if !unicode.IsLetter(r) && r != '-' && r != ' ' {
					t.Fatalf("nom de lieu %q: caractère %q inattendu", s.Name, r)
				}
			}
			if n := len([]rune(s.Name)); n < 4 || n > 30 {
				t.Fatalf("nom de lieu %q: %d caractères", s.Name, n)
			}
			names := map[string]bool{}
			for i, n := range s.NPCs {
				checked++
				if n.Name == "" || n.Region != s.Region || n.Index != i {
					t.Fatalf("PNJ %d de %s: identité incomplète: %+v", i, s.Name, n)
				}
				if names[n.Name] {
					t.Fatalf("deux PNJ de %s s'appellent %q", s.Name, n.Name)
				}
				names[n.Name] = true
				p := n.Persona
				if p.Temperament == "" || p.Quirk == "" || p.Speech == "" {
					t.Fatalf("personnalité incomplète pour %s: %+v", n.Name, p)
				}
				// Le titre reflète le métier.
				switch n.Kind {
				case NPCHealer:
					if !strings.HasPrefix(n.Name, "Frère ") && !strings.HasPrefix(n.Name, "Sœur ") {
						t.Fatalf("soigneur %q sans titre religieux", n.Name)
					}
				case NPCMerchant:
					if !strings.HasPrefix(n.Name, "Maître ") && !strings.HasPrefix(n.Name, "Dame ") {
						t.Fatalf("marchand %q sans titre de commerce", n.Name)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("aucun PNJ vérifié")
	}
}

func TestFortressNamesLookLikeFortresses(t *testing.T) {
	found := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			if s.Kind != KindFortress {
				continue
			}
			found++
			ok := false
			for _, p := range fortPrefix {
				ok = ok || strings.HasPrefix(s.Name, p)
			}
			if !ok {
				t.Fatalf("forteresse nommée %q", s.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("aucune forteresse")
	}
}

// Mêmes identités pour tous, à chaque démarrage et après vidage du cache.
func TestIdentitiesAreDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			sa, sb := a.structureForRegion(RegionPos{rx, ry}), b.structureForRegion(RegionPos{rx, ry})
			if sa == nil {
				continue
			}
			if sa.Name != sb.Name {
				t.Fatalf("nom différent: %q / %q", sa.Name, sb.Name)
			}
			for i := range sa.NPCs {
				if sa.NPCs[i] != sb.NPCs[i] {
					t.Fatalf("PNJ différent: %+v / %+v", sa.NPCs[i], sb.NPCs[i])
				}
			}
		}
	}
	// Vidage du cache: reconstruit à l'identique.
	before := allStructures(a, 3)
	a.smu.Lock()
	clear(a.structs)
	a.smu.Unlock()
	after := allStructures(a, 3)
	for i := range before {
		if before[i].Name != after[i].Name || len(before[i].NPCs) != len(after[i].NPCs) {
			t.Fatal("identité différente après reconstruction du cache")
		}
		for j := range before[i].NPCs {
			if before[i].NPCs[j] != after[i].NPCs[j] {
				t.Fatalf("PNJ %d différent après reconstruction", j)
			}
		}
	}
}

func TestIdentitiesVaryBetweenPlacesAndSeeds(t *testing.T) {
	names := map[string]int{}
	total := 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			names[s.Name]++
			total++
		}
	}
	if len(names) < total*7/10 {
		t.Fatalf("seulement %d noms de lieux distincts sur %d: trop de répétitions", len(names), total)
	}
	personas := map[Persona]bool{}
	npcNames := map[string]bool{}
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			for _, n := range s.NPCs {
				personas[n.Persona] = true
				npcNames[n.Name] = true
			}
		}
	}
	if len(personas) < 20 || len(npcNames) < 15 {
		t.Fatalf("peu de variété: %d personnalités, %d noms de PNJ", len(personas), len(npcNames))
	}
}

// Les identités ne dépendent pas de l'ordre dans lequel on visite les régions.
func TestIdentityDoesNotDependOnVisitOrder(t *testing.T) {
	forward, backward := New(7), New(7)
	var regions []RegionPos
	for ry := -3; ry <= 3; ry++ {
		for rx := -3; rx <= 3; rx++ {
			regions = append(regions, RegionPos{rx, ry})
		}
	}
	for _, r := range regions {
		forward.structureForRegion(r)
	}
	for i := len(regions) - 1; i >= 0; i-- {
		backward.structureForRegion(regions[i])
	}
	for _, r := range regions {
		f, b := forward.structureForRegion(r), backward.structureForRegion(r)
		if (f == nil) != (b == nil) || (f != nil && f.Name != b.Name) {
			t.Fatalf("région %v: identité dépendante de l'ordre de visite", r)
		}
	}
}

func TestNPCKeyIsStableAndResolvable(t *testing.T) {
	w := sharedWorld(1)
	seen := map[NPCKey]bool{}
	count := 0
	for _, s := range allStructures(w, 6) {
		for i, n := range s.NPCs {
			count++
			k := n.Key()
			if k != (NPCKey{s.Region, i}) {
				t.Fatalf("clé %+v, attendue %+v", k, NPCKey{s.Region, i})
			}
			if seen[k] {
				t.Fatalf("clé en double: %+v", k)
			}
			seen[k] = true
			// La clé retrouve bien le PNJ.
			got := w.structureForRegion(k.Region).NPCs[k.Index]
			if got != n {
				t.Fatalf("la clé %+v ne retrouve pas le PNJ", k)
			}
			// NPCAt renvoie l'identité complète.
			at, ok := w.NPCAt(n.Pos.X, n.Pos.Y)
			if !ok || at != n {
				t.Fatalf("NPCAt ne renvoie pas l'identité complète: %+v", at)
			}
		}
	}
	if count == 0 {
		t.Fatal("aucun PNJ")
	}
}

func TestIdentityRNGIsIndependentPerRegionAndSalt(t *testing.T) {
	a := identityRNG(1, RegionPos{0, 0}, 0).Uint64()
	for _, other := range []uint64{
		identityRNG(1, RegionPos{1, 0}, 0).Uint64(),
		identityRNG(1, RegionPos{0, 1}, 0).Uint64(),
		identityRNG(1, RegionPos{0, 0}, 1).Uint64(),
		identityRNG(2, RegionPos{0, 0}, 0).Uint64(),
	} {
		if a == other {
			t.Fatal("deux flux d'identité différents produisent la même valeur")
		}
	}
	if identityRNG(1, RegionPos{3, -4}, 5).Uint64() != identityRNG(1, RegionPos{3, -4}, 5).Uint64() {
		t.Fatal("le flux d'identité n'est pas déterministe")
	}
}

// Les régions opposées, ou symétriques, ne doivent jamais partager d'identité:
// avec un mélange par simple XOR de produits, (x, y) et (-x, -y) à coordonnées
// impaires donnaient le même flux, donc des villes jumelles avec les mêmes PNJ.
func TestIdentityRNGHasNoRegionCollisions(t *testing.T) {
	seen := map[uint64]string{}
	for _, seed := range []int64{1, 2, 42} {
		for salt := uint64(0); salt < 4; salt++ {
			for ry := -12; ry <= 12; ry++ {
				for rx := -12; rx <= 12; rx++ {
					v := identityRNG(seed, RegionPos{rx, ry}, salt).Uint64()
					key := fmt.Sprintf("seed %d salt %d (%d,%d)", seed, salt, rx, ry)
					if prev, dup := seen[v]; dup {
						t.Fatalf("même flux pour %s et %s", prev, key)
					}
					seen[v] = key
				}
			}
		}
	}
}

func TestSymmetricRegionsGetDifferentNames(t *testing.T) {
	w := sharedWorld(1)
	same, pairs := 0, 0
	for ry := -8; ry <= 8; ry++ {
		for rx := -8; rx <= 8; rx++ {
			a, b := w.structureForRegion(RegionPos{rx, ry}), w.structureForRegion(RegionPos{-rx, -ry})
			if a == nil || b == nil || (rx == 0 && ry == 0) || a == b {
				continue
			}
			pairs++
			if a.Name == b.Name {
				same++
			}
		}
	}
	if pairs == 0 {
		t.Fatal("aucune paire de régions symétriques occupées")
	}
	// Quelques homonymes fortuits sont normaux (les tables sont finies), pas des dizaines.
	if same*10 > pairs {
		t.Fatalf("%d paires de lieux symétriques sur %d portent le même nom", same, pairs)
	}
}

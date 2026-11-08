package world

import (
	"fmt"
	"github.com/kevin/ascii-adventure/internal/i18n"
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
			for i := 0; i < numFortPrefixes; i++ {
				p := i18n.T(i18n.FR, fmt.Sprintf("place.fort.%d", i))
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

// Les noms de PNJ et de forteresses existent dans les deux langues, sans changer
// l'identité (prénoms, lieux) ni les mondes déjà joués.
func TestIdentitiesAreTranslated(t *testing.T) {
	forts, npcs := 0, 0
	for _, seed := range testSeeds {
		for _, s := range allStructures(sharedWorld(seed), 5) {
			if s.NameIn(i18n.FR) != s.Name {
				t.Fatalf("NameIn(FR) = %q, Name = %q", s.NameIn(i18n.FR), s.Name)
			}
			if s.Kind == KindFortress {
				forts++
				en := s.NameIn(i18n.EN)
				if en == s.Name && !strings.HasPrefix(s.Name, "Fort ") && !strings.HasPrefix(s.Name, "Bastion ") {
					t.Fatalf("forteresse non traduite: %q", en)
				}
				if strings.HasPrefix(en, "Citadelle") || strings.HasPrefix(en, "Donjon") {
					t.Fatalf("préfixe français dans %q", en)
				}
			} else if s.NameIn(i18n.EN) != s.Name {
				t.Fatalf("%v: le nom propre %q ne doit pas changer", s.Kind, s.Name)
			}
			for _, n := range s.NPCs {
				npcs++
				if n.NameIn(i18n.FR) != n.Name || n.PersonaIn(i18n.FR) != n.Persona {
					t.Fatalf("version française incohérente: %q / %q", n.NameIn(i18n.FR), n.Name)
				}
				en, p := n.NameIn(i18n.EN), n.PersonaIn(i18n.EN)
				if p.Temperament == "" || p.Quirk == "" || p.Speech == "" || p == n.Persona {
					t.Fatalf("personnalité non traduite: %+v", p)
				}
				switch n.Kind {
				case NPCHealer:
					if !strings.HasPrefix(en, "Brother ") && !strings.HasPrefix(en, "Sister ") {
						t.Fatalf("soigneur %q", en)
					}
				case NPCMerchant:
					if !strings.HasPrefix(en, "Master ") && !strings.HasPrefix(en, "Dame ") {
						t.Fatalf("marchand %q", en)
					}
				}
				// Les prénoms accentués (Aélis…) sont des noms propres: seuls titres et épithètes changent.
				if rest := strings.Replace(en, firstNames[n.id.first].name, "", 1); strings.ContainsAny(rest, "éèêàçœ") {
					t.Fatalf("français dans %q", en)
				}
			}
		}
	}
	if forts == 0 || npcs == 0 {
		t.Fatalf("rien à vérifier: %d forteresses, %d PNJ", forts, npcs)
	}
}

// Les tirages d'identité sont bornés par des constantes: elles doivent suivre la taille des
// listes du catalogue, dans chaque langue, sans quoi un indice tomberait dans le vide.
func TestIdentityListsMatchTheCatalog(t *testing.T) {
	lists := map[string]int{
		"persona.temperament": numTemperaments, "persona.quirk": numQuirks, "persona.speech": numSpeeches,
		"name.epithet.m": numEpithets, "name.epithet.f": numEpithets, "place.fort": numFortPrefixes,
	}
	for _, l := range i18n.Langs {
		for prefix, want := range lists {
			if got := i18n.Count(l, prefix); got != want {
				t.Errorf("%s %s: %d entrées, attendu %d", l, prefix, got, want)
			}
		}
	}
}

// Aucun radical de nom n'est partagé par deux lieux voisins (rayon des rumeurs et des quêtes).
func TestPlaceStemsAreDistinctWithinClashRadius(t *testing.T) {
	for _, seed := range []int64{1, 2, 7, 42, 1234} {
		w := New(seed)
		type placed struct {
			rp   RegionPos
			base string
		}
		var all []placed
		for ry := -12; ry <= 12; ry++ {
			for rx := -12; rx <= 12; rx++ {
				rp := RegionPos{rx, ry}
				if s := w.StructureOfRegion(rp); s != nil {
					all = append(all, placed{rp, s.base})
				}
			}
		}
		for i, a := range all {
			for _, b := range all[i+1:] {
				if max(abs(a.rp.X-b.rp.X), abs(a.rp.Y-b.rp.Y)) <= nameClashRadius && a.base == b.base {
					t.Errorf("seed %d : %q à la fois en %v et en %v", seed, a.base, a.rp, b.rp)
				}
			}
		}
	}
}

func TestMostPlacesKeepTheirOriginalName(t *testing.T) {
	changed, total := 0, 0
	for _, seed := range []int64{1, 2, 7, 42} {
		for ry := -15; ry <= 15; ry++ {
			for rx := -15; rx <= 15; rx++ {
				rp := RegionPos{rx, ry}
				total++
				if baseFor(seed, rp) != rawStem(seed, rp).String() {
					changed++
				}
			}
		}
	}
	if ratio := float64(changed) / float64(total); ratio > 0.25 {
		t.Fatalf("%.1f %% des noms changent : trop de remplacements", 100*ratio)
	}
}

func TestStartVillageHasAStableName(t *testing.T) {
	a, b := New(1), New(1)
	pos := a.FindSpawn()
	sa, sb := a.StructureOfRegion(RegionOf(pos.X, pos.Y)), b.StructureOfRegion(RegionOf(pos.X, pos.Y))
	if sa == nil || sa.Name == "" || sa.Name != sb.Name {
		t.Fatalf("village de départ : %v / %v", sa, sb)
	}
}

func TestPlaceNamesAreDeterministic(t *testing.T) {
	a, b := New(5), New(5)
	for ry := -5; ry <= 5; ry++ {
		for rx := -5; rx <= 5; rx++ {
			rp := RegionPos{rx, ry}
			sa, sb := a.StructureOfRegion(rp), b.StructureOfRegion(rp)
			if (sa == nil) != (sb == nil) || (sa != nil && sa.Name != sb.Name) {
				t.Fatalf("noms différents en %v", rp)
			}
		}
	}
}

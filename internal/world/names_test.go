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
				t.Fatalf("seed %d %v: invalid place name %q", seed, s.Kind, s.Name)
			}
			for _, r := range s.Name {
				if !unicode.IsLetter(r) && r != '-' && r != ' ' {
					t.Fatalf("place name %q: unexpected character %q", s.Name, r)
				}
			}
			if n := len([]rune(s.Name)); n < 4 || n > 30 {
				t.Fatalf("place name %q: %d characters", s.Name, n)
			}
			names := map[string]bool{}
			for i, n := range s.NPCs {
				checked++
				if n.Name == "" || n.Region != s.Region || n.Index != i {
					t.Fatalf("NPC %d of %s: incomplete identity: %+v", i, s.Name, n)
				}
				if names[n.Name] {
					t.Fatalf("two NPCs of %s are called %q", s.Name, n.Name)
				}
				names[n.Name] = true
				p := n.Persona
				if p.Temperament == "" || p.Quirk == "" || p.Speech == "" {
					t.Fatalf("incomplete personality for %s: %+v", n.Name, p)
				}
				// The title reflects the trade.
				switch n.Kind {
				case NPCHealer:
					if !strings.HasPrefix(n.Name, "Frère ") && !strings.HasPrefix(n.Name, "Sœur ") {
						t.Fatalf("healer %q without religious title", n.Name)
					}
				case NPCMerchant:
					if !strings.HasPrefix(n.Name, "Maître ") && !strings.HasPrefix(n.Name, "Dame ") {
						t.Fatalf("merchant %q without trade title", n.Name)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no NPC checked")
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
				t.Fatalf("fortress named %q", s.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("no fortress")
	}
}

// Same identities for everyone, at every start and after emptying the cache.
func TestIdentitiesAreDeterministic(t *testing.T) {
	a, b := newTestWorld(42), newTestWorld(42)
	for ry := -4; ry <= 4; ry++ {
		for rx := -4; rx <= 4; rx++ {
			sa, sb := a.structureForRegion(RegionPos{rx, ry}), b.structureForRegion(RegionPos{rx, ry})
			if sa == nil {
				continue
			}
			if sa.Name != sb.Name {
				t.Fatalf("different name: %q / %q", sa.Name, sb.Name)
			}
			for i := range sa.NPCs {
				if sa.NPCs[i] != sb.NPCs[i] {
					t.Fatalf("different NPC: %+v / %+v", sa.NPCs[i], sb.NPCs[i])
				}
			}
		}
	}
	// Cache emptied: rebuilt identically.
	before := allStructures(a, 3)
	a.smu.Lock()
	clear(a.structs)
	a.smu.Unlock()
	after := allStructures(a, 3)
	for i := range before {
		if before[i].Name != after[i].Name || len(before[i].NPCs) != len(after[i].NPCs) {
			t.Fatal("different identity after rebuilding the cache")
		}
		for j := range before[i].NPCs {
			if before[i].NPCs[j] != after[i].NPCs[j] {
				t.Fatalf("NPC %d different after rebuilding", j)
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
		t.Fatalf("only %d distinct place names out of %d: too many repetitions", len(names), total)
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
		t.Fatalf("little variety: %d personalities, %d NPC names", len(personas), len(npcNames))
	}
}

// Identities do not depend on the order in which the regions are visited.
func TestIdentityDoesNotDependOnVisitOrder(t *testing.T) {
	forward, backward := newTestWorld(7), newTestWorld(7)
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
			t.Fatalf("region %v: identity depends on the visit order", r)
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
				t.Fatalf("key %+v, want %+v", k, NPCKey{s.Region, i})
			}
			if seen[k] {
				t.Fatalf("duplicate key: %+v", k)
			}
			seen[k] = true
			// The key does find the NPC.
			got := w.structureForRegion(k.Region).NPCs[k.Index]
			if got != n {
				t.Fatalf("key %+v does not find the NPC", k)
			}
			// NPCAt returns the complete identity.
			at, ok := w.NPCAt(n.Pos.X, n.Pos.Y)
			if !ok || at != n {
				t.Fatalf("NPCAt does not return the complete identity: %+v", at)
			}
		}
	}
	if count == 0 {
		t.Fatal("no NPC")
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
			t.Fatal("two different identity streams produce the same value")
		}
	}
	if identityRNG(1, RegionPos{3, -4}, 5).Uint64() != identityRNG(1, RegionPos{3, -4}, 5).Uint64() {
		t.Fatal("the identity stream is not deterministic")
	}
}

// Opposite or symmetric regions must never share an identity:
// with a mix by a simple XOR of products, (x, y) and (-x, -y) with odd
// coordinates gave the same stream, hence twin towns with the same NPCs.
func TestIdentityRNGHasNoRegionCollisions(t *testing.T) {
	seen := map[uint64]string{}
	for _, seed := range []int64{1, 2, 42} {
		for salt := uint64(0); salt < 4; salt++ {
			for ry := -12; ry <= 12; ry++ {
				for rx := -12; rx <= 12; rx++ {
					v := identityRNG(seed, RegionPos{rx, ry}, salt).Uint64()
					key := fmt.Sprintf("seed %d salt %d (%d,%d)", seed, salt, rx, ry)
					if prev, dup := seen[v]; dup {
						t.Fatalf("same stream for %s and %s", prev, key)
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
		t.Fatal("no pair of occupied symmetric regions")
	}
	// A few chance homonyms are normal (the tables are finite), not dozens.
	if same*10 > pairs {
		t.Fatalf("%d pairs of symmetric places out of %d bear the same name", same, pairs)
	}
}

// NPC and fortress names exist in both languages, without changing
// the identity (first names, places) nor the already played worlds.
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
					t.Fatalf("untranslated fortress: %q", en)
				}
				if strings.HasPrefix(en, "Citadelle") || strings.HasPrefix(en, "Donjon") {
					t.Fatalf("French prefix in %q", en)
				}
			} else if s.NameIn(i18n.EN) != s.Name {
				t.Fatalf("%v: the proper name %q must not change", s.Kind, s.Name)
			}
			for _, n := range s.NPCs {
				npcs++
				if n.NameIn(i18n.FR) != n.Name || n.PersonaIn(i18n.FR) != n.Persona {
					t.Fatalf("inconsistent French version: %q / %q", n.NameIn(i18n.FR), n.Name)
				}
				en, p := n.NameIn(i18n.EN), n.PersonaIn(i18n.EN)
				if p.Temperament == "" || p.Quirk == "" || p.Speech == "" || p == n.Persona {
					t.Fatalf("untranslated personality: %+v", p)
				}
				switch n.Kind {
				case NPCHealer:
					if !strings.HasPrefix(en, "Brother ") && !strings.HasPrefix(en, "Sister ") {
						t.Fatalf("healer %q", en)
					}
				case NPCMerchant:
					if !strings.HasPrefix(en, "Master ") && !strings.HasPrefix(en, "Dame ") {
						t.Fatalf("merchant %q", en)
					}
				}
				// Accented first names (Aélis...) are proper names: only titles and epithets change.
				if rest := strings.Replace(en, firstNames[n.id.first].name, "", 1); strings.ContainsAny(rest, "éèêàçœ") {
					t.Fatalf("French in %q", en)
				}
			}
		}
	}
	if forts == 0 || npcs == 0 {
		t.Fatalf("nothing to check: %d fortresses, %d NPCs", forts, npcs)
	}
}

// Identity draws are bounded by constants: they must follow the size of the
// catalogue lists, in each language, otherwise an index would fall into the void.
func TestIdentityListsMatchTheCatalog(t *testing.T) {
	lists := map[string]int{
		"persona.temperament": numTemperaments, "persona.quirk": numQuirks, "persona.speech": numSpeeches,
		"name.epithet.m": numEpithets, "name.epithet.f": numEpithets, "place.fort": numFortPrefixes,
	}
	for _, l := range i18n.Langs {
		for prefix, want := range lists {
			if got := i18n.Count(l, prefix); got != want {
				t.Errorf("%s %s: %d entries, want %d", l, prefix, got, want)
			}
		}
	}
}

// No name stem is shared by two neighbouring places (radius of rumours and quests).
func TestPlaceStemsAreDistinctWithinClashRadius(t *testing.T) {
	for _, seed := range []int64{1, 2, 7, 42, 1234} {
		w := newTestWorld(seed)
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
					t.Errorf("seed %d : %q both in %v and in %v", seed, a.base, a.rp, b.rp)
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
		t.Fatalf("%.1f %% of the names change: too many replacements", 100*ratio)
	}
}

func TestStartVillageHasAStableName(t *testing.T) {
	a, b := newTestWorld(1), newTestWorld(1)
	pos := a.FindSpawn()
	sa, sb := a.StructureOfRegion(RegionOf(pos.X, pos.Y)), b.StructureOfRegion(RegionOf(pos.X, pos.Y))
	if sa == nil || sa.Name == "" || sa.Name != sb.Name {
		t.Fatalf("starting village: %v / %v", sa, sb)
	}
}

func TestPlaceNamesAreDeterministic(t *testing.T) {
	a, b := newTestWorld(5), newTestWorld(5)
	for ry := -5; ry <= 5; ry++ {
		for rx := -5; rx <= 5; rx++ {
			rp := RegionPos{rx, ry}
			sa, sb := a.StructureOfRegion(rp), b.StructureOfRegion(rp)
			if (sa == nil) != (sb == nil) || (sa != nil && sa.Name != sb.Name) {
				t.Fatalf("different names in %v", rp)
			}
		}
	}
}

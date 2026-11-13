package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "game.db")
	s, err := Open(path, WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestAccountLifecycle(t *testing.T) {
	s, _ := openTemp(t)

	name, err := s.CreateAccount("Alice", "motdepasse1")
	if err != nil || name != "Alice" {
		t.Fatalf("création: %q, %v", name, err)
	}

	// Connexion: la casse saisie est ignorée, le pseudo enregistré est renvoyé.
	got, err := s.Authenticate("aLiCe", "motdepasse1")
	if err != nil || got != "Alice" {
		t.Fatalf("authentification: %q, %v", got, err)
	}

	// Mauvais mot de passe et compte inconnu: même erreur.
	if _, err := s.Authenticate("Alice", "mauvais-mdp"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mauvais mot de passe: %v", err)
	}
	if _, err := s.Authenticate("Nobody", "motdepasse1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("compte inconnu: %v", err)
	}
	// Les mots de passe sont sensibles à la casse.
	if _, err := s.Authenticate("Alice", "MOTDEPASSE1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("casse du mot de passe ignorée: %v", err)
	}
}

func TestDuplicateAccountIsCaseInsensitive(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("Alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Alice", "alice", "ALICE"} {
		if _, err := s.CreateAccount(n, "autremdp123"); !errors.Is(err, ErrExists) {
			t.Errorf("%q: erreur %v, attendu ErrExists", n, err)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	s, _ := openTemp(t)
	for _, pw := range []string{"", "court", strings.Repeat("x", 7), strings.Repeat("x", 73)} {
		if _, err := s.CreateAccount("bob", pw); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("mot de passe de %d octets accepté (%v)", len(pw), err)
		}
	}
	for _, pw := range []string{strings.Repeat("x", 8), strings.Repeat("x", 72)} {
		name := "u" + strings.Repeat("a", len(pw)%7)
		if _, err := s.CreateAccount(name, pw); err != nil {
			t.Errorf("mot de passe de %d octets refusé: %v", len(pw), err)
		}
	}
}

func TestPasswordIsHashedNotStored(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("alice", "secret-tres-long"); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := s.db.QueryRow(`SELECT pass_hash FROM accounts WHERE name = 'alice'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "secret") || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("le mot de passe n'est pas haché avec bcrypt: %q", hash)
	}
	// Deux comptes avec le même mot de passe ont des empreintes différentes (sel).
	if _, err := s.CreateAccount("bob", "secret-tres-long"); err != nil {
		t.Fatal(err)
	}
	var hash2 string
	_ = s.db.QueryRow(`SELECT pass_hash FROM accounts WHERE name = 'bob'`).Scan(&hash2)
	if hash == hash2 {
		t.Fatal("empreintes identiques: pas de sel")
	}
}

func TestCharacterRoundTripAndUpsert(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.LoadCharacter("alice"); c != nil || err != nil {
		t.Fatalf("personnage inexistant: %v, %v", c, err)
	}

	want := Character{Level: 4, XP: 12, Potions: 3, HP: 25, X: -120, Y: 77, Gold: 345,
		LLMConsent: true, Kills: 31, BossKills: 2, Deaths: 5}
	if err := s.SaveCharacters(map[string]Character{"alice": want}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCharacter("ALICE") // casse indifférente
	if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("relu: %+v, %v (attendu %+v)", got, err, want)
	}

	want.Level, want.X = 5, 3
	if err := s.SaveCharacters(map[string]Character{"alice": want}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.LoadCharacter("alice")
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("mise à jour: %+v", got)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM characters`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d lignes, attendu 1 (upsert)", n)
	}
}

func TestSaveUnknownAccountIsIgnored(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	err := s.SaveCharacters(map[string]Character{
		"fantome": {Level: 9},
		"alice":   {Level: 2, HP: 10},
	})
	if err != nil {
		t.Fatalf("un compte inconnu ne doit pas faire échouer le lot: %v", err)
	}
	if c, _ := s.LoadCharacter("fantome"); c != nil {
		t.Fatal("personnage créé pour un compte inexistant")
	}
	if c, _ := s.LoadCharacter("alice"); c == nil || c.Level != 2 {
		t.Fatalf("alice non enregistrée: %+v", c)
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCharacters(map[string]Character{"alice": {Level: 7, HP: 33, X: 1, Y: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("seed", "42"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path, WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.Authenticate("alice", "motdepasse1"); err != nil {
		t.Fatalf("compte perdu après réouverture: %v", err)
	}
	if c, _ := s2.LoadCharacter("alice"); c == nil || c.Level != 7 || c.HP != 33 {
		t.Fatalf("personnage perdu: %+v", c)
	}
	if v, ok, _ := s2.Meta("seed"); !ok || v != "42" {
		t.Fatalf("meta: %q %v", v, ok)
	}
}

func TestMetaAndMigrationVersion(t *testing.T) {
	s, _ := openTemp(t)
	if _, ok, err := s.Meta("absent"); ok || err != nil {
		t.Fatalf("clé absente: ok=%v err=%v", ok, err)
	}
	_ = s.SetMeta("k", "1")
	_ = s.SetMeta("k", "2") // écrase
	if v, _, _ := s.Meta("k"); v != "2" {
		t.Fatalf("meta = %q", v)
	}
	var version int
	_ = s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(migrations) {
		t.Fatalf("user_version=%d, attendu %d", version, len(migrations))
	}
}

func TestRefusesNewerDatabase(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "plus récente") {
		t.Fatalf("une base plus récente doit être refusée: %v", err)
	}
}

// --- Persister ---

func TestPersisterLoadSeesPendingBeforeFlush(t *testing.T) {
	s, _ := openTemp(t)
	_, _ = s.CreateAccount("alice", "motdepasse1")
	p := NewPersister(s, time.Hour, nil) // jamais de flush automatique
	defer p.Close()

	p.Save("alice", Character{Level: 3, HP: 20})
	c, err := p.Load("alice")
	if err != nil || c == nil || c.Level != 3 {
		t.Fatalf("Load doit voir la sauvegarde en attente: %+v, %v", c, err)
	}
	if db, _ := s.LoadCharacter("alice"); db != nil {
		t.Fatal("rien ne devrait encore être écrit en base")
	}
}

func TestPersisterLastWriteWinsAndCloseFlushes(t *testing.T) {
	s, _ := openTemp(t)
	_, _ = s.CreateAccount("alice", "motdepasse1")
	_, _ = s.CreateAccount("bob", "motdepasse2")
	p := NewPersister(s, time.Hour, nil)

	p.Save("alice", Character{Level: 1})
	p.Save("alice", Character{Level: 2})
	p.Save("alice", Character{Level: 3, HP: 9})
	p.Save("bob", Character{Level: 5})
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	if c, _ := s.LoadCharacter("alice"); c == nil || c.Level != 3 || c.HP != 9 {
		t.Fatalf("alice: %+v (la dernière version devait gagner)", c)
	}
	if c, _ := s.LoadCharacter("bob"); c == nil || c.Level != 5 {
		t.Fatalf("bob: %+v", c)
	}
}

func TestPersisterPeriodicFlush(t *testing.T) {
	s, _ := openTemp(t)
	_, _ = s.CreateAccount("alice", "motdepasse1")
	p := NewPersister(s, 20*time.Millisecond, nil)
	defer p.Close()

	p.Save("alice", Character{Level: 2})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, _ := s.LoadCharacter("alice"); c != nil && c.Level == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("l'écriture périodique n'a jamais eu lieu")
}

func TestPersisterSaveAfterCloseIsDropped(t *testing.T) {
	s, _ := openTemp(t)
	_, _ = s.CreateAccount("alice", "motdepasse1")
	p := NewPersister(s, time.Hour, nil)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p.Save("alice", Character{Level: 9}) // ne doit ni paniquer ni bloquer
	if err := p.Close(); err != nil {    // idempotent
		t.Fatal(err)
	}
	if c, _ := s.LoadCharacter("alice"); c != nil {
		t.Fatal("sauvegarde acceptée après fermeture")
	}
}

// Courses: écritures et lectures concurrentes pendant les flushs (go test -race).
func TestPersisterConcurrentUse(t *testing.T) {
	s, _ := openTemp(t)
	names := []string{"a1", "a2", "a3", "a4"}
	for _, n := range names {
		_, _ = s.CreateAccount(n, "motdepasse1")
	}
	p := NewPersister(s, 5*time.Millisecond, nil)

	var wg sync.WaitGroup
	for _, n := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 100; i++ {
				p.Save(n, Character{Level: i, HP: i})
				if c, err := p.Load(n); err != nil || c == nil || c.Level < 1 {
					t.Errorf("%s: lecture %+v, %v", n, c, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if c, _ := s.LoadCharacter(n); c == nil || c.Level != 100 {
			t.Errorf("%s: état final %+v, attendu niveau 100", n, c)
		}
	}
}

// --- Plafond de hachages bcrypt simultanés ---

func openCostly(t *testing.T, concurrency int) *Store {
	t.Helper()
	// Coût réel (10): assez long pour que des appels concurrents se chevauchent.
	s, err := Open(filepath.Join(t.TempDir(), "g.db"), WithHashConcurrency(concurrency))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHashConcurrencyIsCapped(t *testing.T) {
	s := openCostly(t, 2)
	s.peakHash.Store(0)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Compte inconnu: Authenticate fait un hachage factice, sans écriture.
			if _, err := s.Authenticate("inconnu", "motdepasse1"); !errors.Is(err, ErrInvalid) {
				t.Errorf("erreur inattendue: %v", err)
			}
		}()
	}
	wg.Wait()

	if peak := s.peakHash.Load(); peak < 1 || peak > 2 {
		t.Fatalf("pic de hachages simultanés = %d, plafond 2", peak)
	}
	if active := s.activeHash.Load(); active != 0 {
		t.Fatalf("%d hachages encore comptés actifs après la fin", active)
	}
}

func TestDuplicateRegistrationSkipsExpensiveHash(t *testing.T) {
	s := openCostly(t, 2)
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	s.peakHash.Store(0)

	start := time.Now()
	for i := 0; i < 20; i++ {
		if _, err := s.CreateAccount("Alice", "motdepasse1"); !errors.Is(err, ErrExists) {
			t.Fatalf("erreur: %v", err)
		}
	}
	if peak := s.peakHash.Load(); peak != 0 {
		t.Fatalf("%d hachages lancés pour des pseudos déjà pris, aucun attendu", peak)
	}
	// Garde-fou de temps: 20 hachages réels dureraient > 1 s.
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("20 refus ont pris %v: un hachage semble avoir lieu", d)
	}
}

func TestHashConcurrencyDefaultIsPositive(t *testing.T) {
	s, _ := openTemp(t)
	if cap(s.hashSem) < 1 {
		t.Fatalf("capacité par défaut %d", cap(s.hashSem))
	}
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatalf("la création doit fonctionner avec la limite par défaut: %v", err)
	}
}

// Une base créée avant l'ajout de la monnaie (version 3) doit migrer sans perdre
// de personnage, qui démarre alors avec 0 or.
func TestMigrationAddsGoldToExistingCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Fabrique une base à l'ancien schéma (migrations 1 à 3 seulement).
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:3] {
		if _, err := old.Exec(m); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO accounts(name, pass_hash, created_at) VALUES('alice', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO characters(account, level, xp, potions, hp, x, y, updated_at)
		VALUES('alice', 6, 3, 5, 40, 12, -9, 1)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path, WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatalf("migration depuis la version 3: %v", err)
	}
	defer s.Close()

	c, err := s.LoadCharacter("alice")
	if err != nil || c == nil {
		t.Fatalf("personnage perdu à la migration: %v, %v", c, err)
	}
	if c.Level != 6 || c.XP != 3 || c.Potions != 5 || c.HP != 40 || c.X != 12 || c.Y != -9 {
		t.Fatalf("données altérées: %+v", c)
	}
	if c.Gold != 0 {
		t.Fatalf("or = %d, attendu 0 pour un personnage antérieur à la monnaie", c.Gold)
	}

	// Et la nouvelle colonne est bien utilisée en écriture.
	if err := s.SaveCharacters(map[string]Character{"alice": {Level: 6, HP: 40, Gold: 77}}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.LoadCharacter("alice"); c.Gold != 77 {
		t.Fatalf("or non enregistré: %+v", c)
	}
}

// Une base créée avant les dialogues de PNJ (version 4) doit migrer sans perdre de
// personnage : accord refusé par défaut, compteurs à zéro, nouvelles tables vides.
func TestMigrationFromV4AddsConsentFactsAndLoreTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range migrations[:4] {
		if _, err := old.Exec(m); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 4`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO accounts(name, pass_hash, created_at) VALUES('alice', 'x', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO characters(account, level, xp, potions, hp, x, y, updated_at, gold)
		VALUES('alice', 9, 4, 3, 50, 7, 8, 1, 120)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path, WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatalf("migration depuis la version 4: %v", err)
	}
	defer s.Close()

	c, err := s.LoadCharacter("alice")
	if err != nil || c == nil {
		t.Fatalf("personnage perdu: %v, %v", c, err)
	}
	if c.Level != 9 || c.Gold != 120 || c.X != 7 || c.Y != 8 {
		t.Fatalf("données altérées: %+v", c)
	}
	if c.LLMConsent || c.Kills != 0 || c.BossKills != 0 || c.Deaths != 0 {
		t.Fatalf("valeurs par défaut incorrectes: %+v", c)
	}

	// Les nouvelles tables existent, vides, et sont utilisables.
	for _, table := range []string{"npc_cards", "npc_memories"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("table %s: %d lignes, %v", table, n, err)
		}
	}
	var version int
	_ = s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(migrations) {
		t.Fatalf("version %d, attendue %d", version, len(migrations))
	}
}

func TestDeletingAnAccountCascadesToItsLore(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("alice", "motdepasse1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO npc_cards(account, region_x, region_y, alias, role, origin, goal, trait, level, boss_kills, deaths, created_at, updated_at)
		VALUES('alice', 0, 0, 'a', 'r', 'o', 'g', 't', 1, 0, 0, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO npc_memories(region_x, region_y, npc_index, account, summary, updated_at) VALUES(0, 0, 0, 'alice', 's', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM accounts WHERE name = 'alice'`); err != nil {
		t.Fatal(err)
	}
	var cards, mems int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM npc_cards`).Scan(&cards)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM npc_memories`).Scan(&mems)
	if cards != 0 || mems != 0 {
		t.Fatalf("données orphelines après suppression du compte: %d fiches, %d souvenirs", cards, mems)
	}
}

func sampleQuests() []quest.Quest {
	t0 := time.Unix(1_700_000_000, 0)
	return []quest.Quest{
		{ID: 1, Giver: world.NPCKey{Region: world.RegionPos{X: 1, Y: -2}, Index: 3}, Title: "Les loups", Story: "Chassez-les.",
			Objective: quest.Objective{Kind: quest.Kill, Species: "wolf", Count: 5}, Progress: 2,
			Reward: quest.Reward{Gold: 50, XP: 10, Potions: 1, Note: "merci"}, Tier: 2, State: quest.Active, CreatedAt: t0},
		{ID: 2, Giver: world.NPCKey{Region: world.RegionPos{X: 1, Y: -2}, Index: 3}, Title: "Message", Story: "Allez-y.",
			Objective: quest.Objective{Kind: quest.Reach, Count: 1, Target: world.RegionPos{X: 3, Y: 0}}, Progress: 1,
			Reward: quest.Reward{Gold: 20}, Tier: 1, State: quest.Done, CreatedAt: t0, DoneAt: t0.Add(time.Hour)},
		{ID: 3, Title: "Abandonnée", Story: "x", State: quest.Abandoned, CreatedAt: t0},
	}
}

func TestNegativeReputationRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("brigand", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	rp := world.RegionPos{X: 3, Y: 3}
	c := Character{Level: 2, HP: 10, Rep: map[world.RegionPos]int{rp: -12}, QuestsDirty: true}
	if err := st.SaveCharacters(map[string]Character{"brigand": c}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("brigand")
	if err != nil || got == nil || got.Rep[rp] != -12 || len(got.Rep) != 1 {
		t.Fatalf("réputation négative perdue: %+v %v", got, err)
	}
}

func TestQuestsAndReputationRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	rep := map[world.RegionPos]int{{X: 1, Y: -2}: 5}
	c := Character{Level: 3, HP: 10, Quests: sampleQuests(), Rep: rep, QuestsDirty: true}
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if len(got.Quests) != 2 { // l'abandonnée n'est pas conservée
		t.Fatalf("%d quêtes: %+v", len(got.Quests), got.Quests)
	}
	want := sampleQuests()
	for i := range got.Quests {
		g, w := got.Quests[i], want[i]
		g.Account = ""
		g.CreatedAt, w.CreatedAt = g.CreatedAt.UTC(), w.CreatedAt.UTC()
		g.DoneAt, w.DoneAt = g.DoneAt.UTC(), w.DoneAt.UTC()
		if g != w {
			t.Errorf("quête %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
	if got.Rep[world.RegionPos{X: 1, Y: -2}] != 5 || len(got.Rep) != 1 {
		t.Fatalf("réputation: %v", got.Rep)
	}

	// Une sauvegarde non « sale » ne touche pas aux tables de quêtes.
	c.Quests, c.Rep, c.QuestsDirty, c.Gold = nil, nil, false, 99
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	again, _ := st.LoadCharacter("aldric")
	if len(again.Quests) != 2 || again.Gold != 99 {
		t.Fatalf("quêtes perdues par une sauvegarde sans changement : %+v", again)
	}

	// DeedsNear ne voit que les quêtes rendues, dans le carré et la fenêtre de temps.
	t0 := time.Unix(1_700_000_000, 0)
	deeds, err := st.DeedsNear(context.Background(), world.RegionPos{X: 2, Y: -1}, 3, t0, 10)
	if err != nil || len(deeds) != 1 || deeds[0].Title != "Message" || deeds[0].Account != "aldric" {
		t.Fatalf("%+v %v", deeds, err)
	}
	if d, _ := st.DeedsNear(context.Background(), world.RegionPos{X: 9, Y: 9}, 3, t0, 10); len(d) != 0 {
		t.Fatalf("hors carré: %+v", d)
	}
	if d, _ := st.DeedsNear(context.Background(), world.RegionPos{X: 1, Y: -2}, 3, t0.Add(2*time.Hour), 10); len(d) != 0 {
		t.Fatalf("trop ancien: %+v", d)
	}

	// Purge d'un lieu puis d'un compte.
	_, _, n, err := st.PurgePlace(context.Background(), world.RegionPos{X: 1, Y: -2})
	if err != nil || n != 2 {
		t.Fatalf("purge du lieu: %d %v", n, err)
	}
	if after, _ := st.LoadCharacter("aldric"); len(after.Quests) != 0 || len(after.Rep) != 0 {
		t.Fatalf("après purge: %+v", after)
	}
}

func TestGearRoundTripAndWares(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	origin := world.NPCKey{Region: world.RegionPos{X: 2, Y: -1}, Index: 6}
	inv := gear.Inventory{
		Items: []gear.Item{
			{ID: 1, Name: "Épée longue", Desc: "Bien équilibrée.", Slot: gear.Weapon, Tier: 2, Family: "longsword", Quality: 1, Price: 130, Kind: hero.Melee, Origin: origin},
			{ID: 2, Name: "Casque de fer", Slot: gear.Helm, Tier: 1, Def: 1, Price: 30, Origin: origin},
			{ID: 3, Name: "Arc long", Slot: gear.Weapon, Tier: 1, Family: "shortbow", Price: 40, Kind: hero.Ranged, Origin: origin},
		},
		Worn: map[gear.Slot]int64{gear.Weapon: 1},
	}
	c := Character{Level: 5, HP: 10, Gear: inv, GearDirty: true}
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if !reflect.DeepEqual(got.Gear, inv) {
		t.Fatalf("sac relu:\n got %+v\nwant %+v", got.Gear, inv)
	}
	// Une sauvegarde sans changement de sac ne le réécrit pas.
	c.Gear, c.GearDirty = gear.Inventory{}, false
	_ = st.SaveCharacters(map[string]Character{"aldric": c})
	if again, _ := st.LoadCharacter("aldric"); len(again.Gear.Items) != 3 {
		t.Fatalf("sac perdu: %+v", again.Gear)
	}
	// Purger le compte emporte le sac.
	if _, _, _, err := st.PurgeAccount(context.Background(), "aldric"); err != nil {
		t.Fatal(err)
	}
	if after, _ := st.LoadCharacter("aldric"); len(after.Gear.Items) != 0 {
		t.Fatalf("sac après purge: %+v", after.Gear)
	}

	// Assortiments.
	ctx := context.Background()
	w := gear.Wares{NPC: origin, Specialty: "lames courtes", Items: inv.Items, Version: 1, CreatedAt: time.Unix(1_700_000_000, 0)}
	if _, ok, _ := st.GetWares(ctx, origin); ok {
		t.Fatal("assortiment fantôme")
	}
	if err := st.PutWares(ctx, w); err != nil {
		t.Fatal(err)
	}
	back, ok, err := st.GetWares(ctx, origin)
	if err != nil || !ok || back.Specialty != "lames courtes" || len(back.Items) != 3 || back.Items[0].Name != "Épée longue" {
		t.Fatalf("%+v %v %v", back, ok, err)
	}
	w.Specialty = "haches"
	_ = st.PutWares(ctx, w)
	if list, _ := st.ListWares(ctx); len(list) != 1 || list[0].Specialty != "haches" {
		t.Fatalf("liste: %+v", list)
	}
	if ok, _ := st.DeleteWares(ctx, origin); !ok {
		t.Fatal("suppression")
	}
	if ok, _ := st.DeleteWares(ctx, origin); ok {
		t.Fatal("double suppression")
	}
}

func TestHeroSheetRoundTripAndNeedsCreationForOldRows(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	// Un personnage d'avant la création n'a aucune caractéristique.
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 4, HP: 9}}); err != nil {
		t.Fatal(err)
	}
	old, _ := st.LoadCharacter("aldric")
	if old.Scores != [6]int{} || old.Class != "" || old.MaxHP != 0 {
		t.Fatalf("ancien personnage: %+v", old)
	}
	c := Character{Level: 4, HP: 9, Class: "mage", Scores: [6]int{8, 12, 17, 9, 14, 11}, MaxHP: 21, Mana: 7}
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.LoadCharacter("aldric")
	if got.Class != "mage" || got.Scores != c.Scores || got.MaxHP != 21 || got.Mana != 7 {
		t.Fatalf("fiche relue: %+v", got)
	}
}

func TestDiscoveriesRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount("brunhild", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	found := []world.RegionPos{{X: 4, Y: -2}, {X: -1, Y: 3}, {X: 0, Y: -1}}
	c := Character{Level: 2, HP: 10, Discoveries: found, DiscoveriesDirty: true}
	if err := st.SaveCharacters(map[string]Character{"aldric": c, "brunhild": {Level: 1, HP: 5}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	want := []world.RegionPos{{X: 4, Y: -2}, {X: 0, Y: -1}, {X: -1, Y: 3}} // triés par Y puis X
	if len(got.Discoveries) != len(want) {
		t.Fatalf("découvertes %v, attendu %v", got.Discoveries, want)
	}
	for i := range want {
		if got.Discoveries[i] != want[i] {
			t.Errorf("découverte %d: %v, attendu %v", i, got.Discoveries[i], want[i])
		}
	}
	if other, _ := st.LoadCharacter("brunhild"); len(other.Discoveries) != 0 {
		t.Errorf("les découvertes d'un compte fuient chez un autre: %v", other.Discoveries)
	}

	// Une sauvegarde non « sale » n'efface rien, une sauvegarde « sale » remplace la liste.
	c.Discoveries, c.DiscoveriesDirty, c.Gold = nil, false, 7
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.LoadCharacter("aldric"); len(again.Discoveries) != 3 || again.Gold != 7 {
		t.Fatalf("découvertes perdues par une sauvegarde sans changement: %+v", again)
	}
	c.Discoveries, c.DiscoveriesDirty = append(found, world.RegionPos{X: 9, Y: 9}), true
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.LoadCharacter("aldric"); len(again.Discoveries) != 4 {
		t.Fatalf("la nouvelle découverte n'a pas été écrite: %v", again.Discoveries)
	}
}

// Deux sauvegardes se succèdent avant l'écriture: si la première modifiait les découvertes et pas la seconde, la
// version retenue doit quand même les réécrire.
func TestPersisterKeepsDiscoveriesDirtyAcrossSaves(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	p := NewPersister(s, time.Hour, nil)
	found := []world.RegionPos{{X: 2, Y: 5}}
	p.Save("aldric", Character{Level: 1, HP: 5, Discoveries: found, DiscoveriesDirty: true})
	p.Save("aldric", Character{Level: 2, HP: 5, Discoveries: found}) // plus récente, sans drapeau
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if got.Level != 2 || len(got.Discoveries) != 1 || got.Discoveries[0] != found[0] {
		t.Errorf("découverte perdue entre deux sauvegardes: %+v", got)
	}
}

func TestDungeonQuestRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(1_700_000_000, 0)
	q := quest.Quest{ID: 1, Giver: world.NPCKey{Region: world.RegionPos{X: 0, Y: 0}, Index: 2}, Title: "Le maître", Story: "Allez-y.",
		Objective: quest.Objective{Kind: quest.Dungeon, Species: "lich", Count: 1, Target: world.RegionPos{X: 0, Y: -1}},
		Reward:    quest.Reward{Gold: 250, XP: 40, Potions: 2, Note: "prudence"}, Tier: 3, State: quest.Ready, Progress: 1, CreatedAt: t0}
	c := Character{Level: 6, HP: 10, Quests: []quest.Quest{q}, QuestsDirty: true}
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil || len(got.Quests) != 1 {
		t.Fatal(got, err)
	}
	g := got.Quests[0]
	g.Account, g.CreatedAt, q.CreatedAt = "", g.CreatedAt.UTC(), q.CreatedAt.UTC()
	if g != q {
		t.Errorf("quête de donjon:\n got %+v\nwant %+v", g, q)
	}
}

func TestRespawnAndStonesRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	stones := []world.RegionPos{{X: 4, Y: -2}, {X: -1, Y: 3}}
	c := Character{Level: 2, HP: 10, RespawnSet: true, RespawnX: -17, RespawnY: 230, Stones: stones}
	if err := st.SaveCharacters(map[string]Character{"aldric": c}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if !got.RespawnSet || got.RespawnX != -17 || got.RespawnY != 230 {
		t.Errorf("réapparition %+v", got)
	}
	if len(got.Stones) != 2 || got.Stones[0] != stones[0] || got.Stones[1] != stones[1] {
		t.Errorf("pierres %v, attendu %v", got.Stones, stones)
	}

	// Sans feu ni pierre, rien ne fuit d'une sauvegarde à l'autre.
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10}}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.LoadCharacter("aldric"); again.RespawnSet || len(again.Stones) != 0 {
		t.Errorf("état de points d'intérêt non effacé: %+v", again)
	}
}

func TestMapsRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	maps := []world.Pos{{X: 120, Y: -45}, {X: -3, Y: 8}}
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10, Maps: maps}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if len(got.Maps) != 2 || got.Maps[0] != maps[0] || got.Maps[1] != maps[1] {
		t.Fatalf("cartes %v, attendu %v", got.Maps, maps)
	}
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10}}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.LoadCharacter("aldric"); len(again.Maps) != 0 {
		t.Errorf("cartes non effacées: %v", again.Maps)
	}
}

func TestResourcesRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	res := map[string]int{"iron": 3, "oak": 12, "sage": 1}
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10, Resources: res}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
	if len(got.Resources) != 3 || got.Resources["iron"] != 3 || got.Resources["oak"] != 12 || got.Resources["sage"] != 1 {
		t.Fatalf("matières %v, attendu %v", got.Resources, res)
	}
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10}}); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.LoadCharacter("aldric"); len(again.Resources) != 0 {
		t.Errorf("matières non effacées: %v", again.Resources)
	}
}

func TestDecodeResourcesIgnoresGarbage(t *testing.T) {
	got := decodeResources("iron:3;;bad;oak:-2;x:abc;sage:99999")
	if len(got) != 2 || got["iron"] != 3 || got["sage"] != maxStoredQty {
		t.Errorf("décodage %v", got)
	}
}

func TestSkillsRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.CreateAccount("aldric", "un-bon-mot-de-passe"); err != nil {
		t.Fatal(err)
	}
	sk := map[string]int{"wood": 120, "fish": 7}
	if err := st.SaveCharacters(map[string]Character{"aldric": {Level: 2, HP: 10, Skills: sk}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadCharacter("aldric")
	if err != nil || got == nil || got.Skills["wood"] != 120 || got.Skills["fish"] != 7 || len(got.Skills) != 2 {
		t.Fatalf("métiers relus %+v (%v)", got, err)
	}
}

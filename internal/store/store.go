// Package store persiste les comptes et les personnages dans SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite" // pilote SQLite en Go pur (pas de CGO)

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Erreurs renvoyées par le magasin.
var (
	ErrExists       = errors.New("ce pseudo existe déjà")
	ErrInvalid      = errors.New("identifiants invalides") // inconnu ou mauvais mot de passe: volontairement indistinguable
	ErrWeakPassword = errors.New("mot de passe trop court ou trop long")
)

const (
	minPassword = 8
	maxPassword = 72 // limite de bcrypt
)

// Character est l'état sauvegardé d'un personnage. Les caractéristiques de
// combat se déduisent du niveau (voir combat.StatsAt).
type Character struct {
	Level, XP, Potions int
	HP                 int

	// Création du personnage (voir internal/hero). Scores à zéro: la création reste à faire.
	// MaxHP est la somme des dés de vie (0: à recalculer), Mana la réserve actuelle.
	Class  string
	Scores [6]int
	MaxHP  int
	Mana   int
	X, Y   int
	Gold   int

	// Accord du joueur pour l'envoi de ses messages à un service de langage
	// tiers (voir le dialogue des PNJ), et faits établis par le serveur que les
	// PNJ peuvent invoquer.
	LLMConsent bool
	Kills      int
	BossKills  int
	Deaths     int

	// Quêtes en cours et rendues, et réputation par lieu (voir internal/quest).
	// QuestsDirty indique qu'elles ont changé depuis la dernière sauvegarde: seules
	// les sauvegardes qui les modifient réécrivent leurs tables.
	Quests      []quest.Quest
	Rep         map[world.RegionPos]int
	QuestsDirty bool

	// Sac d'équipement et objets portés (voir internal/gear). GearDirty : à réécrire.
	Gear      gear.Inventory
	GearDirty bool
}

// Store est la base de données.
type Store struct {
	db   *sql.DB
	cost int

	// hashSem borne le nombre de hachages bcrypt simultanés: chacun occupe un
	// cœur pendant des dizaines de millisecondes, et des centaines de
	// connexions simultanées affameraient sinon la boucle de jeu.
	hashSem    chan struct{}
	activeHash atomic.Int32 // hachages en cours (pour les tests)
	peakHash   atomic.Int32 // maximum observé (pour les tests)

	// dummyHash sert à égaliser le temps de réponse quand le compte n'existe pas,
	// pour ne pas révéler quels pseudos existent.
	dummyHash []byte
}

// Option configure Open.
type Option func(*Store)

// WithBcryptCost règle le coût de bcrypt (à réduire uniquement dans les tests).
func WithBcryptCost(cost int) Option { return func(s *Store) { s.cost = cost } }

// WithHashConcurrency borne les hachages bcrypt simultanés (défaut: la moitié des cœurs).
func WithHashConcurrency(n int) Option {
	return func(s *Store) { s.hashSem = make(chan struct{}, max(1, n)) }
}

// Open ouvre (et migre si besoin) la base à path.
func Open(path string, opts ...Option) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Une seule connexion: SQLite n'a qu'un écrivain, et cela évite les « database is locked ».
	db.SetMaxOpenConns(1)

	s := &Store{db: db, cost: bcrypt.DefaultCost}
	for _, o := range opts {
		o(s)
	}
	if s.hashSem == nil {
		s.hashSem = make(chan struct{}, max(1, runtime.NumCPU()/2))
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	s.dummyHash, err = bcrypt.GenerateFromPassword([]byte("inutilisé"), s.cost)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close ferme la base.
func (s *Store) Close() error { return s.db.Close() }

// migrations est la liste ordonnée des évolutions du schéma. N'en modifier
// aucune après publication: en ajouter une nouvelle.
var migrations = []string{
	`CREATE TABLE meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`CREATE TABLE accounts (
		name       TEXT PRIMARY KEY COLLATE NOCASE,
		pass_hash  TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE characters (
		account    TEXT PRIMARY KEY COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		level      INTEGER NOT NULL,
		xp         INTEGER NOT NULL,
		potions    INTEGER NOT NULL,
		hp         INTEGER NOT NULL,
		x          INTEGER NOT NULL,
		y          INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	// 4: monnaie du jeu. Les personnages existants démarrent sans or.
	`ALTER TABLE characters ADD COLUMN gold INTEGER NOT NULL DEFAULT 0`,
	// 5 à 8: accord pour les dialogues avec un modèle de langage, et faits vérifiés.
	`ALTER TABLE characters ADD COLUMN llm_consent INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN kills INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN boss_kills INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN deaths INTEGER NOT NULL DEFAULT 0`,
	// 9: présentations des joueurs, une par (compte, lieu). Les faits (level…) sont
	// établis par le serveur; le reste est dit par le joueur et reste non vérifié.
	`CREATE TABLE npc_cards (
		account    TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		region_x   INTEGER NOT NULL,
		region_y   INTEGER NOT NULL,
		alias      TEXT    NOT NULL,
		role       TEXT    NOT NULL,
		origin     TEXT    NOT NULL,
		goal       TEXT    NOT NULL,
		trait      TEXT    NOT NULL,
		level      INTEGER NOT NULL,
		boss_kills INTEGER NOT NULL,
		deaths     INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (account, region_x, region_y)
	)`,
	`CREATE INDEX npc_cards_place ON npc_cards(region_x, region_y, updated_at)`,
	// 11: souvenir privé d'un PNJ sur un joueur (jamais montré à un autre joueur).
	`CREATE TABLE npc_memories (
		region_x   INTEGER NOT NULL,
		region_y   INTEGER NOT NULL,
		npc_index  INTEGER NOT NULL,
		account    TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		summary    TEXT    NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (region_x, region_y, npc_index, account)
	)`,
	// 12: quêtes des joueurs et réputation par lieu. Les quêtes rendues (state = 'done')
	// servent aussi de mémoire aux PNJ (rumeurs).
	`CREATE TABLE quests (
		account    TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		id         INTEGER NOT NULL,
		giver_rx   INTEGER NOT NULL,
		giver_ry   INTEGER NOT NULL,
		giver_idx  INTEGER NOT NULL,
		title      TEXT    NOT NULL,
		story      TEXT    NOT NULL,
		kind       TEXT    NOT NULL,
		species    TEXT    NOT NULL,
		count      INTEGER NOT NULL,
		target_rx  INTEGER NOT NULL,
		target_ry  INTEGER NOT NULL,
		progress   INTEGER NOT NULL,
		gold       INTEGER NOT NULL,
		xp         INTEGER NOT NULL,
		potions    INTEGER NOT NULL,
		note       TEXT    NOT NULL,
		tier       INTEGER NOT NULL,
		state      TEXT    NOT NULL,
		created_at INTEGER NOT NULL,
		done_at    INTEGER NOT NULL,
		PRIMARY KEY (account, id)
	)`,
	`CREATE INDEX quests_done_place ON quests(giver_rx, giver_ry, state, done_at)`,
	`CREATE TABLE reputation (
		account TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		region_x INTEGER NOT NULL,
		region_y INTEGER NOT NULL,
		points   INTEGER NOT NULL,
		PRIMARY KEY (account, region_x, region_y)
	)`,
	// 13: sac d'équipement des joueurs et assortiments générés des vendeurs.
	`CREATE TABLE items (
		account    TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		id         INTEGER NOT NULL,
		slot       TEXT    NOT NULL,
		name       TEXT    NOT NULL,
		descr      TEXT    NOT NULL,
		tier       INTEGER NOT NULL,
		atk        INTEGER NOT NULL,
		def        INTEGER NOT NULL,
		price      INTEGER NOT NULL,
		origin_rx  INTEGER NOT NULL,
		origin_ry  INTEGER NOT NULL,
		origin_idx INTEGER NOT NULL,
		worn       INTEGER NOT NULL,
		PRIMARY KEY (account, id)
	)`,
	`CREATE TABLE npc_wares (
		region_x    INTEGER NOT NULL,
		region_y    INTEGER NOT NULL,
		npc_index   INTEGER NOT NULL,
		specialty   TEXT    NOT NULL,
		items       TEXT    NOT NULL,
		version     INTEGER NOT NULL,
		created_at  INTEGER NOT NULL,
		PRIMARY KEY (region_x, region_y, npc_index)
	)`,
	// 14: classe, caractéristiques tirées à la création (toutes à 0: création à faire), PV maximaux
	// (somme des dés de vie), mana, et style des armes du sac.
	`ALTER TABLE characters ADD COLUMN class TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN str INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN agi INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN int_ INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN con INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN wis INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN cha INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN max_hp INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN mana INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE items ADD COLUMN kind TEXT NOT NULL DEFAULT ''`,
	// 15: combat par dés. Un objet a une famille (type de dégâts ou d'armure) et une qualité (bonus aux dégâts
	// d'une arme) ; la colonne atk n'est plus utilisée. Les anciens objets (famille vide) sont convertis à la
	// lecture par gear.Sanitize.
	`ALTER TABLE items ADD COLUMN family TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE items ADD COLUMN quality INTEGER NOT NULL DEFAULT 0`,
}

// migrate applique les migrations manquantes, chacune dans sa transaction.
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("base plus récente que ce programme (version %d, connue %d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA n'accepte pas de paramètre: i+1 est un entier de confiance.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Meta lit une valeur de la table meta.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetMeta écrit une valeur dans la table meta.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// withHashSlot exécute fn en occupant l'un des créneaux de hachage.
func (s *Store) withHashSlot(fn func()) {
	s.hashSem <- struct{}{}
	n := s.activeHash.Add(1)
	for { // maximum observé, sans verrou
		cur := s.peakHash.Load()
		if n <= cur || s.peakHash.CompareAndSwap(cur, n) {
			break
		}
	}
	defer func() {
		s.activeHash.Add(-1)
		<-s.hashSem
	}()
	fn()
}

// CreateAccount crée un compte et renvoie le pseudo tel qu'enregistré.
func (s *Store) CreateAccount(name, password string) (string, error) {
	if l := len(password); l < minPassword || l > maxPassword {
		return "", ErrWeakPassword
	}
	// Pseudo déjà pris: on refuse avant de dépenser un hachage coûteux. La
	// contrainte UNIQUE ci-dessous reste l'arbitre en cas de course.
	var taken int
	err := s.db.QueryRow(`SELECT 1 FROM accounts WHERE name = ?`, name).Scan(&taken)
	if err == nil {
		return "", ErrExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	var hash []byte
	s.withHashSlot(func() { hash, err = bcrypt.GenerateFromPassword([]byte(password), s.cost) })
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO accounts(name, pass_hash, created_at) VALUES(?, ?, ?)`,
		name, string(hash), time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrExists
		}
		return "", err
	}
	return name, nil
}

// Authenticate vérifie un mot de passe et renvoie le pseudo tel qu'enregistré
// (la casse saisie peut différer). Un pseudo inconnu et un mauvais mot de
// passe donnent la même erreur, après un temps comparable.
func (s *Store) Authenticate(name, password string) (string, error) {
	var canonical, hash string
	err := s.db.QueryRow(`SELECT name, pass_hash FROM accounts WHERE name = ?`, name).Scan(&canonical, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		s.withHashSlot(func() { _ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password)) })
		return "", ErrInvalid
	}
	if err != nil {
		return "", err
	}
	var mismatch bool
	s.withHashSlot(func() { mismatch = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil })
	if mismatch {
		return "", ErrInvalid
	}
	return canonical, nil
}

// LoadCharacter renvoie le personnage d'un compte, ou nil s'il n'en a pas encore.
func (s *Store) LoadCharacter(account string) (*Character, error) {
	var c Character
	var consent int
	err := s.db.QueryRow(`SELECT level, xp, potions, hp, x, y, gold, llm_consent, kills, boss_kills, deaths,
			class, str, agi, int_, con, wis, cha, max_hp, mana
		FROM characters WHERE account = ?`, account).
		Scan(&c.Level, &c.XP, &c.Potions, &c.HP, &c.X, &c.Y, &c.Gold, &consent, &c.Kills, &c.BossKills, &c.Deaths,
			&c.Class, &c.Scores[0], &c.Scores[1], &c.Scores[2], &c.Scores[3], &c.Scores[4], &c.Scores[5], &c.MaxHP, &c.Mana)
	c.LLMConsent = consent != 0
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c.Quests, c.Rep, err = s.loadQuests(account); err != nil {
		return nil, err
	}
	if c.Gear, err = s.loadGear(account); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCharacters enregistre plusieurs personnages en une seule transaction
// (tout ou rien). Les comptes inconnus sont ignorés plutôt que de faire échouer le lot.
func (s *Store) SaveCharacters(chars map[string]Character) error {
	if len(chars) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO characters(account, level, xp, potions, hp, x, y, gold,
			llm_consent, kills, boss_kills, deaths, class, str, agi, int_, con, wis, cha, max_hp, mana, updated_at)
		SELECT name, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM accounts WHERE name = ?
		ON CONFLICT(account) DO UPDATE SET
			level = excluded.level, xp = excluded.xp, potions = excluded.potions,
			hp = excluded.hp, x = excluded.x, y = excluded.y, gold = excluded.gold,
			llm_consent = excluded.llm_consent, kills = excluded.kills,
			boss_kills = excluded.boss_kills, deaths = excluded.deaths,
			class = excluded.class, str = excluded.str, agi = excluded.agi, int_ = excluded.int_,
			con = excluded.con, wis = excluded.wis, cha = excluded.cha,
			max_hp = excluded.max_hp, mana = excluded.mana,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for account, c := range chars {
		if _, err := stmt.Exec(c.Level, c.XP, c.Potions, c.HP, c.X, c.Y, c.Gold,
			boolToInt(c.LLMConsent), c.Kills, c.BossKills, c.Deaths,
			c.Class, c.Scores[0], c.Scores[1], c.Scores[2], c.Scores[3], c.Scores[4], c.Scores[5], c.MaxHP, c.Mana,
			now, account); err != nil {
			return fmt.Errorf("sauvegarde de %s: %w", account, err)
		}
		if c.QuestsDirty {
			if err := saveQuests(tx, account, c); err != nil {
				return fmt.Errorf("quêtes de %s: %w", account, err)
			}
		}
		if c.GearDirty {
			if err := saveGear(tx, account, c.Gear); err != nil {
				return fmt.Errorf("équipement de %s: %w", account, err)
			}
		}
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

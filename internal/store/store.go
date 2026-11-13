// Package store persists accounts and characters in SQLite.
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
	_ "modernc.org/sqlite" // pure Go SQLite driver (no CGO)

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Errors returned by the store.
var (
	ErrExists       = errors.New("username already taken")
	ErrInvalid      = errors.New("invalid credentials") // unknown or wrong password: deliberately indistinguishable
	ErrWeakPassword = errors.New("password too short or too long")
)

const (
	minPassword = 8
	maxPassword = 72 // bcrypt limit
)

// Character is the saved state of a character. Combat stats are
// derived from the level (see combat.StatsAt).
type Character struct {
	Level, XP, Potions int
	HP                 int

	// Character creation (see internal/hero). Zero scores: creation is still to be done.
	// MaxHP is the sum of the hit dice (0: to be recomputed), Mana the current pool.
	Class  string
	Scores [6]int
	MaxHP  int
	Mana   int
	X, Y   int
	Gold   int

	// Player's consent to send their messages to a third-party language
	// service (see NPC dialogue), and facts established by the server that
	// NPCs may invoke.
	LLMConsent bool
	Kills      int
	BossKills  int
	Deaths     int

	// Ongoing and handed-in quests, and reputation per place (see internal/quest).
	// QuestsDirty tells that they changed since the last save: only
	// saves that modify them rewrite their tables.
	Quests      []quest.Quest
	Rep         map[world.RegionPos]int
	QuestsDirty bool

	// Equipment bag and worn items (see internal/gear). GearDirty: to be rewritten.
	Gear      gear.Inventory
	GearDirty bool

	// Dungeons whose entrance the character has discovered, identified by the region that hides them (see
	// internal/world). DiscoveriesDirty: to be rewritten.
	Discoveries      []world.RegionPos
	DiscoveriesDirty bool

	// Respawn point (campfire) and activated standing stones, identified by their region. They fit
	// in the character row: they are rewritten on every save, without a flag.
	RespawnSet bool
	RespawnX   int
	RespawnY   int
	Stones     []world.RegionPos
	Maps       []world.Pos    // treasure maps held: the tile to dig for each one
	Resources  map[string]int // materials bag: quantity per resource identifier
	Skills     map[string]int // accumulated experience of gathering jobs, per category
}

// Store is the database.
type Store struct {
	db   *sql.DB
	cost int

	// hashSem bounds the number of concurrent bcrypt hashes: each one occupies a
	// core for tens of milliseconds, and hundreds of simultaneous
	// connections would otherwise starve the game loop.
	hashSem    chan struct{}
	activeHash atomic.Int32 // hashes in progress (for tests)
	peakHash   atomic.Int32 // observed maximum (for tests)

	// dummyHash is used to equalize the response time when the account does not exist,
	// so as not to reveal which names exist.
	dummyHash []byte
}

// Option configures Open.
type Option func(*Store)

// WithBcryptCost sets the bcrypt cost (to be lowered only in tests).
func WithBcryptCost(cost int) Option { return func(s *Store) { s.cost = cost } }

// WithHashConcurrency bounds concurrent bcrypt hashes (default: half the cores).
func WithHashConcurrency(n int) Option {
	return func(s *Store) { s.hashSem = make(chan struct{}, max(1, n)) }
}

// Open opens (and migrates if needed) the database at path.
func Open(path string, opts ...Option) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection: SQLite has only one writer, and this avoids "database is locked" errors.
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
	s.dummyHash, err = bcrypt.GenerateFromPassword([]byte("unused"), s.cost)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// migrations is the ordered list of schema changes. Never modify
// one after release: add a new one.
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
	// 4: in-game currency. Existing characters start with no gold.
	`ALTER TABLE characters ADD COLUMN gold INTEGER NOT NULL DEFAULT 0`,
	// 5 to 8: consent for dialogues with a language model, and verified facts.
	`ALTER TABLE characters ADD COLUMN llm_consent INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN kills INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN boss_kills INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN deaths INTEGER NOT NULL DEFAULT 0`,
	// 9: player introductions, one per (account, place). Facts (level...) are
	// established by the server; the rest is stated by the player and stays unverified.
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
	// 11: an NPC's private memory of a player (never shown to another player).
	`CREATE TABLE npc_memories (
		region_x   INTEGER NOT NULL,
		region_y   INTEGER NOT NULL,
		npc_index  INTEGER NOT NULL,
		account    TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		summary    TEXT    NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (region_x, region_y, npc_index, account)
	)`,
	// 12: player quests and reputation per place. Handed-in quests (state = 'done')
	// also serve as NPC memory (rumors).
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
	// 13: players' equipment bag and generated vendor wares.
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
	// 14: class, abilities rolled at creation (all 0: creation to be done), max HP
	// (sum of hit dice), mana, and style of the bag's weapons.
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
	// 15: dice combat. An item has a family (damage or armor type) and a quality (damage bonus
	// of a weapon); the atk column is no longer used. Old items (empty family) are converted
	// on read by gear.Sanitize.
	`ALTER TABLE items ADD COLUMN family TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE items ADD COLUMN quality INTEGER NOT NULL DEFAULT 0`,
	// 16: discovered dungeon entrances, per character.
	`CREATE TABLE discoveries (
		account  TEXT    NOT NULL COLLATE NOCASE REFERENCES accounts(name) ON DELETE CASCADE,
		region_x INTEGER NOT NULL,
		region_y INTEGER NOT NULL,
		PRIMARY KEY (account, region_x, region_y)
	)`,
	// 17: respawn point (campfire) and activated standing stones ("x,y;x,y" list).
	`ALTER TABLE characters ADD COLUMN respawn_set INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN respawn_x INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN respawn_y INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN stones TEXT NOT NULL DEFAULT ''`,
	// 18: treasure maps held ("x,y;x,y" list of tiles to dig).
	`ALTER TABLE characters ADD COLUMN maps TEXT NOT NULL DEFAULT ''`,
	// 19: materials bag ("id:quantity;id:quantity" list).
	`ALTER TABLE characters ADD COLUMN resources TEXT NOT NULL DEFAULT ''`,
	// 20: experience of gathering jobs ("category:xp;category:xp" list).
	`ALTER TABLE characters ADD COLUMN skills TEXT NOT NULL DEFAULT ''`,
}

// migrate applies the missing migrations, each in its own transaction.
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database newer than this program (version %d, known %d)", version, len(migrations))
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
		// PRAGMA does not accept a parameter: i+1 is a trusted integer.
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

// Meta reads a value from the meta table.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetMeta writes a value to the meta table.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// withHashSlot runs fn while occupying one of the hashing slots.
func (s *Store) withHashSlot(fn func()) {
	s.hashSem <- struct{}{}
	n := s.activeHash.Add(1)
	for { // observed maximum, lock-free
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

// CreateAccount creates an account and returns the name as stored.
func (s *Store) CreateAccount(name, password string) (string, error) {
	if l := len(password); l < minPassword || l > maxPassword {
		return "", ErrWeakPassword
	}
	// Name already taken: refuse before spending an expensive hash. The
	// UNIQUE constraint below remains the arbiter in case of a race.
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

// Authenticate checks a password and returns the name as stored
// (the typed case may differ). An unknown name and a wrong
// password give the same error, after a comparable time.
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

// LoadCharacter returns an account's character, or nil if it has none yet.
func (s *Store) LoadCharacter(account string) (*Character, error) {
	var c Character
	var consent, respawnSet int
	var stones, maps, resources, skills string
	err := s.db.QueryRow(`SELECT level, xp, potions, hp, x, y, gold, llm_consent, kills, boss_kills, deaths,
			class, str, agi, int_, con, wis, cha, max_hp, mana, respawn_set, respawn_x, respawn_y, stones, maps, resources, skills
		FROM characters WHERE account = ?`, account).
		Scan(&c.Level, &c.XP, &c.Potions, &c.HP, &c.X, &c.Y, &c.Gold, &consent, &c.Kills, &c.BossKills, &c.Deaths,
			&c.Class, &c.Scores[0], &c.Scores[1], &c.Scores[2], &c.Scores[3], &c.Scores[4], &c.Scores[5], &c.MaxHP, &c.Mana,
			&respawnSet, &c.RespawnX, &c.RespawnY, &stones, &maps, &resources, &skills)
	c.LLMConsent = consent != 0
	c.RespawnSet = respawnSet != 0
	c.Stones = decodeStones(stones)
	c.Maps = decodeMaps(maps)
	c.Resources = decodeResources(resources)
	c.Skills = decodeResources(skills)
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
	if c.Discoveries, err = s.loadDiscoveries(account); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCharacters saves several characters in a single transaction
// (all or nothing). Unknown accounts are skipped rather than failing the batch.
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
			llm_consent, kills, boss_kills, deaths, class, str, agi, int_, con, wis, cha, max_hp, mana,
			respawn_set, respawn_x, respawn_y, stones, maps, resources, skills, updated_at)
		SELECT name, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM accounts WHERE name = ?
		ON CONFLICT(account) DO UPDATE SET
			level = excluded.level, xp = excluded.xp, potions = excluded.potions,
			hp = excluded.hp, x = excluded.x, y = excluded.y, gold = excluded.gold,
			llm_consent = excluded.llm_consent, kills = excluded.kills,
			boss_kills = excluded.boss_kills, deaths = excluded.deaths,
			class = excluded.class, str = excluded.str, agi = excluded.agi, int_ = excluded.int_,
			con = excluded.con, wis = excluded.wis, cha = excluded.cha,
			max_hp = excluded.max_hp, mana = excluded.mana,
			respawn_set = excluded.respawn_set, respawn_x = excluded.respawn_x, respawn_y = excluded.respawn_y,
			stones = excluded.stones, maps = excluded.maps, resources = excluded.resources, skills = excluded.skills,
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
			boolToInt(c.RespawnSet), c.RespawnX, c.RespawnY, encodeStones(c.Stones), encodeMaps(c.Maps), encodeResources(c.Resources), encodeResources(c.Skills),
			now, account); err != nil {
			return fmt.Errorf("saving %s: %w", account, err)
		}
		if c.QuestsDirty {
			if err := saveQuests(tx, account, c); err != nil {
				return fmt.Errorf("quests of %s: %w", account, err)
			}
		}
		if c.GearDirty {
			if err := saveGear(tx, account, c.Gear); err != nil {
				return fmt.Errorf("equipment of %s: %w", account, err)
			}
		}
		if c.DiscoveriesDirty {
			if err := saveDiscoveries(tx, account, c); err != nil {
				return fmt.Errorf("discovered dungeons of %s: %w", account, err)
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

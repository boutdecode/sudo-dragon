// Package npcai fait parler les PNJ grâce à un modèle de langage.
//
// Il orchestre, pour chaque réplique d'un joueur : les limites (débit, budget),
// la modération, la mémoire collective (internal/lore), le prompt, l'appel au
// modèle (internal/llm) et l'analyse de sa réponse. Rien de ce que dit le modèle
// n'agit sur le jeu : le résultat est du texte, plus, éventuellement, une
// présentation de joueur validée avant d'être mémorisée.
package npcai

import (
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Speaker est le joueur qui parle, avec les faits établis par le serveur à son sujet.
type Speaker struct {
	Account   string // pseudo du compte
	Level     int
	Kills     int
	BossKills int
	Deaths    int
}

// Turn est un échange déjà tenu pendant la session en cours.
type Turn struct{ Player, NPC string }

// Request est ce que le jeu demande: une réplique du joueur à un PNJ.
type Request struct {
	NPC       world.NPC
	Lang      i18n.Lang // langue du joueur: celle du prompt et de la réplique attendue
	Region    string    // nom de la contrée du lieu (vide: non précisé)
	Place     string    // nom du lieu
	PlaceKind world.Kind
	Speaker   Speaker
	Message   string // déjà nettoyé (textsafe) et borné par l'appelant
	History   []Turn // échanges précédents de la session, du plus ancien au plus récent

	// Opening demande au PNJ d'ouvrir la conversation: il salue le voyageur et, s'il
	// ne le connaît pas, l'invite à se présenter. Message est alors vide, et rien n'est
	// mémorisé (le joueur n'a encore rien dit).
	Opening bool

	// Reputation : points du joueur dans le lieu de ce PNJ (fait établi par le serveur).
	Reputation int
	// Offer, renseigné seulement pour un donneur de quêtes qui peut en confier une
	// au joueur, autorise le modèle à proposer une quête (voir quest.Validate).
	Offer *OfferContext

	// Landmarks : les lieux alentour que ce PNJ connaît (chef de village, maire), établis par le
	// serveur et déjà localisés. Vide pour les autres PNJ.
	Landmarks []Landmark

	// Deeds : quêtes rendues dont ce PNJ a connaissance (renseigné par le service, pas par le jeu).
	Deeds Deeds
}

// Landmark est un lieu voisin tel que le décrit le serveur à un PNJ qui le connaît.
type Landmark struct {
	Name      string // « Kerhaven »
	Kind      string // « village », « ville », « forteresse »
	Region    string // contrée, si elle diffère de celle du PNJ
	Direction string // « à l'est »
	Distance  string // « tout près », « à une bonne marche », « loin »
	Note      string // fait notable (ex. garnison)
}

// Deeds sont les quêtes rendues connues d'un PNJ : celles de son interlocuteur et, en rumeur, celles des autres.
type Deeds struct{ Mine, Others []lore.HeardDeed }

// OfferContext décrit le travail que ce PNJ peut confier à ce joueur.
type OfferContext struct {
	Quest    quest.Context // ce que le serveur accepte (espèces, lieux, boss…)
	Existing []string      // titres des quêtes déjà en cours auprès de ce PNJ (à ne pas répéter)
}

// Failure explique pourquoi aucune vraie réponse n'a pu être produite.
type Failure int

const (
	OK          Failure = iota
	Busy                // file pleine, requête déjà en cours ou limite du fournisseur
	Cooldown            // le joueur parle trop vite ou trop souvent
	Budget              // budget quotidien de jetons épuisé
	Blocked             // message refusé par la modération
	Unavailable         // service indisponible ou réponse inexploitable
)

func (f Failure) String() string {
	return [...]string{"ok", "busy", "cooldown", "budget", "blocked", "unavailable"}[f]
}

// Reply est le résultat d'une demande. Text est toujours affichable: en cas
// d'échec, c'est une réplique « en personnage » (le PNJ est fatigué, distrait…).
type Reply struct {
	Text       string
	Failure    Failure
	Remembered bool // une fiche de présentation a été enregistrée
	// Quest est une quête proposée par le modèle, déjà validée (quest.Validate) et modérée.
	// Elle n'existe pas encore : le joueur doit l'accepter.
	Quest  *quest.Quest
	Tokens int // jetons consommés par l'appel
}

// Config règle le service. Les zéros prennent les valeurs de DefaultConfig.
type Config struct {
	Workers       int           // appels simultanés au fournisseur
	QueueSize     int           // demandes en attente avant de répondre « occupé »
	Timeout       time.Duration // durée maximale d'une demande
	MinInterval   time.Duration // délai minimal entre deux messages d'un même joueur
	HourlyLimit   int           // messages par joueur et par heure
	CardCooldown  time.Duration // délai minimal entre deux écritures de fiche d'un joueur
	DailyTokens   int           // budget global de jetons par jour (UTC)
	MaxTokens     int           // jetons maximum d'une réplique
	Temperature   float64
	MaxHistory    int // échanges de session fournis au modèle
	BreakerFails  int // échecs consécutifs avant de suspendre le service
	BreakerPause  time.Duration
	BlockedThreat float64 // seuil des catégories bloquées (0 à 1; plus bas = plus strict)
}

// DefaultConfig renvoie des valeurs prudentes pour un petit serveur.
func DefaultConfig() Config {
	return Config{
		Workers: 4, QueueSize: 32, Timeout: 25 * time.Second,
		MinInterval: 1500 * time.Millisecond, HourlyLimit: 60, CardCooldown: 30 * time.Second,
		DailyTokens: 1_000_000, MaxTokens: 260, Temperature: 0.8, MaxHistory: 12,
		BreakerFails: 5, BreakerPause: 30 * time.Second, BlockedThreat: 0.5,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.Workers <= 0 {
		c.Workers = d.Workers
	}
	if c.QueueSize <= 0 {
		c.QueueSize = d.QueueSize
	}
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.MinInterval < 0 {
		c.MinInterval = 0
	} else if c.MinInterval == 0 {
		c.MinInterval = d.MinInterval
	}
	if c.HourlyLimit <= 0 {
		c.HourlyLimit = d.HourlyLimit
	}
	if c.CardCooldown <= 0 {
		c.CardCooldown = d.CardCooldown
	}
	if c.DailyTokens <= 0 {
		c.DailyTokens = d.DailyTokens
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = d.MaxTokens
	}
	if c.Temperature <= 0 {
		c.Temperature = d.Temperature
	}
	if c.MaxHistory <= 0 {
		c.MaxHistory = d.MaxHistory
	}
	if c.BreakerFails <= 0 {
		c.BreakerFails = d.BreakerFails
	}
	if c.BreakerPause <= 0 {
		c.BreakerPause = d.BreakerPause
	}
	if c.BlockedThreat <= 0 {
		c.BlockedThreat = d.BlockedThreat
	}
	return c
}

// Stats est un instantané des compteurs du service (pour /stats).
type Stats struct {
	Available      bool   `json:"available"`
	Guardrails     bool   `json:"guardrails"`
	Moderation     bool   `json:"moderation"`
	Requests       uint64 `json:"requests"`
	Failures       uint64 `json:"failures"`
	Blocked        uint64 `json:"blocked"`
	Remembered     uint64 `json:"remembered"`
	QuestsOffered  uint64 `json:"quests_proposed"`
	QuestsRejected uint64 `json:"quests_rejected"`
	TokensToday    int    `json:"tokens_today"`
	DailyBudget    int    `json:"daily_budget"`
	InFlight       int    `json:"in_flight"`
	Queued         int    `json:"queued"`
	AvgLatencyMS   int64  `json:"avg_latency_ms"`
}

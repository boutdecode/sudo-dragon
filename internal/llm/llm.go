// Package llm est le client des modèles de langage (Mistral AI) utilisés pour
// faire parler les PNJ.
//
// Ce paquet ne connaît rien du jeu: il envoie des messages et renvoie du texte.
// Le modèle ne produit JAMAIS d'action (pas d'outils, pas d'appels de
// fonctions): c'est une garantie de conception, le pire effet possible d'une
// réponse est un texte.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role est l'auteur d'un message.
type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
)

// Message est un message d'une conversation.
type Message struct {
	Role    Role
	Content string
}

// Guardrail demande à Mistral de modérer l'ENTRÉE avant de la traiter; une
// violation est renvoyée par l'API (HTTP 403), ce qui donne ErrBlocked.
type Guardrail struct {
	// Thresholds associe une catégorie à un seuil de 0 à 1 (1 = catégorie désactivée).
	Thresholds map[string]float64
	// BlockOnError bloque aussi quand la modération elle-même échoue (fail-closed).
	BlockOnError bool
}

// Request est une requête de complétion.
type Request struct {
	Model       string // vide: modèle par défaut du client
	Messages    []Message
	Temperature *float64
	MaxTokens   int  // 0: valeur par défaut de l'API
	JSON        bool // demande une réponse JSON (le prompt doit aussi l'exiger)
	Seed        *int // échantillonnage reproductible
	Guardrail   *Guardrail
}

// Usage est la consommation de jetons d'une requête. Les champs peuvent être
// nuls si l'API ne les renvoie pas: voir Response.TotalTokens.
type Usage struct{ Prompt, Completion, Total int }

// Response est la réponse d'un modèle.
type Response struct {
	Content      string
	FinishReason string
	Model        string
	Usage        Usage
}

// TotalTokens renvoie les jetons consommés, ou une estimation (environ un jeton
// pour trois caractères) si l'API n'a pas fourni la consommation.
func (r Response) TotalTokens(req Request) int {
	if r.Usage.Total > 0 {
		return r.Usage.Total
	}
	if r.Usage.Prompt+r.Usage.Completion > 0 {
		return r.Usage.Prompt + r.Usage.Completion
	}
	chars := len(r.Content)
	for _, m := range req.Messages {
		chars += len(m.Content)
	}
	return chars/3 + 1
}

// Verdict est le résultat de la modération d'un texte.
type Verdict struct {
	// Flagged liste les catégories jugées violées (ex. "hate_and_discrimination").
	Flagged []string
}

// Violates indique si au moins une catégorie de blocked est signalée. Une liste
// vide signifie « toutes les catégories ».
func (v Verdict) Violates(blocked map[string]bool) bool {
	for _, c := range v.Flagged {
		if len(blocked) == 0 || blocked[c] {
			return true
		}
	}
	return false
}

// Client est l'accès à un fournisseur de modèles.
type Client interface {
	Chat(ctx context.Context, req Request) (Response, error)
	// Moderate classe chaque texte; la réponse a autant d'éléments que texts.
	Moderate(ctx context.Context, texts []string) ([]Verdict, error)
}

// Erreurs. Utiliser errors.Is / errors.As.
var (
	ErrBlocked      = errors.New("llm: entrée bloquée par la modération")
	ErrUnauthorized = errors.New("llm: clé d'API refusée")
	ErrBadRequest   = errors.New("llm: requête refusée par l'API")
	ErrRateLimited  = errors.New("llm: limite de débit atteinte")
	ErrUnavailable  = errors.New("llm: service indisponible")
	ErrBadResponse  = errors.New("llm: réponse inexploitable")
)

// APIError détaille une réponse d'erreur du fournisseur. Le corps est tronqué
// et ne contient jamais la clé d'API.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration // 0 si l'API n'en indique pas
	kind       error
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%v (HTTP %d)", e.kind, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Unwrap permet errors.Is(err, ErrRateLimited), etc.
func (e *APIError) Unwrap() error { return e.kind }

// truncate borne un texte d'erreur affiché ou journalisé.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

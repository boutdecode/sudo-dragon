// Package llm is the client for the language models (Mistral AI) used to
// make NPCs talk.
//
// This package knows nothing about the game: it sends messages and returns text.
// The model NEVER produces an action (no tools, no function calls):
// this is a design guarantee, the worst possible effect of a
// response is text.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role is the author of a message.
type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
)

// Message is a message in a conversation.
type Message struct {
	Role    Role
	Content string
}

// Guardrail asks Mistral to moderate the INPUT before processing it; a
// violation is returned by the API (HTTP 403), which yields ErrBlocked.
type Guardrail struct {
	// Thresholds maps a category to a threshold from 0 to 1 (1 = category disabled).
	Thresholds map[string]float64
	// BlockOnError also blocks when the moderation itself fails (fail-closed).
	BlockOnError bool
}

// Request is a completion request.
type Request struct {
	Model       string // empty: client's default model
	Messages    []Message
	Temperature *float64
	MaxTokens   int  // 0: API default value
	JSON        bool // asks for a JSON response (the prompt must also require it)
	Seed        *int // reproducible sampling
	Guardrail   *Guardrail
}

// Usage is the token consumption of a request. The fields may be
// zero if the API does not return them: see Response.TotalTokens.
type Usage struct{ Prompt, Completion, Total int }

// Response is a model's response.
type Response struct {
	Content      string
	FinishReason string
	Model        string
	Usage        Usage
}

// TotalTokens returns the tokens consumed, or an estimate (about one token
// per three characters) if the API did not provide the usage.
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

// Verdict is the result of moderating a text.
type Verdict struct {
	// Flagged lists the categories judged violated (e.g. "hate_and_discrimination").
	Flagged []string
}

// Violates reports whether at least one category of blocked is flagged. An empty
// list means "all categories".
func (v Verdict) Violates(blocked map[string]bool) bool {
	for _, c := range v.Flagged {
		if len(blocked) == 0 || blocked[c] {
			return true
		}
	}
	return false
}

// Client is the access to a model provider.
type Client interface {
	Chat(ctx context.Context, req Request) (Response, error)
	// Moderate classifies each text; the response has as many elements as texts.
	Moderate(ctx context.Context, texts []string) ([]Verdict, error)
}

// Errors. Use errors.Is / errors.As.
var (
	ErrBlocked      = errors.New("llm: input blocked by moderation")
	ErrUnauthorized = errors.New("llm: API key rejected")
	ErrBadRequest   = errors.New("llm: request rejected by the API")
	ErrRateLimited  = errors.New("llm: rate limit reached")
	ErrUnavailable  = errors.New("llm: service unavailable")
	ErrBadResponse  = errors.New("llm: unusable response")
)

// APIError details an error response from the provider. The body is truncated
// and never contains the API key.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration // 0 if the API gives none
	kind       error
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%v (HTTP %d)", e.kind, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Unwrap enables errors.Is(err, ErrRateLimited), etc.
func (e *APIError) Unwrap() error { return e.kind }

// truncate bounds an error text that is displayed or logged.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

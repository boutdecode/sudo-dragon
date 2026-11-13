//go:build live

package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

// Optional real test: go test -tags live -run Live ./internal/llm (make llm-smoke).
// It consumes a few tokens and checks the API points that fake servers cannot
// guarantee: model names, JSON response, moderation.
func TestLiveMistral(t *testing.T) {
	key := os.Getenv("MISTRAL_API_KEY")
	if key == "" {
		t.Skip("MISTRAL_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := NewMistral(key)

	resp, err := c.Chat(ctx, Request{
		Messages:  []Message{{Role: User, Content: `Reply only with a JSON object {"ok": true}.`}},
		MaxTokens: 20, JSON: true,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	t.Logf("chat ok: model=%s tokens=%+v content=%q", resp.Model, resp.Usage, resp.Content)

	v, err := c.Moderate(ctx, []string{"Hello, I am looking for the village blacksmith."})
	if err != nil {
		t.Fatalf("moderation: %v", err)
	}
	t.Logf("moderation ok: %+v", v)
}

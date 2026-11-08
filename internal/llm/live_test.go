//go:build live

package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

// Test réel, facultatif : go test -tags live -run Live ./internal/llm (make llm-smoke).
// Il consomme quelques jetons et vérifie les points de l'API que les faux serveurs ne
// peuvent pas garantir : noms de modèles, réponse JSON, modération.
func TestLiveMistral(t *testing.T) {
	key := os.Getenv("MISTRAL_API_KEY")
	if key == "" {
		t.Skip("MISTRAL_API_KEY non définie")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := NewMistral(key)

	resp, err := c.Chat(ctx, Request{
		Messages:  []Message{{Role: User, Content: `Réponds uniquement par un objet JSON {"ok": true}.`}},
		MaxTokens: 20, JSON: true,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	t.Logf("chat ok: modèle=%s jetons=%+v contenu=%q", resp.Model, resp.Usage, resp.Content)

	v, err := c.Moderate(ctx, []string{"Bonjour, je cherche le forgeron du village."})
	if err != nil {
		t.Fatalf("modération: %v", err)
	}
	t.Logf("modération ok: %+v", v)
}

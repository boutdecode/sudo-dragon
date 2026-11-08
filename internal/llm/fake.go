package llm

import (
	"context"
	"sync"
)

// Fake est un Client scriptable pour les tests des autres paquets. Il enregistre
// chaque appel et ne contacte aucun réseau.
type Fake struct {
	mu sync.Mutex

	// ChatFunc et ModerateFunc, si définis, répondent aux appels. Par défaut,
	// Chat renvoie une réponse vide et Moderate ne signale rien.
	ChatFunc     func(ctx context.Context, req Request) (Response, error)
	ModerateFunc func(ctx context.Context, texts []string) ([]Verdict, error)

	chats      []Request
	moderation [][]string
}

// Chat enregistre la requête puis délègue à ChatFunc.
func (f *Fake) Chat(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	f.chats = append(f.chats, req)
	fn := f.ChatFunc
	f.mu.Unlock()
	if fn == nil {
		return Response{}, nil
	}
	return fn(ctx, req)
}

// Moderate enregistre les textes puis délègue à ModerateFunc.
func (f *Fake) Moderate(ctx context.Context, texts []string) ([]Verdict, error) {
	f.mu.Lock()
	f.moderation = append(f.moderation, append([]string(nil), texts...))
	fn := f.ModerateFunc
	f.mu.Unlock()
	if fn == nil {
		return make([]Verdict, len(texts)), nil
	}
	return fn(ctx, texts)
}

// ChatCalls renvoie une copie des requêtes de chat reçues.
func (f *Fake) ChatCalls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.chats...)
}

// ModerationCalls renvoie une copie des lots de textes modérés.
func (f *Fake) ModerationCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.moderation...)
}

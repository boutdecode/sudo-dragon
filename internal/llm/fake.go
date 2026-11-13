package llm

import (
	"context"
	"sync"
)

// Fake is a scriptable Client for the tests of other packages. It records
// each call and contacts no network.
type Fake struct {
	mu sync.Mutex

	// ChatFunc and ModerateFunc, if set, answer the calls. By default,
	// Chat returns an empty response and Moderate flags nothing.
	ChatFunc     func(ctx context.Context, req Request) (Response, error)
	ModerateFunc func(ctx context.Context, texts []string) ([]Verdict, error)

	chats      []Request
	moderation [][]string
}

// Chat records the request then delegates to ChatFunc.
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

// Moderate records the texts then delegates to ModerateFunc.
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

// ChatCalls returns a copy of the chat requests received.
func (f *Fake) ChatCalls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.chats...)
}

// ModerationCalls returns a copy of the batches of moderated texts.
func (f *Fake) ModerationCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.moderation...)
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/store/storetest"
	"github.com/kevin/ascii-adventure/internal/world"
)

const secretKey = "cle-de-test-ULTRA-SECRETE-9f8e7d6c"

type apiCall struct {
	Path, Auth string
	Body       map[string]any
}

// fakeMistral is a fake Mistral server: chat, moderation, and controllable failures.
type fakeMistral struct {
	srv      *httptest.Server
	mu       sync.Mutex
	calls    []apiCall
	chatCode atomic.Int32 // HTTP code of the chat (0 = 200)
	modCode  atomic.Int32
}

func newFakeMistral(t *testing.T) *fakeMistral {
	t.Helper()
	f := &fakeMistral{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.URL.Path, r.Header.Get("Authorization"), body})
		f.mu.Unlock()

		code := &f.chatCode
		if r.URL.Path == "/v1/moderations" {
			code = &f.modCode
		}
		if c := int(code.Load()); c != 0 {
			w.WriteHeader(c)
			_, _ = io.WriteString(w, `{"message":"refused"}`)
			return
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"ok\": true}"}}],"usage":{"total_tokens":12}}`)
		case "/v1/moderations":
			_, _ = io.WriteString(w, `{"results":[{"categories":{}}]}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeMistral) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Path)
	}
	return out
}

func (f *fakeMistral) call(path string) (apiCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Path == path {
			return c, true
		}
	}
	return apiCall{}, false
}

func env(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }

type setup struct {
	log *bytes.Buffer
	ctx context.Context
}

func newSetup(t *testing.T) (*setup, *slog.Logger) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	buf := &bytes.Buffer{}
	return &setup{log: buf, ctx: ctx}, slog.New(slog.NewTextHandler(buf, nil))
}

func TestModeOffNeverTouchesTheNetwork(t *testing.T) {
	f := newFakeMistral(t)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, err := setupTalker(s.ctx, llmOptions{Mode: "off", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
	if err != nil || svc != nil {
		t.Fatalf("svc=%v err=%v", svc, err)
	}
	if len(f.paths()) != 0 {
		t.Fatalf("network calls in off mode: %v", f.paths())
	}
}

func TestAutoModeWithoutKeyIsSilentlyDisabled(t *testing.T) {
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, err := setupTalker(s.ctx, llmOptions{Mode: "auto"}, be.Store, world.New(1), log, env(nil))
	if err != nil || svc != nil {
		t.Fatalf("svc=%v err=%v", svc, err)
	}
	if !strings.Contains(s.log.String(), apiKeyEnv) {
		t.Fatalf("the log should explain why dialogues are disabled: %q", s.log.String())
	}
	// Empty mode = auto.
	if svc, err := setupTalker(s.ctx, llmOptions{}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: "   "})); err != nil || svc != nil {
		t.Fatalf("whitespace-only key: svc=%v err=%v", svc, err)
	}
}

func TestOnModeRequiresTheKey(t *testing.T) {
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, err := setupTalker(s.ctx, llmOptions{Mode: "on"}, be.Store, world.New(1), log, env(nil))
	if err == nil || svc != nil || !strings.Contains(err.Error(), apiKeyEnv) {
		t.Fatalf("svc=%v err=%v", svc, err)
	}
}

func TestUnknownModeIsRejected(t *testing.T) {
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	if _, err := setupTalker(s.ctx, llmOptions{Mode: "maybe"}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey})); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
}

func TestHealthyStartupProbesAndEnables(t *testing.T) {
	f := newFakeMistral(t)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, err := setupTalker(s.ctx, llmOptions{
		Mode: "auto", BaseURL: f.srv.URL, Model: "mon-modele-de-chat", ModerationModel: "ma-moderation",
	}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: "  " + secretKey + "\n"}))
	if err != nil || svc == nil {
		t.Fatalf("svc=%v err=%v", svc, err)
	}
	if !svc.Available() {
		t.Fatal("the service should be available")
	}
	chat, ok := f.call("/v1/chat/completions")
	if !ok || chat.Auth != "Bearer "+secretKey || chat.Body["model"] != "mon-modele-de-chat" {
		t.Fatalf("chat probe: %+v", chat)
	}
	mod, ok := f.call("/v1/moderations")
	if !ok || mod.Auth != "Bearer "+secretKey || mod.Body["model"] != "ma-moderation" {
		t.Fatalf("moderation probe: %+v", mod)
	}
	if !strings.Contains(s.log.String(), "NPC dialogues enabled") {
		t.Fatalf("log: %q", s.log.String())
	}
	if st := svc.Stats(); !st.Available || !st.Guardrails || !st.Moderation {
		t.Fatalf("state: %+v", st)
	}
}

// The key must never end up in a log, whatever happens.
func TestAPIKeyNeverAppearsInLogs(t *testing.T) {
	for name, setupFake := range map[string]func(*fakeMistral){
		"success":                func(*fakeMistral) {},
		"key rejected":           func(f *fakeMistral) { f.chatCode.Store(401); f.modCode.Store(401) },
		"chat outage":            func(f *fakeMistral) { f.chatCode.Store(503) },
		"moderation outage":      func(f *fakeMistral) { f.modCode.Store(500) },
		"request rejected (400)": func(f *fakeMistral) { f.chatCode.Store(400) },
		"blocked by moderation":  func(f *fakeMistral) { f.chatCode.Store(403) },
	} {
		f := newFakeMistral(t)
		setupFake(f)
		be := storetest.NewBackend(t, time.Hour)
		s, log := newSetup(t)
		_, err := setupTalker(s.ctx, llmOptions{Mode: "auto", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(s.log.String(), secretKey) {
			t.Errorf("%s: the API key appears in the log:\n%s", name, s.log.String())
		}
	}
}

func TestRejectedKeyDoesNotBlockStartup(t *testing.T) {
	f := newFakeMistral(t)
	f.chatCode.Store(401)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, err := setupTalker(s.ctx, llmOptions{Mode: "auto", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
	if err != nil || svc == nil {
		t.Fatalf("a probe failure must not prevent startup: svc=%v err=%v", svc, err)
	}
	if svc.Available() {
		t.Fatal("the service must not declare itself available with a rejected key")
	}
	if !strings.Contains(s.log.String(), "unavailable") {
		t.Fatalf("log: %q", s.log.String())
	}
}

func TestModerationOutageKeepsTalkingButWarns(t *testing.T) {
	f := newFakeMistral(t)
	f.modCode.Store(500)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, _ := setupTalker(s.ctx, llmOptions{Mode: "auto", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
	if svc == nil || !svc.Available() {
		t.Fatal("chat works: the service must be available")
	}
	if !strings.Contains(s.log.String(), "moderation unavailable") {
		t.Fatalf("expected warning: %q", s.log.String())
	}
	if svc.Stats().Moderation {
		t.Fatal("moderation is announced as available")
	}
}

func TestServiceRecoversOnItsOwnWhenTheAPIComesBack(t *testing.T) {
	old := superviseEvery
	superviseEvery = 20 * time.Millisecond
	t.Cleanup(func() { superviseEvery = old })

	f := newFakeMistral(t)
	f.chatCode.Store(503)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, _ := setupTalker(s.ctx, llmOptions{Mode: "auto", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
	if svc == nil || svc.Available() {
		t.Fatal("the service should start unavailable")
	}

	f.chatCode.Store(0) // the API comes back
	deadline := time.Now().Add(3 * time.Second)
	for !svc.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !svc.Available() {
		t.Fatal("the service did not recover by itself after the API came back")
	}
}

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

// fakeMistral est un faux serveur Mistral : chat, modération, et pannes pilotables.
type fakeMistral struct {
	srv      *httptest.Server
	mu       sync.Mutex
	calls    []apiCall
	chatCode atomic.Int32 // code HTTP du chat (0 = 200)
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
			_, _ = io.WriteString(w, `{"message":"refusé"}`)
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
		t.Fatalf("appels réseau en mode off: %v", f.paths())
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
		t.Fatalf("le journal devrait expliquer pourquoi les dialogues sont désactivés: %q", s.log.String())
	}
	// Mode vide = auto.
	if svc, err := setupTalker(s.ctx, llmOptions{}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: "   "})); err != nil || svc != nil {
		t.Fatalf("clé faite d'espaces: svc=%v err=%v", svc, err)
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
	if _, err := setupTalker(s.ctx, llmOptions{Mode: "peut-être"}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey})); err == nil {
		t.Fatal("un mode inconnu a été accepté")
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
		t.Fatal("le service devrait être disponible")
	}
	chat, ok := f.call("/v1/chat/completions")
	if !ok || chat.Auth != "Bearer "+secretKey || chat.Body["model"] != "mon-modele-de-chat" {
		t.Fatalf("sonde de chat: %+v", chat)
	}
	mod, ok := f.call("/v1/moderations")
	if !ok || mod.Auth != "Bearer "+secretKey || mod.Body["model"] != "ma-moderation" {
		t.Fatalf("sonde de modération: %+v", mod)
	}
	if !strings.Contains(s.log.String(), "dialogues de PNJ activés") {
		t.Fatalf("journal: %q", s.log.String())
	}
	if st := svc.Stats(); !st.Available || !st.Guardrails || !st.Moderation {
		t.Fatalf("état: %+v", st)
	}
}

// La clé ne doit jamais se retrouver dans un journal, quoi qu'il arrive.
func TestAPIKeyNeverAppearsInLogs(t *testing.T) {
	for name, setupFake := range map[string]func(*fakeMistral){
		"succès":                   func(*fakeMistral) {},
		"clé refusée":              func(f *fakeMistral) { f.chatCode.Store(401); f.modCode.Store(401) },
		"panne du chat":            func(f *fakeMistral) { f.chatCode.Store(503) },
		"modération en panne":      func(f *fakeMistral) { f.modCode.Store(500) },
		"requête refusée (400)":    func(f *fakeMistral) { f.chatCode.Store(400) },
		"bloqué par la modération": func(f *fakeMistral) { f.chatCode.Store(403) },
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
			t.Errorf("%s : la clé d'API apparaît dans le journal :\n%s", name, s.log.String())
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
		t.Fatalf("un échec de sonde ne doit pas empêcher le démarrage : svc=%v err=%v", svc, err)
	}
	if svc.Available() {
		t.Fatal("le service ne doit pas se déclarer disponible avec une clé refusée")
	}
	if !strings.Contains(s.log.String(), "indisponibles") {
		t.Fatalf("journal : %q", s.log.String())
	}
}

func TestModerationOutageKeepsTalkingButWarns(t *testing.T) {
	f := newFakeMistral(t)
	f.modCode.Store(500)
	be := storetest.NewBackend(t, time.Hour)
	s, log := newSetup(t)
	svc, _ := setupTalker(s.ctx, llmOptions{Mode: "auto", BaseURL: f.srv.URL}, be.Store, world.New(1), log, env(map[string]string{apiKeyEnv: secretKey}))
	if svc == nil || !svc.Available() {
		t.Fatal("le chat fonctionne : le service doit être disponible")
	}
	if !strings.Contains(s.log.String(), "modération indisponible") {
		t.Fatalf("avertissement attendu : %q", s.log.String())
	}
	if svc.Stats().Moderation {
		t.Fatal("la modération est annoncée disponible")
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
		t.Fatal("le service devrait démarrer indisponible")
	}

	f.chatCode.Store(0) // l'API revient
	deadline := time.Now().Add(3 * time.Second)
	for !svc.Available() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !svc.Available() {
		t.Fatal("le service ne s'est pas rétabli tout seul après le retour de l'API")
	}
}

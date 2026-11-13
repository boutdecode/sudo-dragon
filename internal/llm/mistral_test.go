package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "cle-secrete-de-test-0123456789"

// recorded is a request received by the fake server.
type recorded struct {
	Method, Path string
	Header       http.Header
	Body         map[string]any
}

// fakeAPI starts a fake Mistral server. handler decides the response; the
// requests received are recorded.
func fakeAPI(t *testing.T, handler func(n int, w http.ResponseWriter, r *http.Request)) (*Mistral, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	var got []recorded
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		got = append(got, recorded{r.Method, r.URL.Path, r.Header.Clone(), body})
		mu.Unlock()
		handler(int(count.Add(1)), w, r)
	}))
	t.Cleanup(srv.Close)

	m := NewMistral(testKey, WithBaseURL(srv.URL), WithRetries(2, time.Millisecond))
	// Instant, traceable time.
	m.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	m.jitter = func(d time.Duration) time.Duration { return d }
	return m, &got
}

func okChat(content string) func(int, http.ResponseWriter, *http.Request) {
	return func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m-test",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
		})
	}
}

func TestChatRequestShape(t *testing.T) {
	m, got := fakeAPI(t, okChat("Bonjour"))
	temp, seed := 0.4, 42
	resp, err := m.Chat(context.Background(), Request{
		Messages: []Message{
			{System, "Tu es un soigneur."},
			{User, "Salut"},
			{Assistant, "Bonjour"},
			{User, "Et toi ?"},
		},
		Temperature: &temp, MaxTokens: 150, JSON: true, Seed: &seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Bonjour" || resp.FinishReason != "stop" || resp.Model != "m-test" {
		t.Fatalf("response: %+v", resp)
	}
	if resp.Usage != (Usage{11, 7, 18}) {
		t.Fatalf("usage: %+v", resp.Usage)
	}

	if len(*got) != 1 {
		t.Fatalf("%d requests", len(*got))
	}
	r := (*got)[0]
	if r.Method != "POST" || r.Path != "/v1/chat/completions" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
	}
	if r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
	}
	if r.Body["model"] != DefaultModel {
		t.Fatalf("model = %v, want the default %s", r.Body["model"], DefaultModel)
	}
	msgs := r.Body["messages"].([]any)
	wantRoles := []string{"system", "user", "assistant", "user"}
	if len(msgs) != len(wantRoles) {
		t.Fatalf("%d messages", len(msgs))
	}
	for i, role := range wantRoles {
		if msgs[i].(map[string]any)["role"] != role {
			t.Fatalf("message %d: role %v, want %s", i, msgs[i].(map[string]any)["role"], role)
		}
	}
	if msgs[0].(map[string]any)["content"] != "Tu es un soigneur." {
		t.Fatal("system message content altered")
	}
	if r.Body["temperature"] != 0.4 || r.Body["max_tokens"] != float64(150) || r.Body["random_seed"] != float64(42) {
		t.Fatalf("parameters: %v", r.Body)
	}
	if rf, _ := r.Body["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Fatalf("response_format = %v", r.Body["response_format"])
	}
}

func TestChatOmitsUnsetFields(t *testing.T) {
	m, got := fakeAPI(t, okChat("x"))
	if _, err := m.Chat(context.Background(), Request{Model: "mon-modele", Messages: []Message{{User, "hi"}}}); err != nil {
		t.Fatal(err)
	}
	body := (*got)[0].Body
	for _, k := range []string{"temperature", "max_tokens", "response_format", "random_seed", "guardrails"} {
		if _, present := body[k]; present {
			t.Errorf("field %q should not be sent when not requested", k)
		}
	}
	if body["model"] != "mon-modele" {
		t.Fatalf("model = %v", body["model"])
	}
}

func TestChatGuardrailShape(t *testing.T) {
	m, got := fakeAPI(t, okChat("x"))
	_, err := m.Chat(context.Background(), Request{
		Messages:  []Message{{User, "hi"}},
		Guardrail: &Guardrail{Thresholds: map[string]float64{"pii": 0.5, "jailbreaking": 0.3}, BlockOnError: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	gs, ok := (*got)[0].Body["guardrails"].([]any)
	if !ok || len(gs) != 1 {
		t.Fatalf("guardrails = %v", (*got)[0].Body["guardrails"])
	}
	mod := gs[0].(map[string]any)["moderation_llm_v2"].(map[string]any)
	if mod["action"] != "block" || mod["block_on_error"] != true {
		t.Fatalf("configuration: %v", mod)
	}
	th := mod["custom_category_thresholds"].(map[string]any)
	if th["pii"] != 0.5 || th["jailbreaking"] != 0.3 {
		t.Fatalf("thresholds: %v", th)
	}
}

func TestChatEmptyRequestIsRejectedLocally(t *testing.T) {
	m, got := fakeAPI(t, okChat("x"))
	if _, err := m.Chat(context.Background(), Request{}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("error: %v", err)
	}
	if len(*got) != 0 {
		t.Fatal("an empty request should not reach the network")
	}
}

func TestChatContentAsChunks(t *testing.T) {
	m, _ := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":[{"type":"text","text":"Bon"},{"type":"thinking","text":"CACHÉ"},{"type":"text","text":"jour"}]}}]}`)
	})
	resp, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Bonjour" {
		t.Fatalf("content = %q (non-text fragments must be ignored)", resp.Content)
	}
}

func TestChatBadResponses(t *testing.T) {
	for name, body := range map[string]string{
		"invalid json":    `{pas du json`,
		"no choices":      `{"choices":[]}`,
		"null content":    `{"choices":[{"message":{"content":null}}]}`,
		"missing content": `{"choices":[{"message":{}}]}`,
		"numeric content": `{"choices":[{"message":{"content":42}}]}`,
		"empty fragments": `{"choices":[{"message":{"content":[{"type":"image_url"}]}}]}`,
		"empty body":      ``,
	} {
		m, _ := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
		if _, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}}); !errors.Is(err, ErrBadResponse) {
			t.Errorf("%s: error %v, want ErrBadResponse", name, err)
		}
	}
}

func TestChatMissingUsageIsEstimated(t *testing.T) {
	m, _ := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"douze car"}}]}`)
	})
	req := Request{Messages: []Message{{User, strings.Repeat("a", 90)}}}
	resp, err := m.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage != (Usage{}) {
		t.Fatalf("made-up usage: %+v", resp.Usage)
	}
	if got := resp.TotalTokens(req); got < 30 || got > 40 {
		t.Fatalf("estimate = %d tokens, want about (90+9)/3", got)
	}
	if got := (Response{Usage: Usage{Total: 50}}).TotalTokens(req); got != 50 {
		t.Fatalf("provided total ignored: %d", got)
	}
	if got := (Response{Usage: Usage{Prompt: 20, Completion: 5}}).TotalTokens(req); got != 25 {
		t.Fatalf("prompt+completion sum ignored: %d", got)
	}
}

// --- HTTP errors ---

func status(code int, body string) func(int, http.ResponseWriter, *http.Request) {
	return func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

func TestNoRetryForClientErrors(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{401, ErrUnauthorized}, {403, ErrBlocked}, {400, ErrBadRequest}, {404, ErrBadRequest}, {422, ErrBadRequest},
	}
	for _, c := range cases {
		m, got := fakeAPI(t, status(c.code, `{"message":"détail de l'erreur"}`))
		_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
		if !errors.Is(err, c.want) {
			t.Errorf("HTTP %d: error %v, want %v", c.code, err, c.want)
		}
		if len(*got) != 1 {
			t.Errorf("HTTP %d: %d attempts, no retry expected", c.code, len(*got))
		}
		var ae *APIError
		if !errors.As(err, &ae) || ae.Status != c.code || !strings.Contains(ae.Body, "détail") {
			t.Errorf("HTTP %d: error details missing: %v", c.code, err)
		}
	}
}

func TestRetryOnServerErrorThenSucceed(t *testing.T) {
	m, got := fakeAPI(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		okChat("enfin")(n, w, r)
	})
	resp, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err != nil || resp.Content != "enfin" {
		t.Fatalf("response %+v, error %v", resp, err)
	}
	if len(*got) != 3 {
		t.Fatalf("%d attempts, want 3 (2 failures then success)", len(*got))
	}
}

func TestRetriesAreBounded(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		m, got := fakeAPI(t, status(code, "panne"))
		_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("HTTP %d: %v", code, err)
		}
		if len(*got) != 3 {
			t.Errorf("HTTP %d: %d attempts, want 1 + 2 retries", code, len(*got))
		}
	}
}

func TestRateLimitHonoursRetryAfter(t *testing.T) {
	var waits []time.Duration
	m, got := fakeAPI(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
			return
		}
		okChat("ok")(n, w, r)
	})
	m.sleep = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	resp, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err != nil || resp.Content != "ok" {
		t.Fatalf("response %+v, error %v", resp, err)
	}
	if len(*got) != 2 || len(waits) != 1 || waits[0] != 3*time.Second {
		t.Fatalf("%d attempts, waits %v: Retry-After: 3 should have been honored", len(*got), waits)
	}
}

func TestRateLimitWithoutHeaderUsesBackoff(t *testing.T) {
	var waits []time.Duration
	m, got := fakeAPI(t, status(429, "trop de requêtes"))
	m.backoff = 100 * time.Millisecond
	m.sleep = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error: %v", err)
	}
	if len(*got) != 3 {
		t.Fatalf("%d attempts", len(*got))
	}
	if len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatalf("waits %v, want 100 ms then 200 ms (exponential)", waits)
	}
}

func TestRetryAfterTooLongFailsFast(t *testing.T) {
	var slept bool
	m, got := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	})
	m.sleep = func(ctx context.Context, d time.Duration) error { slept = true; return nil }

	_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	var ae *APIError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &ae) || ae.RetryAfter != 120*time.Second {
		t.Fatalf("error: %v", err)
	}
	if slept || len(*got) != 1 {
		t.Fatalf("useless wait of %v: slept=%v, %d attempts", ae.RetryAfter, slept, len(*got))
	}
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	cases := map[string]func(time.Duration) bool{
		"":     func(d time.Duration) bool { return d == 0 },
		"5":    func(d time.Duration) bool { return d == 5*time.Second },
		" 7 ":  func(d time.Duration) bool { return d == 7*time.Second },
		"0":    func(d time.Duration) bool { return d == 0 },
		"-4":   func(d time.Duration) bool { return d == 0 },
		"soon": func(d time.Duration) bool { return d == 0 },
		future: func(d time.Duration) bool { return d > 25*time.Second && d <= 30*time.Second },
		time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat): func(d time.Duration) bool { return d == 0 },
	}
	for in, ok := range cases {
		if got := parseRetryAfter(in); !ok(got) {
			t.Errorf("parseRetryAfter(%q) = %v", in, got)
		}
	}
}

func TestContextCancelledDuringBackoff(t *testing.T) {
	m, got := fakeAPI(t, status(503, ""))
	ctx, cancel := context.WithCancel(context.Background())
	m.sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }

	_, err := m.Chat(ctx, Request{Messages: []Message{{User, "hi"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("%d attempts after cancellation", len(*got))
	}
}

func TestContextDeadlineStopsSlowServer(t *testing.T) {
	release := make(chan struct{})
	m, _ := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) { <-release })
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := m.Chat(ctx, Request{Messages: []Message{{User, "hi"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the request did not respect the context deadline")
	}
}

func TestPerAttemptTimeoutIsRetried(t *testing.T) {
	release := make(chan struct{})
	m, got := fakeAPI(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			<-release
			return
		}
		okChat("rattrapé")(n, w, r)
	})
	t.Cleanup(func() { close(release) })
	m.http = &http.Client{Timeout: 100 * time.Millisecond}

	resp, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err != nil || resp.Content != "rattrapé" {
		t.Fatalf("response %+v, error %v", resp, err)
	}
	if len(*got) != 2 {
		t.Fatalf("%d attempts", len(*got))
	}
}

func TestConnectionRefusedIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nobody is listening anymore
	m := NewMistral(testKey, WithBaseURL(url), WithRetries(1, time.Millisecond))
	m.sleep = func(ctx context.Context, d time.Duration) error { return nil }
	if _, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error: %v", err)
	}
}

// The API key must never appear in an error, whether displayed or logged.
func TestAPIKeyNeverAppearsInErrors(t *testing.T) {
	codes := []int{400, 401, 403, 422, 429, 500, 503}
	for _, code := range codes {
		m, _ := fakeAPI(t, status(code, `{"message":"échec"}`))
		_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
		if err == nil || strings.Contains(err.Error(), testKey) {
			t.Errorf("HTTP %d: error %v", code, err)
		}
	}
	// Transport error.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	m := NewMistral(testKey, WithBaseURL(url), WithRetries(0, 0))
	_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err == nil || strings.Contains(err.Error(), testKey) {
		t.Errorf("transport error: %v", err)
	}
}

func TestLongErrorBodyIsTruncated(t *testing.T) {
	m, _ := fakeAPI(t, status(400, strings.Repeat("très long ", 500)))
	_, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}})
	if err == nil || len(err.Error()) > 600 {
		t.Fatalf("error message of %d bytes: unbounded", len(err.Error()))
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		okChat("x")(1, w, r)
	}))
	defer srv.Close()
	m := NewMistral(testKey, WithBaseURL(srv.URL+"/"))
	if _, err := m.Chat(context.Background(), Request{Messages: []Message{{User, "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("path = %q", path)
	}
}

// --- Moderation ---

func TestModerateRequestAndVerdicts(t *testing.T) {
	m, got := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"results":[
			{"categories":{"sexual":false,"hate_and_discrimination":false}},
			{"categories":{"hate_and_discrimination":true,"violence_and_threats":true,"sexual":false}}]}`)
	})
	v, err := m.Moderate(context.Background(), []string{"gentil", "méchant"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 || len(v[0].Flagged) != 0 {
		t.Fatalf("verdicts: %+v", v)
	}
	if len(v[1].Flagged) != 2 || v[1].Flagged[0] != "hate_and_discrimination" || v[1].Flagged[1] != "violence_and_threats" {
		t.Fatalf("flagged categories: %v", v[1].Flagged)
	}

	r := (*got)[0]
	if r.Path != "/v1/moderations" || r.Body["model"] != DefaultModerationModel {
		t.Fatalf("%s, model %v", r.Path, r.Body["model"])
	}
	in := r.Body["input"].([]any)
	if len(in) != 2 || in[0] != "gentil" || in[1] != "méchant" {
		t.Fatalf("input: %v", in)
	}
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		t.Fatal("key missing from the moderation request")
	}
}

func TestModerateErrors(t *testing.T) {
	m, _ := fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"results":[{"categories":{}}]}`) // one verdict for two texts
	})
	if _, err := m.Moderate(context.Background(), []string{"a", "b"}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("missing verdicts: %v", err)
	}
	m, _ = fakeAPI(t, func(_ int, w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `pas du json`) })
	if _, err := m.Moderate(context.Background(), []string{"a"}); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("invalid JSON: %v", err)
	}
	m, got := fakeAPI(t, status(500, ""))
	if _, err := m.Moderate(context.Background(), []string{"a"}); !errors.Is(err, ErrUnavailable) || len(*got) != 3 {
		t.Fatalf("5xx: %v, %d attempts", err, len(*got))
	}
	// No text: no network call.
	m, got = fakeAPI(t, status(500, ""))
	if v, err := m.Moderate(context.Background(), nil); err != nil || v != nil || len(*got) != 0 {
		t.Fatalf("empty list: %v %v, %d calls", v, err, len(*got))
	}
}

func TestVerdictViolates(t *testing.T) {
	v := Verdict{Flagged: []string{"pii", "sexual"}}
	if !v.Violates(nil) || !v.Violates(map[string]bool{}) {
		t.Error("empty list = all categories")
	}
	if !v.Violates(map[string]bool{"sexual": true}) {
		t.Error("blocked category not detected")
	}
	if v.Violates(map[string]bool{"hate_and_discrimination": true}) {
		t.Error("non-blocked category judged violated")
	}
	if (Verdict{}).Violates(nil) {
		t.Error("no flag can violate")
	}
}

// --- Fake ---

func TestFakeRecordsCalls(t *testing.T) {
	var f Client = &Fake{ChatFunc: func(_ context.Context, r Request) (Response, error) {
		return Response{Content: "écho: " + r.Messages[0].Content}, nil
	}}
	resp, err := f.Chat(context.Background(), Request{Messages: []Message{{User, "ping"}}})
	if err != nil || resp.Content != "écho: ping" {
		t.Fatalf("%+v %v", resp, err)
	}
	v, _ := f.Moderate(context.Background(), []string{"a", "b"})
	if len(v) != 2 {
		t.Fatalf("default verdicts: %v", v)
	}
	fk := f.(*Fake)
	if len(fk.ChatCalls()) != 1 || len(fk.ModerationCalls()) != 1 || fk.ModerationCalls()[0][1] != "b" {
		t.Fatal("calls not recorded")
	}
	if (&Fake{}).ChatCalls() != nil {
		t.Fatal("empty Fake: no call expected")
	}
}

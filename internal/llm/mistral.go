package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Valeurs par défaut. Les noms de modèles de Mistral changent et sont retirés
// régulièrement (voir la page de cycle de vie des modèles): ils sont donc
// configurables et vérifiés par une sonde au démarrage, jamais supposés valides.
const (
	DefaultBaseURL         = "https://api.mistral.ai"
	DefaultModel           = "mistral-small-latest"
	DefaultModerationModel = "mistral-moderation-latest"

	maxErrorBody   = 300
	maxResponseLen = 1 << 20 // 1 Mo: une réponse de PNJ en fait quelques centaines d'octets
	maxRetryWait   = 10 * time.Second
)

// Mistral est un Client qui parle à l'API de Mistral AI.
type Mistral struct {
	apiKey          string
	baseURL         string
	model           string
	moderationModel string
	http            *http.Client
	retries         int
	backoff         time.Duration

	// Injectables pour les tests.
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func(d time.Duration) time.Duration
}

// Option configure un client Mistral.
type Option func(*Mistral)

// WithBaseURL remplace l'adresse de l'API (tests, proxy).
func WithBaseURL(u string) Option { return func(m *Mistral) { m.baseURL = strings.TrimRight(u, "/") } }

// WithModel fixe le modèle de chat par défaut.
func WithModel(name string) Option { return func(m *Mistral) { m.model = name } }

// WithModerationModel fixe le modèle de modération.
func WithModerationModel(name string) Option { return func(m *Mistral) { m.moderationModel = name } }

// WithHTTPClient remplace le client HTTP (délai par tentative, transport).
func WithHTTPClient(c *http.Client) Option { return func(m *Mistral) { m.http = c } }

// WithRetries fixe le nombre de reprises après un échec transitoire et le délai de base.
func WithRetries(n int, backoff time.Duration) Option {
	return func(m *Mistral) { m.retries, m.backoff = max(0, n), backoff }
}

// NewMistral crée un client. La clé n'est jamais journalisée ni incluse dans une erreur.
func NewMistral(apiKey string, opts ...Option) *Mistral {
	m := &Mistral{
		apiKey:          apiKey,
		baseURL:         DefaultBaseURL,
		model:           DefaultModel,
		moderationModel: DefaultModerationModel,
		http:            &http.Client{Timeout: 20 * time.Second},
		retries:         2,
		backoff:         500 * time.Millisecond,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		jitter: func(d time.Duration) time.Duration { return d/2 + time.Duration(rand.Int64N(int64(d/2)+1)) },
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// --- formes JSON de l'API ---

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireRequest struct {
	Model          string            `json:"model"`
	Messages       []wireMessage     `json:"messages"`
	Temperature    *float64          `json:"temperature,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	ResponseFormat *wireFormat       `json:"response_format,omitempty"`
	RandomSeed     *int              `json:"random_seed,omitempty"`
	Guardrails     []json.RawMessage `json:"guardrails,omitempty"`
}

type wireFormat struct {
	Type string `json:"type"`
}

// wireGuardrail est la forme documentée en novembre 2026:
// {"moderation_llm_v2": {"custom_category_thresholds": {...}, "action": "block", "block_on_error": true}}.
// Elle est à confirmer par la sonde réelle (voir npcai): l'API peut la refuser (HTTP 400/422).
type wireGuardrail struct {
	Moderation struct {
		Thresholds   map[string]float64 `json:"custom_category_thresholds,omitempty"`
		Action       string             `json:"action"`
		BlockOnError bool               `json:"block_on_error,omitempty"`
	} `json:"moderation_llm_v2"`
}

type wireResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			// Une chaîne, ou une liste de fragments {"type":"text","text":"..."}.
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Total      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat envoie une requête de complétion.
func (m *Mistral) Chat(ctx context.Context, req Request) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("%w: aucun message", ErrBadRequest)
	}
	w := wireRequest{Model: req.Model, Temperature: req.Temperature, MaxTokens: req.MaxTokens, RandomSeed: req.Seed}
	if w.Model == "" {
		w.Model = m.model
	}
	for _, msg := range req.Messages {
		w.Messages = append(w.Messages, wireMessage{Role: string(msg.Role), Content: msg.Content})
	}
	if req.JSON {
		w.ResponseFormat = &wireFormat{Type: "json_object"}
	}
	if g := req.Guardrail; g != nil {
		var wg wireGuardrail
		wg.Moderation.Thresholds, wg.Moderation.Action, wg.Moderation.BlockOnError = g.Thresholds, "block", g.BlockOnError
		raw, err := json.Marshal(wg)
		if err != nil {
			return Response{}, err
		}
		w.Guardrails = []json.RawMessage{raw}
	}

	body, err := m.post(ctx, "/v1/chat/completions", w)
	if err != nil {
		return Response{}, err
	}
	var wr wireResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		return Response{}, fmt.Errorf("%w: JSON invalide", ErrBadResponse)
	}
	if len(wr.Choices) == 0 {
		return Response{}, fmt.Errorf("%w: aucune réponse", ErrBadResponse)
	}
	content, err := decodeContent(wr.Choices[0].Message.Content)
	if err != nil {
		return Response{}, err
	}
	out := Response{Content: content, FinishReason: wr.Choices[0].FinishReason, Model: wr.Model}
	if wr.Usage != nil {
		out.Usage = Usage{Prompt: wr.Usage.Prompt, Completion: wr.Usage.Completion, Total: wr.Usage.Total}
	}
	return out, nil
}

// decodeContent lit le contenu d'un message: une chaîne, ou une liste de fragments.
func decodeContent(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("%w: contenu absent", ErrBadResponse)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var chunks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &chunks); err != nil {
		return "", fmt.Errorf("%w: contenu de forme inconnue", ErrBadResponse)
	}
	var b strings.Builder
	for _, c := range chunks {
		if c.Type == "text" || c.Type == "" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("%w: contenu vide", ErrBadResponse)
	}
	return b.String(), nil
}

type wireModeration struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type wireModerationResponse struct {
	Results []struct {
		Categories map[string]bool `json:"categories"`
	} `json:"results"`
}

// Moderate classe chaque texte avec l'endpoint de modération.
func (m *Mistral) Moderate(ctx context.Context, texts []string) ([]Verdict, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := m.post(ctx, "/v1/moderations", wireModeration{Model: m.moderationModel, Input: texts})
	if err != nil {
		return nil, err
	}
	var wr wireModerationResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		return nil, fmt.Errorf("%w: JSON invalide", ErrBadResponse)
	}
	if len(wr.Results) != len(texts) {
		return nil, fmt.Errorf("%w: %d verdicts pour %d textes", ErrBadResponse, len(wr.Results), len(texts))
	}
	out := make([]Verdict, len(wr.Results))
	for i, r := range wr.Results {
		for cat, flagged := range r.Categories {
			if flagged {
				out[i].Flagged = append(out[i].Flagged, cat)
			}
		}
		sortStrings(out[i].Flagged) // ordre stable pour les tests et les journaux
	}
	return out, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// post envoie un corps JSON avec reprises pour les échecs transitoires et
// renvoie le corps de la réponse 200.
func (m *Mistral) post(ctx context.Context, path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt <= m.retries; attempt++ {
		if attempt > 0 {
			wait := m.backoff << (attempt - 1)
			var ae *APIError
			if errors.As(last, &ae) && ae.RetryAfter > 0 {
				wait = ae.RetryAfter
			}
			if wait > maxRetryWait {
				return nil, last // attendre aussi longtemps ne sert à rien à un joueur qui patiente
			}
			if err := m.sleep(ctx, m.jitter(wait)); err != nil {
				return nil, err
			}
		}
		body, retry, err := m.once(ctx, path, data)
		if err == nil {
			return body, nil
		}
		last = err
		if !retry || ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

// once fait une tentative. retry indique si l'échec vaut la peine d'une reprise.
func (m *Mistral) once(ctx context.Context, path string, data []byte) (body []byte, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// L'erreur de transport peut citer l'URL mais jamais la clé (elle est dans un en-tête).
		return nil, true, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseLen))
	if err != nil {
		return nil, true, fmt.Errorf("%w: lecture de la réponse", ErrUnavailable)
	}
	if resp.StatusCode == http.StatusOK {
		return raw, false, nil
	}

	ae := &APIError{Status: resp.StatusCode, Body: truncate(string(raw), maxErrorBody)}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		ae.kind = ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		ae.kind = ErrBlocked
	case resp.StatusCode == http.StatusTooManyRequests:
		ae.kind = ErrRateLimited
		ae.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, true, ae
	case resp.StatusCode >= 500:
		ae.kind = ErrUnavailable
		return nil, true, ae
	default: // 400, 404, 422…
		ae.kind = ErrBadRequest
	}
	return nil, false, ae
}

// parseRetryAfter lit un en-tête Retry-After (secondes ou date HTTP).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

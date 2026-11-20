package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
	"github.com/kevin/ascii-adventure/internal/worldlore"
)

// apiKeyEnv is the environment variable that carries the API key. The key is
// deliberately NEVER a command-line option: it would show up in the
// process list and in the shell history.
const apiKeyEnv = "MISTRAL_API_KEY"

// superviseEvery is the interval between two new probes when the API is unavailable.
var superviseEvery = 5 * time.Minute

// llmOptions groups the settings of NPC dialogues.
type llmOptions struct {
	Mode            string // "auto", "on" or "off"
	Model           string
	ModerationModel string
	BaseURL         string // for a fake server in development; empty = official API
	DailyTokens     int
	Workers         int
	WorldLore       *worldlore.Canon // shared canon of the NPCs (nil = none)
}

// setupTalker builds the NPC dialogue service, or returns nil if it is disabled.
//
//	off:  never
//	auto: only if MISTRAL_API_KEY is set
//	on:   requires the key, otherwise an error (startup refused)
//
// An unreachable API or a rejected key does not block startup: the game works
// without dialogues, and the service resumes by itself if the API becomes available again.
func setupTalker(ctx context.Context, o llmOptions, st *store.Store, w *world.World, log *slog.Logger, getenv func(string) string) (*npcai.Service, error) {
	key := strings.TrimSpace(getenv(apiKeyEnv))
	switch o.Mode {
	case "off":
		return nil, nil
	case "on":
		if key == "" {
			return nil, fmt.Errorf("-llm=on requires the %s environment variable", apiKeyEnv)
		}
	case "auto", "":
		if key == "" {
			log.Info("NPC dialogues disabled: " + apiKeyEnv + " is not set")
			return nil, nil
		}
	default:
		return nil, fmt.Errorf("unknown -llm value: %q (expected auto, on or off)", o.Mode)
	}

	opts := []llm.Option{}
	if o.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(o.BaseURL))
	}
	if o.Model != "" {
		opts = append(opts, llm.WithModel(o.Model))
	}
	if o.ModerationModel != "" {
		opts = append(opts, llm.WithModerationModel(o.ModerationModel))
	}
	client := llm.NewMistral(key, opts...)

	cfg := npcai.DefaultConfig()
	if o.DailyTokens > 0 {
		cfg.DailyTokens = o.DailyTokens
	}
	if o.Workers > 0 {
		cfg.Workers = o.Workers
	}
	svc := npcai.New(client, &lore.Book{Repo: st, Deeds: st}, cfg,
		npcai.WithWares(st),
		npcai.WithLogger(log),
		npcai.WithProvider("Mistral AI"),
		npcai.WithWorldLore(o.WorldLore),
		npcai.WithPlaceNames(func(l i18n.Lang, rp world.RegionPos) string {
			if s := w.StructureOfRegion(rp); s != nil {
				return s.NameIn(l)
			}
			return ""
		}),
	)
	svc.Start(ctx)

	res := svc.Probe(ctx)
	switch {
	case res.Chat:
		log.Info("NPC dialogues enabled",
			"model", orDefault(o.Model, llm.DefaultModel), "guardrails", res.Guardrails, "moderation", res.Moderation)
		if !res.Moderation {
			log.Warn("moderation unavailable: NPCs talk but retain no introduction", "err", errText(res.ModerationErr))
		}
	default:
		// The key never appears in an llm error (see TestAPIKeyNeverAppearsInErrors).
		log.Warn("NPC dialogues unavailable for now: the API is not responding as expected", "err", errText(res.ChatErr))
	}
	go svc.Supervise(ctx, superviseEvery)
	return svc, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	var ae *llm.APIError
	if errors.As(err, &ae) {
		return ae.Error()
	}
	return err.Error()
}

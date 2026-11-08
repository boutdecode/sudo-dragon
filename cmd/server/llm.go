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
)

// apiKeyEnv est la variable d'environnement qui porte la clé d'API. La clé n'est
// volontairement JAMAIS une option de ligne de commande : elle apparaîtrait dans la
// liste des processus et dans l'historique du shell.
const apiKeyEnv = "MISTRAL_API_KEY"

// superviseEvery est l'intervalle entre deux nouvelles sondes quand l'API est indisponible.
var superviseEvery = 5 * time.Minute

// llmOptions regroupe les réglages des dialogues de PNJ.
type llmOptions struct {
	Mode            string // "auto", "on" ou "off"
	Model           string
	ModerationModel string
	BaseURL         string // pour un faux serveur en développement ; vide = API officielle
	DailyTokens     int
	Workers         int
}

// setupTalker construit le service de dialogue des PNJ, ou renvoie nil s'il est désactivé.
//
//	off  : jamais
//	auto : seulement si MISTRAL_API_KEY est définie
//	on   : exige la clé, sinon erreur (démarrage refusé)
//
// Une API injoignable ou une clé refusée ne bloque pas le démarrage : le jeu fonctionne
// sans dialogues, et le service reprend tout seul si l'API redevient disponible.
func setupTalker(ctx context.Context, o llmOptions, st *store.Store, w *world.World, log *slog.Logger, getenv func(string) string) (*npcai.Service, error) {
	key := strings.TrimSpace(getenv(apiKeyEnv))
	switch o.Mode {
	case "off":
		return nil, nil
	case "on":
		if key == "" {
			return nil, fmt.Errorf("-llm=on exige la variable d'environnement %s", apiKeyEnv)
		}
	case "auto", "":
		if key == "" {
			log.Info("dialogues de PNJ désactivés : " + apiKeyEnv + " n'est pas définie")
			return nil, nil
		}
	default:
		return nil, fmt.Errorf("valeur de -llm inconnue : %q (attendu auto, on ou off)", o.Mode)
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
		npcai.WithLogger(log),
		npcai.WithProvider("Mistral AI"),
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
		log.Info("dialogues de PNJ activés",
			"modèle", orDefault(o.Model, llm.DefaultModel), "garde-fous", res.Guardrails, "modération", res.Moderation)
		if !res.Moderation {
			log.Warn("modération indisponible : les PNJ parlent mais ne retiennent aucune présentation", "err", errText(res.ModerationErr))
		}
	default:
		// La clé n'apparaît jamais dans une erreur de llm (voir TestAPIKeyNeverAppearsInErrors).
		log.Warn("dialogues de PNJ indisponibles pour l'instant : l'API ne répond pas comme attendu", "err", errText(res.ChatErr))
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

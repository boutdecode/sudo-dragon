package npcai

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Étalage des vendeurs d'équipement. Le modèle propose une spécialité et des objets en JSON ; chaque objet
// passe par gear.Validate (bornes calculées par le serveur) et ses textes par la modération. Aucun texte
// de joueur n'entre dans cette demande.

// StockVersion identifie la version du prompt d'étalage (enregistrée avec l'assortiment).
// La version 3 passe au combat par dés (famille et qualité des armes, réduction des armures) : les étalages plus
// anciens sont régénérés.
const StockVersion = 3

const (
	stockMaxTokens = 900
	minStockItems  = 4
	maxStockItems  = 8
	maxSpecialty   = 80
)

// WaresRepo est le stockage des assortiments (implémenté par internal/store).
type WaresRepo interface {
	GetWares(ctx context.Context, npc world.NPCKey) (gear.Wares, bool, error)
	PutWares(ctx context.Context, w gear.Wares) error
}

// StockRequest demande l'assortiment d'un vendeur.
type StockRequest struct {
	NPC       world.NPC
	Lang      i18n.Lang
	Place     string
	PlaceKind world.Kind
	Region    string
	Context   gear.Context // emplacements vendus et palier maximal du lieu
}

// StockReply est l'assortiment d'un vendeur, ou la raison de son absence (le jeu utilise alors son repli).
type StockReply struct {
	Wares     gear.Wares
	Failure   Failure
	FromStore bool // relu de la base : aucun appel au modèle
}

// WithWares branche le stockage des assortiments.
func WithWares(r WaresRepo) Option { return func(s *Service) { s.wares = r } }

// BuildStockMessages assemble la demande d'étalage.
func BuildStockMessages(req StockRequest) []llm.Message {
	l := req.Lang
	n := req.NPC
	var b strings.Builder
	b.WriteString(i18n.T(l, "prompt.incarnate", n.NameIn(l), jobLabel(l, n.Kind), placeLabel(l, req.PlaceKind, req.Place)))
	b.WriteString("\n")
	persona := n.PersonaIn(l)
	b.WriteString(i18n.T(l, "prompt.persona", persona.Temperament, persona.Quirk, persona.Speech))
	b.WriteString("\n")
	if req.Region != "" {
		b.WriteString(i18n.T(l, "prompt.region", req.Region))
		b.WriteString("\n")
	}
	slots := make([]string, len(req.Context.Slots))
	for i, s := range req.Context.Slots {
		slots[i] = string(s)
	}
	b.WriteString("\n")
	var fams []string
	for _, sl := range req.Context.Slots {
		if f := gear.Families(sl); len(f) > 0 {
			fams = append(fams, string(sl)+": "+strings.Join(f, ", "))
		}
	}
	b.WriteString(i18n.T(l, "prompt.stock.rules", strings.Join(slots, ", "), req.Context.MaxTier, req.Context.MaxTier, strings.Join(fams, " ; ")))
	b.WriteString("\n\n")
	b.WriteString(i18n.T(l, "prompt.stock.format"))
	return []llm.Message{
		{Role: llm.System, Content: b.String()},
		{Role: llm.User, Content: i18n.T(l, "prompt.stock.stage")},
	}
}

type rawStockItem struct {
	Slot  flexString `json:"slot"`
	Name  flexString `json:"name"`
	Desc  flexString `json:"desc"`
	Tier  flexInt    `json:"tier"`
	Def   flexInt    `json:"def"`
	Fam   flexString `json:"family"`
	Qual  flexInt    `json:"quality"`
	Price flexInt    `json:"price"`
	Kind  flexString `json:"kind"`
}

type rawStock struct {
	Specialty flexString     `json:"specialty"`
	Items     []rawStockItem `json:"items"`
}

// parseStock lit la réponse du modèle de façon tolérante (clôtures, texte autour du JSON, champs mal typés).
func parseStock(content string) (specialty string, props []gear.Proposal, ok bool) {
	body := extractJSON(content)
	if body == "" {
		return "", nil, false
	}
	var raw rawStock
	if err := json.Unmarshal([]byte(body), &raw); err != nil || len(raw.Items) == 0 {
		return "", nil, false
	}
	for _, it := range raw.Items {
		props = append(props, gear.Proposal{
			Slot: string(it.Slot), Name: string(it.Name), Desc: string(it.Desc),
			Tier: int(it.Tier), Family: string(it.Fam), Quality: int(it.Qual), Def: int(it.Def), Price: int(it.Price), Kind: string(it.Kind),
		})
	}
	return textsafe.Clean(string(raw.Specialty), maxSpecialty), props, true
}

// Stock donne l'assortiment d'un vendeur : celui enregistré s'il existe, sinon il est généré une fois, validé,
// modéré et enregistré. Ne bloque jamais ; n'appelle done que depuis une autre goroutine que l'appelant.
func (s *Service) Stock(req StockRequest, done func(StockReply)) {
	fail := func(f Failure) {
		s.nFailures.Add(1)
		go done(StockReply{Failure: f})
	}
	key := req.NPC.Key()
	s.mu.Lock()
	s.rollDay(s.now())
	switch {
	case s.stockInflight[key]:
		s.mu.Unlock()
		fail(Busy)
		return
	case s.tokens+stockMaxTokens > s.cfg.DailyTokens:
		s.mu.Unlock()
		fail(Budget)
		return
	}
	if s.stockInflight == nil {
		s.stockInflight = map[world.NPCKey]bool{}
	}
	s.stockInflight[key] = true
	s.mu.Unlock()

	release := func() { s.mu.Lock(); delete(s.stockInflight, key); s.mu.Unlock() }
	select {
	case s.queue <- &job{stock: &req, stockDone: func(r StockReply) { release(); done(r) }, enqueued: s.now()}:
	default:
		release()
		fail(Busy)
	}
}

// processStock est exécuté par un worker.
func (s *Service) processStock(parent context.Context, req StockRequest) StockReply {
	ctx, cancel := context.WithTimeout(parent, s.cfg.Timeout)
	defer cancel()
	key := req.NPC.Key()

	var stale *gear.Wares // étalage d'une version plus ancienne: il sert tant qu'on ne sait pas le régénérer
	if s.wares != nil {
		if w, ok, err := s.wares.GetWares(ctx, key); err != nil {
			s.log.Warn("lecture de l'assortiment", "err", err)
		} else if ok && len(w.Items) > 0 {
			w.Items = gear.SanitizeItems(w.Items) // objets d'une ancienne version: convertis
			if w.Version >= StockVersion {
				return StockReply{Wares: w, FromStore: true}
			}
			stale = &w
		}
	}
	r := s.generateStock(ctx, req, key)
	if len(r.Wares.Items) == 0 && stale != nil {
		return StockReply{Wares: *stale, FromStore: true}
	}
	return r
}

// generateStock demande un nouvel étalage au modèle, le valide, le modère et l'enregistre.
func (s *Service) generateStock(ctx context.Context, req StockRequest, key world.NPCKey) StockReply {
	if !s.Available() {
		return StockReply{Failure: Unavailable}
	}

	temp := s.cfg.Temperature
	resp, err := s.llm.Chat(ctx, llm.Request{
		Messages: BuildStockMessages(req), JSON: true, MaxTokens: stockMaxTokens, Temperature: &temp,
	})
	if err != nil {
		return StockReply{Failure: s.chatFailure(Request{NPC: req.NPC, Lang: req.Lang}, err).Failure}
	}
	s.success(resp.TotalTokens(llm.Request{MaxTokens: stockMaxTokens}))

	specialty, props, ok := parseStock(resp.Content)
	if !ok {
		s.nStockRejected.Add(1)
		s.log.Warn("assortiment inexploitable")
		return StockReply{Failure: Unavailable}
	}
	var items []gear.Item
	names := map[string]bool{}
	for _, p := range props {
		it, err := gear.Validate(p, req.Context)
		if err != nil || names[strings.ToLower(it.Name)] {
			continue
		}
		names[strings.ToLower(it.Name)] = true
		it.ID = int64(len(items) + 1)
		items = append(items, it)
		if len(items) == maxStockItems {
			break
		}
	}
	if specialty == "" || lore.LooksLikeLink(specialty) || len(items) < minStockItems {
		s.nStockRejected.Add(1)
		s.log.Info("assortiment écarté", "objets_valides", len(items))
		return StockReply{Failure: Unavailable}
	}
	texts := []string{specialty}
	for _, it := range items {
		texts = append(texts, it.Name)
		if it.Desc != "" {
			texts = append(texts, it.Desc)
		}
	}
	if err := s.moderator(ctx, texts); err != nil {
		s.nStockRejected.Add(1)
		s.log.Info("assortiment non modéré", "raison", err.Error())
		return StockReply{Failure: Blocked}
	}

	w := gear.Wares{NPC: key, Specialty: specialty, Items: items, Version: StockVersion, CreatedAt: s.now()}
	if s.wares != nil {
		if err := s.wares.PutWares(ctx, w); err != nil {
			s.log.Warn("écriture de l'assortiment", "err", err) // l'étalage sert quand même, mais ne sera pas figé
		}
	}
	s.nStockGenerated.Add(1)
	return StockReply{Wares: w}
}

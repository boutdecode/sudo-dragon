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
	"github.com/kevin/ascii-adventure/internal/worldlore"
)

// Equipment vendors' stock. The model proposes a specialty and items in JSON; each item
// goes through gear.Validate (bounds computed by the server) and its texts through moderation. No player
// text enters this request.

// StockVersion identifies the stock prompt version (saved with the assortment).
// Version 3 moves to dice-based combat (weapon family and quality, armor reduction): older
// stocks are regenerated.
const StockVersion = 3

const (
	stockMaxTokens = 900
	minStockItems  = 4
	maxStockItems  = 8
	maxSpecialty   = 80
)

// WaresRepo is the storage for assortments (implemented by internal/store).
type WaresRepo interface {
	GetWares(ctx context.Context, npc world.NPCKey) (gear.Wares, bool, error)
	PutWares(ctx context.Context, w gear.Wares) error
}

// StockRequest asks for a vendor's assortment.
type StockRequest struct {
	NPC       world.NPC
	Lang      i18n.Lang
	Place     string
	PlaceKind world.Kind
	Region    string
	Context   gear.Context // slots sold and maximum tier of the place

	// WorldLore is the world's canon; the service fills it in (see WithWorldLore).
	WorldLore worldlore.Text
}

// StockReply is a vendor's assortment, or the reason for its absence (the game then uses its fallback).
type StockReply struct {
	Wares     gear.Wares
	Failure   Failure
	FromStore bool // reread from the database: no model call
}

// WithWares plugs in the assortment storage.
func WithWares(r WaresRepo) Option { return func(s *Service) { s.wares = r } }

// BuildStockMessages assembles the stock request.
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
	writeWorldLore(&b, l, req.WorldLore)
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
	if !req.WorldLore.Empty() {
		b.WriteString(i18n.T(l, "prompt.stock.lore_rules"))
		b.WriteString("\n\n")
	}
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

// parseStock reads the model response leniently (fences, text around the JSON, wrongly typed fields).
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

// Stock gives a vendor's assortment: the saved one if it exists, otherwise it is generated once, validated,
// moderated and saved. Never blocks; calls done only from a goroutine other than the caller's.
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

// processStock is run by a worker.
func (s *Service) processStock(parent context.Context, req StockRequest) StockReply {
	ctx, cancel := context.WithTimeout(parent, s.cfg.Timeout)
	defer cancel()
	key := req.NPC.Key()

	var stale *gear.Wares // stock from an older version: it is used as long as it cannot be regenerated
	if s.wares != nil {
		if w, ok, err := s.wares.GetWares(ctx, key); err != nil {
			s.log.Warn("reading assortment", "err", err)
		} else if ok && len(w.Items) > 0 {
			w.Items = gear.SanitizeItems(w.Items) // items from an older version: converted
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

// generateStock asks the model for a new stock, validates it, moderates it and saves it.
func (s *Service) generateStock(ctx context.Context, req StockRequest, key world.NPCKey) StockReply {
	if !s.Available() {
		return StockReply{Failure: Unavailable}
	}

	temp := s.cfg.Temperature
	req.WorldLore = s.canon.For(req.Lang)
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
		s.log.Warn("unusable assortment")
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
		s.log.Info("assortment discarded", "valid_items", len(items))
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
		s.log.Info("assortment not moderated", "reason", err.Error())
		return StockReply{Failure: Blocked}
	}

	w := gear.Wares{NPC: key, Specialty: specialty, Items: items, Version: StockVersion, CreatedAt: s.now()}
	if s.wares != nil {
		if err := s.wares.PutWares(ctx, w); err != nil {
			s.log.Warn("writing assortment", "err", err) // the stock is still served, but will not be frozen
		}
	}
	s.nStockGenerated.Add(1)
	return StockReply{Wares: w}
}

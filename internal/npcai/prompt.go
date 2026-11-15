package npcai

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// PromptVersion identifies the prompt version (for logs and experiments).
const PromptVersion = 2

// Data coming from players (introductions, memories, nicknames) is placed
// in an ESCAPED JSON block, never in the prose of the rules: a player cannot
// close the block nor pass themselves off as an instruction in it. This is not a
// guarantee against a compliant model, it is a first barrier; the others are
// the model's total lack of power over the game (it only produces text),
// moderation, and the brevity of the cards.

type factsJSON struct {
	Race             string `json:"race,omitempty"` // interlocutor only: race chosen at creation
	Niveau           int    `json:"niveau,omitempty"`
	MonstresVaincus  int    `json:"monstres_vaincus,omitempty"`
	SeigneursVaincus int    `json:"seigneurs_de_guerre_vaincus,omitempty"`
	Defaites         int    `json:"defaites,omitempty"`
}

type cardJSON struct {
	Alias  string `json:"nom_ou_surnom,omitempty"`
	Role   string `json:"metier_ou_rang,omitempty"`
	Origin string `json:"origine,omitempty"`
	Goal   string `json:"but,omitempty"`
	Trait  string `json:"trait,omitempty"`
}

type rumorJSON struct {
	Pseudo     string    `json:"pseudo"`
	Provenance string    `json:"provenance"`
	Said       cardJSON  `json:"dit_de_lui"`
	Certain    factsJSON `json:"faits_certains"`
}

type reputationJSON struct {
	Tier   string `json:"palier"`
	Points int    `json:"points"`
}

type deedJSON struct {
	Title string `json:"quete"`
	When  string `json:"quand"`
}

type hearsayDeedJSON struct {
	Pseudo     string `json:"pseudo"`
	Title      string `json:"quete"`
	Provenance string `json:"provenance"`
}

type interlocutorJSON struct {
	Pseudo       string          `json:"pseudo"`
	Certain      factsJSON       `json:"faits_certains"`
	Presentation *cardJSON       `json:"sa_presentation"`
	Hearsay      *cardJSON       `json:"ce_qu_on_raconte_de_lui,omitempty"` // card received through rumor: the NPC never met them
	Memory       string          `json:"mon_souvenir_de_lui"`
	Reputation   *reputationJSON `json:"reputation_ici,omitempty"`      // fact established by the server
	DeedsDone    []deedJSON      `json:"quetes_rendues_ici,omitempty"`  // facts established by the server
	Impression   string          `json:"premiere_impression,omitempty"` // charisma, computed by the server
}

type speciesJSON struct {
	ID   string `json:"id"`
	Name string `json:"nom"`
	XP   int    `json:"xp"`
}

type offerJSON struct {
	Species  []speciesJSON `json:"especes_autorisees"`
	Places   []string      `json:"lieux_atteignables,omitempty"`
	Boss     bool          `json:"seigneur_de_guerre_possible,omitempty"`
	Dungeons []string      `json:"donjons_secrets,omitempty"`
	CountMin int           `json:"nombre_min"`
	CountMax int           `json:"nombre_max"`
	GoldCap  string        `json:"plafond_or"`
	Existing []string      `json:"deja_confiees,omitempty"`
}

type landmarkJSON struct {
	Name      string `json:"nom"`
	Kind      string `json:"genre"`
	Region    string `json:"contree,omitempty"`
	Direction string `json:"direction"`
	Distance  string `json:"distance"`
	Note      string `json:"a_savoir,omitempty"`
}

type residentJSON struct {
	Name      string `json:"nom"`
	Job       string `json:"metier"`
	Direction string `json:"direction"`
	Steps     int    `json:"pas"`
}

type dataJSON struct {
	Landmarks    []landmarkJSON    `json:"lieux_connus,omitempty"`
	Residents    []residentJSON    `json:"habitants_du_lieu,omitempty"`
	Interlocutor interlocutorJSON  `json:"interlocuteur"`
	Rumors       []rumorJSON       `json:"rumeurs"`
	DeedRumors   []hearsayDeedJSON `json:"quetes_rendues_par_d_autres,omitempty"`
	Offer        *offerJSON        `json:"travail_a_confier,omitempty"`
}

func cardOf(in lore.Intro) cardJSON {
	return cardJSON{Alias: in.Alias, Role: in.Role, Origin: in.Origin, Goal: in.Goal, Trait: in.Trait}
}

func factsOf(f lore.Facts) factsJSON {
	return factsJSON{Niveau: f.Level, SeigneursVaincus: f.BossKills, Defaites: f.Deaths}
}

// age expresses a duration in everyday language ("5 min ago" / "il y a 5 min").
func age(l i18n.Lang, d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T(l, "prompt.age.now")
	case d < time.Hour:
		return i18n.T(l, "prompt.age.min", int(d/time.Minute))
	case d < 48*time.Hour:
		return i18n.T(l, "prompt.age.hours", int(d/time.Hour))
	}
	return i18n.T(l, "prompt.age.days", int(d/(24*time.Hour)))
}

func jobLabel(l i18n.Lang, k world.NPCKind) string {
	switch k {
	case world.NPCSmith:
		return i18n.T(l, "prompt.job.smith")
	case world.NPCArmorer:
		return i18n.T(l, "prompt.job.armorer")
	case world.NPCGuard:
		return i18n.T(l, "prompt.job.guard")
	case world.NPCChief:
		return i18n.T(l, "prompt.job.chief")
	case world.NPCMayor:
		return i18n.T(l, "prompt.job.mayor")
	case world.NPCHealer:
		return i18n.T(l, "prompt.job.healer")
	case world.NPCVillager:
		return i18n.T(l, "prompt.job.villager")
	}
	return i18n.T(l, "prompt.job.merchant")
}

// placeLabel returns the place with its article: "of the village of X", "of the town of X".
func placeLabel(l i18n.Lang, k world.Kind, name string) string {
	switch k {
	case world.KindTown:
		return i18n.T(l, "prompt.place.town", name)
	case world.KindFortress:
		return i18n.T(l, "prompt.place.fortress", name)
	}
	return i18n.T(l, "prompt.place.village", name)
}

// peopleLabel names a race as a people, in the plural ("Dwarves", "Nains").
func peopleLabel(l i18n.Lang, r hero.Race) string { return i18n.T(l, "prompt.people."+string(r)) }

// hatredRule is the instruction matching a hatred level (in %), or "" below world.HateWary.
func hatredRule(h int) string {
	switch {
	case h >= world.HateNoQuest:
		return "prompt.hatred.hate"
	case h >= world.HateHostile:
		return "prompt.hatred.hostile"
	case h >= world.HateWary:
		return "prompt.hatred.wary"
	}
	return ""
}

// BuildMessages assembles the conversation sent to the model.
//
// heard is what this NPC knows (see lore.Book.Heard), memory its private memory of this
// player. Neither contains another player's private memory.
func BuildMessages(req Request, heard []lore.Heard, memory string, maxHistory int, placeNames func(i18n.Lang, world.RegionPos) string) []llm.Message {
	l := req.Lang
	var b strings.Builder
	n := req.NPC
	b.WriteString(i18n.T(l, "prompt.incarnate", n.NameIn(l), jobLabel(l, n.Kind), placeLabel(l, req.PlaceKind, req.Place)))
	b.WriteString("\n")
	persona := n.PersonaIn(l)
	b.WriteString(i18n.T(l, "prompt.persona", persona.Temperament, persona.Quirk, persona.Speech))
	b.WriteString("\n")
	if n.Race != "" {
		b.WriteString(i18n.T(l, "prompt.npc_race", peopleLabel(l, n.Race)))
		b.WriteString("\n")
	}
	if req.Region != "" {
		b.WriteString(i18n.T(l, "prompt.region", req.Region))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if n.Kind == world.NPCVillager {
		b.WriteString(i18n.T(l, "prompt.villager_rule"))
	}
	if n.Kind == world.NPCGuard {
		b.WriteString(i18n.T(l, "prompt.guard_rule"))
		b.WriteString("\n")
		if len(req.Landmarks) > 0 {
			b.WriteString(i18n.T(l, "prompt.geography_rule"))
		} else {
			b.WriteString(i18n.T(l, "prompt.geography_none"))
		}
		b.WriteString("\n")
	}
	if n.Kind == world.NPCChief || n.Kind == world.NPCMayor {
		b.WriteString(i18n.T(l, "prompt.official_rule"))
		b.WriteString("\n")
		if len(req.Landmarks) > 0 {
			b.WriteString(i18n.T(l, "prompt.geography_rule"))
		} else {
			b.WriteString(i18n.T(l, "prompt.geography_none"))
		}
		b.WriteString("\n")
	}
	speakerRace := req.Speaker.Race.OrDefault()
	hate := n.HatredOf(speakerRace)
	if rule := hatredRule(hate); rule != "" {
		b.WriteString(i18n.T(l, rule, peopleLabel(l, speakerRace), hate))
		b.WriteString("\n")
		b.WriteString(i18n.T(l, "prompt.hatred.frame"))
		b.WriteString("\n")
	}
	if req.Offer != nil {
		b.WriteString(i18n.T(l, "prompt.quest_rules"))
		b.WriteString("\n")
	} else if n.GivesQuests && hate >= world.HateNoQuest {
		b.WriteString(i18n.T(l, "prompt.hatred.no_work", peopleLabel(l, speakerRace))) // never works for a race it hates
		b.WriteString("\n")
	} else if n.GivesQuests {
		b.WriteString(i18n.T(l, "prompt.no_work_rule")) // quest giver with nothing to hand out right now
		b.WriteString("\n")
	}
	b.WriteString(i18n.T(l, "prompt.system_rules"))
	b.WriteString("\n\n")

	met := memory != "" // only a private memory proves this NPC has already spoken to this player
	data := dataJSON{Interlocutor: interlocutorJSON{
		Pseudo: req.Speaker.Account,
		Certain: factsJSON{
			Race:   i18n.T(l, "hero.race."+string(speakerRace)),
			Niveau: req.Speaker.Level, MonstresVaincus: req.Speaker.Kills,
			SeigneursVaincus: req.Speaker.BossKills, Defaites: req.Speaker.Deaths,
		},
		Memory: memory,
	}, Rumors: []rumorJSON{}}
	for _, h := range heard {
		if h.Self {
			c := cardOf(h.Intro)
			if met {
				data.Interlocutor.Presentation = &c
			} else {
				data.Interlocutor.Hearsay = &c
			}
			continue
		}
		prov := i18n.T(l, "prompt.prov.here", age(l, h.Age))
		if !h.Local() {
			from := i18n.T(l, "prompt.prov.other_place")
			if placeNames != nil {
				if name := placeNames(l, h.Place); name != "" {
					from = name
				}
			}
			prov = i18n.T(l, "prompt.prov.hearsay", from, age(l, h.Age))
		}
		data.Rumors = append(data.Rumors, rumorJSON{Pseudo: h.Account, Provenance: prov, Said: cardOf(h.Intro), Certain: factsOf(h.Facts)})
	}
	for _, lm := range req.Landmarks {
		if n.Kind != world.NPCChief && n.Kind != world.NPCMayor && n.Kind != world.NPCGuard {
			break // only chiefs, mayors and guards know the surroundings, whatever is passed to them
		}
		data.Landmarks = append(data.Landmarks, landmarkJSON{Name: lm.Name, Kind: lm.Kind, Region: lm.Region, Direction: lm.Direction, Distance: lm.Distance, Note: lm.Note})
	}
	if n.Kind == world.NPCGuard {
		for _, r := range req.Residents {
			data.Residents = append(data.Residents, residentJSON{Name: r.Name, Job: r.Job, Direction: r.Direction, Steps: r.Steps})
		}
	}
	data.Interlocutor.Impression = textsafe.Clean(req.Impression, 40)
	if req.Reputation != 0 {
		data.Interlocutor.Reputation = &reputationJSON{Tier: quest.TierLabel(l, req.Reputation), Points: req.Reputation}
	}
	for _, d := range req.Deeds.Mine {
		data.Interlocutor.DeedsDone = append(data.Interlocutor.DeedsDone, deedJSON{Title: d.Title, When: age(l, d.Age)})
	}
	for _, d := range req.Deeds.Others {
		prov := i18n.T(l, "prompt.prov.here", age(l, d.Age))
		if !d.Local() {
			from := i18n.T(l, "prompt.prov.other_place")
			if placeNames != nil {
				if name := placeNames(l, d.Place); name != "" {
					from = name
				}
			}
			prov = i18n.T(l, "prompt.prov.hearsay", from, age(l, d.Age))
		}
		data.DeedRumors = append(data.DeedRumors, hearsayDeedJSON{Pseudo: d.Account, Title: d.Title, Provenance: prov})
	}
	if o := req.Offer; o != nil {
		oj := &offerJSON{Boss: o.Quest.BossAround && o.Quest.Level >= quest.BossLevel, CountMin: quest.MinCount, CountMax: quest.MaxCount,
			GoldCap: i18n.T(l, "prompt.quest_gold_cap"), Existing: o.Existing}
		for _, sp := range o.Quest.Species {
			oj.Species = append(oj.Species, speciesJSON{ID: sp.Kind, Name: i18n.T(l, "monster."+sp.Kind), XP: sp.XP})
		}
		for name := range o.Quest.Places {
			oj.Places = append(oj.Places, name)
		}
		slices.Sort(oj.Places) // stable order: reproducible prompt
		for name := range o.Quest.Dungeons {
			oj.Dungeons = append(oj.Dungeons, name)
		}
		slices.Sort(oj.Dungeons)
		data.Offer = oj
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // "<" and "&" stay readable; quotes, however, are always escaped
	_ = enc.Encode(data)

	b.WriteString(i18n.T(l, "prompt.data_header"))
	b.WriteString("\n")
	b.Write(bytes.TrimSpace(buf.Bytes()))
	b.WriteString("\n\n")
	hasCard := data.Interlocutor.Presentation != nil || data.Interlocutor.Hearsay != nil
	if !met {
		b.WriteString(i18n.T(l, "prompt.first_meeting"))
		b.WriteString("\n\n")
	}
	if !hasCard {
		b.WriteString(i18n.T(l, "prompt.not_introduced"))
		b.WriteString("\n\n")
	}
	if req.Opening {
		switch {
		case met && hasCard:
			b.WriteString(i18n.T(l, "prompt.opening.known"))
		case hasCard:
			b.WriteString(i18n.T(l, "prompt.opening.heard"))
		default:
			b.WriteString(i18n.T(l, "prompt.opening.stranger"))
		}
		b.WriteString("\n\n")
	}
	b.WriteString(i18n.T(l, "prompt.output_format"))
	if req.Offer != nil {
		b.WriteString("\n")
		b.WriteString(i18n.T(l, "prompt.quest_format"))
	}

	msgs := []llm.Message{{Role: llm.System, Content: b.String()}}
	hist := req.History
	if maxHistory > 0 && len(hist) > maxHistory {
		hist = hist[len(hist)-maxHistory:]
	}
	for _, t := range hist {
		msgs = append(msgs,
			llm.Message{Role: llm.User, Content: t.Player},
			llm.Message{Role: llm.Assistant, Content: assistantJSON(t.NPC)},
		)
	}
	last := req.Message
	if req.Opening {
		last = i18n.T(l, "prompt.opening.stage")
	}
	return append(msgs, llm.Message{Role: llm.User, Content: last})
}

// assistantJSON presents an earlier NPC reply in the requested output format,
// so that the model does not drift toward free text as the conversation goes on.
func assistantJSON(reply string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"reply": reply, "intro": nil, "memory": ""})
	return strings.TrimSpace(buf.String())
}

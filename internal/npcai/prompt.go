package npcai

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/world"
)

// PromptVersion identifie la version du prompt (pour les journaux et les essais).
const PromptVersion = 1

// Les données issues de joueurs (présentations, souvenirs, pseudos) sont placées
// dans un bloc JSON ÉCHAPPÉ, jamais dans la prose des règles: un joueur ne peut pas
// refermer le bloc ni s'y faire passer pour une instruction. Ce n'est pas une
// garantie contre un modèle docile, c'est une première barrière; les autres sont
// l'absence totale de pouvoir du modèle sur le jeu (il ne produit que du texte),
// la modération, et la brièveté des fiches.

type factsJSON struct {
	Niveau           int `json:"niveau,omitempty"`
	MonstresVaincus  int `json:"monstres_vaincus,omitempty"`
	SeigneursVaincus int `json:"seigneurs_de_guerre_vaincus,omitempty"`
	Defaites         int `json:"defaites,omitempty"`
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
	Memory       string          `json:"mon_souvenir_de_lui"`
	Reputation   *reputationJSON `json:"reputation_ici,omitempty"`     // fait établi par le serveur
	DeedsDone    []deedJSON      `json:"quetes_rendues_ici,omitempty"` // faits établis par le serveur
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
	CountMin int           `json:"nombre_min"`
	CountMax int           `json:"nombre_max"`
	GoldCap  string        `json:"plafond_or"`
	Existing []string      `json:"deja_confiees,omitempty"`
}

type dataJSON struct {
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

// age exprime une durée en langage courant ("il y a 5 min" / "5 min ago").
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
	case world.NPCHealer:
		return i18n.T(l, "prompt.job.healer")
	case world.NPCVillager:
		return i18n.T(l, "prompt.job.villager")
	}
	return i18n.T(l, "prompt.job.merchant")
}

// placeLabel renvoie le lieu avec son article: « du village de X », « de la ville de X ».
func placeLabel(l i18n.Lang, k world.Kind, name string) string {
	switch k {
	case world.KindTown:
		return i18n.T(l, "prompt.place.town", name)
	case world.KindFortress:
		return i18n.T(l, "prompt.place.fortress", name)
	}
	return i18n.T(l, "prompt.place.village", name)
}

// BuildMessages assemble la conversation envoyée au modèle.
//
// heard est ce que ce PNJ sait (voir lore.Book.Heard), memory son souvenir privé de ce
// joueur. Ni l'un ni l'autre ne contient le souvenir privé d'un autre joueur.
func BuildMessages(req Request, heard []lore.Heard, memory string, maxHistory int, placeNames func(i18n.Lang, world.RegionPos) string) []llm.Message {
	l := req.Lang
	var b strings.Builder
	n := req.NPC
	b.WriteString(i18n.T(l, "prompt.incarnate", n.NameIn(l), jobLabel(l, n.Kind), placeLabel(l, req.PlaceKind, req.Place)))
	b.WriteString("\n")
	persona := n.PersonaIn(l)
	b.WriteString(i18n.T(l, "prompt.persona", persona.Temperament, persona.Quirk, persona.Speech))
	b.WriteString("\n")
	if req.Region != "" {
		b.WriteString(i18n.T(l, "prompt.region", req.Region))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if n.Kind == world.NPCVillager {
		b.WriteString(i18n.T(l, "prompt.villager_rule"))
	}
	if req.Offer != nil {
		b.WriteString(i18n.T(l, "prompt.quest_rules"))
		b.WriteString("\n")
	}
	b.WriteString(i18n.T(l, "prompt.system_rules"))
	b.WriteString("\n\n")

	data := dataJSON{Interlocutor: interlocutorJSON{
		Pseudo: req.Speaker.Account,
		Certain: factsJSON{
			Niveau: req.Speaker.Level, MonstresVaincus: req.Speaker.Kills,
			SeigneursVaincus: req.Speaker.BossKills, Defaites: req.Speaker.Deaths,
		},
		Memory: memory,
	}, Rumors: []rumorJSON{}}
	for _, h := range heard {
		if h.Self {
			c := cardOf(h.Intro)
			data.Interlocutor.Presentation = &c
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
	if req.Reputation > 0 {
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
		slices.Sort(oj.Places) // ordre stable: prompt reproductible
		data.Offer = oj
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // « < » et « & » restent lisibles; les guillemets, eux, sont toujours échappés
	_ = enc.Encode(data)

	b.WriteString(i18n.T(l, "prompt.data_header"))
	b.WriteString("\n")
	b.Write(bytes.TrimSpace(buf.Bytes()))
	b.WriteString("\n\n")
	if data.Interlocutor.Presentation == nil {
		b.WriteString(i18n.T(l, "prompt.not_introduced"))
		b.WriteString("\n\n")
	}
	if req.Opening {
		if data.Interlocutor.Presentation == nil {
			b.WriteString(i18n.T(l, "prompt.opening.stranger"))
		} else {
			b.WriteString(i18n.T(l, "prompt.opening.known"))
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

// assistantJSON présente une ancienne réplique du PNJ dans le format de sortie demandé,
// pour que le modèle ne dérive pas vers du texte libre au fil de la conversation.
func assistantJSON(reply string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"reply": reply, "intro": nil, "memory": ""})
	return strings.TrimSpace(buf.String())
}

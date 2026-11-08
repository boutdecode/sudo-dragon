package npcai

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/quest"
	"github.com/kevin/ascii-adventure/internal/textsafe"
)

const maxReplyRunes = 400

// output est la réponse analysée du modèle.
type output struct {
	Reply  string
	Intro  *lore.Intro // nil si le joueur ne s'est pas présenté (ou champs inexploitables)
	Memory string
	Quest  *quest.Proposal // proposition brute, à valider (quest.Validate) ; nil si aucune
	// Structured est faux quand le modèle n'a pas respecté le format JSON: on
	// garde alors son texte comme réplique, mais on n'en tire aucune mémoire.
	Structured bool
}

// flexString accepte une chaîne, un nombre, un booléen ou null.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		*f = flexString(x)
	case float64, bool:
		*f = flexString(strings.TrimSpace(string(b)))
	default: // null, objet, tableau
		*f = ""
	}
	return nil
}

type rawOutput struct {
	Reply  flexString      `json:"reply"`
	Intro  json.RawMessage `json:"intro"`
	Memory flexString      `json:"memory"`
	Quest  json.RawMessage `json:"quest"`
}

// flexInt accepte un nombre, une chaîne numérique ou null (0). Les valeurs hors bornes sont ramenées
// dans [-1e9, 1e9] : la validation de la quête fait le reste.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			n = parsed
		}
	}
	*f = flexInt(max(-1e9, min(1e9, n)))
	return nil
}

type rawQuest struct {
	Title     flexString `json:"title"`
	Story     flexString `json:"story"`
	Objective struct {
		Type    flexString `json:"type"`
		Species flexString `json:"species"`
		Place   flexString `json:"place"`
		Count   flexInt    `json:"count"`
	} `json:"objective"`
	Reward struct {
		Gold    flexInt    `json:"gold"`
		XP      flexInt    `json:"xp"`
		Potions flexInt    `json:"potions"`
		Note    flexString `json:"note"`
	} `json:"reward"`
}

func parseQuest(raw json.RawMessage) *quest.Proposal {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var rq rawQuest
	if err := json.Unmarshal(raw, &rq); err != nil {
		return nil
	}
	o, r := rq.Objective, rq.Reward
	return &quest.Proposal{
		Title: string(rq.Title), Story: string(rq.Story),
		Type: string(o.Type), Species: string(o.Species), Place: string(o.Place), Count: int(o.Count),
		Gold: int(r.Gold), XP: int(r.XP), Potions: int(r.Potions), Note: string(r.Note),
	}
}

type rawIntro struct {
	Alias  flexString `json:"alias"`
	Role   flexString `json:"role"`
	Origin flexString `json:"origin"`
	Goal   flexString `json:"goal"`
	Trait  flexString `json:"trait"`
}

// parseOutput analyse la réponse du modèle de façon tolérante: blocs de code
// markdown, texte autour de l'objet JSON, champs de mauvais type. Le texte est
// nettoyé pour l'affichage dans un terminal. ok est faux si aucune réplique
// exploitable n'a pu être extraite.
func parseOutput(content string) (out output, ok bool) {
	body := extractJSON(content)
	if body != "" {
		var raw rawOutput
		if err := json.Unmarshal([]byte(body), &raw); err == nil {
			out.Reply = textsafe.Clean(string(raw.Reply), maxReplyRunes)
			if out.Reply != "" {
				out.Structured = true
				out.Memory = string(raw.Memory)
				out.Intro = parseIntro(raw.Intro)
				out.Quest = parseQuest(raw.Quest)
				return out, true
			}
		}
	}
	// Pas de JSON exploitable: le texte brut devient la réplique, sans mémoire.
	text := textsafe.Clean(stripFences(content), maxReplyRunes)
	if text == "" || strings.HasPrefix(text, "{") { // un JSON cassé n'est pas une réplique
		return output{}, false
	}
	return output{Reply: text}, true
}

func parseIntro(raw json.RawMessage) *lore.Intro {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || raw[0] != '{' {
		return nil // null, false, chaîne…: pas de présentation
	}
	var ri rawIntro
	if err := json.Unmarshal(raw, &ri); err != nil {
		return nil
	}
	in := lore.Intro{Alias: string(ri.Alias), Role: string(ri.Role), Origin: string(ri.Origin), Goal: string(ri.Goal), Trait: string(ri.Trait)}
	if len(in.Fields()) == 0 {
		return nil
	}
	return &in
}

// stripFences retire les clôtures ```json … ``` autour d'une réponse.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.IndexByte(s, '\n'); i >= 0 && !strings.ContainsAny(s[:i], "{}") {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

// extractJSON renvoie le premier objet JSON équilibré de s, ou "".
func extractJSON(s string) string {
	s = stripFences(s)
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

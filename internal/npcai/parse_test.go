package npcai

import (
	"strings"
	"testing"
)

func TestParseStructuredReply(t *testing.T) {
	out, ok := parseOutput(`{"reply": "Bonjour, voyageur.", "intro": null, "memory": "Il est poli."}`)
	if !ok || !out.Structured || out.Reply != "Bonjour, voyageur." || out.Intro != nil || out.Memory != "Il est poli." {
		t.Fatalf("%+v ok=%v", out, ok)
	}
}

func TestParseIntro(t *testing.T) {
	out, ok := parseOutput(`{"reply": "Enchantée.", "intro": {"alias": "Brenna", "role": "archère", "origin": "", "goal": "venger son père", "trait": ""}, "memory": "m"}`)
	if !ok || out.Intro == nil {
		t.Fatalf("%+v", out)
	}
	if out.Intro.Alias != "Brenna" || out.Intro.Role != "archère" || out.Intro.Goal != "venger son père" || out.Intro.Origin != "" {
		t.Fatalf("présentation: %+v", *out.Intro)
	}
}

func TestParseIntroVariantsThatMeanNone(t *testing.T) {
	for _, intro := range []string{`null`, `{}`, `false`, `"rien"`, `[]`, `{"alias":"","role":""}`, `{"alias":null}`, `0`} {
		out, ok := parseOutput(`{"reply":"ok","intro":` + intro + `}`)
		if !ok || out.Intro != nil {
			t.Errorf("intro=%s: %+v", intro, out.Intro)
		}
	}
	// intro absente
	if out, ok := parseOutput(`{"reply":"ok"}`); !ok || out.Intro != nil {
		t.Errorf("intro absente: %+v", out)
	}
}

func TestParseToleratesWrongTypes(t *testing.T) {
	out, ok := parseOutput(`{"reply": "ok", "memory": 42, "intro": {"alias": 7, "role": true, "origin": null, "goal": ["x"], "trait": "t"}}`)
	if !ok || out.Intro == nil {
		t.Fatalf("%+v", out)
	}
	if out.Memory != "42" || out.Intro.Alias != "7" || out.Intro.Role != "true" || out.Intro.Origin != "" || out.Intro.Goal != "" || out.Intro.Trait != "t" {
		t.Fatalf("%+v / %+v", out, *out.Intro)
	}
	// reply numérique: converti en texte, pas d'erreur
	if out, ok := parseOutput(`{"reply": 12}`); !ok || out.Reply != "12" {
		t.Fatalf("%+v", out)
	}
}

func TestParseFencesAndSurroundingText(t *testing.T) {
	for name, in := range map[string]string{
		"clôture json": "```json\n{\"reply\": \"Salut\", \"intro\": null}\n```",
		"clôture nue":  "```\n{\"reply\": \"Salut\"}\n```",
		"texte avant":  "Voici ma réponse : {\"reply\": \"Salut\"}",
		"texte après":  "{\"reply\": \"Salut\"} J'espère que cela vous convient.",
		"espaces":      "\n\n  {\"reply\": \"Salut\"}  \n",
	} {
		out, ok := parseOutput(in)
		if !ok || !out.Structured || out.Reply != "Salut" {
			t.Errorf("%s: %+v ok=%v", name, out, ok)
		}
	}
}

func TestParseFirstObjectWinsAndBracesInStrings(t *testing.T) {
	out, ok := parseOutput(`{"reply": "Il a dit } puis { et \"{\" "} {"reply": "second"}`)
	if !ok || out.Reply != `Il a dit } puis { et "{"` {
		t.Fatalf("%+v ok=%v", out, ok)
	}
}

func TestParseFallsBackToPlainText(t *testing.T) {
	out, ok := parseOutput("Bonjour voyageur, que puis-je pour vous ?")
	if !ok || out.Structured || out.Reply != "Bonjour voyageur, que puis-je pour vous ?" || out.Intro != nil || out.Memory != "" {
		t.Fatalf("%+v ok=%v (texte brut: réplique sans mémoire)", out, ok)
	}
}

func TestParseRejectsUnusableOutput(t *testing.T) {
	for _, in := range []string{
		"", "   \n ", `{"reply": ""}`, `{"reply": "  \n "}`, `{"reply": null}`, `{"pas_de_reply": "x"}`,
		`{"reply": "coupé`, `{"reply": "x", `, "{", "\x1b[31m\x1b[0m",
	} {
		if out, ok := parseOutput(in); ok {
			t.Errorf("%q accepté: %+v", in, out)
		}
	}
}

func TestParseSanitizesTerminalDangers(t *testing.T) {
	out, ok := parseOutput(`{"reply": "Salut\u001b[2J\u001b]0;PIRATÉ\u0007 ‮à l'envers​", "memory": "ok\u001b[31m"}`)
	if !ok {
		t.Fatal("rejeté")
	}
	if strings.ContainsAny(out.Reply, "\x1b\x07") || strings.ContainsRune(out.Reply, '‮') || strings.ContainsRune(out.Reply, '​') {
		t.Fatalf("réplique non nettoyée: %q", out.Reply)
	}
	if out.Reply != "Salut à l'envers" {
		t.Fatalf("réplique = %q", out.Reply)
	}
	// Le texte brut aussi.
	out, ok = parseOutput("Brut\x1b[2J et ‮dangereux")
	if !ok || strings.ContainsAny(out.Reply, "\x1b") || strings.ContainsRune(out.Reply, '‮') {
		t.Fatalf("texte brut non nettoyé: %q", out.Reply)
	}
}

func TestParseTruncatesLongReplies(t *testing.T) {
	out, ok := parseOutput(`{"reply": "` + strings.Repeat("a", 5000) + `"}`)
	if !ok || len([]rune(out.Reply)) != maxReplyRunes {
		t.Fatalf("réplique de %d caractères, plafond %d", len([]rune(out.Reply)), maxReplyRunes)
	}
}

func TestExtractJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`:                 `{"a":1}`,
		`x {"a":{"b":2}} y`:       `{"a":{"b":2}}`,
		`{"a":"}"}`:               `{"a":"}"}`,
		`{"a":"\\"}`:              `{"a":"\\"}`,
		`pas d'objet`:             ``,
		`{non fermé`:              ``,
		"```json\n{\"a\":1}\n```": `{"a":1}`,
	} {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, attendu %q", in, got, want)
		}
	}
}

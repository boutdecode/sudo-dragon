package client

import (
	"context"
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store/storetest"
)

// inEnglish passe un modèle de test (français par défaut) en anglais.
func inEnglish(m Model) Model {
	m.lang = i18n.EN
	return m
}

// frenchWords sont des mots de l'interface qui ne doivent jamais apparaître dans la
// version anglaise.
var frenchWords = []string{"Connexion", "Pseudo", "Mot de passe", "Joueurs", "Discussion", "Attaquer", "Fuir", "Jour ", "en ligne", "Sac à dos", "quitter", "marcher", "historique"}

func assertNoFrench(t *testing.T, name, view string) {
	t.Helper()
	for _, w := range frenchWords {
		if strings.Contains(view, w) {
			t.Errorf("%s: %q dans l'interface anglaise:\n%s", name, w, view)
		}
	}
}

func TestEnglishLoginScreen(t *testing.T) {
	m := NewLang("ws://x/ws", "", i18n.EN)
	view := plainView(m)
	for _, want := range []string{"Log in", "Sign up", "Server", "Username", "Password", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("écran de connexion sans %q:\n%s", want, view)
		}
	}
	assertNoFrench(t, "login", view)
	if m.chatInput.Placeholder != "Your message…" {
		t.Errorf("placeholder: %q", m.chatInput.Placeholder)
	}
}

func TestEnglishPlayingScreen(t *testing.T) {
	m := inEnglish(playingModel(t, 120, 40))
	view := plainView(m)
	for _, want := range []string{"Players", "NEARBY PLAYERS", "Chat", "walk", "history", "Day "} {
		if !strings.Contains(view, want) {
			t.Errorf("écran de jeu sans %q:\n%s", want, view)
		}
	}
	assertNoFrench(t, "jeu", view)

	small := inEnglish(playingModel(t, 40, 10))
	if !strings.Contains(small.View().Content, "too small") {
		t.Error("message anglais de terminal trop petit attendu")
	}
}

func TestEnglishCombatScreen(t *testing.T) {
	m := inEnglish(startCombat(t, playingModel(t, 120, 40), "You attack the Wolf!"))
	view := plainView(m)
	for _, want := range []string{"FIGHT · Wolf", "[A] Strike", "[D] Defend", "[I] Potion (2)", "[F] Flee", "Actions", "Log"} {
		if !strings.Contains(view, want) {
			t.Errorf("combat sans %q:\n%s", want, view)
		}
	}
	assertNoFrench(t, "combat", view)
}

func TestEnglishNPCKeysFollowTheEnglishLabels(t *testing.T) {
	m, out := withConn(inEnglish(playingModel(t, 110, 34)))
	m = openNPC(t, m, merchantDialog())
	view := plainView(m)
	if !strings.Contains(view, "[B]") || !strings.Contains(view, "[S]") || !strings.Contains(view, "b s h t") {
		t.Fatalf("lettres anglaises attendues (B = buy, S = sell):\n%s", view)
	}
	for key, want := range map[string]string{"b": protocol.NPCBuy, "s": protocol.NPCSell} {
		m = press(m, key)
		if got := sentNPCActions(out); len(got) != 1 || got[0] != want {
			t.Errorf("touche %q: actions %v, attendu [%s]", key, got, want)
		}
	}
	// Les lettres françaises ne font plus rien, et le marchand ne soigne pas (H).
	for _, k := range []string{"a", "v", "h"} {
		m = press(m, k)
		if got := sentNPCActions(out); len(got) != 0 {
			t.Errorf("touche %q: action inattendue %v", k, got)
		}
	}
}

func TestEnglishConsentKeys(t *testing.T) {
	m, out := withConn(inEnglish(playingModel(t, 110, 34)))
	m = openNPC(t, m, noticeDialog())
	m = press(m, "o") // « oui » en français: sans effet en anglais
	if got := sentNPCActions(out); len(got) != 0 {
		t.Fatalf("o ne doit pas accepter en anglais: %v", got)
	}
	press(m, "y")
	if got := sentNPCActions(out); len(got) != 1 || got[0] != protocol.NPCConsent {
		t.Fatalf("y doit accepter en anglais: %v", got)
	}
}

func TestDialSendsTheLanguageAndTranslatesErrors(t *testing.T) {
	url := startServer(t)
	_, _, err := DialLang(context.Background(), url, "x", storetest.Password, true, i18n.EN)
	if err == nil || !strings.Contains(err.Error(), "invalid username") {
		t.Fatalf("erreur anglaise attendue: %v", err)
	}
	_, _, err = DialLang(context.Background(), "ws://127.0.0.1:1/ws", "alice", storetest.Password, true, i18n.EN)
	if err == nil || !strings.Contains(err.Error(), "cannot connect") {
		t.Fatalf("erreur de connexion anglaise attendue: %v", err)
	}
	_, _, err = DialLang(context.Background(), "ws://127.0.0.1:1/ws", "alice", storetest.Password, true, i18n.FR)
	if err == nil || !strings.Contains(err.Error(), "connexion impossible") {
		t.Fatalf("erreur de connexion française attendue: %v", err)
	}
}

func TestEnglishConnectionLostMessage(t *testing.T) {
	m := inEnglish(playingModel(t, 120, 40))
	tm, _ := m.Update(eventMsg{Event{Err: errClosed}})
	if got := tm.(Model).errMsg; got != "connection lost: connection closed" {
		t.Fatalf("message: %q", got)
	}
}

package game

import (
	"strings"
	"testing"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Le jeu s'adresse à chaque joueur dans sa langue: ici l'anglais, le français
// restant la langue par défaut (tous les autres tests).

func TestEnglishPlayerGetsEnglishFightLog(t *testing.T) {
	r := newRig(t, weak(testRat))
	r.p.lang = i18n.EN
	r.engage()
	if got := r.last().Log[0]; got != "You attack the Rat!" {
		t.Fatalf("intro: %q", got)
	}
	r.g.act(r.p, combat.Attack, 0)
	log := strings.Join(r.last().Log, " | ")
	for _, want := range []string{"defeated", "XP"} {
		if !strings.Contains(log, want) {
			t.Errorf("journal sans %q: %s", want, log)
		}
	}
	if strings.ContainsAny(log, "éèàçÀ") || strings.Contains(log, "vaincu") {
		t.Errorf("du français dans le journal anglais: %s", log)
	}
}

func TestEnglishPlayerGetsEnglishMerchant(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.lang = i18n.EN
	r.talk()
	d := r.last()
	if !strings.Contains(d.Log[0], "Welcome") {
		t.Fatalf("salutation: %q", d.Log[0])
	}
	if !strings.HasPrefix(d.Name, "Master ") && !strings.HasPrefix(d.Name, "Dame ") {
		t.Fatalf("nom du marchand: %q", d.Name)
	}
	if o, ok := r.offer(protocol.NPCBuy); !ok || o.Label != "Buy a potion" {
		t.Fatalf("offre: %+v", o)
	}
	r.p.stats.Gold = 0
	r.act(protocol.NPCBuy)
	if got := r.last().Log[0]; !strings.Contains(got, "short by") {
		t.Fatalf("manque d'or: %q", got)
	}
}

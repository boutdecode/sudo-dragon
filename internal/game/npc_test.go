package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// npcRig prépare un joueur à côté d'un PNJ du type voulu et capture les dialogues.
type npcRig struct {
	g      *Game
	p      *player
	npc    world.NPC
	dx, dy int // direction du joueur vers le PNJ
	msgs   []protocol.NPCDialog
	rec    *recorder
}

func (r *npcRig) last() protocol.NPCDialog { return r.msgs[len(r.msgs)-1] }

// findNPC cherche un PNJ du type donné près du départ.
func findNPC(t *testing.T, g *Game, kind world.NPCKind) world.NPC {
	t.Helper()
	for _, s := range g.World.StructuresNear(g.World.FindSpawn(), 700) {
		for _, n := range s.NPCs {
			if n.Kind == kind {
				return n
			}
		}
	}
	t.Skipf("pas de PNJ %s près du départ", kind)
	return world.NPC{}
}

func newNPCRig(t *testing.T, kind world.NPCKind) *npcRig {
	t.Helper()
	g := newGame()
	npc := findNPC(t, g, kind)
	p, rec := joinRec(g, "hero")
	r := &npcRig{g: g, p: p, npc: npc, rec: rec}
	p.sendNPC = func(d protocol.NPCDialog) { r.msgs = append(r.msgs, d) }

	// Une case voisine libre du PNJ, avec la direction qui y mène.
	for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
		pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
		if !g.blocked(pos) {
			place(g, p, pos)
			r.dx, r.dy = d[0], d[1]
			return r
		}
	}
	t.Fatalf("aucune case libre à côté du PNJ en %v", npc.Pos)
	return nil
}

// talk marche vers le PNJ pour lui parler.
func (r *npcRig) talk() {
	r.g.move(r.p.id, r.dx, r.dy)
	r.g.step()
}

func (r *npcRig) act(action string) { r.g.talkAct(r.p, action, "") }

func (r *npcRig) offer(action string) (protocol.NPCOffer, bool) {
	for _, o := range r.last().Offers {
		if o.Action == action {
			return o, true
		}
	}
	return protocol.NPCOffer{}, false
}

func TestNPCOccupiesItsTile(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	if !g.blocked(npc.Pos) {
		t.Fatal("la case d'un PNJ doit être bloquée")
	}
	// Ni un joueur restauré là, ni un monstre, ne peuvent s'y poser.
	saved := &Saved{Stats: combat.NewStats(), HP: 10, Pos: npc.Pos}
	p := g.joinWith("x", func(protocol.Snapshot) {}, saved)
	if p.pos == npc.Pos {
		t.Fatal("un joueur a été placé sur un PNJ")
	}
}

func TestWalkingIntoNPCOpensDialog(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	start := r.p.pos
	r.talk()

	if r.p.talk == nil || r.p.talk.npc != r.npc {
		t.Fatal("la conversation ne s'est pas ouverte")
	}
	if r.p.pos != start {
		t.Fatalf("le joueur a bougé: %v", r.p.pos)
	}
	if len(r.msgs) != 1 {
		t.Fatalf("%d messages, attendu 1", len(r.msgs))
	}
	d := r.last()
	if d.Kind != "healer" || d.Over || len(d.Log) != 1 {
		t.Fatalf("ouverture: %+v", d)
	}
	if _, ok := r.offer(protocol.NPCHeal); !ok {
		t.Fatalf("le soigneur ne propose pas de soins: %+v", d.Offers)
	}
}

func TestPlayerIsFrozenWhileTalking(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	start := r.p.pos
	for i := 0; i < 30; i++ {
		r.g.move(r.p.id, -r.dx, -r.dy)
		r.g.step()
	}
	if r.p.pos != start || r.p.hasPending {
		t.Fatalf("le joueur s'est déplacé pendant la conversation: %v", r.p.pos)
	}
}

func TestLeavingEndsTheDialogAndFreesThePlayer(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.act(protocol.NPCLeave)

	if r.p.talk != nil {
		t.Fatal("la conversation est encore ouverte")
	}
	d := r.last()
	if !d.Over || d.Kind != "healer" || len(d.Offers) != 0 {
		t.Fatalf("message de fin: %+v", d)
	}
	// Libre de partir: un pas s'exécute.
	before := r.p.pos
	r.g.move(r.p.id, -r.dx, -r.dy)
	r.g.step()
	if r.p.pos == before && !r.g.blocked(world.Pos{X: before.X - r.dx, Y: before.Y - r.dy}) {
		t.Fatal("le joueur reste figé après la conversation")
	}
}

func TestActionsWithoutDialogAreIgnored(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.hp = 1
	r.p.stats.Gold = 500
	r.act(protocol.NPCHeal)
	if r.p.hp != 1 || r.p.stats.Gold != 500 || len(r.msgs) != 0 {
		t.Fatal("une action hors dialogue a eu un effet")
	}
}

func TestCannotTalkWhileFighting(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	dx, dy := freeDir(t, r.g, r.p)
	m := r.g.addMonster(testRat, world.Pos{X: r.p.pos.X + dx, Y: r.p.pos.Y + dy})
	r.g.startFight(r.p, m, true)
	r.g.startTalk(r.p, r.npc)
	if r.p.talk != nil {
		t.Fatal("une conversation s'est ouverte pendant un combat")
	}
}

// --- Soigneur ---

func TestHealerIsFreeAtLevelOne(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.hp = 3
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); !o.Available || o.Price != 0 {
		t.Fatalf("offre: %+v, attendue gratuite et disponible", o)
	}
	r.act(protocol.NPCHeal)
	if r.p.hp != r.p.stats.MaxHP || r.p.stats.Gold != 0 {
		t.Fatalf("PV %d/%d, or %d", r.p.hp, r.p.stats.MaxHP, r.p.stats.Gold)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "offert") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

func TestHealerChargesPerLevel(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(5, 0, 2)
	r.p.hp = 10
	price := healPrice(5)
	if price != 5*healPricePerLevel {
		t.Fatalf("tarif = %d", price)
	}
	r.p.stats.Gold = price + 7
	r.talk()
	r.act(protocol.NPCHeal)

	if r.p.hp != r.p.stats.MaxHP || r.p.stats.Gold != 7 {
		t.Fatalf("PV %d/%d, or %d (attendu 7)", r.p.hp, r.p.stats.MaxHP, r.p.stats.Gold)
	}
}

func TestHealerRefusesWhenPoor(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(4, 0, 2)
	r.p.hp = 5
	r.p.stats.Gold = healPrice(4) - 3
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); o.Available {
		t.Fatal("l'offre devrait être marquée indisponible")
	}
	r.act(protocol.NPCHeal)
	if r.p.hp != 5 || r.p.stats.Gold != healPrice(4)-3 {
		t.Fatalf("état modifié malgré le refus: PV %d, or %d", r.p.hp, r.p.stats.Gold)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Il vous manque 3 or") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

func TestHealerDoesNotChargeWhenHealthy(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats = combat.StatsAt(6, 0, 2)
	r.p.hp = r.p.stats.MaxHP
	r.p.stats.Gold = 100
	r.talk()
	if o, _ := r.offer(protocol.NPCHeal); o.Available {
		t.Fatal("soigner un joueur en pleine forme ne devrait pas être proposé")
	}
	r.act(protocol.NPCHeal)
	if r.p.stats.Gold != 100 {
		t.Fatalf("or débité (%d) pour un joueur en pleine forme", r.p.stats.Gold)
	}
}

func TestHealerDoesNotSellPotions(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.p.stats.Gold = 200
	before := r.p.stats.Potions
	r.talk()
	r.act(protocol.NPCBuy)
	r.act(protocol.NPCSell)
	if r.p.stats.Gold != 200 || r.p.stats.Potions != before {
		t.Fatalf("le soigneur a commercé: or %d, potions %d", r.p.stats.Gold, r.p.stats.Potions)
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "pas ce que je propose") {
		t.Fatalf("journal: %v", r.last().Log)
	}
}

// --- Marchand ---

func TestMerchantBuy(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Gold = potionBuyPrice + 5
	before := r.p.stats.Potions
	r.talk()
	r.act(protocol.NPCBuy)

	if r.p.stats.Potions != before+1 || r.p.stats.Gold != 5 {
		t.Fatalf("potions %d (attendu %d), or %d (attendu 5)", r.p.stats.Potions, before+1, r.p.stats.Gold)
	}
	if d := r.last(); d.Gold != 5 || d.Potions != before+1 {
		t.Fatalf("le dialogue n'est pas à jour: %+v", d)
	}
}

func TestMerchantBuyRefusals(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()

	r.p.stats.Gold = potionBuyPrice - 1 // trop pauvre
	before := r.p.stats.Potions
	r.act(protocol.NPCBuy)
	if r.p.stats.Potions != before || r.p.stats.Gold != potionBuyPrice-1 {
		t.Fatal("achat accepté sans l'or nécessaire")
	}
	if !strings.Contains(strings.Join(r.last().Log, "|"), "Il vous manque 1 or") {
		t.Fatalf("journal: %v", r.last().Log)
	}

	r.p.stats.Gold = 500 // sac plein: l'or n'est PAS débité
	r.p.stats.Potions = combat.MaxPotions
	r.act(protocol.NPCBuy)
	if r.p.stats.Gold != 500 || r.p.stats.Potions != combat.MaxPotions {
		t.Fatalf("achat avec un sac plein: or %d, potions %d", r.p.stats.Gold, r.p.stats.Potions)
	}
	if o, _ := r.offer(protocol.NPCBuy); o.Available {
		t.Fatal("l'achat ne devrait pas être proposé avec un sac plein")
	}
}

func TestMerchantSell(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Potions = 3
	r.p.stats.Gold = 10
	r.talk()
	r.act(protocol.NPCSell)
	if r.p.stats.Potions != 2 || r.p.stats.Gold != 10+potionSellPrice {
		t.Fatalf("potions %d, or %d", r.p.stats.Potions, r.p.stats.Gold)
	}
	if o, ok := r.offer(protocol.NPCSell); !ok || !o.Gain || o.Price != potionSellPrice {
		t.Fatalf("offre de vente: %+v", o)
	}
}

func TestMerchantSellRefusals(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()

	r.p.stats.Potions = 0
	r.p.stats.Gold = 40
	r.act(protocol.NPCSell)
	if r.p.stats.Gold != 40 || r.p.stats.Potions != 0 {
		t.Fatal("vente sans potion acceptée")
	}

	// Bourse pleine: on ne perd pas une potion contre rien.
	r.p.stats.Potions = 4
	r.p.stats.Gold = combat.MaxGold - 2
	r.act(protocol.NPCSell)
	if r.p.stats.Potions != 4 || r.p.stats.Gold != combat.MaxGold-2 {
		t.Fatalf("potion vendue à bourse pleine: potions %d, or %d", r.p.stats.Potions, r.p.stats.Gold)
	}
}

func TestMerchantBuyThenSellNeverCreatesGold(t *testing.T) {
	if potionSellPrice >= potionBuyPrice {
		t.Fatalf("revendre (%d) doit rapporter moins qu'acheter (%d), sinon on fabrique de l'or", potionSellPrice, potionBuyPrice)
	}
	r := newNPCRig(t, world.NPCMerchant)
	r.p.stats.Gold = 1000
	r.p.stats.Potions = 0
	r.talk()
	start := r.p.stats.Gold
	for i := 0; i < 20; i++ {
		r.act(protocol.NPCBuy)
		r.act(protocol.NPCSell)
	}
	if r.p.stats.Gold > start {
		t.Fatalf("or créé par des allers-retours: %d -> %d", start, r.p.stats.Gold)
	}
}

func TestMerchantDoesNotHeal(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.p.hp = 2
	r.p.stats.Gold = 500
	r.talk()
	r.act(protocol.NPCHeal)
	if r.p.hp != 2 || r.p.stats.Gold != 500 {
		t.Fatalf("le marchand a soigné: PV %d, or %d", r.p.hp, r.p.stats.Gold)
	}
}

// --- Persistance, expiration, boucle réelle ---

func TestTransactionIsSavedImmediately(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.p.stats.Gold = 100
	r.talk()
	if len(sv.calls) != 0 {
		t.Fatal("sauvegarde à l'ouverture d'une conversation")
	}
	r.act(protocol.NPCBuy)
	got, ok := sv.last("hero")
	if !ok || got.Stats.Gold != 100-potionBuyPrice {
		t.Fatalf("sauvegarde après achat: %+v (ok=%v)", got.Stats, ok)
	}
	n := len(sv.calls)
	r.p.stats.Gold = 0
	r.act(protocol.NPCBuy) // refusé
	if len(sv.calls) != n {
		t.Fatal("une transaction refusée a déclenché une sauvegarde")
	}
}

func TestIdleDialogExpires(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.g.tick += fightIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("expirée trop tôt")
	}
	r.g.tick++
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk != nil {
		t.Fatal("la conversation inactive aurait dû expirer")
	}
	if d := r.last(); !d.Over || d.Kind != "healer" {
		t.Fatalf("message d'expiration: %+v", d)
	}
}

func TestActionResetsIdleTimerForDialogs(t *testing.T) {
	r := newNPCRig(t, world.NPCMerchant)
	r.talk()
	r.g.tick += fightIdleTicks
	r.act(protocol.NPCSell) // le joueur agit
	r.g.tick += fightIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("un joueur actif ne doit pas être expulsé")
	}
}

func TestLeavingGameClosesDialogSilently(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	r.g.leave(r.p.id)
	if _, ok := r.g.players[r.p.id]; ok {
		t.Fatal("joueur toujours présent")
	}
}

func TestValidNPCActions(t *testing.T) {
	for _, a := range []string{"buy", "sell", "heal", "leave"} {
		if !ValidNPCAction(a) {
			t.Errorf("%q devrait être valide", a)
		}
	}
	for _, a := range []string{"", "Buy", "steal", "attack", "buy "} {
		if ValidNPCAction(a) {
			t.Errorf("%q ne devrait pas être valide", a)
		}
	}
}

// Parcours complet à travers la vraie boucle (goroutine, file de commandes, callbacks).
func TestDialogThroughRunningGame(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	msgs := make(chan protocol.NPCDialog, 16)
	id, err := g.Join("hero", Handlers{
		Snapshot: func(protocol.Snapshot) {},
		NPC:      func(d protocol.NPCDialog) { msgs <- d },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Place le joueur à côté du PNJ puis lui fait faire un pas vers lui.
	ready := make(chan [2]int, 1)
	g.do(func() {
		p := g.players[id]
		for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
			pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
			if !g.blocked(pos) {
				g.setPos(p, pos)
				p.hp = 1
				ready <- d
				return
			}
		}
		ready <- [2]int{}
	})
	d := <-ready
	if d == ([2]int{}) {
		t.Skip("pas de case libre à côté du PNJ")
	}
	g.Move(id, d[0], d[1])

	recv := func() protocol.NPCDialog {
		select {
		case m := <-msgs:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("aucun message de dialogue")
			return protocol.NPCDialog{}
		}
	}
	if m := recv(); m.Over || m.Kind != "healer" {
		t.Fatalf("ouverture: %+v", m)
	}
	g.TalkAct(id, protocol.NPCHeal, "")
	if m := recv(); m.HP != m.MaxHP {
		t.Fatalf("soin non appliqué: %+v", m)
	}
	g.TalkAct(id, "action-inconnue", "") // ignorée
	g.TalkAct(id, protocol.NPCLeave, "")
	if m := recv(); !m.Over {
		t.Fatalf("fermeture: %+v", m)
	}
}

func TestVillagerOffersOnlyConversation(t *testing.T) {
	r := newNPCRig(t, world.NPCVillager)
	r.g.Talker = newFakeTalker()
	r.talk()
	d := r.last()
	if d.Kind != string(world.NPCVillager) || len(d.Offers) != 1 || d.Offers[0].Action != protocol.NPCTalk {
		t.Fatalf("un villageois ne propose que « Discuter » : %+v", d)
	}
	// Ni commerce ni soin chez lui, et rien n'est débité.
	r.p.stats.Gold = 100
	r.act(protocol.NPCBuy)
	r.act(protocol.NPCHeal)
	if r.p.stats.Gold != 100 || !strings.Contains(strings.Join(r.last().Log, " "), "pas ce que je propose") {
		t.Fatalf("gold=%d dernier=%+v", r.p.stats.Gold, r.last())
	}
}

func TestVillagerWithoutAIGivesAStableAmbientLine(t *testing.T) {
	r := newNPCRig(t, world.NPCVillager) // pas de Talker
	r.talk()
	d := r.last()
	if len(d.Offers) != 0 || len(d.Log) != 1 || d.Log[0] != ambientLine(r.npc) || d.Log[0] == "" {
		t.Fatalf("phrase d'ambiance attendue sans menu vide ni IA : %+v", d)
	}
	if ambientLine(r.npc) != ambientLine(r.npc) {
		t.Fatal("la phrase doit être stable pour un même PNJ")
	}
}

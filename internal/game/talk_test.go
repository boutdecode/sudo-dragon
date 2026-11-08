package game

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevin/ascii-adventure/internal/npcai"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/world"
)

// fakeTalker est un Talker dont le test décide du moment de chaque réponse.
type fakeTalker struct {
	mu       sync.Mutex
	avail    bool
	provider string
	reqs     []npcai.Request
	dones    []func(npcai.Reply)
}

func newFakeTalker() *fakeTalker { return &fakeTalker{avail: true, provider: "Mistral AI"} }

func (f *fakeTalker) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.avail
}

func (f *fakeTalker) Provider() string { return f.provider }

func (f *fakeTalker) setAvailable(v bool) { f.mu.Lock(); f.avail = v; f.mu.Unlock() }

func (f *fakeTalker) Ask(req npcai.Request, done func(npcai.Reply)) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.dones = append(f.dones, done)
	f.mu.Unlock()
}

func (f *fakeTalker) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.reqs) }

func (f *fakeTalker) req(i int) npcai.Request { f.mu.Lock(); defer f.mu.Unlock(); return f.reqs[i] }

// answer livre la réponse de la demande i (comme le ferait le service, depuis une autre goroutine).
func (f *fakeTalker) answer(i int, rep npcai.Reply) {
	f.mu.Lock()
	done := f.dones[i]
	f.mu.Unlock()
	done(rep)
}

// pump exécute les commandes que les réponses ont mises dans la file du jeu.
func (r *npcRig) pump() {
	for {
		select {
		case fn := <-r.g.inbox:
			fn()
		default:
			return
		}
	}
}

func (r *npcRig) say(text string) { r.g.talkAct(r.p, protocol.NPCSay, text) }

func (r *npcRig) lines() []protocol.NPCLine { return r.last().Lines }

// chatRig prépare un joueur en discussion avec un soigneur, prêt à parler.
func chatRig(t *testing.T, consented bool) (*npcRig, *fakeTalker) {
	t.Helper()
	r := newNPCRig(t, world.NPCHealer)
	ft := newFakeTalker()
	r.g.Talker = ft
	r.p.consent = consented
	r.talk()
	return r, ft
}

func ok(text string) npcai.Reply { return npcai.Reply{Text: text} }

func TestTalkOfferAppearsOnlyWhenAModelIsAvailable(t *testing.T) {
	r := newNPCRig(t, world.NPCHealer)
	r.talk()
	if _, has := r.offer(protocol.NPCTalk); has {
		t.Fatal("« Discuter » proposé sans modèle de langage")
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("le commerce du soigneur a disparu")
	}

	ft := newFakeTalker()
	ft.setAvailable(false)
	r2 := newNPCRig(t, world.NPCHealer)
	r2.g.Talker = ft
	r2.talk()
	if _, has := r2.offer(protocol.NPCTalk); has {
		t.Fatal("« Discuter » proposé alors que le modèle est indisponible")
	}

	r3, _ := chatRig(t, false)
	if o, has := r3.offer(protocol.NPCTalk); !has || !o.Available || o.Label != "Discuter" {
		t.Fatalf("offre: %+v", o)
	}
	if _, has := r3.offer(protocol.NPCHeal); !has {
		t.Fatal("le soin n'est plus proposé à côté de la discussion")
	}
}

func TestTalkingRequiresConsentFirst(t *testing.T) {
	r, ft := chatRig(t, false)
	r.act(protocol.NPCTalk)

	d := r.last()
	if d.Mode != protocol.NPCModeNotice || d.Over {
		t.Fatalf("mode = %q, attendu l'avis de confidentialité", d.Mode)
	}
	if !strings.Contains(d.Notice, "Mistral AI") || !strings.Contains(d.Notice, "rumeurs") || !strings.Contains(d.Notice, "n'a d'effet sur le jeu") {
		t.Fatalf("avis incomplet: %q", d.Notice)
	}
	if len(d.Offers) != 2 || d.Offers[0].Action != protocol.NPCConsent || d.Offers[1].Action != protocol.NPCDecline {
		t.Fatalf("offres de l'avis: %+v", d.Offers)
	}
	if ft.count() != 0 {
		t.Fatal("une demande est partie vers le modèle avant l'accord du joueur")
	}
	if r.p.consent {
		t.Fatal("accord enregistré sans que le joueur l'ait donné")
	}
}

func TestDecliningKeepsTheMenuAndConsentOff(t *testing.T) {
	r, ft := chatRig(t, false)
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCDecline)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || r.p.consent || ft.count() != 0 {
		t.Fatalf("mode %q, accord %v, %d demandes", d.Mode, r.p.consent, ft.count())
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("le menu du soigneur doit rester utilisable après un refus")
	}
	// Reproposer l'avis la fois suivante.
	r.act(protocol.NPCTalk)
	if r.last().Mode != protocol.NPCModeNotice {
		t.Fatal("l'avis doit être reproposé après un refus")
	}
}

func TestConsentIsSavedImmediatelyAndOpensTheConversation(t *testing.T) {
	r, ft := chatRig(t, false)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCConsent)

	if !r.p.consent {
		t.Fatal("accord non pris en compte")
	}
	saved, found := sv.last("hero")
	if !found || !saved.LLMConsent {
		t.Fatalf("l'accord n'a pas été sauvegardé tout de suite: %+v (trouvé=%v)", saved, found)
	}
	d := r.last()
	if d.Mode != protocol.NPCModeChat || !d.Thinking || len(d.Lines) != 0 {
		t.Fatalf("après l'accord: mode %q, réflexion %v, lignes %v", d.Mode, d.Thinking, d.Lines)
	}
	if ft.count() != 1 || !ft.req(0).Opening || ft.req(0).Message != "" {
		t.Fatalf("la première demande doit être l'ouverture par le PNJ: %+v", ft.reqs)
	}

	ft.answer(0, ok("Bienvenue, voyageur. Qui êtes-vous ?"))
	r.pump()
	d = r.last()
	if d.Thinking || len(d.Lines) != 1 || d.Lines[0] != (protocol.NPCLine{Who: "npc", Text: "Bienvenue, voyageur. Qui êtes-vous ?"}) {
		t.Fatalf("après la réponse d'ouverture: %+v", d)
	}
}

func TestConsentedPlayerGoesStraightToTheConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	if r.last().Mode != protocol.NPCModeChat || ft.count() != 1 || !ft.req(0).Opening {
		t.Fatalf("mode %q, %d demandes", r.last().Mode, ft.count())
	}
}

func TestConsentOutsideTheNoticeIsIgnored(t *testing.T) {
	r, _ := chatRig(t, false)
	r.act(protocol.NPCConsent) // depuis le menu: sans effet
	if r.p.consent {
		t.Fatal("un accord a pu être donné sans avoir vu l'avis")
	}
	r.act(protocol.NPCTalk)
	r.act(protocol.NPCDecline)
	r.act(protocol.NPCConsent) // l'avis est refermé
	if r.p.consent {
		t.Fatal("un accord a été enregistré après un refus")
	}
}

func TestSayExchangeBuildsTheTranscriptAndHistory(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()

	r.say("Je suis Aldric.")
	d := r.last()
	if !d.Thinking || len(d.Lines) != 2 || d.Lines[1] != (protocol.NPCLine{Who: "you", Text: "Je suis Aldric."}) {
		t.Fatalf("après l'envoi: %+v", d)
	}
	req := ft.req(1)
	if req.Opening || req.Message != "Je suis Aldric." || len(req.History) != 0 {
		t.Fatalf("demande: %+v", req)
	}

	ft.answer(1, ok("Enchanté, Aldric."))
	r.pump()
	d = r.last()
	if d.Thinking || len(d.Lines) != 3 || d.Lines[2].Text != "Enchanté, Aldric." {
		t.Fatalf("après la réponse: %+v", d)
	}

	r.say("Et vous ?")
	if h := ft.req(2).History; len(h) != 1 || h[0].Player != "Je suis Aldric." || h[0].NPC != "Enchanté, Aldric." {
		t.Fatalf("l'échange précédent n'est pas confié au modèle: %+v", h)
	}
}

func TestSpeakerFactsAndIdentityAreSentToTheModel(t *testing.T) {
	r, ft := chatRig(t, true)
	r.p.stats = r.p.stats.WithRecord(31, 2, 5)
	r.p.stats.Level = 9
	r.act(protocol.NPCTalk)

	req := ft.req(0)
	if req.Speaker.Account != "hero" || req.Speaker.Level != 9 || req.Speaker.Kills != 31 || req.Speaker.BossKills != 2 || req.Speaker.Deaths != 5 {
		t.Fatalf("interlocuteur: %+v", req.Speaker)
	}
	if req.NPC != r.npc {
		t.Fatalf("PNJ: %+v, attendu %+v", req.NPC, r.npc)
	}
	st := r.g.World.StructureOfRegion(r.npc.Region)
	if req.Place != st.Name || req.PlaceKind != st.Kind {
		t.Fatalf("lieu: %q (%v), attendu %q (%v)", req.Place, req.PlaceKind, st.Name, st.Kind)
	}
	d := r.last()
	if d.Name != r.npc.Name || d.Place != st.Name {
		t.Fatalf("identité dans le dialogue: %q / %q", d.Name, d.Place)
	}
}

func TestSayIsIgnoredWhenNotAllowed(t *testing.T) {
	r, ft := chatRig(t, true)

	r.say("avant la discussion") // encore en mode menu
	if ft.count() != 0 {
		t.Fatal("une réplique est partie hors discussion")
	}
	r.act(protocol.NPCTalk) // ouverture en cours (pending)
	r.say("pendant l'ouverture")
	if ft.count() != 1 {
		t.Fatalf("%d demandes: on ne parle pas pendant que le PNJ réfléchit", ft.count())
	}
	ft.answer(0, ok("Oui ?"))
	r.pump()

	for _, empty := range []string{"", "   ", "\x1b[0m", "​‮"} {
		r.say(empty)
	}
	if ft.count() != 1 {
		t.Fatalf("%d demandes: un message vide ne doit rien envoyer", ft.count())
	}
	r.say("ok")
	r.say("encore")
	if ft.count() != 2 {
		t.Fatalf("%d demandes: une seule réplique à la fois", ft.count())
	}
}

func TestSayTextIsSanitizedAndBounded(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.say("\x1b[2JBonjour‮ " + strings.Repeat("x", 500))
	got := ft.req(1).Message
	if strings.ContainsAny(got, "\x1b") || strings.ContainsRune(got, '‮') {
		t.Fatalf("caractères dangereux transmis au modèle: %q", got)
	}
	if n := len([]rune(got)); n > protocol.MaxSayLen {
		t.Fatalf("%d caractères transmis, plafond %d", n, protocol.MaxSayLen)
	}
	for _, l := range r.lines() {
		if l.Who == "you" && strings.ContainsAny(l.Text, "\x1b‮") {
			t.Fatalf("transcription non nettoyée: %q", l.Text)
		}
	}
}

func TestFailuresAreShownButNotRememberedInHistory(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.say("Bonjour")
	ft.answer(1, npcai.Reply{Text: "Sœur Katell perd le fil de ses pensées…", Failure: npcai.Unavailable})
	r.pump()
	d := r.last()
	if d.Thinking || d.Lines[len(d.Lines)-1].Text != "Sœur Katell perd le fil de ses pensées…" {
		t.Fatalf("l'échec doit être montré en personnage: %+v", d.Lines)
	}
	r.say("Bonjour ?")
	if h := ft.req(2).History; len(h) != 0 {
		t.Fatalf("un échec est entré dans l'historique du modèle: %+v", h)
	}
}

func TestRememberedAddsAVisibleNotice(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.say("Je suis Aldric.")
	ft.answer(1, npcai.Reply{Text: "Enchanté.", Remembered: true})
	r.pump()

	lines := r.lines()
	last := lines[len(lines)-1]
	if last.Who != "sys" || !strings.Contains(last.Text, r.npc.Name) || !strings.Contains(last.Text, "retient") {
		t.Fatalf("le joueur n'est pas prévenu que le PNJ retient sa présentation: %+v", last)
	}
}

func TestBackKeepsTheTranscriptAndNeverReopens(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.say("Salut")
	ft.answer(1, ok("Bonjour."))
	r.pump()

	r.act(protocol.NPCBack)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || len(d.Lines) != 0 {
		t.Fatalf("retour au menu: mode %q, lignes %v", d.Mode, d.Lines)
	}
	if _, has := r.offer(protocol.NPCHeal); !has {
		t.Fatal("le menu n'est pas revenu")
	}

	r.act(protocol.NPCTalk)
	d = r.last()
	if d.Mode != protocol.NPCModeChat || len(d.Lines) != 3 {
		t.Fatalf("la transcription doit être conservée: mode %q, %d lignes", d.Mode, len(d.Lines))
	}
	if ft.count() != 2 {
		t.Fatalf("%d demandes: le PNJ ne doit pas rouvrir la discussion une seconde fois", ft.count())
	}
}

func TestANewConversationOpensAgain(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Qui êtes-vous ?"))
	r.pump()
	r.act(protocol.NPCLeave)
	if r.p.talk != nil {
		t.Fatal("conversation encore ouverte")
	}
	r.talk() // retour auprès du PNJ: nouvelle conversation
	r.act(protocol.NPCTalk)
	if ft.count() != 2 || !ft.req(1).Opening || len(ft.req(1).History) != 0 {
		t.Fatalf("%d demandes: une nouvelle conversation repart de zéro avec une ouverture", ft.count())
	}
}

func TestMenuActionsAreIgnoredDuringTheConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.p.hp, r.p.stats.Gold = 1, 500
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	for _, a := range []string{protocol.NPCHeal, protocol.NPCBuy, protocol.NPCSell} {
		r.act(a)
	}
	if r.p.hp != 1 || r.p.stats.Gold != 500 {
		t.Fatalf("une action de commerce a eu lieu pendant la discussion: PV %d, or %d", r.p.hp, r.p.stats.Gold)
	}
}

func TestLateReplyAfterLeavingIsDropped(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // ouverture en attente
	r.act(protocol.NPCLeave)
	n := len(r.msgs)

	ft.answer(0, ok("Trop tard."))
	r.pump()
	if len(r.msgs) != n {
		t.Fatalf("une réponse tardive a produit un message: %+v", r.last())
	}
}

func TestLateReplyFromAnOlderConversationNeverReachesTheNewOne(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // conversation 1, ouverture en attente
	oldGen := r.p.talk.gen
	r.act(protocol.NPCLeave)
	r.talk() // conversation 2
	if r.p.talk.gen == oldGen {
		t.Fatal("deux conversations partagent le même numéro: les réponses tardives se mélangeraient")
	}
	r.act(protocol.NPCTalk) // ouverture 2 (demande n°1)
	n := len(r.msgs)

	ft.answer(0, ok("Réponse de la conversation 1.")) // arrive en retard
	r.pump()
	for _, l := range r.p.talk.lines {
		if strings.Contains(l.Text, "conversation 1") {
			t.Fatalf("une réponse périmée est entrée dans la nouvelle conversation: %+v", r.p.talk.lines)
		}
	}
	if len(r.msgs) != n || !r.p.talk.pending {
		t.Fatal("la réponse périmée a modifié l'état de la nouvelle conversation")
	}

	ft.answer(1, ok("Réponse de la conversation 2."))
	r.pump()
	if got := r.lines(); len(got) != 1 || got[0].Text != "Réponse de la conversation 2." {
		t.Fatalf("la bonne réponse n'est pas arrivée: %+v", got)
	}
}

func TestLateReplyAfterThePlayerLeftTheGame(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	r.g.leave(r.p.id)
	ft.answer(0, ok("Personne ne m'écoute."))
	r.pump() // ne doit ni paniquer ni ressusciter le joueur
	if _, present := r.g.players[r.p.id]; present {
		t.Fatal("le joueur est revenu")
	}
}

func TestReplyArrivingWhileInTheMenuIsKeptForLater(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk) // ouverture en attente
	r.act(protocol.NPCBack)
	n := len(r.msgs)

	ft.answer(0, ok("Je vous attendais."))
	r.pump()
	if len(r.msgs) != n {
		t.Fatal("une réponse arrivée pendant que le joueur est au menu a affiché l'écran de discussion")
	}
	r.act(protocol.NPCTalk)
	if got := r.lines(); len(got) != 1 || got[0].Text != "Je vous attendais." {
		t.Fatalf("la réponse n'a pas été conservée: %+v", got)
	}
}

func TestTalkerGoingDownMidConversation(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	ft.setAvailable(false)
	r.say("Vous êtes là ?")
	d := r.last()
	if d.Thinking || ft.count() != 1 {
		t.Fatalf("réflexion %v, %d demandes: on ne doit pas appeler un modèle indisponible", d.Thinking, ft.count())
	}
	if last := d.Lines[len(d.Lines)-1]; last.Who != "npc" || !strings.HasPrefix(last.Text, r.npc.Name) {
		t.Fatalf("réplique de repli attendue en personnage: %+v", last)
	}
	// Et le menu ne propose plus de discuter.
	r.act(protocol.NPCBack)
	if _, has := r.offer(protocol.NPCTalk); has {
		t.Fatal("« Discuter » proposé alors que le modèle est tombé")
	}
}

func TestTalkWhenUnavailableExplainsAndStaysInTheMenu(t *testing.T) {
	r, ft := chatRig(t, true)
	ft.setAvailable(false)
	r.act(protocol.NPCTalk)
	d := r.last()
	if d.Mode != protocol.NPCModeMenu || ft.count() != 0 || len(d.Log) == 0 {
		t.Fatalf("mode %q, %d demandes, journal %v", d.Mode, ft.count(), d.Log)
	}
}

func TestChatIdleLimitIsLongerThanMenuIdleLimit(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()

	r.g.tick += fightIdleTicks + 10 // plus que le délai du menu
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("une discussion libre a été fermée au bout du délai du menu")
	}
	r.g.tick += chatIdleTicks
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk != nil {
		t.Fatal("une discussion abandonnée doit finir par se fermer")
	}

	// Le menu garde son délai court.
	r2 := newNPCRig(t, world.NPCHealer)
	r2.talk()
	r2.g.tick += fightIdleTicks + 1
	r2.g.expireIdleTalks(r2.g.sortedIDs())
	if r2.p.talk != nil {
		t.Fatal("le menu devrait expirer au bout de 60 s")
	}
}

func TestSayingResetsTheIdleTimer(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	r.g.tick += chatIdleTicks - 5
	r.say("Toujours là")
	r.g.tick += chatIdleTicks - 5
	r.g.expireIdleTalks(r.g.sortedIDs())
	if r.p.talk == nil {
		t.Fatal("un joueur actif ne doit pas être expulsé")
	}
}

func TestTranscriptAndHistoryAreBounded(t *testing.T) {
	r, ft := chatRig(t, true)
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	for i := 0; i < 60; i++ {
		r.say("message")
		ft.answer(ft.count()-1, ok("réponse"))
		r.pump()
	}
	if n := len(r.lines()); n > maxTranscript {
		t.Fatalf("transcription de %d lignes, plafond %d", n, maxTranscript)
	}
	if n := len(r.p.talk.history); n != maxHistory {
		t.Fatalf("historique de %d échanges, attendu %d", n, maxHistory)
	}
	if n := len(ft.req(ft.count() - 1).History); n != maxHistory {
		t.Fatalf("%d échanges confiés au modèle", n)
	}
}

func TestOnlyConsentAndTransactionsAreSaved(t *testing.T) {
	r, ft := chatRig(t, true)
	sv := &saves{}
	r.g.Persist = sv.persist
	r.act(protocol.NPCTalk)
	ft.answer(0, ok("Oui ?"))
	r.pump()
	r.say("Bonjour")
	ft.answer(1, ok("Bonjour."))
	r.pump()
	r.act(protocol.NPCBack)
	if len(sv.calls) != 0 {
		t.Fatalf("%d sauvegardes pour une simple discussion", len(sv.calls))
	}
}

// Parcours complet à travers la vraie boucle de jeu (goroutine, file de commandes).
func TestConversationThroughRunningGame(t *testing.T) {
	g := newGame()
	npc := findNPC(t, g, world.NPCHealer)
	ft := newFakeTalker()
	g.Talker = ft
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	msgs := make(chan protocol.NPCDialog, 32)
	id, err := g.Join("hero", Handlers{
		Snapshot: func(protocol.Snapshot) {},
		NPC:      func(d protocol.NPCDialog) { msgs <- d },
	})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan [2]int, 1)
	g.do(func() {
		p := g.players[id]
		p.consent = true
		for _, d := range [][2]int{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
			pos := world.Pos{X: npc.Pos.X - d[0], Y: npc.Pos.Y - d[1]}
			if !g.blocked(pos) {
				g.setPos(p, pos)
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
	if m := recv(); m.Mode != protocol.NPCModeMenu {
		t.Fatalf("ouverture: %+v", m)
	}
	g.TalkAct(id, protocol.NPCTalk, "")
	if m := recv(); m.Mode != protocol.NPCModeChat || !m.Thinking {
		t.Fatalf("entrée en discussion: %+v", m)
	}

	// Le service répond depuis une autre goroutine, comme en vrai.
	deadline := time.Now().Add(3 * time.Second)
	for ft.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	go ft.answer(0, ok("Que la lumière vous garde."))
	if m := recv(); m.Thinking || len(m.Lines) != 1 || m.Lines[0].Text != "Que la lumière vous garde." {
		t.Fatalf("réponse d'ouverture: %+v", m)
	}

	g.TalkAct(id, protocol.NPCSay, "Bonjour")
	if m := recv(); !m.Thinking || m.Lines[len(m.Lines)-1].Who != "you" {
		t.Fatalf("après l'envoi: %+v", m)
	}
	for ft.count() < 2 && time.Now().Before(deadline.Add(2*time.Second)) {
		time.Sleep(5 * time.Millisecond)
	}
	go ft.answer(1, ok("Bonjour à vous."))
	if m := recv(); m.Thinking || m.Lines[len(m.Lines)-1].Text != "Bonjour à vous." {
		t.Fatalf("réponse: %+v", m)
	}
}

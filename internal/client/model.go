package client

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	minWidth   = 60
	minHeight  = 20
	sideWidth  = 24
	chatLines  = 5
	maxChat    = 200
	unloadEach = 100 // snapshots entre deux déchargements de chunks côté client

	maxCombatLog = 50 // lignes de journal de combat conservées
	maxNPCLog    = 30 // lignes de journal de dialogue conservées
)

type state int

const (
	stateLogin state = iota
	stateConnecting
	statePlaying
)

// chanError est un canal purement client, pour afficher les erreurs du serveur.
const chanError = "error"

type chatLine struct{ Channel, From, To, Text string }

// Messages internes.
type (
	connectedMsg struct {
		conn    *Conn
		welcome protocol.Welcome
	}
	connectFailedMsg struct{ err error }
	eventMsg         struct{ Event }
	// pingTickMsg déclenche une mesure de latence; conn évite qu'une ancienne
	// connexion continue d'en émettre après une reconnexion.
	pingTickMsg struct{ conn *Conn }
)

const pingEvery = 5 * time.Second

// Un tick de jeu dure 100 ms: 1 heure de jeu = 1 minute réelle. Le monde
// démarre à 8 h du jour 1.
const (
	ticksPerGameHour = 600
	startGameHour    = 8
)

// gameClock donne le jour (à partir de 1) et l'heure du jeu pour un tick.
func gameClock(tick uint64) (day, hour int) {
	h := int(tick/ticksPerGameHour) + startGameHour
	return h/24 + 1, h % 24
}

func pingTick(c *Conn) tea.Cmd {
	return tea.Tick(pingEvery, func(time.Time) tea.Msg { return pingTickMsg{c} })
}

var errClosed = errors.New("connexion fermée")

// Styles de l'interface (voir package ui).
var (
	titleStyle     = ui.Title
	dimStyle       = ui.Dim
	errStyle       = ui.Err
	nameStyle      = ui.Name
	goldStyle      = ui.Gold
	globalTagStyle = ui.Global
	emoteStyle     = ui.Emote
	whisperStyle   = ui.Whisp
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(ui.ColFrame))
)

// Model est le modèle Bubble Tea du client.
type Model struct {
	state         state
	width, height int

	// transparent laisse le fond du terminal tel quel au lieu de peindre le
	// fond sombre du thème.
	transparent bool

	// Écran de connexion.
	addr, name, pass textinput.Model
	focus            int  // 0 = adresse, 1 = pseudo, 2 = mot de passe
	register         bool // vrai: créer le compte au lieu de s'y connecter
	errMsg           string

	// Partie en cours.
	conn       *Conn
	self       protocol.Welcome
	world      *world.World
	snap       protocol.Snapshot
	hasSnap    bool
	snapCount  int
	chat       []chatLine
	chatScroll int // lignes de recul dans l'historique (0 = en bas)
	chatInput  textinput.Model
	chatting   bool

	// Combat en cours (nil hors combat). Un combat terminé reste affiché
	// jusqu'à ce que le joueur le ferme.
	combat    *protocol.Combat
	combatLog []string
	combatSel int // entrée choisie dans le menu d'actions

	inventory bool // écran d'inventaire ouvert (touche i)

	// Latence mesurée par ping/pong (hasPing faux tant qu'aucune réponse).
	pingSent time.Time
	pingMS   int
	hasPing  bool

	// Dialogue avec un PNJ en cours (nil sinon). Le serveur le ferme (Over) ou
	// le joueur le quitte avec Échap.
	npc      *protocol.NPCDialog
	npcLog   []string
	npcInput textinput.Model // saisie de la discussion libre (voir npctalk.go)
}

// New crée le modèle initial.
func New(addr, name string) Model {
	a := textinput.New()
	a.Prompt = ""
	a.SetValue(addr)
	a.SetWidth(36)

	n := textinput.New()
	n.Prompt = ""
	n.CharLimit = protocol.MaxNameLen
	n.SetValue(name)
	n.SetWidth(36)

	p := textinput.New()
	p.Prompt = ""
	p.EchoMode = textinput.EchoPassword
	p.EchoCharacter = '*'
	p.CharLimit = protocol.MaxPassLen
	p.SetWidth(36)

	// On démarre sur le premier champ à remplir.
	focus := 0
	switch {
	case name != "" && addr != "":
		focus = 2
	case addr != "":
		focus = 1
	}

	c := textinput.New()
	c.Prompt = "> "
	c.CharLimit = protocol.MaxChatLen
	c.Placeholder = "Votre message…"

	m := Model{addr: a, name: n, pass: p, chatInput: c, width: 80, height: 24}
	return m.setFocus(focus)
}

// Marges autour de l'interface, supprimées si le terminal est trop petit pour
// que l'intérieur garde au moins la taille minimale.
const (
	marginX = 2
	marginY = 1
)

func (m Model) margins() (mx, my int) {
	if m.width >= minWidth+2*marginX && m.height >= minHeight+2*marginY {
		return marginX, marginY
	}
	return 0, 0
}

// addMargin entoure s de mx colonnes et my lignes vides, sur une largeur totale w.
func addMargin(s string, mx, my, w int) string {
	if mx == 0 && my == 0 {
		return s
	}
	blank := strings.Repeat(" ", w)
	pad := strings.Repeat(" ", mx)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines)+2*my)
	for range my {
		out = append(out, blank)
	}
	for _, l := range lines {
		out = append(out, ui.Fit(pad+l, w))
	}
	for range my {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// WithTransparent choisit de ne pas peindre le fond du thème.
func (m Model) WithTransparent(t bool) Model {
	m.transparent = t
	return m
}

// Init implémente tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// --- Commandes ---

func connectCmd(addr, name, password string, register bool) tea.Cmd {
	return func() tea.Msg {
		c, w, err := Dial(context.Background(), addr, name, password, register)
		if err != nil {
			return connectFailedMsg{err}
		}
		return connectedMsg{conn: c, welcome: w}
	}
}

func waitEvent(c *Conn) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-c.In
		if !ok {
			return eventMsg{Event{Err: errClosed}}
		}
		return eventMsg{ev}
	}
}

// --- Mise à jour ---

// Update implémente tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if m.conn != nil {
				m.conn.Close()
			}
			return m, tea.Quit
		}
		switch m.state {
		case stateLogin:
			return m.updateLoginKey(msg)
		case statePlaying:
			return m.updatePlayKey(msg)
		}
		return m, nil

	case connectedMsg:
		m.conn = msg.conn
		m.self = msg.welcome
		m.world = world.New(msg.welcome.Seed)
		m.state = statePlaying
		m.errMsg = ""
		m.pass.Reset() // le mot de passe n'a plus de raison de rester en mémoire
		m.hasSnap = false
		m.chat = nil
		m.chatScroll = 0
		m.combat, m.combatLog = nil, nil
		m.npc, m.npcLog = nil, nil
		m.hasPing = false
		m = m.sendPing()
		return m, tea.Batch(waitEvent(m.conn), pingTick(m.conn))

	case pingTickMsg:
		if msg.conn == nil || msg.conn != m.conn || m.state != statePlaying {
			return m, nil // connexion remplacée ou fermée: la chaîne s'arrête
		}
		m = m.sendPing()
		return m, pingTick(m.conn)

	case connectFailedMsg:
		m.state = stateLogin
		m.errMsg = msg.err.Error()
		return m, nil

	case eventMsg:
		return m.updateEvent(msg.Event)
	}

	// Autres messages (clignotement du curseur…) : au champ actif.
	return m.forwardToInput(msg)
}

// sendPing émet un ping et note l'heure d'envoi.
func (m Model) sendPing() Model {
	if m.conn != nil && m.conn.Send(protocol.TPing, nil) {
		m.pingSent = time.Now()
	}
	return m
}

func (m Model) forwardToInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case m.state == statePlaying && m.chatting:
		m.chatInput, cmd = m.chatInput.Update(msg)
	case m.state == stateLogin && m.focus == 0:
		m.addr, cmd = m.addr.Update(msg)
	case m.state == stateLogin && m.focus == 1:
		m.name, cmd = m.name.Update(msg)
	case m.state == stateLogin:
		m.pass, cmd = m.pass.Update(msg)
	}
	return m, cmd
}

func (m Model) updateLoginKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		return m.setFocus((m.focus + 1) % loginFields), nil
	case "shift+tab", "up":
		return m.setFocus((m.focus + loginFields - 1) % loginFields), nil
	case "ctrl+r":
		m.register = !m.register
		m.errMsg = ""
		return m, nil
	case "enter":
		if m.focus < loginFields-1 {
			return m.setFocus(m.focus + 1), nil
		}
		addr := strings.TrimSpace(m.addr.Value())
		name := strings.TrimSpace(m.name.Value())
		pass := m.pass.Value()
		switch {
		case addr == "" || name == "":
			m.errMsg = "adresse et pseudo requis"
			return m, nil
		case pass == "":
			m.errMsg = "mot de passe requis"
			return m, nil
		case m.register && len(pass) < protocol.MinPassLen:
			m.errMsg = fmt.Sprintf("mot de passe: %d caractères minimum", protocol.MinPassLen)
			return m, nil
		}
		m.state = stateConnecting
		m.errMsg = ""
		return m, connectCmd(addr, name, pass, m.register)
	}
	return m.forwardToInput(msg)
}

// loginFields est le nombre de champs de l'écran de connexion.
const loginFields = 3

func (m Model) setFocus(f int) Model {
	m.focus = f
	m.addr.Blur()
	m.name.Blur()
	m.pass.Blur()
	switch f {
	case 0:
		m.addr.Focus()
	case 1:
		m.name.Focus()
	default:
		m.pass.Focus()
	}
	return m
}

// dirOf traduit une touche en direction (flèches, ZQSD, WASD, hjkl, yubn).
func dirOf(key string) (dx, dy int, ok bool) {
	switch key {
	case "up", "z", "w", "k":
		return 0, -1, true
	case "down", "s", "j":
		return 0, 1, true
	case "left", "q", "a", "h":
		return -1, 0, true
	case "right", "d", "l":
		return 1, 0, true
	case "y":
		return -1, -1, true
	case "u":
		return 1, -1, true
	case "b":
		return -1, 1, true
	case "n":
		return 1, 1, true
	}
	return 0, 0, false
}

func (m Model) updatePlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "pgup":
		m.scrollChat(chatLines - 1)
		return m, nil
	case "pgdown":
		m.scrollChat(-(chatLines - 1))
		return m, nil
	}

	if m.combat != nil {
		return m.updateCombatKey(msg)
	}
	if m.npc != nil {
		return m.updateNPCKey(msg)
	}

	if m.chatting {
		switch msg.String() {
		case "esc":
			m.chatting = false
			m.chatInput.Reset()
			m.chatInput.Blur()
			return m, nil
		case "enter":
			if text := strings.TrimSpace(m.chatInput.Value()); text != "" && m.conn != nil {
				m.conn.Send(protocol.TChatSend, protocol.ChatSend{Text: text})
			}
			m.chatting = false
			m.chatInput.Reset()
			m.chatInput.Blur()
			return m, nil
		}
		return m.forwardToInput(msg)
	}

	if m.inventory {
		if k := msg.String(); k == "esc" || k == "i" {
			m.inventory = false
		}
		return m, nil
	}
	if msg.String() == "i" {
		m.inventory = true
		return m, nil
	}
	if msg.String() == "enter" {
		m.chatting = true
		return m, m.chatInput.Focus()
	}
	if dx, dy, ok := dirOf(msg.String()); ok && m.conn != nil {
		m.conn.Send(protocol.TMove, protocol.Move{DX: dx, DY: dy})
	}
	return m, nil
}

func (m Model) updateEvent(ev Event) (tea.Model, tea.Cmd) {
	if ev.Err != nil {
		if m.conn != nil {
			m.conn.Close()
		}
		m.conn = nil
		m.state = stateLogin
		m.chatting = false
		m.combat, m.combatLog = nil, nil
		m.npc, m.npcLog = nil, nil
		m.errMsg = "connexion perdue: " + ev.Err.Error()
		return m, nil
	}

	switch ev.Env.T {
	case protocol.TSnapshot:
		var s protocol.Snapshot
		if err := ev.Env.Payload(&s); err == nil {
			m.snap, m.hasSnap = s, true
			m.snapCount++
			if m.snapCount%unloadEach == 0 {
				m.unloadWorld()
			}
		}
	case protocol.TChat:
		var c protocol.Chat
		if err := ev.Env.Payload(&c); err == nil {
			m.addChat(chatLine{Channel: c.Channel, From: c.From, To: c.To, Text: c.Text})
		}
	case protocol.TCombat:
		var c protocol.Combat
		if err := ev.Env.Payload(&c); err == nil {
			m.applyCombat(c)
		}
	case protocol.TPong:
		if !m.pingSent.IsZero() {
			m.pingMS, m.hasPing = int(time.Since(m.pingSent).Milliseconds()), true
		}
	case protocol.TNPC:
		var d protocol.NPCDialog
		if err := ev.Env.Payload(&d); err == nil {
			m.applyNPC(d)
		}
	case protocol.TError:
		var e protocol.Error
		if err := ev.Env.Payload(&e); err == nil {
			m.addChat(chatLine{Channel: chanError, Text: e.Msg})
		}
	}
	if m.conn == nil {
		return m, nil
	}
	return m, waitEvent(m.conn)
}

// applyCombat enregistre un nouvel état de combat et accumule son journal.
func (m *Model) applyCombat(c protocol.Combat) {
	if m.combat == nil || m.combat.Over { // nouveau combat
		m.combatLog = nil
		m.combatSel = 0
	}
	m.chatting, m.inventory = false, false
	m.chatInput.Reset()
	m.chatInput.Blur()

	log := append(make([]string, 0, len(m.combatLog)+len(c.Log)), m.combatLog...)
	log = append(log, c.Log...)
	if len(log) > maxCombatLog {
		log = log[len(log)-maxCombatLog:]
	}
	m.combatLog = log
	m.combat = &c
}

// applyNPC enregistre l'état d'un dialogue; un dialogue terminé se ferme aussitôt.
func (m *Model) applyNPC(d protocol.NPCDialog) {
	d = cleanNPCDialog(d)
	if d.Over {
		m.npc, m.npcLog = nil, nil
		m.npcInput.Blur()
		return
	}
	prevMode := ""
	if m.npc != nil {
		prevMode = m.npc.Mode
	}
	if m.npc == nil { // nouveau dialogue
		m.npcLog = nil
	}
	m.chatting, m.inventory = false, false
	m.chatInput.Reset()
	m.chatInput.Blur()

	log := append(make([]string, 0, len(m.npcLog)+len(d.Log)), m.npcLog...)
	log = append(log, d.Log...)
	if len(log) > maxNPCLog {
		log = log[len(log)-maxNPCLog:]
	}
	m.npcLog = log
	m.npc = &d
	m.syncNPCInput(prevMode)
}

// npcKeys associe les touches aux actions d'un dialogue. Seules les actions que
// le PNJ propose réellement sont envoyées.
var npcKeys = map[string]string{
	"a": protocol.NPCBuy,
	"v": protocol.NPCSell,
	"s": protocol.NPCHeal,
	"d": protocol.NPCTalk,
}

func (m Model) updateNPCKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.npc.Mode {
	case protocol.NPCModeChat:
		return m.updateNPCChatKey(msg)
	case protocol.NPCModeNotice:
		return m.updateNPCNoticeKey(msg)
	}
	key := msg.String()
	if key == "esc" || key == "enter" {
		if m.conn != nil {
			m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: protocol.NPCLeave})
		}
		return m, nil
	}
	action, ok := npcKeys[key]
	if !ok || m.conn == nil {
		return m, nil
	}
	for _, o := range m.npc.Offers {
		if o.Action == action {
			m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: action})
			break
		}
	}
	return m, nil
}

// combatKeys associe les touches aux actions de combat (lettres ou chiffres).
var combatKeys = map[string]string{
	"a": "attack", "1": "attack",
	"d": "defend", "2": "defend",
	"i": "item", "3": "item",
	"f": "flee", "4": "flee",
}

func (m Model) updateCombatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.combat.Over {
		switch key {
		case "enter", "space", "esc":
			m.combat, m.combatLog = nil, nil
		}
		return m, nil
	}
	switch key {
	case "up":
		m.combatSel = (m.combatSel + len(combatMenu) - 1) % len(combatMenu)
		return m, nil
	case "down":
		m.combatSel = (m.combatSel + 1) % len(combatMenu)
		return m, nil
	case "enter":
		if m.conn != nil {
			m.conn.Send(protocol.TCombatAction, protocol.CombatAction{Action: combatMenu[m.combatSel].action})
		}
		return m, nil
	}
	if action, ok := combatKeys[key]; ok && m.conn != nil {
		m.conn.Send(protocol.TCombatAction, protocol.CombatAction{Action: action})
	}
	return m, nil
}

// combatMenu est le menu d'actions, dans l'ordre d'affichage.
var combatMenu = []struct{ key, action, label string }{
	{"A", "attack", "Attaquer"},
	{"D", "defend", "Défendre"},
	{"I", "item", "Potion"},
	{"F", "flee", "Fuir"},
}

func (m *Model) addChat(l chatLine) {
	// Copie pour ne pas partager le tableau sous-jacent entre copies du modèle.
	chat := append(make([]chatLine, 0, len(m.chat)+1), m.chat...)
	chat = append(chat, l)
	if len(chat) > maxChat {
		chat = chat[len(chat)-maxChat:]
	}
	m.chat = chat
	// Si l'on relit l'historique, garder la même page à l'écran.
	if m.chatScroll > 0 {
		m.chatScroll = min(m.chatScroll+1, m.maxScroll())
	}
}

// maxScroll est le recul maximal dans l'historique.
func (m Model) maxScroll() int { return max(0, len(m.chat)-chatLines) }

func (m *Model) scrollChat(delta int) {
	m.chatScroll = max(0, min(m.chatScroll+delta, m.maxScroll()))
}

// formatChat met en forme une ligne selon son canal.
func (m Model) formatChat(l chatLine) string {
	switch l.Channel {
	case protocol.ChanGlobal:
		return globalTagStyle.Render("[G] ") + nameStyle.Render(l.From) + ": " + l.Text
	case protocol.ChanEmote:
		return emoteStyle.Render("* " + l.From + " " + l.Text)
	case protocol.ChanWhisper:
		if l.From == m.self.Name {
			return whisperStyle.Render("à " + l.To + ": " + l.Text)
		}
		return whisperStyle.Render(l.From + " vous chuchote: " + l.Text)
	case chanError:
		return errStyle.Render("! " + l.Text)
	}
	if l.From == "" { // système
		return dimStyle.Render("* " + l.Text)
	}
	return nameStyle.Render(l.From) + ": " + l.Text
}

func (m Model) unloadWorld() {
	c := world.ChunkOf(m.snap.Self.X, m.snap.Self.Y)
	keep := make(map[world.ChunkPos]bool)
	// Garder au moins ce qui est à l'écran (+1 chunk de marge).
	rx := max(3, m.width/2/world.ChunkSize+2)
	ry := max(3, m.height/2/world.ChunkSize+2)
	for dy := -ry; dy <= ry; dy++ {
		for dx := -rx; dx <= rx; dx++ {
			keep[world.ChunkPos{X: c.X + dx, Y: c.Y + dy}] = true
		}
	}
	m.world.Unload(keep)
}

// --- Vue ---

// View implémente tea.Model.
func (m Model) View() tea.View {
	// L'interface est dessinée dans la zone intérieure, entourée d'une marge.
	mx, my := m.margins()
	inner := m
	inner.width, inner.height = m.width-2*mx, m.height-2*my
	var s string
	switch m.state {
	case statePlaying:
		s = inner.viewPlaying()
	default:
		s = inner.viewLogin()
	}
	s = addMargin(s, mx, my, m.width)
	v := tea.NewView(s)
	v.AltScreen = true
	if !m.transparent {
		v.BackgroundColor = lipgloss.Color(ui.ColInk)
		v.ForegroundColor = lipgloss.Color(ui.ColText)
	}
	v.WindowTitle = "sudo Dragon"
	return v
}

func (m Model) viewLogin() string {
	panelW := min(m.width, 56)
	inner := panelW - 2
	rich := m.height >= 22 && m.width >= ui.LogoWidth()+6 // assez de place pour le logo

	var lines []string
	if rich {
		lines = append(lines,
			ui.Center(ui.Faint.Render(".   '   *   .   '   .   *   '"), inner),
			ui.Fit(strings.Repeat(" ", max(0, (inner-ui.LogoWidth())/2))+ui.OK.Render("$")+" "+titleStyle.Render("sudo"), inner))
		for _, l := range ui.Logo() {
			lines = append(lines, ui.Center(l, inner))
		}
		lines = append(lines, ui.Center(dimStyle.Render("[")+ui.OK.Render("sudo")+dimStyle.Render("] privilèges accordés. Bonne chance."), inner))
	} else {
		lines = append(lines, ui.Center(titleStyle.Render("sudo Dragon"), inner))
	}
	lines = append(lines, "")

	// Mode: Ctrl+R bascule entre les deux entrées.
	modeEntry := func(label string, on bool) string {
		if on {
			return ui.Select.Render(" › " + label + " ")
		}
		return dimStyle.Render("   " + label + " ")
	}
	lines = append(lines, ui.Center(modeEntry("Connexion", !m.register)+" "+modeEntry("Création de compte", m.register), inner), "")

	field := func(i int, label string, view string) string {
		mark := "  "
		if m.focus == i {
			mark = titleStyle.Render("› ")
		}
		return " " + mark + dimStyle.Render(ui.Fit(label, 13)) + view
	}
	lines = append(lines,
		field(0, "Serveur", m.addr.View()),
		field(1, "Pseudo", m.name.View()),
		field(2, "Mot de passe", m.pass.View()),
		"")

	switch {
	case m.state == stateConnecting:
		lines = append(lines, ui.Center("Connexion…", inner))
	case m.errMsg != "":
		lines = append(lines, ui.Center(errStyle.Render(m.errMsg), inner))
	default:
		lines = append(lines, ui.Center(ui.KeyBar([2]string{"tab", "champ"}, [2]string{"↵", "ok"}, [2]string{"^r", "mode"}, [2]string{"^c", "quitter"}), inner))
	}

	box := ui.Panel("", strings.Join(lines, "\n"), panelW, len(lines)+2)
	footer := ui.Fit(dimStyle.Render(" v0.1.0")+strings.Repeat(" ", max(1, panelW-len(" v0.1.0")-len("Bubble Tea · Lip Gloss · Bubbles ")))+dimStyle.Render("Bubble Tea · Lip Gloss · Bubbles"), panelW)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box+"\n"+footer)
}

func (m Model) viewPlaying() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal trop petit (%dx%d), minimum %dx%d.", m.width, m.height, minWidth, minHeight)
	}
	if !m.hasSnap {
		return "Chargement du monde…"
	}

	// Hauteur: en-tête (1) + panneau central (mapH+2) + discussion (chatLines+2) + raccourcis (1).
	// L'interface occupe toute la fenêtre. Le terrain vient de la graine et peut
	// dépasser la zone de vision du serveur; seules les entités (joueurs,
	// monstres) sont limitées à cette zone.
	mapW := m.width - sideWidth - 4
	mapH := m.height - chatLines - 6
	fullW := mapW + sideWidth + 4

	self := m.snap.Self
	ents := append([]protocol.Entity{self}, m.snap.Others...)
	mapStr := render.Viewport(m.world, self.X, self.Y, mapW, mapH, ents, m.snap.Monsters, self.ID)

	var top string
	switch {
	case m.inventory && m.combat == nil && m.npc == nil:
		top = m.inventoryView(fullW, mapH+2)
	case m.combat != nil:
		top = m.combatView(fullW, mapH+2)
	case m.npc != nil:
		top = m.npcView(fullW, mapH+2)
	default:
		terrain := fmt.Sprintf("%s · (%d, %d)", render.TerrainName(m.world.At(self.X, self.Y)), self.X, self.Y)
		top = lipgloss.JoinHorizontal(lipgloss.Top,
			ui.Panel(terrain, mapStr, mapW+2, mapH+2),
			ui.Panel("Joueurs", m.sidePanel(), sideWidth+2, mapH+2),
		)
	}
	return m.headerLine(fullW) + "\n" + top + "\n" + m.chatPanel(fullW) + "\n" + ui.Fit(m.helpLine(), fullW)
}

// headerLine est la ligne du haut: nom du jeu à gauche, infos de session à droite.
func (m Model) headerLine(w int) string {
	parts := []string{m.serverHost()}
	if m.snap.Online > 0 {
		parts = append(parts, fmt.Sprintf("%d en ligne", m.snap.Online))
	}
	if m.hasPing {
		parts = append(parts, ui.OK.Render(fmt.Sprintf("%d ms", m.pingMS)))
	}
	day, hour := gameClock(m.snap.Tick)
	parts = append(parts, fmt.Sprintf("Jour %d, %02dh", day, hour))
	right := dimStyle.Render(parts[0])
	for _, p := range parts[1:] {
		right += dimStyle.Render(" · ") + p
	}
	pad := max(1, w-ansi.StringWidth("sudo Dragon")-ansi.StringWidth(right))
	return ui.Fit(titleStyle.Render("sudo Dragon")+strings.Repeat(" ", pad)+right, w)
}

// serverHost extrait l'hôte de l'adresse saisie (ws://hote:8080/ws -> hote:8080).
func (m Model) serverHost() string {
	a := strings.TrimSpace(m.addr.Value())
	if _, rest, ok := strings.Cut(a, "://"); ok {
		a = rest
	}
	host, _, _ := strings.Cut(a, "/")
	return host
}

func (m Model) sidePanel() string {
	self := m.snap.Self
	st := m.snap.Stats

	lines := []string{
		" " + nameStyle.Render(self.Name),
		fmt.Sprintf(" Niv %d   XP %d/%d", st.Level, st.XP, st.XPNext),
		"    " + ui.FlatBar(st.XP, st.XPNext, 10, ui.ColOK),
		" PV " + ui.Bar(st.HP, st.MaxHP, 10) + fmt.Sprintf(" %d/%d", st.HP, st.MaxHP),
		dimStyle.Render(fmt.Sprintf(" Atq %d  Déf %d  Pot %d", st.Atk, st.Def, st.Potions)),
		" " + goldStyle.Render(fmt.Sprintf("Or  %d", st.Gold)),
		"",
		" " + dimStyle.Render(fmt.Sprintf("JOUEURS PROCHES (%d)", len(m.snap.Others))),
	}
	for _, o := range m.snap.Others {
		lines = append(lines, "  "+nameStyle.Render("@")+" "+o.Name)
	}

	// Monstres en vue, du plus proche au plus loin, avec leur position relative.
	mons := slices.Clone(m.snap.Monsters)
	dist := func(mo protocol.Monster) int { return max(abs(mo.X-self.X), abs(mo.Y-self.Y)) }
	slices.SortStableFunc(mons, func(a, b protocol.Monster) int { return dist(a) - dist(b) })
	lines = append(lines, "", " "+dimStyle.Render(fmt.Sprintf("MONSTRES EN VUE (%d)", len(mons))))
	for _, mo := range mons {
		lines = append(lines, fmt.Sprintf("  %c %s %+d,%+d", render.MonsterGlyph(mo.Kind), render.MonsterName(mo.Kind), mo.X-self.X, mo.Y-self.Y))
	}
	lines = append(lines, "",
		" "+dimStyle.Render("Graine ")+fmt.Sprintf("%d", m.self.Seed),
	)
	return strings.Join(lines, "\n")
}

// chatPanel dessine la discussion: chatLines lignes dans un cadre, la saisie
// remplaçant la dernière ligne pendant qu'on écrit.
func (m Model) chatPanel(w int) string {
	scroll := min(m.chatScroll, m.maxScroll())
	end := len(m.chat) - scroll
	start := max(0, end-chatLines)
	visible := m.chat[start:end]

	out := make([]string, 0, chatLines)
	for len(out)+len(visible) < chatLines {
		out = append(out, "")
	}
	for _, l := range visible {
		out = append(out, " "+m.formatChat(l))
	}
	if m.chatting {
		out[len(out)-1] = " " + m.chatInput.View()
	}
	return ui.Panel("Discussion", strings.Join(out, "\n"), w, chatLines+2)
}

// helpLine est la barre de raccourcis, adaptée au contexte.
func (m Model) helpLine() string {
	scroll := min(m.chatScroll, m.maxScroll())
	switch {
	case m.inventory:
		return ui.KeyBar([2]string{"esc", "fermer"})
	case m.chatting:
		return ui.KeyBar([2]string{"↵", "envoyer"}, [2]string{"esc", "annuler"})
	case scroll > 0:
		return dimStyle.Render(fmt.Sprintf("↑ historique (-%d) · PgBas: revenir en bas", scroll))
	case m.npc != nil:
		return m.npcHelp()
	case m.combat != nil && !m.combat.Over:
		return ui.KeyBar([2]string{"↑↓", "choisir"}, [2]string{"↵", "valider"}, [2]string{"a d i f", "raccourcis"})
	case m.combat != nil:
		return ui.KeyBar([2]string{"↵", "continuer"})
	}
	return ui.KeyBar([2]string{"↑↓←→", "marcher"}, [2]string{"i", "sac"}, [2]string{"↵", "discuter"}, [2]string{"PgHaut", "historique"}, [2]string{"ctrl+c", "quitter"})
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// inventoryView dessine le sac (potions et or, seuls objets du jeu) et le
// détail de la potion, dans exactement w x h cellules.
func (m Model) inventoryView(w, h int) string {
	st := m.snap.Stats
	detailW := min(40, w/2)
	bag := []string{
		"",
		ui.Select.Render(ui.Fit(fmt.Sprintf(" › Potion de soin ×%d", st.Potions), w-detailW-2)),
		"",
		" " + dimStyle.Render("Or     ") + goldStyle.Render(fmt.Sprintf("%d po", st.Gold)),
	}
	detail := []string{
		"",
		" " + titleStyle.Render("Potion de soin"),
		" " + dimStyle.Render("Consommable"),
		"",
		" Rend des PV pendant un combat",
		" (touche I ou 3 en combat).",
		"",
		" " + dimStyle.Render("S'achète chez les marchands,"),
		" " + dimStyle.Render("les soigneurs sont gratuits."),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel("Sac à dos · "+m.snap.Self.Name, strings.Join(bag, "\n"), w-detailW, h),
		ui.Panel("Détail", strings.Join(detail, "\n"), detailW, h),
	)
}

// journalPanel dessine les dernières lignes d'un journal dans un cadre de w x h.
func journalPanel(title string, log []string, w, h int) string {
	n := max(0, h-2)
	if len(log) > n {
		log = log[len(log)-n:]
	}
	lines := make([]string, len(log))
	for i, l := range log {
		lines[i] = " " + l
	}
	return ui.Panel(title, strings.Join(lines, "\n"), w, h)
}

// combatView dessine l'écran de combat dans exactement w x h cellules: le
// champ de bataille et le menu d'actions côte à côte, le journal dessous.
func (m Model) combatView(w, h int) string {
	c := m.combat
	name := render.MonsterName(c.Monster)

	actionW := min(30, w/2)
	leftW := w - actionW
	logH := min(8, h-6)
	midH := h - logH
	iw, ih := leftW-2, midH-2

	barW := max(4, min(30, iw-12))
	monsterLines := []string{" " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColErr)).Render(name),
		fmt.Sprintf(" %s %3d/%d", ui.Bar(c.MonsterHP, c.MonsterMaxHP, barW), c.MonsterHP, c.MonsterMaxHP)}
	playerLines := []string{" Vous",
		fmt.Sprintf(" %s %3d/%d", ui.Bar(c.PlayerHP, c.PlayerMaxHP, barW), c.PlayerHP, c.PlayerMaxHP)}

	field := slices.Clone(monsterLines)
	if art := render.MonsterArt(c.Monster); ih >= len(art)+len(monsterLines)+len(playerLines)+2 {
		style := render.ArtStyle(c.Monster)
		field = append(field, "")
		for _, l := range art {
			field = append(field, ui.Center(style.Render(l), iw))
		}
		field = append(field, "")
	}
	field = append(field, playerLines...)

	var actions []string
	if c.Over {
		switch c.Result {
		case protocol.ResultWon:
			actions = append(actions, " "+ui.OK.Bold(true).Render("VICTOIRE !"))
		case protocol.ResultLost:
			actions = append(actions, " "+errStyle.Bold(true).Render("DÉFAITE…"))
		default:
			actions = append(actions, " "+dimStyle.Render("Vous avez fui."))
		}
		actions = append(actions, " "+dimStyle.Render("Entrée: continuer"))
	} else {
		for i, it := range combatMenu {
			label := it.label
			if it.action == "item" {
				label = fmt.Sprintf("%s (%d)", label, c.Potions)
			}
			if i == m.combatSel {
				actions = append(actions, ui.Select.Render(ui.Fit(" › ["+it.key+"] "+label, actionW-2)))
			} else {
				actions = append(actions, "   ["+it.key+"] "+label)
			}
		}
	}

	title := "COMBAT · " + name
	mid := lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(title, strings.Join(field, "\n"), leftW, midH),
		ui.Panel("Actions", strings.Join(actions, "\n"), actionW, midH),
	)
	return mid + "\n" + journalPanel("Journal", m.combatLog, w, logH)
}

// npcKeyLabel est la lettre affichée pour chaque action de dialogue.
var npcKeyLabel = map[string]string{
	protocol.NPCBuy:     "A",
	protocol.NPCSell:    "V",
	protocol.NPCHeal:    "S",
	protocol.NPCTalk:    "D",
	protocol.NPCConsent: "O",
	protocol.NPCDecline: "N",
}

// npcView dessine le dialogue avec un PNJ dans exactement w x h cellules:
// portrait et état à gauche, offres à droite, journal dessous.
func (m Model) npcView(w, h int) string {
	d := m.npc
	kind := world.NPCKind(d.Kind)
	switch d.Mode {
	case protocol.NPCModeChat:
		return m.npcChatView(w, h)
	case protocol.NPCModeNotice:
		return m.npcNoticeView(w, h)
	}

	rightW := min(40, w/2)
	leftW := w - rightW
	logH := max(3, min(6, h-8))
	midH := h - logH
	iw, ih := leftW-2, midH-2

	status := []string{
		" " + goldStyle.Render(fmt.Sprintf("Or: %d", d.Gold)),
		fmt.Sprintf(" Potions: %d", d.Potions),
		fmt.Sprintf(" PV: %d/%d", d.HP, d.MaxHP),
	}
	var left []string
	if art := render.NPCArt(kind); ih >= len(art)+1+len(status) {
		style := render.NPCArtStyle(kind)
		for _, l := range art {
			left = append(left, ui.Center(style.Render(l), iw))
		}
		left = append(left, "")
	}
	left = append(left, status...)

	var offers []string
	for _, o := range d.Offers {
		var price string
		switch {
		case o.Gain:
			price = fmt.Sprintf("+%d or", o.Price)
		case o.Price == 0:
			price = "gratuit"
		default:
			price = fmt.Sprintf("%d or", o.Price)
		}
		line := fmt.Sprintf(" [%s] %-22s %s", npcKeyLabel[o.Action], o.Label, price)
		if !o.Available {
			line = dimStyle.Render(line)
		}
		offers = append(offers, line)
	}
	offers = append(offers, "", " "+dimStyle.Render("[Échap] Quitter"))

	mid := lipgloss.JoinHorizontal(lipgloss.Top,
		ui.Panel(m.npcTitle(), strings.Join(left, "\n"), leftW, midH),
		ui.Panel("Offres", strings.Join(offers, "\n"), rightW, midH),
	)
	return mid + "\n" + journalPanel("Dialogue", m.npcLog, w, logH)
}

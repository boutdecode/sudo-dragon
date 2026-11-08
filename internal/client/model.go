package client

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
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
)

var errClosed = errors.New("connexion fermée")

// Styles de l'interface.
var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#facc15"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	nameStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#22d3ee"))

	goldStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24"))
	globalTagStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fb923c"))
	emoteStyle     = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#c084fc"))
	whisperStyle   = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#f472b6"))
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4b5563"))
)

// Model est le modèle Bubble Tea du client.
type Model struct {
	state         state
	width, height int

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

	// Dialogue avec un PNJ en cours (nil sinon). Le serveur le ferme (Over) ou
	// le joueur le quitte avec Échap.
	npc    *protocol.NPCDialog
	npcLog []string
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
		return m, waitEvent(m.conn)

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
	}
	m.chatting = false
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
	if d.Over {
		m.npc, m.npcLog = nil, nil
		return
	}
	if m.npc == nil { // nouveau dialogue
		m.npcLog = nil
	}
	m.chatting = false
	m.chatInput.Reset()
	m.chatInput.Blur()

	log := append(make([]string, 0, len(m.npcLog)+len(d.Log)), m.npcLog...)
	log = append(log, d.Log...)
	if len(log) > maxNPCLog {
		log = log[len(log)-maxNPCLog:]
	}
	m.npcLog = log
	m.npc = &d
}

// npcKeys associe les touches aux actions d'un dialogue. Seules les actions que
// le PNJ propose réellement sont envoyées.
var npcKeys = map[string]string{
	"a": protocol.NPCBuy,
	"v": protocol.NPCSell,
	"s": protocol.NPCHeal,
}

func (m Model) updateNPCKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	if action, ok := combatKeys[key]; ok && m.conn != nil {
		m.conn.Send(protocol.TCombatAction, protocol.CombatAction{Action: action})
	}
	return m, nil
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
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			keep[world.ChunkPos{X: c.X + dx, Y: c.Y + dy}] = true
		}
	}
	m.world.Unload(keep)
}

// --- Vue ---

// View implémente tea.Model.
func (m Model) View() tea.View {
	var s string
	switch m.state {
	case statePlaying:
		s = m.viewPlaying()
	default:
		s = m.viewLogin()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	v.WindowTitle = "sudo Dragon"
	return v
}

func (m Model) viewLogin() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("s u d o   D R A G O N") + "\n")
	mode := "Connexion"
	if m.register {
		mode = "Création de compte"
	}
	b.WriteString(dimStyle.Render(mode) + "\n\n")
	b.WriteString("Serveur\n" + m.addr.View() + "\n\n")
	b.WriteString("Pseudo\n" + m.name.View() + "\n\n")
	b.WriteString("Mot de passe\n" + m.pass.View() + "\n\n")
	switch {
	case m.state == stateConnecting:
		b.WriteString("Connexion…")
	case m.errMsg != "":
		b.WriteString(errStyle.Render(m.errMsg))
	default:
		b.WriteString(dimStyle.Render("Tab: champ suivant · Entrée: valider\nCtrl+R: connexion / création de compte · Ctrl+C: quitter"))
	}
	box := boxStyle.Padding(1, 3).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// padRight tronque ou complète s pour qu'il occupe exactement w colonnes.
func padRight(s string, w int) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if n := w - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

func (m Model) viewPlaying() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal trop petit (%dx%d), minimum %dx%d.", m.width, m.height, minWidth, minHeight)
	}
	if !m.hasSnap {
		return "Chargement du monde…"
	}

	mapW := min(m.width-sideWidth-4, 2*protocol.ViewHalfW+1)
	mapH := min(m.height-2-chatLines-1, 2*protocol.ViewHalfH+1)

	ents := append([]protocol.Entity{m.snap.Self}, m.snap.Others...)
	mapStr := render.Viewport(m.world, m.snap.Self.X, m.snap.Self.Y, mapW, mapH, ents, m.snap.Monsters, m.snap.Self.ID)

	var top string
	if m.combat != nil {
		top = boxStyle.Render(m.combatPanel(mapW+sideWidth+2, mapH))
	} else if m.npc != nil {
		top = boxStyle.Render(m.npcPanel(mapW+sideWidth+2, mapH))
	} else {
		top = lipgloss.JoinHorizontal(lipgloss.Top,
			boxStyle.Render(mapStr),
			boxStyle.Render(m.sidePanel(mapH)),
		)
	}
	return top + "\n" + m.chatPanel()
}

func (m Model) sidePanel(h int) string {
	self := m.snap.Self
	terrain := render.TerrainName(m.world.At(self.X, self.Y))
	st := m.snap.Stats

	lines := []string{
		nameStyle.Render(self.Name),
		dimStyle.Render(fmt.Sprintf("Pos  %d, %d", self.X, self.Y)),
		dimStyle.Render("Sol  " + terrain),
		fmt.Sprintf("Niv %d   XP %d/%d", st.Level, st.XP, st.XPNext),
		"PV " + render.Bar(st.HP, st.MaxHP, 10) + fmt.Sprintf(" %d/%d", st.HP, st.MaxHP),
		dimStyle.Render(fmt.Sprintf("Atq %d  Déf %d  Pot %d", st.Atk, st.Def, st.Potions)),
		goldStyle.Render(fmt.Sprintf("Or  %d", st.Gold)),
		"",
		fmt.Sprintf("Joueurs proches (%d)", len(m.snap.Others)),
	}
	for _, o := range m.snap.Others {
		lines = append(lines, "  "+o.Name)
	}

	// Monstres en vue, du plus proche au plus loin, avec leur position relative.
	mons := slices.Clone(m.snap.Monsters)
	dist := func(mo protocol.Monster) int { return max(abs(mo.X-self.X), abs(mo.Y-self.Y)) }
	slices.SortStableFunc(mons, func(a, b protocol.Monster) int { return dist(a) - dist(b) })
	lines = append(lines, "", fmt.Sprintf("Monstres en vue (%d)", len(mons)))
	for _, mo := range mons {
		lines = append(lines, fmt.Sprintf("  %c %s %+d,%+d", render.MonsterGlyph(mo.Kind), render.MonsterName(mo.Kind), mo.X-self.X, mo.Y-self.Y))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = padRight(lines[i], sideWidth)
	}
	return strings.Join(lines, "\n")
}

func (m Model) chatPanel() string {
	w := m.width
	scroll := min(m.chatScroll, m.maxScroll())
	end := len(m.chat) - scroll
	start := max(0, end-chatLines)
	visible := m.chat[start:end]

	out := make([]string, 0, chatLines+1)
	for _, l := range visible {
		out = append(out, padRight(m.formatChat(l), w))
	}
	for len(out) < chatLines {
		out = append([]string{padRight("", w)}, out...)
	}

	switch {
	case m.chatting:
		out = append(out, padRight(m.chatInput.View(), w))
	case scroll > 0:
		out = append(out, padRight(dimStyle.Render(fmt.Sprintf("↑ historique (-%d) · PgBas: revenir en bas", scroll)), w))
	case m.npc != nil:
		out = append(out, padRight(dimStyle.Render("Dialogue: touches entre crochets · Échap ou Entrée: quitter"), w))
	case m.combat != nil && !m.combat.Over:
		out = append(out, padRight(dimStyle.Render("Combat: A attaquer · D défendre · I potion · F fuir (ou 1 à 4)"), w))
	case m.combat != nil:
		out = append(out, padRight(dimStyle.Render("Combat terminé: Entrée pour continuer"), w))
	default:
		out = append(out, padRight(dimStyle.Render("Entrée: chat (/help) · flèches/ZQSD/hjkl: bouger · PgHaut: historique · Ctrl+C: quitter"), w))
	}
	return strings.Join(out, "\n")
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// centerPad centre s dans une largeur de w colonnes.
func centerPad(s string, w int) string {
	left := max(0, (w-ansi.StringWidth(s))/2)
	return padRight(strings.Repeat(" ", left)+s, w)
}

// combatPanel dessine l'écran de combat dans exactement w x h cellules.
func (m Model) combatPanel(w, h int) string {
	c := m.combat
	name := render.MonsterName(c.Monster)
	barW := max(8, min(30, w-30))

	header := titleStyle.Render("COMBAT") + " — " + name
	// Colonne des noms assez large pour « Seigneur de guerre » (les deux jauges restent alignées).
	nameW := max(8, ansi.StringWidth(name))
	barW = max(8, min(barW, w-nameW-24))
	monsterBar := fmt.Sprintf("%-*s %s %3d/%d", nameW, name, render.Bar(c.MonsterHP, c.MonsterMaxHP, barW), c.MonsterHP, c.MonsterMaxHP)
	playerBar := fmt.Sprintf("%-*s %s %3d/%d   Potions: %d", nameW, "Vous", render.Bar(c.PlayerHP, c.PlayerMaxHP, barW), c.PlayerHP, c.PlayerMaxHP, c.Potions)

	var actions string
	if c.Over {
		switch c.Result {
		case protocol.ResultWon:
			actions = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ade80")).Render("VICTOIRE !")
		case protocol.ResultLost:
			actions = errStyle.Bold(true).Render("DÉFAITE…")
		default:
			actions = dimStyle.Render("Vous avez fui.")
		}
		actions += dimStyle.Render("   Entrée: continuer")
	} else {
		actions = fmt.Sprintf("[A] Attaquer  [D] Défendre  [I] Potion (%d)  [F] Fuir", c.Potions)
	}

	// Lignes fixes: titre, 2 jauges, actions + 3 séparateurs. Le reste se
	// partage entre le sprite (s'il tient) et le journal.
	art := render.MonsterArt(c.Monster)
	room := h - 7
	spriteLines := 0
	if room-4 >= len(art)+1 {
		spriteLines = len(art) + 1
	}
	logLines := min(room-spriteLines, 8)

	lines := []string{centerPad(header, w), ""}
	if spriteLines > 0 {
		style := render.ArtStyle(c.Monster)
		for _, l := range art {
			lines = append(lines, centerPad(style.Render(l), w))
		}
		lines = append(lines, "")
	}
	lines = append(lines, centerPad(monsterBar, w), centerPad(playerBar, w), "")

	if logLines > 0 {
		visible := m.combatLog
		if len(visible) > logLines {
			visible = visible[len(visible)-logLines:]
		}
		for _, l := range visible {
			lines = append(lines, "  "+l)
		}
	}
	for len(lines) < h-2 { // remplissage jusqu'à la zone d'actions
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), h-2)]
	lines = append(lines, "", centerPad(actions, w))

	for i := range lines {
		lines[i] = padRight(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

// npcKeyLabel est la lettre affichée pour chaque action de dialogue.
var npcKeyLabel = map[string]string{
	protocol.NPCBuy:  "A",
	protocol.NPCSell: "V",
	protocol.NPCHeal: "S",
}

// npcPanel dessine l'écran de dialogue dans exactement w x h cellules.
func (m Model) npcPanel(w, h int) string {
	d := m.npc
	kind := world.NPCKind(d.Kind)
	title := titleStyle.Render(strings.ToUpper(render.NPCName(kind))) + dimStyle.Render(" — dialogue")
	status := fmt.Sprintf("%s   Potions: %d   PV: %d/%d",
		goldStyle.Render(fmt.Sprintf("Or: %d", d.Gold)), d.Potions, d.HP, d.MaxHP)

	// Lignes fixes: titre, état, hint + séparateurs; offres et journal se partagent le reste.
	art := render.NPCArt(kind)
	offerLines := make([]string, 0, len(d.Offers))
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
		line := fmt.Sprintf("[%s] %-22s %s", npcKeyLabel[o.Action], o.Label, price)
		if !o.Available {
			line = dimStyle.Render(line)
		}
		offerLines = append(offerLines, line)
	}

	room := h - 8 - len(offerLines)
	spriteLines := 0
	if room-3 >= len(art)+1 {
		spriteLines = len(art) + 1
	}
	logLines := max(0, min(room-spriteLines, 6))

	lines := []string{centerPad(title, w), ""}
	if spriteLines > 0 {
		style := render.NPCArtStyle(kind)
		for _, l := range art {
			lines = append(lines, centerPad(style.Render(l), w))
		}
		lines = append(lines, "")
	}
	lines = append(lines, centerPad(status, w), "")
	for _, l := range offerLines {
		lines = append(lines, centerPad(l, w))
	}
	lines = append(lines, "")
	if logLines > 0 {
		visible := m.npcLog
		if len(visible) > logLines {
			visible = visible[len(visible)-logLines:]
		}
		for _, l := range visible {
			lines = append(lines, "  "+l)
		}
	}
	for len(lines) < h-2 {
		lines = append(lines, "")
	}
	lines = lines[:min(len(lines), h-2)]
	lines = append(lines, "", centerPad(dimStyle.Render("[Échap] Quitter"), w))

	for i := range lines {
		lines[i] = padRight(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

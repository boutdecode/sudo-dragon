package client

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/render"
	"github.com/kevin/ascii-adventure/internal/ui"
	"github.com/kevin/ascii-adventure/internal/world"
)

const (
	minWidth   = 60
	minHeight  = 20
	sideWidth  = 34
	chatLines  = 5 // hauteur par défaut (et minimale) de la discussion
	chatStep   = 3 // lignes gagnées ou perdues par ctrl+haut / ctrl+bas
	minMapH    = 8 // hauteur de carte conservée quand la discussion grandit
	maxChat    = 200
	unloadEach = 100 // snapshots entre deux déchargements de chunks côté client

	maxCombatLog = 50 // lignes de journal de combat conservées
	maxNPCLog    = 30 // lignes de journal de dialogue conservées
)

type state int

const (
	stateLogin state = iota
	stateConnecting
	stateCreating // personnage à créer: tirage des caractéristiques et choix de la classe
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
		create  *protocol.Create // non nil: le personnage reste à créer (pas encore de welcome)
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
	ticksPerGameHour = protocol.TicksPerGameHour
	startGameHour    = protocol.StartGameHour
)

// gameClock donne le jour (à partir de 1) et l'heure du jeu pour un tick.
func gameClock(tick uint64) (day, hour int) {
	h := int(tick/ticksPerGameHour) + startGameHour
	return h/24 + 1, h % 24
}

// clockTick est le tick de jeu estimé maintenant. Le serveur n'envoie un
// instantané que si la vue change: le tick du dernier reçu est donc prolongé
// par le temps écoulé depuis (un tick = 100 ms).
func (m Model) clockTick() uint64 {
	if m.snapAt.IsZero() {
		return m.snap.Tick
	}
	return m.snap.Tick + uint64(time.Since(m.snapAt)/(100*time.Millisecond))
}

// skyIcon est l'indicateur de moment de la journée de l'en-tête (ASCII pur).
func skyIcon(tick uint64) string {
	switch d := render.Daylight(tick); {
	case d >= 1:
		return "*"
	case d <= 0:
		return "("
	default:
		return "~"
	}
}

func pingTick(c *Conn) tea.Cmd {
	return tea.Tick(pingEvery, func(time.Time) tea.Msg { return pingTickMsg{c} })
}

var errClosed = errors.New("connection closed")

// t traduit une clé dans la langue de l'interface.
func (m Model) t(key string, args ...any) string { return i18n.T(m.lang, key, args...) }

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
	lang          i18n.Lang // langue de l'interface (et des messages demandés au serveur)

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
	snapAt     time.Time // réception du dernier instantané: sert à prolonger l'horloge entre deux
	hasSnap    bool
	snapCount  int
	chat       []chatLine
	chatScroll int // lignes de recul dans l'historique (0 = en bas)
	chatH      int // hauteur voulue de la discussion (0 = chatLines), agrandie par ctrl+haut
	chatInput  textinput.Model
	chatHist   []string        // lignes envoyées depuis le chat (la plus récente en dernier), rappelées par haut/bas
	histPos    int             // position dans chatHist pendant le rappel (len(chatHist) = saisie en cours)
	histDraft  string          // saisie en cours mise de côté pendant le rappel
	markName   textinput.Model // nom d'un repère de la carte en cours de saisie
	chatting   bool

	// Combat en cours (nil hors combat). Un combat terminé reste affiché
	// jusqu'à ce que le joueur le ferme.
	combat       *protocol.Combat
	combatLog    []string
	combatSel    int     // entrée choisie dans le menu d'actions
	fx           fxState // animation des coups (voir fx.go)
	noFX         bool    // effets coupés (SUDO_DRAGON_NO_FX)
	combatTarget int     // monstre visé (indice dans Combat.Enemies)

	inventory bool // écran d'inventaire ouvert (touche i)
	sheet     bool // fiche de personnage ouverte (touche c)
	worldMap  bool // carte du monde ouverte (touche m)

	// Donjon (voir dungeon.go): dmap est nil sur la carte principale.
	zone protocol.Zone
	// dungeons: entrées de donjon que le joueur a découvertes (envoyées par le serveur).
	dungeons []protocol.DungeonInfo
	dmap     *world.DungeonMap
	dseen    map[world.Pos]bool // cases du donjon déjà vues
	wmap     mapState

	// Création du personnage (état stateCreating).
	create *createState

	// Latence mesurée par ping/pong (hasPing faux tant qu'aucune réponse).
	pingSent time.Time
	pingMS   int
	hasPing  bool

	// Dialogue avec un PNJ en cours (nil sinon). Le serveur le ferme (Over) ou
	// le joueur le quitte avec Échap.
	npc      *protocol.NPCDialog
	npcLog   []string
	npcSel   int             // offre d'équipement examinée (index dans itemOffers), -1 = aucune
	npcInput textinput.Model // saisie de la discussion libre (voir npctalk.go)

	// Livre de quêtes (voir quests.go).
	quests       protocol.Quests
	questsAt     time.Time // réception du livre (les pauses des donneurs en partent)
	questBook    bool      // livre ouvert (touche q)
	questTab     int       // 0 = quêtes en cours, 1 = réputation et historique
	questSel     int
	questConfirm bool // un premier "x" attend sa confirmation

	// Sac d'équipement (voir bag.go).
	bag    protocol.Bag
	bagAt  time.Time // réception du sac : les bénédictions de sanctuaire y décomptent leur temps restant
	bagSel int

	// Dernier endroit annoncé dans le journal (contrée, lieu) : voir location.go.
	locSeen      bool
	locTerritory world.TerritoryID
	locPlace     world.RegionPos
	locInPlace   bool
	locBand      int // tranche de niveaux de région de la dernière position

	// Marche automatique /goto (voir goto.go).
	walking    bool
	walkTarget world.Pos
	walkPath   []world.Pos
	walkGen    int
	walkLast   world.Pos // position au dernier pas envoyé
	walkStuck  int       // ticks consécutifs sans progrès
}

// New crée le modèle initial, en français. Voir NewLang pour une autre langue.
func New(addr, name string) Model { return NewLang(addr, name, i18n.FR) }

// NewLang crée le modèle initial dans la langue lang.
func NewLang(addr, name string, lang i18n.Lang) Model {
	lang = lang.Normalize()
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
	c.Placeholder = i18n.T(lang, "chat.placeholder")

	m := Model{addr: a, name: n, pass: p, chatInput: c, markName: newMarkerInput(), width: 80, height: 24, lang: lang, noFX: fxDisabled()}
	m.npcInput = newNPCInput(lang)
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

var sgrRe = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// themeSGR renvoie la séquence SGR qui applique le fond et le texte du thème.
func themeSGR() string {
	rgb := func(hex string) (int, int, int) {
		var r, g, b int
		fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
		return r, g, b
	}
	br, bg, bb := rgb(ui.ColInk)
	fr, fg, fb := rgb(ui.ColText)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm", br, bg, bb, fr, fg, fb)
}

// paintBackground peint le fond du thème dans les cellules elles-mêmes (le
// changement de fond demandé au terminal par OSC 11 n'est pas partout
// supporté) : chaque ligne est complétée à w colonnes, le tout à h lignes, et
// le fond est rétabli après chaque remise à zéro des styles internes.
func paintBackground(s string, w, h int) string {
	base := themeSGR()
	lines := strings.Split(s, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		l = sgrRe.ReplaceAllStringFunc(l, func(seq string) string {
			switch sgrRe.FindStringSubmatch(seq)[1] {
			case "", "0", "49", "39":
				return seq + base
			}
			return seq
		})
		lines[i] = base + ui.Fit(l, w) + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// WithTransparent choisit de ne pas peindre le fond du thème.
func (m Model) WithTransparent(t bool) Model {
	m.transparent = t
	return m
}

// Init implémente tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// --- Commandes ---

func connectCmd(addr, name, password string, register bool, lang i18n.Lang) tea.Cmd {
	return func() tea.Msg {
		c, w, create, err := DialCreate(context.Background(), addr, name, password, register, lang)
		if err != nil {
			return connectFailedMsg{err}
		}
		return connectedMsg{conn: c, welcome: w, create: create}
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
	case shakeTickMsg:
		return m.updateShake(msg)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			if m.state == statePlaying {
				m.saveMapNow()
			}
			if m.conn != nil {
				m.conn.Close()
			}
			return m, tea.Quit
		}
		switch m.state {
		case stateLogin:
			return m.updateLoginKey(msg)
		case stateCreating:
			return m.updateCreateKey(msg)
		case statePlaying:
			return m.updatePlayKey(msg)
		}
		return m, nil

	case connectedMsg:
		if msg.create != nil {
			m.conn = msg.conn
			m.state = stateCreating
			m.errMsg = ""
			m.pass.Reset()
			m.create = newCreateState(*msg.create)
			return m, waitEvent(m.conn)
		}
		return m.enterGame(msg.conn, msg.welcome)

	case pingTickMsg:
		if msg.conn == nil || msg.conn != m.conn || m.state != statePlaying {
			return m, nil // connexion remplacée ou fermée: la chaîne s'arrête
		}
		m = m.sendPing()
		return m, pingTick(m.conn)

	case mapBlinkMsg:
		return m.updateMapBlink(msg)

	case walkTickMsg:
		return m.updateWalk(msg)

	case connectFailedMsg:
		m.state = stateLogin
		m.errMsg = msg.err.Error()
		return m, nil

	case eventMsg:
		if m.state == stateCreating {
			return m.updateCreateEvent(msg.Event)
		}
		return m.updateEvent(msg.Event)
	}

	// Autres messages (clignotement du curseur…) : au champ actif.
	return m.forwardToInput(msg)
}

// enterGame passe en jeu une fois la connexion établie et le personnage créé.
func (m Model) enterGame(conn *Conn, welcome protocol.Welcome) (tea.Model, tea.Cmd) {
	m.conn = conn
	m.self = welcome
	m.world = world.NewVersion(welcome.Seed, welcome.Gen)
	m.zone, m.dmap, m.dseen, m.dungeons = protocol.Zone{}, nil, nil, nil
	m.state = statePlaying
	m.errMsg = ""
	m.pass.Reset() // le mot de passe n'a plus de raison de rester en mémoire
	m.hasSnap = false
	m.chat = nil
	m.chatScroll = 0
	m.combat, m.combatLog = nil, nil
	m.npc, m.npcLog = nil, nil
	m.quests, m.questBook, m.questSel, m.questConfirm = protocol.Quests{}, false, 0, false
	m.locSeen, m.locInPlace = false, false
	m.bag, m.bagSel, m.sheet = protocol.Bag{}, 0, false
	m.worldMap, m.wmap = false, loadMapState(welcome.Seed, m.world.GenVersion)
	m.stopNaming()
	m.hasPing = false
	m.walkGen++
	m.walking, m.walkPath = false, nil
	m = m.sendPing()
	return m, tea.Batch(waitEvent(m.conn), pingTick(m.conn))
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
	case m.state == statePlaying && m.worldMap && m.wmap.naming:
		m.markName, cmd = m.markName.Update(msg)
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
			m.errMsg = m.t("client.err.addr_name")
			return m, nil
		case pass == "":
			m.errMsg = m.t("client.err.pass")
			return m, nil
		case m.register && len(pass) < protocol.MinPassLen:
			m.errMsg = m.t("client.err.pass_min", protocol.MinPassLen)
			return m, nil
		}
		m.state = stateConnecting
		m.errMsg = ""
		return m, connectCmd(addr, name, pass, m.register, m.lang)
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
		m.scrollChat(m.chatHeight() - 1)
		return m, nil
	case "pgdown":
		m.scrollChat(-(m.chatHeight() - 1))
		return m, nil
	case "ctrl+up":
		m.chatH = m.chatHeight() + chatStep
		m.chatH = m.chatHeight() // borne au plafond actuel
		m.scrollChat(0)
		return m, nil
	case "ctrl+down":
		m.chatH = max(chatLines, m.chatHeight()-chatStep)
		m.scrollChat(0)
		return m, nil
	}

	if m.combat != nil {
		return m.updateCombatKey(msg)
	}
	if m.npc != nil {
		return m.updateNPCKey(msg)
	}

	// Une touche de déplacement, Échap, q ou i interrompt la marche automatique.
	if !m.chatting && m.walking {
		k := msg.String()
		if _, _, isDir := dirOf(k); isDir || k == "esc" || k == "q" || k == "i" || k == "p" || k == "m" {
			m.stopWalk("cli.goto.interrupted")
			if isDir || k == "esc" {
				return m, nil
			}
		}
	}

	if m.chatting {
		switch msg.String() {
		case "tab":
			m.completeChat()
			return m, nil
		case "up":
			m.recallHistory(-1)
			return m, nil
		case "down":
			m.recallHistory(+1)
			return m, nil
		case "esc":
			m.chatting = false
			m.chatInput.Reset()
			m.chatInput.Blur()
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.chatInput.Value())
			m.pushHistory(text)
			m.chatting = false
			m.chatInput.Reset()
			m.chatInput.Blur()
			if target, isCmd, ok := parseGoto(text); isCmd {
				if !ok {
					if name, isName := parseGotoName(text); isName {
						return m.gotoName(name)
					}
					m.sysLine("cli.err.usage_goto")
					return m, nil
				}
				return m.startGoto(target)
			}
			if text != "" && m.conn != nil {
				m.conn.Send(protocol.TChatSend, protocol.ChatSend{Text: text})
			}
			return m, nil
		}
		return m.forwardToInput(msg)
	}

	if m.questBook {
		return m.updateQuestKey(msg)
	}
	if m.inventory {
		return m.updateBagKey(msg)
	}
	if m.sheet {
		return m.updateSheetKey(msg)
	}
	if m.worldMap {
		return m.updateMapKey(msg)
	}
	if msg.String() == "m" {
		return m.openMap()
	}
	if msg.String() == "p" {
		m.sheet = true
		return m, nil
	}
	if msg.String() == "q" {
		m.questBook, m.questTab = true, 0
		return m, nil
	}
	if msg.String() == "i" {
		m.inventory = true
		return m, nil
	}
	if msg.String() == "enter" {
		m.chatting = true
		m.histPos, m.histDraft = len(m.chatHist), ""
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
		reason := ev.Err.Error()
		if errors.Is(ev.Err, errClosed) {
			reason = m.t("client.closed")
		}
		m.errMsg = m.t("client.lost", reason)
		return m, nil
	}

	var saveCmd tea.Cmd
	switch ev.Env.T {
	case protocol.TSnapshot:
		var s protocol.Snapshot
		if err := ev.Env.Payload(&s); err == nil {
			m.snap, m.hasSnap, m.snapAt = s, true, time.Now()
			if m.dmap != nil {
				m.revealAround() // sous terre: ni lieux, ni exploration du monde
			} else {
				m.trackLocation()
				m.markExplored()
				saveCmd = m.maybeSaveExplored()
				m.snapCount++
				if m.snapCount%unloadEach == 0 {
					m.unloadWorld()
				}
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
			saveCmd = tea.Batch(saveCmd, m.startFx(c))
		}
	case protocol.TPong:
		if !m.pingSent.IsZero() {
			m.pingMS, m.hasPing = int(time.Since(m.pingSent).Milliseconds()), true
		}
	case protocol.TZone:
		var z protocol.Zone
		if err := ev.Env.Payload(&z); err == nil {
			m.applyZone(z)
		}
	case protocol.TDungeons:
		var d protocol.Dungeons
		if err := ev.Env.Payload(&d); err == nil {
			m.applyDungeons(d)
		}
	case protocol.TBag:
		var b protocol.Bag
		if err := ev.Env.Payload(&b); err == nil {
			m.applyBag(b)
		}
	case protocol.TQuests:
		var b protocol.Quests
		if err := ev.Env.Payload(&b); err == nil {
			m.applyQuests(b)
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
		return m, saveCmd
	}
	return m, tea.Batch(waitEvent(m.conn), saveCmd)
}

// applyCombat enregistre un nouvel état de combat et accumule son journal.
func (m *Model) applyCombat(c protocol.Combat) {
	if m.combat == nil || m.combat.Over { // nouveau combat
		m.combatLog = nil
		m.combatSel, m.combatTarget = 0, -1 // -1: le plus fort, choisi plus bas
	}
	m.chatting, m.inventory, m.questBook, m.sheet = false, false, false, false
	m.worldMap = false
	m.stopNaming()
	m.chatInput.Reset()
	m.chatInput.Blur()

	log := append(make([]string, 0, len(m.combatLog)+len(c.Log)), m.combatLog...)
	log = append(log, c.Log...)
	if len(log) > maxCombatLog {
		log = log[len(log)-maxCombatLog:]
	}
	m.combatLog = log
	m.combat = &c
	if t := m.combatTarget; t < 0 || t >= len(c.Enemies) || c.Enemies[t].HP <= 0 { // cible abattue: le monstre le plus fort
		m.combatTarget = max(0, c.Boss)
	}
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
	fresh := m.npc == nil
	if fresh { // nouveau dialogue
		m.npcLog = nil
	}
	m.chatting, m.inventory, m.sheet = false, false, false
	m.worldMap = false
	m.stopNaming()
	m.chatInput.Reset()
	m.chatInput.Blur()

	log := append(make([]string, 0, len(m.npcLog)+len(d.Log)), m.npcLog...)
	log = append(log, d.Log...)
	if len(log) > maxNPCLog {
		log = log[len(log)-maxNPCLog:]
	}
	m.npcLog = log
	m.npc = &d
	switch n := len(m.itemOffers()); {
	case n == 0 || fresh || prevMode != d.Mode:
		m.npcSel = -1
	case m.npcSel >= n: // objet vendu ou acheté en fin de liste
		m.npcSel = n - 1
	}
	m.syncNPCInput(prevMode)
}

// npcKeys associe les touches aux actions d'un dialogue. Seules les actions que
// le PNJ propose réellement sont envoyées.
func (m Model) npcKeys() map[string]string {
	return map[string]string{
		strings.ToLower(m.t("npc.key.buy")):    protocol.NPCBuy,
		strings.ToLower(m.t("npc.key.sell")):   protocol.NPCSell,
		strings.ToLower(m.t("npc.key.heal")):   protocol.NPCHeal,
		strings.ToLower(m.t("npc.key.talk")):   protocol.NPCTalk,
		strings.ToLower(m.t("npc.key.work")):   protocol.NPCAskWork,
		strings.ToLower(m.t("npc.key.places")): protocol.NPCPlaces,
		strings.ToLower(m.t("npc.key.steal")):  protocol.NPCSteal,
	}
}

func (m Model) updateNPCKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.npc.Mode {
	case protocol.NPCModeChat:
		return m.updateNPCChatKey(msg)
	case protocol.NPCModeNotice:
		return m.updateNPCNoticeKey(msg)
	case protocol.NPCModeQuest:
		return m.updateNPCQuestKey(msg)
	}
	key := msg.String()
	if items := m.itemOffers(); len(items) > 0 {
		switch key {
		case "tab", "down", "j":
			m.npcSel = (m.npcSel + 1) % len(items)
			return m, nil
		case "shift+tab", "up", "k":
			if m.npcSel <= 0 {
				m.npcSel = len(items) - 1
			} else {
				m.npcSel--
			}
			return m, nil
		case "enter":
			if m.npcSel >= 0 && m.npcSel < len(items) {
				if m.conn != nil {
					o := items[m.npcSel]
					m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: o.Action, Text: o.Arg})
				}
				return m, nil
			}
		}
	}
	if key == "esc" || key == "enter" {
		if m.conn != nil {
			// Dans la liste de revente, Échap revient à la boutique ; sinon il quitte le dialogue.
			action := protocol.NPCLeave
			if m.npc.Mode == protocol.NPCModeSell {
				action = protocol.NPCBack
			}
			m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: action})
		}
		return m, nil
	}
	if n, err := strconv.Atoi(key); err == nil && n >= 1 && m.conn != nil {
		if offers := m.numberedOffers(); n <= len(offers) { // n-ième offre numérotée : quête à rendre, objet à acheter ou revendre
			m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: offers[n-1].Action, Text: offers[n-1].Arg})
		}
		return m, nil
	}
	action, ok := m.npcKeys()[key]
	if !ok || m.conn == nil {
		return m, nil
	}
	offered := func(a string) bool {
		for _, o := range m.npc.Offers {
			if o.Action == a {
				return true
			}
		}
		return false
	}
	if action == protocol.NPCSell && !offered(action) && offered(protocol.NPCSellMenu) {
		action = protocol.NPCSellMenu // chez un forgeron, la même touche ouvre la revente d'équipement
	}
	if offered(action) {
		m.conn.Send(protocol.TNPCAction, protocol.NPCAction{Action: action})
	}
	return m, nil
}

// combatKeys associe les touches aux actions de combat (lettres ou chiffres).
var combatKeys = map[string]string{
	"a": "attack", "1": "attack",
	"i": "item", "2": "item",
	"f": "flee", "3": "flee",
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
	case "left", "right":
		m.combatTarget = m.nextTarget(key == "right")
		return m, nil
	case "enter":
		if m.conn != nil {
			m.conn.Send(protocol.TCombatAction, protocol.CombatAction{Action: combatMenu[m.combatSel].action, Target: m.combatTarget})
		}
		return m, nil
	}
	if action, ok := combatKeys[key]; ok && m.conn != nil {
		m.conn.Send(protocol.TCombatAction, protocol.CombatAction{Action: action, Target: m.combatTarget})
	}
	return m, nil
}

// nextTarget renvoie le monstre vivant suivant (ou précédent) la cible actuelle.
func (m Model) nextTarget(forward bool) int {
	n, step := len(m.combat.Enemies), 1
	if !forward {
		step = -1
	}
	for i := 1; i <= n; i++ {
		t := ((m.combatTarget+i*step)%n + n) % n
		if m.combat.Enemies[t].HP > 0 {
			return t
		}
	}
	return m.combatTarget
}

// combatMenu est le menu d'actions, dans l'ordre d'affichage.
var combatMenu = []struct{ key, action, label string }{
	{"A", "attack", "fight.act.attack"},
	{"I", "item", "fight.act.item"},
	{"F", "flee", "fight.act.flee"},
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
func (m Model) maxScroll() int { return max(0, len(m.chat)-m.chatHeight()) }

// chatHeight est la hauteur de la discussion: celle voulue, bornée pour laisser minMapH lignes à la carte.
func (m Model) chatHeight() int {
	h := m.chatH
	if h < chatLines {
		h = chatLines
	}
	return max(chatLines, min(h, m.height-6-minMapH))
}

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
			return whisperStyle.Render(m.t("chat.whisper_to", l.To, l.Text))
		}
		return whisperStyle.Render(m.t("chat.whisper_from", l.From, l.Text))
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
	case stateCreating:
		s = inner.viewCreate()
	default:
		s = inner.viewLogin()
	}
	s = addMargin(s, mx, my, m.width)
	if !m.transparent {
		s = paintBackground(s, m.width, m.height)
	}
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
		lines = append(lines, ui.Center(dimStyle.Render("[")+ui.OK.Render("sudo")+dimStyle.Render("] "+m.t("login.sudo_granted")), inner))
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
	lines = append(lines, ui.Center(modeEntry(m.t("login.mode.login"), !m.register)+" "+modeEntry(m.t("login.mode.register"), m.register), inner), "")

	field := func(i int, label string, view string) string {
		mark := "  "
		if m.focus == i {
			mark = titleStyle.Render("› ")
		}
		return " " + mark + dimStyle.Render(ui.Fit(label, 13)) + view
	}
	lines = append(lines,
		field(0, m.t("login.field.server"), m.addr.View()),
		field(1, m.t("login.field.name"), m.name.View()),
		field(2, m.t("login.field.pass"), m.pass.View()),
		"")

	switch {
	case m.state == stateConnecting:
		lines = append(lines, ui.Center(m.t("login.connecting"), inner))
	case m.errMsg != "":
		lines = append(lines, ui.Center(errStyle.Render(m.errMsg), inner))
	default:
		lines = append(lines, ui.Center(ui.KeyBar([2]string{"tab", m.t("key.field")}, [2]string{"↵", m.t("key.ok")}, [2]string{"^r", m.t("key.mode")}, [2]string{"^c", m.t("key.quit")}), inner))
	}

	box := ui.Panel("", strings.Join(lines, "\n"), panelW, len(lines)+2)
	footer := ui.Fit(dimStyle.Render(" v0.1.0")+strings.Repeat(" ", max(1, panelW-len(" v0.1.0")-len("Bubble Tea · Lip Gloss · Bubbles ")))+dimStyle.Render("Bubble Tea · Lip Gloss · Bubbles"), panelW)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box+"\n"+footer)
}

func (m Model) viewPlaying() string {
	if m.width < minWidth || m.height < minHeight {
		return m.t("client.too_small", m.width, m.height, minWidth, minHeight)
	}
	if !m.hasSnap {
		return m.t("client.loading")
	}

	// Hauteur: en-tête (1) + panneau central (mapH+2) + discussion (chatHeight+2) + raccourcis (1).
	// L'interface occupe toute la fenêtre. Le terrain vient de la graine et peut
	// dépasser la zone de vision du serveur; seules les entités (joueurs,
	// monstres) sont limitées à cette zone.
	mapW := m.width - sideWidth - 4
	mapH := m.height - m.chatHeight() - 6
	fullW := mapW + sideWidth + 4

	self := m.snap.Self
	ents := append([]protocol.Entity{self}, m.snap.Others...)
	night := 0
	if !m.inDungeon() { // le cycle jour/nuit ne se voit pas sous terre
		night = render.NightLevel(m.clockTick())
	}
	mapStr := render.ViewportNight(m.terrain(), self.X, self.Y, mapW, mapH, ents, m.snap.Monsters, self.ID, m.readyGivers(), m.giverIdle, night)

	var top string
	switch {
	case m.questBook && m.combat == nil && m.npc == nil:
		top = m.questView(fullW, mapH+2)
	case m.inventory && m.combat == nil && m.npc == nil:
		top = m.inventoryView(fullW, mapH+2)
	case m.sheet && m.combat == nil && m.npc == nil:
		top = m.sheetView(fullW, mapH+2)
	case m.worldMap && m.combat == nil && m.npc == nil:
		top = m.mapView(fullW, mapH+2)
	case m.combat != nil:
		top = m.combatView(fullW, mapH+2)
	case m.npc != nil:
		top = m.npcView(fullW, mapH+2)
	default:
		top = lipgloss.JoinHorizontal(lipgloss.Top,
			ui.Panel(m.locationTitle(self.X, self.Y, mapW), mapStr, mapW+2, mapH+2),
			ui.Panel(m.t("hud.players"), m.sidePanel(), sideWidth+2, mapH+2),
		)
	}
	return m.headerLine(fullW) + "\n" + top + "\n" + m.chatPanel(fullW) + "\n" + ui.Fit(m.helpLine(), fullW)
}

// headerLine est la ligne du haut: nom du jeu à gauche, infos de session à droite.
func (m Model) headerLine(w int) string {
	parts := []string{m.serverHost()}
	if m.snap.Online > 0 {
		parts = append(parts, m.t("hud.online", m.snap.Online))
	}
	if m.hasPing {
		parts = append(parts, ui.OK.Render(fmt.Sprintf("%d ms", m.pingMS)))
	}
	tick := m.clockTick()
	day, hour := gameClock(tick)
	parts = append(parts, m.t("hud.day", day, hour)+" "+skyIcon(tick))
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

	// Trois jauges au même format : libellé, barre de 10 cases, valeur.
	gauge := func(label, bar string, cur, maxV int) string {
		return label + bar + fmt.Sprintf(" %d/%d", cur, maxV)
	}
	const barW = 10
	lines := []string{
		" " + nameStyle.Render(self.Name),
		m.t("hud.level", st.Level),
		gauge(m.t("hud.xp"), ui.FlatBar(st.XP, st.XPNext, barW, ui.ColGold), st.XP, st.XPNext),
		gauge(m.t("hud.hp"), ui.Bar(st.HP, st.MaxHP, barW), st.HP, st.MaxHP),
	}
	if st.MaxMana > 0 {
		lines = append(lines, gauge(m.t("hud.mana"), ui.FlatBar(st.Mana, st.MaxMana, barW, ui.ColMana), st.Mana, st.MaxMana))
	}
	lines = append(lines, "",
		" "+dimStyle.Render(m.t("hud.nearby", len(m.snap.Others))))
	for _, o := range m.snap.Others {
		lines = append(lines, "  "+nameStyle.Render("@")+" "+o.Name)
	}

	// Monstres en vue, du plus proche au plus loin, avec leur position relative.
	mons := slices.Clone(m.snap.Monsters)
	dist := func(mo protocol.Monster) int { return max(abs(mo.X-self.X), abs(mo.Y-self.Y)) }
	slices.SortStableFunc(mons, func(a, b protocol.Monster) int { return dist(a) - dist(b) })
	lines = append(lines, "", " "+dimStyle.Render(m.t("hud.monsters", len(mons))))
	for _, mo := range mons {
		lines = append(lines, fmt.Sprintf("  %c %s %+d,%+d", render.MonsterGlyph(mo.Kind), render.MonsterName(m.lang, mo.Kind), mo.X-self.X, mo.Y-self.Y))
	}
	if s := m.questSummary(); s != "" {
		lines = append(lines, "", s)
	}
	return strings.Join(lines, "\n")
}

// chatPanel dessine la discussion: chatHeight lignes dans un cadre, la saisie
// remplaçant la dernière ligne pendant qu'on écrit.
func (m Model) chatPanel(w int) string {
	scroll := min(m.chatScroll, m.maxScroll())
	end := len(m.chat) - scroll
	ch := m.chatHeight()
	start := max(0, end-ch)
	visible := m.chat[start:end]

	out := make([]string, 0, ch)
	for len(out)+len(visible) < ch {
		out = append(out, "")
	}
	for _, l := range visible {
		out = append(out, " "+m.formatChat(l))
	}
	if m.chatting {
		out[len(out)-1] = " " + m.chatInput.View()
	}
	return ui.Panel(m.t("chat.title"), strings.Join(out, "\n"), w, ch+2)
}

// helpLine est la barre de raccourcis, adaptée au contexte.
func (m Model) helpLine() string {
	scroll := min(m.chatScroll, m.maxScroll())
	switch {
	case m.questBook:
		return ui.KeyBar([2]string{"↑↓", m.t("key.choose")}, [2]string{"tab", m.t("key.tab")}, [2]string{"x", m.t("key.abandon")}, [2]string{"esc", m.t("key.close")})
	case m.inventory:
		return ui.KeyBar([2]string{"↑↓", m.t("key.choose")}, [2]string{"e", m.t("key.equip")}, [2]string{"esc", m.t("key.close")})
	case m.sheet:
		return ui.KeyBar([2]string{"esc", m.t("key.close")})
	case m.worldMap && m.wmap.naming:
		return dimStyle.Render(m.t("map.name_prompt")+" ") + m.markName.View() + "  " + ui.KeyBar([2]string{"↵", m.t("key.confirm")}, [2]string{"esc", m.t("key.cancel")})
	case m.worldMap:
		return ui.KeyBar([2]string{"↑↓←→", m.t("key.cursor")}, [2]string{"+ -", m.t("key.zoom")}, [2]string{"g", m.t("key.goto")}, [2]string{"w", m.t("key.marker")}, [2]string{"x", m.t("key.delete_marker")}, [2]string{"0", m.t("key.recenter")}, [2]string{"esc", m.t("key.close")})
	case m.chatting:
		return ui.KeyBar([2]string{"↵", m.t("key.send")}, [2]string{"esc", m.t("key.cancel")})
	case scroll > 0:
		return dimStyle.Render(m.t("key.history_hint", scroll))
	case m.npc != nil:
		return m.npcHelp()
	case m.combat != nil && !m.combat.Over:
		return ui.KeyBar([2]string{"↑↓", m.t("key.choose")}, [2]string{"↵", m.t("key.confirm")}, [2]string{"←→", m.t("key.target")}, [2]string{"a i f", m.t("key.shortcuts")})
	case m.combat != nil:
		return ui.KeyBar([2]string{"↵", m.t("key.continue")})
	}
	return ui.KeyBar([2]string{"↑↓←→", m.t("key.walk")}, [2]string{"i", m.t("key.bag")}, [2]string{"p", m.t("key.sheet")}, [2]string{"m", m.t("key.map")}, [2]string{"q", m.t("key.quests")}, [2]string{"↵", m.t("key.chat")}, [2]string{m.t("key.pgup"), m.t("key.history")}, [2]string{"ctrl+↑↓", m.t("key.chat_size")}, [2]string{"ctrl+c", m.t("key.quit")})
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
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

// npcKeyLabel est la lettre affichée pour chaque action de dialogue.
func (m Model) npcKeyLabel(action string) string {
	switch action {
	case protocol.NPCBuy:
		return m.t("npc.key.buy")
	case protocol.NPCSell:
		return m.t("npc.key.sell")
	case protocol.NPCHeal:
		return m.t("npc.key.heal")
	case protocol.NPCTalk:
		return m.t("npc.key.talk")
	case protocol.NPCAskWork:
		return m.t("npc.key.work")
	case protocol.NPCSellMenu:
		return m.t("npc.key.sell")
	case protocol.NPCPlaces:
		return m.t("npc.key.places")
	case protocol.NPCSteal:
		return m.t("npc.key.steal")
	case protocol.NPCBack:
		return "Esc"
	case protocol.NPCConsent:
		return m.t("npc.key.consent")
	case protocol.NPCDecline:
		return m.t("npc.key.decline")
	}
	return "?"
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
	case protocol.NPCModeQuest:
		return m.npcQuestView(w, h)
	}

	rightW := min(40, w/2)
	if len(m.numberedOffers()) > 0 && m.npc.Specialty != "" || m.npc.Mode == protocol.NPCModeSell {
		rightW = min(70, w*3/5) // étalage d'équipement : une ligne par objet avec ses bonus
	}
	leftW := w - rightW
	logH := max(3, min(6, h-8))
	if len(m.itemOffers()) > 0 {
		logH = 3 // l'étalage a besoin de la place : liste + fiche de l'objet
	}
	midH := h - logH
	iw, ih := leftW-2, midH-2

	status := []string{
		" " + goldStyle.Render(m.t("npc.gold", d.Gold)),
		m.t("npc.potions", d.Potions),
		m.t("npc.hp", d.HP, d.MaxHP),
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
	if d.Specialty != "" {
		left = append(left, "")
		for _, l := range wrap(m.t("shop.specialty", d.Specialty), max(8, iw-2)) {
			left = append(left, " "+dimStyle.Render(l))
		}
	}

	var offers []string
	turnIn, itemIdx := 0, 0
	for _, o := range d.Offers {
		var price string
		switch {
		case o.Gain:
			price = m.t("npc.price_gain", o.Price)
		case o.Price == 0:
			price = m.t("npc.price_free")
		default:
			price = m.t("npc.price", o.Price)
		}
		if o.Action == protocol.NPCTurnIn {
			price = ""
		}
		var line string
		if o.Detail != "" { // équipement : nom, bonus et niveau, prix
			line = fmt.Sprintf(" [%s] %s %s %s", m.npcOfferKey(o, turnIn), ui.Fit(o.Label, 26), ui.Fit(o.Detail, 24), price)
		} else {
			line = fmt.Sprintf(" [%s] %-22s %s", m.npcOfferKey(o, turnIn), o.Label, price)
		}
		if isNumbered(o.Action) {
			turnIn++
		}
		if !o.Available {
			line = dimStyle.Render(line)
		}
		if o.Info != nil {
			if itemIdx == m.npcSel {
				line = ui.Select.Render(ansi.Strip(line))
			}
			itemIdx++
		}
		offers = append(offers, line)
	}
	offers = append(offers, "", " "+dimStyle.Render(m.t("npc.leave")))

	leftPanel := ui.Panel(m.npcTitle(), strings.Join(left, "\n"), leftW, midH)
	right := ui.Panel(m.t("npc.offers"), strings.Join(offers, "\n"), rightW, midH)
	if items := m.itemOffers(); len(items) > 0 { // étalage d'équipement : fiche de l'objet examiné sous la liste
		listH := len(offers) + 2
		detailW := rightW
		if midH-listH < 8 { // écran bas : la fiche remplace le portrait, à gauche, et la liste garde toute la colonne
			detailW = leftW
		}
		var detail []string
		if m.npcSel >= 0 && m.npcSel < len(items) {
			detail = m.itemDetailLines(*items[m.npcSel].Info, detailW-4, true)
		} else {
			for _, l := range wrap(m.t("npc.details.hint"), max(8, detailW-4)) {
				detail = append(detail, " "+dimStyle.Render(l))
			}
		}
		if detailW == rightW {
			detailH := min(12, midH-listH)
			right = lipgloss.JoinVertical(lipgloss.Left,
				ui.Panel(m.t("npc.offers"), strings.Join(offers, "\n"), rightW, midH-detailH),
				ui.Panel(m.t("npc.details"), strings.Join(detail, "\n"), rightW, detailH),
			)
		} else {
			leftPanel = ui.Panel(m.t("npc.details"), strings.Join(detail, "\n"), leftW, midH)
		}
	}

	mid := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, right)
	return mid + "\n" + journalPanel(m.t("npc.dialogue"), m.npcLog, w, logH)
}

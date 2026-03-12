package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"opal/internal/omiai"
	"opal/internal/store"
)

const (
	typingIdleTimeout  = 1500 * time.Millisecond
	typingRemoteWindow = 3 * time.Second
)

type screen int

const (
	screenAuth screen = iota
	screenLoading
	screenChat
)

type authMode int

const (
	authModeLogin authMode = iota
	authModeSignup
	authModeDirect
)

type Config struct {
	API            *omiai.APIClient
	APIURL         string
	SignalingURL   string
	Store          *store.Store
	InitialSession omiai.Session
}

type chatMessage struct {
	FromSelf    bool
	DisplayName string
	Body        string
	SentAt      time.Time
}

type connectResultMsg struct {
	Session omiai.Session
	Client  *omiai.SocketClient
	Err     error
}

type socketEventMsg struct {
	Event omiai.SocketEvent
	Open  bool
}

type sendResultMsg struct {
	Kind string
	Err  error
}

type tickMsg time.Time

type model struct {
	cfg Config

	screen    screen
	authMode  authMode
	authFocus int
	busy      bool
	status    string
	errorText string

	width  int
	height int

	session omiai.Session
	client  *omiai.SocketClient
	events  <-chan omiai.SocketEvent

	loginInputs  []textinput.Model
	signupInputs []textinput.Model
	directInputs []textinput.Model

	peerListFocused bool
	peers           []omiai.Peer
	selected        int

	composer textinput.Model
	viewport viewport.Model

	conversations map[string][]chatMessage
	unread        map[string]int
	typingUntil   map[string]time.Time

	typingActive bool
	typingPeerID string
	lastInputAt  time.Time

	styles styles
}

type styles struct {
	app            lipgloss.Style
	panel          lipgloss.Style
	header         lipgloss.Style
	subtle         lipgloss.Style
	muted          lipgloss.Style
	error          lipgloss.Style
	accent         lipgloss.Style
	selected       lipgloss.Style
	selfBubble     lipgloss.Style
	peerBubble     lipgloss.Style
	inputPanel     lipgloss.Style
	tab            lipgloss.Style
	tabActive      lipgloss.Style
	statusBar      lipgloss.Style
	peerSelected   lipgloss.Style
	peerUnselected lipgloss.Style
}

func NewModel(cfg Config) tea.Model {
	loginInputs := []textinput.Model{
		newInput("quicdial id", false),
		newInput("password", true),
	}
	signupInputs := []textinput.Model{
		newInput("display name", false),
		newInput("quicdial id (optional)", false),
		newInput("password", true),
	}
	directInputs := []textinput.Model{
		newInput("quicdial id", false),
	}

	loginInputs[0].Focus()

	composer := newInput("type a message", false)
	composer.Prompt = "> "
	composer.CharLimit = 512
	composer.Width = 64

	m := model{
		cfg:           cfg,
		screen:        screenAuth,
		authMode:      authModeLogin,
		loginInputs:   loginInputs,
		signupInputs:  signupInputs,
		directInputs:  directInputs,
		composer:      composer,
		viewport:      viewport.New(0, 0),
		conversations: make(map[string][]chatMessage),
		unread:        make(map[string]int),
		typingUntil:   make(map[string]time.Time),
		styles:        defaultStyles(),
	}
	m.setComposerFocus(false)

	if cfg.InitialSession.User.QuicdialID != "" {
		m.screen = screenLoading
		m.session = cfg.InitialSession
		m.status = fmt.Sprintf("Reconnecting as %s...", cfg.InitialSession.User.QuicdialID)
		m.busy = true
	}

	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, tickCmd()}
	if m.screen == screenLoading {
		cmds = append(cmds, connectSessionCmd(m.cfg, m.session))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
	case tickMsg:
		if cmd := m.handleTick(time.Time(msg)); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, tickCmd())
	case connectResultMsg:
		m.busy = false
		if msg.Err != nil {
			m.client = nil
			m.events = nil
			m.screen = screenAuth
			m.errorText = msg.Err.Error()
			m.status = ""
			return m, tea.Batch(cmds...)
		}

		m.session = msg.Session
		m.client = msg.Client
		m.events = msg.Client.Events()
		m.screen = screenChat
		m.errorText = ""
		m.status = fmt.Sprintf("Connected as %s", m.session.User.QuicdialID)
		m.setComposerFocus(false)
		if !m.session.Direct && m.session.Token != "" {
			if err := m.cfg.Store.Save(m.session); err != nil {
				m.status = fmt.Sprintf("Connected, but failed to save session: %v", err)
			}
		}
		cmds = append(cmds, waitForSocketEvent(m.events))
	case socketEventMsg:
		if m.screen != screenChat {
			break
		}
		if !msg.Open {
			m.client = nil
			m.events = nil
			m.status = "Disconnected from signaling server"
			break
		}

		switch event := msg.Event.(type) {
		case omiai.PeersEvent:
			m.applyPeers(event.Peers)
		case omiai.RelayEvent:
			m.applyRelay(event.Message)
		case omiai.ErrorEvent:
			m.client = nil
			m.events = nil
			m.errorText = event.Err.Error()
			m.status = "Disconnected from signaling server"
		}

		if m.events != nil {
			cmds = append(cmds, waitForSocketEvent(m.events))
		}
	case sendResultMsg:
		if m.screen != screenChat {
			break
		}
		if msg.Err != nil {
			m.errorText = msg.Err.Error()
			if msg.Kind == "chat" {
				m.status = "Message delivery failed"
			} else {
				m.status = "Typing update failed"
			}
		}
	case tea.KeyMsg:
		switch m.screen {
		case screenAuth, screenLoading:
			cmds = append(cmds, m.updateAuth(msg))
		case screenChat:
			cmds = append(cmds, m.updateChat(msg))
		}
	default:
		if m.screen == screenChat {
			if cmd := m.updateViewport(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	switch m.screen {
	case screenLoading:
		return m.styles.app.Padding(1, 2).Render(m.loadingView())
	case screenChat:
		return m.styles.app.Padding(1, 1).Render(m.chatView())
	default:
		return m.styles.app.Padding(1, 2).Render(m.authView())
	}
}

func (m *model) updateAuth(msg tea.KeyMsg) tea.Cmd {
	if key := msg.String(); key == "ctrl+c" || key == "q" {
		return tea.Quit
	}

	if m.busy {
		return nil
	}

	switch msg.String() {
	case "left", "h":
		m.shiftAuthMode(-1)
		return nil
	case "right", "l":
		m.shiftAuthMode(1)
		return nil
	case "tab", "down":
		m.shiftAuthFocus(1)
		return nil
	case "shift+tab", "up":
		m.shiftAuthFocus(-1)
		return nil
	case "enter":
		return m.submitAuth()
	}

	inputs := m.currentAuthInputs()
	if len(inputs) == 0 {
		return nil
	}

	var cmd tea.Cmd
	inputs[m.authFocus], cmd = inputs[m.authFocus].Update(msg)
	m.setCurrentAuthInputs(inputs)
	return cmd
}

func (m *model) updateChat(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.client != nil {
			_ = m.client.Close()
		}
		return tea.Quit
	case "ctrl+l":
		return m.logout()
	case "r":
		if !m.busy && m.client == nil && m.session.User.QuicdialID != "" {
			m.busy = true
			m.status = fmt.Sprintf("Reconnecting as %s...", m.session.User.QuicdialID)
			m.errorText = ""
			return connectSessionCmd(m.cfg, m.session)
		}
	case "tab":
		m.setComposerFocus(m.peerListFocused)
		return nil
	case "esc":
		m.setComposerFocus(false)
		return nil
	}

	if m.peerListFocused {
		switch msg.String() {
		case "up", "k":
			return m.moveSelection(-1)
		case "down", "j":
			return m.moveSelection(1)
		case "enter":
			m.setComposerFocus(true)
			return nil
		}
	}

	if !m.peerListFocused {
		switch msg.String() {
		case "enter":
			return m.sendCurrentMessage()
		case "pgup", "pgdown":
			return m.updateViewport(msg)
		}

		previous := m.composer.Value()
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		if m.composer.Value() != previous {
			return tea.Batch(cmd, m.handleComposerChange())
		}
		return cmd
	}

	return nil
}

func (m *model) updateViewport(msg tea.Msg) tea.Cmd {
	if m.viewport.Width == 0 || m.viewport.Height == 0 {
		return nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return cmd
}

func (m *model) handleTick(now time.Time) tea.Cmd {
	var cmds []tea.Cmd

	for peerID, until := range m.typingUntil {
		if now.After(until) {
			delete(m.typingUntil, peerID)
		}
	}

	if m.typingActive && now.Sub(m.lastInputAt) > typingIdleTimeout {
		cmds = append(cmds, m.stopTyping())
	}

	return tea.Batch(cmds...)
}

func (m *model) shiftAuthMode(delta int) {
	total := 3
	next := (int(m.authMode) + delta + total) % total
	m.authMode = authMode(next)
	m.authFocus = 0
	m.syncAuthFocus()
	m.errorText = ""
	m.status = ""
}

func (m *model) shiftAuthFocus(delta int) {
	inputs := m.currentAuthInputs()
	if len(inputs) == 0 {
		return
	}
	m.authFocus = (m.authFocus + delta + len(inputs)) % len(inputs)
	m.syncAuthFocus()
}

func (m *model) syncAuthFocus() {
	inputs := m.currentAuthInputs()
	for i := range inputs {
		if i == m.authFocus {
			inputs[i].Focus()
		} else {
			inputs[i].Blur()
		}
	}
	m.setCurrentAuthInputs(inputs)
}

func (m *model) submitAuth() tea.Cmd {
	m.errorText = ""
	m.status = ""
	m.busy = true

	switch m.authMode {
	case authModeSignup:
		inputs := m.signupInputs
		displayName := strings.TrimSpace(inputs[0].Value())
		quicdialID := strings.TrimSpace(inputs[1].Value())
		password := inputs[2].Value()
		if displayName == "" || password == "" {
			m.busy = false
			m.errorText = "Display name and password are required."
			return nil
		}
		m.status = "Creating account..."
		return signupAndConnectCmd(m.cfg, omiai.SignupRequest{
			DisplayName: displayName,
			QuicdialID:  quicdialID,
			Password:    password,
			AvatarID:    "kyu-kun",
		})
	case authModeDirect:
		quicdialID := strings.TrimSpace(m.directInputs[0].Value())
		if quicdialID == "" {
			m.busy = false
			m.errorText = "Quicdial ID is required."
			return nil
		}
		m.status = "Connecting in direct mode..."
		session := omiai.Session{
			DeviceID: store.GenerateDeviceID(),
			Direct:   true,
			User: omiai.User{
				QuicdialID:  quicdialID,
				DisplayName: quicdialID,
				AvatarID:    "default",
			},
		}
		return connectSessionCmd(m.cfg, session)
	default:
		inputs := m.loginInputs
		quicdialID := strings.TrimSpace(inputs[0].Value())
		password := inputs[1].Value()
		if quicdialID == "" || password == "" {
			m.busy = false
			m.errorText = "Quicdial ID and password are required."
			return nil
		}
		m.status = "Signing in..."
		return loginAndConnectCmd(m.cfg, quicdialID, password)
	}
}

func (m *model) logout() tea.Cmd {
	var clearErr string
	if m.client != nil {
		_ = m.client.Close()
	}
	if err := m.cfg.Store.Clear(); err != nil {
		clearErr = err.Error()
	}

	m.client = nil
	m.events = nil
	m.busy = false
	m.screen = screenAuth
	m.status = ""
	m.errorText = clearErr
	m.session = omiai.Session{}
	m.peers = nil
	m.selected = 0
	m.conversations = make(map[string][]chatMessage)
	m.unread = make(map[string]int)
	m.typingUntil = make(map[string]time.Time)
	m.typingActive = false
	m.typingPeerID = ""
	m.composer.SetValue("")
	m.setComposerFocus(false)
	m.authMode = authModeLogin
	m.authFocus = 0
	m.syncAuthFocus()

	return nil
}

func (m *model) moveSelection(delta int) tea.Cmd {
	if len(m.peers) == 0 {
		return nil
	}

	next := m.selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.peers) {
		next = len(m.peers) - 1
	}
	if next == m.selected {
		return nil
	}

	m.selected = next
	delete(m.unread, m.currentPeerID())
	m.rebuildViewport()
	return m.stopTyping()
}

func (m *model) sendCurrentMessage() tea.Cmd {
	if m.client == nil {
		return nil
	}

	peerID := m.currentPeerID()
	body := strings.TrimSpace(m.composer.Value())
	if peerID == "" || body == "" {
		return nil
	}

	displayName := m.displayNameForSelf()
	m.appendMessage(peerID, chatMessage{
		FromSelf:    true,
		DisplayName: displayName,
		Body:        body,
		SentAt:      time.Now(),
	})
	m.composer.SetValue("")

	chatCmd := sendChatCmd(m.client, peerID, body, displayName)
	stopCmd := m.stopTyping()
	return tea.Batch(chatCmd, stopCmd)
}

func (m *model) handleComposerChange() tea.Cmd {
	if m.client == nil {
		return nil
	}

	peerID := m.currentPeerID()
	if peerID == "" {
		return nil
	}

	current := strings.TrimSpace(m.composer.Value())
	m.lastInputAt = time.Now()

	if current == "" {
		return m.stopTyping()
	}

	if !m.typingActive || m.typingPeerID != peerID {
		m.typingActive = true
		m.typingPeerID = peerID
		return sendTypingCmd(m.client, peerID, m.displayNameForSelf(), true)
	}

	return nil
}

func (m *model) stopTyping() tea.Cmd {
	if m.client == nil || !m.typingActive || m.typingPeerID == "" {
		m.typingActive = false
		m.typingPeerID = ""
		return nil
	}

	peerID := m.typingPeerID
	m.typingActive = false
	m.typingPeerID = ""
	return sendTypingCmd(m.client, peerID, m.displayNameForSelf(), false)
}

func (m *model) applyPeers(peers []omiai.Peer) {
	currentID := m.currentPeerID()
	seen := make(map[string]omiai.Peer, len(peers))

	for _, peer := range peers {
		if peer.QuicdialID == m.session.User.QuicdialID {
			continue
		}
		peer.Online = true
		seen[peer.QuicdialID] = peer
	}

	for _, peer := range m.peers {
		if _, ok := seen[peer.QuicdialID]; ok {
			continue
		}
		if len(m.conversations[peer.QuicdialID]) == 0 && m.unread[peer.QuicdialID] == 0 {
			continue
		}
		peer.Online = false
		seen[peer.QuicdialID] = peer
	}

	next := make([]omiai.Peer, 0, len(seen))
	for _, peer := range seen {
		next = append(next, peer)
	}

	sortPeers(next)

	m.peers = next
	if len(m.peers) == 0 {
		m.selected = 0
		m.rebuildViewport()
		return
	}

	if currentID != "" {
		for i, peer := range m.peers {
			if peer.QuicdialID == currentID {
				m.selected = i
				m.rebuildViewport()
				return
			}
		}
	}

	if m.selected >= len(m.peers) {
		m.selected = len(m.peers) - 1
	}
	delete(m.unread, m.currentPeerID())
	m.rebuildViewport()
}

func (m *model) applyRelay(message omiai.RelayMessage) {
	peerID := strings.TrimSpace(message.FromQuicdialID)
	if peerID == "" {
		return
	}

	m.ensurePeer(peerID, message.DisplayName)

	switch message.Kind {
	case "typing":
		if message.Typing {
			m.typingUntil[peerID] = time.Now().Add(typingRemoteWindow)
		} else {
			delete(m.typingUntil, peerID)
		}
	case "chat":
		when := time.Now()
		if message.SentAt > 0 {
			when = time.UnixMilli(message.SentAt)
		}
		m.appendMessage(peerID, chatMessage{
			FromSelf:    false,
			DisplayName: message.DisplayName,
			Body:        message.Body,
			SentAt:      when,
		})
		if peerID != m.currentPeerID() {
			m.unread[peerID]++
		}
		delete(m.typingUntil, peerID)
	default:
	}
}

func (m *model) ensurePeer(peerID, name string) {
	for i := range m.peers {
		if m.peers[i].QuicdialID == peerID {
			if name != "" {
				m.peers[i].DisplayName = name
			}
			m.peers[i].Online = true
			return
		}
	}

	m.peers = append(m.peers, omiai.Peer{
		QuicdialID:  peerID,
		DisplayName: fallback(name, peerID),
		AvatarID:    "default",
		Online:      true,
	})
	sortPeers(m.peers)
	if len(m.peers) == 1 {
		m.selected = 0
	}
}

func (m *model) appendMessage(peerID string, message chatMessage) {
	if message.DisplayName == "" {
		if message.FromSelf {
			message.DisplayName = m.displayNameForSelf()
		} else {
			message.DisplayName = peerID
		}
	}
	m.conversations[peerID] = append(m.conversations[peerID], message)
	if peerID == m.currentPeerID() {
		m.rebuildViewport()
	}
}

func (m *model) rebuildViewport() {
	if m.viewport.Width <= 0 {
		return
	}

	peerID := m.currentPeerID()
	if peerID == "" {
		m.viewport.SetContent(m.styles.subtle.Render("No peer selected.\n\nUse the left panel to choose someone online."))
		return
	}

	messages := m.conversations[peerID]
	if len(messages) == 0 {
		name := m.currentPeerName()
		m.viewport.SetContent(m.styles.subtle.Render(fmt.Sprintf("No messages with %s yet.\n\nStart the conversation below.", name)))
		return
	}

	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		label := message.DisplayName
		if message.FromSelf {
			label = "You"
		}
		meta := m.styles.muted.Render(fmt.Sprintf("%s  %s", label, message.SentAt.Format("15:04")))
		bubble := m.styles.peerBubble
		if message.FromSelf {
			bubble = m.styles.selfBubble
		}
		body := bubble.MaxWidth(maxInt(20, m.viewport.Width-4)).Render(message.Body)
		parts = append(parts, lipgloss.JoinVertical(lipgloss.Left, meta, body))
	}

	m.viewport.SetContent(strings.Join(parts, "\n\n"))
	m.viewport.GotoBottom()
}

func (m *model) resizeViewport() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	leftWidth := m.peerListWidth()
	rightWidth := maxInt(24, m.width-leftWidth-5)
	bodyHeight := maxInt(8, m.height-11)

	m.viewport.Width = rightWidth - 4
	m.viewport.Height = bodyHeight - 2
	m.composer.Width = rightWidth - 8
	m.rebuildViewport()
}

func (m *model) authView() string {
	modeTabs := []string{
		m.renderTab("Login", m.authMode == authModeLogin),
		m.renderTab("Signup", m.authMode == authModeSignup),
		m.renderTab("Direct", m.authMode == authModeDirect),
	}

	header := m.styles.header.Render("Opal") + "\n" +
		m.styles.subtle.Render("Charm-powered Omiai peer chat for server-relayed messaging and typing presence.") + "\n" +
		m.styles.muted.Render(fmt.Sprintf("API %s | Signal %s", m.cfg.APIURL, m.cfg.SignalingURL))

	form := []string{lipgloss.JoinHorizontal(lipgloss.Top, modeTabs...)}
	labels := m.currentAuthLabels()
	for i, input := range m.currentAuthInputs() {
		form = append(form, m.styles.muted.Render(labels[i]))
		form = append(form, input.View())
	}

	help := m.styles.subtle.Render("left/right switch mode | tab move field | enter submit | q quit")

	if m.status != "" {
		form = append(form, m.styles.statusBar.Render(m.status))
	}
	if m.errorText != "" {
		form = append(form, m.styles.error.Render(m.errorText))
	}
	form = append(form, help)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.styles.panel.Width(maxInt(52, minInt(80, m.width-6))).Render(strings.Join(form, "\n")))
}

func (m *model) loadingView() string {
	lines := []string{
		m.styles.header.Render("Opal"),
		"",
		m.styles.panel.Width(maxInt(42, minInt(72, m.width-6))).Render(
			m.styles.accent.Render("Connecting...") + "\n" +
				m.styles.subtle.Render(m.status),
		),
	}
	if m.errorText != "" {
		lines = append(lines, m.styles.error.Render(m.errorText))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *model) chatView() string {
	header := m.renderHeader()
	body := m.renderBody()
	typing := m.renderTypingLine()
	input := m.renderInput()
	footer := m.styles.subtle.Render("tab focus | enter send/select | ctrl+l logout | r reconnect | q quit")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, typing, input, footer)
}

func (m *model) renderHeader() string {
	title := m.styles.header.Render("Opal")
	identity := m.styles.accent.Render(fmt.Sprintf("%s (%s)", m.displayNameForSelf(), m.session.User.QuicdialID))
	status := m.styles.subtle.Render(m.status)
	if m.client == nil && m.screen == screenChat {
		status = m.styles.error.Render("offline")
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", identity),
		status,
	)
}

func (m *model) renderBody() string {
	leftWidth := m.peerListWidth()
	rightWidth := maxInt(24, m.width-leftWidth-5)
	bodyHeight := maxInt(8, m.height-11)

	peersPanel := m.styles.panel.Width(leftWidth).Height(bodyHeight).Render(m.renderPeerList(leftWidth - 4))
	messagesPanel := m.styles.panel.Width(rightWidth).Height(bodyHeight).Render(m.viewport.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, peersPanel, " ", messagesPanel)
}

func (m *model) renderPeerList(width int) string {
	if len(m.peers) == 0 {
		return m.styles.subtle.Render("No peers online yet.")
	}

	lines := make([]string, 0, len(m.peers))
	for i, peer := range m.peers {
		name := displayName(peer)
		status := "online"
		if !peer.Online {
			status = "offline"
		}
		line := fmt.Sprintf("%s  [%s]", name, status)
		if unread := m.unread[peer.QuicdialID]; unread > 0 {
			line = fmt.Sprintf("%s  (%d new)", line, unread)
		}

		style := m.styles.peerUnselected
		if i == m.selected {
			style = m.styles.peerSelected
		}
		lines = append(lines, style.Width(width).Render(line))
	}

	title := m.styles.muted.Render("Peers")
	if m.peerListFocused {
		title = m.styles.accent.Render("Peers")
	}
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{title, ""}, lines...)...)
}

func (m *model) renderTypingLine() string {
	peerID := m.currentPeerID()
	if peerID == "" {
		return m.styles.subtle.Render("Select a peer to start chatting.")
	}

	if until, ok := m.typingUntil[peerID]; ok && time.Now().Before(until) {
		return m.styles.accent.Render(fmt.Sprintf("%s is typing...", m.currentPeerName()))
	}

	if m.errorText != "" {
		return m.styles.error.Render(m.errorText)
	}

	return m.styles.subtle.Render(fmt.Sprintf("Chatting with %s", m.currentPeerName()))
}

func (m *model) renderInput() string {
	title := "Compose"
	if !m.peerListFocused {
		title = "Compose (focused)"
	}

	body := m.composer.View()
	if m.currentPeerID() == "" {
		body = m.styles.subtle.Render("No peer selected.")
	}

	return m.styles.inputPanel.Width(maxInt(28, m.width-2)).Render(
		m.styles.muted.Render(title) + "\n" + body,
	)
}

func (m *model) currentAuthInputs() []textinput.Model {
	switch m.authMode {
	case authModeSignup:
		return m.signupInputs
	case authModeDirect:
		return m.directInputs
	default:
		return m.loginInputs
	}
}

func (m *model) setCurrentAuthInputs(inputs []textinput.Model) {
	switch m.authMode {
	case authModeSignup:
		m.signupInputs = inputs
	case authModeDirect:
		m.directInputs = inputs
	default:
		m.loginInputs = inputs
	}
}

func (m model) currentAuthLabels() []string {
	switch m.authMode {
	case authModeSignup:
		return []string{"Display name", "Quicdial ID", "Password"}
	case authModeDirect:
		return []string{"Quicdial ID"}
	default:
		return []string{"Quicdial ID", "Password"}
	}
}

func (m *model) currentPeerID() string {
	if m.selected < 0 || m.selected >= len(m.peers) {
		return ""
	}
	return m.peers[m.selected].QuicdialID
}

func (m *model) currentPeerName() string {
	if m.selected < 0 || m.selected >= len(m.peers) {
		return "nobody"
	}
	return displayName(m.peers[m.selected])
}

func (m *model) displayNameForSelf() string {
	return fallback(m.session.User.DisplayName, m.session.User.QuicdialID)
}

func (m *model) setComposerFocus(focused bool) {
	m.peerListFocused = !focused
	if focused {
		m.composer.Focus()
	} else {
		m.composer.Blur()
	}
}

func (m model) renderTab(label string, active bool) string {
	style := m.styles.tab
	if active {
		style = m.styles.tabActive
	}
	return style.Render(label)
}

func connectSessionCmd(cfg Config, session omiai.Session) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: cfg.SignalingURL,
			Session:      session,
		})
		return connectResultMsg{
			Session: session,
			Client:  client,
			Err:     err,
		}
	}
}

func loginAndConnectCmd(cfg Config, quicdialID, password string) tea.Cmd {
	return func() tea.Msg {
		deviceID := store.GenerateDeviceID()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := cfg.API.Login(ctx, quicdialID, password, deviceID)
		if err != nil {
			return connectResultMsg{Err: err}
		}

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: cfg.SignalingURL,
			Session:      session,
		})
		return connectResultMsg{
			Session: session,
			Client:  client,
			Err:     err,
		}
	}
}

func signupAndConnectCmd(cfg Config, req omiai.SignupRequest) tea.Cmd {
	return func() tea.Msg {
		deviceID := store.GenerateDeviceID()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := cfg.API.Signup(ctx, req, deviceID)
		if err != nil {
			return connectResultMsg{Err: err}
		}

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: cfg.SignalingURL,
			Session:      session,
		})
		return connectResultMsg{
			Session: session,
			Client:  client,
			Err:     err,
		}
	}
}

func waitForSocketEvent(events <-chan omiai.SocketEvent) tea.Cmd {
	if events == nil {
		return nil
	}

	return func() tea.Msg {
		event, ok := <-events
		return socketEventMsg{
			Event: event,
			Open:  ok,
		}
	}
}

func sendChatCmd(client *omiai.SocketClient, peerID, body, displayName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := client.SendChat(ctx, peerID, body, displayName)
		return sendResultMsg{Kind: "chat", Err: err}
	}
}

func sendTypingCmd(client *omiai.SocketClient, peerID, displayName string, typing bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := client.SendTyping(ctx, peerID, displayName, typing)
		return sendResultMsg{Kind: "typing", Err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func newInput(placeholder string, password bool) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 256
	input.Width = 32
	if password {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '*'
	}
	return input
}

func defaultStyles() styles {
	return styles{
		app:            lipgloss.NewStyle().Foreground(lipgloss.Color("#f5efe6")).Background(lipgloss.Color("#0f1419")),
		panel:          lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#4f6d7a")).Padding(1),
		header:         lipgloss.NewStyle().Foreground(lipgloss.Color("#f7b267")).Bold(true),
		subtle:         lipgloss.NewStyle().Foreground(lipgloss.Color("#cbd5df")),
		muted:          lipgloss.NewStyle().Foreground(lipgloss.Color("#89a6b1")),
		error:          lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6b6b")).Bold(true),
		accent:         lipgloss.NewStyle().Foreground(lipgloss.Color("#9fd356")).Bold(true),
		selected:       lipgloss.NewStyle().Foreground(lipgloss.Color("#0f1419")).Background(lipgloss.Color("#f7b267")),
		selfBubble:     lipgloss.NewStyle().Foreground(lipgloss.Color("#0f1419")).Background(lipgloss.Color("#f7b267")).Padding(0, 1),
		peerBubble:     lipgloss.NewStyle().Foreground(lipgloss.Color("#f5efe6")).Background(lipgloss.Color("#20313f")).Padding(0, 1),
		inputPanel:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#9fd356")).Padding(1),
		tab:            lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("#89a6b1")),
		tabActive:      lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("#0f1419")).Background(lipgloss.Color("#f7b267")).Bold(true),
		statusBar:      lipgloss.NewStyle().Foreground(lipgloss.Color("#0f1419")).Background(lipgloss.Color("#9fd356")).Padding(0, 1),
		peerSelected:   lipgloss.NewStyle().Foreground(lipgloss.Color("#0f1419")).Background(lipgloss.Color("#9fd356")).Padding(0, 1),
		peerUnselected: lipgloss.NewStyle().Foreground(lipgloss.Color("#f5efe6")).Padding(0, 1),
	}
}

func displayName(peer omiai.Peer) string {
	return fallback(peer.DisplayName, peer.QuicdialID)
}

func fallback(value, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sortPeers(peers []omiai.Peer) {
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		left := strings.ToLower(displayName(peers[i]))
		right := strings.ToLower(displayName(peers[j]))
		if left == right {
			return peers[i].QuicdialID < peers[j].QuicdialID
		}
		return left < right
	})
}

func (m model) peerListWidth() int {
	return maxInt(22, minInt(32, m.width/4))
}

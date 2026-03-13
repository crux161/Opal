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
	"opal/internal/social"
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
	ServerHost     string
	Store          *store.Store
	InitialSession omiai.Session
}

type chatMessage struct {
	ID          string
	FromSelf    bool
	DisplayName string
	Body        string
	SentAt      time.Time
	Receipt     string
	Secure      bool
	Intro       bool
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

type relationshipsResultMsg struct {
	Friends []omiai.Friend
	Pending []omiai.PendingFriendRequest
	Err     error
}

type messageDispatchMsg struct {
	Kind         string
	PeerID       string
	MessageID    string
	FriendshipID string
	Err          error
}

type friendMutationMsg struct {
	Kind         string
	PeerID       string
	FriendshipID string
	Err          error
}

type tickMsg time.Time

type dialogKind string

const (
	dialogKindFriendRequest dialogKind = "friend_request"
	dialogKindFriendIntro   dialogKind = "friend_intro"
)

type contactDialog struct {
	Kind         dialogKind
	PeerID       string
	DisplayName  string
	Message      string
	PublicKey    string
	FriendshipID string
	MessageID    string
	Ciphertext   string
}

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
	serverInput  textinput.Model

	peerListFocused bool
	peers           []omiai.Peer
	selected        int
	dialing         bool

	dialInput textinput.Model
	composer  textinput.Model
	viewport  viewport.Model

	conversations map[string][]chatMessage
	unread        map[string]int
	typingUntil   map[string]time.Time

	trust           social.TrustState
	friends         map[string]omiai.Friend
	pendingIncoming map[string]omiai.PendingFriendRequest
	pendingOutgoing map[string]bool
	activeDialog    *contactDialog
	dialogQueue     []contactDialog

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
	modal          lipgloss.Style
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
	dialInput := newInput("peer quicdial code", false)
	dialInput.Prompt = "dial> "
	dialInput.CharLimit = 256
	serverInput := newInput("10.10.10.10", false)
	serverInput.Prompt = ""
	serverInput.CharLimit = 64
	serverInput.SetValue(omiai.NormalizeServerHost(cfg.ServerHost))

	m := model{
		cfg:             cfg,
		screen:          screenAuth,
		authMode:        authModeLogin,
		loginInputs:     loginInputs,
		signupInputs:    signupInputs,
		directInputs:    directInputs,
		serverInput:     serverInput,
		dialInput:       dialInput,
		composer:        composer,
		viewport:        viewport.New(0, 0),
		conversations:   make(map[string][]chatMessage),
		unread:          make(map[string]int),
		typingUntil:     make(map[string]time.Time),
		friends:         make(map[string]omiai.Friend),
		pendingIncoming: make(map[string]omiai.PendingFriendRequest),
		pendingOutgoing: make(map[string]bool),
		styles:          defaultStyles(),
	}
	m.setComposerFocus(false)
	m.dialInput.Blur()
	if trustState, err := cfg.Store.EnsureTrustState(); err == nil {
		m.trust = trustState
	} else {
		m.errorText = err.Error()
	}

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
		if m.session.ServerHost == "" {
			m.session.ServerHost = m.cfg.ServerHost
		}
		m.applyServerHost(m.session.ServerHost)
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
			cmds = append(cmds, syncRelationshipsCmd(m.cfg, m.session))
		}
		cmds = append(cmds, waitForSocketEvent(m.events))
	case relationshipsResultMsg:
		if msg.Err != nil {
			m.errorText = msg.Err.Error()
			break
		}
		m.applyRelationships(msg.Friends, msg.Pending)
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
			if cmd := m.applyRelay(event.Message); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case omiai.PushEvent:
			if cmd := m.handlePushEvent(event); cmd != nil {
				cmds = append(cmds, cmd)
			}
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
	case messageDispatchMsg:
		if m.screen != screenChat {
			break
		}
		if msg.Err != nil {
			m.updateMessageReceipt(msg.PeerID, msg.MessageID, "failed")
			m.errorText = msg.Err.Error()
			break
		}
		if msg.Kind == "intro" && m.session.Token != "" && !m.isFriend(msg.PeerID) {
			m.pendingOutgoing[msg.PeerID] = true
		}
	case friendMutationMsg:
		if msg.Err != nil {
			m.errorText = msg.Err.Error()
			break
		}
		switch msg.Kind {
		case "friend_request":
			m.pendingOutgoing[msg.PeerID] = true
			m.status = fmt.Sprintf("Friend request sent to %s", msg.PeerID)
		case "friend_accept":
			m.status = fmt.Sprintf("Friend request accepted for %s", msg.PeerID)
		case "friend_decline":
			m.status = fmt.Sprintf("Friend request declined for %s", msg.PeerID)
		case "friend_remove":
			delete(m.friends, msg.PeerID)
			delete(m.pendingIncoming, msg.PeerID)
			delete(m.pendingOutgoing, msg.PeerID)
			_ = m.forgetPeer(msg.PeerID)
			m.status = fmt.Sprintf("Removed %s from friends", msg.PeerID)
		}
		if m.session.Token != "" {
			cmds = append(cmds, syncRelationshipsCmd(m.cfg, m.session))
		}
	case dialResultMsg:
		m.closeDialPrompt()
		m.applyDialResult(msg.Code, msg.Result, msg.Err)
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

	if m.authFocus >= len(inputs) {
		var cmd tea.Cmd
		m.serverInput, cmd = m.serverInput.Update(msg)
		return cmd
	}

	var cmd tea.Cmd
	inputs[m.authFocus], cmd = inputs[m.authFocus].Update(msg)
	m.setCurrentAuthInputs(inputs)
	return cmd
}

func (m *model) updateChat(msg tea.KeyMsg) tea.Cmd {
	if m.activeDialog != nil {
		switch msg.String() {
		case "y", "a", "enter":
			return m.acceptActiveDialog()
		case "n", "d", "esc":
			return m.rejectActiveDialog()
		case "ctrl+c", "q":
			if m.client != nil {
				_ = m.client.Close()
			}
			return tea.Quit
		default:
			return nil
		}
	}

	if m.dialing {
		switch msg.String() {
		case "esc":
			m.closeDialPrompt()
			return nil
		case "enter":
			return m.submitDial()
		case "ctrl+c", "q":
			if m.client != nil {
				_ = m.client.Close()
			}
			return tea.Quit
		}

		var cmd tea.Cmd
		m.dialInput, cmd = m.dialInput.Update(msg)
		return cmd
	}

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
	case "f":
		if peerID := m.currentPeerID(); peerID != "" && !m.isFriend(peerID) && !m.pendingOutgoing[peerID] {
			return sendFriendRequestCmd(m.cfg, m.session, peerID)
		}
	case "u":
		if peerID := m.currentPeerID(); peerID != "" && m.isFriend(peerID) {
			return removeFriendCmd(m.cfg, m.session, peerID)
		}
	case "d":
		m.openDialPrompt()
		return nil
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
	total := len(inputs) + 1
	m.authFocus = (m.authFocus + delta + total) % total
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
	if m.authFocus == len(inputs) {
		m.serverInput.Focus()
	} else {
		m.serverInput.Blur()
	}
	m.setCurrentAuthInputs(inputs)
}

func (m *model) submitAuth() tea.Cmd {
	m.errorText = ""
	m.status = ""
	m.busy = true

	serverHost := strings.TrimSpace(m.serverInput.Value())
	if err := omiai.ValidateServerHost(serverHost); err != nil {
		m.busy = false
		m.errorText = err.Error()
		return nil
	}
	m.applyServerHost(serverHost)

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
			DeviceID:   store.GenerateDeviceID(),
			Direct:     true,
			ServerHost: m.cfg.ServerHost,
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
	m.friends = make(map[string]omiai.Friend)
	m.pendingIncoming = make(map[string]omiai.PendingFriendRequest)
	m.pendingOutgoing = make(map[string]bool)
	m.activeDialog = nil
	m.dialogQueue = nil
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
	messageID := social.RandomID()
	entry := chatMessage{
		ID:          messageID,
		FromSelf:    true,
		DisplayName: displayName,
		Body:        body,
		SentAt:      time.Now(),
		Receipt:     "sent",
	}

	var sendCmd tea.Cmd
	if trustedKey := m.trustedPublicKey(peerID); trustedKey != "" {
		ciphertext, err := social.Encrypt(m.trust.Identity, trustedKey, body)
		if err != nil {
			m.errorText = err.Error()
			return nil
		}
		entry.Secure = true
		sendCmd = sendSecureMessageCmd(m.client, m.session, peerID, ciphertext, m.trust.Identity.PublicKey, messageID)
	} else {
		entry.Intro = true
		entry.Receipt = "awaiting trust"
		sendCmd = sendIntroMessageCmd(m.cfg, m.client, m.session, peerID, body, m.trust.Identity.PublicKey, messageID)
	}

	m.appendMessage(peerID, chatMessage{
		ID:          entry.ID,
		FromSelf:    entry.FromSelf,
		DisplayName: entry.DisplayName,
		Body:        entry.Body,
		SentAt:      entry.SentAt,
		Receipt:     entry.Receipt,
		Secure:      entry.Secure,
		Intro:       entry.Intro,
	})
	m.composer.SetValue("")

	stopCmd := m.stopTyping()
	return tea.Batch(sendCmd, stopCmd)
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

func (m *model) applyRelay(message omiai.RelayMessage) tea.Cmd {
	peerID := strings.TrimSpace(message.FromQuicdialID)
	if peerID == "" {
		return nil
	}

	m.ensurePeer(peerID, message.DisplayName)

	switch message.Kind {
	case "typing":
		if message.Typing {
			m.typingUntil[peerID] = time.Now().Add(typingRemoteWindow)
		} else {
			delete(m.typingUntil, peerID)
		}
		return nil
	case "chat":
		when := time.Now()
		if message.SentAt > 0 {
			when = time.UnixMilli(message.SentAt)
		}
		m.appendMessage(peerID, chatMessage{
			ID:          message.MessageID,
			FromSelf:    false,
			DisplayName: message.DisplayName,
			Body:        message.Body,
			SentAt:      when,
			Receipt:     "read",
		})
		if peerID != m.currentPeerID() {
			m.unread[peerID]++
		}
		delete(m.typingUntil, peerID)
		return nil
	case "friend_intro":
		m.queueDialog(contactDialog{
			Kind:         dialogKindFriendIntro,
			PeerID:       peerID,
			DisplayName:  fallback(message.DisplayName, peerID),
			Message:      message.Body,
			PublicKey:    message.PublicFriendKey,
			FriendshipID: fallback(message.FriendshipID, m.pendingIncoming[peerID].FriendshipID),
			MessageID:    message.MessageID,
		})
		m.status = fmt.Sprintf("First contact from %s is waiting for your decision", fallback(message.DisplayName, peerID))
		return nil
	case "secure_chat":
		publicKey := message.PublicFriendKey
		if trustedKey := m.trustedPublicKey(peerID); trustedKey != "" {
			publicKey = trustedKey
		}
		if trustedKey := m.trustedPublicKey(peerID); trustedKey == "" {
			m.queueDialog(contactDialog{
				Kind:         dialogKindFriendIntro,
				PeerID:       peerID,
				DisplayName:  fallback(message.DisplayName, peerID),
				PublicKey:    message.PublicFriendKey,
				MessageID:    message.MessageID,
				Ciphertext:   message.Ciphertext,
				FriendshipID: m.pendingIncoming[peerID].FriendshipID,
			})
			m.status = fmt.Sprintf("%s sent an encrypted contact message", fallback(message.DisplayName, peerID))
			return nil
		}
		body, err := social.Decrypt(m.trust.Identity, publicKey, message.Ciphertext)
		if err != nil {
			m.errorText = err.Error()
			return nil
		}
		when := time.Now()
		if message.SentAt > 0 {
			when = time.UnixMilli(message.SentAt)
		}
		m.appendMessage(peerID, chatMessage{
			ID:          message.MessageID,
			FromSelf:    false,
			DisplayName: fallback(message.DisplayName, peerID),
			Body:        body,
			SentAt:      when,
			Receipt:     "read",
			Secure:      true,
		})
		if peerID != m.currentPeerID() {
			m.unread[peerID]++
		}
		delete(m.typingUntil, peerID)
		return sendReceiptCmd(m.client, m.session, peerID, message.MessageID)
	case "friend_ack":
		if message.Status == "accepted" {
			if err := m.trustPeer(peerID, message.PublicFriendKey); err != nil {
				m.errorText = err.Error()
			}
			m.updateMessageReceipt(peerID, message.ReceiptMessageID, "read")
			m.status = fmt.Sprintf("%s trusted your friend key", fallback(message.DisplayName, peerID))
		} else {
			m.updateMessageReceipt(peerID, message.ReceiptMessageID, "rejected")
			m.status = fmt.Sprintf("%s rejected first contact", fallback(message.DisplayName, peerID))
		}
		return nil
	case "receipt":
		m.updateMessageReceipt(peerID, message.ReceiptMessageID, "read")
		return nil
	default:
		return nil
	}
}

func (m *model) ensurePeer(peerID, name string) *omiai.Peer {
	for i := range m.peers {
		if m.peers[i].QuicdialID == peerID {
			if name != "" {
				m.peers[i].DisplayName = name
			}
			m.peers[i].Online = true
			return &m.peers[i]
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
	for i := range m.peers {
		if m.peers[i].QuicdialID == peerID {
			return &m.peers[i]
		}
	}
	return nil
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
		metaParts := []string{label, message.SentAt.Format("15:04")}
		if message.Secure {
			metaParts = append(metaParts, "secure")
		}
		if message.Intro {
			metaParts = append(metaParts, "intro")
		}
		if message.FromSelf && message.Receipt != "" {
			metaParts = append(metaParts, message.Receipt)
		}
		meta := m.styles.muted.Render(strings.Join(metaParts, "  "))
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
	form = append(form, "")
	form = append(form, m.styles.accent.Render("Connection Settings"))
	form = append(form, m.styles.muted.Render("Omiai Server IP"))
	form = append(form, m.serverInput.View())
	form = append(form, m.styles.subtle.Render("Opal derives http://<ip>:8000 and ws://<ip>:4000/ws/sankaku/websocket automatically."))

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
	footer := m.styles.subtle.Render("tab focus | d dial code | enter send/select | f add friend | u remove friend | ctrl+l logout | r reconnect | q quit")
	connection := m.styles.muted.Render(m.connectionStatusLine())
	base := lipgloss.JoinVertical(lipgloss.Left, header, body, typing, input, footer, connection)
	if m.activeDialog != nil {
		return lipgloss.JoinVertical(lipgloss.Left, base, "", m.renderDialog())
	}
	if m.dialing {
		return lipgloss.JoinVertical(lipgloss.Left, base, "", m.renderDialPrompt())
	}
	return base
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
		m.styles.subtle.Render(m.selectedPeerSummary()),
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
		line := fmt.Sprintf("%s  %s", displayName(peer), m.peerTags(peer))
		if unread := m.unread[peer.QuicdialID]; unread > 0 {
			line = fmt.Sprintf("%s  (%d new)", line, unread)
		}
		subline := m.styles.muted.Render(peer.QuicdialID)

		style := m.styles.peerUnselected
		if i == m.selected {
			style = m.styles.peerSelected
		}
		lines = append(lines, style.Width(width).Render(line+"\n"+subline))
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

	return m.styles.subtle.Render(fmt.Sprintf("Chatting with %s | %s", m.currentPeerName(), m.currentPeerStatus()))
}

func (m *model) renderInput() string {
	title := "Compose"
	if !m.peerListFocused {
		title = "Compose (focused)"
	}

	body := m.composer.View()
	if m.currentPeerID() == "" {
		body = m.styles.subtle.Render("No peer selected.")
	} else if !m.isTrusted(m.currentPeerID()) {
		body = body + "\n" + m.styles.subtle.Render("First send shares your public friend key and waits for receiver approval.")
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

func (m *model) applyServerHost(host string) {
	host = omiai.NormalizeServerHost(host)
	apiURL, signalingURL := omiai.EndpointsForServerHost(host)
	m.cfg.ServerHost = host
	m.cfg.APIURL = apiURL
	m.cfg.SignalingURL = signalingURL
	m.cfg.API = omiai.NewAPIClient(apiURL)
	m.serverInput.SetValue(host)
}

func (m *model) connectionStatusLine() string {
	host := m.session.ServerHost
	if host == "" {
		host = m.cfg.ServerHost
	}
	if host == "" {
		host = omiai.ServerHostFromEndpoint(m.cfg.SignalingURL)
	}
	return fmt.Sprintf("Connected Omiai: %s | %s", host, time.Now().Format("2006-01-02 15:04 MST"))
}

func connectSessionCmd(cfg Config, session omiai.Session) tea.Cmd {
	return func() tea.Msg {
		serverHost := session.ServerHost
		if serverHost == "" {
			serverHost = cfg.ServerHost
		}
		_, signalingURL := omiai.EndpointsForServerHost(serverHost)
		session.ServerHost = serverHost

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: signalingURL,
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
		serverHost := omiai.NormalizeServerHost(cfg.ServerHost)
		apiURL, signalingURL := omiai.EndpointsForServerHost(serverHost)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := omiai.NewAPIClient(apiURL).Login(ctx, quicdialID, password, deviceID)
		if err != nil {
			return connectResultMsg{Err: err}
		}
		session.ServerHost = serverHost

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: signalingURL,
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
		serverHost := omiai.NormalizeServerHost(cfg.ServerHost)
		apiURL, signalingURL := omiai.EndpointsForServerHost(serverHost)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := omiai.NewAPIClient(apiURL).Signup(ctx, req, deviceID)
		if err != nil {
			return connectResultMsg{Err: err}
		}
		session.ServerHost = serverHost

		client, err := omiai.Dial(ctx, omiai.SocketConfig{
			SignalingURL: signalingURL,
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
		modal:          lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#f7b267")).Background(lipgloss.Color("#16202a")).Padding(1, 2),
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

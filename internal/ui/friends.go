package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"opal/internal/omiai"
	"opal/internal/social"
)

func syncRelationshipsCmd(cfg Config, session omiai.Session) tea.Cmd {
	if session.Token == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		friends, err := cfg.API.ListFriends(ctx, session.Token)
		if err != nil {
			return relationshipsResultMsg{Err: err}
		}

		pending, err := cfg.API.ListPendingRequests(ctx, session.Token)
		if err != nil {
			return relationshipsResultMsg{Err: err}
		}

		return relationshipsResultMsg{
			Friends: friends,
			Pending: pending,
		}
	}
}

func sendIntroMessageCmd(cfg Config, client *omiai.SocketClient, session omiai.Session, peerID, body, publicKey, messageID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		friendshipID := ""
		if session.Token != "" {
			response, err := cfg.API.SendFriendRequest(ctx, session.Token, peerID)
			if err == nil {
				friendshipID = response.FriendshipID
			} else if !isNonFatalFriendError(err) {
				return messageDispatchMsg{Kind: "intro", PeerID: peerID, MessageID: messageID, Err: err}
			}
		}

		err := client.SendRelay(ctx, omiai.RelayMessage{
			Kind:            "friend_intro",
			ToQuicdialID:    peerID,
			DisplayName:     session.User.DisplayName,
			Body:            body,
			PublicFriendKey: publicKey,
			MessageID:       messageID,
			FriendshipID:    friendshipID,
		})

		return messageDispatchMsg{
			Kind:         "intro",
			PeerID:       peerID,
			MessageID:    messageID,
			FriendshipID: friendshipID,
			Err:          err,
		}
	}
}

func sendSecureMessageCmd(client *omiai.SocketClient, session omiai.Session, peerID, ciphertext, publicKey, messageID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.SendRelay(ctx, omiai.RelayMessage{
			Kind:            "secure_chat",
			ToQuicdialID:    peerID,
			DisplayName:     session.User.DisplayName,
			Ciphertext:      ciphertext,
			PublicFriendKey: publicKey,
			MessageID:       messageID,
		})

		return messageDispatchMsg{
			Kind:      "secure",
			PeerID:    peerID,
			MessageID: messageID,
			Err:       err,
		}
	}
}

func sendFriendAckCmd(client *omiai.SocketClient, session omiai.Session, peerID, receiptMessageID, status, publicKey, reason string) tea.Cmd {
	if client == nil || peerID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.SendRelay(ctx, omiai.RelayMessage{
			Kind:             "friend_ack",
			ToQuicdialID:     peerID,
			DisplayName:      session.User.DisplayName,
			MessageID:        social.RandomID(),
			ReceiptMessageID: receiptMessageID,
			PublicFriendKey:  publicKey,
			Status:           status,
			Reason:           reason,
		})

		return messageDispatchMsg{
			Kind:      "friend_ack",
			PeerID:    peerID,
			MessageID: receiptMessageID,
			Err:       err,
		}
	}
}

func sendReceiptCmd(client *omiai.SocketClient, session omiai.Session, peerID, receiptMessageID string) tea.Cmd {
	if client == nil || peerID == "" || receiptMessageID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := client.SendRelay(ctx, omiai.RelayMessage{
			Kind:             "receipt",
			ToQuicdialID:     peerID,
			DisplayName:      session.User.DisplayName,
			MessageID:        social.RandomID(),
			ReceiptMessageID: receiptMessageID,
		})

		return messageDispatchMsg{
			Kind:      "receipt",
			PeerID:    peerID,
			MessageID: receiptMessageID,
			Err:       err,
		}
	}
}

func sendFriendRequestCmd(cfg Config, session omiai.Session, peerID string) tea.Cmd {
	if session.Token == "" || peerID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		response, err := cfg.API.SendFriendRequest(ctx, session.Token, peerID)
		if err != nil && !isNonFatalFriendError(err) {
			return friendMutationMsg{Kind: "friend_request", PeerID: peerID, Err: err}
		}

		return friendMutationMsg{
			Kind:         "friend_request",
			PeerID:       peerID,
			FriendshipID: response.FriendshipID,
		}
	}
}

func acceptFriendRequestCmd(cfg Config, session omiai.Session, friendshipID, peerID string) tea.Cmd {
	if session.Token == "" || friendshipID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := cfg.API.AcceptFriendRequest(ctx, session.Token, friendshipID)
		return friendMutationMsg{
			Kind:         "friend_accept",
			PeerID:       peerID,
			FriendshipID: friendshipID,
			Err:          err,
		}
	}
}

func declineFriendRequestCmd(cfg Config, session omiai.Session, friendshipID, peerID string) tea.Cmd {
	if session.Token == "" || friendshipID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := cfg.API.DeclineFriendRequest(ctx, session.Token, friendshipID)
		return friendMutationMsg{
			Kind:         "friend_decline",
			PeerID:       peerID,
			FriendshipID: friendshipID,
			Err:          err,
		}
	}
}

func removeFriendCmd(cfg Config, session omiai.Session, peerID string) tea.Cmd {
	if session.Token == "" || peerID == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := cfg.API.RemoveFriend(ctx, session.Token, peerID)
		return friendMutationMsg{
			Kind:   "friend_remove",
			PeerID: peerID,
			Err:    err,
		}
	}
}

func (m *model) applyRelationships(friends []omiai.Friend, pending []omiai.PendingFriendRequest) {
	newFriends := make(map[string]omiai.Friend, len(friends))
	for _, friend := range friends {
		newFriends[friend.QuicdialID] = friend
		delete(m.pendingOutgoing, friend.QuicdialID)
		m.ensurePeer(friend.QuicdialID, friend.DisplayName)
	}
	m.friends = newFriends

	oldPending := m.pendingIncoming
	newPending := make(map[string]omiai.PendingFriendRequest, len(pending))
	for _, request := range pending {
		newPending[request.FromQuicdialID] = request
		m.ensurePeer(request.FromQuicdialID, request.FromDisplayName)
		if _, seen := oldPending[request.FromQuicdialID]; !seen {
			m.queueDialog(contactDialog{
				Kind:         dialogKindFriendRequest,
				PeerID:       request.FromQuicdialID,
				DisplayName:  request.FromDisplayName,
				FriendshipID: request.FriendshipID,
			})
		}
	}
	m.pendingIncoming = newPending
}

func (m *model) handlePushEvent(event omiai.PushEvent) tea.Cmd {
	switch event.Event {
	case "friend_request_received":
		request := omiai.PendingFriendRequest{
			FriendshipID:    stringFromMap(event.Payload, "friendship_id"),
			FromQuicdialID:  stringFromMap(event.Payload, "from_quicdial_id"),
			FromDisplayName: stringFromMap(event.Payload, "from_display_name"),
			FromAvatarID:    stringFromMap(event.Payload, "from_avatar_id"),
		}
		if request.FromQuicdialID != "" {
			m.pendingIncoming[request.FromQuicdialID] = request
			m.ensurePeer(request.FromQuicdialID, request.FromDisplayName)
			m.queueDialog(contactDialog{
				Kind:         dialogKindFriendRequest,
				PeerID:       request.FromQuicdialID,
				DisplayName:  request.FromDisplayName,
				FriendshipID: request.FriendshipID,
			})
			m.status = fmt.Sprintf("%s sent a friend request", fallback(request.FromDisplayName, request.FromQuicdialID))
		}
	case "friend_accepted":
		peerID := stringFromMap(event.Payload, "by_quicdial_id")
		if peerID != "" {
			m.friends[peerID] = omiai.Friend{
				FriendshipID: stringFromMap(event.Payload, "friendship_id"),
				QuicdialID:   peerID,
				DisplayName:  fallback(stringFromMap(event.Payload, "by_display_name"), peerID),
				AvatarID:     fallback(stringFromMap(event.Payload, "by_avatar_id"), "default"),
			}
			delete(m.pendingOutgoing, peerID)
			m.ensurePeer(peerID, stringFromMap(event.Payload, "by_display_name"))
			m.status = fmt.Sprintf("%s accepted your friend request", fallback(stringFromMap(event.Payload, "by_display_name"), peerID))
		}
	case "friend_declined":
		peerID := stringFromMap(event.Payload, "by_quicdial_id")
		delete(m.pendingOutgoing, peerID)
		if peerID != "" {
			m.status = fmt.Sprintf("%s declined your friend request", peerID)
		}
	case "friend_removed":
		peerID := stringFromMap(event.Payload, "by_quicdial_id")
		delete(m.friends, peerID)
		delete(m.pendingOutgoing, peerID)
		delete(m.pendingIncoming, peerID)
		_ = m.forgetPeer(peerID)
		if peerID != "" {
			m.status = fmt.Sprintf("%s removed you as a friend", peerID)
		}
	}

	return syncRelationshipsCmd(m.cfg, m.session)
}

func (m *model) isFriend(peerID string) bool {
	_, ok := m.friends[peerID]
	return ok
}

func (m *model) isTrusted(peerID string) bool {
	_, ok := m.trust.Trusted[peerID]
	return ok
}

func (m *model) trustedPublicKey(peerID string) string {
	if trusted, ok := m.trust.Trusted[peerID]; ok {
		return trusted.PublicKey
	}
	return ""
}

func (m *model) trustPeer(peerID, publicKey string) error {
	if strings.TrimSpace(peerID) == "" || strings.TrimSpace(publicKey) == "" {
		return nil
	}
	m.trust.Trusted[peerID] = social.TrustedPeer{
		PeerID:    peerID,
		PublicKey: publicKey,
		TrustedAt: time.Now(),
	}
	return m.cfg.Store.SaveTrustState(m.trust)
}

func (m *model) forgetPeer(peerID string) error {
	delete(m.trust.Trusted, peerID)
	return m.cfg.Store.SaveTrustState(m.trust)
}

func (m *model) queueDialog(dialog contactDialog) {
	if dialog.PeerID == "" {
		return
	}
	if m.activeDialog != nil && m.activeDialog.PeerID == dialog.PeerID {
		mergeDialog(m.activeDialog, dialog)
		return
	}
	for i := range m.dialogQueue {
		if m.dialogQueue[i].PeerID == dialog.PeerID {
			mergeDialog(&m.dialogQueue[i], dialog)
			return
		}
	}
	if dialog.DisplayName == "" {
		dialog.DisplayName = dialog.PeerID
	}
	m.dialogQueue = append(m.dialogQueue, dialog)
	m.ensureActiveDialog()
}

func (m *model) ensureActiveDialog() {
	if m.activeDialog != nil || len(m.dialogQueue) == 0 {
		return
	}
	dialog := m.dialogQueue[0]
	m.dialogQueue = m.dialogQueue[1:]
	m.activeDialog = &dialog
}

func (m *model) dismissDialog() {
	m.activeDialog = nil
	m.ensureActiveDialog()
}

func (m *model) acceptActiveDialog() tea.Cmd {
	if m.activeDialog == nil {
		return nil
	}

	dialog := *m.activeDialog
	var cmds []tea.Cmd

	if err := m.trustPeer(dialog.PeerID, dialog.PublicKey); err != nil {
		m.errorText = err.Error()
	}

	body := dialog.Message
	if body == "" && dialog.Ciphertext != "" && dialog.PublicKey != "" {
		plaintext, err := social.Decrypt(m.trust.Identity, dialog.PublicKey, dialog.Ciphertext)
		if err != nil {
			m.errorText = err.Error()
		} else {
			body = plaintext
		}
	}
	if body != "" {
		m.ensurePeer(dialog.PeerID, dialog.DisplayName)
		m.appendMessage(dialog.PeerID, chatMessage{
			ID:          dialog.MessageID,
			FromSelf:    false,
			DisplayName: fallback(dialog.DisplayName, dialog.PeerID),
			Body:        body,
			SentAt:      time.Now(),
			Receipt:     "read",
			Intro:       true,
		})
	}

	delete(m.pendingIncoming, dialog.PeerID)
	if dialog.FriendshipID != "" {
		cmds = append(cmds, acceptFriendRequestCmd(m.cfg, m.session, dialog.FriendshipID, dialog.PeerID))
	}
	cmds = append(cmds, sendFriendAckCmd(m.client, m.session, dialog.PeerID, dialog.MessageID, "accepted", m.trust.Identity.PublicKey, ""))
	m.status = fmt.Sprintf("Trusted %s for secure chat", fallback(dialog.DisplayName, dialog.PeerID))
	m.dismissDialog()
	return tea.Batch(cmds...)
}

func (m *model) rejectActiveDialog() tea.Cmd {
	if m.activeDialog == nil {
		return nil
	}

	dialog := *m.activeDialog
	var cmds []tea.Cmd

	delete(m.pendingIncoming, dialog.PeerID)
	if dialog.FriendshipID != "" {
		cmds = append(cmds, declineFriendRequestCmd(m.cfg, m.session, dialog.FriendshipID, dialog.PeerID))
	}
	cmds = append(cmds, sendFriendAckCmd(m.client, m.session, dialog.PeerID, dialog.MessageID, "rejected", "", "403_REJECTED"))
	m.status = fmt.Sprintf("Rejected first contact from %s", fallback(dialog.DisplayName, dialog.PeerID))
	m.dismissDialog()
	return tea.Batch(cmds...)
}

func (m *model) updateMessageReceipt(peerID, messageID, receipt string) {
	messages := m.conversations[peerID]
	for i := range messages {
		if messages[i].ID == messageID {
			messages[i].Receipt = receipt
		}
	}
	m.conversations[peerID] = messages
	if peerID == m.currentPeerID() {
		m.rebuildViewport()
	}
}

func mergeDialog(target *contactDialog, update contactDialog) {
	if update.Kind == dialogKindFriendIntro {
		target.Kind = update.Kind
	}
	if update.DisplayName != "" {
		target.DisplayName = update.DisplayName
	}
	if update.Message != "" {
		target.Message = update.Message
	}
	if update.PublicKey != "" {
		target.PublicKey = update.PublicKey
	}
	if update.FriendshipID != "" {
		target.FriendshipID = update.FriendshipID
	}
	if update.MessageID != "" {
		target.MessageID = update.MessageID
	}
	if update.Ciphertext != "" {
		target.Ciphertext = update.Ciphertext
	}
}

func isNonFatalFriendError(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "already_pending", "already_friends", "recipient_not_found":
		return true
	default:
		return false
	}
}

func stringFromMap(values map[string]any, key string) string {
	value := strings.TrimSpace(fmt.Sprintf("%v", values[key]))
	if value == "<nil>" {
		return ""
	}
	return value
}

func (m *model) focusPeer(peerID string) {
	for i := range m.peers {
		if m.peers[i].QuicdialID == peerID {
			m.selected = i
			delete(m.unread, peerID)
			m.rebuildViewport()
			return
		}
	}
}

func (m *model) peerTags(peer omiai.Peer) string {
	tags := []string{}
	if peer.Online {
		tags = append(tags, "[online]")
	} else {
		tags = append(tags, "[offline]")
	}
	if m.isFriend(peer.QuicdialID) {
		tags = append(tags, "[friend]")
	}
	if m.isTrusted(peer.QuicdialID) {
		tags = append(tags, "[trusted]")
	}
	if _, ok := m.pendingIncoming[peer.QuicdialID]; ok {
		tags = append(tags, "[incoming]")
	}
	if m.pendingOutgoing[peer.QuicdialID] {
		tags = append(tags, "[pending]")
	}
	return strings.Join(tags, " ")
}

func (m *model) currentPeerStatus() string {
	peerID := m.currentPeerID()
	if peerID == "" {
		return "no active peer"
	}
	if m.isTrusted(peerID) {
		return "trusted secure chat"
	}
	if m.isFriend(peerID) {
		return "friend, waiting for key trust"
	}
	if _, ok := m.pendingIncoming[peerID]; ok {
		return "incoming friend request"
	}
	if m.pendingOutgoing[peerID] {
		return "friend request pending"
	}
	return "not friends"
}

func (m *model) selectedPeerSummary() string {
	peerID := m.currentPeerID()
	if peerID == "" {
		return "No peer selected"
	}
	summary := fmt.Sprintf("Selected: %s (%s) | %s", m.currentPeerName(), peerID, m.currentPeerStatus())
	for _, peer := range m.peers {
		if peer.QuicdialID == peerID && strings.TrimSpace(peer.IP) != "" {
			summary = summary + fmt.Sprintf(" | %s", peer.IP)
			break
		}
	}
	return summary
}

func (m *model) renderDialog() string {
	if m.activeDialog == nil {
		return ""
	}

	title := "Incoming friend request"
	if m.activeDialog.Kind == dialogKindFriendIntro {
		title = "First contact"
	}

	lines := []string{
		m.styles.accent.Render(title),
		fmt.Sprintf("%s (%s)", fallback(m.activeDialog.DisplayName, m.activeDialog.PeerID), m.activeDialog.PeerID),
	}
	if m.activeDialog.Message != "" {
		lines = append(lines, "")
		lines = append(lines, m.activeDialog.Message)
	}
	lines = append(lines, "")
	lines = append(lines, m.styles.subtle.Render("Accept trusts this peer key and enables encrypted follow-up messages."))
	lines = append(lines, m.styles.subtle.Render("Press y to accept or n to reject."))

	width := maxInt(42, minInt(72, m.width-4))
	if width <= 0 {
		width = 56
	}

	return lipgloss.PlaceHorizontal(
		maxInt(width, m.width-2),
		lipgloss.Center,
		m.styles.modal.Width(width).Render(strings.Join(lines, "\n")),
	)
}

func (m *model) renderDialPrompt() string {
	lines := []string{
		m.styles.accent.Render("Dial Peer"),
		m.styles.subtle.Render("Enter a quicdial code and press enter."),
		"",
		m.dialInput.View(),
		"",
		m.styles.subtle.Render("esc close"),
	}

	width := maxInt(42, minInt(72, m.width-4))
	if width <= 0 {
		width = 56
	}

	return lipgloss.PlaceHorizontal(
		maxInt(width, m.width-2),
		lipgloss.Center,
		m.styles.modal.Width(width).Render(strings.Join(lines, "\n")),
	)
}

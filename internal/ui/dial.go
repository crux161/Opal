package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"opal/internal/omiai"
)

type dialResultMsg struct {
	Code   string
	Result omiai.ResolveResult
	Err    error
}

func dialPeerCmd(client *omiai.SocketClient, code string) tea.Cmd {
	code = strings.TrimSpace(code)
	if client == nil || code == "" {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := client.ResolveQuicdial(ctx, code)
		return dialResultMsg{
			Code:   code,
			Result: result,
			Err:    err,
		}
	}
}

func (m *model) openDialPrompt() {
	m.dialing = true
	m.dialInput.SetValue("")
	m.dialInput.CursorEnd()
	m.dialInput.Focus()
	m.status = "Enter a quicdial code to dial a peer"
}

func (m *model) closeDialPrompt() {
	m.dialing = false
	m.dialInput.Blur()
}

func (m *model) submitDial() tea.Cmd {
	code := strings.TrimSpace(m.dialInput.Value())
	if code == "" {
		m.errorText = "Quicdial code is required."
		return nil
	}

	m.errorText = ""
	m.status = fmt.Sprintf("Dialing %s...", code)
	return dialPeerCmd(m.client, code)
}

func (m *model) applyDialResult(code string, result omiai.ResolveResult, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return
	}

	if err != nil {
		peer := m.ensurePeer(code, code)
		if peer != nil {
			peer.Online = false
		}
		m.focusPeer(code)
		m.status = fmt.Sprintf("%s is currently offline", code)
		m.errorText = err.Error()
		return
	}

	peer := m.ensurePeer(result.PeerID, result.PeerID)
	if peer != nil {
		peer.Online = true
		peer.IP = result.IP
	}
	m.focusPeer(result.PeerID)
	m.status = fmt.Sprintf("Dialed %s at %s", result.PeerID, fallback(result.IP, "unknown address"))
}

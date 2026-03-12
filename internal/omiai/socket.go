package omiai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	peerTopicPrefix = "peer:"
	lobbyTopic      = "lobby:sankaku"
)

type SocketConfig struct {
	SignalingURL string
	Session      Session
}

type SocketClient struct {
	phoenix *phoenixClient
	peerID  string
	events  chan SocketEvent
}

func Dial(ctx context.Context, cfg SocketConfig) (*SocketClient, error) {
	socketURL, err := websocketURL(cfg)
	if err != nil {
		return nil, err
	}

	phoenix, err := dialPhoenix(ctx, socketURL)
	if err != nil {
		return nil, err
	}

	client := &SocketClient{
		phoenix: phoenix,
		peerID:  cfg.Session.User.QuicdialID,
		events:  make(chan SocketEvent, 64),
	}

	peerTopic := peerTopicPrefix + cfg.Session.User.QuicdialID
	if err := phoenix.Join(ctx, peerTopic, map[string]any{}); err != nil {
		_ = phoenix.Close()
		return nil, err
	}

	registerPayload := map[string]any{
		"public_key":    cfg.Session.User.QuicdialID,
		"session_token": cfg.Session.DeviceID,
		"sig_ts":        fmt.Sprintf("%d", time.Now().UnixMilli()),
		"sig_nonce":     randomNonce(),
	}
	if _, err := phoenix.Push(ctx, peerTopic, "register_startup", registerPayload); err != nil {
		_ = phoenix.Close()
		return nil, err
	}

	if err := phoenix.Join(ctx, lobbyTopic, map[string]any{}); err != nil {
		_ = phoenix.Close()
		return nil, err
	}

	go client.dispatch()

	refreshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.RefreshPeers(refreshCtx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

func (c *SocketClient) Events() <-chan SocketEvent {
	return c.events
}

func (c *SocketClient) SendChat(ctx context.Context, toPeerID, body, displayName string) error {
	return c.pushRelay(ctx, RelayMessage{
		Kind:        "chat",
		Body:        body,
		DisplayName: displayName,
	}, toPeerID)
}

func (c *SocketClient) SendTyping(ctx context.Context, toPeerID, displayName string, typing bool) error {
	return c.pushRelay(ctx, RelayMessage{
		Kind:        "typing",
		Typing:      typing,
		DisplayName: displayName,
	}, toPeerID)
}

func (c *SocketClient) RefreshPeers(ctx context.Context) error {
	response, err := c.phoenix.Push(ctx, lobbyTopic, "list_peers", map[string]any{})
	if err != nil {
		return err
	}

	var payload struct {
		Peers []Peer `json:"peers"`
	}
	if err := decodeReply(response, &payload); err != nil {
		return err
	}

	select {
	case c.events <- PeersEvent{Peers: payload.Peers}:
	default:
	}

	return nil
}

func (c *SocketClient) Close() error {
	return c.phoenix.Close()
}

func (c *SocketClient) dispatch() {
	defer close(c.events)

	for frame := range c.phoenix.Events() {
		switch frame.Event {
		case "relay_message":
			var relay RelayMessage
			if err := json.Unmarshal(frame.Payload, &relay); err != nil {
				c.emit(ErrorEvent{Err: err})
				return
			}
			c.emit(RelayEvent{Message: relay})
		case "presence_state", "presence_diff":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.RefreshPeers(ctx)
			cancel()
			if err != nil {
				c.emit(ErrorEvent{Err: err})
				return
			}
		case "phx_error", "phx_close":
			c.emit(ErrorEvent{Err: fmt.Errorf("phoenix %s on %s", frame.Event, frame.Topic)})
			return
		}
	}

	c.emit(ErrorEvent{Err: fmt.Errorf("signaling connection closed")})
}

func (c *SocketClient) emit(event SocketEvent) {
	select {
	case c.events <- event:
	default:
	}
}

func (c *SocketClient) pushRelay(ctx context.Context, message RelayMessage, toPeerID string) error {
	message.ToQuicdialID = strings.TrimSpace(toPeerID)
	_, err := c.phoenix.Push(ctx, peerTopicPrefix+c.peerID, "relay_message", message)
	return err
}

func websocketURL(cfg SocketConfig) (string, error) {
	raw := cfg.SignalingURL
	if raw == "" {
		raw = "ws://127.0.0.1:4000/ws/sankaku/websocket"
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	if cfg.Session.Token != "" {
		query.Set("auth_token", cfg.Session.Token)
	} else {
		query.Set("quicdial_id", cfg.Session.User.QuicdialID)
	}
	query.Set("device_uuid", cfg.Session.DeviceID)
	query.Set("vsn", "2.0.0")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func decodeReply(raw map[string]any, out any) error {
	payload, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func randomNonce() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

package omiai

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestPhoenixEnvelopeRoundTrip(t *testing.T) {
	t.Parallel()

	input := phoenixEnvelope{
		JoinRef: "1",
		Ref:     "2",
		Topic:   "peer:alice",
		Event:   "relay_message",
		Payload: json.RawMessage(`{"kind":"chat","body":"hello"}`),
	}

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var output phoenixEnvelope
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if output.JoinRef != input.JoinRef || output.Ref != input.Ref || output.Topic != input.Topic || output.Event != input.Event {
		t.Fatalf("unexpected envelope: %#v", output)
	}

	if string(output.Payload) != string(input.Payload) {
		t.Fatalf("payload mismatch: got %s want %s", output.Payload, input.Payload)
	}
}

func TestWebsocketURLUsesAuthToken(t *testing.T) {
	t.Parallel()

	rawURL, err := websocketURL(SocketConfig{
		SignalingURL: "ws://localhost:4000/ws/sankaku/websocket",
		Session: Session{
			Token:    "token-123",
			DeviceID: "device-1",
			User: User{
				QuicdialID: "alice",
			},
		},
	})
	if err != nil {
		t.Fatalf("websocketURL: %v", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	query := parsed.Query()
	if got := query.Get("auth_token"); got != "token-123" {
		t.Fatalf("auth token mismatch: %q", got)
	}
	if got := query.Get("quicdial_id"); got != "" {
		t.Fatalf("expected no direct quicdial id, got %q", got)
	}
	if got := query.Get("device_uuid"); got != "device-1" {
		t.Fatalf("device id mismatch: %q", got)
	}
	if got := query.Get("vsn"); got != "2.0.0" {
		t.Fatalf("vsn mismatch: %q", got)
	}
}

func TestWebsocketURLUsesDirectMode(t *testing.T) {
	t.Parallel()

	rawURL, err := websocketURL(SocketConfig{
		SignalingURL: "ws://localhost:4000/ws/sankaku/websocket",
		Session: Session{
			DeviceID: "device-2",
			Direct:   true,
			User: User{
				QuicdialID: "bob",
			},
		},
	})
	if err != nil {
		t.Fatalf("websocketURL: %v", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	query := parsed.Query()
	if got := query.Get("auth_token"); got != "" {
		t.Fatalf("expected no auth token, got %q", got)
	}
	if got := query.Get("quicdial_id"); got != "bob" {
		t.Fatalf("quicdial id mismatch: %q", got)
	}
	if got := query.Get("device_uuid"); got != "device-2" {
		t.Fatalf("device id mismatch: %q", got)
	}
}

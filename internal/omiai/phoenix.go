package omiai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const phoenixHeartbeatInterval = 25 * time.Second

type phoenixEnvelope struct {
	JoinRef string
	Ref     string
	Topic   string
	Event   string
	Payload json.RawMessage
}

func (e phoenixEnvelope) MarshalJSON() ([]byte, error) {
	payload := e.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	frame := []any{emptyToNil(e.JoinRef), emptyToNil(e.Ref), e.Topic, e.Event, json.RawMessage(payload)}
	return json.Marshal(frame)
}

func (e *phoenixEnvelope) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 5 {
		return fmt.Errorf("phoenix frame length %d", len(raw))
	}
	e.JoinRef = rawString(raw[0])
	e.Ref = rawString(raw[1])
	if err := json.Unmarshal(raw[2], &e.Topic); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[3], &e.Event); err != nil {
		return err
	}
	e.Payload = append(e.Payload[:0], raw[4]...)
	return nil
}

type phoenixReply struct {
	Status   string         `json:"status"`
	Response map[string]any `json:"response"`
}

type phoenixClient struct {
	conn     *websocket.Conn
	counter  atomic.Uint64
	events   chan phoenixEnvelope
	closed   chan struct{}
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan phoenixReply
	joinRefs map[string]string
	closeErr error
}

func dialPhoenix(ctx context.Context, socketURL string) (*phoenixClient, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, socketURL, nil)
	if err != nil {
		return nil, err
	}

	client := &phoenixClient{
		conn:     conn,
		events:   make(chan phoenixEnvelope, 64),
		closed:   make(chan struct{}),
		pending:  make(map[string]chan phoenixReply),
		joinRefs: make(map[string]string),
	}

	go client.readLoop()
	go client.heartbeatLoop()

	return client, nil
}

func (c *phoenixClient) Events() <-chan phoenixEnvelope {
	return c.events
}

func (c *phoenixClient) Join(ctx context.Context, topic string, payload any) error {
	ref := c.nextRef()
	replyCh := make(chan phoenixReply, 1)

	c.mu.Lock()
	c.pending[ref] = replyCh
	c.mu.Unlock()

	if err := c.writeFrame(phoenixEnvelope{
		JoinRef: ref,
		Ref:     ref,
		Topic:   topic,
		Event:   "phx_join",
		Payload: mustMarshalPayload(payload),
	}); err != nil {
		c.deletePending(ref)
		return err
	}

	select {
	case <-ctx.Done():
		c.deletePending(ref)
		return ctx.Err()
	case <-c.closed:
		return c.err()
	case reply := <-replyCh:
		if reply.Status != "ok" {
			return fmt.Errorf("phoenix join failed: %v", reply.Response)
		}
		c.mu.Lock()
		c.joinRefs[topic] = ref
		c.mu.Unlock()
		return nil
	}
}

func (c *phoenixClient) Push(ctx context.Context, topic, event string, payload any) (map[string]any, error) {
	c.mu.Lock()
	joinRef := c.joinRefs[topic]
	c.mu.Unlock()

	if topic != "phoenix" && joinRef == "" {
		return nil, fmt.Errorf("topic %s not joined", topic)
	}

	ref := c.nextRef()
	replyCh := make(chan phoenixReply, 1)

	c.mu.Lock()
	c.pending[ref] = replyCh
	c.mu.Unlock()

	if err := c.writeFrame(phoenixEnvelope{
		JoinRef: joinRef,
		Ref:     ref,
		Topic:   topic,
		Event:   event,
		Payload: mustMarshalPayload(payload),
	}); err != nil {
		c.deletePending(ref)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.deletePending(ref)
		return nil, ctx.Err()
	case <-c.closed:
		return nil, c.err()
	case reply := <-replyCh:
		if reply.Status != "ok" {
			return nil, fmt.Errorf("phoenix %s failed: %v", event, reply.Response)
		}
		return reply.Response, nil
	}
}

func (c *phoenixClient) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return c.conn.Close()
}

func (c *phoenixClient) readLoop() {
	defer close(c.events)
	defer close(c.closed)

	for {
		var frame phoenixEnvelope
		if err := c.conn.ReadJSON(&frame); err != nil {
			c.fail(err)
			return
		}

		if frame.Event == "phx_reply" && frame.Ref != "" {
			var reply phoenixReply
			if err := json.Unmarshal(frame.Payload, &reply); err != nil {
				c.fail(err)
				return
			}

			c.mu.Lock()
			replyCh := c.pending[frame.Ref]
			delete(c.pending, frame.Ref)
			c.mu.Unlock()

			if replyCh != nil {
				replyCh <- reply
			}
			continue
		}

		select {
		case c.events <- frame:
		default:
		}
	}
}

func (c *phoenixClient) heartbeatLoop() {
	ticker := time.NewTicker(phoenixHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = c.Push(ctx, "phoenix", "heartbeat", map[string]any{})
			cancel()
		case <-c.closed:
			return
		}
	}
}

func (c *phoenixClient) writeFrame(frame phoenixEnvelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(frame)
}

func (c *phoenixClient) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closeErr = err
	for ref, replyCh := range c.pending {
		delete(c.pending, ref)
		select {
		case replyCh <- phoenixReply{Status: "error", Response: map[string]any{"reason": err.Error()}}:
		default:
		}
	}
}

func (c *phoenixClient) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	return errors.New("phoenix socket closed")
}

func (c *phoenixClient) deletePending(ref string) {
	c.mu.Lock()
	delete(c.pending, ref)
	c.mu.Unlock()
}

func (c *phoenixClient) nextRef() string {
	return fmt.Sprintf("%d", c.counter.Add(1))
}

func mustMarshalPayload(payload any) json.RawMessage {
	if payload == nil {
		return json.RawMessage(`{}`)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func rawString(raw json.RawMessage) string {
	if string(raw) == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

package omiai

import "time"

type User struct {
	QuicdialID  string `json:"quicdial_id"`
	DisplayName string `json:"display_name"`
	AvatarID    string `json:"avatar_id"`
	Email       string `json:"email,omitempty"`
}

type Session struct {
	Token      string `json:"token,omitempty"`
	DeviceID   string `json:"device_id"`
	Direct     bool   `json:"direct,omitempty"`
	ServerHost string `json:"server_host,omitempty"`
	User       User   `json:"user"`
}

type SignupRequest struct {
	QuicdialID  string `json:"quicdial_id,omitempty"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	AvatarID    string `json:"avatar_id,omitempty"`
}

type Peer struct {
	QuicdialID  string `json:"quicdial_id"`
	DisplayName string `json:"display_name"`
	AvatarID    string `json:"avatar_id"`
	IP          string `json:"ip"`
	DeviceID    string `json:"device_uuid"`
	OnlineAt    int64  `json:"online_at"`
	Online      bool   `json:"-"`
}

type RelayMessage struct {
	Kind             string `json:"kind,omitempty"`
	Body             string `json:"body,omitempty"`
	Typing           bool   `json:"typing,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	MessageID        string `json:"message_id,omitempty"`
	PublicFriendKey  string `json:"public_friend_key,omitempty"`
	Ciphertext       string `json:"ciphertext,omitempty"`
	FriendshipID     string `json:"friendship_id,omitempty"`
	ReceiptMessageID string `json:"receipt_message_id,omitempty"`
	Status           string `json:"status,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ToQuicdialID     string `json:"to_quicdial_id,omitempty"`
	FromQuicdialID   string `json:"from_quicdial_id,omitempty"`
	FromDeviceID     string `json:"from_device_uuid,omitempty"`
	SentAt           int64  `json:"sent_at,omitempty"`
}

type Friend struct {
	FriendshipID string `json:"friendship_id"`
	QuicdialID   string `json:"quicdial_id"`
	DisplayName  string `json:"display_name"`
	AvatarID     string `json:"avatar_id"`
}

type PendingFriendRequest struct {
	FriendshipID    string    `json:"friendship_id"`
	FromQuicdialID  string    `json:"from_quicdial_id"`
	FromDisplayName string    `json:"from_display_name"`
	FromAvatarID    string    `json:"from_avatar_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type FriendRequestResponse struct {
	FriendshipID string `json:"friendship_id"`
	Status       string `json:"status"`
}

type SocketEvent interface {
	isSocketEvent()
}

type PeersEvent struct {
	Peers []Peer
}

func (PeersEvent) isSocketEvent() {}

type RelayEvent struct {
	Message RelayMessage
}

func (RelayEvent) isSocketEvent() {}

type ErrorEvent struct {
	Err error
}

func (ErrorEvent) isSocketEvent() {}

type PushEvent struct {
	Event   string
	Payload map[string]any
}

func (PushEvent) isSocketEvent() {}

type ResolveResult struct {
	PeerID     string
	IP         string
	ICEServers []map[string]any
}

package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"opal/internal/omiai"
	"opal/internal/social"
)

func TestStoreSaveLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	input := omiai.Session{
		Token:    "token-123",
		DeviceID: "device-123",
		User: omiai.User{
			QuicdialID:  "alice",
			DisplayName: "Alice",
			AvatarID:    "kyu-kun",
			Email:       "alice@example.com",
		},
	}

	if err := store.Save(input); err != nil {
		t.Fatalf("save: %v", err)
	}

	output, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(output, input) {
		t.Fatalf("loaded session mismatch:\n got: %#v\nwant: %#v", output, input)
	}
}

func TestGenerateDeviceIDFormat(t *testing.T) {
	t.Parallel()

	deviceID := GenerateDeviceID()
	parts := strings.Split(deviceID, "-")
	if len(parts) != 5 {
		t.Fatalf("unexpected device id: %s", deviceID)
	}

	expected := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expected[i] {
			t.Fatalf("unexpected part length %d for %q", len(part), deviceID)
		}
	}
}

func TestEnsureTrustStatePersistsIdentity(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	state, err := store.EnsureTrustState()
	if err != nil {
		t.Fatalf("ensure trust state: %v", err)
	}
	if state.Identity.PublicKey == "" || state.Identity.PrivateKey == "" {
		t.Fatalf("identity not initialized: %#v", state.Identity)
	}

	state.Trusted["alice"] = social.TrustedPeer{
		PeerID:    "alice",
		PublicKey: "pubkey",
	}
	if err := store.SaveTrustState(state); err != nil {
		t.Fatalf("save trust state: %v", err)
	}

	loaded, err := store.LoadTrustState()
	if err != nil {
		t.Fatalf("load trust state: %v", err)
	}
	if !reflect.DeepEqual(loaded.Trusted, state.Trusted) {
		t.Fatalf("loaded trusted peers mismatch:\n got: %#v\nwant: %#v", loaded.Trusted, state.Trusted)
	}
}

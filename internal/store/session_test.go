package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"opal/internal/omiai"
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

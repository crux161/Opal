package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"opal/internal/omiai"
)

type Store struct {
	path string
}

func New(path string) (*Store, error) {
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(configDir, "opal", "session.json")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	return &Store{path: path}, nil
}

func (s *Store) Load() (omiai.Session, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return omiai.Session{}, err
	}

	var session omiai.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return omiai.Session{}, err
	}

	if session.DeviceID == "" {
		session.DeviceID = GenerateDeviceID()
	}

	return session, nil
}

func (s *Store) Save(session omiai.Session) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o600)
}

func (s *Store) Clear() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) Path() string {
	return s.path
}

func GenerateDeviceID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Errorf("generate device id: %w", err))
	}

	hexValue := hex.EncodeToString(raw)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexValue[0:8],
		hexValue[8:12],
		hexValue[12:16],
		hexValue[16:20],
		hexValue[20:32],
	)
}

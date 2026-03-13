package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"opal/internal/omiai"
)

type Store struct {
	path string
}

type encryptedSession struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
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

	if session, err := s.loadEncrypted(data); err == nil {
		if session.DeviceID == "" {
			session.DeviceID = GenerateDeviceID()
		}
		if session.ServerHost == "" {
			session.ServerHost = omiai.DefaultServerHost
		}
		return session, nil
	}

	var session omiai.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return omiai.Session{}, err
	}

	if session.DeviceID == "" {
		session.DeviceID = GenerateDeviceID()
	}
	if session.ServerHost == "" {
		session.ServerHost = omiai.DefaultServerHost
	}
	_ = s.Save(session)

	return session, nil
}

func (s *Store) Save(session omiai.Session) error {
	if session.ServerHost == "" {
		session.ServerHost = omiai.DefaultServerHost
	}

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	key := s.sessionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	sealed := aead.Seal(nil, nonce, data, nil)
	payload, err := json.MarshalIndent(encryptedSession{
		Version:    1,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(sealed),
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, payload, 0o600)
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

func (s *Store) loadEncrypted(data []byte) (omiai.Session, error) {
	var payload encryptedSession
	if err := json.Unmarshal(data, &payload); err != nil {
		return omiai.Session{}, err
	}
	if payload.Version == 0 || payload.Nonce == "" || payload.Ciphertext == "" {
		return omiai.Session{}, fmt.Errorf("legacy_session_format")
	}

	nonce, err := base64.RawURLEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return omiai.Session{}, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return omiai.Session{}, err
	}

	key := s.sessionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return omiai.Session{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return omiai.Session{}, err
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return omiai.Session{}, err
	}

	var session omiai.Session
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return omiai.Session{}, err
	}
	return session, nil
}

func (s *Store) sessionKey() [32]byte {
	machineID := readMachineID()
	currentUser, _ := user.Current()
	username := ""
	homeDir := ""
	if currentUser != nil {
		username = currentUser.Username
		homeDir = currentUser.HomeDir
	}
	hostname, _ := os.Hostname()

	material := strings.Join([]string{
		"opal-session",
		machineID,
		username,
		homeDir,
		hostname,
		filepath.Dir(s.path),
	}, "|")
	return sha256.Sum256([]byte(material))
}

func readMachineID() string {
	for _, candidate := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(candidate)
		if err == nil {
			value := strings.TrimSpace(string(data))
			if value != "" {
				return value
			}
		}
	}
	return "opal-machine-id-unavailable"
}

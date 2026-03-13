package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"opal/internal/social"
)

func (s *Store) LoadTrustState() (social.TrustState, error) {
	data, err := os.ReadFile(s.friendsPath())
	if err != nil {
		return social.TrustState{}, err
	}

	var state social.TrustState
	if err := json.Unmarshal(data, &state); err != nil {
		return social.TrustState{}, err
	}
	if state.Trusted == nil {
		state.Trusted = make(map[string]social.TrustedPeer)
	}
	return social.EnsureIdentity(state)
}

func (s *Store) SaveTrustState(state social.TrustState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.friendsPath(), data, 0o600)
}

func (s *Store) EnsureTrustState() (social.TrustState, error) {
	state, err := s.LoadTrustState()
	if err == nil {
		return state, nil
	}
	if !os.IsNotExist(err) {
		return social.TrustState{}, err
	}

	state, err = social.EnsureIdentity(social.TrustState{})
	if err != nil {
		return social.TrustState{}, err
	}
	return state, s.SaveTrustState(state)
}

func (s *Store) friendsPath() string {
	return filepath.Join(filepath.Dir(s.path), "friends.json")
}

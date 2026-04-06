package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type StateStore struct {
	mu          sync.RWMutex
	sessionsDir string
	sessionID   string
}

func NewStateStore(sessionsDir, sessionID string) *StateStore {
	return &StateStore{
		sessionsDir: strings.TrimSpace(sessionsDir),
		sessionID:   strings.TrimSpace(sessionID),
	}
}

func (s *StateStore) Namespace(extensionID string) StateNamespace {
	return StateNamespace{
		store:       s,
		extensionID: strings.TrimSpace(extensionID),
	}
}

func (s *StateStore) Bound() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionsDir != "" && s.sessionID != ""
}

type StateNamespace struct {
	store       *StateStore
	extensionID string
}

func (n StateNamespace) Get() (map[string]any, bool, error) {
	if n.store == nil || n.extensionID == "" {
		return nil, false, nil
	}
	path, ok := n.store.pathFor(n.extensionID)
	if !ok {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading extension state %s: %w", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false, fmt.Errorf("parsing extension state %s: %w", path, err)
	}
	return out, true, nil
}

func (n StateNamespace) Set(value any) error {
	if n.store == nil || n.extensionID == "" {
		return nil
	}
	path, ok := n.store.pathFor(n.extensionID)
	if !ok {
		return nil
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding extension state for %q: %w", n.extensionID, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating extension state dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing extension state %s: %w", path, err)
	}
	return nil
}

func (n StateNamespace) Delete() error {
	if n.store == nil || n.extensionID == "" {
		return nil
	}
	path, ok := n.store.pathFor(n.extensionID)
	if !ok {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting extension state %s: %w", path, err)
	}
	return nil
}

func (s *StateStore) pathFor(extensionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sessionsDir == "" || s.sessionID == "" || extensionID == "" {
		return "", false
	}
	return filepath.Join(s.sessionsDir, s.sessionID, "state", "extensions", extensionID+".json"), true
}

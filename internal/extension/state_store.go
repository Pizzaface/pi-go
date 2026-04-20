package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pizzaface/go-pi/internal/extension/api"
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

// Patch applies an RFC 7396 JSON Merge Patch to the namespace's stored
// value. Null values in the patch delete keys; objects recurse; arrays and
// scalars replace. A missing store is treated as an empty object.
func (n StateNamespace) Patch(merge json.RawMessage) error {
	if n.store == nil || n.extensionID == "" {
		return nil
	}
	current, _, err := n.Get()
	if err != nil {
		return err
	}
	if current == nil {
		current = map[string]any{}
	}
	var patch any
	if err := json.Unmarshal(merge, &patch); err != nil {
		return fmt.Errorf("state.patch: invalid patch JSON: %w", err)
	}
	merged := mergePatch(current, patch)
	return n.Set(merged)
}

// mergePatch implements RFC 7396 JSON Merge Patch.
// - If patch is not a map, it replaces target wholesale.
// - If patch is a map, each key: null deletes, map recurses, anything else replaces.
func mergePatch(target, patch any) any {
	pm, pOK := patch.(map[string]any)
	if !pOK {
		return patch
	}
	tm, tOK := target.(map[string]any)
	if !tOK {
		tm = map[string]any{}
	}
	for k, v := range pm {
		if v == nil {
			delete(tm, k)
			continue
		}
		if _, isMap := v.(map[string]any); isMap {
			tm[k] = mergePatch(tm[k], v)
			continue
		}
		tm[k] = v
	}
	return tm
}

func (s *StateStore) pathFor(extensionID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sessionsDir == "" || s.sessionID == "" || extensionID == "" {
		return "", false
	}
	return filepath.Join(s.sessionsDir, s.sessionID, "state", "extensions", extensionID+".json"), true
}

// HostedView returns an api.StateStoreIface adapter for this store.
// Used by the hosted RPC handler to access state without an import cycle.
func (s *StateStore) HostedView() api.StateStoreIface { return &hostedStoreView{s: s} }

type hostedStoreView struct{ s *StateStore }

func (v *hostedStoreView) Namespace(extensionID string) api.StateNamespaceIface {
	return &hostedNamespaceView{ns: v.s.Namespace(extensionID)}
}

type hostedNamespaceView struct{ ns StateNamespace }

func (v *hostedNamespaceView) Get() (map[string]any, bool, error) { return v.ns.Get() }
func (v *hostedNamespaceView) Set(value any) error                { return v.ns.Set(value) }
func (v *hostedNamespaceView) Patch(merge json.RawMessage) error  { return v.ns.Patch(merge) }
func (v *hostedNamespaceView) Delete() error                      { return v.ns.Delete() }

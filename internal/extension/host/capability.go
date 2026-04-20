package host

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// TrustClass distinguishes the three tiers the capability gate honours.
type TrustClass int

const (
	TrustUnknown TrustClass = iota
	TrustCompiledIn
	TrustFirstParty
	TrustThirdParty
)

func (t TrustClass) String() string {
	switch t {
	case TrustCompiledIn:
		return "compiled-in"
	case TrustFirstParty:
		return "first-party"
	case TrustThirdParty:
		return "third-party"
	default:
		return "unknown"
	}
}

// StarAll is the sentinel returned by Grants for TrustCompiledIn — every
// capability is implicitly granted, so no explicit list is meaningful.
const StarAll = "*"

type approvalsFile struct {
	Version    int                        `json:"version"`
	Extensions map[string]*approvalsEntry `json:"extensions"`
}

type approvalsEntry struct {
	TrustClass          string    `json:"trust_class"`
	FirstParty          bool      `json:"first_party"`
	Approved            bool      `json:"approved"`
	ApprovedAt          time.Time `json:"approved_at,omitempty"`
	GrantedCapabilities []string  `json:"granted_capabilities,omitempty"`
	DeniedCapabilities  []string  `json:"denied_capabilities,omitempty"`
}

// Gate consults approvals.json to decide whether an extension may use a
// given capability. Compiled-in extensions bypass the gate entirely.
type Gate struct {
	path string
	mu   sync.RWMutex
	data approvalsFile
}

// NewGate loads the approvals file. A missing file is not an error — the
// gate simply starts empty (every non-compiled-in call will be denied).
func NewGate(path string) (*Gate, error) {
	g := &Gate{path: path}
	if err := g.Reload(); err != nil {
		return nil, err
	}
	return g, nil
}

// Reload re-reads the approvals file from disk.
func (g *Gate) Reload() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.data = approvalsFile{Extensions: map[string]*approvalsEntry{}}
	data, err := os.ReadFile(g.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("gate: read %s: %w", g.path, err)
	}
	if err := json.Unmarshal(data, &g.data); err != nil {
		return fmt.Errorf("gate: parse %s: %w", g.path, err)
	}
	if g.data.Extensions == nil {
		g.data.Extensions = map[string]*approvalsEntry{}
	}
	return nil
}

// Allowed returns (true, "") when the capability is granted to the
// extension, and (false, reason) otherwise.
func (g *Gate) Allowed(id, capability string, trust TrustClass) (bool, string) {
	if trust == TrustCompiledIn {
		return true, ""
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	entry := g.data.Extensions[id]
	if entry == nil {
		return false, fmt.Sprintf("extension %q has no approvals entry", id)
	}
	if !entry.Approved {
		return false, fmt.Sprintf("extension %q is not approved", id)
	}
	for _, denied := range entry.DeniedCapabilities {
		if denied == capability {
			return false, fmt.Sprintf("capability %s explicitly denied for %s", capability, id)
		}
	}
	for _, granted := range entry.GrantedCapabilities {
		if granted == capability {
			return true, ""
		}
	}
	return false, fmt.Sprintf("capability %s not granted to %s", capability, id)
}

// Grants returns the sorted list of capabilities granted to the extension.
// For TrustCompiledIn it returns the single-element sentinel {StarAll}.
func (g *Gate) Grants(id string, trust TrustClass) []string {
	if trust == TrustCompiledIn {
		return []string{StarAll}
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	entry := g.data.Extensions[id]
	if entry == nil || !entry.Approved {
		return nil
	}
	denied := map[string]bool{}
	for _, d := range entry.DeniedCapabilities {
		denied[d] = true
	}
	out := make([]string, 0, len(entry.GrantedCapabilities))
	for _, g := range entry.GrantedCapabilities {
		if !denied[g] {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

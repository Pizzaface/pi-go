package host

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// State is the lifecycle state of a registered extension.
type State int

const (
	StateUnknown State = iota
	StatePending
	StateReady
	StateRunning
	StateStopped
	StateErrored
	StateDenied
)

func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateReady:
		return "ready"
	case StateRunning:
		return "running"
	case StateStopped:
		return "stopped"
	case StateErrored:
		return "errored"
	case StateDenied:
		return "denied"
	default:
		return "unknown"
	}
}

// Registration is one entry in the Manager's table.
type Registration struct {
	ID       string
	Mode     string // "compiled-in" | "hosted-go" | "hosted-ts"
	Trust    TrustClass
	Metadata piapi.Metadata
	State    State
	Err      error
	API      piapi.API
	Conn     *RPCConn
}

// Manager owns the capability gate, event dispatcher, and the registration
// table for the entire process.
type Manager struct {
	gate       *Gate
	dispatcher *Dispatcher

	mu   sync.RWMutex
	regs map[string]*Registration
}

// NewManager constructs a manager with the supplied gate.
func NewManager(gate *Gate) *Manager {
	return &Manager{
		gate:       gate,
		dispatcher: NewDispatcher(),
		regs:       map[string]*Registration{},
	}
}

// Gate returns the capability gate.
func (m *Manager) Gate() *Gate { return m.gate }

// Dispatcher returns the event dispatcher.
func (m *Manager) Dispatcher() *Dispatcher { return m.dispatcher }

// Register admits a new extension to the table.
//
//   - Compiled-in extensions go straight to StateReady (no gate check).
//   - Hosted extensions with approved grants go to StateReady.
//   - Hosted extensions without grants go to StatePending.
//   - Duplicate IDs are rejected.
func (m *Manager) Register(reg *Registration) error {
	if reg == nil || reg.ID == "" {
		return fmt.Errorf("manager: registration requires non-empty ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.regs[reg.ID]; exists {
		return fmt.Errorf("manager: duplicate extension id %q", reg.ID)
	}
	switch {
	case reg.Trust == TrustCompiledIn:
		reg.State = StateReady
	case len(m.gate.Grants(reg.ID, reg.Trust)) > 0:
		reg.State = StateReady
	default:
		reg.State = StatePending
	}
	m.regs[reg.ID] = reg
	return nil
}

// Get returns the registration for id, or nil if not present.
func (m *Manager) Get(id string) *Registration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.regs[id]
}

// List returns all registrations sorted by ID.
func (m *Manager) List() []*Registration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Registration, 0, len(m.regs))
	for _, r := range m.regs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetState updates the state (and optional err) for an extension.
func (m *Manager) SetState(id string, state State, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.regs[id]
	if !ok {
		return
	}
	r.State = state
	r.Err = err
}

// Shutdown notifies every running extension, closes its connection, and
// transitions state to StateStopped.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	regs := make([]*Registration, 0, len(m.regs))
	for _, r := range m.regs {
		regs = append(regs, r)
	}
	m.mu.Unlock()
	for _, r := range regs {
		if r.Conn != nil {
			_ = r.Conn.Notify(hostproto.MethodShutdown, map[string]any{})
			r.Conn.Close()
		}
		m.dispatcher.Unsubscribe(r.ID)
		m.SetState(r.ID, StateStopped, nil)
	}
	_ = ctx
}

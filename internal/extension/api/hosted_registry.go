package api

import (
	"fmt"
	"sync"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// HostedToolEntry is one tool registered by a hosted extension. Desc.Execute
// is always nil for hosted tools — invocation is dispatched over Reg.Conn,
// not executed in-process.
type HostedToolEntry struct {
	ExtID   string
	Desc    piapi.ToolDescriptor
	Reg     *host.Registration
	Manager *host.Manager
}

// CollisionError is returned by HostedToolRegistry.Add when another extension
// already owns the requested tool name in the global namespace.
type CollisionError struct {
	Name         string
	ConflictWith string
}

// Error implements error.
func (e *CollisionError) Error() string {
	return fmt.Sprintf("hosted tool name %q already owned by %q", e.Name, e.ConflictWith)
}

// ChangeKind enumerates registry mutation events surfaced via OnChange.
type ChangeKind int

const (
	// ChangeAdded fires when a new tool is inserted.
	ChangeAdded ChangeKind = iota
	// ChangeReplaced fires when an existing tool is replaced by the same owner.
	ChangeReplaced
	// ChangeRemoved fires when a tool is removed.
	ChangeRemoved
	// ChangeCollisionRejected fires when an Add was rejected because another
	// extension already owns the name. The registry is not mutated.
	ChangeCollisionRejected
)

// Change is one notification delivered to OnChange subscribers.
type Change struct {
	Kind         ChangeKind
	ExtID        string
	ToolName     string
	ConflictWith string // only populated for ChangeCollisionRejected
}

// HostedToolRegistry is the mutable source of truth for hosted tools across
// all extensions. It enforces the global tool-name namespace and is safe for
// concurrent use. Mutations emit Change notifications to any subscribers
// registered via OnChange; callbacks run after the internal write lock is
// released to avoid reentrancy hazards.
type HostedToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]HostedToolEntry // key: tool name

	subMu       sync.Mutex
	subscribers []func(Change)
}

// NewHostedToolRegistry returns an empty registry ready for use.
func NewHostedToolRegistry() *HostedToolRegistry {
	return &HostedToolRegistry{tools: map[string]HostedToolEntry{}}
}

// Add inserts a tool, or replaces an existing entry if it is owned by the
// same extension. If another extension already owns the name, Add returns a
// *CollisionError and the registry is left unchanged.
func (r *HostedToolRegistry) Add(extID string, desc piapi.ToolDescriptor, reg *host.Registration, mgr *host.Manager) error {
	r.mu.Lock()
	existing, exists := r.tools[desc.Name]
	if exists && existing.ExtID != extID {
		conflictWith := existing.ExtID
		r.mu.Unlock()
		r.emit(Change{Kind: ChangeCollisionRejected, ExtID: extID, ToolName: desc.Name, ConflictWith: conflictWith})
		return &CollisionError{Name: desc.Name, ConflictWith: conflictWith}
	}
	r.tools[desc.Name] = HostedToolEntry{ExtID: extID, Desc: desc, Reg: reg, Manager: mgr}
	r.mu.Unlock()
	if exists {
		r.emit(Change{Kind: ChangeReplaced, ExtID: extID, ToolName: desc.Name})
	} else {
		r.emit(Change{Kind: ChangeAdded, ExtID: extID, ToolName: desc.Name})
	}
	return nil
}

// Remove deletes a tool owned by extID. A missing tool is a no-op (idempotent);
// a tool owned by a different extension returns an error without mutation.
func (r *HostedToolRegistry) Remove(extID, toolName string) error {
	r.mu.Lock()
	existing, exists := r.tools[toolName]
	if !exists {
		r.mu.Unlock()
		return nil
	}
	if existing.ExtID != extID {
		owner := existing.ExtID
		r.mu.Unlock()
		return fmt.Errorf("tool %q owned by %q, not %q", toolName, owner, extID)
	}
	delete(r.tools, toolName)
	r.mu.Unlock()
	r.emit(Change{Kind: ChangeRemoved, ExtID: extID, ToolName: toolName})
	return nil
}

// RemoveExt drops every tool owned by extID. Idempotent — an extension with
// no registered tools is a silent no-op. Emits one ChangeRemoved per tool.
func (r *HostedToolRegistry) RemoveExt(extID string) {
	r.mu.Lock()
	var removed []string
	for name, e := range r.tools {
		if e.ExtID == extID {
			delete(r.tools, name)
			removed = append(removed, name)
		}
	}
	r.mu.Unlock()
	for _, n := range removed {
		r.emit(Change{Kind: ChangeRemoved, ExtID: extID, ToolName: n})
	}
}

// Snapshot returns a copy of the current entries. Callers may retain the
// slice beyond the lock; entries should be treated as read-only.
func (r *HostedToolRegistry) Snapshot() []HostedToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]HostedToolEntry, 0, len(r.tools))
	for _, e := range r.tools {
		out = append(out, e)
	}
	return out
}

// OnChange subscribes fn to every mutation. The returned function
// unsubscribes; it is safe to call more than once. Callbacks must not block
// and must not call back into the registry (the emit path does not hold the
// main lock, but callbacks should still be fast).
func (r *HostedToolRegistry) OnChange(fn func(Change)) func() {
	r.subMu.Lock()
	idx := len(r.subscribers)
	r.subscribers = append(r.subscribers, fn)
	r.subMu.Unlock()
	return func() {
		r.subMu.Lock()
		defer r.subMu.Unlock()
		if idx < len(r.subscribers) {
			r.subscribers[idx] = nil
		}
	}
}

func (r *HostedToolRegistry) emit(c Change) {
	r.subMu.Lock()
	subs := append([]func(Change){}, r.subscribers...)
	r.subMu.Unlock()
	for _, s := range subs {
		if s != nil {
			s(c)
		}
	}
}

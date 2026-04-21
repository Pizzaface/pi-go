package api

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// CommandSpec is the static description of a slash command contributed by an
// extension. Runtime registrations from `commands.register` and startup
// registrations from `cfg.ExtensionCommands` use the same shape.
type CommandSpec struct {
	Name        string
	Label       string
	Description string
	ArgHint     string
}

// CommandEntry is one command in the registry with owner/source metadata.
type CommandEntry struct {
	Spec   CommandSpec
	Owner  string
	Source string // "manifest" | "runtime"
}

// CommandCollisionError is returned when another extension owns the name.
type CommandCollisionError struct {
	Name         string
	ConflictWith string
}

func (e *CommandCollisionError) Error() string {
	return fmt.Sprintf("command %q already owned by %q", e.Name, e.ConflictWith)
}

// CommandInvokeResult mirrors hostproto.CommandsInvokeResult.
type CommandInvokeResult struct {
	Handled bool
	Message string
	Silent  bool
}

// CommandInvokeTransport is injected by the runtime so the registry can
// dispatch commands.invoke to the owning extension without taking a direct
// host.Dispatcher dependency.
type CommandInvokeTransport func(ctx context.Context, extID, name, args, entryID string) (CommandInvokeResult, error)

// CommandRegistry is the shared command namespace across all extensions.
// Mirrors HostedToolRegistry: same-owner replace, other-owner reject,
// missing-remove is a no-op.
type CommandRegistry struct {
	mu        sync.RWMutex
	entries   map[string]CommandEntry
	transport CommandInvokeTransport
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{entries: map[string]CommandEntry{}}
}

func (r *CommandRegistry) Add(owner string, spec CommandSpec, source string) error {
	if spec.Name == "" {
		return fmt.Errorf("command name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.entries[spec.Name]; ok && existing.Owner != owner {
		return &CommandCollisionError{Name: spec.Name, ConflictWith: existing.Owner}
	}
	r.entries[spec.Name] = CommandEntry{Spec: spec, Owner: owner, Source: source}
	return nil
}

func (r *CommandRegistry) Remove(owner, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.entries[name]
	if !ok {
		return nil
	}
	if existing.Owner != owner {
		return fmt.Errorf("command %q owned by %q, not %q", name, existing.Owner, owner)
	}
	delete(r.entries, name)
	return nil
}

func (r *CommandRegistry) RemoveAllByOwner(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for n, e := range r.entries {
		if e.Owner == owner {
			delete(r.entries, n)
		}
	}
}

func (r *CommandRegistry) Get(name string) (CommandEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e, ok
}

func (r *CommandRegistry) List() []CommandEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CommandEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec.Name < out[j].Spec.Name })
	return out
}

// SetInvokeTransport replaces the dispatcher. A nil transport returns an
// error from Invoke.
func (r *CommandRegistry) SetInvokeTransport(fn CommandInvokeTransport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transport = fn
}

// Invoke looks up the command and routes the invocation to the owner.
func (r *CommandRegistry) Invoke(ctx context.Context, name, args, entryID string) (CommandInvokeResult, error) {
	r.mu.RLock()
	entry, ok := r.entries[name]
	tr := r.transport
	r.mu.RUnlock()
	if !ok {
		return CommandInvokeResult{}, fmt.Errorf("command %q not found", name)
	}
	if tr == nil {
		return CommandInvokeResult{}, fmt.Errorf("no invoke transport set")
	}
	return tr(ctx, entry.Owner, name, args, entryID)
}

package extension

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CompiledExtension contributes runtime capabilities from in-process code.
type CompiledExtension interface {
	ID() string
	Register(*Registrar) error
}

// Registry stores compiled extension registrations by ID.
type Registry struct {
	mu         sync.RWMutex
	extensions map[string]CompiledExtension
}

func NewRegistry() *Registry {
	return &Registry{
		extensions: map[string]CompiledExtension{},
	}
}

func (r *Registry) Register(ext CompiledExtension) error {
	if ext == nil {
		return fmt.Errorf("compiled extension is nil")
	}
	id := strings.TrimSpace(ext.ID())
	if id == "" {
		return fmt.Errorf("compiled extension id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.extensions[id]; exists {
		return fmt.Errorf("compiled extension %q already registered", id)
	}
	r.extensions[id] = ext
	return nil
}

func (r *Registry) Get(id string) (CompiledExtension, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ext, ok := r.extensions[strings.TrimSpace(id)]
	return ext, ok
}

func (r *Registry) List() []CompiledExtension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.extensions))
	for id := range r.extensions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]CompiledExtension, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.extensions[id])
	}
	return out
}

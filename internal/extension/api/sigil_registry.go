package api

import (
	"fmt"
	"sort"
	"sync"

	"github.com/pizzaface/go-pi/internal/tui/sigils"
)

// SigilPrefixCollisionError is returned when another extension already owns
// a prefix. The registry leaves state unchanged on collision.
type SigilPrefixCollisionError struct {
	Prefix       string
	ConflictWith string
}

func (e *SigilPrefixCollisionError) Error() string {
	return fmt.Sprintf("sigil prefix %q already owned by %q", e.Prefix, e.ConflictWith)
}

// SigilRegistry maps prefix → owner extension ID. Safe for concurrent use.
type SigilRegistry struct {
	mu     sync.RWMutex
	owners map[string]string
}

func NewSigilRegistry() *SigilRegistry {
	return &SigilRegistry{owners: map[string]string{}}
}

// Add registers prefixes atomically: if any prefix is invalid or collides
// with a different owner, none are registered.
func (r *SigilRegistry) Add(owner string, prefixes []string) error {
	for _, p := range prefixes {
		if !sigils.ValidPrefix(p) {
			return fmt.Errorf("invalid sigil prefix %q", p)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range prefixes {
		if cur, ok := r.owners[p]; ok && cur != owner {
			return &SigilPrefixCollisionError{Prefix: p, ConflictWith: cur}
		}
	}
	for _, p := range prefixes {
		r.owners[p] = owner
	}
	return nil
}

// Remove drops prefixes owned by owner. Prefixes owned by a different
// extension return an error; missing prefixes are no-ops.
func (r *SigilRegistry) Remove(owner string, prefixes []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range prefixes {
		if cur, ok := r.owners[p]; ok {
			if cur != owner {
				return fmt.Errorf("sigil prefix %q owned by %q, not %q", p, cur, owner)
			}
			delete(r.owners, p)
		}
	}
	return nil
}

// RemoveAllByOwner drops every prefix owned by owner.
func (r *SigilRegistry) RemoveAllByOwner(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for p, o := range r.owners {
		if o == owner {
			delete(r.owners, p)
		}
	}
}

// Owner returns the owner of prefix or false.
func (r *SigilRegistry) Owner(prefix string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.owners[prefix]
	return o, ok
}

// List returns prefix→owner pairs sorted by prefix.
func (r *SigilRegistry) List() []SigilEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SigilEntry, 0, len(r.owners))
	for p, o := range r.owners {
		out = append(out, SigilEntry{Prefix: p, Owner: o})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// SigilEntry is one registered prefix.
type SigilEntry struct {
	Prefix string
	Owner  string
}

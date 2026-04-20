// Package compiled holds the registry of extensions linked directly into
// the go-pi binary. Each compiled-in extension calls Append from its
// package init() to add itself to the slice. BuildRuntime reads the
// slice during startup and registers every entry with the host Manager.
package compiled

import (
	"sync"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// Entry describes one compiled-in extension.
type Entry struct {
	Name     string
	Register piapi.Register
	Metadata piapi.Metadata
}

var (
	mu       sync.Mutex
	compiled []Entry
)

// Append records a compiled-in extension. Typically called from a package
// init() function.
func Append(e Entry) {
	mu.Lock()
	defer mu.Unlock()
	compiled = append(compiled, e)
}

// Compiled returns a snapshot of the registered entries.
func Compiled() []Entry {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Entry, len(compiled))
	copy(out, compiled)
	return out
}

// Reset clears the registry. Intended for tests only.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	compiled = nil
}

package api

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ReadinessState enumerates the lifecycle states a tracked extension can
// occupy while the startup barrier is waiting for it.
type ReadinessState int

const (
	// ReadinessUnknown is returned for extension IDs that have never been
	// passed to Track.
	ReadinessUnknown ReadinessState = iota
	// ReadinessLaunching indicates Track was called and no terminal
	// transition has occurred yet.
	ReadinessLaunching
	// ReadinessReady is a terminal state reached via an explicit MarkReady
	// or via quiescence (no activity for QuiescenceWindow after a Kick).
	ReadinessReady
	// ReadinessErrored is a terminal state reached via MarkErrored.
	ReadinessErrored
	// ReadinessTimedOut is a terminal state Wait promotes launching
	// entries to when its timeout elapses.
	ReadinessTimedOut
)

// String returns a lowercase label suitable for logs and diagnostics.
func (s ReadinessState) String() string {
	switch s {
	case ReadinessLaunching:
		return "launching"
	case ReadinessReady:
		return "ready"
	case ReadinessErrored:
		return "errored"
	case ReadinessTimedOut:
		return "timed_out"
	default:
		return "unknown"
	}
}

// Readiness tracks whether each launched-at-startup extension has signalled
// readiness, either explicitly (MarkReady) or implicitly (quiescence after
// Kick). It is safe for concurrent use.
type Readiness struct {
	// QuiescenceWindow is the duration of inactivity after the most recent
	// Kick required to promote a launching entry to Ready. Defaults to
	// 250ms; tests may adjust it.
	QuiescenceWindow time.Duration

	mu      sync.Mutex
	entries map[string]*readinessEntry
}

type readinessEntry struct {
	state    ReadinessState
	lastKick time.Time
	err      error
	ready    chan struct{} // closed on terminal state transition
}

// NewReadiness constructs a Readiness tracker with the default
// QuiescenceWindow of 250ms.
func NewReadiness() *Readiness {
	return &Readiness{
		QuiescenceWindow: 250 * time.Millisecond,
		entries:          map[string]*readinessEntry{},
	}
}

// Track registers extID as launching. Subsequent Kick/MarkReady/MarkErrored
// calls reference this ID. Track is idempotent; calling it for an already
// tracked ID is a no-op.
func (r *Readiness) Track(extID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[extID]; ok {
		return
	}
	r.entries[extID] = &readinessEntry{state: ReadinessLaunching, ready: make(chan struct{})}
}

// Kick records a tools.register (or similar) activity for extID and starts
// or resets the quiescence timer. It is a no-op for unknown IDs or
// extensions that are no longer launching.
func (r *Readiness) Kick(extID string) {
	r.mu.Lock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		r.mu.Unlock()
		return
	}
	e.lastKick = time.Now()
	r.mu.Unlock()
}

// MarkReady transitions extID to Ready as an explicit readiness signal. It
// is a no-op for unknown IDs or extensions already in a terminal state.
func (r *Readiness) MarkReady(extID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		return
	}
	e.state = ReadinessReady
	close(e.ready)
}

// MarkErrored transitions extID to Errored with the supplied cause. It is
// a no-op for unknown IDs or extensions already in a terminal state.
func (r *Readiness) MarkErrored(extID string, cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		return
	}
	e.state = ReadinessErrored
	e.err = cause
	close(e.ready)
}

// State returns the current ReadinessState for extID, or ReadinessUnknown
// if the ID has never been tracked.
func (r *Readiness) State(extID string) ReadinessState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[extID]; ok {
		return e.state
	}
	return ReadinessUnknown
}

// Wait blocks until every tracked extension is in a terminal state, the
// supplied context is cancelled, or timeout elapses. Quiescence promotes
// launching entries to Ready once time.Since(lastKick) >= QuiescenceWindow
// (provided at least one Kick has been recorded). Extensions that never
// Kick and never MarkReady fall through to TimedOut when timeout elapses,
// at which point Wait returns a non-nil error.
func (r *Readiness) Wait(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		r.promoteQuiescent()
		if r.allTerminal() {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			r.timeoutLaunching()
			return fmt.Errorf("readiness: timed out after %s", timeout)
		}
		pause := 50 * time.Millisecond
		if remaining < pause {
			pause = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
}

func (r *Readiness) promoteQuiescent() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, e := range r.entries {
		if e.state != ReadinessLaunching {
			continue
		}
		if !e.lastKick.IsZero() && now.Sub(e.lastKick) >= r.QuiescenceWindow {
			e.state = ReadinessReady
			close(e.ready)
		}
	}
}

func (r *Readiness) allTerminal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.state == ReadinessLaunching {
			return false
		}
	}
	return true
}

func (r *Readiness) timeoutLaunching() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.state == ReadinessLaunching {
			e.state = ReadinessTimedOut
			close(e.ready)
		}
	}
}

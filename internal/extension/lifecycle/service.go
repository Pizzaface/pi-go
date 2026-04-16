package lifecycle

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/internal/extension/host"
)

// Service is the programmatic surface for extension management. See
// docs/superpowers/specs/2026-04-15-extensions-lifecycle-tui-management-design.md
// for the full contract.
type Service interface {
	List() []View
	Get(id string) (View, bool)

	Approve(ctx context.Context, id string, grants []string) error
	Deny(ctx context.Context, id string, reason string) error
	Revoke(ctx context.Context, id string) error

	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error

	StartApproved(ctx context.Context) []error
	StopAll(ctx context.Context) []error
	Reload(ctx context.Context) error

	Subscribe() (<-chan Event, func())
}

// New constructs a Service. All mutating methods are safe for concurrent
// use. approvalsPath is the absolute path to approvals.json (may not
// exist yet). workDir is the project CWD used by Reload to re-walk
// discovery roots.
func New(mgr *host.Manager, gate *host.Gate, approvalsPath, workDir string) Service {
	s := &service{
		mgr:           mgr,
		gate:          gate,
		approvalsPath: approvalsPath,
		workDir:       workDir,
		subs:          map[int]chan Event{},
	}
	s.launchFunc = s.defaultLaunch
	s.stopFunc = s.defaultStop
	return s
}

type service struct {
	mgr           *host.Manager
	gate          *host.Gate
	approvalsPath string
	workDir       string

	writeMu sync.Mutex // serializes mutateApprovals callers

	subMu  sync.Mutex
	nextID int
	subs   map[int]chan Event

	// launchFunc is overridable for tests. In production it wraps
	// host.LaunchHosted with an api.HostedAPIHandler router.
	launchFunc func(ctx context.Context, reg *host.Registration, mgr *host.Manager, cmd []string) error
	// stopFunc is called by Stop on a running reg; overridable for tests.
	stopFunc func(ctx context.Context, reg *host.Registration) error
}

// defaultLaunch wraps host.LaunchHosted with a router backed by
// api.NewHostedHandler. Split out for test injection.
func (s *service) defaultLaunch(ctx context.Context, reg *host.Registration, mgr *host.Manager, cmd []string) error {
	handler := api.NewHostedHandler(mgr, reg)
	return host.LaunchHosted(ctx, reg, mgr, cmd, handler.Handle)
}

// defaultStop sends shutdown notification, gives 3s to react, then closes the conn.
func (s *service) defaultStop(ctx context.Context, reg *host.Registration) error {
	if reg.Conn == nil {
		return nil
	}
	_ = reg.Conn.Notify("pi.extension/shutdown", map[string]any{})
	done := make(chan struct{})
	go func() {
		t := time.NewTimer(3 * time.Second)
		defer t.Stop()
		<-t.C
		close(done)
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
	reg.Conn.Close()
	return nil
}

func (s *service) List() []View {
	regs := s.mgr.List()
	out := make([]View, 0, len(regs))
	for _, r := range regs {
		out = append(out, s.viewFromRegistration(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *service) Get(id string) (View, bool) {
	reg := s.mgr.Get(id)
	if reg == nil {
		return View{}, false
	}
	return s.viewFromRegistration(reg), true
}

func (s *service) viewFromRegistration(reg *host.Registration) View {
	granted := s.gate.Grants(reg.ID, reg.Trust)
	errMsg := ""
	if reg.Err != nil {
		errMsg = reg.Err.Error()
	}
	return View{
		ID:        reg.ID,
		Mode:      reg.Mode,
		Trust:     reg.Trust,
		State:     reg.State,
		Version:   reg.Metadata.Version,
		WorkDir:   reg.WorkDir,
		Requested: append([]string(nil), reg.Metadata.RequestedCapabilities...),
		Granted:   granted,
		Approved:  reg.State != host.StatePending && reg.State != host.StateDenied,
		Err:       errMsg,
	}
}

// Subscribe returns a buffered channel (cap 16) that receives Events,
// plus a cleanup function. The cleanup is safe to call more than once.
// Publishers drop events if the channel is full — callers needing
// stronger guarantees should call List() on a coarse trigger (e.g. the
// TUI rebase on WindowSizeMsg).
func (s *service) Subscribe() (<-chan Event, func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan Event, 16)
	s.subs[id] = ch
	cancel := func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if c, ok := s.subs[id]; ok {
			close(c)
			delete(s.subs, id)
		}
	}
	return ch, cancel
}

// publish fans out to every subscriber. Buffered subscribers that fill
// up are skipped with a warning via log.
func (s *service) publish(ev Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for id, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			log.Printf("lifecycle: dropping event for subscriber %d (channel full)", id)
		}
	}
}

// --- Approve ----------------------------------------------------------

// Approve merges grants into approvals.json and updates manager state.
// Emits EventApprovalChanged then EventStateChanged, in that order.
func (s *service) Approve(ctx context.Context, id string, grants []string) error {
	_ = ctx
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "approve", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "approve", ID: id, Err: ErrCompiledIn}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := mutateApprovals(s.approvalsPath, id, func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = true
		entry["approved_at"] = time.Now().UTC().Format(time.RFC3339)
		delete(entry, "deny_reason")
		delete(entry, "denied_at")
		if _, ok := entry["trust_class"]; !ok {
			entry["trust_class"] = "third-party"
		}
		merged := mergeStringSet(entry["granted_capabilities"], grants)
		entry["granted_capabilities"] = merged
		return entry
	})
	if err != nil {
		return &Error{Op: "approve", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "approve", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	if reg.State == host.StatePending || reg.State == host.StateDenied {
		s.mgr.SetState(id, host.StateReady, nil)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	}
	return nil
}

// mergeStringSet accepts an arbitrary existing JSON-decoded value
// (probably []any from map[string]any decoding) and merges new values
// into a dedup-sorted []any ready to re-encode.
func mergeStringSet(existing any, toAdd []string) []any {
	set := map[string]bool{}
	if arr, ok := existing.([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				set[s] = true
			}
		}
	}
	for _, s := range toAdd {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	typed := make([]any, len(out))
	for i, s := range out {
		typed[i] = s
	}
	return typed
}

// --- Deny -------------------------------------------------------------

// Deny writes approved:false + deny_reason and moves the registration
// to StateDenied. If running, Stop is called first.
func (s *service) Deny(ctx context.Context, id string, reason string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "deny", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "deny", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		if err := s.Stop(ctx, id); err != nil {
			return &Error{Op: "deny", ID: id, Err: err}
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := mutateApprovals(s.approvalsPath, id, func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = false
		entry["denied_at"] = time.Now().UTC().Format(time.RFC3339)
		entry["deny_reason"] = reason
		if _, ok := entry["trust_class"]; !ok {
			entry["trust_class"] = "third-party"
		}
		return entry
	})
	if err != nil {
		return &Error{Op: "deny", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "deny", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	s.mgr.SetState(id, host.StateDenied, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}

// --- Revoke -----------------------------------------------------------

// Revoke removes the approvals.json entry entirely and returns the
// registration to StatePending. If running, Stop is called first.
func (s *service) Revoke(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "revoke", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "revoke", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		if err := s.Stop(ctx, id); err != nil {
			return &Error{Op: "revoke", ID: id, Err: err}
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := mutateApprovals(s.approvalsPath, id, func(map[string]any) map[string]any { return nil }); err != nil {
		return &Error{Op: "revoke", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "revoke", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	s.mgr.SetState(id, host.StatePending, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}

// --- Start ------------------------------------------------------------

// Start launches a hosted extension subprocess (no-op for compiled-in,
// idempotent on running). Returns ErrUnknownExtension on unknown id.
func (s *service) Start(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "start", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "start", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		return nil
	}
	if reg.State != host.StateReady {
		return &Error{Op: "start", ID: id, Err: fmt.Errorf("cannot start from state %s", reg.State)}
	}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "start", ID: id, Err: err}
	}
	ctx2, cancel := context.WithCancel(ctx)
	go s.watchHandshakeTimeout(ctx2, id, 5*time.Second, cancel)
	if err := s.launchFunc(ctx2, reg, s.mgr, cmd); err != nil {
		cancel()
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "start", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}

// watchHandshakeTimeout polls manager state every 100ms. On StateRunning
// (handshake success) or StateErrored it returns cleanly. On timeout
// it calls cancel() and transitions to StateErrored.
func (s *service) watchHandshakeTimeout(ctx context.Context, id string, timeout time.Duration, cancel context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		reg := s.mgr.Get(id)
		if reg == nil {
			return
		}
		if reg.State == host.StateRunning || reg.State == host.StateErrored {
			return
		}
		if time.Now().After(deadline) {
			cancel()
			s.mgr.SetState(id, host.StateErrored, fmt.Errorf("handshake timeout after %s", timeout))
			s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
			return
		}
	}
}

// --- Stop -------------------------------------------------------------

// Stop sends shutdown, closes the RPC conn, and transitions to
// StateStopped. Idempotent on already-stopped/pending/ready.
func (s *service) Stop(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "stop", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "stop", ID: id, Err: ErrCompiledIn}
	}
	switch reg.State {
	case host.StateStopped, host.StatePending, host.StateReady, host.StateDenied:
		return nil
	}
	if err := s.stopFunc(ctx, reg); err != nil {
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "stop", ID: id, Err: err}
	}
	s.mgr.SetState(id, host.StateStopped, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}

// --- Restart ----------------------------------------------------------

// Restart = Stop + (ensure StateReady) + Start. Callers see a single
// operation's error shape: any failure is wrapped with Op="restart".
func (s *service) Restart(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "restart", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "restart", ID: id, Err: ErrCompiledIn}
	}
	if err := s.Stop(ctx, id); err != nil {
		return &Error{Op: "restart", ID: id, Err: err}
	}
	if s.mgr.Get(id).State == host.StateStopped {
		s.mgr.SetState(id, host.StateReady, nil)
	}
	if err := s.Start(ctx, id); err != nil {
		return &Error{Op: "restart", ID: id, Err: err}
	}
	return nil
}

// --- Reload stub filled in by Task 15 -----------------------------------

func (s *service) Reload(context.Context) error { return nil }

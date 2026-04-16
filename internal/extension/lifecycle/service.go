package lifecycle

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

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
	return &service{
		mgr:           mgr,
		gate:          gate,
		approvalsPath: approvalsPath,
		workDir:       workDir,
		subs:          map[int]chan Event{},
	}
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

// --- Stubs filled in by later tasks -----------------------------------

func (s *service) Deny(context.Context, string, string) error { return nil }
func (s *service) Revoke(context.Context, string) error       { return nil }
func (s *service) Start(context.Context, string) error        { return nil }
func (s *service) Stop(context.Context, string) error         { return nil }
func (s *service) Restart(context.Context, string) error      { return nil }
func (s *service) StartApproved(context.Context) []error      { return nil }
func (s *service) StopAll(context.Context) []error            { return nil }
func (s *service) Reload(context.Context) error               { return nil }

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// newTestService spins up a real Manager + Gate pointed at a temp
// approvals file. Returns the service and the manager so tests can
// register registrations directly.
func newTestService(t *testing.T) (Service, *host.Manager, string) {
	t.Helper()
	tmp := t.TempDir()
	approvalsPath := filepath.Join(tmp, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp)
	return svc, mgr, approvalsPath
}

func TestService_ListIncludesEveryRegistration(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	for _, reg := range []*host.Registration{
		{ID: "a", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "a", Version: "0.1"}},
		{ID: "b", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "b", Version: "0.1"}},
	} {
		if err := mgr.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	views := svc.List()
	if len(views) != 2 {
		t.Fatalf("expected 2 views; got %d", len(views))
	}
	if views[0].ID != "a" || views[1].ID != "b" {
		t.Fatalf("expected sorted a,b; got %q,%q", views[0].ID, views[1].ID)
	}
	if views[0].Mode != "compiled-in" {
		t.Fatalf("expected compiled-in mode; got %q", views[0].Mode)
	}
}

func TestService_GetMissingReturnsFalse(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, ok := svc.Get("nope"); ok {
		t.Fatal("expected not found")
	}
}

func TestService_SubscribeReceivesPublishedEvents(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	reg := &host.Registration{ID: "a", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "a", Version: "0.1"}}
	if err := mgr.Register(reg); err != nil {
		t.Fatal(err)
	}
	ch, cancel := svc.Subscribe()
	defer cancel()
	svc.(*service).publish(Event{Kind: EventStateChanged, View: View{ID: "a"}})
	select {
	case ev := <-ch:
		if ev.Kind != EventStateChanged || ev.View.ID != "a" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no event after 500ms")
	}
}

func TestService_UnsubscribeStopsDelivery(t *testing.T) {
	svc, _, _ := newTestService(t)
	ch, cancel := svc.Subscribe()
	cancel()
	svc.(*service).publish(Event{Kind: EventStateChanged})
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected channel closed or drained")
		}
	case <-time.After(100 * time.Millisecond):
		// Acceptable: no delivery.
	}
}

func TestService_ApprovePendingHostedGoesReady(t *testing.T) {
	svc, mgr, path := newTestService(t)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	if err := mgr.Register(reg); err != nil {
		t.Fatal(err)
	}
	if reg.State != host.StatePending {
		t.Fatalf("expected StatePending; got %s", reg.State)
	}
	ch, cancel := svc.Subscribe()
	defer cancel()
	if err := svc.Approve(context.Background(), "h", []string{"tools.register", "events.session_start"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	events := drainEvents(t, ch, 2, 500*time.Millisecond)
	if events[0].Kind != EventApprovalChanged {
		t.Fatalf("first event %s; expected approval_changed", events[0].Kind)
	}
	if events[1].Kind != EventStateChanged {
		t.Fatalf("second event %s; expected state_changed", events[1].Kind)
	}
	if r := mgr.Get("h"); r.State != host.StateReady {
		t.Fatalf("expected StateReady; got %s", r.State)
	}
	got, _ := readApprovals(path)
	if _, ok := got.Extensions["h"]; !ok {
		t.Fatalf("expected h in approvals file")
	}
}

func TestService_ApproveCompiledInReturnsErrCompiledIn(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	reg := &host.Registration{ID: "c", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "c", Version: "0.1"}}
	_ = mgr.Register(reg)
	err := svc.Approve(context.Background(), "c", []string{"tools.register"})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *lifecycle.Error; got %T %v", err, err)
	}
	if !errors.Is(err, ErrCompiledIn) {
		t.Fatalf("expected ErrCompiledIn; got %v", err)
	}
}

func TestService_ApproveUnknownReturnsErrUnknown(t *testing.T) {
	svc, _, _ := newTestService(t)
	err := svc.Approve(context.Background(), "no-such", nil)
	if !errors.Is(err, ErrUnknownExtension) {
		t.Fatalf("expected ErrUnknownExtension; got %v", err)
	}
}

// drainEvents reads exactly n events from ch within total time, fatal otherwise.
func drainEvents(t *testing.T, ch <-chan Event, n int, total time.Duration) []Event {
	t.Helper()
	deadline := time.Now().Add(total)
	out := make([]Event, 0, n)
	for len(out) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for %d events; got %d", n, len(out))
		}
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-time.After(remaining):
			t.Fatalf("timed out waiting for %d events; got %d", n, len(out))
		}
	}
	return out
}

func TestService_DenyTransitionsState(t *testing.T) {
	svc, mgr, path := newTestService(t)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	_ = mgr.Register(reg)
	if err := svc.Deny(context.Background(), "h", "security review failed"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if mgr.Get("h").State != host.StateDenied {
		t.Fatalf("expected StateDenied; got %s", mgr.Get("h").State)
	}
	got, _ := readApprovals(path)
	var entry map[string]any
	_ = json.Unmarshal(got.Extensions["h"], &entry)
	if entry["approved"] != false {
		t.Fatalf("expected approved:false; got %v", entry["approved"])
	}
	if entry["deny_reason"] != "security review failed" {
		t.Fatalf("unexpected deny_reason: %v", entry["deny_reason"])
	}
}

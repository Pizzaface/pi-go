package lifecycle

import (
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

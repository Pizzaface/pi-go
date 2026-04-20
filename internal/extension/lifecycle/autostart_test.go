package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestStartApproved_LaunchesEveryReadyHosted(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	for _, id := range []string{"a", "b", "c"} {
		reg := &host.Registration{ID: id, Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: id, Version: "0.1", Command: []string{"echo"}}}
		_ = mgr.Register(reg)
		_ = svc.Approve(context.Background(), id, []string{"tools.register"})
	}
	var count int64
	impl.launchFunc = func(_ context.Context, reg *host.Registration, _ *host.Manager, _ []string) error {
		atomic.AddInt64(&count, 1)
		mgr.SetState(reg.ID, host.StateRunning, nil)
		return nil
	}
	errs := svc.StartApproved(context.Background())
	if len(errs) != 0 {
		t.Fatalf("expected no errors; got %v", errs)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt64(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&count); got != 3 {
		t.Fatalf("expected 3 launches; got %d", got)
	}
}

func TestStartApproved_SkipsCompiledInAndPending(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	_ = mgr.Register(&host.Registration{ID: "compiled", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "compiled", Version: "0.1"}})
	_ = mgr.Register(&host.Registration{ID: "pending", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "pending", Version: "0.1"}})
	called := false
	impl.launchFunc = func(context.Context, *host.Registration, *host.Manager, []string) error {
		called = true
		return nil
	}
	svc.StartApproved(context.Background())
	time.Sleep(100 * time.Millisecond)
	if called {
		t.Fatal("launchFunc should not fire for compiled-in or pending extensions")
	}
}

func TestStopAll_StopsEveryRunning(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	for _, id := range []string{"a", "b"} {
		reg := &host.Registration{ID: id, Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: id, Version: "0.1"}}
		_ = mgr.Register(reg)
		_ = svc.Approve(context.Background(), id, []string{"tools.register"})
		mgr.SetState(id, host.StateRunning, nil)
	}
	var stops int64
	impl.stopFunc = func(context.Context, *host.Registration) error {
		atomic.AddInt64(&stops, 1)
		return nil
	}
	errs := svc.StopAll(context.Background())
	if len(errs) != 0 {
		t.Fatalf("expected no errors; got %v", errs)
	}
	if got := atomic.LoadInt64(&stops); got != 2 {
		t.Fatalf("expected 2 stops; got %d", got)
	}
}

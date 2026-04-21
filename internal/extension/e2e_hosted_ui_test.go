package extension

import (
	"context"
	"testing"
	"time"
)

func TestE2E_HostedUI_StatusWidgetNotifyDialog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ui E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "ui")
	rt, _, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	if got := rt.UIService.Status("hosted-surface-fixture"); got != "fixture-status" {
		t.Fatalf("status = %q", got)
	}
	if ws := rt.UIService.Widgets("hosted-surface-fixture"); len(ws) != 1 || ws[0].ID != "w1" {
		t.Fatalf("widgets = %+v", ws)
	}
	if active := rt.UIService.ActiveDialog(); active == nil {
		t.Fatalf("no dialog enqueued")
	}
}

func TestE2E_HostedUI_DialogResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ui E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "ui")
	rt, _, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	active := rt.UIService.ActiveDialog()
	if active == nil {
		t.Fatalf("no active dialog")
	}
	resolution, ok := rt.UIService.ResolveDialog(active.ID, map[string]any{"x": 1}, false, "ok")
	if !ok {
		t.Fatalf("resolve failed")
	}
	if resolution.DialogID != active.ID {
		t.Fatalf("resolution id mismatch: %q vs %q", resolution.DialogID, active.ID)
	}
}

package extension

import (
	"context"
	"testing"
	"time"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
)

// TestE2E_SlowReadyUsesExplicitSignal proves Ready() — not inactivity —
// is what unblocks WaitForHostedReady. The fixture sleeps 300ms before
// registering, and we tighten QuiescenceWindow to 50ms so a
// quiescence-only fallback would race with (and potentially miss) the
// registration.
func TestE2E_SlowReadyUsesExplicitSignal(t *testing.T) {
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_slow_ready.json",
		"hosted-slow-ready-go")
	defer cleanup()

	// Tighten quiescence so any missing Ready() would result in a
	// launching-state timeout rather than accidental promotion.
	rt.Readiness.QuiescenceWindow = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if got := rt.Readiness.State("hosted-slow-ready-go"); got != extapi.ReadinessReady {
		t.Fatalf("state = %v; want ReadinessReady", got)
	}
	snap := rt.HostedToolRegistry.Snapshot()
	if len(snap) != 1 || snap[0].Desc.Name != "slow_greet" {
		t.Fatalf("snapshot = %+v; want exactly slow_greet", snap)
	}
}

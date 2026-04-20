package extension

import (
	"context"
	"testing"
	"time"
)

// TestE2E_DynamicApproval covers approve→start→revoke transitions
// mid-session: the registry stays empty until approval lands, fills
// when the extension comes up, then clears again when revoked.
func TestE2E_DynamicApproval(t *testing.T) {
	// Use the empty approvals fixture — the extension is present on
	// disk but unapproved, so BuildRuntime leaves it in StatePending.
	rt, cleanup := setupHostedFixtures(t, "approvals_empty.json",
		"hosted-hello-go")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Nothing tracked (StatePending extensions aren't added to Readiness),
	// so Wait returns immediately.
	if err := rt.WaitForHostedReady(ctx, 1*time.Second); err != nil {
		t.Fatalf("initial ready: %v", err)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("expected empty registry, got %d", n)
	}

	if err := rt.Lifecycle.Approve(ctx, "hosted-hello-go", []string{
		"tools.register", "events.session_start", "events.tool_execute",
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := rt.Lifecycle.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("post-approve snapshot = %d", n)
	}

	if err := rt.Lifecycle.Revoke(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("post-revoke snapshot = %d", n)
	}
}

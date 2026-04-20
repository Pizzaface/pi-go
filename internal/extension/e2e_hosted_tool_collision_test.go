package extension

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
)

// TestE2E_ToolNameCollision brings up two hosted extensions that both try
// to register the tool name "greet". The registry must accept exactly one
// (the first arrival — hosted-hello-go), reject the second with a
// CollisionError, and emit a ChangeCollisionRejected notification.
func TestE2E_ToolNameCollision(t *testing.T) {
	// Subscribe BEFORE StartApproved fires so we don't miss the event.
	// setupHostedFixtures starts the extensions immediately, so we need
	// another path: build the runtime ourselves or snapshot-poll. Polling
	// registry state is sufficient — the collision is recorded as a
	// persistent registry fact (winner stays; loser is absent).
	var seenCollision atomic.Bool
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_collide.json",
		"hosted-hello-go", "hosted-collide")
	defer cleanup()

	rt.HostedToolRegistry.OnChange(func(c extapi.Change) {
		if c.Kind == extapi.ChangeCollisionRejected {
			seenCollision.Store(true)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 15*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}

	snap := rt.HostedToolRegistry.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot size = %d; want 1 winner: %+v", len(snap), snap)
	}
	if snap[0].ExtID != "hosted-hello-go" {
		t.Fatalf("winner = %q; want hosted-hello-go", snap[0].ExtID)
	}

	// The collision fixture surfaces rejection via the registry; restart to
	// re-trigger the attempt and confirm the OnChange path fires now that we
	// have a live subscriber.
	if err := rt.Lifecycle.Restart(ctx, "hosted-collide"); err != nil {
		t.Fatalf("restart collide: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !seenCollision.Load() {
		time.Sleep(50 * time.Millisecond)
	}
	if !seenCollision.Load() {
		t.Fatal("collision change not observed")
	}
}

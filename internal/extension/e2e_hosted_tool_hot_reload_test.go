package extension

import (
	"context"
	"testing"
	"time"
)

// TestE2E_HotReload verifies that stopping and re-starting a hosted
// extension removes and then re-adds its tools in the HostedToolRegistry.
func TestE2E_HotReload(t *testing.T) {
	rt, cleanup := setupHostedHelloGo(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 15*time.Second); err != nil {
		t.Fatalf("initial ready: %v", err)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("initial snapshot size = %d", n)
	}

	if err := rt.Lifecycle.Stop(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("post-stop snapshot = %d", n)
	}

	if err := rt.Lifecycle.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("post-restart snapshot = %d", n)
	}
}

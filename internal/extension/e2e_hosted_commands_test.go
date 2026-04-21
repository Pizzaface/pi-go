package extension

import (
	"context"
	"testing"
	"time"
)

func TestE2E_HostedCommands_RegisterAndInvoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping commands E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "commands")
	rt, _, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	entries := rt.CommandRegistry.List()
	found := false
	for _, e := range entries {
		if e.Spec.Name == "fixture-cmd" && e.Owner == "hosted-surface-fixture" && e.Source == "runtime" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture-cmd missing from registry: %+v", entries)
	}

	res, err := rt.CommandRegistry.Invoke(ctx, "fixture-cmd", "hello", "entry-1")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !res.Handled || res.Message != "invoked:hello" {
		t.Fatalf("result = %+v", res)
	}
}

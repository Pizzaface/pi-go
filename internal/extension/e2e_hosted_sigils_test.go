package extension

import (
	"context"
	"testing"
	"time"
)

func TestE2E_HostedSigils_RegisterAndCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sigils E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "sigils")
	rt, _, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	for _, p := range []string{"fix", "fixture"} {
		o, ok := rt.SigilRegistry.Owner(p)
		if !ok || o != "hosted-surface-fixture" {
			t.Fatalf("prefix %q owner=%q ok=%v", p, o, ok)
		}
	}

	if err := rt.SigilRegistry.Add("other-ext", []string{"fix"}); err == nil {
		t.Fatalf("expected collision error on overlapping prefix")
	}
}

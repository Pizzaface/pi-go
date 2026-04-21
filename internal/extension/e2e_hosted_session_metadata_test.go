package extension

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestE2E_HostedSessionMetadata_NameAndTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping session-metadata E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "session")
	rt, _, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	if rt.Bridge == nil {
		t.Fatal("runtime Bridge is nil; setupSurfaceFixture should wire one")
	}
	meta := rt.Bridge.GetSessionMetadata()
	if meta.Name != "fixture-branch" {
		t.Fatalf("name = %q; want fixture-branch", meta.Name)
	}
	if !reflect.DeepEqual(meta.Tags, []string{"one", "two"}) {
		t.Fatalf("tags = %+v", meta.Tags)
	}
}

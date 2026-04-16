package extension

import (
	"context"
	"testing"
)

func TestBuildRuntime_ProvidesLifecycle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if rt.Lifecycle == nil {
		t.Fatal("expected rt.Lifecycle to be non-nil")
	}
	if len(rt.Lifecycle.List()) == 0 {
		t.Fatal("expected at least the compiled-in hello extension in Lifecycle.List()")
	}
}

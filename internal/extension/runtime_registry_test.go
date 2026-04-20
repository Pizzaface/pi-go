package extension

import (
	"context"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/api"
)

func TestBuildRuntime_HostedToolRegistryPresent(t *testing.T) {
	tmp := t.TempDir()
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if rt.HostedToolRegistry == nil {
		t.Fatal("expected HostedToolRegistry on Runtime")
	}
	if rt.Readiness == nil {
		t.Fatal("expected Readiness on Runtime")
	}
	if len(rt.Toolsets) == 0 {
		t.Fatal("expected at least the HostedToolset in Toolsets")
	}
	found := false
	for _, ts := range rt.Toolsets {
		if _, ok := ts.(*api.HostedToolset); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("HostedToolset not in Toolsets slice")
	}
}

func TestRuntime_WaitForHostedReady_NoExtensions(t *testing.T) {
	tmp := t.TempDir()
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	// With no extensions tracked, Wait returns immediately (nil).
	if err := rt.WaitForHostedReady(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}
}

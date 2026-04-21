package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E_HostedState_SetPatchReadsFromDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping state E2E under -short")
	}
	t.Setenv("PI_SURFACE_MODE", "state")
	rt, sessionID, cleanup := setupSurfaceFixture(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	tmp := os.Getenv("HOME")
	blob := filepath.Join(tmp, ".go-pi", "sessions", sessionID, "state", "extensions", "hosted-surface-fixture.json")
	data, err := os.ReadFile(blob)
	if err != nil {
		t.Fatalf("read blob %s: %v", blob, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", blob, err)
	}
	if v, _ := m["count"].(float64); v != 2 {
		t.Fatalf("patch not applied: count=%v", m["count"])
	}
	if v, _ := m["note"].(string); v != "hi" {
		t.Fatalf("patch note missing: %v", m["note"])
	}
}

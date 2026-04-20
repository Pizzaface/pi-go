package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/host"
)

// TestE2E_HostedTool_Invocation launches hosted-hello-go via the full
// BuildRuntime + lifecycle path, waits for pi.Ready(), and asserts that
// the greet tool lands in the HostedToolRegistry and the tool adapter
// builds — the exact surface the agent consumes.
func TestE2E_HostedTool_Invocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-tool E2E under -short")
	}
	projectRoot, err := repoRoot()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	exampleDir := filepath.Join(projectRoot, "examples", "extensions", "hosted-hello-go")
	if _, err := os.Stat(filepath.Join(exampleDir, "main.go")); err != nil {
		t.Skipf("hosted-hello-go example missing: %v", err)
	}

	tmp := t.TempDir()
	extsDir := filepath.Join(tmp, ".go-pi", "extensions")
	if err := os.MkdirAll(extsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(extsDir, "hosted-hello-go")
	if err := os.Symlink(exampleDir, target); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}

	approvalsSrc := filepath.Join("testdata", "approvals_granted_hello.json")
	approvalsData, err := os.ReadFile(approvalsSrc)
	if err != nil {
		t.Fatalf("read approvals fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvalsData, 0o644); err != nil {
		t.Fatalf("write approvals: %v", err)
	}

	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := BuildRuntime(ctx, RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(func() { rt.Manager.Shutdown(context.Background()) })

	reg := rt.Manager.Get("hosted-hello-go")
	if reg == nil || reg.State != host.StateReady {
		t.Fatalf("expected StateReady; got %+v", reg)
	}

	if errs := rt.Lifecycle.StartApproved(ctx); len(errs) > 0 {
		t.Fatalf("StartApproved: %v", errs)
	}
	if err := rt.WaitForHostedReady(ctx, 15*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	snap := rt.HostedToolRegistry.Snapshot()
	if len(snap) != 1 || snap[0].Desc.Name != "greet" {
		t.Fatalf("registry snapshot = %+v", snap)
	}

	// Adapter build-through: exercises the exact construction the agent
	// hits when resolving hosted tools from the registry.
	if _, err := extapi.NewHostedToolAdapter(snap[0]); err != nil {
		t.Fatalf("NewHostedToolAdapter: %v", err)
	}
}

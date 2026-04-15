package extension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/internal/extension/host"
)

// TestE2E_HostedGo exercises discovery → gate approval → BuildRuntime
// → LaunchHosted end-to-end with the real hosted-hello-go example
// binary and a `go run .` child process. It asserts that the
// extension's registration reaches StateRunning after the handshake
// completes.
func TestE2E_HostedGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-go E2E under -short")
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
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(extsDir, "hosted-hello-go")
	if err := os.Symlink(exampleDir, target); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}

	// Copy approvals into the extensions dir.
	approvalsSrc := filepath.Join("testdata", "approvals_granted_hello.json")
	approvalsData, err := os.ReadFile(approvalsSrc)
	if err != nil {
		t.Fatalf("read approvals fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvalsData, 0644); err != nil {
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
	reg := rt.Manager.Get("hosted-hello-go")
	if reg == nil {
		t.Fatalf("expected hosted-hello-go registration; got %v", extIDs(rt))
	}
	if reg.State != host.StateReady {
		t.Fatalf("expected StateReady after approved discovery; got %s", reg.State)
	}

	handler := extapi.NewHostedHandler(rt.Manager, reg)
	if err := host.LaunchHosted(ctx, reg, rt.Manager, []string{"go", "run", "."}, handler.Handle); err != nil {
		t.Fatalf("LaunchHosted: %v", err)
	}
	// Let Go start the child, finish compilation, and send its handshake.
	time.Sleep(1500 * time.Millisecond)

	reg = rt.Manager.Get("hosted-hello-go")
	if reg.State != host.StateRunning {
		t.Fatalf("expected StateRunning after handshake; got %s (err=%v)", reg.State, reg.Err)
	}

	rt.Manager.Shutdown(ctx)

	reg = rt.Manager.Get("hosted-hello-go")
	if reg.State != host.StateStopped {
		t.Fatalf("expected StateStopped after Shutdown; got %s", reg.State)
	}
}

// repoRoot walks up from this test file to find the repo root (the
// directory containing go.mod at the top level).
func repoRoot() (string, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

func extIDs(rt *Runtime) []string {
	out := make([]string, 0, len(rt.Extensions))
	for _, r := range rt.Extensions {
		out = append(out, r.ID)
	}
	return out
}

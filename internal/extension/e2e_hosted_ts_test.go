package extension

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/internal/extension/host"
)

// TestE2E_HostedTS mirrors TestE2E_HostedGo for the TypeScript fixture.
// It extracts the vendored pi-go-extension-host bundle, points it at
// the absolute entry path, and asserts the hosted registration reaches
// StateRunning after the handshake completes. Skips when:
//   - node is not on PATH,
//   - node_modules have not been vendored (npm install in the example
//     directory is a prerequisite),
//   - symlink creation is unavailable.
func TestE2E_HostedTS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-ts E2E under -short")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	projectRoot, err := repoRoot()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	exampleDir := filepath.Join(projectRoot, "examples", "extensions", "hosted-hello-ts")
	entry := filepath.Join(exampleDir, "src", "index.ts")
	if _, err := os.Stat(entry); err != nil {
		t.Skipf("hosted-hello-ts example missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exampleDir, "node_modules", "@pi-go", "extension-sdk")); err != nil {
		t.Skipf("hosted-hello-ts node_modules missing; run npm install in %s", exampleDir)
	}

	tmp := t.TempDir()
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exampleDir, filepath.Join(extsDir, "hosted-hello-ts")); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}

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
	reg := rt.Manager.Get("hosted-hello-ts")
	if reg == nil {
		t.Fatalf("expected hosted-hello-ts registration; got %v", extIDs(rt))
	}
	if reg.State != host.StateReady {
		t.Fatalf("expected StateReady after approved discovery; got %s", reg.State)
	}

	hostPath, err := host.ExtractedHostPath("test")
	if err != nil {
		t.Fatalf("extract host bundle: %v", err)
	}
	absEntry, err := filepath.Abs(entry)
	if err != nil {
		t.Fatalf("abs entry: %v", err)
	}

	handler := extapi.NewHostedHandler(rt.Manager, reg, nil)
	cmd := []string{"node", hostPath, "--entry", absEntry, "--name", "hosted-hello-ts"}
	if err := host.LaunchHosted(ctx, reg, rt.Manager, cmd, handler.Handle); err != nil {
		t.Fatalf("LaunchHosted: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)

	reg = rt.Manager.Get("hosted-hello-ts")
	if reg.State != host.StateRunning {
		t.Fatalf("expected StateRunning after handshake; got %s (err=%v)", reg.State, reg.Err)
	}

	rt.Manager.Shutdown(ctx)
}

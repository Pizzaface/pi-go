package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/internal/extension/host"
)

// TestE2E_HostedGo_Spec5 verifies that a hosted Go extension can call
// AppendEntry and emit a log.append (via piext.Log()) and that a FakeBridge
// captures both calls end-to-end.
//
// Strategy: Option 2 — the probe fires bridge calls directly from Register()
// when PI_SPEC5_PROBE=1, immediately during the handshake/registration phase.
// This avoids needing to invoke a tool through the agent loop (which would
// require full session machinery). The round-trip is: extension process →
// stdio JSON-RPC → HostedAPIHandler.Handle → FakeBridge methods.
func TestE2E_HostedGo_Spec5(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-go spec#5 E2E under -short")
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

	// Use spec#5 approvals that grant session.append_entry and log.append.
	approvalsSrc := filepath.Join("testdata", "approvals_granted_hello_spec5.json")
	approvalsData, err := os.ReadFile(approvalsSrc)
	if err != nil {
		t.Fatalf("read spec5 approvals fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvalsData, 0644); err != nil {
		t.Fatalf("write approvals: %v", err)
	}

	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PI_SPEC5_PROBE", "1")

	fb := &testbridge.FakeBridge{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := BuildRuntime(ctx, RuntimeConfig{WorkDir: tmp, Bridge: fb})
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

	handler := extapi.NewHostedHandler(rt.Manager, reg, fb)
	if err := host.LaunchHosted(ctx, reg, rt.Manager, []string{"go", "run", "."}, handler.Handle); err != nil {
		t.Fatalf("LaunchHosted: %v", err)
	}

	// Wait for the child process to compile, handshake, and complete Register()
	// (which fires the probe calls). The probe fires before the extension blocks
	// on shutdown, so we just need to wait long enough for compilation.
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		reg = rt.Manager.Get("hosted-hello-go")
		if reg.State == host.StateRunning {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	reg = rt.Manager.Get("hosted-hello-go")
	if reg.State != host.StateRunning {
		t.Fatalf("expected StateRunning after handshake; got %s (err=%v)", reg.State, reg.Err)
	}

	// Give the probe calls (which run during Register) a moment to propagate
	// through the RPC layer to the FakeBridge. The AppendEntry is a Call so it
	// must complete before Register returns; the log.append is a Notify so it
	// may still be in-flight. Allow a short grace period.
	time.Sleep(300 * time.Millisecond)

	rt.Manager.Shutdown(ctx)

	// Assert AppendEntry was called by the probe. Shutdown() has closed the
	// extension connection so no further bridge calls can arrive.
	calls := fb.Snapshot()
	var sawAppendEntry, sawAppendLog bool
	for _, c := range calls {
		switch c.Method {
		case "AppendEntry":
			sawAppendEntry = true
		case "AppendExtensionLog":
			sawAppendLog = true
		}
	}
	if !sawAppendEntry {
		t.Errorf("expected FakeBridge.AppendEntry call from spec5 probe; got calls: %+v", calls)
	}
	if !sawAppendLog {
		t.Errorf("expected FakeBridge.AppendExtensionLog call from spec5 probe log; got calls: %+v", calls)
	}
}

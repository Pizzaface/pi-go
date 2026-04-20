package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/host"
)

func TestLifecycleE2E_HostedTS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-ts E2E under -short")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not on PATH: %v", err)
	}
	root, err := repoRootFromHere()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	example := filepath.Join(root, "examples", "extensions", "hosted-hello-ts")
	if _, err := os.Stat(filepath.Join(example, "src", "index.ts")); err != nil {
		t.Skipf("hosted-hello-ts example missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(example, "node_modules", "@go-pi", "extension-sdk")); err != nil {
		t.Skipf("run `npm install` in %s first", example)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	extsDir := filepath.Join(tmp, ".go-pi", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(example, filepath.Join(extsDir, "hosted-hello-ts")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	approvalsPath := filepath.Join(extsDir, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp, nil)

	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Get("hosted-hello-ts"); !ok {
		t.Fatal("expected hosted-hello-ts")
	}
	if err := svc.Approve(context.Background(), "hosted-hello-ts", []string{"tools.register", "events.session_start", "events.tool_execute"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Start(ctx, "hosted-hello-ts"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := svc.Get("hosted-hello-ts"); v.State == host.StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v, _ := svc.Get("hosted-hello-ts"); v.State != host.StateRunning {
		t.Fatalf("expected StateRunning; got %s (err=%s)", v.State, v.Err)
	}
	_ = svc.Stop(ctx, "hosted-hello-ts")
}

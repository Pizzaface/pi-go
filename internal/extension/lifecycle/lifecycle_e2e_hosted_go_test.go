package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestLifecycleE2E_HostedGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-go E2E under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not on PATH: %v", err)
	}
	root, err := repoRootFromHere()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	example := filepath.Join(root, "examples", "extensions", "hosted-hello-go")
	if _, err := os.Stat(filepath.Join(example, "main.go")); err != nil {
		t.Skipf("hosted-hello-go example missing: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(example, filepath.Join(extsDir, "hosted-hello-go")); err != nil {
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
	if _, ok := svc.Get("hosted-hello-go"); !ok {
		t.Fatal("expected hosted-hello-go in Service after Reload")
	}
	if err := svc.Approve(context.Background(), "hosted-hello-go", []string{"tools.register", "events.session_start", "events.tool_execute"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateReady {
		t.Fatalf("expected StateReady after Approve; got %s", v.State)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := svc.Get("hosted-hello-go"); v.State == host.StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateRunning {
		t.Fatalf("expected StateRunning; got %s (err=%s)", v.State, v.Err)
	}
	if err := svc.Stop(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateStopped {
		t.Fatalf("expected StateStopped; got %s", v.State)
	}
}

// repoRootFromHere finds the project root by walking up until we hit go.work.
func repoRootFromHere() (string, error) {
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
	_ = piapi.Metadata{} // keep piapi import alive
	return "", os.ErrNotExist
}

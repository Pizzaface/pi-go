package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupHostedHelloGo builds a Runtime wired to a symlinked hosted-hello-go
// fixture with the standard approvals file. It calls StartApproved so the
// extension is launching when the helper returns. The cleanup func stops
// the manager. On Windows without developer mode, the symlink step fails
// and the test is skipped — matching the sibling e2e tests.
func setupHostedHelloGo(t *testing.T) (*Runtime, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping hosted-hello-go setup under -short")
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
	if err := os.Symlink(exampleDir, filepath.Join(extsDir, "hosted-hello-go")); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}

	approvalsSrc := filepath.Join("testdata", "approvals_granted_hello.json")
	data, err := os.ReadFile(approvalsSrc)
	if err != nil {
		t.Fatalf("read approvals fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), data, 0o644); err != nil {
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
	if errs := rt.Lifecycle.StartApproved(ctx); len(errs) > 0 {
		t.Fatalf("StartApproved: %v", errs)
	}
	cleanup := func() { rt.Manager.Shutdown(context.Background()) }
	return rt, cleanup
}

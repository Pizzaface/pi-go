package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/adk/session"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	pisession "github.com/pizzaface/go-pi/internal/session"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// setupSurfaceFixture builds a Runtime wired with the hosted-surface-fixture
// and a state-bound StateStore + FileService-backed bridge. Returns the
// runtime, the session ID, and a cleanup func. Skips on Windows without
// symlink permission.
func setupSurfaceFixture(t *testing.T) (*Runtime, string, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping hosted-surface-fixture setup under -short")
	}
	projectRoot, err := repoRoot()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	srcDir := filepath.Join(projectRoot, "internal", "extension", "testdata", "hosted-surface-fixture")
	if _, err := os.Stat(filepath.Join(srcDir, "main.go")); err != nil {
		t.Skipf("hosted-surface-fixture missing: %v", err)
	}

	tmp := t.TempDir()
	extsDir := filepath.Join(tmp, ".go-pi", "extensions")
	if err := os.MkdirAll(extsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(srcDir, filepath.Join(extsDir, "hosted-surface-fixture")); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}
	approvals, err := os.ReadFile(filepath.Join("testdata", "approvals_granted_surface.json"))
	if err != nil {
		t.Fatalf("read approvals: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvals, 0o644); err != nil {
		t.Fatalf("write approvals: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	sessionsDir := filepath.Join(tmp, ".go-pi", "sessions")
	fsvc, err := pisession.NewFileService(sessionsDir)
	if err != nil {
		t.Fatalf("NewFileService: %v", err)
	}
	resp, err := fsvc.Create(context.Background(), &session.CreateRequest{AppName: "go-pi", UserID: "test"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionID := resp.Session.ID()

	bridge := &fileServiceBridge{fs: fsvc, sessionID: sessionID, appName: "go-pi", userID: "test"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := BuildRuntime(ctx, RuntimeConfig{
		WorkDir:     tmp,
		Bridge:      bridge,
		SessionsDir: sessionsDir,
		SessionID:   sessionID,
	})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if errs := rt.Lifecycle.StartApproved(ctx); len(errs) > 0 {
		t.Fatalf("StartApproved: %v", errs)
	}
	cleanup := func() { rt.Manager.Shutdown(context.Background()) }
	return rt, sessionID, cleanup
}

// fileServiceBridge is a SessionBridge backed by a real FileService for
// session-metadata round-trip testing. All other methods are no-ops.
type fileServiceBridge struct {
	extapi.NoopBridge
	fs        *pisession.FileService
	sessionID string
	appName   string
	userID    string
}

func (b *fileServiceBridge) GetSessionMetadata() extapi.SessionMetadata {
	m, err := b.fs.GetMetadata(b.sessionID, b.appName, b.userID)
	if err != nil {
		return extapi.SessionMetadata{}
	}
	return extapi.SessionMetadata{
		Name: m.Name, Title: m.Title, Tags: m.Tags,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
}
func (b *fileServiceBridge) SetSessionName(name string) error {
	return b.fs.SetName(b.sessionID, b.appName, b.userID, name)
}
func (b *fileServiceBridge) SetSessionTags(tags []string) error {
	return b.fs.SetTags(b.sessionID, b.appName, b.userID, tags)
}

// ensure the piapi import stays live across platforms that skip this file's tests.
var _ = piapi.EventSessionStart

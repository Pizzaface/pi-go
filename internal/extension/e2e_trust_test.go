package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// TestE2E_CompiledInBypassesGate asserts that a compiled-in extension
// reaches StateReady even when approvals.json is absent. Compiled-in
// trust is implicit and bypasses the capability gate entirely.
func TestE2E_CompiledInBypassesGate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	reg := rt.Manager.Get("hello")
	if reg == nil {
		t.Fatalf("expected hello registration; got %v", extIDs(rt))
	}
	if reg.State != host.StateReady {
		t.Fatalf("compiled-in must be StateReady without approvals.json; got %s", reg.State)
	}
}

// TestE2E_HostedWithoutApprovalPending asserts that a discovered
// hosted extension with no corresponding approvals entry sits in
// StatePending rather than being auto-approved.
func TestE2E_HostedWithoutApprovalPending(t *testing.T) {
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
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exampleDir, filepath.Join(extsDir, "hosted-hello-go")); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}
	// No approvals.json file: the gate starts empty, every hosted
	// capability is denied, so Register → StatePending.

	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	reg := rt.Manager.Get("hosted-hello-go")
	if reg == nil {
		t.Fatalf("expected hosted-hello-go registration; got %v", extIDs(rt))
	}
	if reg.State != host.StatePending {
		t.Fatalf("expected StatePending without approval; got %s", reg.State)
	}
}

// TestE2E_ProtocolDowngrade asserts the host's handshake builder
// rejects a request with a mismatched protocol_version by returning an
// error carrying hostproto.ErrCodeHandshakeFailed. Running the builder
// directly (rather than through a spawned subprocess) keeps this
// check platform-independent.
func TestE2E_ProtocolDowngrade(t *testing.T) {
	gate, err := host.NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	manager := host.NewManager(gate)
	reg := &host.Registration{
		ID:       "third-party",
		Trust:    host.TrustThirdParty,
		Metadata: piapi.Metadata{Name: "third-party", Version: "0.1"},
	}
	if err := manager.Register(reg); err != nil {
		t.Fatal(err)
	}

	params, _ := json.Marshal(hostproto.HandshakeRequest{
		ProtocolVersion: "2.0",
		ExtensionID:     reg.ID,
	})

	_, hsErr := host.BuildHandshakeResponse(reg, manager, params)
	if hsErr == nil {
		t.Fatal("expected handshake rejection for protocol_version=2.0")
	}
	code, msg := host.HandshakeErrorCode(hsErr)
	if code != hostproto.ErrCodeHandshakeFailed {
		t.Fatalf("expected ErrCodeHandshakeFailed (%d); got %d", hostproto.ErrCodeHandshakeFailed, code)
	}
	if !strings.Contains(msg, "protocol_version") {
		t.Fatalf("expected message to mention protocol_version; got %q", msg)
	}
}

package extension_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/tools"
)

// TestHostedHello_V2_EndToEnd spins up the manager with an in-process
// fake extension that mimics the v2 hosted-hello flow: handshake,
// commands.register, ui.status, then EOF. It verifies the manager
// routes both registrations to the expected destinations (commands
// registry + UI intent fan-out). Pure Go (no shell scripts), so it
// runs on all platforms.
func TestHostedHello_V2_EndToEnd(t *testing.T) {
	mgr := extension.NewManager(extension.ManagerOptions{
		Permissions: extension.NewPermissions([]extension.ApprovalRecord{{
			ExtensionID:    "hosted-hello",
			TrustClass:     extension.TrustClassHostedThirdParty,
			HostedRequired: true,
			GrantedCapabilities: []extension.Capability{
				extension.CapabilityUIStatus,
				extension.CapabilityCommandRegister,
			},
		}}),
		HostedLauncher: pipeLauncher{},
	})

	manifest := extension.Manifest{
		Name: "hosted-hello",
		Capabilities: []extension.Capability{
			extension.CapabilityUIStatus,
			extension.CapabilityCommandRegister,
		},
		Runtime: extension.RuntimeSpec{
			Type:    extension.RuntimeTypeHostedStdioJSONRPC,
			Command: "fake",
		},
	}
	if err := mgr.RegisterManifest(manifest); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}

	sub, cancel := mgr.SubscribeUIIntents(8)
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCtx()
	if err := mgr.StartHostedExtensions(ctx, "hosted_stdio"); err != nil {
		t.Fatalf("StartHostedExtensions: %v", err)
	}

	// Wait for the fake client to send both host_calls.
	deadline := time.After(2 * time.Second)
	var gotStatus bool
	for !gotStatus {
		select {
		case envelope := <-sub:
			if envelope.ExtensionID == "hosted-hello" && envelope.Intent.Status != nil && envelope.Intent.Status.Text == "hosted-hello connected" {
				gotStatus = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for ui.status intent")
		}
	}

	// Command should be registered.
	time.Sleep(50 * time.Millisecond)
	if _, ok := mgr.FindCommand("hello"); !ok {
		t.Fatal("hello command was not registered")
	}
}

// pipeLauncher implements HostedLauncher with an in-process fake that
// simulates the v2 hosted-hello protocol flow over direct method calls
// (no actual subprocess spawn).
type pipeLauncher struct{}

func (pipeLauncher) Launch(_ context.Context, _ extension.Manifest) (extension.HostedClient, error) {
	return &pipeClient{}, nil
}

type pipeClient struct{}

func (c *pipeClient) Handshake(_ context.Context, _ hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error) {
	return hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
		GrantedServices: []hostproto.ServiceGrant{
			{Service: "ui", Version: 1},
			{Service: "commands", Version: 1},
		},
	}, nil
}

func (c *pipeClient) ServeInbound(ctx context.Context, extensionID string, dispatcher extension.Dispatcher) error {
	// Simulate commands.register.
	registerPayload, _ := json.Marshal(map[string]any{
		"name":        "hello",
		"description": "Say hello",
		"prompt":      "Say hello",
		"kind":        "prompt",
	})
	if _, err := dispatcher.Dispatch(extensionID, hostproto.HostCallParams{
		Service: "commands", Method: "register", Version: 1, Payload: registerPayload,
	}); err != nil {
		return err
	}
	// Simulate ui.status.
	statusPayload, _ := json.Marshal(map[string]any{
		"text":  "hosted-hello connected",
		"color": "cyan",
	})
	if _, err := dispatcher.Dispatch(extensionID, hostproto.HostCallParams{
		Service: "ui", Method: "status", Version: 1, Payload: statusPayload,
	}); err != nil {
		return err
	}
	// Block until context is canceled, simulating the real extension.
	<-ctx.Done()
	return nil
}

func (c *pipeClient) Shutdown(_ context.Context) error { return nil }
func (c *pipeClient) IsHealthy() bool                  { return true }

func TestHostedHelloE2E_PendingApprovalDoesNotHang(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")

	extDir := filepath.Join(root, ".pi-go", "extensions", "hosted-hello")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"name": "hosted-hello",
		"runtime": {
			"type": "hosted_stdio_jsonrpc",
			"command": "this-binary-does-not-exist"
		},
		"capabilities": ["ui.status"]
	}`
	if err := os.WriteFile(filepath.Join(extDir, "extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rt, err := extension.BuildRuntime(ctx, extension.RuntimeConfig{
		Config:          config.Config{},
		WorkDir:         root,
		Sandbox:         sandbox,
		BaseInstruction: "Base.",
	})
	if err != nil {
		t.Fatalf("BuildRuntime returned error: %v", err)
	}
	if rt.Manager == nil {
		t.Fatal("expected runtime manager")
	}

	var info *extension.ExtensionInfo
	for _, ext := range rt.Manager.Extensions() {
		if ext.ID == "hosted-hello" {
			found := ext
			info = &found
			break
		}
	}
	if info == nil {
		t.Fatal("expected hosted-hello to appear in Extensions()")
	}
	if info.State != extension.StatePending {
		t.Fatalf("hosted-hello state = %q, want pending", info.State)
	}
}

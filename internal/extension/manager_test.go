package extension

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/tools"
)

func TestBuildRuntime_InitializesExtensionManager(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	setTestHome(t, home)
	mustMkdirAllRuntime(t, filepath.Join(root, ".pi-go", "extensions"))

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{
		Config:          config.Config{},
		WorkDir:         root,
		Sandbox:         sandbox,
		BaseInstruction: "Base instruction.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Manager == nil {
		t.Fatal("expected runtime manager")
	}
}

func TestManager_AcceptsEventSubscription(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.SubscribeEvent("ext.demo", string(EventSessionStart)); err != nil {
		t.Fatal(err)
	}
	if !m.HasSubscription("ext.demo", string(EventSessionStart)) {
		t.Fatal("expected manager to store event subscription")
	}
}

func TestManager_RejectsDuplicateDynamicCommandNames(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.RegisterDynamicCommand("ext.a", SlashCommand{Name: "shipit", Description: "A"}); err != nil {
		t.Fatal(err)
	}
	err := m.RegisterDynamicCommand("ext.b", SlashCommand{Name: "shipit", Description: "B"})
	if err == nil {
		t.Fatal("expected duplicate dynamic command registration to fail")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected deterministic duplicate error, got %v", err)
	}
}

func TestManager_RejectsDynamicCommandConflictingWithBuiltin(t *testing.T) {
	m := NewManager(ManagerOptions{})
	err := m.RegisterDynamicCommand("ext.demo", SlashCommand{
		Name:        "help",
		Description: "shadow help",
	})
	if err == nil {
		t.Fatal("expected built-in command conflict to fail")
	}
	if !strings.Contains(err.Error(), "conflicts with built-in") {
		t.Fatalf("expected built-in conflict error, got %v", err)
	}
}

func TestManager_RejectsDeclarativeCommandConflictingWithBuiltin(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	err := m.RegisterManifest(Manifest{
		Name: "ext.demo",
		TUI: TUIConfig{
			Commands: []SlashCommand{{
				Name:        "help",
				Description: "shadow help",
				Prompt:      "demo",
			}},
		},
	})
	if err == nil {
		t.Fatal("expected declarative command conflict with built-in to fail")
	}
	if !strings.Contains(err.Error(), "conflicts with built-in") {
		t.Fatalf("expected built-in conflict error, got %v", err)
	}
}

func TestManager_RefusesToLaunchUnapprovedHostedExtension(t *testing.T) {
	launcher := &mockHostedLauncher{client: &mockHostedClient{
		response: hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		},
	}}
	permissions := NewPermissions([]ApprovalRecord{{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}})
	m := NewManager(ManagerOptions{
		Permissions:    permissions,
		HostedLauncher: launcher,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate approvals being revoked before launch.
	m.permissions = EmptyPermissions()
	err := m.StartHostedExtensions(context.Background(), "interactive")
	if err == nil {
		t.Fatal("expected hosted launch to fail when approvals are missing")
	}
	if launcher.launches != 0 {
		t.Fatalf("expected launcher not to run for unapproved extension, got %d launches", launcher.launches)
	}
}

func TestManager_LaunchesHostedExtensionFromManifestRuntime(t *testing.T) {
	client := &mockHostedClient{
		response: hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		},
	}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
		HostedLauncher: launcher,
	})
	manifest := Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
			Args:    []string{"serve", "--stdio"},
			Env:     map[string]string{"LOG_LEVEL": "debug"},
		},
	}
	if err := m.RegisterManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := m.StartHostedExtensions(context.Background(), "interactive"); err != nil {
		t.Fatal(err)
	}
	if launcher.launches != 1 {
		t.Fatalf("expected one hosted launch, got %d", launcher.launches)
	}
	if launcher.lastManifest.Runtime.Command != "hosted-ext" {
		t.Fatalf("expected manifest command to be passed to launcher, got %+v", launcher.lastManifest.Runtime)
	}
	if client.lastHandshake.Mode != "interactive" {
		t.Fatalf("expected interactive mode handshake, got %+v", client.lastHandshake)
	}
	if _, ok := m.HostedClient("ext.hosted"); !ok {
		t.Fatal("expected hosted client to be tracked by manager")
	}
}

func TestManager_ProvidesStateNamespaceToExtensions(t *testing.T) {
	m := NewManager(ManagerOptions{})
	m.BindSession("session-1", t.TempDir())

	state := m.StateNamespace("ext.demo")
	if err := state.Set(map[string]any{"counter": 7}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := state.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected extension state namespace to persist values")
	}
	if got["counter"] != float64(7) {
		t.Fatalf("expected counter=7, got %+v", got["counter"])
	}
}

func TestManager_DeliversSessionStartEvent(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.SubscribeEvent("ext.demo", string(EventSessionStart)); err != nil {
		t.Fatal(err)
	}
	delivered := false
	m.RegisterEventHandler("ext.demo", func(event Event) {
		if event.Type == EventSessionStart {
			delivered = true
		}
	})

	m.EmitEvent(Event{Type: EventSessionStart})
	if !delivered {
		t.Fatal("expected subscribed extension to receive session_start event")
	}
}

func TestManager_DoesNotDeliverToUnsubscribedExtension(t *testing.T) {
	m := NewManager(ManagerOptions{})
	delivered := false
	m.RegisterEventHandler("ext.demo", func(event Event) {
		delivered = true
	})

	m.EmitEvent(Event{Type: EventSessionStart})
	if delivered {
		t.Fatal("did not expect unsubscribed extension to receive event")
	}
}

func TestManager_AllowsApprovedToolRegistration(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:         "ext.hosted",
			TrustClass:          TrustClassHostedThirdParty,
			HostedRequired:      true,
			GrantedCapabilities: []Capability{CapabilityToolRegister},
		}}),
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterDynamicTool("ext.hosted", "demo_tool", false); err != nil {
		t.Fatalf("expected approved tool registration, got %v", err)
	}
}

func TestManager_DeniesToolInterceptForHostedThirdParty(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:         "ext.hosted",
			TrustClass:          TrustClassHostedThirdParty,
			HostedRequired:      true,
			GrantedCapabilities: []Capability{CapabilityToolRegister},
		}}),
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	err := m.RegisterDynamicTool("ext.hosted", "demo_tool", true)
	if err == nil {
		t.Fatal("expected tools.intercept to be denied for hosted third-party by default")
	}
	if !strings.Contains(err.Error(), string(CapabilityToolIntercept)) {
		t.Fatalf("expected intercept capability error, got %v", err)
	}
}

func TestManager_RejectsDuplicateToolNames(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.RegisterDynamicTool("ext.a", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	err := m.RegisterDynamicTool("ext.b", "demo_tool", false)
	if err == nil {
		t.Fatal("expected duplicate tool names to fail")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestBuildRuntime_IncludesManagerRegisteredTools(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	setTestHome(t, home)

	registry := NewRegistry()
	if err := registry.Register(compiledToolExtension{id: "ext.compiled"}); err != nil {
		t.Fatal(err)
	}

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{
		Config:           config.Config{},
		WorkDir:          root,
		Sandbox:          sandbox,
		BaseInstruction:  "Base instruction.",
		CompiledRegistry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, runtimeTool := range rt.Tools {
		if runtimeTool.Name() == "compiled_demo_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected compiled manager tool in runtime tools, got %d tools", len(rt.Tools))
	}
}

type mockHostedLauncher struct {
	client       HostedClient
	launches     int
	lastManifest Manifest
}

func (m *mockHostedLauncher) Launch(_ context.Context, manifest Manifest) (HostedClient, error) {
	m.launches++
	m.lastManifest = manifest
	return m.client, nil
}

type mockHostedClient struct {
	lastHandshake hostproto.HandshakeRequest
	response      hostproto.HandshakeResponse
	shutdowns     int
	healthy       bool
	serveCalled   bool
}

func (m *mockHostedClient) Handshake(_ context.Context, request hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error) {
	m.lastHandshake = request
	m.healthy = m.response.Accepted
	return m.response, nil
}

func (m *mockHostedClient) ServeInbound(ctx context.Context, _ string, _ Dispatcher) error {
	m.serveCalled = true
	<-ctx.Done()
	return nil
}

func (m *mockHostedClient) Shutdown(_ context.Context) error {
	m.shutdowns++
	m.healthy = false
	return nil
}

func (m *mockHostedClient) IsHealthy() bool {
	return m.healthy
}

type compiledToolExtension struct {
	id string
}

func (c compiledToolExtension) ID() string {
	return c.id
}

func (c compiledToolExtension) Register(registrar *Registrar) error {
	return registrar.RegisterRuntimeTool(mockRuntimeTool{name: "compiled_demo_tool", description: "compiled demo"}, false)
}

type mockRuntimeTool struct {
	name        string
	description string
}

func (m mockRuntimeTool) Name() string {
	return m.name
}

func (m mockRuntimeTool) Description() string {
	return m.description
}

func (m mockRuntimeTool) IsLongRunning() bool {
	return false
}

func TestManager_RegistersUIAndCommandsServices(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	catalog := mgr.servicesRegistry.HostServices()
	found := map[string]bool{}
	for _, entry := range catalog {
		found[entry.Service] = true
	}
	if !found["ui"] {
		t.Error("ui service not registered")
	}
	if !found["commands"] {
		t.Error("commands service not registered")
	}
}

func TestManager_DispatchUIStatusForwardsToIntentChannel(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	// Pre-register the test extension as compiled-in so the cap gate passes.
	mgr.extensions["ext.test"] = extensionRegistration{
		manifest: Manifest{Name: "ext.test"},
		trust:    TrustClassCompiledIn,
	}

	sub, cancel := mgr.SubscribeUIIntents(4)
	defer cancel()

	result, err := mgr.DispatchHostCall("ext.test", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
		Payload: []byte(`{"text":"hi"}`),
	})
	if err != nil {
		t.Fatalf("DispatchHostCall: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}

	select {
	case envelope := <-sub:
		if envelope.ExtensionID != "ext.test" {
			t.Errorf("ExtensionID = %q", envelope.ExtensionID)
		}
		if envelope.Intent.Type != UIIntentStatus {
			t.Errorf("Intent.Type = %q", envelope.Intent.Type)
		}
		if envelope.Intent.Status == nil || envelope.Intent.Status.Text != "hi" {
			t.Errorf("Intent.Status = %+v", envelope.Intent.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for UI intent")
	}
}

func TestStartHostedExtensions_RunsServeInbound(t *testing.T) {
	client := &mockHostedClient{
		response: hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		},
	}
	mgr := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			HostedRequired:      true,
			GrantedCapabilities: []Capability{CapabilityUIStatus},
		}}),
		HostedLauncher: stubHostedLauncher{client: client},
	})
	manifest := Manifest{
		Name:         "ext.demo",
		Capabilities: []Capability{CapabilityUIStatus},
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "demo",
		},
	}
	if err := mgr.RegisterManifest(manifest); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.StartHostedExtensions(ctx, "hosted_stdio"); err != nil {
		t.Fatalf("StartHostedExtensions: %v", err)
	}
	// Give the spawned ServeInbound goroutine a chance to run.
	deadline := time.After(time.Second)
	for {
		if client.serveCalled {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ServeInbound was not called")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if client.lastHandshake.ExtensionID != "ext.demo" {
		t.Errorf("handshake ExtensionID = %q", client.lastHandshake.ExtensionID)
	}
	if len(client.lastHandshake.RequestedServices) == 0 {
		t.Error("handshake RequestedServices empty")
	}
}

type stubHostedLauncher struct {
	client *mockHostedClient
}

func (l stubHostedLauncher) Launch(_ context.Context, _ Manifest) (HostedClient, error) {
	return l.client, nil
}

func TestManager_DispatchCommandRegisterForwardsToCommandRegistry(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	mgr.extensions["ext.test"] = extensionRegistration{
		manifest: Manifest{Name: "ext.test"},
		trust:    TrustClassCompiledIn,
	}

	_, err := mgr.DispatchHostCall("ext.test", hostproto.HostCallParams{
		Service: "commands",
		Method:  "register",
		Version: 1,
		Payload: []byte(`{"name":"hello","description":"say hi","kind":"prompt","prompt":"say hi"}`),
	})
	if err != nil {
		t.Fatalf("DispatchHostCall: %v", err)
	}
	cmd, ok := mgr.FindCommand("hello")
	if !ok {
		t.Fatal("command 'hello' was not registered")
	}
	if cmd.Description != "say hi" {
		t.Errorf("Description = %q", cmd.Description)
	}
}

package extension

import (
	"context"
	"fmt"
	"os"
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

func TestManager_SkipsPendingHostedExtensionOnStart(t *testing.T) {
	launcher := &mockHostedLauncher{client: &mockHostedClient{
		response: hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		},
	}}
	m := NewManager(ManagerOptions{
		Permissions:    EmptyPermissions(),
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
	if err := m.StartHostedExtensions(context.Background(), "interactive"); err != nil {
		t.Fatalf("expected StartHostedExtensions to tolerate pending extensions, got %v", err)
	}
	if launcher.launches != 0 {
		t.Fatalf("expected launcher not to run for pending extension, got %d launches", launcher.launches)
	}
}

func TestManager_RegistersUnapprovedHostedAsPending(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	})
	if err != nil {
		t.Fatalf("expected RegisterManifest to succeed for unapproved hosted, got %v", err)
	}
	infos := m.Extensions()
	if len(infos) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(infos))
	}
	if infos[0].State != StatePending {
		t.Fatalf("expected StatePending, got %q", infos[0].State)
	}
}

func TestManager_RegistersApprovedHostedAsReady(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
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
	infos := m.Extensions()
	if len(infos) != 1 || infos[0].State != StateReady {
		t.Fatalf("expected ready state, got %+v", infos)
	}
}

func TestManager_RegistersDeclarativeAsReady(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.RegisterManifest(Manifest{Name: "ext.decl"}); err != nil {
		t.Fatal(err)
	}
	infos := m.Extensions()
	if len(infos) != 1 || infos[0].State != StateReady {
		t.Fatalf("expected ready state, got %+v", infos)
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
	infos := m.Extensions()
	if len(infos) != 1 || infos[0].State != StateRunning {
		t.Fatalf("expected running state after start, got %+v", infos)
	}
}

func TestStartHostedExtensions_PartialFailureTolerant(t *testing.T) {
	good := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	bad := &mockHostedClient{response: hostproto.HandshakeResponse{
		Accepted: false,
		Message:  "boom",
	}}
	launcher := &sequencedHostedLauncher{clients: []HostedClient{bad, good}}

	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{
			{ExtensionID: "ext.bad", TrustClass: TrustClassHostedThirdParty, HostedRequired: true},
			{ExtensionID: "ext.good", TrustClass: TrustClassHostedThirdParty, HostedRequired: true},
		}),
		HostedLauncher: launcher,
	})
	for _, id := range []string{"ext.bad", "ext.good"} {
		if err := m.RegisterManifest(Manifest{
			Name: id,
			Runtime: RuntimeSpec{
				Type:    RuntimeTypeHostedStdioJSONRPC,
				Command: "hosted-ext",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.StartHostedExtensions(context.Background(), "interactive"); err != nil {
		t.Fatalf("expected nil return despite one failure, got %v", err)
	}

	states := map[string]ExtensionState{}
	for _, info := range m.Extensions() {
		states[info.ID] = info.State
	}
	if states["ext.bad"] != StateErrored {
		t.Errorf("ext.bad state = %q, want errored", states["ext.bad"])
	}
	if states["ext.good"] != StateRunning {
		t.Errorf("ext.good state = %q, want running", states["ext.good"])
	}
}

// sequencedHostedLauncher returns clients in order on successive Launch calls.
type sequencedHostedLauncher struct {
	clients []HostedClient
	calls   int
}

func (l *sequencedHostedLauncher) Launch(_ context.Context, _ Manifest) (HostedClient, error) {
	if l.calls >= len(l.clients) {
		return nil, fmt.Errorf("sequencedHostedLauncher: out of clients")
	}
	c := l.clients[l.calls]
	l.calls++
	return c, nil
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

func TestManager_GrantApproval(t *testing.T) {
	dir := t.TempDir()
	approvalsPath := filepath.Join(dir, "approvals.json")
	m := NewManager(ManagerOptions{
		Permissions:   EmptyPermissions(),
		ApprovalsPath: approvalsPath,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
		Capabilities: []Capability{CapabilityUIStatus},
	}); err != nil {
		t.Fatal(err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StatePending {
		t.Fatalf("pre-grant state = %q, want pending", info.State)
	}

	if err := m.GrantApproval(GrantInput{
		ExtensionID:  "ext.hosted",
		TrustClass:   TrustClassHostedThirdParty,
		Capabilities: []Capability{CapabilityUIStatus},
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if info := findExtension(t, m, "ext.hosted"); info.State != StateReady {
		t.Fatalf("post-grant state = %q, want ready", info.State)
	}

	reloaded, err := LoadPermissions(approvalsPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Approval("ext.hosted"); !ok {
		t.Fatal("expected approvals.json to contain ext.hosted")
	}
}

func TestManager_DenyApproval(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.DenyApproval("ext.hosted"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateDenied {
		t.Fatalf("post-deny state = %q, want denied", info.State)
	}
}

// findExtension is a test helper.
func findExtension(t *testing.T, m *Manager, id string) ExtensionInfo {
	t.Helper()
	for _, info := range m.Extensions() {
		if info.ID == id {
			return info
		}
	}
	t.Fatalf("extension %q not found", id)
	return ExtensionInfo{}
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

func TestManager_ReloadManifests(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "extensions")

	mustWriteManifest(t, extDir, "ext.first", `{"name":"ext.first","description":"first"}`)

	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	manifests, err := LoadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterManifests(manifests); err != nil {
		t.Fatal(err)
	}

	// Add a new extension on disk.
	mustWriteManifest(t, extDir, "ext.second", `{"name":"ext.second","description":"second"}`)

	added, removed, err := m.ReloadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "ext.second" {
		t.Fatalf("added = %v, want [ext.second]", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want []", removed)
	}

	// Remove the first extension from disk.
	if err := os.RemoveAll(filepath.Join(extDir, "ext.first")); err != nil {
		t.Fatal(err)
	}
	added, removed, err = m.ReloadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("added = %v, want []", added)
	}
	if len(removed) != 1 || removed[0] != "ext.first" {
		t.Fatalf("removed = %v, want [ext.first]", removed)
	}

	ids := map[string]bool{}
	for _, info := range m.Extensions() {
		ids[info.ID] = true
	}
	if ids["ext.first"] || !ids["ext.second"] {
		t.Fatalf("unexpected extensions after reload: %v", ids)
	}
}

func mustWriteManifest(t *testing.T, extDir, name, body string) {
	t.Helper()
	dir := filepath.Join(extDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestManager_StartAndStopExtension(t *testing.T) {
	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
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

	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateRunning {
		t.Fatalf("post-start state = %q, want running", info.State)
	}
	if launcher.launches != 1 {
		t.Fatalf("launches = %d, want 1", launcher.launches)
	}

	// Idempotent: re-starting a running extension is a no-op.
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	if launcher.launches != 1 {
		t.Fatalf("launches after idempotent = %d, want 1", launcher.launches)
	}

	if err := m.StopExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateStopped {
		t.Fatalf("post-stop state = %q, want stopped", info.State)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", client.shutdowns)
	}
}

func TestManager_StartExtension_RejectsPending(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	err := m.StartExtension(context.Background(), "ext.hosted")
	if err == nil {
		t.Fatal("expected start of pending extension to fail")
	}
}

func TestManager_RestartExtension(t *testing.T) {
	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
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
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatal(err)
	}
	if err := m.RestartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if launcher.launches != 2 {
		t.Fatalf("launches = %d, want 2", launcher.launches)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", client.shutdowns)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateRunning {
		t.Fatalf("post-restart state = %q, want running", info.State)
	}
}

func TestManager_RevokeApproval(t *testing.T) {
	dir := t.TempDir()
	approvalsPath := filepath.Join(dir, "approvals.json")

	seed := NewPermissions([]ApprovalRecord{{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}})
	if err := seed.Upsert(approvalsPath, ApprovalRecord{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}); err != nil {
		t.Fatal(err)
	}

	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
		HostedLauncher: launcher,
		ApprovalsPath:  approvalsPath,
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
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatal(err)
	}

	if err := m.RevokeApproval(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", client.shutdowns)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StatePending {
		t.Fatalf("post-revoke state = %q, want pending", info.State)
	}

	reloaded, err := LoadPermissions(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Approval("ext.hosted"); ok {
		t.Fatal("expected approvals.json to no longer contain ext.hosted")
	}
}

func TestManager_GrantApproval_RegistersManifestCommands(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
		TUI: TUIConfig{
			Commands: []SlashCommand{
				{Name: "ext-cmd", Description: "Extension command", Prompt: "do {{args}}"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// While pending, commands should not be registered.
	if cmds := m.SlashCommands(); len(cmds) != 0 {
		t.Fatalf("expected no commands while pending, got %v", cmds)
	}

	if err := m.GrantApproval(GrantInput{
		ExtensionID: "ext.hosted",
		TrustClass:  TrustClassHostedThirdParty,
	}); err != nil {
		t.Fatal(err)
	}

	cmds := m.SlashCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command after grant, got %d", len(cmds))
	}
	if cmds[0].Name != "ext-cmd" {
		t.Fatalf("expected command name %q, got %q", "ext-cmd", cmds[0].Name)
	}
}

func TestManager_RevokeApproval_UnregistersManifestCommands(t *testing.T) {
	perms := NewPermissions([]ApprovalRecord{{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}})
	m := NewManager(ManagerOptions{Permissions: perms})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
		TUI: TUIConfig{
			Commands: []SlashCommand{
				{Name: "ext-cmd", Description: "Extension command"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Pre-approved extension starts Ready with commands registered.
	if cmds := m.SlashCommands(); len(cmds) != 1 {
		t.Fatalf("expected 1 command before revoke, got %d", len(cmds))
	}

	if err := m.RevokeApproval(context.Background(), "ext.hosted"); err != nil {
		t.Fatal(err)
	}

	if cmds := m.SlashCommands(); len(cmds) != 0 {
		t.Fatalf("expected no commands after revoke, got %v", cmds)
	}
}

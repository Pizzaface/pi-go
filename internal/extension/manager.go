package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/hostruntime"
	"github.com/dimetron/pi-go/internal/extension/services"
	commandsservice "github.com/dimetron/pi-go/internal/extension/services/commands"
	uiservice "github.com/dimetron/pi-go/internal/extension/services/ui"
	"google.golang.org/adk/tool"
)

// Dispatcher is re-exported from hostruntime so HostedClient
// implementations and tests can reference it without importing
// hostruntime directly.
type Dispatcher = hostruntime.Dispatcher

// HostedHandshakeTimeout bounds the initial JSON-RPC handshake with a
// hosted extension after the process is launched.
const HostedHandshakeTimeout = 5 * time.Second

// HostedShutdownTimeout bounds the graceful shutdown of a hosted
// extension subprocess.
const HostedShutdownTimeout = 3 * time.Second

type commandRegistration struct {
	command   SlashCommand
	owner     string
	bootstrap bool
}

type toolRegistration struct {
	owner     string
	intercept bool
}

// ExtensionState is the lifecycle state of a registered extension.
type ExtensionState string

const (
	StatePending ExtensionState = "pending_approval"
	StateReady   ExtensionState = "ready"
	StateRunning ExtensionState = "running"
	StateStopped ExtensionState = "stopped"
	StateErrored ExtensionState = "errored"
	StateDenied  ExtensionState = "denied"
)

// ExtensionInfo is a read-only snapshot of a single extension's state,
// used by the TUI panel and status line.
type ExtensionInfo struct {
	ID                    string
	TrustClass            TrustClass
	State                 ExtensionState
	RequestedCapabilities []Capability
	Runtime               RuntimeSpec
	LastError             string
	StartedAt             time.Time
}

// GrantInput carries the parameters for an approval grant.
type GrantInput struct {
	ExtensionID  string
	TrustClass   TrustClass
	Capabilities []Capability
}

type extensionRegistration struct {
	manifest  Manifest
	trust     TrustClass
	state     ExtensionState
	lastError string
	startedAt time.Time
}

type rendererRegistration struct {
	owner        string
	allowedKinds map[RenderKind]struct{}
	renderer     RendererFunc
}

type ManagerOptions struct {
	Permissions      *Permissions
	Registry         *Registry
	BuiltinCommands  []string
	HostedLauncher   HostedLauncher
	ServicesRegistry *services.Registry
	ApprovalsPath    string
}

type HostedClient interface {
	Handshake(context.Context, hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error)
	ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error
	Shutdown(context.Context) error
	IsHealthy() bool
}

type HostedLauncher interface {
	Launch(context.Context, Manifest) (HostedClient, error)
}

type defaultHostedLauncher struct{}

type RendererFunc func(context.Context, RenderRequest) (RenderResult, error)

func (defaultHostedLauncher) Launch(ctx context.Context, manifest Manifest) (HostedClient, error) {
	process, err := hostruntime.StartProcess(ctx, hostruntime.ProcessConfig{
		Command: manifest.Runtime.Command,
		Args:    manifest.Runtime.Args,
		Env:     manifest.Runtime.Env,
		WorkDir: manifest.Dir,
	})
	if err != nil {
		return nil, err
	}
	return hostruntime.NewClientFromProcess(process), nil
}

// Manager stores runtime extension registrations and ownership metadata.
type Manager struct {
	mu               sync.RWMutex
	permissions      *Permissions
	registry         *Registry
	servicesRegistry *services.Registry
	hostedLauncher   HostedLauncher
	approvalsPath    string
	stateStore       *StateStore
	builtins         map[string]struct{}
	extensions       map[string]extensionRegistration
	commands         map[string]commandRegistration
	tools            map[string]toolRegistration
	runtimeTools     map[string]tool.Tool
	hostedClients    map[string]HostedClient
	subscriptions    map[string]map[string]struct{} // event -> extension IDs
	eventHandlers    map[string]func(Event)
	intentSubs       map[int]chan UIIntentEnvelope
	nextIntentSubID  int
	renderers        map[RenderSurface]rendererRegistration
}

// Registrar is passed to compiled extensions so they can register contributions.
type Registrar struct {
	manager     *Manager
	extensionID string
}

func (r *Registrar) Subscribe(event EventType) error {
	return r.manager.SubscribeEvent(r.extensionID, string(event))
}

func (r *Registrar) RegisterCommand(command SlashCommand) error {
	return r.manager.RegisterDynamicCommand(r.extensionID, command)
}

func (r *Registrar) RegisterTool(name string, intercept bool) error {
	return r.manager.RegisterDynamicTool(r.extensionID, name, intercept)
}

func (r *Registrar) RegisterRuntimeTool(runtimeTool tool.Tool, intercept bool) error {
	return r.manager.RegisterRuntimeTool(r.extensionID, runtimeTool, intercept)
}

func (r *Registrar) RegisterRenderer(surface RenderSurface, allowedKinds []RenderKind, renderer RendererFunc) error {
	return r.manager.RegisterRenderer(r.extensionID, surface, allowedKinds, renderer)
}

var defaultBuiltinSlashCommands = []string{
	"/help",
	"/clear",
	"/model",
	"/effort",
	"/session",
	"/new",
	"/resume",
	"/fork",
	"/tree",
	"/settings",
	"/context",
	"/branch",
	"/compact",
	"/history",
	"/login",
	"/skills",
	"/skill-create",
	"/skill-load",
	"/skill-list",
	"/theme",
	"/ping",
	"/debug",
	"/restart",
	"/exit",
	"/quit",
}

func DefaultBuiltinSlashCommands() []string {
	out := make([]string, len(defaultBuiltinSlashCommands))
	copy(out, defaultBuiltinSlashCommands)
	return out
}

func NewManager(opts ManagerOptions) *Manager {
	permissions := opts.Permissions
	if permissions == nil {
		permissions = EmptyPermissions()
	}
	registry := opts.Registry
	if registry == nil {
		registry = NewRegistry()
	}
	builtinCommands := opts.BuiltinCommands
	if len(builtinCommands) == 0 {
		builtinCommands = DefaultBuiltinSlashCommands()
	}
	hostedLauncher := opts.HostedLauncher
	if hostedLauncher == nil {
		hostedLauncher = defaultHostedLauncher{}
	}
	approvalsPath := strings.TrimSpace(opts.ApprovalsPath)
	builtins := make(map[string]struct{}, len(builtinCommands))
	for _, cmd := range builtinCommands {
		name := normalizeCommandName(cmd)
		if name != "" {
			builtins[name] = struct{}{}
		}
	}

	mgr := &Manager{
		permissions:      permissions,
		registry:         registry,
		servicesRegistry: opts.ServicesRegistry,
		hostedLauncher:   hostedLauncher,
		approvalsPath:    approvalsPath,
		stateStore:       nil,
		builtins:         builtins,
		extensions:       map[string]extensionRegistration{},
		commands:         map[string]commandRegistration{},
		tools:            map[string]toolRegistration{},
		runtimeTools:     map[string]tool.Tool{},
		hostedClients:    map[string]HostedClient{},
		subscriptions:    map[string]map[string]struct{}{},
		eventHandlers:    map[string]func(Event){},
		intentSubs:       map[int]chan UIIntentEnvelope{},
		renderers:        map[RenderSurface]rendererRegistration{},
	}

	if mgr.servicesRegistry == nil {
		gate := managerCapabilityGate{permissions: permissions, manager: mgr}
		mgr.servicesRegistry = services.NewRegistry(gate)
	}

	// Register the v1 services. Sinks forward to the manager's existing
	// fan-out channels so the TUI doesn't need to know about services.
	_ = mgr.servicesRegistry.Register(uiservice.New(managerUISink{manager: mgr}))
	_ = mgr.servicesRegistry.Register(commandsservice.New(managerCommandsSink{manager: mgr}))

	return mgr
}

// DispatchHostCall routes an ext-initiated host_call through the
// services registry. It's the integration point for hostruntime.Client
// .ServeInbound (via the Dispatcher interface).
func (m *Manager) DispatchHostCall(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	sess := &services.SessionContext{
		ExtensionID: extensionID,
	}
	m.mu.RLock()
	if m.stateStore != nil && m.stateStore.Bound() {
		// SessionID/SessionsDir are populated for services that need them.
		// (Plan 2 wires this through fully when the state service lands.)
	}
	m.mu.RUnlock()
	return m.servicesRegistry.Dispatch(extensionID, params, sess)
}

func (m *Manager) BindSession(sessionID, sessionsDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	sessionsDir = strings.TrimSpace(sessionsDir)
	if sessionID == "" || sessionsDir == "" {
		m.stateStore = nil
		return
	}
	m.stateStore = NewStateStore(sessionsDir, sessionID)
}

func (m *Manager) StateNamespace(extensionID string) StateNamespace {
	m.mu.RLock()
	store := m.stateStore
	m.mu.RUnlock()
	if store == nil {
		return StateNamespace{}
	}
	return store.Namespace(extensionID)
}

func (m *Manager) Permissions() *Permissions {
	return m.permissions
}

func (m *Manager) Extensions() []ExtensionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ExtensionInfo, 0, len(m.extensions))
	for id, reg := range m.extensions {
		caps := append([]Capability(nil), reg.manifest.Capabilities...)
		out = append(out, ExtensionInfo{
			ID:                    id,
			TrustClass:            reg.trust,
			State:                 reg.state,
			RequestedCapabilities: caps,
			Runtime:               reg.manifest.Runtime,
			LastError:             reg.lastError,
			StartedAt:             reg.startedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GrantApproval records an approval for a pending extension and
// transitions it to Ready. Persists to approvals.json if
// ManagerOptions.ApprovalsPath was set. Does NOT auto-start.
func (m *Manager) GrantApproval(input GrantInput) error {
	id := strings.TrimSpace(input.ExtensionID)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}

	m.mu.Lock()
	reg, ok := m.extensions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.state != StatePending && reg.state != StateDenied {
		m.mu.Unlock()
		return fmt.Errorf("extension %q cannot be granted from state %q", id, reg.state)
	}

	trust := input.TrustClass
	if trust == "" {
		trust = reg.trust
	}
	caps := append([]Capability(nil), input.Capabilities...)
	if len(caps) == 0 {
		caps = append(caps, reg.manifest.Capabilities...)
	}

	record := ApprovalRecord{
		ExtensionID:         id,
		TrustClass:          trust,
		GrantedCapabilities: caps,
		HostedRequired:      reg.manifest.runtimeType() == RuntimeTypeHostedStdioJSONRPC,
		ApprovedAt:          time.Now().UTC(),
	}

	reg.trust = trust
	reg.state = StateReady
	reg.lastError = ""
	m.extensions[id] = reg
	approvalsPath := m.approvalsPath
	m.mu.Unlock()

	if approvalsPath != "" {
		if err := m.permissions.Upsert(approvalsPath, record); err != nil {
			m.mu.Lock()
			reg.state = StatePending
			reg.lastError = err.Error()
			m.extensions[id] = reg
			m.mu.Unlock()
			return fmt.Errorf("persisting approval for %q: %w", id, err)
		}
	} else {
		m.permissions.approvals[id] = record
	}
	return nil
}

// DenyApproval transitions a pending extension to Denied. In-memory only.
func (m *Manager) DenyApproval(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.extensions[id]
	if !ok {
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.state != StatePending {
		return fmt.Errorf("extension %q cannot be denied from state %q", id, reg.state)
	}
	reg.state = StateDenied
	m.extensions[id] = reg
	return nil
}

// SubscribeUIIntents creates a non-blocking subscription channel for UI intents.
func (m *Manager) SubscribeUIIntents(buffer int) (<-chan UIIntentEnvelope, func()) {
	if buffer <= 0 {
		buffer = 16
	}
	ch := make(chan UIIntentEnvelope, buffer)
	m.mu.Lock()
	id := m.nextIntentSubID
	m.nextIntentSubID++
	m.intentSubs[id] = ch
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		subscriber, ok := m.intentSubs[id]
		if ok {
			delete(m.intentSubs, id)
			close(subscriber)
		}
		m.mu.Unlock()
	}
	return ch, cancel
}

func (m *Manager) EmitUIIntent(extensionID string, intent UIIntent) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return fmt.Errorf("extension id is required")
	}
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("invalid ui intent: %w", err)
	}
	requiredCapability := intent.RequiredCapability()

	m.mu.RLock()
	trust := TrustClassDeclarative
	if reg, ok := m.extensions[extensionID]; ok {
		trust = reg.trust
	}
	subscribers := make([]chan UIIntentEnvelope, 0, len(m.intentSubs))
	for _, subscriber := range m.intentSubs {
		subscribers = append(subscribers, subscriber)
	}
	m.mu.RUnlock()

	if requiredCapability != "" && !m.permissions.AllowsCapability(extensionID, trust, requiredCapability) {
		return fmt.Errorf("extension %q capability %q is not approved", extensionID, requiredCapability)
	}

	envelope := UIIntentEnvelope{
		ExtensionID: extensionID,
		Intent:      intent,
	}
	for _, subscriber := range subscribers {
		select {
		case subscriber <- envelope:
		default:
			// Subscriber is slow or stalled; keep runtime non-blocking.
		}
	}
	return nil
}

func (m *Manager) RegisterManifests(manifests []Manifest) error {
	for _, manifest := range manifests {
		if err := m.RegisterManifest(manifest); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) RegisterManifest(manifest Manifest) error {
	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if err := validateRuntimeSpec(manifest); err != nil {
		return fmt.Errorf("extension %q runtime: %w", manifest.Name, err)
	}

	trust := m.permissions.ResolveTrust(manifest.Name, ResolveManifestTrust(manifest))
	initialState := StateReady
	if manifest.runtimeType() == RuntimeTypeHostedStdioJSONRPC &&
		!m.permissions.HostedApproved(manifest.Name, trust) {
		initialState = StatePending
	}

	// Capability gate validation only applies to Ready extensions;
	// Pending extensions carry their requested caps through to the
	// approval dialog untouched.
	if initialState == StateReady {
		for _, capability := range manifest.Capabilities {
			if !m.permissions.AllowsCapability(manifest.Name, trust, capability) {
				return fmt.Errorf("extension %q capability %q is not approved", manifest.Name, capability)
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[manifest.Name] = extensionRegistration{
		manifest: manifest,
		trust:    trust,
		state:    initialState,
	}
	if initialState == StateReady {
		for _, command := range manifest.TUI.Commands {
			if err := m.registerCommandLocked(manifest.Name, command, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) RegisterCompiledExtensions() error {
	for _, ext := range m.registry.List() {
		id := strings.TrimSpace(ext.ID())
		if id == "" {
			return fmt.Errorf("compiled extension has empty id")
		}
		manifest := Manifest{
			Name:    id,
			Runtime: RuntimeSpec{Type: RuntimeTypeCompiledIn},
		}
		if err := m.RegisterManifest(manifest); err != nil {
			return err
		}
		if err := ext.Register(&Registrar{
			manager:     m,
			extensionID: id,
		}); err != nil {
			return fmt.Errorf("registering compiled extension %q: %w", id, err)
		}
	}
	return nil
}

func (m *Manager) StartHostedExtensions(ctx context.Context, mode string) error {
	type hostedRegistration struct {
		id       string
		manifest Manifest
		trust    TrustClass
	}
	var toStart []hostedRegistration

	m.mu.RLock()
	for id, reg := range m.extensions {
		if reg.manifest.runtimeType() != RuntimeTypeHostedStdioJSONRPC {
			continue
		}
		if reg.state != StateReady {
			continue
		}
		if _, started := m.hostedClients[id]; started {
			continue
		}
		toStart = append(toStart, hostedRegistration{
			id:       id,
			manifest: reg.manifest,
			trust:    reg.trust,
		})
	}
	m.mu.RUnlock()

	for _, reg := range toStart {
		if err := m.startOneHosted(ctx, reg.id, reg.manifest, reg.trust, mode); err != nil {
			m.markErrored(reg.id, err)
		}
	}
	return nil
}

// startOneHosted launches, handshakes, and wires the dispatch goroutine
// for a single hosted extension.
func (m *Manager) startOneHosted(ctx context.Context, id string, manifest Manifest, trust TrustClass, mode string) error {
	client, err := m.hostedLauncher.Launch(ctx, manifest)
	if err != nil {
		return fmt.Errorf("launching hosted extension %q: %w", id, err)
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, HostedHandshakeTimeout)
	resp, err := client.Handshake(handshakeCtx, hostproto.HandshakeRequest{
		ProtocolVersion:   hostproto.ProtocolVersion,
		ExtensionID:       id,
		Mode:              mode,
		RequestedServices: manifestToRequestedServices(manifest),
	})
	cancel()
	if err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
		_ = client.Shutdown(shutdownCtx)
		shutdownCancel()
		return fmt.Errorf("handshake with hosted extension %q failed: %w", id, err)
	}
	if !resp.Accepted {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
		_ = client.Shutdown(shutdownCtx)
		shutdownCancel()
		msg := resp.Message
		if msg == "" {
			msg = "handshake rejected"
		}
		return fmt.Errorf("hosted extension %q handshake not accepted: %s", id, msg)
	}

	m.mu.Lock()
	m.hostedClients[id] = client
	reg := m.extensions[id]
	reg.state = StateRunning
	reg.lastError = ""
	reg.startedAt = time.Now()
	m.extensions[id] = reg
	m.mu.Unlock()

	go func(extID string, c HostedClient) {
		serveCtx, serveCancel := context.WithCancel(context.Background())
		defer serveCancel()
		dispatcher := dispatcherFunc(func(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
			return m.DispatchHostCall(extensionID, params)
		})
		_ = c.ServeInbound(serveCtx, extID, dispatcher)
	}(id, client)

	return nil
}

// markErrored transitions an extension to StateErrored and records the
// error message.
func (m *Manager) markErrored(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.extensions[id]
	if !ok {
		return
	}
	reg.state = StateErrored
	reg.lastError = err.Error()
	m.extensions[id] = reg
}

// dispatcherFunc adapts a function to the Dispatcher interface.
type dispatcherFunc func(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error)

func (f dispatcherFunc) Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	return f(extensionID, params)
}

func (m *Manager) HostedClient(extensionID string) (HostedClient, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.hostedClients[strings.TrimSpace(extensionID)]
	return client, ok
}

func (m *Manager) ShutdownHostedExtensions(ctx context.Context) {
	m.mu.Lock()
	clients := make([]HostedClient, 0, len(m.hostedClients))
	for _, client := range m.hostedClients {
		clients = append(clients, client)
	}
	m.hostedClients = map[string]HostedClient{}
	m.mu.Unlock()

	for _, client := range clients {
		shutdownCtx, cancel := context.WithTimeout(ctx, HostedShutdownTimeout)
		_ = client.Shutdown(shutdownCtx)
		cancel()
	}
}

func (m *Manager) RegisterBootstrapCommand(extensionID string, command SlashCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerCommandLocked(extensionID, command, true)
}

func (m *Manager) RegisterDynamicCommand(extensionID string, command SlashCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerCommandLocked(extensionID, command, false)
}

func (m *Manager) registerCommandLocked(extensionID string, command SlashCommand, bootstrap bool) error {
	name := normalizeCommandName(command.Name)
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	if _, reserved := m.builtins[name]; reserved {
		return fmt.Errorf("command %q conflicts with built-in command", name)
	}
	command.Name = name
	if existing, exists := m.commands[name]; exists {
		if bootstrap && existing.bootstrap {
			m.commands[name] = commandRegistration{
				command:   command,
				owner:     extensionID,
				bootstrap: true,
			}
			return nil
		}
		return fmt.Errorf("command %q already registered by extension %q", name, existing.owner)
	}
	m.commands[name] = commandRegistration{
		command:   command,
		owner:     extensionID,
		bootstrap: bootstrap,
	}
	return nil
}

func (m *Manager) RegisterDynamicTool(extensionID, toolName string, intercept bool) error {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return fmt.Errorf("tool name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerToolLocked(extensionID, toolName, nil, intercept)
}

func (m *Manager) RegisterRuntimeTool(extensionID string, runtimeTool tool.Tool, intercept bool) error {
	if runtimeTool == nil {
		return fmt.Errorf("runtime tool is required")
	}
	toolName := strings.TrimSpace(runtimeTool.Name())
	if toolName == "" {
		return fmt.Errorf("runtime tool name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerToolLocked(extensionID, toolName, runtimeTool, intercept)
}

func (m *Manager) registerToolLocked(extensionID, toolName string, runtimeTool tool.Tool, intercept bool) error {
	if existing, exists := m.tools[toolName]; exists {
		return fmt.Errorf("tool %q already registered by extension %q", toolName, existing.owner)
	}
	trust := TrustClassDeclarative
	if reg, ok := m.extensions[extensionID]; ok {
		trust = reg.trust
	}
	if !m.permissions.AllowsCapability(extensionID, trust, CapabilityToolRegister) {
		return fmt.Errorf("extension %q capability %q is not approved", extensionID, CapabilityToolRegister)
	}
	if intercept && !m.permissions.AllowsCapability(extensionID, trust, CapabilityToolIntercept) {
		return fmt.Errorf("extension %q capability %q is not approved", extensionID, CapabilityToolIntercept)
	}
	m.tools[toolName] = toolRegistration{
		owner:     extensionID,
		intercept: intercept,
	}
	if runtimeTool != nil {
		m.runtimeTools[toolName] = runtimeTool
	}
	return nil
}

func (m *Manager) SubscribeEvent(extensionID, event string) error {
	extensionID = strings.TrimSpace(extensionID)
	event = strings.TrimSpace(event)
	if extensionID == "" {
		return fmt.Errorf("extension id is required")
	}
	if event == "" {
		return fmt.Errorf("event name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	subscribers, ok := m.subscriptions[event]
	if !ok {
		subscribers = map[string]struct{}{}
		m.subscriptions[event] = subscribers
	}
	subscribers[extensionID] = struct{}{}
	return nil
}

func (m *Manager) Subscribers(event string) []string {
	event = strings.TrimSpace(event)
	m.mu.RLock()
	defer m.mu.RUnlock()
	subscribers := m.subscriptions[event]
	out := make([]string, 0, len(subscribers))
	for extensionID := range subscribers {
		out = append(out, extensionID)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) HasSubscription(extensionID, event string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if subscribers, ok := m.subscriptions[strings.TrimSpace(event)]; ok {
		_, ok := subscribers[strings.TrimSpace(extensionID)]
		return ok
	}
	return false
}

func (m *Manager) RegisterEventHandler(extensionID string, handler func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" || handler == nil {
		return
	}
	m.eventHandlers[extensionID] = handler
}

func (m *Manager) EmitEvent(event Event) {
	subscribers := m.Subscribers(string(event.Type))
	if len(subscribers) == 0 {
		return
	}
	handlers := make([]func(Event), 0, len(subscribers))
	m.mu.RLock()
	for _, extensionID := range subscribers {
		if handler, ok := m.eventHandlers[extensionID]; ok {
			handlers = append(handlers, handler)
		}
	}
	m.mu.RUnlock()
	for _, handler := range handlers {
		handler(event)
	}
}

func (m *Manager) SlashCommands() []SlashCommand {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.commands))
	for name := range m.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]SlashCommand, 0, len(names))
	for _, name := range names {
		out = append(out, m.commands[name].command)
	}
	return out
}

func (m *Manager) FindCommand(name string) (SlashCommand, bool) {
	name = normalizeCommandName(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	reg, ok := m.commands[name]
	if !ok {
		return SlashCommand{}, false
	}
	return reg.command, true
}

func (m *Manager) CommandOwner(name string) (string, bool) {
	name = normalizeCommandName(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	reg, ok := m.commands[name]
	if !ok {
		return "", false
	}
	return reg.owner, true
}

func (m *Manager) ToolOwner(name string) (string, bool) {
	name = strings.TrimSpace(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	reg, ok := m.tools[name]
	if !ok {
		return "", false
	}
	return reg.owner, true
}

func (m *Manager) RuntimeTools() []tool.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.runtimeTools))
	for name := range m.runtimeTools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]tool.Tool, 0, len(names))
	for _, name := range names {
		out = append(out, m.runtimeTools[name])
	}
	return out
}

func (m *Manager) RegisterRenderer(extensionID string, surface RenderSurface, allowedKinds []RenderKind, renderer RendererFunc) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return fmt.Errorf("extension id is required")
	}
	surface = normalizeRenderSurface(surface)
	if surface == "" {
		return fmt.Errorf("renderer surface is required")
	}
	if renderer == nil {
		return fmt.Errorf("renderer callback is required")
	}
	if len(allowedKinds) == 0 {
		allowedKinds = []RenderKind{RenderKindText}
	}

	m.mu.RLock()
	trust := TrustClassDeclarative
	if reg, ok := m.extensions[extensionID]; ok {
		trust = reg.trust
	}
	m.mu.RUnlock()

	kindSet := make(map[RenderKind]struct{}, len(allowedKinds))
	for _, kind := range allowedKinds {
		kind = normalizeRenderKind(kind)
		capability, err := renderKindCapability(kind)
		if err != nil {
			return err
		}
		if !m.permissions.AllowsCapability(extensionID, trust, capability) {
			return fmt.Errorf("extension %q capability %q is not approved", extensionID, capability)
		}
		kindSet[kind] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.renderers[surface]; ok && existing.owner != extensionID {
		return fmt.Errorf("renderer surface %q already registered by extension %q", surface, existing.owner)
	}
	m.renderers[surface] = rendererRegistration{
		owner:        extensionID,
		allowedKinds: kindSet,
		renderer:     renderer,
	}
	return nil
}

func (m *Manager) RendererOwner(surface RenderSurface) (string, bool) {
	surface = normalizeRenderSurface(surface)
	m.mu.RLock()
	defer m.mu.RUnlock()
	registration, ok := m.renderers[surface]
	if !ok {
		return "", false
	}
	return registration.owner, true
}

func (m *Manager) UnregisterRenderers(extensionID string) {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for surface, registration := range m.renderers {
		if registration.owner == extensionID {
			delete(m.renderers, surface)
		}
	}
}

func (m *Manager) UnregisterExtension(extensionID string) {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return
	}

	var hostedClient HostedClient
	m.mu.Lock()
	for name, registration := range m.commands {
		if registration.owner == extensionID {
			delete(m.commands, name)
		}
	}
	for name, registration := range m.tools {
		if registration.owner == extensionID {
			delete(m.tools, name)
			delete(m.runtimeTools, name)
		}
	}
	for surface, registration := range m.renderers {
		if registration.owner == extensionID {
			delete(m.renderers, surface)
		}
	}
	for event, subscribers := range m.subscriptions {
		delete(subscribers, extensionID)
		if len(subscribers) == 0 {
			delete(m.subscriptions, event)
		}
	}
	delete(m.eventHandlers, extensionID)
	delete(m.extensions, extensionID)
	if client, ok := m.hostedClients[extensionID]; ok {
		hostedClient = client
		delete(m.hostedClients, extensionID)
	}
	m.mu.Unlock()

	if hostedClient != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
		_ = hostedClient.Shutdown(shutdownCtx)
		cancel()
	}
}

func (m *Manager) Render(
	ctx context.Context,
	extensionID string,
	surface RenderSurface,
	payload map[string]any,
	timeout time.Duration,
) (RenderResult, bool, error) {
	extensionID = strings.TrimSpace(extensionID)
	surface = normalizeRenderSurface(surface)
	if extensionID == "" || surface == "" {
		return RenderResult{}, false, nil
	}

	m.mu.RLock()
	registration, ok := m.renderers[surface]
	if !ok || registration.owner != extensionID || registration.renderer == nil {
		m.mu.RUnlock()
		return RenderResult{}, false, nil
	}
	allowedKinds := make(map[RenderKind]struct{}, len(registration.allowedKinds))
	for kind := range registration.allowedKinds {
		allowedKinds[kind] = struct{}{}
	}
	renderer := registration.renderer
	m.mu.RUnlock()

	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}
	if ctx == nil {
		ctx = context.Background()
	}
	renderCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type renderOutcome struct {
		result RenderResult
		err    error
	}
	done := make(chan renderOutcome, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- renderOutcome{err: fmt.Errorf("renderer panic: %v", rec)}
			}
		}()
		result, err := renderer(renderCtx, RenderRequest{
			Surface: surface,
			Payload: payload,
		})
		done <- renderOutcome{result: result, err: err}
	}()

	select {
	case <-renderCtx.Done():
		return RenderResult{}, true, renderCtx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return RenderResult{}, true, outcome.err
		}
		if err := outcome.result.Validate(); err != nil {
			return RenderResult{}, true, err
		}
		outcome.result.Kind = normalizeRenderKind(outcome.result.Kind)
		if _, ok := allowedKinds[outcome.result.Kind]; !ok {
			return RenderResult{}, true, fmt.Errorf("renderer returned kind %q which is not allowed", outcome.result.Kind)
		}
		return outcome.result, true, nil
	}
}

func validateRuntimeSpec(manifest Manifest) error {
	switch manifest.runtimeType() {
	case RuntimeTypeDeclarative, RuntimeTypeCompiledIn:
		return nil
	case RuntimeTypeHostedStdioJSONRPC:
		if strings.TrimSpace(manifest.Runtime.Command) == "" {
			return fmt.Errorf("%s requires runtime.command", RuntimeTypeHostedStdioJSONRPC)
		}
		return nil
	default:
		return fmt.Errorf("unsupported runtime type %q", manifest.Runtime.Type)
	}
}

func normalizeCommandName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(name, "/"))
}

// manifestToRequestedServices synthesizes a v2 service request list
// from a manifest's declared Capabilities. Each distinct service prefix
// (the part of a capability before the first ".") becomes one
// ServiceRequest at version 1. Plan 2 will replace this with an
// explicit RequestedServices field on the manifest.
func manifestToRequestedServices(manifest Manifest) []hostproto.ServiceRequest {
	seen := map[string]bool{}
	var out []hostproto.ServiceRequest
	for _, capability := range manifest.Capabilities {
		prefix := capabilityServicePrefix(string(capability))
		if prefix == "" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, hostproto.ServiceRequest{
			Service: prefix,
			Version: 1,
		})
	}
	return out
}

// capabilityServicePrefix returns the service-name prefix of a
// capability string. "ui.status" -> "ui", "commands.register" ->
// "commands", "render.text" -> "render".
func capabilityServicePrefix(capability string) string {
	for i := 0; i < len(capability); i++ {
		if capability[i] == '.' {
			return capability[:i]
		}
	}
	return ""
}

func renderKindCapability(kind RenderKind) (Capability, error) {
	switch kind {
	case RenderKindText:
		return CapabilityRenderText, nil
	case RenderKindMarkdown:
		return CapabilityRenderMarkdown, nil
	default:
		return "", fmt.Errorf("unsupported render kind %q", kind)
	}
}

// managerCapabilityGate adapts *Permissions to the
// services.CapabilityGate interface. It looks up the registered
// extension's trust class to pick the right gate policy. If the
// extension is unknown, it defaults to HostedThirdParty (the most
// restrictive class).
type managerCapabilityGate struct {
	permissions *Permissions
	manager     *Manager
}

// Allowed consults Permissions.AllowsService for the looked-up trust.
func (g managerCapabilityGate) Allowed(extensionID, service, method string) bool {
	trust := TrustClassHostedThirdParty
	if g.manager != nil {
		g.manager.mu.RLock()
		if reg, ok := g.manager.extensions[extensionID]; ok {
			trust = reg.trust
		}
		g.manager.mu.RUnlock()
	}
	return g.permissions.AllowsService(extensionID, trust, service, method)
}

// managerUISink adapts uiservice.Sink to the manager's existing UI
// intent fan-out (SubscribeUIIntents subscribers receive a status
// envelope each time SetStatus is called).
type managerUISink struct {
	manager *Manager
}

func (s managerUISink) SetStatus(entry uiservice.StatusEntry) {
	_ = s.manager.EmitUIIntent(entry.ExtensionID, UIIntent{
		Type:   UIIntentStatus,
		Status: &StatusIntent{Text: entry.Text},
	})
}

func (s managerUISink) ClearStatus(extensionID string) {
	// No existing "clear" channel semantic — emit an empty-text status,
	// which TUI subscribers can interpret as "no status from this ext".
	_ = s.manager.EmitUIIntent(extensionID, UIIntent{
		Type:   UIIntentStatus,
		Status: &StatusIntent{Text: ""},
	})
}

// managerCommandsSink adapts commandsservice.Sink to the manager's
// existing dynamic command registry.
type managerCommandsSink struct {
	manager *Manager
}

func (s managerCommandsSink) RegisterCommand(reg commandsservice.Registration) error {
	return s.manager.RegisterDynamicCommand(reg.ExtensionID, SlashCommand{
		Name:        reg.Name,
		Description: reg.Description,
		Prompt:      reg.Prompt,
	})
}

func (s managerCommandsSink) UnregisterCommand(input commandsservice.UnregisterInput) error {
	// Plan 1 does not implement per-command unregister; the underlying
	// flow lands in Plan 2 alongside the ext_call mirror direction.
	_ = input
	return nil
}

func normalizeRenderSurface(surface RenderSurface) RenderSurface {
	return RenderSurface(strings.TrimSpace(string(surface)))
}

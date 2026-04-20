package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"

	"github.com/pizzaface/go-pi/internal/config"
	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/compiled"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/lifecycle"
	"github.com/pizzaface/go-pi/internal/extension/loader"
	"github.com/pizzaface/go-pi/internal/provider"
	"github.com/pizzaface/go-pi/internal/tools"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// RuntimeConfig controls extension runtime bootstrap.
type RuntimeConfig struct {
	Config          config.Config
	WorkDir         string
	Sandbox         *tools.Sandbox
	BaseInstruction string
	ScreenProvider  tools.ScreenProvider
	RestartFunc     tools.RestartFunc
	// Bridge is the session/UI bridge for spec #5 operations. Nil means
	// the NoopBridge is used (messaging + session control become no-ops).
	Bridge extapi.SessionBridge
}

// Runtime is the assembled extension runtime output consumed by CLI/TUI startup.
type Runtime struct {
	Extensions       []*host.Registration
	Tools            []tool.Tool
	Toolsets         []tool.Toolset
	Skills           []Skill
	SkillDirs        []string
	PromptTemplates  []loader.PromptTemplate
	ThemeDirs        []string
	ProviderRegistry *provider.Registry
	Manager          *host.Manager
	SlashCommands    []loader.SlashCommand
	// Legacy caller-facing fields kept as empty slices so existing callers
	// continue to compile. Behavior lands in spec #2/#3/#5.
	BeforeToolCallbacks []llmagent.BeforeToolCallback
	AfterToolCallbacks  []llmagent.AfterToolCallback
	LifecycleHooks      []HookConfig
	Instruction         string
	Lifecycle           lifecycle.Service
	// Bridge is the session/UI bridge for spec #5 operations. Nil means
	// lifecycle hooks that append entries are no-ops.
	Bridge extapi.SessionBridge
	// HostedToolRegistry is the live registry of tools contributed by hosted
	// extensions. Populated as each extension's pi.tool/register call lands.
	HostedToolRegistry *extapi.HostedToolRegistry
	// Readiness tracks handshake completion of hosted extensions registered
	// at runtime build time. WaitForHostedReady blocks on it.
	Readiness *extapi.Readiness
}

// HookConfig is one aggregated lifecycle hook, copied from a
// piapi.HookConfig plus the owning extension's ID.
type HookConfig struct {
	ExtensionID string
	Event       string
	Command     string
	Tools       []string
	Timeout     int
	Critical    bool
}

// Lifecycle event names fired at the 5 standard call sites.
const (
	LifecycleEventStartup      = "startup"
	LifecycleEventSessionStart = "session_start"
	LifecycleEventBeforeTurn   = "before_turn"
	LifecycleEventAfterTurn    = "after_turn"
	LifecycleEventShutdown     = "shutdown"
)

// BuildRuntime assembles core tools and extension contributions behind one startup boundary.
func BuildRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	coreTools, err := tools.CoreTools(cfg.Sandbox)
	if err != nil {
		return nil, fmt.Errorf("creating core tools: %w", err)
	}
	if cfg.ScreenProvider != nil {
		screenTool, err := tools.NewScreenTool(cfg.ScreenProvider)
		if err != nil {
			return nil, fmt.Errorf("creating screen tool: %w", err)
		}
		coreTools = append(coreTools, screenTool)
	}
	if cfg.RestartFunc != nil {
		restartTool, err := tools.NewRestartTool(cfg.RestartFunc)
		if err != nil {
			return nil, fmt.Errorf("creating restart tool: %w", err)
		}
		coreTools = append(coreTools, restartTool)
	}

	resources := loader.DiscoverResourceDirs(cfg.WorkDir)
	promptTemplates, err := loader.LoadPromptTemplates(resources.PromptDirs...)
	if err != nil {
		return nil, fmt.Errorf("loading prompt templates: %w", err)
	}
	providerRegistry, err := BuildProviderRegistry(cfg.WorkDir, cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("building provider registry: %w", err)
	}

	approvalsPath := DefaultApprovalsPath()
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		return nil, fmt.Errorf("loading approvals: %w", err)
	}
	manager := host.NewManager(gate)

	registry := extapi.NewHostedToolRegistry()
	readiness := extapi.NewReadiness()
	toolsets := []tool.Toolset{extapi.NewHostedToolset(registry)}

	instruction := strings.TrimSpace(cfg.BaseInstruction)
	var registrations []*host.Registration

	// Compiled-in extensions.
	for _, entry := range compiled.Compiled() {
		reg := &host.Registration{
			ID:       entry.Name,
			Mode:     "compiled-in",
			Trust:    host.TrustCompiledIn,
			Metadata: entry.Metadata,
		}
		if err := manager.Register(reg); err != nil {
			return nil, fmt.Errorf("register compiled-in %q: %w", entry.Name, err)
		}
		api := extapi.NewCompiled(reg, manager, cfg.Bridge)
		reg.API = api
		if err := entry.Register(api); err != nil {
			return nil, fmt.Errorf("compiled-in %q Register: %w", entry.Name, err)
		}
		// Pull registered tools + prompt into runtime.
		for _, t := range extapi.CompiledTools(api) {
			adapter, err := extapi.NewPiapiToolAdapter(t)
			if err != nil {
				return nil, fmt.Errorf("compiled-in %q tool %q: %w", entry.Name, t.Name, err)
			}
			coreTools = append(coreTools, adapter)
		}
		if entry.Metadata.Prompt != "" {
			instruction += "\n\n# Extension: " + entry.Name + "\n\n" + entry.Metadata.Prompt
		}
		registrations = append(registrations, reg)
	}

	// Hosted candidates. Discovery records them; launching is the caller's
	// responsibility via host.LaunchHosted (Task 39).
	candidates, err := loader.Discover(cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("discover hosted candidates: %w", err)
	}
	for _, c := range candidates {
		trust := host.TrustThirdParty
		reg := &host.Registration{
			ID:       c.Metadata.Name,
			Mode:     c.Mode.String(),
			Trust:    trust,
			Metadata: c.Metadata,
			WorkDir:  c.Dir,
		}
		if err := manager.Register(reg); err != nil {
			// Duplicate with a compiled-in? Skip.
			continue
		}
		// Track readiness for hosted extensions that passed the gate and are
		// ready to launch. Pending/denied ones don't contribute to startup
		// readiness — they'll never handshake this session.
		if reg.State == host.StateReady {
			readiness.Track(reg.ID)
		}
		// On RPC close, drop the extension's tools from the live registry
		// and mark readiness as errored so Wait unblocks.
		extID := reg.ID
		manager.OnClose(extID, func() {
			registry.RemoveExt(extID)
			readiness.MarkErrored(extID, errors.New("connection closed"))
		})
		registrations = append(registrations, reg)
	}

	skillDirs := append([]string{}, resources.SkillDirs...)
	skills, err := LoadSkills(skillDirs...)
	if err != nil {
		return nil, fmt.Errorf("loading skills: %w", err)
	}
	if len(skills) > 0 {
		instruction += "\n\n# Available Skills\n\n"
		instruction += "The following skills provide specialized instructions for specific tasks.\n"
		instruction += "When a task matches a skill's description, use your file-read tool to load\n"
		instruction += "the SKILL.md at the listed location before proceeding.\n"
		instruction += "When a skill references relative paths, resolve them against the skill's\n"
		instruction += "directory (the parent of SKILL.md) and use absolute paths in tool calls.\n\n"
		for _, s := range skills {
			instruction += fmt.Sprintf("- /%s: %s\n  location: %s\n", s.Name, s.Description, s.Location)
		}
	}

	var slashCommands []loader.SlashCommand
	for _, tpl := range promptTemplates {
		slashCommands = append(slashCommands, tpl.SlashCommand())
	}

	// Aggregate lifecycle hooks from all registrations.
	var lifecycleHooks []HookConfig
	for _, reg := range registrations {
		for _, h := range reg.Metadata.Hooks {
			if h.Critical && reg.Trust != host.TrustFirstParty && reg.Trust != host.TrustCompiledIn {
				continue
			}
			lifecycleHooks = append(lifecycleHooks, HookConfig{
				ExtensionID: reg.ID,
				Event:       h.Event,
				Command:     h.Command,
				Tools:       append([]string(nil), h.Tools...),
				Timeout:     h.Timeout,
				Critical:    h.Critical,
			})
		}
	}

	rt := &Runtime{
		Extensions:         registrations,
		Tools:              coreTools,
		Toolsets:           toolsets,
		Skills:             skills,
		SkillDirs:          skillDirs,
		PromptTemplates:    promptTemplates,
		ThemeDirs:          resources.ThemeDirs,
		ProviderRegistry:   providerRegistry,
		Manager:            manager,
		SlashCommands:      slashCommands,
		Instruction:        instruction,
		LifecycleHooks:     lifecycleHooks,
		Bridge:             cfg.Bridge,
		HostedToolRegistry: registry,
		Readiness:          readiness,
	}
	rt.Lifecycle = lifecycle.New(manager, gate, approvalsPath, cfg.WorkDir, cfg.Bridge)
	rt.Lifecycle.SetShutdownHook(rt.RunLifecycleHooks, "")
	// Wire registry + readiness into the lifecycle service via a type-asserted
	// optional interface. These setters live on *service (the concrete impl)
	// rather than the lifecycle.Service interface to keep the TUI's fake
	// implementations compiling without stubs. See Task 9.
	if s, ok := rt.Lifecycle.(interface {
		SetRegistry(*extapi.HostedToolRegistry)
		SetReadiness(*extapi.Readiness)
	}); ok {
		s.SetRegistry(registry)
		s.SetReadiness(readiness)
	}

	// Fire startup hooks now that all extensions are registered.
	names := make([]string, 0, len(registrations))
	for _, r := range registrations {
		names = append(names, r.ID)
	}
	if err := rt.RunLifecycleHooks(ctx, LifecycleEventStartup, map[string]any{
		"work_dir":   cfg.WorkDir,
		"extensions": names,
	}); err != nil {
		return nil, fmt.Errorf("startup hooks: %w", err)
	}

	return rt, nil
}

// RunLifecycleHooks fires all hooks subscribed to the given event in
// declaration order. Hook errors are logged but don't abort the caller
// unless a hook has Critical=true and the event is "startup".
//
// Hooks are invoked by synthesizing a ToolCall against the extension's
// registered tool of the same name. before_turn hook results with text
// content are appended via Bridge.AppendEntry so the LLM sees them.
func (r *Runtime) RunLifecycleHooks(ctx context.Context, event string, data map[string]any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("RunLifecycleHooks: marshal: %w", err)
	}

	for _, h := range r.LifecycleHooks {
		if h.Event != event {
			continue
		}
		if !hookMatchesTools(h.Tools, r.activeToolNames()) {
			continue
		}

		reg := r.findRegistration(h.ExtensionID)
		if reg == nil || reg.API == nil {
			continue
		}
		toolMap := extapi.CompiledTools(reg.API)
		if toolMap == nil {
			continue
		}
		toolDesc, ok := toolMap[h.Command]
		if !ok {
			continue
		}

		timeout := time.Duration(h.Timeout) * time.Millisecond
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		call := piapi.ToolCall{
			ID:   fmt.Sprintf("hook-%s-%d", event, time.Now().UnixNano()),
			Name: h.Command,
			Args: payload,
		}
		result, hookErr := toolDesc.Execute(hookCtx, call, nil)
		cancel()

		if hookErr != nil {
			if event == "startup" && h.Critical {
				return fmt.Errorf("critical startup hook %s/%s failed: %w", h.ExtensionID, h.Command, hookErr)
			}
			continue
		}

		if event == "before_turn" && r.Bridge != nil {
			for _, c := range result.Content {
				if c.Type == "text" {
					_ = r.Bridge.AppendEntry(h.ExtensionID, "hook/before_turn", c.Text)
				}
			}
		}
	}
	return nil
}

// hookMatchesTools returns true when filter is empty (no constraint) or
// when filter contains "*" or any name found in active.
func hookMatchesTools(filter, active []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == "*" {
			return true
		}
		for _, a := range active {
			if a == f {
				return true
			}
		}
	}
	return false
}

func (r *Runtime) activeToolNames() []string {
	out := make([]string, 0, len(r.Tools))
	for _, t := range r.Tools {
		out = append(out, t.Name())
	}
	return out
}

func (r *Runtime) findRegistration(id string) *host.Registration {
	for _, reg := range r.Extensions {
		if reg.ID == id {
			return reg
		}
	}
	return nil
}

// Reload re-reads approvals.json and updates the gate in place without
// restarting any running extensions.
func (r *Runtime) Reload(ctx context.Context) error {
	_ = ctx
	if r.Manager == nil {
		return nil
	}
	approvalsPath := DefaultApprovalsPath()
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		return fmt.Errorf("reload approvals: %w", err)
	}
	r.Manager.SetGate(gate)
	return nil
}

// WaitForHostedReady blocks until every tracked hosted extension has
// completed its handshake (or errored), the context is cancelled, or the
// timeout elapses. Returns nil when Readiness is nil (no extensions built).
func (r *Runtime) WaitForHostedReady(ctx context.Context, timeout time.Duration) error {
	if r.Readiness == nil {
		return nil
	}
	return r.Readiness.Wait(ctx, timeout)
}

// DefaultApprovalsPath returns the path to the approvals.json file that
// gates hosted extensions. Defaults to <userHome>/.go-pi/extensions/approvals.json.
func DefaultApprovalsPath() string {
	home, err := loader.UserHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-pi", "extensions", "approvals.json")
}

// ensure the piapi import stays live even when no local code path references it.
var _ = piapi.EventSessionStart

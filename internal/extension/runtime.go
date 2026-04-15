package extension

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"

	"github.com/dimetron/pi-go/internal/config"
	extapi "github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/internal/extension/compiled"
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/loader"
	"github.com/dimetron/pi-go/internal/provider"
	"github.com/dimetron/pi-go/internal/tools"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// RuntimeConfig controls extension runtime bootstrap.
type RuntimeConfig struct {
	Config          config.Config
	WorkDir         string
	Sandbox         *tools.Sandbox
	BaseInstruction string
	ScreenProvider  tools.ScreenProvider
	RestartFunc     tools.RestartFunc
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
}

// HookConfig is a spec #5 stub retained to keep CLI callers compiling.
type HookConfig struct {
	Event   string
	Command string
	Tools   []string
	Timeout int
}

// Lifecycle event name stubs — spec #5 will wire these. Retained so CLI
// code continues to compile without conditional import.
const (
	LifecycleEventStartup      = "startup"
	LifecycleEventSessionStart = "session_start"
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
		api := extapi.NewCompiled(reg, manager)
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

	rt := &Runtime{
		Extensions:       registrations,
		Tools:            coreTools,
		Skills:           skills,
		SkillDirs:        skillDirs,
		PromptTemplates:  promptTemplates,
		ThemeDirs:        resources.ThemeDirs,
		ProviderRegistry: providerRegistry,
		Manager:          manager,
		SlashCommands:    slashCommands,
		Instruction:      instruction,
	}
	_ = ctx
	return rt, nil
}

// RunLifecycleHooks is a spec #5 stub — no hooks are defined in spec #1.
func (r *Runtime) RunLifecycleHooks(ctx context.Context, event string, data map[string]any) error {
	_ = ctx
	_ = event
	_ = data
	return nil
}

// DefaultApprovalsPath returns the path to the approvals.json file that
// gates hosted extensions. Defaults to <userHome>/.pi-go/extensions/approvals.json.
func DefaultApprovalsPath() string {
	home, err := loader.UserHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi-go", "extensions", "approvals.json")
}

// ensure the piapi import stays live even when no local code path references it.
var _ = piapi.EventSessionStart

package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/provider"
	"github.com/dimetron/pi-go/internal/tools"
)

// RuntimeConfig controls extension runtime bootstrap.
type RuntimeConfig struct {
	Config           config.Config
	WorkDir          string
	Sandbox          *tools.Sandbox
	BaseInstruction  string
	CompiledRegistry *Registry
	ScreenProvider   tools.ScreenProvider
	RestartFunc      tools.RestartFunc
}

// Runtime is the assembled extension runtime output consumed by CLI/TUI startup.
type Runtime struct {
	Extensions          []Manifest
	Tools               []tool.Tool
	Toolsets            []tool.Toolset
	Skills              []Skill
	SkillDirs           []string
	PromptTemplates     []PromptTemplate
	ThemeDirs           []string
	ProviderRegistry    *provider.Registry
	Manager             *Manager
	SlashCommands       []SlashCommand
	BeforeToolCallbacks []llmagent.BeforeToolCallback
	AfterToolCallbacks  []llmagent.AfterToolCallback
	LifecycleHooks      []HookConfig
	Instruction         string
}

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

	resources := DiscoverResourceDirs(cfg.WorkDir)
	manifests, err := LoadManifests(resources.ExtensionDirs...)
	if err != nil {
		return nil, fmt.Errorf("loading extension manifests: %w", err)
	}
	promptTemplates, err := LoadPromptTemplates(resources.PromptDirs...)
	if err != nil {
		return nil, fmt.Errorf("loading prompt templates: %w", err)
	}
	providerRegistry, err := BuildProviderRegistry(cfg.WorkDir, cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("building provider registry: %w", err)
	}
	approvalsPath := DefaultApprovalsPath()
	permissions, err := LoadPermissions(approvalsPath)
	if err != nil {
		return nil, fmt.Errorf("loading extension approvals: %w", err)
	}
	manager := NewManager(ManagerOptions{
		Permissions:   permissions,
		Registry:      cfg.CompiledRegistry,
		ApprovalsPath: approvalsPath,
	})
	if err := manager.RegisterManifests(manifests); err != nil {
		return nil, fmt.Errorf("registering extension manifests: %w", err)
	}
	if err := manager.RegisterCompiledExtensions(); err != nil {
		return nil, fmt.Errorf("registering compiled extensions: %w", err)
	}
	if err := manager.StartHostedExtensions(ctx, "startup"); err != nil {
		return nil, fmt.Errorf("starting hosted extensions: %w", err)
	}

	before := BuildBeforeToolCallbacks(convertConfigHooks(cfg.Config.Hooks))
	after := BuildAfterToolCallbacks(convertConfigHooks(cfg.Config.Hooks))
	skillDirs := append([]string{}, resources.SkillDirs...)
	instruction := strings.TrimSpace(cfg.BaseInstruction)

	var lifecycle []HookConfig
	var toolsets []tool.Toolset

	for _, manifest := range manifests {
		prompt, err := manifest.resolvePrompt()
		if err != nil {
			return nil, err
		}
		if prompt != "" {
			instruction += "\n\n# Extension: " + manifest.Name + "\n\n" + prompt
		}
		if dir := manifest.resolveSkillsDir(); dir != "" {
			skillDirs = append(skillDirs, dir)
		}
		before = append(before, BuildBeforeToolCallbacks(manifest.Hooks)...)
		after = append(after, BuildAfterToolCallbacks(manifest.Hooks)...)
		lifecycle = append(lifecycle, manifest.Lifecycle...)

		if len(manifest.MCPServers) > 0 {
			ts, err := BuildMCPToolsets(manifest.MCPServers)
			if err != nil {
				return nil, fmt.Errorf("building MCP toolsets for extension %q: %w", manifest.Name, err)
			}
			toolsets = append(toolsets, ts...)
		}
	}

	skillDirs = dedupeStrings(skillDirs)
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

	for _, tpl := range promptTemplates {
		if err := manager.RegisterBootstrapCommand("prompt_template:"+tpl.Name, tpl.SlashCommand()); err != nil {
			return nil, fmt.Errorf("registering prompt template slash command %q: %w", tpl.Name, err)
		}
	}
	coreTools = append(coreTools, manager.RuntimeTools()...)

	rt := &Runtime{
		Extensions:          manifests,
		Tools:               coreTools,
		Toolsets:            toolsets,
		Skills:              skills,
		SkillDirs:           skillDirs,
		PromptTemplates:     promptTemplates,
		ThemeDirs:           resources.ThemeDirs,
		ProviderRegistry:    providerRegistry,
		Manager:             manager,
		SlashCommands:       manager.SlashCommands(),
		BeforeToolCallbacks: before,
		AfterToolCallbacks:  after,
		LifecycleHooks:      lifecycle,
		Instruction:         instruction,
	}
	manager.EmitEvent(Event{
		Type:      EventStartup,
		Timestamp: time.Now(),
		Data:      map[string]any{"workdir": cfg.WorkDir},
	})

	if err := rt.RunLifecycleHooks(ctx, LifecycleEventStartup, map[string]any{"workdir": cfg.WorkDir}); err != nil {
		return nil, err
	}
	return rt, nil
}

// RunLifecycleHooks executes lifecycle hook commands for the given event.
func (r *Runtime) RunLifecycleHooks(ctx context.Context, event string, data map[string]any) error {
	for _, hook := range r.LifecycleHooks {
		if hook.Event != event {
			continue
		}
		if err := runHookCommand(ctx, hook, event, data); err != nil {
			return fmt.Errorf("lifecycle hook %q: %w", hook.Command, err)
		}
	}
	return nil
}

// LoadManifests discovers extension manifests from extension root directories.
// Later directories override earlier ones by extension name.
func LoadManifests(dirs ...string) ([]Manifest, error) {
	seen := map[string]int{}
	var manifests []Manifest
	for _, root := range dirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading extension dir %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(root, entry.Name(), "extension.json")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("reading manifest %s: %w", manifestPath, err)
			}
			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("parsing manifest %s: %w", manifestPath, err)
			}
			if manifest.Name == "" {
				manifest.Name = entry.Name()
			}
			manifest.Dir = filepath.Join(root, entry.Name())
			if !manifest.enabled() {
				continue
			}
			if idx, ok := seen[manifest.Name]; ok {
				manifests[idx] = manifest
			} else {
				seen[manifest.Name] = len(manifests)
				manifests = append(manifests, manifest)
			}
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
	return manifests, nil
}

func convertConfigHooks(cfgHooks []config.HookConfig) []HookConfig {
	hooks := make([]HookConfig, len(cfgHooks))
	for i, h := range cfgHooks {
		hooks[i] = HookConfig{
			Event:   h.Event,
			Command: h.Command,
			Tools:   h.Tools,
			Timeout: h.Timeout,
		}
	}
	return hooks
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

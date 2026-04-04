package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/tools"
)

const (
	LifecycleEventStartup      = "startup"
	LifecycleEventSessionStart = "session_start"
)

// SlashCommand is a narrow TUI extension point backed by prompt rendering.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// Render returns the prompt text for the command using a minimal args placeholder.
func (c SlashCommand) Render(args []string) string {
	tpl := PromptTemplate{Name: c.Name, Description: c.Description, Prompt: c.Prompt}
	return tpl.Render(args)
}

// TUIConfig defines narrow, Bubble Tea-aligned TUI contribution points.
type TUIConfig struct {
	Commands []SlashCommand `json:"commands,omitempty"`
}

// Manifest describes a discovered extension instance.
type Manifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Prompt      string            `json:"prompt,omitempty"`
	PromptFile  string            `json:"prompt_file,omitempty"`
	SkillsDir   string            `json:"skills_dir,omitempty"`
	Hooks       []HookConfig      `json:"hooks,omitempty"`
	Lifecycle   []HookConfig      `json:"lifecycle,omitempty"`
	MCPServers  []MCPServerConfig `json:"mcp_servers,omitempty"`
	TUI         TUIConfig         `json:"tui,omitempty"`

	Dir string `json:"-"`
}

func (m Manifest) enabled() bool {
	return m.Enabled == nil || *m.Enabled
}

func (m Manifest) resolvePrompt() (string, error) {
	if strings.TrimSpace(m.PromptFile) == "" {
		return strings.TrimSpace(m.Prompt), nil
	}
	data, err := os.ReadFile(filepath.Join(m.Dir, m.PromptFile))
	if err != nil {
		return "", fmt.Errorf("reading prompt file for extension %q: %w", m.Name, err)
	}
	if strings.TrimSpace(m.Prompt) == "" {
		return strings.TrimSpace(string(data)), nil
	}
	return strings.TrimSpace(m.Prompt) + "\n\n" + strings.TrimSpace(string(data)), nil
}

func (m Manifest) resolveSkillsDir() string {
	if strings.TrimSpace(m.SkillsDir) == "" {
		return ""
	}
	return filepath.Join(m.Dir, m.SkillsDir)
}

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
	Extensions          []Manifest
	Tools               []tool.Tool
	Toolsets            []tool.Toolset
	Skills              []Skill
	SkillDirs           []string
	PromptTemplates     []PromptTemplate
	ThemeDirs           []string
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

	before := BuildBeforeToolCallbacks(convertConfigHooks(cfg.Config.Hooks))
	after := BuildAfterToolCallbacks(convertConfigHooks(cfg.Config.Hooks))
	skillDirs := append([]string{}, resources.SkillDirs...)
	instruction := strings.TrimSpace(cfg.BaseInstruction)

	var lifecycle []HookConfig
	var toolsets []tool.Toolset
	var slashCommands []SlashCommand

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
		slashCommands = append(slashCommands, manifest.TUI.Commands...)

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
		for _, s := range skills {
			instruction += fmt.Sprintf("- /%s: %s\n", s.Name, s.Description)
		}
	}

	for _, tpl := range promptTemplates {
		slashCommands = append(slashCommands, tpl.SlashCommand())
	}

	rt := &Runtime{
		Extensions:          manifests,
		Tools:               coreTools,
		Toolsets:            toolsets,
		Skills:              skills,
		SkillDirs:           skillDirs,
		PromptTemplates:     promptTemplates,
		ThemeDirs:           resources.ThemeDirs,
		SlashCommands:       normalizeSlashCommands(slashCommands),
		BeforeToolCallbacks: before,
		AfterToolCallbacks:  after,
		LifecycleHooks:      lifecycle,
		Instruction:         instruction,
	}

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

func normalizeSlashCommands(cmds []SlashCommand) []SlashCommand {
	seen := make(map[string]SlashCommand, len(cmds))
	order := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		name := strings.TrimSpace(strings.TrimPrefix(cmd.Name, "/"))
		if name == "" {
			continue
		}
		cmd.Name = name
		if _, ok := seen[name]; !ok {
			order = append(order, name)
		}
		seen[name] = cmd
	}
	sort.Strings(order)
	out := make([]SlashCommand, 0, len(order))
	for _, name := range order {
		out = append(out, seen[name])
	}
	return out
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

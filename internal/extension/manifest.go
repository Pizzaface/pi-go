package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// RuntimeTypeDeclarative means no process/runtime is needed.
	// This is the implicit default when runtime.type is omitted.
	RuntimeTypeDeclarative = ""
	// RuntimeTypeCompiledIn resolves an extension through the compiled registry.
	RuntimeTypeCompiledIn = "compiled_in"
	// RuntimeTypeHostedStdioJSONRPC launches a hosted process and speaks JSON-RPC over stdio.
	RuntimeTypeHostedStdioJSONRPC = "hosted_stdio_jsonrpc"
)

// RuntimeSpec defines optional runtime metadata for an extension.
type RuntimeSpec struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

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
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	PromptFile   string            `json:"prompt_file,omitempty"`
	SkillsDir    string            `json:"skills_dir,omitempty"`
	Hooks        []HookConfig      `json:"hooks,omitempty"`
	Lifecycle    []HookConfig      `json:"lifecycle,omitempty"`
	MCPServers   []MCPServerConfig `json:"mcp_servers,omitempty"`
	TUI          TUIConfig         `json:"tui,omitempty"`
	Runtime      RuntimeSpec       `json:"runtime,omitempty"`
	Capabilities []Capability      `json:"capabilities,omitempty"`

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

// runtimeType resolves the normalized runtime type.
//
// Rules:
// - missing runtime block means declarative-only
// - compiled_in is resolved via registry, not executable paths
// - hosted_stdio_jsonrpc requires a command
func (m Manifest) runtimeType() string {
	return strings.TrimSpace(m.Runtime.Type)
}

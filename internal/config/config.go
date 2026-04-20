package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pizzaface/go-pi/internal/provider"
)

// HookConfig defines a shell command hook for tool call events.
type HookConfig struct {
	Event   string   `json:"event"`
	Command string   `json:"command"`
	Tools   []string `json:"tools,omitempty"`
	Timeout int      `json:"timeout,omitempty"`
}

// RoleConfig maps a role to a specific model and optional provider override.
type RoleConfig struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
}

// ErrNoDefaultRole is returned when no default role is configured.
var ErrNoDefaultRole = errors.New("no default model role configured")

// Config holds all go-pi configuration.
type Config struct {
	Roles           map[string]RoleConfig      `json:"roles,omitempty"`
	DefaultModel    string                     `json:"defaultModel,omitempty"` // deprecated: use roles
	DefaultProvider string                     `json:"defaultProvider"`
	ThinkingLevel   string                     `json:"thinkingLevel"`
	Theme           string                     `json:"theme"`
	ExtraHeaders    map[string]string          `json:"extraHeaders,omitempty"`
	InsecureSkipTLS bool                       `json:"insecureSkipTLS,omitempty"`
	Tools           map[string]any             `json:"tools,omitempty"`
	MCP             *MCPConfig                 `json:"mcp,omitempty"` // optional building blocks for custom integrations; ignored by default startup
	Hooks           []HookConfig               `json:"hooks,omitempty"`
	MaxDailyTokens  int64                      `json:"maxDailyTokens,omitempty"` // 0 = unlimited
	Compactor       *CompactorConfig           `json:"compactor,omitempty"`
	Providers       []provider.Definition      `json:"providers,omitempty"`
	Models          []provider.ModelDefinition `json:"models,omitempty"`
}

// CompactorConfig holds user-overridable compaction settings.
// When nil in config, defaults are applied by the tools package.
type CompactorConfig struct {
	Enabled             *bool  `json:"enabled,omitempty"`
	SourceCodeFiltering string `json:"source_code_filtering,omitempty"` // "none", "minimal", "aggressive"
	MaxChars            int    `json:"max_chars,omitempty"`
	MaxLines            int    `json:"max_lines,omitempty"`
}

type MCPConfig struct {
	Servers []MCPServer `json:"servers"`
}

type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Defaults returns a Config with default values.
func Defaults() Config {
	return Config{
		Roles: map[string]RoleConfig{
			"default": {Model: "gpt-5.4"},
		},
		DefaultProvider: "openai",
		ThinkingLevel:   "medium",
		Theme:           "default",
	}
}

// Known model prefixes for auto-detecting provider.
var modelPrefixes = map[string]string{
	"claude": "anthropic",
	"gpt":    "openai",
	"gpt-5":  "openai",
	"o1":     "openai",
	"o3":     "openai",
	"o4":     "openai",
	"gemini": "gemini",
}

// ResolveRole returns the model name and provider for a given role.
// Falls back: requested role → "default" role → error.
func (c *Config) ResolveRole(role string) (model string, prov string, err error) {
	return c.ResolveRoleWithRegistry(role, nil)
}

func (c *Config) resolveRoleConfig(role string) (RoleConfig, error) {
	if len(c.Roles) == 0 {
		return RoleConfig{}, ErrNoDefaultRole
	}
	rc, ok := c.Roles[role]
	if !ok {
		rc, ok = c.Roles["default"]
		if !ok {
			return RoleConfig{}, ErrNoDefaultRole
		}
	}
	if rc.Model == "" {
		return RoleConfig{}, fmt.Errorf("role %q has no model configured", role)
	}
	return rc, nil
}

// ResolveRoleInfoWithRegistry resolves a role using an optional provider registry.
func (c *Config) ResolveRoleInfoWithRegistry(role string, reg *provider.Registry) (provider.Info, error) {
	rc, err := c.resolveRoleConfig(role)
	if err != nil {
		return provider.Info{}, err
	}

	if reg != nil {
		info, resolveErr := reg.Resolve(rc.Model, rc.Provider)
		if resolveErr == nil {
			return info, nil
		}
		if rc.Provider != "" {
			return provider.Info{Provider: rc.Provider, Family: rc.Provider, Model: rc.Model, Ollama: rc.Provider == "ollama"}, nil
		}
		if autoDetectProvider(rc.Model) == "" {
			return provider.Info{}, resolveErr
		}
	}

	prov := rc.Provider
	if prov == "" {
		prov = autoDetectProvider(rc.Model)
		if prov == "" {
			prov = c.DefaultProvider
		}
	}
	return provider.Info{Provider: prov, Family: prov, Model: rc.Model, Ollama: prov == "ollama"}, nil
}

// ResolveRoleWithRegistry resolves a role using an optional provider registry.
func (c *Config) ResolveRoleWithRegistry(role string, reg *provider.Registry) (model string, prov string, err error) {
	info, err := c.ResolveRoleInfoWithRegistry(role, reg)
	if err != nil {
		return "", "", err
	}
	return info.Model, info.Provider, nil
}

func autoDetectProvider(modelName string) string {
	// Ollama suffixes → native Ollama provider.
	if strings.HasSuffix(modelName, ":cloud") || strings.HasSuffix(modelName, ":local") {
		return "ollama"
	}
	lower := strings.ToLower(modelName)
	for prefix, provider := range modelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return provider
		}
	}
	// Common Ollama model prefixes → native Ollama provider.
	ollamaPrefixes := []string{"qwen", "minimax", "deepseek", "llama", "mistral", "phi", "codellama", "gemma"}
	for _, prefix := range ollamaPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return "ollama"
		}
	}
	// ollama/ prefix → native Ollama provider.
	if strings.HasPrefix(lower, "ollama/") {
		return "ollama"
	}
	return ""
}

// Load reads config from global (~/.go-pi/config.json) and project (.go-pi/config.json),
// merging project overrides onto global.
func Load() (Config, error) {
	cfg := Defaults()

	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, ".go-pi", "config.json")
		if err := loadFile(globalPath, &cfg); err != nil && !os.IsNotExist(err) {
			return cfg, err
		}
	}

	projectPath := filepath.Join(".go-pi", "config.json")
	if err := loadFile(projectPath, &cfg); err != nil && !os.IsNotExist(err) {
		return cfg, err
	}

	// Migrate deprecated DefaultModel to roles if roles not set.
	if cfg.DefaultModel != "" && len(cfg.Roles) == 0 {
		cfg.Roles = map[string]RoleConfig{
			"default": {Model: cfg.DefaultModel},
		}
	} else if cfg.DefaultModel != "" && cfg.Roles != nil {
		// If DefaultModel is set alongside roles, update the default role if not already set.
		if _, ok := cfg.Roles["default"]; !ok {
			cfg.Roles["default"] = RoleConfig{Model: cfg.DefaultModel}
		}
	}

	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	existingProviders := append([]provider.Definition(nil), cfg.Providers...)
	existingModels := append([]provider.ModelDefinition(nil), cfg.Models...)
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	cfg.Providers = mergeProviderDefinitions(existingProviders, cfg.Providers)
	cfg.Models = mergeModelDefinitions(existingModels, cfg.Models)
	return nil
}

func mergeProviderDefinitions(base, override []provider.Definition) []provider.Definition {
	if len(base) == 0 {
		return override
	}
	merged := make([]provider.Definition, 0, len(base)+len(override))
	index := make(map[string]int, len(base)+len(override))
	for _, def := range base {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		index[name] = len(merged)
		merged = append(merged, def)
	}
	for _, def := range override {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		if idx, ok := index[name]; ok {
			merged[idx] = def
			continue
		}
		index[name] = len(merged)
		merged = append(merged, def)
	}
	return merged
}

func mergeModelDefinitions(base, override []provider.ModelDefinition) []provider.ModelDefinition {
	if len(base) == 0 {
		return override
	}
	merged := make([]provider.ModelDefinition, 0, len(base)+len(override))
	index := make(map[string]int, len(base)+len(override))
	for _, mdl := range base {
		name := strings.TrimSpace(mdl.Name)
		if name == "" {
			continue
		}
		index[name] = len(merged)
		merged = append(merged, mdl)
	}
	for _, mdl := range override {
		name := strings.TrimSpace(mdl.Name)
		if name == "" {
			continue
		}
		if idx, ok := index[name]; ok {
			merged[idx] = mdl
			continue
		}
		index[name] = len(merged)
		merged = append(merged, mdl)
	}
	return merged
}

// APIKeys returns detected API keys from environment variables.
// For Anthropic, checks ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN (Ollama compatibility).
func APIKeys() map[string]string {
	keys := make(map[string]string)
	envVars := map[string][]string{
		"anthropic":    {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
		"openai":       {"OPENAI_API_KEY"},
		"gemini":       {"GOOGLE_API_KEY", "GEMINI_API_KEY"},
		"mistral":      {"MISTRAL_API_KEY"},
		"groq":         {"GROQ_API_KEY"},
		"xai":          {"XAI_API_KEY"},
		"openrouter":   {"OPENROUTER_API_KEY"},
		"azure-openai": {"AZURE_OPENAI_API_KEY"},
	}
	for provider, vars := range envVars {
		for _, envVar := range vars {
			if val := os.Getenv(envVar); val != "" {
				keys[provider] = val
				break
			}
		}
	}
	return keys
}

// BaseURLs returns provider base URLs from environment variables.
// Supports ANTHROPIC_BASE_URL (Ollama compatibility), OPENAI_BASE_URL, and GEMINI_BASE_URL.
func BaseURLs() map[string]string {
	urls := make(map[string]string)
	envVars := map[string]string{
		"anthropic": "ANTHROPIC_BASE_URL",
		"openai":    "OPENAI_BASE_URL",
		"gemini":    "GEMINI_BASE_URL",
	}
	for provider, envVar := range envVars {
		if val := os.Getenv(envVar); val != "" {
			urls[provider] = val
		}
	}
	return urls
}

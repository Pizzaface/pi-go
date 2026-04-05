package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MatchRule describes how a provider claims model names.
type MatchRule struct {
	Prefix      string `json:"prefix,omitempty"`
	Suffix      string `json:"suffix,omitempty"`
	StripPrefix bool   `json:"strip_prefix,omitempty"`
}

// Definition describes a provider compatible with one of the built-in families.
type Definition struct {
	Name           string            `json:"name"`
	Family         string            `json:"family"`
	APIKeyEnv      []string          `json:"api_key_env,omitempty"`
	BaseURLEnv     string            `json:"base_url_env,omitempty"`
	DefaultBaseURL string            `json:"default_base_url,omitempty"`
	DefaultHeaders map[string]string `json:"default_headers,omitempty"`
	PingEndpoint   string            `json:"ping_endpoint,omitempty"`
	Match          []MatchRule       `json:"match,omitempty"`
}

// ModelDefinition maps an external model name to a provider and target model.
type ModelDefinition struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Target   string `json:"target,omitempty"`
}

// RegistryDocument is the JSON shape loaded from discoverable model registry files.
type RegistryDocument struct {
	Providers []Definition      `json:"providers,omitempty"`
	Models    []ModelDefinition `json:"models,omitempty"`
}

// Registry resolves model names, provider env/config, and provider families.
type Registry struct {
	providers         map[string]Definition
	models            map[string]ModelDefinition
	order             []string
	hasCustomizations bool
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Definition),
		models:    make(map[string]ModelDefinition),
	}
}

func (r *Registry) cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *Registry) AddBuiltins() {
	r.AddDocument(RegistryDocument{
		Providers: []Definition{
			{
				Name:         "anthropic",
				Family:       "anthropic",
				APIKeyEnv:    []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
				BaseURLEnv:   "ANTHROPIC_BASE_URL",
				PingEndpoint: "/v1/messages",
				Match:        []MatchRule{{Prefix: "claude"}},
			},
			{
				Name:           "openai",
				Family:         "openai",
				APIKeyEnv:      []string{"OPENAI_API_KEY"},
				BaseURLEnv:     "OPENAI_BASE_URL",
				DefaultBaseURL: "https://api.openai.com/v1",
				PingEndpoint:   "/v1/models",
				Match:          []MatchRule{{Prefix: "gpt-5"}, {Prefix: "gpt"}, {Prefix: "o1"}, {Prefix: "o3"}, {Prefix: "o4"}},
			},
			{
				Name:           "gemini",
				Family:         "gemini",
				APIKeyEnv:      []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"},
				BaseURLEnv:     "GEMINI_BASE_URL",
				DefaultBaseURL: "https://generativelanguage.googleapis.com",
				PingEndpoint:   "/v1beta/models",
				Match:          []MatchRule{{Prefix: "gemini"}},
			},
			{
				Name:           "ollama",
				Family:         "ollama",
				DefaultBaseURL: "http://localhost:11434",
				PingEndpoint:   "/",
				Match: []MatchRule{
					{Prefix: "ollama/", StripPrefix: true},
					{Suffix: ":cloud"},
					{Suffix: ":local"},
					{Prefix: "qwen"},
					{Prefix: "minimax"},
					{Prefix: "deepseek"},
					{Prefix: "llama"},
					{Prefix: "mistral"},
					{Prefix: "phi"},
					{Prefix: "codellama"},
					{Prefix: "gemma"},
				},
			},
		},
	})
	r.hasCustomizations = false
}

func (r *Registry) AddDocument(doc RegistryDocument) {
	if len(doc.Providers) > 0 || len(doc.Models) > 0 {
		r.hasCustomizations = true
	}
	for _, def := range doc.Providers {
		if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.Family) == "" {
			continue
		}
		def.Name = strings.TrimSpace(def.Name)
		def.Family = strings.TrimSpace(def.Family)
		def.DefaultHeaders = r.cloneMap(def.DefaultHeaders)
		r.providers[def.Name] = def
		r.bumpOrder("provider:" + def.Name)
	}
	for _, mdl := range doc.Models {
		if strings.TrimSpace(mdl.Name) == "" || strings.TrimSpace(mdl.Provider) == "" {
			continue
		}
		mdl.Name = strings.TrimSpace(mdl.Name)
		mdl.Provider = strings.TrimSpace(mdl.Provider)
		if strings.TrimSpace(mdl.Target) == "" {
			mdl.Target = mdl.Name
		}
		r.models[mdl.Name] = mdl
		r.bumpOrder("model:" + mdl.Name)
	}
}

func (r *Registry) bumpOrder(key string) {
	filtered := r.order[:0]
	for _, existing := range r.order {
		if existing != key {
			filtered = append(filtered, existing)
		}
	}
	r.order = append(filtered, key)
}

func (r *Registry) provider(name string) (Definition, bool) {
	def, ok := r.providers[name]
	return def, ok
}

func (r *Registry) model(name string) (ModelDefinition, bool) {
	mdl, ok := r.models[name]
	return mdl, ok
}

func (r *Registry) Resolve(modelName, providerOverride string) (Info, error) {
	if strings.TrimSpace(modelName) == "" {
		return Info{}, fmt.Errorf("no model specified")
	}
	if providerOverride != "" {
		def, ok := r.provider(providerOverride)
		if !ok {
			return Info{}, fmt.Errorf("unknown provider %q", providerOverride)
		}
		if mdl, ok := r.model(modelName); ok && mdl.Provider == providerOverride {
			return newInfo(def, mdl.Target, true), nil
		}
		resolved, appendLatest, _ := applyProviderMatch(def, modelName)
		return newInfo(def, resolved, appendLatest), nil
	}
	if mdl, ok := r.model(modelName); ok {
		def, ok := r.provider(mdl.Provider)
		if !ok {
			return Info{}, fmt.Errorf("model %q references unknown provider %q", mdl.Name, mdl.Provider)
		}
		return newInfo(def, mdl.Target, true), nil
	}

	for i := len(r.order) - 1; i >= 0; i-- {
		key := r.order[i]
		if !strings.HasPrefix(key, "provider:") {
			continue
		}
		name := strings.TrimPrefix(key, "provider:")
		def := r.providers[name]
		if resolved, appendLatest, ok := applyProviderMatch(def, modelName); ok {
			return newInfo(def, resolved, appendLatest), nil
		}
	}

	return Info{}, fmt.Errorf("unknown model %q: cannot determine provider", modelName)
}

func matchesRule(lowerModel, originalModel string, rule MatchRule) bool {
	if rule.Prefix != "" && !strings.HasPrefix(lowerModel, strings.ToLower(rule.Prefix)) {
		return false
	}
	if rule.Suffix != "" && !strings.HasSuffix(lowerModel, strings.ToLower(rule.Suffix)) {
		return false
	}
	return rule.Prefix != "" || rule.Suffix != ""
}

func applyProviderMatch(def Definition, modelName string) (resolved string, appendLatest bool, ok bool) {
	for _, rule := range def.Match {
		if !matchesRule(strings.ToLower(modelName), modelName, rule) {
			continue
		}
		resolved = modelName
		appendLatest = true
		if rule.StripPrefix && rule.Prefix != "" && strings.HasPrefix(strings.ToLower(modelName), strings.ToLower(rule.Prefix)) {
			resolved = modelName[len(rule.Prefix):]
			appendLatest = false
		}
		return resolved, appendLatest, true
	}
	return modelName, true, false
}

func newInfo(def Definition, modelName string, appendLatest bool) Info {
	info := Info{Provider: def.Name, Family: def.Family, Model: modelName}
	if def.Family == "ollama" {
		info.Ollama = true
		if appendLatest && !strings.Contains(modelName, ":") {
			info.Model = modelName + ":latest"
		}
	}
	return info
}

func (r *Registry) APIKey(providerName string) string {
	def, ok := r.provider(providerName)
	if !ok {
		return ""
	}
	for _, envVar := range def.APIKeyEnv {
		if val := os.Getenv(envVar); val != "" {
			return val
		}
	}
	return ""
}

func (r *Registry) BaseURL(providerName string) string {
	def, ok := r.provider(providerName)
	if !ok {
		return ""
	}
	if def.BaseURLEnv != "" {
		if val := os.Getenv(def.BaseURLEnv); val != "" {
			return val
		}
	}
	return def.DefaultBaseURL
}

func (r *Registry) DefaultHeaders(providerName string) map[string]string {
	def, ok := r.provider(providerName)
	if !ok {
		return nil
	}
	return r.cloneMap(def.DefaultHeaders)
}

func (r *Registry) PingEndpoint(providerName string) string {
	def, ok := r.provider(providerName)
	if !ok || def.PingEndpoint == "" {
		return "/"
	}
	return def.PingEndpoint
}

func (r *Registry) ProviderEnvVar(providerName string) string {
	def, ok := r.provider(providerName)
	if !ok || len(def.APIKeyEnv) == 0 {
		return strings.ToUpper(providerName) + "_API_KEY"
	}
	return def.APIKeyEnv[0]
}

func (r *Registry) HasCustomizations() bool {
	return r != nil && r.hasCustomizations
}

func (r *Registry) HasModel(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.models[name]
	return ok
}

func (r *Registry) Providers() []Definition {
	if r == nil {
		return nil
	}
	out := make([]Definition, 0, len(r.providers))
	for _, def := range r.providers {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Models() []ModelDefinition {
	if r == nil {
		return nil
	}
	out := make([]ModelDefinition, 0, len(r.models))
	for _, mdl := range r.models {
		out = append(out, mdl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) RequiresAPIKey(providerName string) bool {
	def, ok := r.provider(providerName)
	if !ok {
		return false
	}
	switch def.Family {
	case "ollama":
		return false
	case "anthropic", "openai":
		return true
	case "gemini":
		if def.Name == "gemini" {
			return false
		}
		return len(def.APIKeyEnv) > 0
	default:
		return len(def.APIKeyEnv) > 0
	}
}

// ListModels fetches available models for the named provider, resolving the
// API key, base URL, and default headers from the registry. The returned
// entries can be presented directly in a TUI list.
func (r *Registry) ListModels(ctx context.Context, providerName string, opts *LLMOptions) ([]ModelEntry, error) {
	def, ok := r.provider(providerName)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", providerName)
	}
	info := Info{
		Provider: def.Name,
		Family:   def.Family,
		Ollama:   def.Family == "ollama",
	}
	apiKey := r.APIKey(providerName)
	baseURL := r.BaseURL(providerName)

	// Merge default headers with any caller-supplied options.
	merged := &LLMOptions{}
	if opts != nil {
		*merged = *opts
	}
	if dh := r.DefaultHeaders(providerName); len(dh) > 0 {
		if merged.ExtraHeaders == nil {
			merged.ExtraHeaders = make(map[string]string, len(dh))
		}
		for k, v := range dh {
			if _, exists := merged.ExtraHeaders[k]; !exists {
				merged.ExtraHeaders[k] = v
			}
		}
	}
	return ListModels(ctx, info, apiKey, baseURL, merged)
}

func LoadRegistryDocuments(dirs ...string) ([]RegistryDocument, error) {
	var docs []RegistryDocument
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading model registry dir %s: %w", dir, err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading model registry file %s: %w", path, err)
			}
			var doc RegistryDocument
			if err := json.Unmarshal(data, &doc); err != nil {
				return nil, fmt.Errorf("parsing model registry file %s: %w", path, err)
			}
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

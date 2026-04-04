package provider

import (
	"testing"
)

func TestRegistryResolveCustomProviderAndModelAlias(t *testing.T) {
	reg := NewRegistry()
	reg.AddBuiltins()
	reg.AddDocument(RegistryDocument{
		Providers: []Definition{{
			Name:           "openrouter",
			Family:         "openai",
			APIKeyEnv:      []string{"OPENROUTER_API_KEY"},
			BaseURLEnv:     "OPENROUTER_BASE_URL",
			DefaultBaseURL: "https://openrouter.ai/api/v1",
			PingEndpoint:   "/models",
			Match: []MatchRule{{
				Prefix:      "openrouter/",
				StripPrefix: true,
			}},
		}},
		Models: []ModelDefinition{{
			Name:     "router-sonnet",
			Provider: "openrouter",
			Target:   "anthropic/claude-sonnet-4",
		}},
	})

	info, err := reg.Resolve("router-sonnet", "")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if info.Provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", info.Provider)
	}
	if info.Family != "openai" {
		t.Fatalf("family = %q, want openai", info.Family)
	}
	if info.Model != "anthropic/claude-sonnet-4" {
		t.Fatalf("model = %q, want anthropic/claude-sonnet-4", info.Model)
	}

	info, err = reg.Resolve("openrouter/meta-llama/llama-4", "")
	if err != nil {
		t.Fatalf("Resolve() prefix error: %v", err)
	}
	if info.Provider != "openrouter" || info.Model != "meta-llama/llama-4" {
		t.Fatalf("unexpected prefix resolution: %+v", info)
	}
}

func TestRegistryResolveExplicitProviderUsesAliasTarget(t *testing.T) {
	reg := NewRegistry()
	reg.AddBuiltins()
	reg.AddDocument(RegistryDocument{
		Providers: []Definition{{
			Name:   "xai",
			Family: "openai",
			Match:  []MatchRule{{Prefix: "xai/", StripPrefix: true}},
		}},
		Models: []ModelDefinition{{Name: "grok-fast", Provider: "xai", Target: "grok-4-fast"}},
	})

	info, err := reg.Resolve("grok-fast", "xai")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if info.Provider != "xai" || info.Model != "grok-4-fast" {
		t.Fatalf("unexpected explicit provider resolution: %+v", info)
	}

	reg.AddDocument(RegistryDocument{Models: []ModelDefinition{{Name: "router-sonnet", Provider: "xai", Target: "xai/anthropic/claude-sonnet-4"}}})
	info, err = reg.Resolve("router-sonnet", "xai")
	if err != nil {
		t.Fatalf("Resolve() alias-with-prefix error: %v", err)
	}
	if info.Model != "xai/anthropic/claude-sonnet-4" {
		t.Fatalf("exact alias target should not be rewritten again, got %+v", info)
	}

	info, err = reg.Resolve("xai/grok-4", "xai")
	if err != nil {
		t.Fatalf("Resolve() prefix error: %v", err)
	}
	if info.Model != "grok-4" {
		t.Fatalf("explicit provider should still apply match rules, got %+v", info)
	}
}

func TestRegistryRequiresAPIKey(t *testing.T) {
	reg := NewRegistry()
	reg.AddBuiltins()
	reg.AddDocument(RegistryDocument{
		Providers: []Definition{{Name: "proxy", Family: "openai"}},
	})
	if !reg.RequiresAPIKey("openai") {
		t.Fatal("expected openai to require an API key")
	}
	if reg.RequiresAPIKey("gemini") {
		t.Fatal("expected built-in gemini to allow keyless auth fallback")
	}
	if !reg.RequiresAPIKey("proxy") {
		t.Fatal("expected openai-family providers to require a key unless the runtime supports keyless auth")
	}

	reg.AddDocument(RegistryDocument{
		Providers: []Definition{{Name: "gemini", Family: "gemini", APIKeyEnv: []string{"CUSTOM_GEMINI_API_KEY"}}},
	})
	if reg.RequiresAPIKey("gemini") {
		t.Fatal("expected built-in gemini name to continue allowing ADC fallback")
	}
}

func TestRegistryLookupEnvAndDefaults(t *testing.T) {
	reg := NewRegistry()
	reg.AddBuiltins()
	reg.AddDocument(RegistryDocument{
		Providers: []Definition{{
			Name:           "openrouter",
			Family:         "openai",
			APIKeyEnv:      []string{"OPENROUTER_API_KEY"},
			BaseURLEnv:     "OPENROUTER_BASE_URL",
			DefaultBaseURL: "https://openrouter.ai/api/v1",
			PingEndpoint:   "/models",
			DefaultHeaders: map[string]string{"HTTP-Referer": "https://example.com"},
		}},
	})

	t.Setenv("OPENROUTER_API_KEY", "test-key")
	if got := reg.APIKey("openrouter"); got != "test-key" {
		t.Fatalf("APIKey() = %q, want test-key", got)
	}
	if got := reg.BaseURL("openrouter"); got != "https://openrouter.ai/api/v1" {
		t.Fatalf("BaseURL() = %q, want default base URL", got)
	}
	t.Setenv("OPENROUTER_BASE_URL", "https://override.example.com/v1")
	if got := reg.BaseURL("openrouter"); got != "https://override.example.com/v1" {
		t.Fatalf("BaseURL() override = %q, want env override", got)
	}
	if got := reg.PingEndpoint("openrouter"); got != "/models" {
		t.Fatalf("PingEndpoint() = %q, want /models", got)
	}
	if got := reg.DefaultHeaders("openrouter"); got["HTTP-Referer"] != "https://example.com" {
		t.Fatalf("DefaultHeaders() = %#v", got)
	}
}

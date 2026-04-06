package extension

import (
	"path/filepath"
	"testing"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/provider"
)

func TestBuildProviderRegistry_LoadOrderAndConfigOverride(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	setTestHome(t, home)

	mustWriteFile(t, filepath.Join(home, ".pi-go", "models", "openrouter.json"), `{
		"providers": [{
			"name": "openrouter",
			"family": "openai",
			"default_base_url": "https://global.example/v1",
			"match": [{"prefix": "openrouter/", "strip_prefix": true}]
		}],
		"models": [{"name": "router-sonnet", "provider": "openrouter", "target": "global-model"}]
	}`)
	mustWriteFile(t, filepath.Join(project, ".pi-go", "models", "openrouter.json"), `{
		"providers": [{
			"name": "openrouter",
			"family": "openai",
			"default_base_url": "https://project.example/v1",
			"match": [{"prefix": "openrouter/", "strip_prefix": true}]
		}],
		"models": [{"name": "router-sonnet", "provider": "openrouter", "target": "project-model"}]
	}`)

	reg, err := BuildProviderRegistry(project, config.Config{
		Providers: []provider.Definition{{
			Name:           "openrouter",
			Family:         "openai",
			DefaultBaseURL: "https://config.example/v1",
			Match:          []provider.MatchRule{{Prefix: "openrouter/", StripPrefix: true}},
		}},
	})
	if err != nil {
		t.Fatalf("BuildProviderRegistry() error: %v", err)
	}

	info, err := reg.Resolve("router-sonnet", "")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if info.Provider != "openrouter" || info.Model != "project-model" {
		t.Fatalf("unexpected alias resolution: %+v", info)
	}
	if got := reg.BaseURL("openrouter"); got != "https://config.example/v1" {
		t.Fatalf("BaseURL() = %q, want config override", got)
	}
}

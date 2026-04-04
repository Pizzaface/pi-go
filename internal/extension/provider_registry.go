package extension

import (
	"fmt"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/provider"
)

// BuildProviderRegistry composes built-in providers with discoverable model
// registry resources and config-local overrides.
func BuildProviderRegistry(workDir string, cfg config.Config) (*provider.Registry, error) {
	resources := DiscoverResourceDirs(workDir)
	docs, err := provider.LoadRegistryDocuments(resources.ModelDirs...)
	if err != nil {
		return nil, fmt.Errorf("loading model registry resources: %w", err)
	}

	reg := provider.NewRegistry()
	reg.AddBuiltins()
	for _, doc := range docs {
		reg.AddDocument(doc)
	}
	reg.AddDocument(provider.RegistryDocument{
		Providers: cfg.Providers,
		Models:    cfg.Models,
	})
	return reg, nil
}

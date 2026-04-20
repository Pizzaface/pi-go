package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

type piTOML struct {
	Name                  string       `toml:"name"`
	Version               string       `toml:"version"`
	Description           string       `toml:"description"`
	Prompt                string       `toml:"prompt"`
	Runtime               string       `toml:"runtime"`
	Command               []string     `toml:"command"`
	Entry                 string       `toml:"entry"`
	RequestedCapabilities []string     `toml:"requested_capabilities"`
	Hooks                 []HookConfig `toml:"hooks"`
}

// HookConfig is a type alias for piapi.HookConfig so that existing loader
// tests can reference the short name without import changes.
type HookConfig = piapi.HookConfig

type packageJSONPi struct {
	Entry                 string   `json:"entry"`
	Description           string   `json:"description"`
	Prompt                string   `json:"prompt"`
	RequestedCapabilities []string `json:"requested_capabilities"`
}

type packageJSON struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Pi      *packageJSONPi `json:"pi"`
}

// parsePiToml reads a pi.toml and returns the extension metadata plus the
// runtime command (empty for compiled-in semantics — not applicable here).
func parsePiToml(path string) (piapi.Metadata, []string, error) {
	var p piTOML
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: parse %s: %w", path, err)
	}
	if p.Name == "" {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: pi.toml at %s missing name", path)
	}
	if p.Version == "" {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: pi.toml at %s missing version", path)
	}

	// Validate hooks
	for i, h := range p.Hooks {
		if h.Event == "" {
			return piapi.Metadata{}, nil, fmt.Errorf("pi.toml [[hooks]][%d]: event is required", i)
		}
		switch h.Event {
		case "startup", "session_start", "before_turn", "after_turn", "shutdown":
		default:
			return piapi.Metadata{}, nil, fmt.Errorf("pi.toml [[hooks]][%d]: unknown event %q", i, h.Event)
		}
		if h.Command == "" {
			return piapi.Metadata{}, nil, fmt.Errorf("pi.toml [[hooks]][%d]: command is required", i)
		}
		if len(h.Tools) == 0 {
			p.Hooks[i].Tools = []string{"*"}
		}
		if h.Timeout == 0 {
			p.Hooks[i].Timeout = 5000
		} else if h.Timeout < 0 || h.Timeout > 60000 {
			return piapi.Metadata{}, nil, fmt.Errorf("pi.toml [[hooks]][%d]: timeout must be 1..60000 ms", i)
		}
	}

	md := piapi.Metadata{
		Name:                  p.Name,
		Version:               p.Version,
		Description:           p.Description,
		Prompt:                p.Prompt,
		RequestedCapabilities: p.RequestedCapabilities,
		Entry:                 p.Entry,
		Command:               p.Command,
		Hooks:                 p.Hooks,
	}
	return md, p.Command, nil
}

// parsePackageJSON reads a package.json and extracts metadata from the "pi" block.
func parsePackageJSON(path string) (piapi.Metadata, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: read %s: %w", path, err)
	}
	var p packageJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: parse %s: %w", path, err)
	}
	if p.Name == "" {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: package.json at %s missing name", path)
	}
	if p.Pi == nil {
		return piapi.Metadata{}, nil, fmt.Errorf("loader: package.json at %s missing pi block", path)
	}
	md := piapi.Metadata{
		Name:                  p.Name,
		Version:               p.Version,
		Description:           p.Pi.Description,
		Prompt:                p.Pi.Prompt,
		RequestedCapabilities: p.Pi.RequestedCapabilities,
		Entry:                 p.Pi.Entry,
	}
	return md, nil, nil
}

// parseMetadataFromFile dispatches by extension name to pi.toml or
// package.json parsing.
func parseMetadataFromFile(path string) (piapi.Metadata, []string, error) {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "pi.toml":
		return parsePiToml(path)
	case "package.json":
		return parsePackageJSON(path)
	default:
		return piapi.Metadata{}, nil, fmt.Errorf("loader: unsupported metadata file %q", path)
	}
}

package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// Mode identifies how a candidate is executed.
type Mode int

const (
	ModeUnknown Mode = iota
	ModeCompiledIn
	ModeHostedGo
	ModeHostedTS
)

func (m Mode) String() string {
	switch m {
	case ModeCompiledIn:
		return "compiled-in"
	case ModeHostedGo:
		return "hosted-go"
	case ModeHostedTS:
		return "hosted-ts"
	default:
		return "unknown"
	}
}

// Candidate is a discovered extension waiting to be registered.
type Candidate struct {
	Mode     Mode
	Path     string
	Dir      string
	Metadata piapi.Metadata
	Command  []string
}

func detectMode(path string) (Mode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ModeUnknown, err
	}
	if !info.IsDir() {
		if strings.HasSuffix(path, ".ts") {
			return ModeHostedTS, nil
		}
		return ModeUnknown, fmt.Errorf("loader: unsupported single-file extension %q", path)
	}
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		var pkg struct {
			Pi *struct {
				Entry string `json:"entry"`
			} `json:"pi"`
		}
		data, err := os.ReadFile(filepath.Join(path, "package.json"))
		if err == nil {
			_ = json.Unmarshal(data, &pkg)
			if pkg.Pi != nil && pkg.Pi.Entry != "" {
				return ModeHostedTS, nil
			}
		}
	}
	if _, err := os.Stat(filepath.Join(path, "pi.toml")); err == nil {
		return ModeHostedGo, nil
	}
	if _, err := os.Stat(filepath.Join(path, "pi.json")); err == nil {
		return ModeHostedGo, nil
	}
	if _, err := os.Stat(filepath.Join(path, "index.ts")); err == nil {
		return ModeHostedTS, nil
	}
	return ModeUnknown, fmt.Errorf("loader: cannot determine mode for %q", path)
}

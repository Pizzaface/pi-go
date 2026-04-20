package host

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed embedded/host.bundle.js
var embeddedHost []byte

var (
	extractOnce sync.Map // version -> *extractResult
)

type extractResult struct {
	once sync.Once
	path string
	err  error
}

// ExtractedHostPath writes the embedded Node host bundle to a per-version
// cache directory (idempotent) and returns the path. Version is a stable
// string tied to the go-pi build so stale copies are harmless.
func ExtractedHostPath(version string) (string, error) {
	v, _ := extractOnce.LoadOrStore(version, &extractResult{})
	res := v.(*extractResult)
	res.once.Do(func() {
		res.path, res.err = extractHostBundle(version)
	})
	return res.path, res.err
}

func extractHostBundle(version string) (string, error) {
	base, err := cacheBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "extension-host", version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("host: mkdir cache dir: %w", err)
	}
	path := filepath.Join(dir, "host.bundle.js")
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	if err := os.WriteFile(path, embeddedHost, 0644); err != nil {
		return "", fmt.Errorf("host: write bundle: %w", err)
	}
	return path, nil
}

func cacheBaseDir() (string, error) {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("LOCALAPPDATA"); p != "" {
			return filepath.Join(p, "go-pi", "cache"), nil
		}
	}
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-pi", "cache"), nil
}

func userHomeDir() (string, error) {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("USERPROFILE"); p != "" {
			return p, nil
		}
	}
	if p := os.Getenv("HOME"); p != "" {
		return p, nil
	}
	return os.UserHomeDir()
}

package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// UserHome returns the user's home directory in a cross-platform way.
func UserHome() (string, error) {
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

// Discover walks the four conventional extension directories and returns
// discovered candidates. Later layers win on name collision.
func Discover(cwd string) ([]Candidate, error) {
	home, err := UserHome()
	if err != nil {
		return nil, fmt.Errorf("loader: resolve user home: %w", err)
	}
	layers := []string{
		filepath.Join(home, ".pi-go", "packages"),
		filepath.Join(home, ".pi-go", "extensions"),
		filepath.Join(cwd, ".pi-go", "packages"),
		filepath.Join(cwd, ".pi-go", "extensions"),
	}
	byName := map[string]Candidate{}
	for _, layer := range layers {
		entries, err := os.ReadDir(layer)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("loader: read %s: %w", layer, err)
		}
		for _, e := range entries {
			path := filepath.Join(layer, e.Name())
			cand, err := candidateFromPath(path)
			if err != nil {
				continue
			}
			byName[cand.Metadata.Name] = cand
		}
	}
	out := make([]Candidate, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Metadata.Name < out[j].Metadata.Name
	})
	return out, nil
}

func candidateFromPath(path string) (Candidate, error) {
	mode, err := detectMode(path)
	if err != nil {
		return Candidate{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Candidate{}, err
	}
	dir := path
	if !info.IsDir() {
		dir = filepath.Dir(path)
	}
	var md piapi.Metadata
	var command []string
	if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".ts") {
		md = piapi.Metadata{
			Name:    strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Version: "0.0.0",
			Entry:   path,
		}
	} else {
		if pkgJSON := filepath.Join(dir, "package.json"); fileExists(pkgJSON) {
			m, cmd, err := parsePackageJSON(pkgJSON)
			if err != nil {
				return Candidate{}, err
			}
			md = m
			command = cmd
		}
		if piTomlPath := filepath.Join(dir, "pi.toml"); fileExists(piTomlPath) {
			m, cmd, err := parsePiToml(piTomlPath)
			if err != nil {
				return Candidate{}, err
			}
			md = m
			if len(cmd) > 0 {
				command = cmd
			}
		}
	}
	if md.Name == "" {
		return Candidate{}, fmt.Errorf("loader: candidate at %s missing name", path)
	}
	return Candidate{
		Mode:     mode,
		Path:     path,
		Dir:      dir,
		Metadata: md,
		Command:  command,
	}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

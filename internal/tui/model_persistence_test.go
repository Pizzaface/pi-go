package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempHome overrides $HOME for the duration of the test and returns the
// temp home directory. The caller does NOT need to restore $HOME — t.Cleanup
// handles it.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origUserProfile)
	})
	return tmp
}

func TestSaveAndLoadHiddenModels(t *testing.T) {
	home := withTempHome(t)

	hidden := map[string]bool{"model-a": true, "model-c": true}
	saveHiddenModels(hidden)

	// Verify the file was created at the right path.
	path := filepath.Join(home, ".go-pi", "hidden_models.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected hidden_models.json to exist: %v", err)
	}

	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		t.Fatalf("invalid JSON in hidden_models.json: %v", err)
	}
	if len(ids) != 2 || ids[0] != "model-a" || ids[1] != "model-c" {
		t.Errorf("expected sorted [model-a, model-c], got %v", ids)
	}

	// Load back.
	loaded := loadHiddenModels()
	if len(loaded) != 2 || !loaded["model-a"] || !loaded["model-c"] {
		t.Errorf("loadHiddenModels returned %v, expected model-a and model-c", loaded)
	}
}

func TestSaveHiddenModelsEmptyRemovesFile(t *testing.T) {
	home := withTempHome(t)

	// Create a file first.
	saveHiddenModels(map[string]bool{"x": true})
	path := filepath.Join(home, ".go-pi", "hidden_models.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist after save: %v", err)
	}

	// Save empty — should remove file.
	saveHiddenModels(nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected hidden_models.json to be removed when empty")
	}
}

func TestLoadHiddenModelsMigratesFromConfigJSON(t *testing.T) {
	home := withTempHome(t)

	// Write legacy format in config.json.
	dir := filepath.Join(home, ".go-pi")
	_ = os.MkdirAll(dir, 0o755)
	legacy := map[string]any{
		"theme":        "tokyo-night",
		"hiddenModels": []string{"legacy-model"},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644)

	// Load should migrate.
	loaded := loadHiddenModels()
	if !loaded["legacy-model"] {
		t.Error("expected legacy-model to be migrated")
	}

	// hidden_models.json should now exist.
	if _, err := os.Stat(filepath.Join(dir, "hidden_models.json")); err != nil {
		t.Error("expected hidden_models.json to be created during migration")
	}

	// config.json should no longer have hiddenModels key.
	cfgData, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var raw map[string]any
	_ = json.Unmarshal(cfgData, &raw)
	if _, ok := raw["hiddenModels"]; ok {
		t.Error("expected hiddenModels key to be removed from config.json after migration")
	}
	// Other keys should be preserved.
	if raw["theme"] != "tokyo-night" {
		t.Errorf("expected theme to be preserved, got %v", raw["theme"])
	}
}

func TestLoadHiddenModelsNoFile(t *testing.T) {
	withTempHome(t)
	loaded := loadHiddenModels()
	if loaded != nil {
		t.Errorf("expected nil from empty home, got %v", loaded)
	}
}

func TestSaveAndLoadLastSelectedModel(t *testing.T) {
	home := withTempHome(t)

	saveLastSelectedModel("claude-sonnet-4", "anthropic")

	// Verify it's in config.json.
	data, err := os.ReadFile(filepath.Join(home, ".go-pi", "config.json"))
	if err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if raw["lastModel"] != "claude-sonnet-4" {
		t.Errorf("expected lastModel=claude-sonnet-4, got %v", raw["lastModel"])
	}
	if raw["lastProvider"] != "anthropic" {
		t.Errorf("expected lastProvider=anthropic, got %v", raw["lastProvider"])
	}

	// Load back.
	model, prov := LoadLastModel()
	if model != "claude-sonnet-4" || prov != "anthropic" {
		t.Errorf("LoadLastModel() = (%q, %q), want (claude-sonnet-4, anthropic)", model, prov)
	}
}

func TestLoadLastModelNoFile(t *testing.T) {
	withTempHome(t)
	model, prov := LoadLastModel()
	if model != "" || prov != "" {
		t.Errorf("expected empty strings from empty home, got (%q, %q)", model, prov)
	}
}

func TestSaveLastSelectedModelPreservesExistingConfig(t *testing.T) {
	home := withTempHome(t)

	// Write existing config with a theme.
	dir := filepath.Join(home, ".go-pi")
	_ = os.MkdirAll(dir, 0o755)
	existing := map[string]any{"theme": "gruvbox"}
	data, _ := json.MarshalIndent(existing, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644)

	// Save last model.
	saveLastSelectedModel("gpt-4o", "openai")

	// Verify both keys exist.
	cfgData, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var raw map[string]any
	_ = json.Unmarshal(cfgData, &raw)
	if raw["theme"] != "gruvbox" {
		t.Errorf("expected theme=gruvbox to be preserved, got %v", raw["theme"])
	}
	if raw["lastModel"] != "gpt-4o" {
		t.Errorf("expected lastModel=gpt-4o, got %v", raw["lastModel"])
	}
}

func TestLoadCollapsedToolsDefaultsToTrue(t *testing.T) {
	withTempHome(t)
	if collapsed := loadCollapsedTools(); !collapsed {
		t.Fatal("expected collapsed tools to default to true")
	}
}

func TestSaveAndLoadCollapsedTools(t *testing.T) {
	home := withTempHome(t)
	saveCollapsedTools(false)

	data, err := os.ReadFile(filepath.Join(home, ".go-pi", "config.json"))
	if err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON in config.json: %v", err)
	}
	if raw["collapsedTools"] != false {
		t.Fatalf("expected collapsedTools=false, got %v", raw["collapsedTools"])
	}
	if collapsed := loadCollapsedTools(); collapsed {
		t.Fatal("expected persisted collapsedTools=false to be loaded")
	}
}

func TestSaveCollapsedToolsPreservesExistingConfig(t *testing.T) {
	home := withTempHome(t)
	dir := filepath.Join(home, ".go-pi")
	_ = os.MkdirAll(dir, 0o755)
	existing := map[string]any{"theme": "tokyo-night", "lastModel": "claude-sonnet-4"}
	data, _ := json.MarshalIndent(existing, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644)

	saveCollapsedTools(true)

	cfgData, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(cfgData, &raw); err != nil {
		t.Fatalf("invalid JSON in config.json: %v", err)
	}
	if raw["theme"] != "tokyo-night" {
		t.Fatalf("expected theme to be preserved, got %v", raw["theme"])
	}
	if raw["lastModel"] != "claude-sonnet-4" {
		t.Fatalf("expected lastModel to be preserved, got %v", raw["lastModel"])
	}
	if raw["collapsedTools"] != true {
		t.Fatalf("expected collapsedTools=true, got %v", raw["collapsedTools"])
	}
}

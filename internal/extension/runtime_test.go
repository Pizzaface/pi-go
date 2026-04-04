package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/tools"
)

func TestLoadManifests_ProjectOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	writeManifest(t, filepath.Join(globalDir, "demo"), `{
		"name": "demo",
		"description": "global",
		"prompt": "GLOBAL",
		"tui": {"commands": [{"name": "demo", "description": "global cmd", "prompt": "global {{args}}"}]}
	}`)
	writeManifest(t, filepath.Join(projectDir, "demo"), `{
		"name": "demo",
		"description": "project",
		"prompt": "PROJECT",
		"tui": {"commands": [{"name": "demo", "description": "project cmd", "prompt": "project {{args}}"}]}
	}`)

	manifests, err := LoadManifests(globalDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	if manifests[0].Description != "project" {
		t.Fatalf("expected project manifest to override global, got %q", manifests[0].Description)
	}
	if len(manifests[0].TUI.Commands) != 1 || manifests[0].TUI.Commands[0].Description != "project cmd" {
		t.Fatalf("expected project TUI command override, got %+v", manifests[0].TUI.Commands)
	}
}

func TestBuildRuntime_LoadsManifestContributions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	extDir := filepath.Join(root, ".pi-go", "extensions", "demo")
	writeManifest(t, extDir, `{
		"name": "demo",
		"description": "demo extension",
		"prompt": "Use the demo extension.",
		"hooks": [
			{"event": "before_tool", "command": "echo before", "tools": ["read"]},
			{"event": "after_tool", "command": "echo after", "tools": ["write"]}
		],
		"mcp_servers": [
			{"name": "echo", "command": "echo", "args": ["hello"]}
		],
		"skills_dir": "skills",
		"tui": {
			"commands": [
				{"name": "demo", "description": "Run demo flow", "prompt": "demo {{args}}"}
			]
		}
	}`)
	writeSkill(t, filepath.Join(extDir, "skills", "demo-skill"), `---
name: demo-skill
description: Demo skill
---
Demo skill body.
`)
	writePromptTemplate(t, filepath.Join(root, ".pi-go", "prompts", "review.md"), `---
name: review
description: Review the current branch
---
Review the current branch. Extra context: {{args}}
`)
	mustMkdirAllRuntime(t, filepath.Join(root, ".pi-go", "themes"))

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{
		Config:          config.Config{},
		WorkDir:         root,
		Sandbox:         sandbox,
		BaseInstruction: "Base instruction.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(rt.Extensions) != 1 {
		t.Fatalf("expected 1 loaded extension, got %d", len(rt.Extensions))
	}
	if len(rt.Toolsets) != 1 {
		t.Fatalf("expected 1 MCP toolset, got %d", len(rt.Toolsets))
	}
	if len(rt.BeforeToolCallbacks) != 1 {
		t.Fatalf("expected 1 before callback, got %d", len(rt.BeforeToolCallbacks))
	}
	if len(rt.AfterToolCallbacks) != 1 {
		t.Fatalf("expected 1 after callback, got %d", len(rt.AfterToolCallbacks))
	}
	if len(rt.Skills) != 1 || rt.Skills[0].Name != "demo-skill" {
		t.Fatalf("expected demo skill to load, got %+v", rt.Skills)
	}
	if len(rt.PromptTemplates) != 1 || rt.PromptTemplates[0].Name != "review" {
		t.Fatalf("expected review prompt template, got %+v", rt.PromptTemplates)
	}
	if len(rt.SlashCommands) != 2 {
		t.Fatalf("expected demo + prompt-template slash commands, got %+v", rt.SlashCommands)
	}
	if rt.ThemeDirs == nil || len(rt.ThemeDirs) == 0 {
		t.Fatalf("expected discovered theme dirs, got %+v", rt.ThemeDirs)
	}
	if !strings.Contains(rt.Instruction, "Use the demo extension.") {
		t.Fatalf("expected instruction to include manifest prompt, got %q", rt.Instruction)
	}
	if !strings.Contains(rt.Instruction, "/demo-skill") {
		t.Fatalf("expected instruction to include available skills, got %q", rt.Instruction)
	}
}

func TestRuntimeRunLifecycleHooks(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "started.txt")
	extDir := filepath.Join(root, ".pi-go", "extensions", "demo")
	writeManifest(t, extDir, `{
		"name": "demo",
		"lifecycle": [
			{"event": "startup", "command": "echo started > `+marker+`"}
		]
	}`)

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	rt, err := BuildRuntime(context.Background(), RuntimeConfig{
		Config:          config.Config{},
		WorkDir:         root,
		Sandbox:         sandbox,
		BaseInstruction: "Base instruction.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := rt.RunLifecycleHooks(context.Background(), LifecycleEventStartup, map[string]any{"phase": "test"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "started" {
		t.Fatalf("unexpected lifecycle hook output: %q", string(data))
	}
}

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSkill(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePromptTemplate(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdirAllRuntime(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

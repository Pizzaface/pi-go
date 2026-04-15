package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverResourceDirs_IncludesPackagesAndProjectOverrides(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	setTestHome(t, home)

	globalPkg := filepath.Join(home, ".pi-go", "packages", "alpha")
	projectPkg := filepath.Join(project, ".pi-go", "packages", "beta")
	mustMkdirAll(t, filepath.Join(globalPkg, "extensions"))
	mustMkdirAll(t, filepath.Join(globalPkg, "skills"))
	mustMkdirAll(t, filepath.Join(globalPkg, "prompts"))
	mustMkdirAll(t, filepath.Join(globalPkg, "themes"))
	mustMkdirAll(t, filepath.Join(globalPkg, "models"))
	mustMkdirAll(t, filepath.Join(projectPkg, "extensions"))
	mustMkdirAll(t, filepath.Join(projectPkg, "skills"))
	mustMkdirAll(t, filepath.Join(projectPkg, "prompts"))
	mustMkdirAll(t, filepath.Join(projectPkg, "themes"))
	mustMkdirAll(t, filepath.Join(projectPkg, "models"))

	dirs := DiscoverResourceDirs(project)

	assertContainsPath(t, dirs.ExtensionDirs, filepath.Join(home, ".pi-go", "packages", "alpha", "extensions"))
	assertContainsPath(t, dirs.ExtensionDirs, filepath.Join(project, ".pi-go", "packages", "beta", "extensions"))
	assertContainsPath(t, dirs.SkillDirs, filepath.Join(home, ".agents", "skills"))
	assertContainsPath(t, dirs.SkillDirs, filepath.Join(project, ".agents", "skills"))
	assertContainsPath(t, dirs.SkillDirs, filepath.Join(project, ".claude", "skills"))
	assertContainsPath(t, dirs.PromptDirs, filepath.Join(project, ".pi-go", "prompts"))
	assertContainsPath(t, dirs.ThemeDirs, filepath.Join(home, ".pi-go", "themes"))
	assertContainsPath(t, dirs.ModelDirs, filepath.Join(project, ".pi-go", "models"))

	globalLoose := indexOf(dirs.ExtensionDirs, filepath.Join(home, ".pi-go", "extensions"))
	projectLoose := indexOf(dirs.ExtensionDirs, filepath.Join(project, ".pi-go", "extensions"))
	if globalLoose == -1 || projectLoose == -1 || globalLoose >= projectLoose {
		t.Fatalf("expected project extension dir after global for override order, got %v", dirs.ExtensionDirs)
	}
}

func TestDiscoverResourceDirs_UsesProjectRootFromNestedDir(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	nested := filepath.Join(project, "sub", "dir")
	setTestHome(t, home)
	mustMkdirAll(t, nested)
	mustMkdirAll(t, filepath.Join(project, ".pi-go", "models"))

	dirs := DiscoverResourceDirs(nested)
	assertContainsPath(t, dirs.ModelDirs, filepath.Join(project, ".pi-go", "models"))
}

func TestLoadPromptTemplates_ProjectOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	mustWriteFile(t, filepath.Join(globalDir, "review.md"), `---
name: review
description: global review
---
Global prompt {{args}}
`)
	mustWriteFile(t, filepath.Join(projectDir, "review.md"), `---
name: review
description: project review
---
Project prompt {{args}}
`)

	templates, err := LoadPromptTemplates(globalDir, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
	if templates[0].Description != "project review" {
		t.Fatalf("expected project override, got %+v", templates[0])
	}
	if got := templates[0].Render([]string{"now"}); got != "Project prompt now" {
		t.Fatalf("unexpected render output %q", got)
	}
	if cmd := templates[0].SlashCommand(); cmd.Name != "review" || cmd.Description != "project review" {
		t.Fatalf("unexpected slash command %+v", cmd)
	}
}

func TestLoadPromptTemplates_DefaultNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "triage.md"), "Triage {{args}}")

	templates, err := LoadPromptTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
	if templates[0].Name != "triage" {
		t.Fatalf("expected filename-derived name, got %+v", templates[0])
	}
}

func TestLoadPromptTemplates_AllowsHorizontalRuleWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "plain.md"), "hello\n---\nworld")

	templates, err := LoadPromptTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := templates[0].Prompt; got != "hello\n---\nworld" {
		t.Fatalf("unexpected prompt body %q", got)
	}
}

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("PI_GO_HOME", "")
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContainsPath(t *testing.T, values []string, want string) {
	t.Helper()
	if indexOf(values, want) == -1 {
		t.Fatalf("expected %q in %v", want, values)
	}
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

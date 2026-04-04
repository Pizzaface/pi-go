package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverResourceDirs_IncludesPackagesAndProjectOverrides(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	globalPkg := filepath.Join(home, ".pi-go", "packages", "alpha")
	projectPkg := filepath.Join(project, ".pi-go", "packages", "beta")
	mustMkdirAll(t, filepath.Join(globalPkg, "extensions"))
	mustMkdirAll(t, filepath.Join(globalPkg, "skills"))
	mustMkdirAll(t, filepath.Join(globalPkg, "prompts"))
	mustMkdirAll(t, filepath.Join(globalPkg, "themes"))
	mustMkdirAll(t, filepath.Join(projectPkg, "extensions"))
	mustMkdirAll(t, filepath.Join(projectPkg, "skills"))
	mustMkdirAll(t, filepath.Join(projectPkg, "prompts"))
	mustMkdirAll(t, filepath.Join(projectPkg, "themes"))

	dirs := DiscoverResourceDirs(project)

	assertContainsPath(t, dirs.ExtensionDirs, filepath.Join(home, ".pi-go", "packages", "alpha", "extensions"))
	assertContainsPath(t, dirs.ExtensionDirs, filepath.Join(project, ".pi-go", "packages", "beta", "extensions"))
	assertContainsPath(t, dirs.SkillDirs, filepath.Join(project, ".claude", "skills"))
	assertContainsPath(t, dirs.PromptDirs, filepath.Join(project, ".pi-go", "prompts"))
	assertContainsPath(t, dirs.ThemeDirs, filepath.Join(home, ".pi-go", "themes"))

	globalLoose := indexOf(dirs.ExtensionDirs, filepath.Join(home, ".pi-go", "extensions"))
	projectLoose := indexOf(dirs.ExtensionDirs, filepath.Join(project, ".pi-go", "extensions"))
	if globalLoose == -1 || projectLoose == -1 || globalLoose >= projectLoose {
		t.Fatalf("expected project extension dir after global for override order, got %v", dirs.ExtensionDirs)
	}
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

func TestPackageLifecycle_LocalInstallUpdateListRemove(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	source := t.TempDir()
	mustWriteFile(t, filepath.Join(source, "prompts", "triage.md"), "First version")
	mustWriteFile(t, filepath.Join(source, "themes", "custom.json"), `{"name":"custom","displayName":"Custom","themeType":"dark","colors":{"text":"#fff","base":"#000","primary":"#111","tool":"#222","success":"#333","error":"#444","secondary":"#555","info":"#666","warning":"#777","diffAdded":"#888","diffRemoved":"#999","diffAddedText":"#aaa","diffRemovedText":"#bbb"}}`)

	installed, err := InstallPackage(project, PackageScopeProject, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name == "" || installed.Scope != PackageScopeProject {
		t.Fatalf("unexpected install record %+v", installed)
	}
	if _, err := os.Stat(filepath.Join(installed.Dir, "prompts", "triage.md")); err != nil {
		t.Fatalf("expected prompt copied into package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed.Dir, packageMetaFile)); err != nil {
		t.Fatalf("expected package metadata: %v", err)
	}

	pkgs, err := ListInstalledPackages(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != installed.Name {
		t.Fatalf("unexpected package listing %+v", pkgs)
	}

	mustWriteFile(t, filepath.Join(source, "prompts", "triage.md"), "Second version")
	updated, err := UpdatePackage(project, PackageScopeProject, installed.Name)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(updated.Dir, "prompts", "triage.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "Second version" {
		t.Fatalf("expected updated package contents, got %q", string(data))
	}

	if err := RemovePackage(project, PackageScopeProject, installed.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(updated.Dir); !os.IsNotExist(err) {
		t.Fatalf("expected package dir removed, got err=%v", err)
	}
}

func TestPackageLifecycle_InvalidNamesRejected(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	source := t.TempDir()

	if _, err := InstallPackage(project, PackageScopeProject, source, "../../escape"); err == nil {
		t.Fatal("expected invalid install name to fail")
	}
	if err := RemovePackage(project, PackageScopeProject, "../../escape"); err == nil {
		t.Fatal("expected invalid remove name to fail")
	}
}

func TestPackageUpdateFailurePreservesExistingInstall(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	source := t.TempDir()
	mustWriteFile(t, filepath.Join(source, "prompts", "triage.md"), "stable")
	installed, err := InstallPackage(project, PackageScopeProject, source, "stable-pack")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdatePackage(project, PackageScopeProject, installed.Name); err == nil {
		t.Fatal("expected update from missing source to fail")
	}
	data, err := os.ReadFile(filepath.Join(installed.Dir, "prompts", "triage.md"))
	if err != nil {
		t.Fatalf("expected existing package to remain after failed update: %v", err)
	}
	if strings.TrimSpace(string(data)) != "stable" {
		t.Fatalf("expected existing contents to remain, got %q", string(data))
	}
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

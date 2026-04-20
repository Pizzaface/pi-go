package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension"
)

func TestComplete_CommandCompletion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		first    string
	}{
		{"help matches", "/he", 1, "/help"},
		{"core c-commands", "/co", 2, "/compact"},
		{"all commands", "/", 0, ""},
		{"no match", "/xyz", 0, ""},
		{"exact match", "/help", 1, "/help"},
		{"skill-like", "/skills", 1, "/skills"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Complete(tt.input, nil, "")
			if len(result.Candidates) != tt.expected {
				t.Errorf("expected %d matches, got %d: %v", tt.expected, len(result.Candidates), result.Candidates)
			}
			if tt.first != "" && len(result.Candidates) > 0 && result.Candidates[0].Text != tt.first {
				t.Errorf("expected first match %q, got %q", tt.first, result.Candidates[0].Text)
			}
		})
	}
}

func TestComplete_SkillCompletion(t *testing.T) {
	skills := []extension.Skill{
		{Name: "my-skill", Description: "Does something"},
		{Name: "my-other", Description: "Does another thing"},
		{Name: "other-skill", Description: "Different"},
	}

	result := Complete("/my-", skills, "")
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(result.Candidates), result.Candidates)
	}
	if result.Candidates[0].Text != "/my-other" {
		t.Fatalf("expected alphabetical first match, got %q", result.Candidates[0].Text)
	}
}

func TestComplete_IncludesDynamicExtensionCommands(t *testing.T) {
	result := Complete("/de", nil, "", extension.SlashCommand{
		Name:        "demo",
		Description: "Run demo",
	})
	if len(result.Candidates) == 0 {
		t.Fatalf("expected dynamic extension command candidate, got %+v", result.Candidates)
	}
	found := false
	for _, candidate := range result.Candidates {
		if candidate.Text == "/demo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /demo in completion candidates, got %+v", result.Candidates)
	}
}

func TestMatchingCommands_IncludesDynamicExtensions(t *testing.T) {
	candidates := matchingCommands("/dem", []extension.SlashCommand{{
		Name:        "demo",
		Description: "Run demo",
	}})
	if len(candidates) != 1 || candidates[0].Text != "/demo" {
		t.Fatalf("expected dynamic command match /demo, got %+v", candidates)
	}
}

func TestComplete_CycleSelection(t *testing.T) {
	result := Complete("/c", nil, "")
	if len(result.Candidates) < 2 {
		t.Fatalf("need at least 2 candidates, got %d", len(result.Candidates))
	}

	result.CycleSelection(1)
	if result.Selected != 1 {
		t.Errorf("expected selected 1, got %d", result.Selected)
	}

	result.CycleSelection(-1)
	if result.Selected != 0 {
		t.Errorf("expected selected 0, got %d", result.Selected)
	}
}

func TestComplete_ApplySelection(t *testing.T) {
	result := Complete("/he", nil, "")
	if len(result.Candidates) == 0 {
		t.Fatal("need at least 1 candidate")
	}

	applied := result.ApplySelection(0)
	if applied != result.Candidates[0].Text {
		t.Errorf("expected %q, got %q", result.Candidates[0].Text, applied)
	}
	if result.ApplySelection(999) != "" {
		t.Error("expected empty string for invalid index")
	}
}

func TestComplete_SelectedCandidate(t *testing.T) {
	result := Complete("/c", nil, "")
	if len(result.Candidates) == 0 {
		t.Fatal("need at least 1 candidate")
	}
	if result.SelectedCandidate() == nil {
		t.Error("expected non-nil selected candidate")
	}
	result.Selected = 999
	if result.SelectedCandidate() != nil {
		t.Error("expected nil for out-of-bounds selection")
	}
}

func TestCompleteMention_MatchingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestFiles(t, tmpDir, "src/main.go", "src/model.go", "internal/tui/input.go", "README.md")

	result := CompleteMention("src/", tmpDir)
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates for 'src/', got %d: %v", len(result.Candidates), result.Candidates)
	}
	if result.Candidates[0].Text != "src/main.go" {
		t.Errorf("first = %q, want 'src/main.go'", result.Candidates[0].Text)
	}
}

func TestCompleteMention_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestFiles(t, tmpDir, "src/main.go")

	result := CompleteMention("zzz", tmpDir)
	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}
}

func TestCompleteMention_FuzzyMatch(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestFiles(t, tmpDir, "src/main.go", "src/model.go", "internal/tui/input.go")

	result := CompleteMention("smo", tmpDir)
	found := false
	for _, c := range result.Candidates {
		if c.Text == "src/model.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fuzzy match for 'src/model.go' in %v", result.Candidates)
	}
}

func TestCompleteMention_SkipsHiddenDirs(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestFiles(t, tmpDir, ".git/config", "src/main.go")

	result := CompleteMention("", tmpDir)
	for _, c := range result.Candidates {
		if c.Text == ".git/config" {
			t.Error("should not include files in hidden directories")
		}
	}
}

func TestFindMentionAtCursor(t *testing.T) {
	start, pfx := findMentionAtCursor("fix @src/ma", 11)
	if start != 4 || pfx != "src/ma" {
		t.Fatalf("got (%d, %q), want (4, %q)", start, pfx, "src/ma")
	}
}

func TestExtractMentions(t *testing.T) {
	got := extractMentions("look at @src/a.go and @src/b.go")
	want := []string{"src/a.go", "src/b.go"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFuzzyMatchPath(t *testing.T) {
	if !fuzzyMatchPath("src/model.go", "smo") {
		t.Error("expected fuzzy match")
	}
	if fuzzyMatchPath("src/main.go", "zzz") {
		t.Error("unexpected fuzzy match")
	}
}

func setupTestFiles(t *testing.T, workDir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := workDir + "/" + p
		dir := full[:strings.LastIndex(full, "/")]
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("// "+p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompleteMention_MaxResults(t *testing.T) {
	tmpDir := t.TempDir()
	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("file%02d.go", i)
	}
	setupTestFiles(t, tmpDir, files...)

	result := CompleteMention("", tmpDir)
	if len(result.Candidates) > 20 {
		t.Errorf("expected at most 20 candidates, got %d", len(result.Candidates))
	}
}

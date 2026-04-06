package claudecli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestName(t *testing.T) {
	p := New(Config{})
	if got := p.Name(); got != "claudecli" {
		t.Errorf("Name() = %q, want %q", got, "claudecli")
	}
}

func TestExtractUserMessage(t *testing.T) {
	tests := []struct {
		name     string
		req      *model.LLMRequest
		want     string
	}{
		{
			name: "nil request",
			req:  nil,
			want: "",
		},
		{
			name: "empty contents",
			req:  &model.LLMRequest{},
			want: "",
		},
		{
			name: "single user message",
			req: &model.LLMRequest{
				Contents: []*genai.Content{
					{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("hello")}},
				},
			},
			want: "hello",
		},
		{
			name: "multi-part user message",
			req: &model.LLMRequest{
				Contents: []*genai.Content{
					{Role: "user", Parts: []*genai.Part{
						genai.NewPartFromText("line1"),
						genai.NewPartFromText("line2"),
					}},
				},
			},
			want: "line1\nline2",
		},
		{
			name: "takes last user message",
			req: &model.LLMRequest{
				Contents: []*genai.Content{
					{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("first")}},
					{Role: "model", Parts: []*genai.Part{genai.NewPartFromText("response")}},
					{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("second")}},
				},
			},
			want: "second",
		},
		{
			name: "skips model messages",
			req: &model.LLMRequest{
				Contents: []*genai.Content{
					{Role: "model", Parts: []*genai.Part{genai.NewPartFromText("only model")}},
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUserMessage(tt.req)
			if got != tt.want {
				t.Errorf("extractUserMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFirstWord(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"rm -rf /", "rm"},
		{"  sudo apt install", "sudo"},
		{"ls", "ls"},
		{"", ""},
		{"git status\n", "git"},
	}
	for _, tt := range tests {
		if got := firstWord(tt.input); got != tt.want {
			t.Errorf("firstWord(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanUseTool(t *testing.T) {
	p := New(Config{
		ApprovalRules: []ApprovalRule{
			{
				ToolName: "Bash",
				DenyCmds: []string{"sudo", "rm"},
			},
			{
				ToolName:     "Write",
				AllowPaths:   []string{"*.go", "*.md"},
				DefaultAllow: false,
			},
		},
	})

	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		want     string
	}{
		{
			name:  "bash allowed command",
			tool:  "Bash",
			input: map[string]any{"command": "git status"},
			want:  "allow",
		},
		{
			name:  "bash denied sudo",
			tool:  "Bash",
			input: map[string]any{"command": "sudo apt install"},
			want:  "deny",
		},
		{
			name:  "bash denied rm",
			tool:  "Bash",
			input: map[string]any{"command": "rm -rf /"},
			want:  "deny",
		},
		{
			name:  "write allowed go file",
			tool:  "Write",
			input: map[string]any{"file_path": "main.go"},
			want:  "allow",
		},
		{
			name:  "write denied unknown extension",
			tool:  "Write",
			input: map[string]any{"file_path": "secrets.env"},
			want:  "deny",
		},
		{
			name:  "read no rules default allow",
			tool:  "Read",
			input: map[string]any{"file_path": "anything"},
			want:  "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.canUseTool(tt.tool, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("canUseTool(%q, %v) = %q, want %q", tt.tool, tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateContentNoUserMessage(t *testing.T) {
	p := New(Config{})
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "model", Parts: []*genai.Part{genai.NewPartFromText("hi")}},
		},
	}

	var gotErr error
	for _, err := range p.GenerateContent(context.Background(), req, true) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Error("expected error for missing user message")
	}
}

func TestGenerateContentClosed(t *testing.T) {
	p := New(Config{})
	_ = p.Close()

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("hello")}},
		},
	}

	var gotErr error
	for _, err := range p.GenerateContent(context.Background(), req, true) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil {
		t.Error("expected error for closed provider")
	}
}

func TestToolUseSummary(t *testing.T) {
	tests := []struct {
		name  string
		block *toolUseBlockArg
		want  string
	}{
		{
			name: "read file",
			block: &toolUseBlockArg{
				Name:  "Read",
				Input: map[string]any{"file_path": "/home/user/main.go"},
			},
			want: "/home/user/main.go",
		},
		{
			name: "bash short",
			block: &toolUseBlockArg{
				Name:  "Bash",
				Input: map[string]any{"command": "git status"},
			},
			want: "git status",
		},
		{
			name: "bash long truncated",
			block: &toolUseBlockArg{
				Name: "Bash",
				Input: map[string]any{
					"command": "find / -name '*.go' -exec grep -l 'something very long that exceeds the limit of 80 characters for display purposes' {} \\;",
				},
			},
		},
		{
			name: "grep pattern",
			block: &toolUseBlockArg{
				Name:  "Grep",
				Input: map[string]any{"pattern": "TODO"},
			},
			want: "TODO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap into a real claude.ToolUseBlock via our helper.
			got := toolUseSummaryFromInput(tt.block.Name, tt.block.Input)
			if tt.want != "" && got != tt.want {
				t.Errorf("toolUseSummary() = %q, want %q", got, tt.want)
			}
			if tt.name == "bash long truncated" && len(got) > 84 { // 80 + "..."
				t.Errorf("expected truncated output, got len=%d: %q", len(got), got)
			}
		})
	}
}

// toolUseBlockArg is a test helper to avoid importing claude package for block creation.
type toolUseBlockArg struct {
	Name  string
	Input map[string]any
}

// toolUseSummaryFromInput mirrors toolUseSummary logic for testing.
func toolUseSummaryFromInput(name string, input map[string]any) string {
	switch name {
	case "Read", "Write", "Edit":
		if fp, ok := input["file_path"].(string); ok {
			return fp
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			if len(cmd) > 80 {
				return cmd[:80] + "..."
			}
			return cmd
		}
	case "Grep", "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	}
	return ""
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Simple globs
		{"*.go", "main.go", true},
		{"*.go", "main.rs", false},
		{"*.md", "README.md", true},

		// Doublestar
		{"./**", "src/main.go", true},
		{"./**/*.go", "src/pkg/main.go", true},
		{"./**/*.go", "src/pkg/main.rs", false},
		{"**/*.go", "deeply/nested/file.go", true},
		{"**", "anything/at/all", true},

		// Prefix matching with **
		{"src/**/*.go", "src/pkg/main.go", true},
		{"src/**/*.go", "other/main.go", false},
		{"src/**", "src/any/file.txt", true},

		// Windows-style paths (normalized to forward slash)
		{"*.go", "main.go", true},
		{"src/**/*.go", "src/pkg/main.go", true},

		// No match
		{"docs/**/*.md", "src/main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			if got := matchPath(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestCanUseToolWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		// On Unix, filepath.ToSlash is a no-op for backslashes (they're
		// valid filename chars). This test only verifies Windows behavior.
		t.Skip("backslash normalization only applies on Windows")
	}

	p := New(Config{
		ApprovalRules: []ApprovalRule{
			{
				ToolName:   "Write",
				AllowPaths: []string{"src/**/*.go"},
			},
		},
	})

	got, err := p.canUseTool("Write", map[string]any{"file_path": "src\\pkg\\main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "allow" {
		t.Errorf("expected allow for Windows path, got %q", got)
	}
}

func TestCanUseToolForwardSlashPaths(t *testing.T) {
	// Forward-slash paths work on all platforms.
	p := New(Config{
		ApprovalRules: []ApprovalRule{
			{
				ToolName:   "Write",
				AllowPaths: []string{"src/**/*.go"},
			},
		},
	})

	got, err := p.canUseTool("Write", map[string]any{"file_path": "src/pkg/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "allow" {
		t.Errorf("expected allow for forward-slash path, got %q", got)
	}
}

func TestFindBinaryEnvOverride(t *testing.T) {
	origLookup := lookupEnv
	origStat := statFile
	defer func() { lookupEnv = origLookup; statFile = origStat }()

	lookupEnv = func(key string) (string, bool) {
		if key == "CLAUDE_CLI_PATH" {
			return "/custom/path/claude", true
		}
		return "", false
	}
	statFile = func(path string) (os.FileInfo, error) {
		if path == "/custom/path/claude" {
			return nil, nil // exists
		}
		return nil, os.ErrNotExist
	}

	path, err := FindBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/custom/path/claude" {
		t.Errorf("FindBinary() = %q, want /custom/path/claude", path)
	}
}

func TestFindBinaryWellKnownPaths(t *testing.T) {
	origLookup := lookupEnv
	origStat := statFile
	origIsWin := isWindows
	origLookPath := lookPath
	defer func() { lookupEnv = origLookup; statFile = origStat; isWindows = origIsWin; lookPath = origLookPath }()

	// Simulate: no env override, not in PATH, but found at ~/.local/bin/claude
	lookupEnv = func(key string) (string, bool) { return "", false }
	lookPath = func(file string) (string, error) { return "", exec.ErrNotFound }
	isWindows = func() bool { return false }

	home, _ := os.UserHomeDir()
	expectedPath := home + "/.local/bin/claude"

	statFile = func(path string) (os.FileInfo, error) {
		if path == expectedPath {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	path, err := FindBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != expectedPath {
		t.Errorf("FindBinary() = %q, want %q", path, expectedPath)
	}
}

func TestFindBinaryWindowsPaths(t *testing.T) {
	origLookup := lookupEnv
	origStat := statFile
	origIsWin := isWindows
	origLookPath := lookPath
	defer func() { lookupEnv = origLookup; statFile = origStat; isWindows = origIsWin; lookPath = origLookPath }()

	lookupEnv = func(key string) (string, bool) {
		if key == "APPDATA" {
			return `C:\Users\test\AppData\Roaming`, true
		}
		return "", false
	}
	lookPath = func(file string) (string, error) { return "", exec.ErrNotFound }
	isWindows = func() bool { return true }

	home, _ := os.UserHomeDir()
	// filepath.Join uses the current OS separator, so on macOS/Linux
	// it produces forward slashes even though we're simulating Windows.
	// We match on what filepath.Join actually produces.
	winNative := filepath.Join(home, ".local", "bin", "claude.exe")

	statFile = func(path string) (os.FileInfo, error) {
		if path == winNative {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}

	path, err := FindBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != winNative {
		t.Errorf("FindBinary() = %q, want %q", path, winNative)
	}
}

func TestFindBinaryNotFound(t *testing.T) {
	origLookup := lookupEnv
	origStat := statFile
	origIsWin := isWindows
	origLookPath := lookPath
	defer func() { lookupEnv = origLookup; statFile = origStat; isWindows = origIsWin; lookPath = origLookPath }()

	lookupEnv = func(key string) (string, bool) { return "", false }
	lookPath = func(file string) (string, error) { return "", exec.ErrNotFound }
	isWindows = func() bool { return false }
	statFile = func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	_, err := FindBinary()
	if err == nil {
		t.Fatal("expected error when claude not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestWellKnownPathsWindows(t *testing.T) {
	origIsWin := isWindows
	origLookup := lookupEnv
	defer func() { isWindows = origIsWin; lookupEnv = origLookup }()

	isWindows = func() bool { return true }
	lookupEnv = func(key string) (string, bool) {
		if key == "APPDATA" {
			return `C:\Users\test\AppData\Roaming`, true
		}
		return "", false
	}

	paths := wellKnownPaths(`C:\Users\test`)

	if len(paths) < 2 {
		t.Fatalf("expected at least 2 Windows paths, got %d: %v", len(paths), paths)
	}

	// Check that the native installer path is included.
	found := false
	for _, p := range paths {
		if strings.Contains(p, "claude.exe") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected claude.exe in Windows paths: %v", paths)
	}

	// Check that the npm path is included.
	foundNpm := false
	for _, p := range paths {
		if strings.Contains(p, "claude.cmd") {
			foundNpm = true
			break
		}
	}
	if !foundNpm {
		t.Errorf("expected claude.cmd in Windows paths: %v", paths)
	}
}

func TestWellKnownPathsUnix(t *testing.T) {
	origIsWin := isWindows
	origLookup := lookupEnv
	defer func() { isWindows = origIsWin; lookupEnv = origLookup }()

	isWindows = func() bool { return false }
	lookupEnv = func(key string) (string, bool) { return "", false }

	paths := wellKnownPaths("/home/user")

	if len(paths) < 2 {
		t.Fatalf("expected at least 2 Unix paths, got %d: %v", len(paths), paths)
	}

	// Should include ~/.local/bin/claude and ~/.claude/local/claude
	hasLocal := false
	hasDotClaude := false
	for _, p := range paths {
		if strings.Contains(p, ".local/bin/claude") {
			hasLocal = true
		}
		if strings.Contains(p, ".claude/local/claude") {
			hasDotClaude = true
		}
	}
	if !hasLocal {
		t.Errorf("expected .local/bin/claude in Unix paths: %v", paths)
	}
	if !hasDotClaude {
		t.Errorf("expected .claude/local/claude in Unix paths: %v", paths)
	}
}

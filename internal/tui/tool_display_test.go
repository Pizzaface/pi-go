package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension"
)

func TestRenderCompactTool_RegularTool(t *testing.T) {
	td := ToolDisplayModel{Width: 80, CompactTools: true}
	msg := message{
		role:    "tool",
		tool:    "read",
		toolIn:  "main.go",
		content: `{"content":"package main\n","total_lines":1}`,
	}
	result := td.RenderToolMessage(msg)
	if !strings.Contains(result, "read") {
		t.Error("expected tool name in compact output")
	}
	if !strings.Contains(result, "✓") {
		t.Error("expected checkmark in compact output")
	}
	// Should be a single line (no multi-line content).
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line in compact output, got %d", len(lines))
	}
}

func TestRenderCompactTool_LongArgs(t *testing.T) {
	td := ToolDisplayModel{Width: 80, CompactTools: true}
	longArg := strings.Repeat("a", 100)
	msg := message{
		role:   "tool",
		tool:   "bash",
		toolIn: longArg,
	}
	result := td.RenderToolMessage(msg)
	// Args should be truncated.
	if strings.Contains(result, longArg) {
		t.Error("expected long args to be truncated")
	}
}

func TestRenderExpandedTool_Default(t *testing.T) {
	td := ToolDisplayModel{Width: 80, CompactTools: false}
	msg := message{
		role:    "tool",
		tool:    "read",
		toolIn:  "main.go",
		content: "     1\tpackage main\n     2\t\n     3\timport \"fmt\"",
	}
	result := td.RenderToolMessage(msg)
	// Expanded mode shows multi-line output with │ borders.
	if !strings.Contains(result, "│") {
		t.Error("expected pipe borders in expanded output")
	}
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) < 2 {
		t.Error("expected multi-line expanded output")
	}
}

func TestCompactToggle_SwitchModes(t *testing.T) {
	td := ToolDisplayModel{Width: 80}
	if td.CompactTools {
		t.Error("expected compact mode off by default")
	}
	td.CompactTools = true
	msg := message{
		role:    "tool",
		tool:    "grep",
		toolIn:  "pattern",
		content: "file.go:1: match\nfile.go:2: another",
	}
	compact := td.RenderToolMessage(msg)
	td.CompactTools = false
	expanded := td.RenderToolMessage(msg)
	if compact == expanded {
		t.Error("compact and expanded output should differ")
	}
	compactLines := strings.Count(compact, "\n")
	expandedLines := strings.Count(expanded, "\n")
	if compactLines >= expandedLines {
		t.Errorf("compact (%d lines) should have fewer lines than expanded (%d lines)",
			compactLines, expandedLines)
	}
}

func TestRenderCompactTool_NoContent(t *testing.T) {
	td := ToolDisplayModel{Width: 80, CompactTools: true}
	msg := message{
		role:   "tool",
		tool:   "write",
		toolIn: "out.txt",
	}
	result := td.RenderToolMessage(msg)
	if !strings.Contains(result, "write") {
		t.Error("expected tool name")
	}
	// No checkmark when no content.
	if strings.Contains(result, "✓") {
		t.Error("expected no checkmark when content is empty")
	}
}

func TestToolCallRow_UsesCustomRendererForSupportedType(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	if err := manager.RegisterDynamicTool("ext.demo", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterRenderer(
		"ext.demo",
		extension.RenderSurfaceToolCallRow,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKindText, Content: "custom call row"}, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	td := ToolDisplayModel{
		Width:            80,
		CompactTools:     false,
		ExtensionManager: manager,
	}
	result := td.RenderToolMessage(message{
		role:   "tool",
		tool:   "demo_tool",
		toolIn: "arg",
	})
	if !strings.Contains(result, "custom call row") {
		t.Fatalf("expected custom renderer output, got %q", result)
	}
}

func TestToolResult_FallsBackWhenExtensionRendererFails(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	if err := manager.RegisterDynamicTool("ext.demo", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterRenderer(
		"ext.demo",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{}, context.DeadlineExceeded
		},
	); err != nil {
		t.Fatal(err)
	}

	td := ToolDisplayModel{
		Width:            80,
		CompactTools:     false,
		ExtensionManager: manager,
	}
	result := td.RenderToolMessage(message{
		role:    "tool",
		tool:    "demo_tool",
		content: "line one\nline two",
	})
	if !strings.Contains(result, "line one") {
		t.Fatalf("expected builtin fallback output, got %q", result)
	}
}

func TestRenderer_FallsBackOnTimeout(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	if err := manager.RegisterDynamicTool("ext.demo", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterRenderer(
		"ext.demo",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			time.Sleep(200 * time.Millisecond)
			return extension.RenderResult{Kind: extension.RenderKindText, Content: "slow custom"}, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	td := ToolDisplayModel{
		Width:            80,
		CompactTools:     false,
		ExtensionManager: manager,
		RenderTimeout:    20 * time.Millisecond,
	}

	start := time.Now()
	result := td.RenderToolMessage(message{
		role:    "tool",
		tool:    "demo_tool",
		content: "fallback text",
	})
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("expected timeout fallback to return quickly, took %s", elapsed)
	}
	if !strings.Contains(result, "fallback text") {
		t.Fatalf("expected builtin fallback content, got %q", result)
	}
}

func TestRenderer_PlainTextAndMarkdownOnly(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	if err := manager.RegisterDynamicTool("ext.demo", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterRenderer(
		"ext.demo",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText, extension.RenderKindMarkdown},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKind("ansi"), Content: "unsupported"}, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	td := ToolDisplayModel{
		Width:            80,
		CompactTools:     false,
		ExtensionManager: manager,
	}
	fallback := td.RenderToolMessage(message{
		role:    "tool",
		tool:    "demo_tool",
		content: "fallback content",
	})
	if !strings.Contains(fallback, "fallback content") {
		t.Fatalf("expected fallback for unsupported kind, got %q", fallback)
	}

	manager2 := extension.NewManager(extension.ManagerOptions{})
	if err := manager2.RegisterDynamicTool("ext.demo", "demo_tool", false); err != nil {
		t.Fatal(err)
	}
	if err := manager2.RegisterRenderer(
		"ext.demo",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindMarkdown},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKindMarkdown, Content: "**md result**"}, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	td2 := ToolDisplayModel{
		Width:            80,
		CompactTools:     false,
		ExtensionManager: manager2,
		RenderMarkdown: func(s string) string {
			return "rendered:" + s
		},
	}
	rendered := td2.RenderToolMessage(message{
		role:    "tool",
		tool:    "demo_tool",
		content: "ignored fallback",
	})
	if !strings.Contains(rendered, "rendered:**md result**") {
		t.Fatalf("expected markdown renderer output, got %q", rendered)
	}
}

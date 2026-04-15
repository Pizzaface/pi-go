package tui

import (
	"regexp"
	"strings"
	"testing"
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

// Spec #5 will re-introduce extension-driven renderer tests once the
// Manager surface is wired.

func TestRenderToolMessage_CollapsedShowsHeaderOnly(t *testing.T) {
	td := ToolDisplayModel{Width: 60, CollapsedTools: true}
	msg := message{
		role:    "tool",
		tool:    "read",
		toolIn:  "internal/tui/chat.go",
		content: "line one\nline two",
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result, "read") {
		t.Fatalf("expected tool header in collapsed output, got %q", result)
	}
	if strings.Contains(result, "line one") || strings.Contains(result, "line two") {
		t.Fatalf("expected collapsed output to hide tool result body, got %q", result)
	}
}

func TestRenderToolMessage_WrapsLongHeaderAtWidth(t *testing.T) {
	td := ToolDisplayModel{Width: 36}
	msg := message{
		role:   "tool",
		tool:   "read",
		toolIn: "C:/Users/Jordan/Documents/Projects/pi-go/internal/tui/really/long/path/to/file_with_long_name.go",
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	lines := nonEmptyToolLines(result)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped multi-line header, got %q", result)
	}
}

func TestRenderAgentTool_CollapsedShowsChevronOnly(t *testing.T) {
	td := ToolDisplayModel{Width: 80}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Branch ship-readiness audit",
		content:   "All checks pass.\nNo issues found.",
		collapsed: true,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result, "▶") {
		t.Error("expected collapsed chevron ▶ in Agent accordion")
	}
	if strings.Contains(result, "▼") {
		t.Error("expected no expanded chevron ▼ when collapsed")
	}
	if strings.Contains(result, "All checks pass") {
		t.Error("expected collapsed Agent to hide body content")
	}
	if !strings.Contains(result, "Agent") {
		t.Error("expected tool name in Agent header")
	}
	if !strings.Contains(result, "Branch ship-readiness audit") {
		t.Error("expected description in Agent header")
	}
}

func TestRenderAgentTool_ExpandedIsHeaderOnly(t *testing.T) {
	td := ToolDisplayModel{Width: 80}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Code review",
		content:   "Found 3 issues.\nLine 42: missing error check.",
		collapsed: false,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result, "▼") {
		t.Error("expected expanded chevron ▼ in Agent accordion")
	}
	if strings.Contains(result, "▶") {
		t.Error("expected no collapsed chevron ▶ when expanded")
	}
	// Body content is rendered by RenderMessages, not the accordion panel.
	if strings.Contains(result, "Found 3 issues") {
		t.Error("expected Agent accordion to NOT contain body content (rendered externally)")
	}
}

func TestRenderAgentTool_ChevronDiffersBetweenStates(t *testing.T) {
	td := ToolDisplayModel{Width: 80}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Test audit",
		content:   "Tests passed.",
		collapsed: true,
	}

	collapsed := stripToolANSI(td.RenderToolMessage(msg))
	msg.collapsed = false
	expanded := stripToolANSI(td.RenderToolMessage(msg))

	if !strings.Contains(collapsed, "▶") {
		t.Error("expected ▶ in collapsed output")
	}
	if !strings.Contains(expanded, "▼") {
		t.Error("expected ▼ in expanded output")
	}
}

func TestRenderAgentTool_SpinnerWhenNoResult(t *testing.T) {
	td := ToolDisplayModel{Width: 80, SpinnerFrame: 0}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Research task",
		content:   "",
		collapsed: true,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	// First spinner frame is "⠋"
	if !strings.Contains(result, spinnerFrames[0]) {
		t.Errorf("expected spinner frame %q when Agent has no result, got %q", spinnerFrames[0], result)
	}

	// Advancing the frame changes the character.
	td.SpinnerFrame = 3
	result2 := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result2, spinnerFrames[3]) {
		t.Errorf("expected spinner frame %q at frame 3, got %q", spinnerFrames[3], result2)
	}
}

func TestRenderAgentTool_NoActiveIndicatorWhenDone(t *testing.T) {
	td := ToolDisplayModel{Width: 80}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Research task",
		content:   "Done.",
		collapsed: true,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	// The chevron ▶ is expected, but no extra ● indicator
	stripped := strings.ReplaceAll(result, "▶", "")
	if strings.Contains(stripped, "●") {
		t.Error("expected no active indicator when Agent has content (done)")
	}
}

func TestRenderAgentTool_NotAffectedByGlobalCollapsed(t *testing.T) {
	td := ToolDisplayModel{Width: 80, CollapsedTools: true}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Research task",
		content:   "Found relevant info.",
		collapsed: false, // Explicitly expanded
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	// Agent accordion renders its own header regardless of global CollapsedTools.
	if !strings.Contains(result, "▼") {
		t.Error("expected expanded chevron for explicitly expanded Agent")
	}
	if !strings.Contains(result, "Agent") {
		t.Error("expected Agent name in header")
	}
}

func TestIsAgentTool(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Agent", true},
		{"agent", true},
		{"AGENT", true},
		{"read", false},
		{"bash", false},
		{"AgentX", false},
	}
	for _, tt := range tests {
		if got := isAgentTool(tt.name); got != tt.want {
			t.Errorf("isAgentTool(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestToolCallSummary_Agent(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "with description",
			args: map[string]any{"description": "Branch audit", "prompt": "Check everything"},
			want: "Branch audit",
		},
		{
			name: "with subagent_type",
			args: map[string]any{"subagent_type": "code-reviewer", "prompt": "Review code"},
			want: "code-reviewer",
		},
		{
			name: "with prompt only",
			args: map[string]any{"prompt": "Do something complex"},
			want: "Do something complex",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolCallSummary("Agent", tt.args)
			if got != tt.want {
				t.Errorf("toolCallSummary(Agent, %v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestToolResultSummary_ContentPartsArray(t *testing.T) {
	// Agent results often come as [{"text":"...","type":"text"}]
	content := `[{"text":"Audit complete. Found 3 issues.","type":"text"}]`
	result := toolResultSummary(content)
	if result != "Audit complete. Found 3 issues." {
		t.Errorf("expected extracted text, got %q", result)
	}
}

func TestToolResultSummary_MultipleContentParts(t *testing.T) {
	content := `[{"text":"Part one.","type":"text"},{"text":"Part two.","type":"text"}]`
	result := toolResultSummary(content)
	if !strings.Contains(result, "Part one.") || !strings.Contains(result, "Part two.") {
		t.Errorf("expected both parts in result, got %q", result)
	}
}

func TestToolResultSummary_GoFormatSinglePart(t *testing.T) {
	// Go %v format: [map[text:... type:text]]
	content := "[map[text:Audit complete. Found 3 issues. type:text]]"
	result := toolResultSummary(content)
	if result != "Audit complete. Found 3 issues." {
		t.Errorf("expected extracted text from Go format, got %q", result)
	}
}

func TestToolResultSummary_GoFormatMultiline(t *testing.T) {
	content := "[map[text:Line one.\nLine two.\nLine three. type:text]]"
	result := toolResultSummary(content)
	if !strings.Contains(result, "Line one.") || !strings.Contains(result, "Line three.") {
		t.Errorf("expected multiline text from Go format, got %q", result)
	}
}

func TestToolResultSummary_GoFormatMultipleParts(t *testing.T) {
	content := "[map[text:Part one. type:text] map[text:Part two. type:text]]"
	result := toolResultSummary(content)
	if !strings.Contains(result, "Part one.") || !strings.Contains(result, "Part two.") {
		t.Errorf("expected both parts from Go format, got %q", result)
	}
}

func TestToolResultSummary_FallsBackForNonParts(t *testing.T) {
	// Plain string — not JSON at all
	result := toolResultSummary("just some text")
	if result != "just some text" {
		t.Errorf("expected plain text fallback, got %q", result)
	}
}

func TestRenderMessages_CollapsedAgentHidesChildTools(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "user", content: "do something"},
		{role: "tool", tool: "Agent", toolIn: "Research task", content: "Done.", collapsed: true, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "main.go", content: "package main", agentGroupID: 1},
		{role: "tool", tool: "grep", toolIn: "pattern", content: "file:1: match", agentGroupID: 1},
		{role: "assistant", content: "Finished."},
	}

	result := stripToolANSI(chat.RenderMessages(false))
	if !strings.Contains(result, "Agent") {
		t.Error("expected Agent header to be visible when collapsed")
	}
	if strings.Contains(result, "main.go") {
		t.Error("expected child tool 'read main.go' to be hidden when Agent is collapsed")
	}
	if strings.Contains(result, "pattern") {
		t.Error("expected child tool 'grep pattern' to be hidden when Agent is collapsed")
	}
	// Agent response should still appear as a conversation message even when collapsed.
	if !strings.Contains(result, "Done.") {
		t.Error("expected Agent response to be visible as conversation message")
	}
	if !strings.Contains(result, "Finished") {
		t.Error("expected assistant message to still be visible")
	}
}

func TestRenderMessages_AgentResponseAppearsAfterChildren(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "user", content: "go"},
		{role: "tool", tool: "Agent", toolIn: "Audit", content: "Audit complete.", collapsed: false, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "main.go", content: "package main", agentGroupID: 1},
		{role: "assistant", content: "All done."},
	}

	result := stripToolANSI(chat.RenderMessages(false))

	// The Agent response should be in the output as a conversation message.
	if !strings.Contains(result, "Audit complete.") {
		t.Error("expected Agent response 'Audit complete.' to appear in output")
	}
	// It should appear AFTER the child tool, not inside the accordion header.
	agentIdx := strings.Index(result, "Agent")
	childIdx := strings.Index(result, "main.go")
	respIdx := strings.Index(result, "Audit complete.")
	if respIdx < childIdx {
		t.Errorf("expected Agent response (pos %d) to appear after child tool (pos %d)", respIdx, childIdx)
	}
	_ = agentIdx // Agent header comes first
}

func TestRenderMessages_ExpandedAgentShowsChildTools(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "user", content: "do something"},
		{role: "tool", tool: "Agent", toolIn: "Research task", content: "Done.", collapsed: false, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "main.go", content: "package main", agentGroupID: 1},
		{role: "tool", tool: "grep", toolIn: "pattern", content: "file:1: match", agentGroupID: 1},
		{role: "assistant", content: "Finished."},
	}

	result := stripToolANSI(chat.RenderMessages(false))
	if !strings.Contains(result, "Agent") {
		t.Error("expected Agent header to be visible")
	}
	if !strings.Contains(result, "main.go") {
		t.Error("expected child tool 'read main.go' to be visible when Agent is expanded")
	}
	if !strings.Contains(result, "pattern") {
		t.Error("expected child tool 'grep pattern' to be visible when Agent is expanded")
	}
}

func TestRenderMessages_UngroupedToolsUnaffectedByAgentCollapse(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "tool", tool: "bash", toolIn: "ls", content: "file1 file2"},
		{role: "tool", tool: "Agent", toolIn: "Audit", content: "OK.", collapsed: true, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "f.go", content: "pkg", agentGroupID: 1},
		{role: "tool", tool: "write", toolIn: "out.txt", content: "wrote"},
		{role: "assistant", content: "Done."},
	}

	result := stripToolANSI(chat.RenderMessages(false))
	// Ungrouped tools (agentGroupID==0) should always render.
	if !strings.Contains(result, "bash") {
		t.Error("expected ungrouped 'bash' tool to be visible")
	}
	if !strings.Contains(result, "write") {
		t.Error("expected ungrouped 'write' tool to be visible")
	}
	// Grouped child should be hidden.
	if strings.Contains(result, "f.go") {
		t.Error("expected grouped child 'read f.go' to be hidden")
	}
}

func TestRenderMessages_ChildToolsAreIndented(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "tool", tool: "Agent", toolIn: "Audit", content: "OK.", collapsed: false, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "main.go", content: "package main", agentGroupID: 1},
		{role: "tool", tool: "bash", toolIn: "ls", content: "file1 file2"},
		{role: "assistant", content: "Done."},
	}

	result := chat.RenderMessages(false)
	lines := strings.Split(result, "\n")

	// Find lines containing "read" (child tool) and "bash" (top-level tool).
	// Child tool lines should have a │ border marker indicating nesting.
	var childLine, topLine string
	for _, line := range lines {
		plain := stripToolANSI(line)
		if strings.Contains(plain, "read") && strings.Contains(plain, "main.go") {
			childLine = plain
		}
		if strings.Contains(plain, "bash") {
			topLine = plain
		}
	}
	if childLine == "" {
		t.Fatal("could not find child tool line containing 'read' + 'main.go'")
	}
	if topLine == "" {
		t.Fatal("could not find top-level tool line containing 'bash'")
	}

	// Child tools should have a │ border marker; top-level tools should not.
	if !strings.Contains(childLine, "│") {
		t.Errorf("expected child tool to have │ border marker, got %q", childLine)
	}
	if strings.Contains(topLine, "│") {
		t.Errorf("expected top-level tool to NOT have │ border marker, got %q", topLine)
	}
}

func TestRenderMessages_NestedAgentGroups(t *testing.T) {
	chat := NewChatModel(nil)
	chat.Width = 80
	chat.Messages = []message{
		{role: "user", content: "go"},
		// Outer Agent (expanded) — group 1
		{role: "tool", tool: "Agent", toolIn: "Outer", content: "outer done", collapsed: false, agentGroupID: 1},
		{role: "tool", tool: "read", toolIn: "a.go", content: "pkg a", agentGroupID: 1},
		// Inner Agent (collapsed) — group 2
		{role: "tool", tool: "Agent", toolIn: "Inner", content: "inner done", collapsed: true, agentGroupID: 2},
		{role: "tool", tool: "grep", toolIn: "inner_pat", content: "inner match", agentGroupID: 2},
		// Back to outer child
		{role: "tool", tool: "write", toolIn: "b.go", content: "pkg b", agentGroupID: 1},
		{role: "assistant", content: "All done."},
	}

	result := stripToolANSI(chat.RenderMessages(false))
	// Outer agent is expanded, so its direct children should be visible.
	if !strings.Contains(result, "a.go") {
		t.Error("expected outer child 'read a.go' to be visible (outer expanded)")
	}
	if !strings.Contains(result, "b.go") {
		t.Error("expected outer child 'write b.go' to be visible (outer expanded)")
	}
	// Inner agent header should be visible (it's a child of expanded outer).
	if !strings.Contains(result, "Inner") {
		t.Error("expected inner Agent header to be visible")
	}
	// Inner agent's child should be hidden (inner is collapsed).
	if strings.Contains(result, "inner_pat") {
		t.Error("expected inner child 'grep inner_pat' to be hidden (inner collapsed)")
	}
}

func TestRenderAgentTool_ShowsToolCallCount(t *testing.T) {
	td := ToolDisplayModel{Width: 80, AgentChildCount: 3}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Code review",
		content:   "Done.",
		collapsed: false,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result, "(3 tool calls)") {
		t.Errorf("expected '(3 tool calls)' in Agent header, got %q", result)
	}
}

func TestRenderAgentTool_SingularToolCallCount(t *testing.T) {
	td := ToolDisplayModel{Width: 80, AgentChildCount: 1}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Quick check",
		content:   "Done.",
		collapsed: true,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if !strings.Contains(result, "(1 tool call)") {
		t.Errorf("expected '(1 tool call)' in Agent header, got %q", result)
	}
}

func TestRenderAgentTool_ZeroCountHidden(t *testing.T) {
	td := ToolDisplayModel{Width: 80, AgentChildCount: 0}
	msg := message{
		role:      "tool",
		tool:      "Agent",
		toolIn:    "Research",
		content:   "",
		collapsed: true,
	}

	result := stripToolANSI(td.RenderToolMessage(msg))
	if strings.Contains(result, "tool call") {
		t.Errorf("expected no tool call count when zero, got %q", result)
	}
}

func stripToolANSI(s string) string {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansi.ReplaceAllString(s, "")
}

func nonEmptyToolLines(s string) []string {
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

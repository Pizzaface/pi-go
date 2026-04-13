package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

// --- toolCallSummary for agent ---

func TestToolCallSummary_Read(t *testing.T) {
	args := map[string]any{"file_path": "/path/to/file.go"}
	result := toolCallSummary("read", args)
	if result != "/path/to/file.go" {
		t.Errorf("expected file path, got %q", result)
	}
}

func TestToolCallSummary_Bash(t *testing.T) {
	args := map[string]any{"command": "go build ./..."}
	result := toolCallSummary("bash", args)
	if result != "go build ./..." {
		t.Errorf("expected command, got %q", result)
	}
}

func TestToolCallSummary_BashLongCommand(t *testing.T) {
	long := strings.Repeat("x", 100)
	args := map[string]any{"command": long}
	result := toolCallSummary("bash", args)
	if len(result) > 80 {
		t.Errorf("expected truncated command, got len=%d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("expected '...' suffix for truncated command")
	}
}

func TestToolCallSummary_Grep(t *testing.T) {
	args := map[string]any{"pattern": "func main"}
	result := toolCallSummary("grep", args)
	if result != "func main" {
		t.Errorf("expected pattern, got %q", result)
	}
}

func TestToolCallSummary_Tree(t *testing.T) {
	args := map[string]any{"path": "src", "depth": float64(3)}
	result := toolCallSummary("tree", args)
	if result != "src (depth 3)" {
		t.Errorf("expected 'src (depth 3)', got %q", result)
	}
}

func TestToolCallSummary_TreeDefaultPath(t *testing.T) {
	args := map[string]any{}
	result := toolCallSummary("tree", args)
	if result != "." {
		t.Errorf("expected '.', got %q", result)
	}
}

func TestToolCallSummary_Unknown(t *testing.T) {
	args := map[string]any{"foo": "bar"}
	result := toolCallSummary("unknown_tool", args)
	if result != "" {
		t.Errorf("expected empty string for unknown tool, got %q", result)
	}
}

// --- formatToolResult for read ---

func TestFormatToolResult_ReadContent(t *testing.T) {
	data := map[string]any{
		"content":     "     1\tpackage main\n     2\t\n     3\tfunc main() {}\n",
		"total_lines": float64(3),
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "package main") {
		t.Errorf("expected content preserved, got %q", result)
	}
	if !strings.Contains(result, "1") {
		t.Errorf("expected line number in content, got %q", result)
	}
}

func TestFormatToolResult_ReadTruncated(t *testing.T) {
	data := map[string]any{
		"content":     "     1\tpackage main\n",
		"total_lines": float64(1000),
		"truncated":   true,
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "1000 total lines, truncated") {
		t.Errorf("expected truncation note, got %q", result)
	}
}

func TestFormatToolResult_ReadNoContent(t *testing.T) {
	data := map[string]any{
		"total_lines": float64(42),
	}
	result := formatToolResult(data)
	if result != "42 lines" {
		t.Errorf("expected '42 lines', got %q", result)
	}
}

func TestFormatToolResult_Bash(t *testing.T) {
	data := map[string]any{
		"exit_code": float64(0),
		"stdout":    "ok",
	}
	result := formatToolResult(data)
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestFormatToolResult_BashError(t *testing.T) {
	data := map[string]any{
		"exit_code": float64(1),
		"stdout":    "build failed",
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "exit 1") {
		t.Errorf("expected 'exit 1' in output, got %q", result)
	}
}

func TestFormatToolResult_BashNoOutput(t *testing.T) {
	data := map[string]any{
		"exit_code": float64(0),
		"stdout":    "",
	}
	result := formatToolResult(data)
	if result != "(No output)" {
		t.Errorf("expected '(No output)', got %q", result)
	}
}

func TestFormatToolResult_Edit(t *testing.T) {
	data := map[string]any{
		"replacements": float64(3),
	}
	result := formatToolResult(data)
	if result != "3 replacements" {
		t.Errorf("expected '3 replacements', got %q", result)
	}
}

func TestFormatToolResult_Write(t *testing.T) {
	data := map[string]any{
		"bytes_written": float64(1024),
		"path":          "/tmp/file.go",
	}
	result := formatToolResult(data)
	if result != "/tmp/file.go (1024 bytes)" {
		t.Errorf("expected path and bytes, got %q", result)
	}
}

func TestFormatToolResult_GrepWithMatches(t *testing.T) {
	data := map[string]any{
		"matches": []any{
			map[string]any{"file": "main.go", "line": float64(10), "content": "func main() {}"},
			map[string]any{"file": "util.go", "line": float64(5), "content": "var x = 1"},
		},
		"total_matches": float64(2),
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "main.go:10:") {
		t.Errorf("expected 'main.go:10:' in output, got %q", result)
	}
	if !strings.Contains(result, "util.go:5:") {
		t.Errorf("expected 'util.go:5:' in output, got %q", result)
	}
	if !strings.Contains(result, "func main()") {
		t.Errorf("expected content in output, got %q", result)
	}
}

func TestFormatToolResult_GrepTruncated(t *testing.T) {
	data := map[string]any{
		"matches": []any{
			map[string]any{"file": "a.go", "line": float64(1), "content": "x"},
		},
		"total_matches": float64(200),
		"truncated":     true,
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "200 total matches, truncated") {
		t.Errorf("expected truncation note, got %q", result)
	}
}

func TestFormatToolResult_GrepFallback(t *testing.T) {
	// No matches array, only count — fallback to "N matches".
	data := map[string]any{
		"total_matches": float64(7),
	}
	result := formatToolResult(data)
	if result != "7 matches" {
		t.Errorf("expected '7 matches', got %q", result)
	}
}

func TestFormatToolResult_FindWithFiles(t *testing.T) {
	data := map[string]any{
		"files":       []any{"internal/tools/read.go", "internal/tools/write.go", "cmd/pi/main.go"},
		"total_files": float64(3),
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "internal/tools/read.go") {
		t.Errorf("expected file path in output, got %q", result)
	}
	if !strings.Contains(result, "cmd/pi/main.go") {
		t.Errorf("expected file path in output, got %q", result)
	}
}

func TestFormatToolResult_FindTruncated(t *testing.T) {
	data := map[string]any{
		"files":       []any{"a.go"},
		"total_files": float64(500),
		"truncated":   true,
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "500 total files, truncated") {
		t.Errorf("expected truncation note, got %q", result)
	}
}

func TestFormatToolResult_FindFallback(t *testing.T) {
	// No files array, only count — fallback to "N files".
	data := map[string]any{
		"total_files": float64(15),
	}
	result := formatToolResult(data)
	if result != "15 files" {
		t.Errorf("expected '15 files', got %q", result)
	}
}

func TestFormatToolResult_Ls(t *testing.T) {
	data := map[string]any{
		"entries": []any{
			map[string]any{"name": "main.go", "is_dir": false},
			map[string]any{"name": "pkg", "is_dir": true},
		},
	}
	result := formatToolResult(data)
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected 'main.go' in ls output, got %q", result)
	}
	if !strings.Contains(result, "pkg/") {
		t.Errorf("expected 'pkg/' in ls output, got %q", result)
	}
}

func TestFormatToolResult_Fallback(t *testing.T) {
	data := map[string]any{
		"custom": "value",
	}
	result := formatToolResult(data)
	if result == "" {
		t.Error("expected non-empty fallback JSON")
	}
}

// --- toolResultSummary ---

func TestToolResultSummary_JSON(t *testing.T) {
	data := map[string]any{"exit_code": float64(0), "stdout": "ok"}
	jsonBytes, _ := json.Marshal(data)
	result := toolResultSummary(string(jsonBytes))
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestToolResultSummary_PlainText(t *testing.T) {
	result := toolResultSummary("just some plain text")
	if result != "just some plain text" {
		t.Errorf("expected plain text preserved, got %q", result)
	}
}

func TestToolResultSummary_LongText(t *testing.T) {
	long := strings.Repeat("x", 200)
	result := toolResultSummary(long)
	if len(result) > 120 {
		t.Errorf("expected truncated to 120, got len=%d", len(result))
	}
}

func TestToolResultSummary_MultiLine(t *testing.T) {
	result := toolResultSummary("line1\nline2\nline3")
	if strings.Contains(result, "\n") {
		t.Error("expected newlines collapsed")
	}
}

func TestRenderMessages_RegularToolUnchanged(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{
				role:    "tool",
				tool:    "read",
				toolIn:  "/path/to/file.go",
				content: "42 lines",
			},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "read") {
		t.Error("expected 'read' tool name")
	}
	if !strings.Contains(output, "/path/to/file.go") {
		t.Error("expected file path in tool args")
	}
}

func TestRenderMessages_GrepHighlighted(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{
				role:    "tool",
				tool:    "grep",
				toolIn:  "func main",
				content: "main.go:5: func main() {}\nutil.go:10: func helper() {}",
			},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "\033[") {
		t.Error("expected ANSI codes for highlighted grep output")
	}
	if !strings.Contains(output, "main.go") {
		t.Error("expected file path in grep output")
	}
}

func TestRenderMessages_FindHighlighted(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{
				role:    "tool",
				tool:    "find",
				toolIn:  "*.go",
				content: "internal/tools/read.go\ninternal/tools/write.go",
			},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "\033[") {
		t.Error("expected ANSI codes for highlighted find output")
	}
	if !strings.Contains(output, "read.go") {
		t.Error("expected file path in find output")
	}
}

func TestInit_NoChannels(t *testing.T) {
	m := &model{
		cfg: Config{},
	}
	cmd := m.Init()
	// With no channels, Init returns tea.Batch() with empty cmds which returns nil.
	_ = cmd
}

// --- renderMessages with read tool highlighting ---

func TestRenderMessages_ReadToolHighlighted(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{
				role:    "tool",
				tool:    "read",
				toolIn:  "main.go",
				content: "     1\tpackage main\n     2\t\n     3\tfunc main() {}",
			},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	// Should contain ANSI codes from syntax highlighting.
	if !strings.Contains(output, "\033[") {
		t.Error("expected ANSI escape codes for highlighted Go code")
	}
	if !strings.Contains(output, "1") {
		t.Error("expected line number in output")
	}
}

// --- renderMessages with various message types ---

func TestRenderMessages_UserMessage(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "hello world"},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "hello world") {
		t.Error("expected user message content")
	}
}

func TestRenderMessages_AssistantMessage(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{role: "assistant", content: "I can help with that"},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "help") {
		t.Error("expected assistant message content")
	}
}

func TestRenderMessages_AssistantBulletSharesFirstLineWithMessage(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{role: "assistant", content: "Hi! What can I help you with?"},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := stripANSIEscapeCodes(m.chatModel.RenderMessages(m.running))
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		t.Fatal("expected rendered assistant output")
	}
	if !strings.Contains(lines[0], "●") || !strings.Contains(lines[0], "Hi! What can I help you with?") {
		t.Fatalf("expected bullet and message on the same rendered line, got: %q", lines[0])
	}
}

func TestRenderMessages_AssistantTextAlignsWithUserText(t *testing.T) {
	m := &model{
		width: 120,
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "Hi!"},
			{role: "assistant", content: "Hi! How can I help?"},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := stripANSIEscapeCodes(m.chatModel.RenderMessages(m.running))
	lines := nonEmptyLines(output)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 rendered lines, got %d: %q", len(lines), output)
	}

	userTextCol := visualColumn(lines[0], "Hi!")
	assistantTextCol := visualColumn(lines[1], "Hi!")
	if userTextCol == -1 || assistantTextCol == -1 {
		t.Fatalf("expected both lines to contain Hi!: %q / %q", lines[0], lines[1])
	}
	if assistantTextCol != userTextCol {
		t.Fatalf("expected assistant text column %d to match user text column %d; user=%q assistant=%q", assistantTextCol, userTextCol, lines[0], lines[1])
	}
}

func TestRenderMessages_UserMessageWrapsWithinChatWidth(t *testing.T) {
	m := &model{
		width: 34,
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "This is a long user message that should wrap cleanly within the chat column."},
		}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := stripANSIEscapeCodes(m.chatModel.RenderMessages(m.running))
	lines := nonEmptyLines(output)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped user message output, got %q", output)
	}
}

func visualColumn(line, text string) int {
	idx := strings.Index(line, text)
	if idx == -1 {
		return -1
	}
	return lipgloss.Width(line[:idx])
}

func stripANSIEscapeCodes(s string) string {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansi.ReplaceAllString(s, "")
}

func nonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestRenderMessages_Empty(t *testing.T) {
	m := &model{
		width:     120,
		chatModel: ChatModel{Messages: []message{}},
	}
	m.chatModel.UpdateRenderer(m.width)

	output := m.chatModel.RenderMessages(m.running)
	if !strings.Contains(output, "Welcome") {
		t.Error("expected welcome message for empty conversation")
	}
}

// --- isUserInput ---

func TestIsUserInput_Normal(t *testing.T) {
	if !isUserInput("hello") {
		t.Error("expected 'hello' to be user input")
	}
}

func TestIsUserInput_NonPrintable(t *testing.T) {
	if isUserInput("\x00invalid") {
		t.Error("expected non-printable chars to be rejected")
	}
}

func TestIsUserInput_TerminalEscape(t *testing.T) {
	if isUserInput("]11;rgb:ffff/ffff/ffff") {
		t.Error("expected terminal escape sequence to be rejected")
	}
}

// --- Screen ---

func TestScreen_UpdateAndRead(t *testing.T) {
	s := &Screen{}
	s.update("test content")
	if s.ScreenContent() != "test content" {
		t.Errorf("expected 'test content', got %q", s.ScreenContent())
	}
}

func TestScreen_Empty(t *testing.T) {
	s := &Screen{}
	if s.ScreenContent() != "" {
		t.Errorf("expected empty string, got %q", s.ScreenContent())
	}
}

// --- additional agent message tests from existing patterns ---

func TestAgentTextMsg_AccumulatesStreaming(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{role: "assistant", content: ""}}},
		running:   true,
		agentCh:   make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentTextMsg{text: "Hello ", partial: true})
	mm := newM.(*model)
	if mm.chatModel.Streaming != "Hello " {
		t.Errorf("expected streaming 'Hello ', got %q", mm.chatModel.Streaming)
	}

	newM2, _ := mm.Update(agentTextMsg{text: "world", partial: true})
	mm2 := newM2.(*model)
	if mm2.chatModel.Streaming != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", mm2.chatModel.Streaming)
	}
}

func TestAgentTextMsg_FinalReplayDoesNotDuplicateStreaming(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{role: "assistant", content: ""}}},
		running:   true,
		agentCh:   make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentTextMsg{text: "Hello ", partial: true})
	mm := newM.(*model)
	newM, _ = mm.Update(agentTextMsg{text: "world", partial: true})
	mm = newM.(*model)
	newM, _ = mm.Update(agentTextMsg{text: "Hello world"})
	mm = newM.(*model)

	if mm.chatModel.Streaming != "Hello world" {
		t.Fatalf("expected streaming %q, got %q", "Hello world", mm.chatModel.Streaming)
	}
	if got := mm.chatModel.Messages[len(mm.chatModel.Messages)-1].content; got != "Hello world" {
		t.Fatalf("expected assistant content %q, got %q", "Hello world", got)
	}
}

func TestAgentDoneMsg_ClearsRunning(t *testing.T) {
	m := &model{
		chatModel: ChatModel{
			Messages:  []message{{role: "assistant", content: "done"}},
			Streaming: "text",
			Thinking:  "thought",
		},
		running: true,
		statusModel: StatusModel{
			ActiveTool:  "read",
			ActiveTools: map[string]time.Time{"read": {}},
		},
		agentCh: make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentDoneMsg{})
	mm := newM.(*model)
	if mm.running {
		t.Error("expected running=false after done")
	}
	if mm.statusModel.ActiveTool != "" {
		t.Errorf("expected empty activeTool, got %q", mm.statusModel.ActiveTool)
	}
	if mm.statusModel.ActiveTools != nil {
		t.Error("expected nil activeTools")
	}
	if mm.chatModel.Streaming != "" {
		t.Error("expected empty streaming")
	}
}

func TestAgentDoneMsg_WithError(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{role: "assistant"}}},
		running:   true,
		agentCh:   make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentDoneMsg{err: fmt.Errorf("connection lost")})
	mm := newM.(*model)
	found := false
	for _, msg := range mm.chatModel.Messages {
		if strings.Contains(msg.content, "connection lost") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error message in messages")
	}
}

func TestAgentToolCallMsg_SetsActiveTool(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		running:   true,
		agentCh:   make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentToolCallMsg{
		name: "read",
		args: map[string]any{"file_path": "/tmp/file.go"},
	})
	mm := newM.(*model)
	if mm.statusModel.ActiveTool != "read" {
		t.Errorf("expected activeTool 'read', got %q", mm.statusModel.ActiveTool)
	}
}

// TestAgentToolCallMsg_ToolsBeforeAssistant verifies that tool messages are
// inserted before the trailing assistant placeholder so they render above the
// final response text.
func TestAgentToolCallMsg_ToolsBeforeAssistant(t *testing.T) {
	// Simulate the state after submitPrompt: [user, assistant("")]
	m := &model{
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "explain"},
			{role: "assistant", content: ""},
		}},
		running: true,
		agentCh: make(chan agentMsg, 64),
	}

	// First tool call.
	newM, _ := m.Update(agentToolCallMsg{name: "read", args: map[string]any{"file_path": "a.go"}})
	mm := newM.(*model)

	// Second tool call.
	newM2, _ := mm.Update(agentToolCallMsg{name: "bash", args: map[string]any{"command": "ls"}})
	mm2 := newM2.(*model)

	msgs := mm2.chatModel.Messages
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}

	// Expected order: user, tool(read), tool(bash), assistant
	if msgs[0].role != "user" {
		t.Errorf("msgs[0] should be user, got %q", msgs[0].role)
	}
	if msgs[1].role != "tool" || msgs[1].tool != "read" {
		t.Errorf("msgs[1] should be tool/read, got %q/%q", msgs[1].role, msgs[1].tool)
	}
	if msgs[2].role != "tool" || msgs[2].tool != "bash" {
		t.Errorf("msgs[2] should be tool/bash, got %q/%q", msgs[2].role, msgs[2].tool)
	}
	if msgs[3].role != "assistant" {
		t.Errorf("msgs[3] should be assistant, got %q", msgs[3].role)
	}
}

// TestAgentText_RelocatesAssistantAfterTools verifies that when text arrives
// after tool messages have been appended, the assistant message is moved to
// the tail so it renders below tools.
func TestAgentText_RelocatesAssistantAfterTools(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "explain"},
			{role: "assistant", content: ""}, // placeholder
			{role: "tool", tool: "read"},     // tool appended after (e.g. via thinking path)
		}},
		running: true,
		agentCh: make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentTextMsg{text: "Here's the answer", partial: false})
	mm := newM.(*model)

	msgs := mm.chatModel.Messages
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (no duplicates), got %d", len(msgs))
	}
	if msgs[1].role != "tool" {
		t.Errorf("msgs[1] should be tool, got %q", msgs[1].role)
	}
	if msgs[2].role != "assistant" || msgs[2].content != "Here's the answer" {
		t.Errorf("msgs[2] should be assistant with text, got role=%q content=%q", msgs[2].role, msgs[2].content)
	}
}

// TestAgentText_ThinkingThenToolThenText verifies the full flow:
// thinking → tool call → text response — no duplicates, correct order.
func TestAgentText_ThinkingThenToolThenText(t *testing.T) {
	// Start: [user, assistant("")]
	m := &model{
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "hi"},
			{role: "assistant", content: ""},
		}},
		running: true,
		agentCh: make(chan agentMsg, 64),
	}

	// 1. Thinking arrives.
	newM, _ := m.Update(agentThinkingMsg{text: "let me think"})
	mm := newM.(*model)
	// Should be: [user, assistant(""), thinking("let me think")]

	// 2. Tool call arrives (last is thinking, not assistant → appends).
	newM, _ = mm.Update(agentToolCallMsg{name: "read", args: map[string]any{"file_path": "x.go"}})
	mm = newM.(*model)
	// Should be: [user, assistant(""), thinking("let me think"), tool(read)]

	// 3. Text arrives — should relocate assistant to tail, keep thinking.
	newM, _ = mm.Update(agentTextMsg{text: "Done!", partial: false})
	mm = newM.(*model)

	msgs := mm.chatModel.Messages
	// Thinking persists in history, assistant relocated to tail.
	// Expected: [user, thinking("let me think"), tool(read), assistant("Done!")]

	lastMsg := msgs[len(msgs)-1]
	if lastMsg.role != "assistant" || lastMsg.content != "Done!" {
		t.Errorf("last message should be assistant/Done!, got %q/%q", lastMsg.role, lastMsg.content)
	}

	// Count assistant messages — should be exactly 1 (no duplicates).
	assistantCount := 0
	for _, msg := range msgs {
		if msg.role == "assistant" {
			assistantCount++
		}
	}
	if assistantCount != 1 {
		t.Errorf("expected exactly 1 assistant message, got %d; messages: %+v", assistantCount, msgs)
	}
}

// TestAgentText_ThinkingThenTextPersists verifies that thinking messages
// remain in the chat history when text arrives directly after them.
func TestAgentText_ThinkingThenTextPersists(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "hi"},
			{role: "assistant", content: ""},
		}},
		running: true,
		agentCh: make(chan agentMsg, 64),
	}

	// 1. Thinking arrives.
	newM, _ := m.Update(agentThinkingMsg{text: "Let me analyze..."})
	mm := newM.(*model)

	// 2. Text arrives directly — thinking should persist.
	newM, _ = mm.Update(agentTextMsg{text: "Here's the answer.", partial: false})
	mm = newM.(*model)

	msgs := mm.chatModel.Messages
	// Expected: [user, thinking("Let me analyze..."), assistant("Here's the answer.")]

	// Thinking message should still be present.
	thinkingCount := 0
	for _, msg := range msgs {
		if msg.role == "thinking" {
			thinkingCount++
			if msg.content != "Let me analyze..." {
				t.Errorf("thinking content = %q, want %q", msg.content, "Let me analyze...")
			}
		}
	}
	if thinkingCount != 1 {
		t.Errorf("expected 1 thinking message, got %d; messages: %+v", thinkingCount, msgs)
	}

	// Assistant should be at the tail.
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.role != "assistant" || lastMsg.content != "Here's the answer." {
		t.Errorf("last message should be assistant/'Here's the answer.', got %q/%q", lastMsg.role, lastMsg.content)
	}

	// Thinking accumulator should be cleared.
	if mm.chatModel.Thinking != "" {
		t.Errorf("Thinking accumulator should be empty, got %q", mm.chatModel.Thinking)
	}
}

// TestAgentText_TextBeforeToolStaysAbove verifies that when text precedes a
// tool call, the text is frozen above the tool and subsequent post-tool text
// appears separately below it — not concatenated or overwritten.
func TestAgentText_TextBeforeToolStaysAbove(t *testing.T) {
	// Start: [user, assistant("")]
	m := &model{
		chatModel: ChatModel{Messages: []message{
			{role: "user", content: "check the file"},
			{role: "assistant", content: ""},
		}},
		running: true,
		agentCh: make(chan agentMsg, 64),
	}

	// 1. Text streams in before the tool call.
	newM, _ := m.Update(agentTextMsg{text: "Let me check.", partial: true})
	mm := newM.(*model)

	// 2. Tool call arrives — pre-tool text should be frozen.
	newM, _ = mm.Update(agentToolCallMsg{name: "read", args: map[string]any{"file_path": "a.go"}})
	mm = newM.(*model)

	// Streaming buffer should be cleared for the next text segment.
	if mm.chatModel.Streaming != "" {
		t.Errorf("expected empty Streaming after tool call, got %q", mm.chatModel.Streaming)
	}

	// 3. Post-tool text arrives.
	newM, _ = mm.Update(agentTextMsg{text: "Here's what I found.", partial: true})
	mm = newM.(*model)

	msgs := mm.chatModel.Messages
	// Expected order:
	//   [user, assistant("Let me check."), tool(read), assistant("Here's what I found.")]
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].role != "assistant" || msgs[1].content != "Let me check." {
		t.Errorf("msgs[1] should be frozen assistant text, got role=%q content=%q", msgs[1].role, msgs[1].content)
	}
	if msgs[2].role != "tool" || msgs[2].tool != "read" {
		t.Errorf("msgs[2] should be tool/read, got %q/%q", msgs[2].role, msgs[2].tool)
	}
	if msgs[3].role != "assistant" || msgs[3].content != "Here's what I found." {
		t.Errorf("msgs[3] should be new assistant text, got role=%q content=%q", msgs[3].role, msgs[3].content)
	}
}

func TestAgentToolResultMsg_ClearsActiveTool(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{role: "tool", tool: "read", content: ""}}},
		running:   true,
		statusModel: StatusModel{
			ActiveTool:  "read",
			ActiveTools: map[string]time.Time{"read": {}},
		},
		agentCh: make(chan agentMsg, 64),
	}

	newM, _ := m.Update(agentToolResultMsg{
		name:    "read",
		content: `{"content":"hello","total_lines":1}`,
	})
	mm := newM.(*model)
	if mm.statusModel.ActiveTool != "" {
		t.Errorf("expected empty activeTool, got %q", mm.statusModel.ActiveTool)
	}
	if mm.chatModel.Messages[0].content == "" {
		t.Error("expected message content to be updated")
	}
}

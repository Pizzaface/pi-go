package claudecli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// mockCLIScript creates a shell script that simulates a Claude CLI session.
// It outputs the system init line immediately, then for each stdin line
// (user message), outputs the given response NDJSON lines.
func mockCLIScript(t *testing.T, initLines []string, responseLines []string) string {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")

	// Output init lines immediately.
	for _, line := range initLines {
		escaped := strings.ReplaceAll(line, "'", "'\\''")
		fmt.Fprintf(&sb, "echo '%s'\n", escaped)
	}

	// For each stdin line, output response lines.
	sb.WriteString("while IFS= read -r input; do\n")
	// Skip control messages.
	sb.WriteString("  case \"$input\" in *control_response*) continue ;; esac\n")
	for _, line := range responseLines {
		escaped := strings.ReplaceAll(line, "'", "'\\''")
		fmt.Fprintf(&sb, "  echo '%s'\n", escaped)
	}
	sb.WriteString("done\n")

	f, err := os.CreateTemp(t.TempDir(), "mock-claude-*.sh")
	if err != nil {
		t.Fatalf("creating mock script: %v", err)
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		t.Fatalf("writing mock script: %v", err)
	}
	f.Close()
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		t.Fatalf("chmod mock script: %v", err)
	}
	return f.Name()
}

// NDJSON fixtures for testing.
const (
	systemInit = `{"type":"system","subtype":"init","session_id":"test-session","uuid":"sys-001","cwd":"/tmp","model":"claude-sonnet-4-6","tools":["Read","Write","Edit","Bash"],"mcp_servers":[],"permissionMode":"default","apiKeySource":"anthropic","claude_code_version":"2.1.39"}`

	assistantText = `{"type":"assistant","uuid":"msg_01","session_id":"test-session","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"Hello! The answer is 42."}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20}}}`

	assistantWithToolUse = `{"type":"assistant","uuid":"msg_02","session_id":"test-session","message":{"id":"msg_02","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"Let me check that file."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/tmp/test.go"}}],"stop_reason":"tool_use","usage":{"input_tokens":200,"output_tokens":50}}}`

	userToolResult = `{"type":"user","uuid":"usr_01","session_id":"test-session","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"package main\nfunc main() {}","is_error":false}]}}`

	assistantAfterTool = `{"type":"assistant","uuid":"msg_03","session_id":"test-session","message":{"id":"msg_03","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"The file contains a simple main package."}],"stop_reason":"end_turn","usage":{"input_tokens":300,"output_tokens":30}}}`

	resultWithText = `{"type":"result","subtype":"success","session_id":"test-session","is_error":false,"result":"Hello! The answer is 42.","num_turns":1,"duration_ms":1234,"duration_api_ms":890,"total_cost_usd":0.0023,"usage":{"input_tokens":100,"output_tokens":20}}`

	resultEmpty = `{"type":"result","subtype":"success","session_id":"test-session","is_error":false,"num_turns":2,"duration_ms":2000,"duration_api_ms":1500,"total_cost_usd":0.005,"usage":{"input_tokens":300,"output_tokens":50}}`

	assistantThinking = `{"type":"assistant","uuid":"msg_04","session_id":"test-session","message":{"id":"msg_04","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"thinking","thinking":"Let me analyze this...","signature":"sig_abc"},{"type":"text","text":"Here is my analysis."}],"stop_reason":"end_turn","usage":{"input_tokens":300,"output_tokens":100}}}`
)

// TestE2E_SimpleTextResponse tests the full path:
// mock CLI script → claude SDK Session → Provider.GenerateContent → model.LLMResponse
func TestE2E_SimpleTextResponse(t *testing.T) {
	script := mockCLIScript(t,
		[]string{systemInit},
		[]string{assistantText, resultWithText},
	)

	p := New(Config{BinaryPath: script, WorkDir: t.TempDir()})
	t.Cleanup(func() { p.Close() })

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("What is the answer?")}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var responses []*model.LLMResponse
	var errors []error
	for resp, err := range p.GenerateContent(ctx, req, true) {
		if err != nil {
			errors = append(errors, err)
			break
		}
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	if len(errors) > 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}

	t.Logf("Got %d responses", len(responses))
	for i, resp := range responses {
		t.Logf("  [%d] Partial=%v TurnComplete=%v Content=%v", i, resp.Partial, resp.TurnComplete, resp.Content != nil)
		if resp.Content != nil {
			for j, part := range resp.Content.Parts {
				t.Logf("       Part[%d] Text=%q", j, truncate(part.Text, 80))
			}
		}
	}

	// Verify we got at least one response with text.
	foundText := false
	for _, resp := range responses {
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if strings.Contains(part.Text, "42") {
					foundText = true
				}
			}
		}
	}
	if !foundText {
		t.Error("expected to find '42' in response text")
	}

	// Verify the last response is turn-complete.
	if len(responses) == 0 {
		t.Fatal("no responses received")
	}
	last := responses[len(responses)-1]
	if !last.TurnComplete {
		t.Error("last response should have TurnComplete=true")
	}
	if last.Content == nil {
		t.Error("last response Content must not be nil (ADK flow requires it)")
	}
}

// TestE2E_ToolUseSession tests a session with tool use:
// assistant (text + tool_use) → user (tool_result) → assistant (text) → result
func TestE2E_ToolUseSession(t *testing.T) {
	script := mockCLIScript(t,
		[]string{systemInit},
		[]string{assistantWithToolUse, userToolResult, assistantAfterTool, resultEmpty},
	)

	p := New(Config{BinaryPath: script, WorkDir: t.TempDir()})
	t.Cleanup(func() { p.Close() })

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("Read the test file")}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var responses []*model.LLMResponse
	var errors []error
	for resp, err := range p.GenerateContent(ctx, req, true) {
		if err != nil {
			errors = append(errors, err)
			break
		}
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	if len(errors) > 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}

	t.Logf("Got %d responses", len(responses))
	for i, resp := range responses {
		t.Logf("  [%d] Partial=%v TurnComplete=%v Content=%v", i, resp.Partial, resp.TurnComplete, resp.Content != nil)
		if resp.Content != nil {
			for j, part := range resp.Content.Parts {
				t.Logf("       Part[%d] Text=%q", j, truncate(part.Text, 80))
			}
		}
	}

	// Should have responses for: assistant(tool_use), user(tool_result), assistant(text), result
	if len(responses) < 3 {
		t.Errorf("expected at least 3 responses, got %d", len(responses))
	}

	// Verify tool use text appears.
	foundToolUse := false
	foundFinalText := false
	for _, resp := range responses {
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if strings.Contains(part.Text, "[tool:Read]") {
					foundToolUse = true
				}
				if strings.Contains(part.Text, "simple main package") {
					foundFinalText = true
				}
			}
		}
	}
	if !foundToolUse {
		t.Error("expected to find [tool:Read] in response text")
	}
	if !foundFinalText {
		t.Error("expected to find final answer text")
	}

	// Last response must be turn-complete with non-nil Content.
	last := responses[len(responses)-1]
	if !last.TurnComplete {
		t.Error("last response should have TurnComplete=true")
	}
	if last.Content == nil {
		t.Error("last response Content must not be nil (ADK requires non-nil for turn completion)")
	}
}

// TestE2E_EmptyResult tests that an empty ResultMessage still produces a
// valid turn-complete response with non-nil Content.
func TestE2E_EmptyResult(t *testing.T) {
	script := mockCLIScript(t,
		[]string{systemInit},
		[]string{assistantText, resultEmpty},
	)

	p := New(Config{BinaryPath: script, WorkDir: t.TempDir()})
	t.Cleanup(func() { p.Close() })

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("hello")}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var responses []*model.LLMResponse
	var errors []error
	for resp, err := range p.GenerateContent(ctx, req, true) {
		if err != nil {
			errors = append(errors, err)
			break
		}
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	if len(errors) > 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}

	t.Logf("Got %d responses", len(responses))
	for i, resp := range responses {
		t.Logf("  [%d] Partial=%v TurnComplete=%v Content=%v", i, resp.Partial, resp.TurnComplete, resp.Content != nil)
		if resp.Content != nil {
			for j, part := range resp.Content.Parts {
				t.Logf("       Part[%d] Text=%q", j, truncate(part.Text, 80))
			}
		}
	}

	if len(responses) == 0 {
		t.Fatal("no responses received")
	}

	last := responses[len(responses)-1]
	if !last.TurnComplete {
		t.Error("last response should have TurnComplete=true")
	}
	if last.Content == nil {
		t.Fatal("CRITICAL: last response Content is nil — ADK flow will skip this event and error with 'TODO: last event is not final'")
	}
}

// TestE2E_ThinkingBlocks tests that thinking content is surfaced.
func TestE2E_ThinkingBlocks(t *testing.T) {
	script := mockCLIScript(t,
		[]string{systemInit},
		[]string{assistantThinking, resultEmpty},
	)

	p := New(Config{BinaryPath: script, WorkDir: t.TempDir()})
	t.Cleanup(func() { p.Close() })

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("analyze this")}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var responses []*model.LLMResponse
	for resp, err := range p.GenerateContent(ctx, req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	t.Logf("Got %d responses", len(responses))
	for i, resp := range responses {
		t.Logf("  [%d] Partial=%v TurnComplete=%v", i, resp.Partial, resp.TurnComplete)
		if resp.Content != nil {
			for j, part := range resp.Content.Parts {
				t.Logf("       Part[%d] Text=%q", j, truncate(part.Text, 80))
			}
		}
	}

	foundThinking := false
	foundAnalysis := false
	for _, resp := range responses {
		if resp.Content != nil {
			if resp.Content.Role == "thinking" {
				foundThinking = true
			}
			for _, part := range resp.Content.Parts {
				if strings.Contains(part.Text, "analysis") {
					foundAnalysis = true
				}
			}
		}
	}
	if !foundThinking {
		t.Error("expected thinking block with Role=\"thinking\" in response")
	}
	if !foundAnalysis {
		t.Error("expected analysis text in response")
	}
}

// TestE2E_ADKFlowIntegration tests the full ADK agent stack:
// Provider → ADK Agent → Runner → session.Event
// This is what the TUI actually consumes.
func TestE2E_ADKFlowIntegration(t *testing.T) {
	script := mockCLIScript(t,
		[]string{systemInit},
		[]string{assistantText, resultWithText},
	)

	provider := New(Config{BinaryPath: script, WorkDir: t.TempDir()})
	t.Cleanup(func() { provider.Close() })

	// Create a real ADK agent with our provider.
	agent, err := newTestAgent(provider)
	if err != nil {
		t.Fatalf("creating test agent: %v", err)
	}

	sessionID, err := agent.CreateSession(context.Background())
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run through the full ADK stack — this is what the TUI calls.
	var events []eventSummary
	for ev, err := range agent.RunStreaming(ctx, sessionID, "What is the answer?") {
		if err != nil {
			t.Logf("ADK error: %v", err)
			events = append(events, eventSummary{err: err})
			break
		}
		if ev == nil {
			continue
		}
		summary := eventSummary{
			author:       ev.Author,
			hasContent:   ev.Content != nil,
			partial:      ev.Partial,
			turnComplete: ev.TurnComplete,
		}
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					summary.texts = append(summary.texts, part.Text)
				}
			}
		}
		events = append(events, summary)
	}

	t.Logf("Got %d events from ADK RunStreaming:", len(events))
	for i, ev := range events {
		if ev.err != nil {
			t.Logf("  [%d] ERROR: %v", i, ev.err)
		} else {
			t.Logf("  [%d] author=%q hasContent=%v partial=%v turnComplete=%v texts=%v",
				i, ev.author, ev.hasContent, ev.partial, ev.turnComplete, ev.texts)
		}
	}

	// Verify: at least one event has text containing "42".
	foundText := false
	for _, ev := range events {
		for _, text := range ev.texts {
			if strings.Contains(text, "42") {
				foundText = true
			}
		}
	}
	if !foundText {
		t.Error("FAIL: no event contained '42' — TUI would show nothing")
	}

	// Verify: no errors.
	for _, ev := range events {
		if ev.err != nil {
			t.Errorf("FAIL: ADK flow error: %v", ev.err)
		}
	}

	// Verify: at least one event has hasContent=true (TUI skips nil content).
	foundContent := false
	for _, ev := range events {
		if ev.hasContent && len(ev.texts) > 0 {
			foundContent = true
		}
	}
	if !foundContent {
		t.Error("FAIL: no events had content with text — TUI would show nothing")
	}
}

// TestE2E_RealCLI tests with the actual claude binary.
// Skipped unless CLAUDE_CLI_E2E=1 is set (requires auth).
func TestE2E_RealCLI(t *testing.T) {
	if os.Getenv("CLAUDE_CLI_E2E") != "1" {
		t.Skip("set CLAUDE_CLI_E2E=1 to run with real Claude CLI")
	}

	binary, err := FindBinary()
	if err != nil {
		t.Skipf("claude CLI not found: %v", err)
	}
	t.Logf("Using claude binary: %s", binary)

	p := New(Config{
		BinaryPath: binary,
		WorkDir:    t.TempDir(),
	})
	t.Cleanup(func() { p.Close() })

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{genai.NewPartFromText("Say exactly: hello world")}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("Sending prompt to real Claude CLI...")
	var responses []*model.LLMResponse
	for resp, err := range p.GenerateContent(ctx, req, true) {
		if err != nil {
			t.Logf("ERROR: %v", err)
			break
		}
		if resp != nil {
			responses = append(responses, resp)
			t.Logf("Response: Partial=%v TurnComplete=%v", resp.Partial, resp.TurnComplete)
			if resp.Content != nil {
				for _, part := range resp.Content.Parts {
					if part.Text != "" {
						t.Logf("  Text: %q", truncate(part.Text, 200))
					}
				}
			}
		}
	}

	t.Logf("Total responses: %d", len(responses))
	if len(responses) == 0 {
		t.Fatal("FAIL: no responses from real Claude CLI — check stderr logs above")
	}
}

type eventSummary struct {
	author       string
	hasContent   bool
	partial      bool
	turnComplete bool
	texts        []string
	err          error
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

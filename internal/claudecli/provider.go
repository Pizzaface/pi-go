// Package claudecli implements a model.LLM provider that delegates to the
// Claude Code CLI via the partio-io/claude-agent-sdk-go SDK. Claude CLI
// handles its own tool execution internally (pass-through mode); go-pi
// streams the output and manages tool approval requests.
package claudecli

import (
	"context"
	"fmt"
	"iter"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/partio-io/claude-agent-sdk-go"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/dimetron/pi-go/internal/llmutil"
)

// Config holds configuration for the Claude CLI provider.
type Config struct {
	// BinaryPath is the path to the `claude` CLI binary.
	// If empty, resolved from $CLAUDE_CLI_PATH then exec.LookPath.
	BinaryPath string

	// WorkDir is the working directory for the CLI process.
	WorkDir string

	// EnvVars are additional environment variables for the CLI process.
	EnvVars map[string]string

	// AllowedTools are tool glob patterns that auto-approve without
	// hitting the CanUseTool callback.
	AllowedTools []string

	// AppendSystemPrompt appends text to Claude CLI's default system prompt.
	// This is the recommended way to inject go-pi context since the provider
	// cannot replace Claude CLI's system prompt entirely.
	AppendSystemPrompt string

	// MaxThinkingTokens sets the thinking budget for the CLI session.
	// 0 means use the CLI's default. Maps to WithMaxThinkingTokens in the SDK.
	MaxThinkingTokens int

	// ApprovalRules are evaluated in order when the CLI requests tool
	// permission. If no rule matches, the tool is auto-approved.
	ApprovalRules []ApprovalRule
}

// ApprovalRule defines a policy for tool permission decisions.
type ApprovalRule struct {
	ToolName     string   // exact tool name match (e.g. "Bash", "Write")
	AllowPaths   []string // filepath.Match globs; if set, allow when path matches
	DenyCmds     []string // command prefixes to deny (first token match)
	AllowCmds    []string // command prefixes to allow (first token match)
	DefaultAllow bool     // if true, allow when no path/cmd patterns match
}

// Provider implements model.LLM by delegating to Claude Code CLI.
type Provider struct {
	config         Config
	session        *claude.Session
	mu             sync.Mutex
	closed         bool
	sawStreamDelta bool              // true once we've emitted any stream delta (for dedup)
	toolNames      map[string]string // tool_use ID → tool name, for matching results to calls
}

// anthropicEnvKeys are environment variables that Claude CLI reads to use an
// "external" API key instead of its own stored OAuth credentials. We strip
// them from the subprocess environment at spawn time so a stale key loaded
// from ~/.pi-go/.env (used by the native anthropic provider) cannot hijack
// the CLI's own auth flow.
var anthropicEnvKeys = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// spawnEnvMu serializes process-environment manipulation around the Claude
// CLI subprocess spawn. Without this, parallel spawns from multiple providers
// could see an inconsistent environment.
var spawnEnvMu sync.Mutex

// withCleanAnthropicEnv temporarily unsets ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN
// in the parent process environment, runs fn, then restores the previous values.
// This lets Claude CLI fall back to its own stored credentials during spawn.
//
// Side note: this briefly affects *any* subprocess spawned concurrently by the
// same pi-go process. That window is only as long as fn() runs, which in
// practice is a few hundred milliseconds covering the CLI spawn and first
// write. The alternative (passing a stale ANTHROPIC_API_KEY through) breaks
// Claude CLI outright, so this tradeoff is worth it.
func withCleanAnthropicEnv(fn func() error) error {
	spawnEnvMu.Lock()
	defer spawnEnvMu.Unlock()

	type snapshot struct {
		value string
		had   bool
	}
	saved := make(map[string]snapshot, len(anthropicEnvKeys))
	for _, k := range anthropicEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = snapshot{value: v, had: true}
			_ = os.Unsetenv(k)
		}
	}
	defer func() {
		for k, s := range saved {
			if s.had {
				_ = os.Setenv(k, s.value)
			}
		}
	}()

	return fn()
}

// New creates a new Claude CLI provider.
func New(cfg Config) *Provider {
	return &Provider{config: cfg}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "claudecli"
}

// Close shuts down the underlying CLI session.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.session != nil {
		err := p.session.Close()
		p.session = nil
		return err
	}
	return nil
}

// GenerateContent sends a user message to the Claude CLI and streams back
// responses as model.LLMResponse events. Claude CLI handles tool execution
// internally; tool activity is surfaced as structured text in the response.
func (p *Provider) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Note: Claude CLI uses its own system prompt. ADK system instructions
		// are not forwarded. This is by design — see the design spec.

		// Extract user message text from the request.
		userText := extractUserMessage(req)
		if userText == "" {
			yield(nil, fmt.Errorf("claudecli: no user message in request"))
			return
		}

		// Reset per-turn state.
		p.sawStreamDelta = false
		p.toolNames = nil

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			yield(nil, fmt.Errorf("claudecli: provider is closed"))
			return
		}

		// Ensure session is started. NewSession itself does not spawn — the
		// subprocess is spawned lazily on the first Send, which is why we
		// wrap only that first Send in withCleanAnthropicEnv below.
		firstSend := false
		if p.session == nil {
			p.session = claude.NewSession(p.buildOptions()...)
			firstSend = true
		}
		session := p.session
		p.mu.Unlock()

		// Send the user message. On the very first Send we strip
		// ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN from the parent env so
		// the spawned Claude CLI uses its own stored OAuth credentials
		// rather than a (possibly-stale) key loaded from ~/.pi-go/.env.
		sendFn := func() error { return session.Send(ctx, userText) }
		var sendErr error
		if firstSend {
			sendErr = withCleanAnthropicEnv(sendFn)
		} else {
			sendErr = sendFn()
		}
		if err := sendErr; err != nil {
			// Session may have died — reset so next call creates a fresh one.
			p.mu.Lock()
			if p.session == session {
				_ = session.Close()
				p.session = nil
			}
			p.mu.Unlock()
			yield(nil, fmt.Errorf("claudecli: send: %w", err))
			return
		}

		// Stream responses.
		gotMessages := false
		for msg, err := range session.Stream(ctx) {
			if err != nil {
				// The SDK yields parse errors for malformed messages but continues
				// reading. Common case: SystemMessage.Agents field changed from
				// map to array in newer CLI versions. Log and continue.
				if msg == nil {
					// Silently skip — typically SDK/CLI version mismatch on system init.
					continue
				}
				// If we have both msg and err, something unusual happened.
				yield(nil, fmt.Errorf("claudecli: stream: %w", err))
				return
			}

			gotMessages = true

			responses := p.messageToResponses(msg)
			for _, resp := range responses {
				if !yield(resp, nil) {
					return
				}
			}
		}

		// If we got no messages at all, the CLI process likely died on startup.
		if !gotMessages {
			p.mu.Lock()
			if p.session == session {
				_ = session.Close()
				p.session = nil
			}
			p.mu.Unlock()
			yield(nil, fmt.Errorf("claudecli: CLI process produced no output — check stderr logs above for auth/startup errors"))
			return
		}
	}
}

// buildOptions constructs claude.Option slice from Config.
func (p *Provider) buildOptions() []claude.Option {
	var opts []claude.Option

	if p.config.BinaryPath != "" {
		opts = append(opts, claude.WithCLIPath(p.config.BinaryPath))
	}
	if p.config.WorkDir != "" {
		opts = append(opts, claude.WithCwd(p.config.WorkDir))
	}
	for k, v := range p.config.EnvVars {
		opts = append(opts, claude.WithEnv(k, v))
	}
	if len(p.config.AllowedTools) > 0 {
		opts = append(opts, claude.WithAllowedTools(p.config.AllowedTools...))
	}
	if p.config.AppendSystemPrompt != "" {
		opts = append(opts, claude.WithAppendSystemPrompt(p.config.AppendSystemPrompt))
	}
	if p.config.MaxThinkingTokens > 0 {
		opts = append(opts, claude.WithMaxThinkingTokens(p.config.MaxThinkingTokens))
	}

	// Claude CLI requires --verbose when using --print --output-format=stream-json.
	opts = append(opts, claude.WithVerbose(true))

	// Enable token-level streaming so the TUI gets incremental text/thinking
	// deltas instead of waiting for the full response.
	opts = append(opts, claude.WithIncludePartialMessages(true))

	// Capture stderr — only surface errors/warnings, not routine verbose output.
	opts = append(opts, claude.WithStderrCallback(func(line string) {
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "Warning:") {
			log.Printf("claudecli: %s", line)
		}
	}))

	// Set up the tool approval callback.
	opts = append(opts, claude.WithCanUseTool(p.canUseTool))

	return opts
}

// canUseTool evaluates approval rules and returns "allow" or "deny".
func (p *Provider) canUseTool(toolName string, input map[string]any) (string, error) {
	for _, rule := range p.config.ApprovalRules {
		if rule.ToolName != "" && rule.ToolName != toolName {
			continue
		}

		// Check deny commands first (Bash tool).
		if toolName == "Bash" {
			cmd, _ := input["command"].(string)
			if cmd != "" {
				firstToken := firstWord(cmd)
				for _, deny := range rule.DenyCmds {
					if strings.HasPrefix(firstToken, deny) {
						return "deny", nil
					}
				}
				for _, allow := range rule.AllowCmds {
					if strings.HasPrefix(firstToken, allow) {
						return "allow", nil
					}
				}
			}
		}

		// Check path patterns (file tools).
		if filePath, ok := input["file_path"].(string); ok && len(rule.AllowPaths) > 0 {
			// Normalize path separators for cross-platform matching.
			normalized := filepath.ToSlash(filePath)
			for _, pattern := range rule.AllowPaths {
				if matchPath(pattern, normalized) {
					return "allow", nil
				}
			}
			return "deny", nil
		}

		if rule.DefaultAllow {
			return "allow", nil
		}
	}

	// Default: allow everything.
	return "allow", nil
}

// messageToResponses converts a claude.Message to one or more model.LLMResponse values.
// Thinking blocks are emitted as separate responses with Role "thinking" so the TUI
// can render them with the collapsible thinking UI. Returns nil for messages that
// should be skipped (e.g. SystemMessage).
func (p *Provider) messageToResponses(msg claude.Message) []*model.LLMResponse {
	switch m := msg.(type) {
	case *claude.AssistantMessage:
		return p.assistantToResponses(m)
	case *claude.UserMessage:
		if r := p.userToolResultToResponse(m); r != nil {
			return []*model.LLMResponse{r}
		}
		return nil
	case *claude.ResultMessage:
		if r := p.resultToResponse(m); r != nil {
			return []*model.LLMResponse{r}
		}
		return nil
	case *claude.SystemMessage:
		// Skip system init messages.
		return nil
	case *claude.StreamEvent:
		return p.streamEventToResponses(m)
	default:
		return nil
	}
}

// assistantToResponses converts an assistant message's content blocks to LLM responses.
// Thinking blocks are emitted as separate responses with Role "thinking" so the TUI
// renders them with the collapsible thinking UI, matching the Anthropic provider behavior.
//
// When includePartialMessages is enabled, text and thinking content has already been
// streamed via StreamEvent deltas. In that case we skip text/thinking blocks here to
// avoid duplication, but still emit tool use/result blocks which aren't streamed.
func (p *Provider) assistantToResponses(m *claude.AssistantMessage) []*model.LLMResponse {
	if m.Message == nil {
		return nil
	}

	// If we already streamed text/thinking via deltas, skip those block types
	// from the final AssistantMessage to avoid duplicate content.
	skipTextAndThinking := p.sawStreamDelta

	var responses []*model.LLMResponse
	var parts []*genai.Part

	for _, block := range m.Message.Content {
		switch b := block.(type) {
		case *claude.TextBlock:
			if !skipTextAndThinking && b.Text != "" {
				parts = append(parts, genai.NewPartFromText(b.Text))
			}
		case *claude.ThinkingBlock:
			if !skipTextAndThinking && b.Thinking != "" {
				responses = append(responses, &model.LLMResponse{
					Content: &genai.Content{
						Role:  "thinking",
						Parts: []*genai.Part{genai.NewPartFromText(b.Thinking)},
					},
					Partial: true,
				})
			}
		case *claude.ToolUseBlock:
			fc := genai.NewPartFromFunctionCall(b.Name, b.Input)
			fc.FunctionCall.ID = b.ID
			parts = append(parts, fc)
			// Track tool ID → name for matching results.
			if p.toolNames == nil {
				p.toolNames = make(map[string]string)
			}
			p.toolNames[b.ID] = b.Name
		case *claude.ToolResultBlock:
			// Skip tool results in assistant messages — they come through
			// UserMessage which is handled by userToolResultToResponse.
			continue
		}
	}

	if len(parts) > 0 {
		responses = append(responses, &model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: parts,
			},
			Partial: true, // more messages may follow
		})
	}

	return responses
}

// streamEventToResponses converts a StreamEvent (token-level delta) to an LLM response.
// Only emitted when WithIncludePartialMessages is enabled. Extracts text_delta and
// thinking_delta from content_block_delta events.
func (p *Provider) streamEventToResponses(m *claude.StreamEvent) []*model.LLMResponse {
	evt := m.Event
	if evt == nil {
		return nil
	}

	// We only care about content_block_delta events.
	evtType, _ := evt["type"].(string)
	if evtType != "content_block_delta" {
		return nil
	}

	delta, ok := evt["delta"].(map[string]any)
	if !ok {
		return nil
	}

	deltaType, _ := delta["type"].(string)
	switch deltaType {
	case "text_delta":
		text, _ := delta["text"].(string)
		if text == "" {
			return nil
		}
		p.sawStreamDelta = true
		return []*model.LLMResponse{{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{genai.NewPartFromText(text)},
			},
			Partial: true,
		}}
	case "thinking_delta":
		text, _ := delta["thinking"].(string)
		if text == "" {
			return nil
		}
		p.sawStreamDelta = true
		return []*model.LLMResponse{{
			Content: &genai.Content{
				Role:  "thinking",
				Parts: []*genai.Part{genai.NewPartFromText(text)},
			},
			Partial: true,
		}}
	default:
		// input_json_delta, etc. — skip.
		return nil
	}
}

// userToolResultToResponse converts a user message (tool results) to FunctionResponse parts.
func (p *Provider) userToolResultToResponse(m *claude.UserMessage) *model.LLMResponse {
	if m.Message == nil {
		return nil
	}

	var parts []*genai.Part
	for _, block := range m.Message.Content {
		switch b := block.(type) {
		case *claude.ToolResultBlock:
			resp := p.toolResultToFunctionResponse(b)
			if resp != nil {
				parts = append(parts, resp)
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		Partial: true,
	}
}

// toolResultToFunctionResponse converts a Claude ToolResultBlock to a genai FunctionResponse part.
func (p *Provider) toolResultToFunctionResponse(b *claude.ToolResultBlock) *genai.Part {
	// Build the response map from the content.
	response := map[string]any{}
	switch c := b.Content.(type) {
	case string:
		response["content"] = c
	default:
		response["content"] = fmt.Sprintf("%v", c)
	}
	if b.IsError {
		response["is_error"] = true
	}

	// Resolve tool name from the tool_use ID.
	name := p.toolNames[b.ToolUseID]
	if name == "" {
		name = b.ToolUseID // fallback to ID if name not tracked
	}

	part := genai.NewPartFromFunctionResponse(name, response)
	part.FunctionResponse.ID = b.ToolUseID
	return part
}

// resultToResponse converts the final result message to a model.LLMResponse
// with usage metadata and completion signal.
//
// Content is ALWAYS non-nil. The ADK flow skips events with nil Content
// (base_flow.go line ~170), so a nil Content here would cause the final
// turn-complete signal to be dropped, leaving the last event as Partial=true
// and triggering "TODO: last event is not final".
func (p *Provider) resultToResponse(m *claude.ResultMessage) *model.LLMResponse {
	resp := &model.LLMResponse{
		TurnComplete: true,
		FinishReason: genai.FinishReasonStop,
	}

	// Always set Content — even if Result is empty, we need a non-nil Content
	// so the ADK flow doesn't skip this event.
	if m.Result != nil && *m.Result != "" {
		resp.Content = &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{genai.NewPartFromText(*m.Result)},
		}
	} else {
		// Empty sentinel content — the ADK flow requires non-nil Content
		// to process the turn-complete signal.
		resp.Content = &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{genai.NewPartFromText("")},
		}
	}

	// Map usage metadata.
	if m.Usage != nil {
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(m.Usage.InputTokens),
			CandidatesTokenCount: int32(m.Usage.OutputTokens),
			TotalTokenCount:      int32(m.Usage.InputTokens + m.Usage.OutputTokens),
		}
	}

	// Store cost in custom metadata.
	if m.TotalCostUSD != nil {
		resp.CustomMetadata = map[string]any{
			"total_cost_usd":  *m.TotalCostUSD,
			"num_turns":       m.NumTurns,
			"duration_ms":     m.DurationMs,
			"duration_api_ms": m.DurationAPIMs,
		}
		if m.Subtype != claude.ResultSuccess {
			resp.CustomMetadata["result_subtype"] = string(m.Subtype)
		}
	}

	// Map error results.
	if m.IsError {
		resp.FinishReason = genai.FinishReasonOther
		resp.ErrorCode = "CLI_ERROR"
		msg := fmt.Sprintf("Claude CLI error: %s", m.Subtype)
		if m.Result != nil && strings.TrimSpace(*m.Result) != "" {
			msg = strings.TrimSpace(*m.Result)
		}
		resp.ErrorMessage = llmutil.ResponseErrorText("CLI_ERROR", msg)
		resp.Content = genai.NewContentFromText(llmutil.ResponseErrorDisplayText("CLI_ERROR", resp.ErrorMessage), genai.RoleModel)
	}

	return resp
}

// extractUserMessage finds the last user message text from ADK request contents.
func extractUserMessage(req *model.LLMRequest) string {
	if req == nil || len(req.Contents) == 0 {
		return ""
	}
	// Walk backwards to find the last user content.
	for i := len(req.Contents) - 1; i >= 0; i-- {
		c := req.Contents[i]
		if c.Role != "user" {
			continue
		}
		var texts []string
		for _, part := range c.Parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return ""
}

// matchPath matches a glob pattern against a forward-slash-normalized path.
// Supports ** as a recursive wildcard (matches zero or more path segments).
// Uses filepath.Match for single-segment patterns.
func matchPath(pattern, path string) bool {
	// Normalize separators.
	pattern = filepath.ToSlash(pattern)

	// Strip leading ./ from both pattern and path for consistent matching.
	pattern = strings.TrimPrefix(pattern, "./")
	path = strings.TrimPrefix(path, "./")

	// Handle ** by splitting into segments.
	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := strings.TrimLeft(parts[1], "/")

		if prefix != "" && !strings.HasPrefix(path, prefix) {
			return false
		}

		remaining := strings.TrimPrefix(path, prefix)

		if suffix == "" {
			return true // "dir/**" matches everything under dir/
		}

		// Try matching the suffix against every tail of the remaining path.
		segments := strings.Split(remaining, "/")
		for i := range segments {
			candidate := strings.Join(segments[i:], "/")
			if matched, _ := filepath.Match(suffix, candidate); matched {
				return true
			}
		}
		// Try matching just the base name against the suffix.
		if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
			return true
		}
		return false
	}

	matched, _ := filepath.Match(pattern, path)
	return matched
}

// firstWord returns the first whitespace-delimited token from s.
func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// FindBinary locates the claude CLI binary by checking, in order:
//  1. $CLAUDE_CLI_PATH environment variable (explicit override)
//  2. System PATH (exec.LookPath)
//  3. Platform-specific well-known install locations
//
// On Windows, the native installer places claude.exe at %USERPROFILE%\.local\bin\.
// On macOS/Linux, it may be at ~/.local/bin/ or ~/.claude/local/.
func FindBinary() (string, error) {
	// 1. Explicit override.
	if path, ok := lookupEnv("CLAUDE_CLI_PATH"); ok && path != "" {
		if _, err := statFile(path); err == nil {
			return path, nil
		}
	}

	// 2. System PATH.
	if path, err := lookPath("claude"); err == nil {
		return path, nil
	}

	// 3. Well-known install locations.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("claude CLI not found in PATH and cannot determine home directory: %w", err)
	}

	candidates := wellKnownPaths(home)
	for _, candidate := range candidates {
		if _, err := statFile(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("claude CLI not found (searched: PATH, %s); install from https://code.claude.com or set CLAUDE_CLI_PATH",
		strings.Join(candidates, ", "))
}

// wellKnownPaths returns platform-specific paths where Claude CLI may be installed.
func wellKnownPaths(home string) []string {
	var paths []string

	if isWindows() {
		// Native Windows installer: %USERPROFILE%\.local\bin\claude.exe
		paths = append(paths, filepath.Join(home, ".local", "bin", "claude.exe"))
		// npm global on Windows: %APPDATA%\npm\claude.cmd
		if appData, ok := lookupEnv("APPDATA"); ok {
			paths = append(paths, filepath.Join(appData, "npm", "claude.cmd"))
		}
	} else {
		// Native installer (macOS/Linux): ~/.local/bin/claude
		paths = append(paths, filepath.Join(home, ".local", "bin", "claude"))
		// Claude's own local directory
		paths = append(paths, filepath.Join(home, ".claude", "local", "claude"))
	}

	return paths
}

// isWindows reports whether the current OS is Windows.
// Extracted as a var for testing.
var isWindows = func() bool {
	return os.PathSeparator == '\\'
}

// lookupEnv is a variable for testing.
var lookupEnv = os.LookupEnv

// lookPath is exec.LookPath, extracted for testing.
var lookPath = exec.LookPath

// statFile is os.Stat, extracted for testing.
var statFile = func(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

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

	claude "github.com/partio-io/claude-agent-sdk-go"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
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

	// VerboseTools shows full tool input/output in the text stream
	// instead of one-line summaries.
	VerboseTools bool

	// AppendSystemPrompt appends text to Claude CLI's default system prompt.
	// This is the recommended way to inject go-pi context since the provider
	// cannot replace Claude CLI's system prompt entirely.
	AppendSystemPrompt string

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
	config  Config
	session *claude.Session
	mu      sync.Mutex
	closed  bool

	// warnedSystemPrompt tracks whether we've logged the system prompt warning.
	warnedSystemPrompt bool
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
		// Warn once if system instruction is set but ignored.
		if req.Config != nil && req.Config.SystemInstruction != nil && !p.warnedSystemPrompt {
			p.warnedSystemPrompt = true
			log.Println("claudecli: system instruction from ADK is not forwarded; Claude CLI uses its own agent prompt")
		}

		// Extract user message text from the request.
		userText := extractUserMessage(req)
		if userText == "" {
			yield(nil, fmt.Errorf("claudecli: no user message in request"))
			return
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			yield(nil, fmt.Errorf("claudecli: provider is closed"))
			return
		}

		// Ensure session is started.
		if p.session == nil {
			p.session = claude.NewSession(p.buildOptions()...)
		}
		session := p.session
		p.mu.Unlock()

		// Send the user message.
		if err := session.Send(ctx, userText); err != nil {
			yield(nil, fmt.Errorf("claudecli: send: %w", err))
			return
		}

		// Stream responses.
		for msg, err := range session.Stream(ctx) {
			if err != nil {
				yield(nil, fmt.Errorf("claudecli: stream: %w", err))
				return
			}

			resp := p.messageToResponse(msg)
			if resp == nil {
				continue // skip system messages, etc.
			}

			if !yield(resp, nil) {
				return
			}
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

// messageToResponse converts a claude.Message to a model.LLMResponse.
// Returns nil for messages that should be skipped (e.g. SystemMessage).
func (p *Provider) messageToResponse(msg claude.Message) *model.LLMResponse {
	switch m := msg.(type) {
	case *claude.AssistantMessage:
		return p.assistantToResponse(m)
	case *claude.UserMessage:
		return p.userToolResultToResponse(m)
	case *claude.ResultMessage:
		return p.resultToResponse(m)
	case *claude.SystemMessage:
		// Skip system init messages.
		return nil
	case *claude.StreamEvent:
		// Skip raw stream events.
		return nil
	default:
		return nil
	}
}

// assistantToResponse converts an assistant message's content blocks to an LLM response.
func (p *Provider) assistantToResponse(m *claude.AssistantMessage) *model.LLMResponse {
	if m.Message == nil {
		return nil
	}

	var parts []*genai.Part
	for _, block := range m.Message.Content {
		switch b := block.(type) {
		case *claude.TextBlock:
			if b.Text != "" {
				parts = append(parts, genai.NewPartFromText(b.Text))
			}
		case *claude.ThinkingBlock:
			if b.Thinking != "" {
				parts = append(parts, genai.NewPartFromText(
					fmt.Sprintf("[thinking]\n%s\n[/thinking]", b.Thinking),
				))
			}
		case *claude.ToolUseBlock:
			parts = append(parts, genai.NewPartFromText(
				p.formatToolUse(b),
			))
		case *claude.ToolResultBlock:
			parts = append(parts, genai.NewPartFromText(
				p.formatToolResult(b),
			))
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
		Partial: true, // more messages may follow
	}
}

// userToolResultToResponse converts a user message (tool results) to text.
func (p *Provider) userToolResultToResponse(m *claude.UserMessage) *model.LLMResponse {
	if m.Message == nil {
		return nil
	}

	var parts []*genai.Part
	for _, block := range m.Message.Content {
		switch b := block.(type) {
		case *claude.ToolResultBlock:
			parts = append(parts, genai.NewPartFromText(
				p.formatToolResult(b),
			))
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

// resultToResponse converts the final result message to a model.LLMResponse
// with usage metadata and completion signal.
func (p *Provider) resultToResponse(m *claude.ResultMessage) *model.LLMResponse {
	resp := &model.LLMResponse{
		TurnComplete: true,
		FinishReason: genai.FinishReasonStop,
	}

	// Add the final result text if present.
	if m.Result != nil && *m.Result != "" {
		resp.Content = &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{genai.NewPartFromText(*m.Result)},
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
			"total_cost_usd": *m.TotalCostUSD,
			"num_turns":      m.NumTurns,
			"duration_ms":    m.DurationMs,
			"duration_api_ms": m.DurationAPIMs,
		}
		if m.Subtype != claude.ResultSuccess {
			resp.CustomMetadata["result_subtype"] = string(m.Subtype)
		}
	}

	// Map error results.
	if m.IsError {
		resp.FinishReason = genai.FinishReasonOther
		resp.ErrorMessage = fmt.Sprintf("Claude CLI error: %s", m.Subtype)
	}

	return resp
}

// formatToolUse formats a ToolUseBlock as structured text.
func (p *Provider) formatToolUse(b *claude.ToolUseBlock) string {
	if p.config.VerboseTools {
		return fmt.Sprintf("[tool:%s] %v", b.Name, b.Input)
	}
	summary := toolUseSummary(b)
	return fmt.Sprintf("[tool:%s] %s", b.Name, summary)
}

// formatToolResult formats a ToolResultBlock as structured text.
func (p *Provider) formatToolResult(b *claude.ToolResultBlock) string {
	prefix := "[tool-result]"
	if b.IsError {
		prefix = "[tool-error]"
	}

	if p.config.VerboseTools {
		return fmt.Sprintf("%s %v", prefix, b.Content)
	}

	// Truncate content for summary.
	content := fmt.Sprintf("%v", b.Content)
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	return fmt.Sprintf("%s %s", prefix, content)
}

// toolUseSummary produces a one-line summary of tool input.
func toolUseSummary(b *claude.ToolUseBlock) string {
	switch b.Name {
	case "Read":
		if fp, ok := b.Input["file_path"].(string); ok {
			return fp
		}
	case "Write":
		if fp, ok := b.Input["file_path"].(string); ok {
			return fp
		}
	case "Edit":
		if fp, ok := b.Input["file_path"].(string); ok {
			return fp
		}
	case "Bash":
		if cmd, ok := b.Input["command"].(string); ok {
			if len(cmd) > 80 {
				return cmd[:80] + "..."
			}
			return cmd
		}
	case "Grep", "Glob":
		if pattern, ok := b.Input["pattern"].(string); ok {
			return pattern
		}
	}
	return fmt.Sprintf("%v", b.Input)
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

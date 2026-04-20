# Claude CLI Provider — Design Spec

**Date:** 2026-04-06
**Status:** Draft
**Branch:** feat/go-pi-reboot

## Summary

Add a new `claudecli` provider family to go-pi that implements `model.LLM` by
wrapping the `partio-io/claude-agent-sdk-go` SDK. The SDK manages the Claude
Code CLI subprocess, NDJSON protocol, and tool approval callbacks. go-pi only
needs to implement the `model.LLM` adapter and provider registration.

## Motivation

Claude Code CLI ships with a rich, battle-tested toolset (Read, Write, Edit,
Bash, etc.) and a headless NDJSON protocol. By wrapping it as a go-pi provider:

- Full Claude Code toolset without re-implementing tools
- Claude Code's built-in safety and permission system
- A path toward child-process orchestration (spawning multiple CLI workers)
- Compatibility with the existing go-pi agent runner, TUI, and session system

## Key Dependency

**`github.com/partio-io/claude-agent-sdk-go`** — a zero-dependency Go SDK that
wraps Claude Code CLI as a subprocess, handling:

- Process lifecycle (spawn, stdin/stdout NDJSON, restart)
- Typed message blocks (`TextBlock`, `ThinkingBlock`, `ToolUseBlock`, `ToolResultBlock`)
- Tool approval via `WithCanUseTool` callback
- Tool allowlists via `WithAllowedTools` (glob support)
- Multi-turn sessions via `WithContinueConversation`
- Working directory, binary path, env vars, hooks, MCP servers

This eliminates the need for custom protocol, process, and approval layers.

## Architecture

### Package Structure

```
internal/
  claudecli/
    provider.go      — model.LLM implementation wrapping the SDK
    provider_test.go — unit tests
```

That's it. Protocol types, process management, and approval handling are all
provided by the SDK.

### Provider (`provider.go`)

Implements `model.LLM` from ADK:

```go
package claudecli

import (
    "context"
    "iter"

    claude "github.com/partio-io/claude-agent-sdk-go"
    "google.golang.org/adk/model"
    "google.golang.org/genai"
)

// Config holds configuration for the Claude CLI provider.
type Config struct {
    BinaryPath   string            // path to `claude` binary; empty = resolve from PATH
    WorkDir      string            // working directory for the CLI process
    EnvVars      map[string]string // additional env vars
    AllowedTools []string          // tool glob patterns to auto-approve
    VerboseTools bool              // show full tool input/output in stream
}

// Provider implements model.LLM by delegating to Claude Code CLI.
type Provider struct {
    config Config
}

func New(cfg Config) *Provider {
    return &Provider{config: cfg}
}

func (p *Provider) Name() string {
    return "claudecli"
}
```

**`GenerateContent(ctx, req, stream) iter.Seq2[*model.LLMResponse, error]`:**

1. Build SDK options from `Config`:
   - `claude.WithCLIPath(cfg.BinaryPath)` if set
   - `claude.WithCwd(cfg.WorkDir)` if set
   - `claude.WithAllowedTools(cfg.AllowedTools...)` if set
   - `claude.WithCanUseTool(approvalCallback)` for layered policy
   - `claude.WithContinueConversation(true)` for multi-turn
   - `claude.WithEnv(k, v)` for each env var

2. Extract the user message text from `req.Contents` (last user content)

3. Call `claude.Prompt(ctx, userMessage, options...)` — returns a channel of
   typed messages

4. Iterate over messages, yielding `model.LLMResponse` events:
   - `*claude.AssistantMessage` → iterate content blocks:
     - `*claude.TextBlock` → yield as `model.LLMResponse` with text `genai.Part`
     - `*claude.ToolUseBlock` → yield as structured text:
       `[tool:<Name>] <summary>`
     - `*claude.ToolResultBlock` → yield as structured text:
       `[tool-result:<Name>] <truncated output>`
     - `*claude.ThinkingBlock` → yield as text (prefixed with thinking marker)
   - `*claude.ResultMessage` → final response; extract cost/token data,
     set `FinishReason: Stop`, yield final `model.LLMResponse`
   - Error from SDK → yield as error in the iterator

5. System instruction from `req.Config.SystemInstruction` is logged as a
   warning on first call but NOT forwarded — Claude CLI manages its own
   agent prompt. If the SDK supports `WithAppendSystemPrompt`, we can
   optionally forward it there.

**Streaming:** The SDK always streams NDJSON. The `stream` parameter from ADK
is accepted but has no effect — both streaming and non-streaming callers
receive the same iterator.

**Tool calls in ADK terms:** The provider does NOT emit `genai.Part` with
`FunctionCall`. All tool activity is flattened to structured text. ADK sees a
model that thinks for a while and returns text.

### Tool Approval

The SDK's `WithCanUseTool` callback provides the hook point for our layered
approval strategy:

```go
claude.WithCanUseTool(func(ctx context.Context, tool string, input map[string]any) (any, error) {
    // 1. Check policy rules from config
    decision := p.checkPolicy(tool, input)
    if decision != nil {
        return decision, nil
    }
    // 2. Fall back to auto-approve (or TUI callback in future)
    return &claude.PermissionResultAllow{}, nil
})
```

The SDK handles sending the `control_response` back to the CLI process —
we just return a decision struct.

### Tool Output Formatting

Tool activity is formatted as structured text blocks for TUI parseability:

```
[tool:Read] path/to/file.go
[tool:Bash] git status
[tool-result:Read] (248 lines)
[tool-result:Bash] exit=0 (truncated to 5 lines)
```

When `VerboseTools` is true, full tool input/output is included instead of
one-line summaries.

### Provider Registration

Uses the existing `RegistryDocument` / `Definition` API. The `claude` prefix is
already claimed by the `anthropic` provider, so Claude CLI uses the `cli/`
prefix to avoid collision.

In `AddBuiltins()` (or via a discoverable `models/*.json` file):

```go
{
    Name:         "claudecli",
    Family:       "claudecli",
    // No API key needed — CLI handles its own auth.
    Match:        []MatchRule{{Prefix: "cli/", StripPrefix: true}},
}
```

Plus model aliases for convenience:

```go
Models: []ModelDefinition{
    {Name: "claude-cli",  Provider: "claudecli", Target: "claude-cli"},
    {Name: "claude-code", Provider: "claudecli", Target: "claude-cli"},
},
```

Usage: `--model cli/claude` or `--model claude-cli` or `--provider claudecli`.

In `internal/provider/provider.go` `NewLLM`, the new family case:

```go
case "claudecli":
    cwd, _ := os.Getwd()
    return claudecli.New(claudecli.Config{
        BinaryPath: findClaudeBinary(),
        WorkDir:    cwd,
    }), nil
```

Note: `findClaudeBinary()` checks `$CLAUDE_CLI_PATH` env var, then
`exec.LookPath("claude")`.

### Configuration

In `~/.go-pi/config.json`:

```json
{
  "claudecli": {
    "binary": "/usr/local/bin/claude",
    "verbose_tools": false,
    "allowed_tools": ["Read", "Write", "Edit", "Bash", "Grep", "Glob"],
    "approval": {
      "rules": [
        {"tool_name": "Bash", "deny_commands": ["rm -rf /", "sudo"]},
        {"tool_name": "Write", "allow_paths": ["./**"]}
      ],
      "fallback": "auto"
    }
  }
}
```

**Policy matching details:**
- `allowed_tools` are passed to `claude.WithAllowedTools` — auto-approved by
  the SDK without hitting the callback
- `approval.rules` are evaluated in the `WithCanUseTool` callback:
  - `allow_paths` uses Go's `filepath.Match` glob, resolved relative to WorkDir
  - `deny_commands` uses first-token prefix matching on the Bash command string
  - Rules evaluated in order; first match wins
  - `fallback` applies when no rule matches: `"auto"` (approve) or `"prompt"`
    (future TUI integration)

## Data Flow

```
┌──────────────────────────────────────────────────────────┐
│ go-pi TUI                                                 │
│                                                           │
│  User types message                                       │
│       ↓                                                   │
│  ADK Runner.Run(sessionID, message)                       │
│       ↓                                                   │
│  Provider.GenerateContent(req)                            │
│       ↓                                                   │
│  claude.Prompt(ctx, msg, opts...)                          │
│       ↓                                                   │
│  ┌─────────────────────────────────────────────┐          │
│  │ Claude CLI Process (managed by SDK)         │          │
│  │                                             │          │
│  │  [Claude thinks, calls tools internally]    │          │
│  │                                             │          │
│  │  SDK handles NDJSON, control_request/resp   │          │
│  └─────────────────────────────────────────────┘          │
│       ↓                                                   │
│  AssistantMessage / ResultMessage channels                 │
│       ↓                                                   │
│  Provider maps to model.LLMResponse                       │
│       ↓                                                   │
│  ADK session stores text response                         │
│       ↓                                                   │
│  TUI renders markdown                                     │
└──────────────────────────────────────────────────────────┘
```

## Testing Strategy

1. **Provider unit tests** — mock the SDK's `Prompt` function (or use an
   interface wrapper) to test `GenerateContent` produces correct
   `LLMResponse` streams for:
   - Simple text response
   - Response with tool call blocks (verify structured text output)
   - Thinking blocks
   - Error handling (SDK errors, context cancellation)
   - Cost/token metadata from ResultMessage

2. **Approval policy tests** — unit test the `checkPolicy` function directly:
   - deny_commands matching
   - allow_paths glob matching
   - Rule ordering (first match wins)
   - Fallback behavior

3. **Integration test** — optional, requires `claude` CLI installed. Sends a
   simple prompt, verifies a response comes back. Gated behind
   `-tags integration` or `CLAUDE_CLI_PATH` env var.

## Scope & Non-Goals

**In scope:**
- `model.LLM` adapter wrapping `partio-io/claude-agent-sdk-go`
- Provider registration in the registry
- Basic policy-based approval config
- Tool output formatting

**Not in scope (future work):**
- Multi-instance orchestration (child process manager)
- TUI approval UI (callback handler exists, wiring is future)
- Cost tracking aggregation
- Claude CLI auto-install or version detection
- Inbound protocol compatibility (go-pi speaking the protocol as a server)
- Intercepting tool calls for go-pi's own sandboxed tools

## Decisions

1. **ADK system prompt:** Ignored by default. Claude CLI manages its own prompt.
   If `WithAppendSystemPrompt` is available in the SDK, optionally forward
   go-pi's system instruction there. Logged as a warning on first call.

2. **Session identity:** Independent. go-pi and CLI maintain separate sessions.
   Future work may use `WithContinueConversation` or `--session-id` to align.

3. **Tool output:** Structured `[tool:Name]` prefix convention for TUI
   parseability. Configurable verbosity via `verbose_tools`.

## Open Questions

1. **SDK API stability:** The partio-io SDK is community-maintained. If the API
   changes or the project becomes unmaintained, we'd need to fork or revert to
   our own protocol layer. Mitigated by the thin adapter pattern — only
   `provider.go` depends on it.

2. **Concurrent turns:** Does the SDK support multiple concurrent `Prompt`
   calls on the same CLI process? If not, the provider's mutex serialization
   is correct. Need to verify.

3. **`Name() string` method:** Need to verify whether `model.LLM` requires
   this method. If so, return `"claudecli"`.

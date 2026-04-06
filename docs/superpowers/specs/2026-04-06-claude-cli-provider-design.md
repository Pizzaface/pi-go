# Claude CLI Provider — Design Spec

**Date:** 2026-04-06
**Status:** Draft
**Branch:** feat/go-pi-reboot

## Summary

Add a new `claudecli` provider family to go-pi that implements `model.LLM` by
spawning a long-lived Claude Code CLI process and communicating via the NDJSON
stream-json protocol. Claude CLI handles its own tool execution internally
(pass-through mode); go-pi streams the output to the TUI and manages tool
approval requests via a layered policy + callback pipeline.

## Motivation

Claude Code CLI ships with a rich, battle-tested toolset (Read, Write, Edit,
Bash, etc.) and a headless NDJSON protocol. By wrapping it as a go-pi provider,
we get:

- Full Claude Code toolset without re-implementing tools
- Claude Code's built-in safety and permission system
- A path toward child-process orchestration (spawning multiple CLI workers)
- Compatibility with the existing go-pi agent runner, TUI, and session system

## Architecture

### Package Structure

```
internal/
  claudecli/
    protocol.go      — NDJSON message types and marshaling
    process.go       — subprocess lifecycle management
    approval.go      — ApprovalHandler interface and implementations
    provider.go      — model.LLM implementation
    provider_test.go — unit tests with mock process
```

### Protocol Layer (`protocol.go`)

Go structs for every NDJSON message type exchanged with Claude CLI.

#### Outbound Messages (go-pi → Claude CLI stdin)

```go
// UserMessage is sent to Claude CLI to deliver a user prompt.
type UserMessage struct {
    Type    string        `json:"type"`    // always "user"
    Message MessageBody   `json:"message"`
}

type MessageBody struct {
    Role    string        `json:"role"`    // always "user"
    Content []ContentPart `json:"content"`
}

type ContentPart struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"`
}

// ControlResponse is sent back to Claude CLI after a control_request.
type ControlResponse struct {
    Type     string               `json:"type"` // always "control_response"
    Response ControlResponseBody  `json:"response"`
}

type ControlResponseBody struct {
    Subtype   string                    `json:"subtype"`    // "success"
    RequestID string                    `json:"request_id"`
    Response  *ControlResponseDecision  `json:"response,omitempty"` // for can_use_tool
}

type ControlResponseDecision struct {
    Behavior     string          `json:"behavior"` // "allow" or "deny"
    UpdatedInput json.RawMessage `json:"updatedInput,omitempty"` // required for "allow"
    Message      string          `json:"message,omitempty"`      // required for "deny"
}
```

#### Inbound Messages (Claude CLI stdout → go-pi)

```go
// StdoutMessage is the generic envelope read from Claude CLI stdout.
// The Type field determines which concrete fields are populated.
type StdoutMessage struct {
    Type string `json:"type"`

    // system fields
    Model     string   `json:"model,omitempty"`
    Tools     []string `json:"tools,omitempty"`
    SessionID string   `json:"session_id,omitempty"`
    Cwd       string   `json:"cwd,omitempty"`

    // assistant / user fields (tool results)
    Message json.RawMessage `json:"message,omitempty"`

    // result fields
    TotalCostUSD      float64  `json:"total_cost_usd,omitempty"`
    InputTokens       int      `json:"input_tokens,omitempty"`
    OutputTokens      int      `json:"output_tokens,omitempty"`
    PermissionDenials []string `json:"permission_denials,omitempty"`

    // control_request fields
    RequestID string              `json:"request_id,omitempty"`
    Request   *ControlRequestBody `json:"request,omitempty"`
}

type ControlRequestBody struct {
    Subtype        string          `json:"subtype"`         // "can_use_tool"
    ToolName       string          `json:"tool_name"`
    Input          json.RawMessage `json:"input"`
    DecisionReason string          `json:"decision_reason"`
    ToolUseID      string          `json:"tool_use_id"`
}
```

### Process Management (`process.go`)

Manages a single Claude CLI subprocess.

```go
type Process struct {
    cmd       *exec.Cmd
    stdin     io.WriteCloser
    scanner   *bufio.Scanner  // line-delimited JSON from stdout
    stderrBuf *ringBuffer     // bounded ring buffer (last 64KB) for diagnostics
    mu        sync.Mutex      // serializes writes to stdin
    done      chan struct{}    // closed when process exits
    exitErr   error
}

type ProcessConfig struct {
    BinaryPath string   // path to `claude` binary; default: look up in PATH
    WorkDir    string   // working directory for the CLI process
    EnvVars    []string // additional env vars (e.g. ANTHROPIC_API_KEY)
}
```

**Lifecycle:**
1. `NewProcess(cfg ProcessConfig) (*Process, error)` — spawns `claude` with
   `--output-format stream-json --input-format stream-json --verbose --permission-prompt-tool stdio`.
   Configures `scanner.Buffer()` with 1MB max line size to handle large tool outputs.
2. `Send(msg any) error` — JSON-encodes + newline to stdin (mutex-protected)
3. `Recv() (StdoutMessage, error)` — reads next NDJSON line from stdout
4. `Close() error` — SIGTERM, wait 5s, SIGKILL if needed
5. If `Recv()` returns `io.EOF` or the process exits, the caller can call
   `NewProcess` again to restart
6. Stderr is drained by a background goroutine into a bounded `ringBuffer`
   (last 64KB). On process death, `LastStderr() string` returns the tail
   for diagnostics.

**No automatic restart.** The provider decides when to restart (on next
`GenerateContent` call if the process is dead). This keeps the process layer
simple and testable.

### Tool Approval Pipeline (`approval.go`)

```go
// ApprovalHandler decides whether to allow or deny a tool use request.
type ApprovalHandler interface {
    Handle(ctx context.Context, req ControlRequestBody) (ControlResponseDecision, error)
}
```

**Implementations:**

1. **`AutoApproveHandler`** — always returns `{behavior: "allow", updatedInput: req.Input}`

2. **`PolicyHandler`** — evaluates configurable rules:
   ```go
   type PolicyRule struct {
       ToolName    string   // exact match, e.g. "Bash"
       AllowPaths  []string // glob patterns for file tools
       AllowCmds   []string // prefix patterns for Bash commands
       DenyCmds    []string // prefix patterns to deny (checked first)
       Action      string   // "allow" or "deny"; default "allow" if patterns match
   }
   ```
   - Rules are evaluated in order; first match wins
   - If no rule matches, returns a sentinel `ErrNoMatch`

3. **`CallbackHandler`** — wraps a `func(context.Context, ControlRequestBody) (ControlResponseDecision, error)` for TUI integration

4. **`ChainHandler`** — tries handlers in order; if one returns `ErrNoMatch`, tries the next. Default chain: `PolicyHandler → CallbackHandler`

### Provider (`provider.go`)

Implements `model.LLM` from ADK:

```go
type Provider struct {
    process  *Process
    config   ProcessConfig
    approval ApprovalHandler
    mu       sync.Mutex // serializes GenerateContent calls
}

func New(cfg ProcessConfig, approval ApprovalHandler) (*Provider, error)
```

**`GenerateContent(ctx, req, stream) iter.Seq2[*model.LLMResponse, error]`:**

1. Ensure process is alive (lazy start or restart if dead)
2. Translate `model.LLMRequest` → `UserMessage`:
   - Extract last user message text from `req.Contents`
   - System instruction: ignored (Claude CLI has its own; go-pi's system prompt
     is not forwarded because the CLI manages its own agent loop)
3. Send `UserMessage` via `Process.Send`
4. Read loop on `Process.Recv`:
   - `type: "system"` → log, skip (first turn only)
   - `type: "assistant"` → parse content parts:
     - Text blocks → yield as `model.LLMResponse` with text `genai.Part`
     - Tool-use blocks → yield as structured text delta with metadata
       (see Tool Output Formatting below)
   - `type: "user"` → tool results flowing back to Claude; yield as
     structured text with tool name and truncated output
   - `type: "control_request"` → invoke `approval.Handle()`, send
     `ControlResponse` back via `Process.Send`, resume reading
   - `type: "error"` → yield as `model.LLMResponse` error; log the
     message content. Do NOT kill the process — errors may be recoverable
     (e.g., rate limit, transient API failure)
   - Unknown types → log at debug level, skip. Never panic on unknown types.
   - `type: "result"` → final response; set `FinishReason: Stop`, attach
     usage metadata, yield final `model.LLMResponse`, return
5. On context cancellation: yield a cancellation error and return.
   Do NOT send SIGTERM — the process is long-lived and shared across turns.
   The caller (or `Provider.Close()`) handles process shutdown.

**Streaming:** Always stream. The `stream` parameter from ADK is accepted but
has no effect — Claude CLI always produces streamed NDJSON output. Both streaming
and non-streaming callers receive the same iterator. This is a deliberate design
choice: the CLI's NDJSON protocol is inherently streaming, and buffering to
simulate non-streaming would add complexity without benefit. If future ADK
versions rely on non-streaming semantics, the provider can buffer internally.

**Tool calls in ADK terms:** The provider does NOT emit `genai.Part` with
`FunctionCall` — that would cause ADK to try dispatching tools. Instead, all
tool activity is flattened to text. ADK sees a model that thinks for a while
and returns text.

**Tool Output Formatting:** Tool activity is formatted as structured text blocks
rather than raw emoji markers, to allow future TUI parsing:

```
[tool:Read] path/to/file.go
[tool:Bash] git status
[tool-result:Read] (248 lines)
[tool-result:Bash] exit=0 (truncated to 5 lines)
```

The `[tool:...]` / `[tool-result:...]` prefix convention is parseable by the
TUI if it wants to render collapsible sections, but degrades to readable text
in plain terminals. This avoids mixing presentation into the provider layer.

**Session mapping:** Each `GenerateContent` call is one user turn in the
persistent Claude CLI session. The CLI maintains its own conversation history.
go-pi's ADK session stores the text summaries of what happened.

### Provider Registration

Uses the existing `RegistryDocument` / `Definition` API. The `claude` prefix is
already claimed by the `anthropic` provider, so Claude CLI uses the `cli/`
prefix to avoid collision:

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

In `internal/provider/provider.go` `NewLLM`, the new family case passes the
working directory from the caller's context (not from `Info`, which has no
`WorkDir` field):

```go
case "claudecli":
    cwd, _ := os.Getwd()
    return claudecli.New(claudecli.ProcessConfig{
        BinaryPath: findClaudeBinary(),
        WorkDir:    cwd,
    }, claudecli.DefaultApprovalHandler())
```

Note: `findClaudeBinary()` checks `$CLAUDE_CLI_PATH` env var, then
`exec.LookPath("claude")`.

### Configuration

In `~/.pi-go/config.json`:

```json
{
  "claudecli": {
    "binary": "/usr/local/bin/claude",
    "verbose_tools": false,
    "approval": {
      "default": "auto",
      "rules": [
        {"tool_name": "Bash", "deny_commands": ["rm -rf /", "sudo"]},
        {"tool_name": "Write", "allow_paths": ["./**"]}
      ],
      "fallback": "prompt"
    }
  }
}
```

**JSON field naming:** All config JSON uses `snake_case` consistently. Go
struct fields use Go conventions with `json:"snake_case"` tags.

**Policy matching details:**
- `allow_paths` uses Go's `filepath.Match` glob syntax, resolved relative to
  the provider's `WorkDir`
- `deny_commands` / `allow_commands` use prefix matching on the full command
  string (after shell expansion by Claude CLI). `"rm"` matches `"rm -rf /"`
  but NOT `"echo rm"`. Only the first whitespace-delimited token is compared
  for prefix rules; for substring matching, prefix the pattern with `*`
- Rules are evaluated in order; first match wins. If no rule matches,
  `fallback` applies (`"auto"` or `"prompt"`)
```

- `binary` — path to `claude` binary (default: resolve from `$PATH`)
- `approval.default` — `"auto"` (approve all) or `"prompt"` (ask user)
- `approval.rules` — policy rules evaluated before the default
- `approval.fallback` — what to do when no rule matches: `"auto"` or `"prompt"`

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
│  ┌─────────────────────────────────────────────┐          │
│  │ Claude CLI Process (long-lived)             │          │
│  │                                             │          │
│  │  stdin ← UserMessage (NDJSON)               │          │
│  │                                             │          │
│  │  [Claude thinks, calls tools internally]    │          │
│  │                                             │          │
│  │  stdout → assistant messages (NDJSON)       │──→ text deltas → TUI
│  │  stdout → control_request (NDJSON)          │──→ ApprovalHandler
│  │  stdin  ← control_response (NDJSON)         │←── allow/deny
│  │  stdout → result (NDJSON)                   │──→ final response
│  └─────────────────────────────────────────────┘          │
│       ↓                                                   │
│  ADK session stores text response                         │
│       ↓                                                   │
│  TUI renders markdown                                     │
└──────────────────────────────────────────────────────────┘
```

## Testing Strategy

1. **Protocol tests** — round-trip marshal/unmarshal for every message type
2. **Process tests** — use a mock `claude` binary (a small Go program or shell
   script that speaks the NDJSON protocol) to test spawn/send/recv/close
3. **Approval tests** — unit tests for each handler: auto-approve, policy
   matching, chain fallback
4. **Provider tests** — mock the process layer, verify `GenerateContent`
   produces correct `LLMResponse` stream for various scenarios:
   - Simple text response
   - Response with tool calls (verify they appear as text deltas)
   - Control request handling (auto-approve + deny)
   - Process death and restart
   - Context cancellation

## Scope & Non-Goals

**In scope:**
- NDJSON protocol types
- Process lifecycle
- Layered approval pipeline
- `model.LLM` implementation (pass-through mode)
- Provider registration
- Basic config loading

**Not in scope (future work):**
- Multi-instance orchestration (child process manager)
- Intercept mode (go-pi dispatches tools instead of Claude CLI)
- TUI approval UI (CallbackHandler exists but TUI wiring is future)
- Cost tracking aggregation across sessions
- Claude CLI auto-install or version detection
- Inbound protocol compatibility (go-pi speaking the protocol as a server)

## Decisions (formerly Open Questions)

1. **ADK system prompt:** Ignored. Claude CLI manages its own system prompt
   and agent behavior. If `req.Config.SystemInstruction` is set, the provider
   logs a warning on first call: "claudecli provider ignores system instruction;
   Claude CLI uses its own agent prompt." This is documented as a known
   limitation in the provider's godoc and in user-facing settings docs.

2. **Session identity:** Independent. go-pi session IDs and Claude CLI session
   IDs are not aligned. The CLI process maintains its own conversation history.
   If the process dies mid-session, the CLI history is lost; go-pi's session
   retains the text summaries. The `Provider` exposes a `Healthy() bool` method
   so callers can detect process death. Future work may add CLI session
   persistence via `--session-id` flag.

3. **Tool output verbosity:** Structured text with `[tool:Name]` prefix
   convention (see Tool Output Formatting above). Default shows tool name +
   one-line summary. Configurable via `claudecli.verbose_tools: true` in config
   to show full input/output.

## Open Questions

1. **Concurrent turns:** The `Provider.mu` mutex serializes `GenerateContent`
   calls. A second call blocks until the first completes. This is correct for
   a single-process provider, but means the go-pi TUI cannot cancel a running
   turn by starting a new one. Should we add explicit cancellation support
   (e.g., writing a cancel control message, or killing/restarting the process)?
   Current design: block and wait; cancellation is future work.

2. **`Name() string` method:** If `model.LLM` requires a `Name()` method
   (the anthropic provider implements one), the provider returns
   `"claudecli:" + processConfig.BinaryPath`. Need to verify the ADK interface.

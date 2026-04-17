# Extensions: Session/UI (Spec #5)

## Summary

Spec #5 promotes the messaging, session-control, and lifecycle-hook stubs left deferred in spec #1 into working implementations. Extensions gain the ability to append entries to the running session, send user-visible messages, trigger turns, name/label sessions, drive session branching, subscribe to lifecycle events via declarative hooks, and stream partial tool results to the TUI. The existing protocol-level `MethodToolUpdate` and `MethodLog` "accept-and-drop" paths are replaced with real routing to the TUI tool display and the extension log.

This is spec #5 of a six-spec sequence. Specs #2 (commands), #3 (events/tool & model control), and #6 (renderers/flags/shortcuts/providers) are out of scope.

## Scope

In scope:

- Messaging surface: `SendMessage`, `SendUserMessage`, `AppendEntry`, `SetSessionName`/`GetSessionName`, `SetLabel`.
- CommandContext session ops: `WaitForIdle`, `NewSession`, `Fork`, `NavigateTree`, `SwitchSession`, `Reload`.
- Lifecycle hooks: promoting `HookConfig`, `LifecycleEvent*` constants, and `Runtime.RunLifecycleHooks` from stubs to real implementations, with three additional event points (`before_turn`, `after_turn`, `shutdown`).
- Routing plumbing: `MethodToolUpdate` → streaming tool-display updates; `MethodLog` → TUI trace panel plus rotating log file.

Out of scope:

- `RegisterMessageRenderer` and the `renderCustomAssistantMessage` / `renderWithExtension` TUI hooks. These remain deferred to spec #6. Spec #5 ships the default rendering path for custom messages only.
- Event-bus (`pi.Events()`), tool-execute event family, model registry, active-tools control. All spec #3.
- Commands (`pi.RegisterCommand`). Spec #2.
- Shortcuts, flags, providers. Spec #6.

## Architecture

### The `SessionBridge` seam

A new `SessionBridge` interface lives at `internal/extension/api/bridge.go` and exposes exactly the operations extensions need:

```go
type SessionBridge interface {
    // Messaging
    AppendEntry(kind string, payload any) error
    SendCustomMessage(piapi.CustomMessage, piapi.SendOptions) error
    SendUserMessage(piapi.UserMessage, piapi.SendOptions) error
    SetSessionTitle(title string) error
    GetSessionTitle() string
    SetEntryLabel(entryID, label string) error

    // Session control (CommandContext only)
    WaitForIdle(ctx context.Context) error
    NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error)
    Fork(entryID string) (piapi.ForkResult, error)
    NavigateBranch(targetID string) (piapi.NavigateResult, error)
    SwitchSession(sessionPath string) (piapi.SwitchResult, error)
    Reload(ctx context.Context) error

    // Streaming & logs (routed from hosted protocol)
    EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error
    AppendExtensionLog(extID, level, message string, fields map[string]any) error
}
```

Concrete implementations:

- `internal/tui/session_bridge.go` — `*tuiSessionBridge` wraps `*model`, routes all mutations through `Program.Send` so writes are goroutine-safe.
- `internal/cli/session_bridge.go` — `*cliSessionBridge` for non-interactive mode; messaging ops are no-ops-plus-stderr, session-control ops return `ErrSessionControlUnsupportedInCLI`, log + reload work.
- `internal/extension/api/testing/fakebridge.go` — test helper recording all calls; shared by compiled and hosted tests.

### API-implementation rewiring

- `extapi.NewCompiled(reg, manager, bridge)` gains a `bridge` parameter. Every method that currently returns `ErrNotImplemented{Spec:"#5"}` calls into the bridge.
- `pkg/piext/rpc_api.go` methods call `hostCall(...)` with new services; the host's `HostedAPIHandler.handleHostCall` dispatches the new `session.*`, `session_control.*`, `tool_stream.*`, and `log.*` services through the bridge.

### New hostproto services

| Service | Methods | Direction |
|---|---|---|
| `session` | `append_entry`, `send_custom_message`, `send_user_message`, `set_title`, `get_title`, `set_entry_label` | ext → host |
| `session_control` | `wait_idle`, `new`, `fork`, `navigate`, `switch`, `reload` | ext → host |
| `tool_stream` | `update` | ext → host (service form of existing `MethodToolUpdate`) |
| `log` | `append` | ext → host (service form of existing `MethodLog`) |

Capabilities use the existing `<service>.<method>` shape (e.g. `session.append_entry`, `session_control.fork`). `ProtocolVersion` stays `2.1` — the new services are additive and old extensions that don't request them are unaffected.

The previous `MethodToolUpdate` / `MethodLog` constants continue to be accepted by `HostedAPIHandler.Handle` for one release as aliases for the service-form dispatch. They're deprecated in docs now and removed in spec #6.

## Messaging APIs

### `AppendEntry(kind string, payload any) error`

Appends an extension-generated entry to the session transcript.

- Kind must match `^[a-z][a-z0-9_-]*$`. Returns `ErrInvalidKind` otherwise.
- Payload is serialized to JSON; empty payload is valid (kind-only marker).
- TUI bridge converts into a `message` row with `role = "extension"`, storing the extension name, kind, and payload. Rendered with a dim `[<ext-name>/<kind>]` prefix. If `kind == "markdown"`, the payload's `text` field (if string) is markdown-rendered; otherwise a compact JSON summary is shown.
- CLI bridge writes `[<ext-name>/<kind>] <payload-summary>` to stderr.
- Persisted via existing `appendHistory` so entries survive `/resume`.

Capability: `session.append_entry`.

### `SendUserMessage(msg UserMessage, opts SendOptions) error`

Injects a user-role message into the transcript.

Delivery modes (`opts.DeliverAs`):

- `steer` — aborts the current turn via `ctx.Abort()`, then queues the message as the first input of the next turn. If no turn is running, behaves like `nextTurn`. Rejected with `ErrIncoherentOptions` if `TriggerTurn=false`.
- `followUp` — queues the message in the bridge's `pendingUserMessages`. When the current turn ends, a new turn starts with this message as input. If idle when called, behaves like `nextTurn` with `TriggerTurn=true`.
- `nextTurn` — appends to transcript. If `TriggerTurn=true` and agent is idle, starts a turn immediately. If agent is busy and `TriggerTurn=true`, queues and auto-triggers when idle. If `TriggerTurn=false`, the message waits for user to press enter.

Rendered in TUI with standard user-message styling plus a `[<ext-name>]` chip.

Capabilities: `session.send_user_message` always; `session.trigger_turn` additionally when `TriggerTurn=true` or `DeliverAs="steer"`.

### `SendMessage(msg CustomMessage, opts SendOptions) error`

Injects a non-user, non-assistant entry.

- `msg.CustomType` is the discriminator; `msg.Content` is the body.
- `msg.Display == false`: entry is persisted but not rendered (extension-private annotation).
- `msg.Display == true`: rendered via the default markdown renderer prefixed with `[<ext-name>:<custom-type>]`. The `renderCustomAssistantMessage` hook remains a spec #6 stub and is not invoked yet.
- `DeliverAs="steer"` is rejected with `ErrIncoherentOptions` — only user messages may steer. Other delivery modes behave as in `SendUserMessage`.

Capabilities: `session.append_entry` always (the message is extension-authored, not user-attributed); `session.trigger_turn` additionally when `TriggerTurn=true`. `SendMessage` deliberately does not require `session.send_user_message` — that capability is reserved for user-attributed content that the LLM will treat as user input.

### `SetSessionName(string) error` / `GetSessionName() string`

Binds to `session.Meta.Title`. `SetSessionName` invokes a new `SessionService.SetTitle(sessionID, title)` helper. Empty string clears the title — session picker then falls back to the first user message, matching existing behavior.

Capability: `session.manage`.

### `SetLabel(entryID, label string) error`

Per the session-model deviation documented below, `entryID` is interpreted as a session-branch ID. `entryID == ""` targets the current session. Returns `ErrEntryNotFound` for unknown IDs.

Capability: `session.manage`.

## CommandContext session ops

These methods live on `CommandContext`, which is only passed to command handlers (spec #2). Until spec #2 lands, they're callable from compiled-in tests through a private `piapi.WithCommandContext(ctx, api)` bridge hook — not a public helper, just a first-party test seam.

### `WaitForIdle(ctx context.Context) error`

Blocks until the bridge's turn-state machine is idle (no active turn, no queued user messages). Uses a channel the bridge closes on every idle transition; caller re-checks after wake. Honors `ctx` cancellation.

Capability: `session_control.wait_idle`.

### `NewSession(opts NewSessionOptions) (NewSessionResult, error)`

`NewSessionOptions` stays empty for this spec. Calls `Agent.CreateSession`, swaps the TUI to the new session (same path as the `/new` slash command: reset chat messages, call provider `ResetSession`, update `cfg.SessionID`). Returns `{ID: newID, Cancelled: false}`. `NewSessionResult` gains one additive field `ID string`.

Capability: `session_control.new`.

### `Fork(entryID string) (ForkResult, error)`

`entryID` is a session ID; empty = current session. Delegates to the `handleBranchCommand(["create", ...])` path with a generated `fork-<timestamp>` name. Extensions can follow up with `SetLabel` to rename.

`ForkResult` gains two additive fields: `BranchID string`, `BranchTitle string`. Emits a system message `Forked session into branch <title>.`.

Capability: `session_control.fork`.

### `NavigateTree(targetID string, opts NavigateOptions) (NavigateResult, error)`

`targetID` is a branch ID. Same path as branch-switch in `handleBranchCommand`. Loads messages via `loadSessionMessages`, updates `cfg.SessionID`. `NavigateOptions` stays empty. `NavigateResult` gains `BranchID string`. Returns `ErrBranchNotFound` for unknown IDs.

Capability: `session_control.navigate`.

### `SwitchSession(sessionPath string) (SwitchResult, error)`

`sessionPath` is a session ID (a leading `sessions/` is tolerated and stripped). Same path as `/resume <id>`. Returns `ErrSessionNotFound` for no match. `SwitchResult` gains `SessionID string`.

Capability: `session_control.switch`.

### `Reload(ctx context.Context) error`

Re-reads `approvals.json`, rebuilds `ProviderRegistry`, re-discovers hosted candidates without restarting running extensions. Running extensions' capability grants are re-evaluated live — revoked grants take effect on the next `host_call`. Newly discovered hosted candidates enter `StateReady` but don't auto-launch. TUI emits `Extension runtime reloaded (N active, M newly discovered)`.

Capability: `session_control.reload`. Available from the CLI bridge too — CLI reloads approvals/providers even though it has no turn loop.

### CLI bridge behavior for session control

All session-control methods except `Reload` return `ErrSessionControlUnsupportedInCLI` with a message pointing at the interactive TUI. The CLI has no turn loop to wait on, no branch tree to navigate.

### Deadlock protection

The bridge tracks per-extension call depth. If an event handler (which receives `Context`, not `CommandContext`) somehow acquires a `CommandContext` and calls one of these methods, the bridge detects the reentrancy and returns `ErrSessionControlInEventHandler` rather than deadlocking the agent loop inside `WaitForIdle`.

## Lifecycle hooks

### `pi.toml` schema

```toml
[[hooks]]
event   = "session_start"        # required
command = "ext_announce"         # required; must match a tool this ext registered
tools   = ["read", "grep"]       # optional; "*" = always. Default "*".
timeout = 5000                   # optional ms; default 5000, max 60000
```

- Multiple `[[hooks]]` blocks per extension are allowed.
- `command` names a tool the extension itself registered. Invoking a hook = invoking that tool with a synthesized `ToolCall` whose `Args` is the event payload. Hooks ride the existing tool-execute path — no separate dispatch wire.
- `loader.Metadata` gains a `Hooks []HookConfig` slice. `HookConfig` gains a private `extensionID string` field populated during aggregation in `BuildRuntime`.

### Events fired

| Event | When | Payload |
|---|---|---|
| `LifecycleEventStartup` (`"startup"`) | Once, after `BuildRuntime` completes, before TUI/CLI accepts user input. Sequential; failures logged. A hook with `Critical = true` aborts startup on failure; only extensions whose `approvals.json` entry has `trust_class == "first-party"` may set this flag — the loader rejects `Critical = true` from third-party extensions at parse time. | `{"work_dir": string, "extensions": [name...]}` |
| `LifecycleEventSessionStart` (`"session_start"`) | Each session create/resume/fork/switch. Fires after the existing `EventSessionStart` API event. | `{"session_id": string, "reason": "new"\|"resume"\|"fork"\|"switch", "title": string}` |
| `LifecycleEventBeforeTurn` (`"before_turn"`) | Before each agent turn, after user input is accepted. Hook return content with `kind=hook/before_turn` is `AppendEntry`'d so the LLM sees it. | `{"session_id": string, "user_text": string}` |
| `LifecycleEventAfterTurn` (`"after_turn"`) | After each turn completes. | `{"session_id": string, "turn_events": int, "aborted": bool}` |
| `LifecycleEventShutdown` (`"shutdown"`) | During `Lifecycle.Stop` before extensions are signaled to exit. 10s total budget. | `{"reason": "user"\|"signal"\|"error"}` |

### Dispatch order

Hooks per event fire **sequentially** in declaration order across extensions. No parallelism — keeps ordering deterministic for hooks mutating session state.

### Firing sites

- `BuildRuntime` end → `startup`.
- `Agent.CreateSession` / `loadSessionMessages` (TUI) → `session_start`.
- `agent_loop.go` before `Agent.Run` → `before_turn`; after iterator closes → `after_turn`.
- `lifecycle.Service.Stop` → `shutdown`.

### Capability

Declaring any `[[hooks]]` entry requires the `hooks.register` capability in `requested_capabilities`. Without it, hooks are silently dropped with a trace-panel warning and the gate records the denial visible in the extensions panel.

## Routing plumbing

### `MethodToolUpdate` → streaming tool display

Today: `HostedAPIHandler.Handle` accepts the method and drops the payload. The in-process `UpdateFunc` for compiled-in extensions is also unwired.

New flow:

1. Extension calls `onUpdate(partial)`.
2. For hosted: `rpcAPI.handleToolExecute` already invokes `onUpdate`, which sends `pi.extension/tool_update`. The host handler now calls `bridge.EmitToolUpdate(toolCallID, partial)` instead of dropping.
3. For compiled: `NewCompiled` wires its `UpdateFunc` closure to call `bridge.EmitToolUpdate` directly.
4. Bridge dispatches a tea message `ToolStreamMsg{ToolCallID, Partial}` through `Program.Send`.
5. `ToolDisplayModel` gains `streamingRows map[string]*toolRow` keyed by `toolCallID`. A streaming update replaces the row's body with `partial.Content`, bumps an update counter, and shows a spinner glyph. The row transitions to "complete" when the final `ToolResult` arrives via the normal ADK event path; the streaming map entry is then cleared.
6. Trace log also receives the partial under kind `tool-stream` (dim, collapsed by default).

Capability: none — `tool_stream.update` is auto-granted on handshake since dropping to stderr was already possible via `piext.Log()`.

### `MethodLog` → TUI trace panel + rotating log file

Payload shape (new hostproto type):

```go
type LogParams struct {
    Level   string         `json:"level"`   // debug|info|warn|error
    Message string         `json:"message"`
    Fields  map[string]any `json:"fields,omitempty"`
    Ts      string         `json:"ts,omitempty"` // RFC3339, optional
}
```

`bridge.AppendExtensionLog(extID, level, message, fields)`:

1. Appends to `TraceLog` as trace kind `extension-log`, colored by level.
2. Writes a JSONL line `{ts, ext, level, message, fields}` to `~/.pi-go/logs/extensions.log`. File opened lazily; rotated at 10 MB (keep last 3 files).

Sanitization:

- `message` truncated at 8 KB.
- `fields` marshaled with a depth limit of 6 to guard against pathological nesting.

`piext.Log()` (currently returns stderr) is rebound so hosted-extension `fmt.Fprintln(piext.Log(), ...)` calls funnel through `log.append` automatically. Plain stderr remains the fallback when the transport isn't yet connected.

Capability: `log.append` — auto-granted on handshake (parity with stderr, which no extension currently has to request).

## Error types

Added to `pkg/piapi`:

- `ErrInvalidKind`
- `ErrIncoherentOptions`
- `ErrEntryNotFound`
- `ErrBranchNotFound`
- `ErrSessionNotFound`
- `ErrSessionControlUnsupportedInCLI`
- `ErrSessionControlInEventHandler`

All are value types implementing `error`; callers can `errors.Is` them against exported sentinels.

## Deviations from the source design shape

1. `Fork(entryID)` uses `entryID` as a session ID, not an in-session entry ID. pi-go has no per-entry IDs surfaced to extensions.
2. `NavigateTree(targetID)` switches session branches. Same reason.
3. `SwitchSession(sessionPath)` interprets the path as a session ID with a tolerated `sessions/` prefix.
4. `NewSessionResult` / `ForkResult` / `NavigateResult` / `SwitchResult` each gain additive ID fields. No breaking change to existing consumers (none exist in production).
5. Three additional lifecycle events (`before_turn`, `after_turn`, `shutdown`) beyond what the source design named. Added now to avoid a schema break later.

## Testing strategy

Per-surface unit tests (new or rewritten):

- `internal/extension/api/bridge_test.go` — `fakeSessionBridge` recording; verify each API method routes correctly and maps errors.
- `internal/extension/api/compiled_spec5_test.go` — replaces `TestCompiled_SendMessageNotImplemented` etc. with assertions that calls hit the bridge.
- `pkg/piext/rpc_api_spec5_test.go` — fake `Transport` confirms each method issues the right `host_call`.
- `internal/tui/session_bridge_test.go` — `AppendEntry` produces the expected `message` row; `SendUserMessage` with `TriggerTurn=true` calls `startTurn`; `DeliverAs="steer"` calls `Abort` then queues.
- `internal/tui/tool_stream_test.go` — `ToolDisplayModel` streaming-row transitions.
- `internal/extension/hostproto/protocol_test.go` — new services/methods round-trip.
- `internal/extension/runtime_lifecycle_test.go` — hook parsing, filtering, firing order, timeout, missing-tool handling.

Integration tests:

- `internal/extension/lifecycle/lifecycle_e2e_hosted_go_test.go` gains `TestHostedGo_Messaging`: hosted extension AppendEntries, SendUserMessages with `TriggerTurn=true`, asserts a turn runs.
- `internal/extension/e2e_hosted_go_spec5_test.go` (new) — round-trips real stdio: handshake → `session.append_entry` → `session.send_user_message` → `tool_stream.update` → `log.append` → hook firing on `session_start`.

CLI tests:

- `internal/cli/session_bridge_test.go` — asserts session-control methods return `ErrSessionControlUnsupportedInCLI`; `AppendExtensionLog` and `Reload` succeed.

Fakes:

- `internal/extension/api/testing/fakebridge.go` — shared by compiled and hosted tests.

## Migration & compatibility

- Every method currently returning `piapi.ErrNotImplemented{Spec:"#5"}` gains a working implementation. Callers using `errors.Is(err, piapi.ErrNotImplementedSentinel)` stop matching on success paths — that is the intended behavior. Repo audit confirms only existing stub-tests depend on the sentinel, and those tests are rewritten.
- `MethodToolUpdate` and `MethodLog` method names remain valid for one release as aliases for the service-form dispatch. Deprecated in docs with spec #5; removed in spec #6.
- pi.toml without `[[hooks]]` works unchanged. `hosted-hello-go` and `hosted-hello-ts` are unaffected — they don't request new capabilities.
- `approvals.json` schema is unchanged. New capability strings (`session.*`, `session_control.*`, `log.*`, `tool_stream.*`, `hooks.register`) slot into the existing `granted_capabilities` array.

## Task outline

The execution plan (written separately via `superpowers:writing-plans`) will expand these into step-by-step tasks with file paths and commit boundaries:

1. `hostproto` additions (services, methods, `LogParams`).
2. `SessionBridge` interface, fake, and error types.
3. Compiled API rewiring to bridge.
4. Hosted API handler routing for new services.
5. `pkg/piext/rpc_api` method wiring (extension-side).
6. `piext.Log()` funnel through `log.append`.
7. TUI session bridge with tea-message dispatch.
8. CLI session bridge (headless).
9. `ToolDisplayModel` streaming rows.
10. Extension log file + rotation.
11. Lifecycle hook parsing in `loader` (`[[hooks]]`).
12. Hook aggregation in `BuildRuntime`; firing at five lifecycle sites.
13. Result-type field additions on `NewSessionResult` / `ForkResult` / `NavigateResult` / `SwitchResult`.
14. Capability plumbing (approvals schema unchanged).
15. Unit tests per surface.
16. E2E test additions.
17. Docs updates (`docs/extensions.md` spec #5 section).
18. Deprecation notes on old `MethodToolUpdate` / `MethodLog` names.

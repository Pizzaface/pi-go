---
name: extensions-lifecycle-tui-management
description: Programmatic lifecycle service + TUI approval/management surface for hosted extensions, built on the Phase 8 host.Manager foundation.
status: draft
created: 2026-04-15T12:00:00Z
updated: 2026-04-15T12:00:00Z
---

# Extensions Lifecycle & TUI Management

## Goals

1. A single programmatic surface (`lifecycle.Service`) owns approve / deny / revoke / start / stop / restart / reload for hosted extensions. The TUI consumes it. Future SDK work can expose a subset through `pkg/piapi` / `packages/extension-sdk` without rewriting either side.
2. Approved hosted extensions auto-start at TUI boot without blocking startup.
3. Unapproved hosted extensions are visible in the TUI and approvable from inside it — no more hand-editing
   `~/.go-pi/extensions/approvals.json`.
4. Crashed, stuck, or failed extensions never hang or kill go-pi; they surface in the panel with an error and a retry
   path.

## Non-goals

- Exposing the lifecycle surface to extensions-themselves in this spec. That's a deliberate future extension of `piapi.API`; the interface shape is chosen so the exposure is near-mechanical.
- Per-capability incremental grant UX beyond "approve the set the extension asked for" (checkbox per capability is in scope; progressive grant negotiation is not).
- Filesystem watchers / auto-reload on file change. Reload is user-initiated (`R` in the panel).
- Migrating compiled-in extensions into the lifecycle state machine. They remain implicitly trusted and are shown read-only.
- CLI subcommands (`pi ext approve`, etc.). The `lifecycle.Service` is designed to support them later; wiring is out of scope for this spec.

## Architecture

```
internal/extension/lifecycle/        NEW
    service.go        Service interface + concrete impl
    approvals.go      approvals.json atomic read/write with unknown-field preservation
    event.go          Event + EventKind + View types
    autostart.go      StartApproved + StopAll + buildCommand
    service_test.go
    approvals_test.go
    autostart_test.go
    lifecycle_e2e_hosted_go_test.go   (skip when `go` missing)
    lifecycle_e2e_hosted_ts_test.go   (skip when `node` missing)

internal/extension/host/              unchanged
    manager.go        state machine stays here (Registration, State, Register, Get, List, SetState)
    launch.go         LaunchHosted unchanged
    capability.go     Gate.Reload unchanged; no write method — lifecycle.approvals owns writes

internal/extension/runtime.go         touched
    BuildRuntime constructs a *lifecycle.Service; Runtime gains .Lifecycle field.

internal/tui/                         NEW files
    extension_panel.go                /extensions overlay (list + actions)
    extension_panel_test.go
    extension_approval_dialog.go      per-extension Approve/Deny modal
    extension_approval_dialog_test.go
    extension_toast.go                status-bar "N pending — press e" line
    extension_toast_test.go
    chat.go / commands.go             touch — /extensions slash command, 'e' binding, event bridge

cmd/pi/                               touched
    main.go / cli.go                  hand Runtime.Lifecycle to the TUI; on tea.Quit call Lifecycle.StopAll.
```

**Dependency direction:** `tui → lifecycle → host/*`. The TUI never imports `host` directly; it goes through `lifecycle.Service`.

**State ownership:** `host.Manager` remains the single source of state for `Registration` + `State`. `lifecycle.Service` is a write-facing orchestrator; it never shadows state.

## The `lifecycle.Service` interface

```go
// internal/extension/lifecycle/event.go

type View struct {
    ID         string
    Mode       string          // "compiled-in" | "hosted-go" | "hosted-ts"
    Trust      host.TrustClass
    State      host.State
    Version    string
    WorkDir    string
    Requested  []string        // requested_capabilities from metadata
    Granted    []string        // current grants from approvals.json
    Approved   bool
    Err        string          // last error message, if any
}

type EventKind int
const (
    EventStateChanged EventKind = iota
    EventApprovalChanged
    EventRegistrationAdded
    EventRegistrationRemoved
)

type Event struct {
    Kind EventKind
    View View
}

// Error is the canonical error shape. TUI formats as "<op> failed for <id>: <err>".
type Error struct {
    Op  string
    ID  string
    Err error
}
func (e *Error) Error() string { ... }
func (e *Error) Unwrap() error { return e.Err }

var ErrCompiledIn = errors.New("compiled-in extensions cannot be approved/denied/revoked")
var ErrUnknownExtension = errors.New("unknown extension")
```

```go
// internal/extension/lifecycle/service.go

type Service interface {
    List() []View
    Get(id string) (View, bool)

    Approve(ctx context.Context, id string, grants []string) error
    Deny(ctx context.Context, id string, reason string) error
    Revoke(ctx context.Context, id string) error

    Start(ctx context.Context, id string) error
    Stop(ctx context.Context, id string) error
    Restart(ctx context.Context, id string) error

    StartApproved(ctx context.Context) []error
    StopAll(ctx context.Context) []error
    Reload(ctx context.Context) error

    Subscribe() (<-chan Event, func())
}

// Concrete constructor.
func New(mgr *host.Manager, gate *host.Gate, approvalsPath, workDir string) *service
```

**Semantics:**

- `Approve(id, grants)` — writes `approved:true`, `approved_at:<ISO>`, merges `grants` into `granted_capabilities` (dedup sorted), clears `deny_reason`. If registration is hosted and state was `StatePending` or `StateDenied`, transitions to `StateReady`. Emits `EventApprovalChanged` first, then `EventStateChanged` — observable ordering contract so subscribers can rely on approval metadata being current before acting on the state change.
- `Deny(id, reason)` — writes `approved:false`, `denied_at:<ISO>`, `deny_reason:reason`. State → `StateDenied`. If running, `Stop` is called first.
- `Revoke(id)` — deletes the entry entirely. State → `StatePending`. If running, `Stop` is called first.
- `Start(id)` — hosted only; builds command, calls `host.LaunchHosted`; hooks up a handshake-timeout watcher. Idempotent on `StateRunning`.
- `Stop(id)` — hosted only; sends `pi.extension/shutdown`, closes conn, waits up to 3s, force-kills. State → `StateStopped`. Idempotent on `StateStopped`.
- `Restart(id)` — `Stop` then `Start`. Wait for `StateStopped` before re-starting.
- `StartApproved(ctx)` — spawn each ready hosted extension in its own goroutine. Non-blocking; returns an empty slice today and the per-extension errors later via `Subscribe`. Return type is `[]error` so a future synchronous caller can choose to collect.
- `StopAll(ctx)` — parallel `Stop` across all running extensions. Bounded 3s wait per extension.
- `Reload(ctx)` — rewalks discovery paths, diffs against current registrations, emits `RegistrationAdded` / `RegistrationRemoved`, refreshes metadata for existing ones.
- `Subscribe()` — returns a buffered channel (cap 16) + cleanup func. Publishes drop on full and log a warning.

**Compiled-in handling:** Approve / Deny / Revoke / Start / Stop / Restart return `ErrCompiledIn` for compiled-in IDs. `List` / `Get` / `Reload` / `Subscribe` treat them the same as hosted (read-only rows).

## approvals.json persistence

```go
// internal/extension/lifecycle/approvals.go

type approvalsFile struct {
    Version    int                        `json:"version"`
    Extensions map[string]json.RawMessage `json:"extensions"`   // preserve unknown fields
}

// mutateApprovals loads the file (or {"version":2}), applies op to the
// raw entry for id (decoded as map[string]any), re-encodes to RawMessage,
// writes atomically, and calls gate.Reload.
//
// op returns nil to delete the entry (Revoke).
func (s *service) mutateApprovals(id string, op func(map[string]any) map[string]any) error
```

Each mutation:

```
s.mu.Lock() defer s.mu.Unlock()

raw := readApprovals(s.path) or empty {"version":2,"extensions":{}}

entry := decodeRaw(raw.Extensions[id])          // map[string]any, may be nil
entry = op(entry)
if entry == nil {
    delete(raw.Extensions, id)
} else {
    raw.Extensions[id] = encodeRaw(entry)
}

atomicWrite(s.path, raw)                         // tmp + rename
s.gate.Reload()
```

**Atomic write** — `os.Rename` on POSIX, `os.Remove(target)+os.Rename` on Windows (small race; acceptable under in-process mutex).

**Unknown-field preservation** — entries round-trip through `json.RawMessage` then `map[string]any`. Fields we don't name (e.g. a future `hash`, `notes`, `installed_at`) survive rewrites unchanged.

**No file lock across processes** — v1 is human-driven, single-go-pi-per-user. Last-write-wins. Future work can add
`flock` if needed.

## Auto-start flow

TUI init spawns a single bubbletea `tea.Cmd`:

```go
return func() tea.Msg {
    errs := rt.Lifecycle.StartApproved(ctx)
    return lifecycleStartedMsg{errs: errs}
}
```

`StartApproved`:

```
for reg in mgr.List():
    if reg.State != StateReady: continue
    if reg.Mode not in (hosted-go, hosted-ts): continue
    go func(reg) {
        cmd, err := buildCommand(reg)
        if err != nil { mgr.SetState(reg.ID, StateErrored, err); return }
        ctx2, cancel := context.WithCancel(ctx)
        go watchHandshakeTimeout(ctx2, mgr, reg, 5*time.Second, cancel)
        handler := api.NewHostedHandler(mgr, reg)
        if err := host.LaunchHosted(ctx2, reg, mgr, cmd, handler.Handle); err != nil {
            mgr.SetState(reg.ID, StateErrored, err)
        }
    }(reg)
```

`buildCommand`:

- **hosted-go** — returns `reg.Metadata.Command` (from `pi.toml`). Falls back to `["go", "run", "."]`.
- **hosted-ts** — `["node", hostPath, "--entry", abs(reg.Metadata.Entry), "--name", reg.ID]` where `hostPath` comes from
  `host.ExtractedHostPath(piVersion)` (the go-pi build version — extraction is idempotent per version). Errors if
  `exec.LookPath("node")` fails.

`watchHandshakeTimeout`:

- Polls `mgr.Get(id).State` every 100ms; on `StateRunning` returns cleanly.
- On `StateErrored` returns.
- On timeout: calls `cancel()` (kills the subprocess via the exec ctx), then `mgr.SetState(id, StateErrored, errHandshakeTimeout)`.

`StopAll` (on `tea.Quit`):

- Parallel `Stop` across every running registration, 3s per-extension timeout.
- Force-kill via `reg.Conn.Close()` and the process handle on timeout.

## TUI surface

Three elements:

```
Toast (in status bar):
  > ask me anything
    [ gpt-5-mini | 0 ctx ]  2 extensions pending approval — press e to review

/extensions panel (overlay):
  ┌─ Extensions ─────────────────────────────── esc close ─┐
  │ NAME              MODE        STATE     TRUST          │
  │ hello             compiled-in ready     compiled-in    │
  │ hosted-hello-go   hosted-go   running   third-party    │
  │ hosted-hello-ts   hosted-ts   pending   third-party   ◀│  (selected)
  │                                                         │
  │ Pending approval. Requested capabilities:               │
  │   tools.register                                        │
  │   events.session_start                                  │
  │   events.tool_execute                                   │
  │                                                         │
  │ a approve  d deny  s start  x stop  r restart          │
  │ v revoke   R reload all   / filter                      │
  └─────────────────────────────────────────────────────────┘

Approval dialog (pressing 'a' on a pending row):
  ┌─ Approve hosted-hello-ts? ─────────────────────────────┐
  │ hosted-hello-ts v0.1.0                                  │
  │ Canonical hosted-ts extension fixture; registers greet. │
  │ Requested capabilities:                                 │
  │   [x] tools.register                                    │
  │   [x] events.session_start                              │
  │   [x] events.tool_execute                               │
  │ Space toggle · Enter approve · Esc cancel               │
  └─────────────────────────────────────────────────────────┘
```

**1. Status-bar toast**  
Appears when `≥1` extension has `State == StatePending`. Line format:
```
N extensions pending approval — press e to review
```
Auto-hides on the next keystroke regardless of which key (including `e`); `e` additionally opens the panel. Re-appears on the next `EventStateChanged` that leaves ≥1 extension pending.

**2. `/extensions` panel**  
Bubbletea overlay following `slash_command_overlay` conventions.

Columns: `NAME | MODE | STATE | TRUST`. Selected row reveals a detail pane below the list showing requested capabilities, granted capabilities (if different), and last error (if any).

Keybindings on the panel:
```
↑/↓ select · / filter · enter no-op (detail already visible)
a approve     d deny      v revoke
s start       x stop      r restart
R reload all  ?  help     esc close
```

Compiled-in rows render dimmed with a `(implicit)` trust tag; per-row action keys (`a/d/v/s/x/r`) are no-ops when a compiled-in row is selected. Panel-level keys (`R`, `/`, navigation, `esc`) work regardless of selection.

Failed rows render red with the error in the detail pane. `r` retries (Restart).

**3. Approval dialog**  
Opened by pressing `a` on a pending row. Shows metadata + checkbox list of requested capabilities (pre-ticked). Keys: `Space` toggle · `Enter` approve ticked set · `Esc` cancel. Partial-grants case: if user unticks items, only the ticked set is passed to `Service.Approve`.

**Event bridge:** in `model.Init`, register a `tea.Cmd` that reads one `lifecycle.Event` from the subscribe channel, converts it to `extensionEventMsg`, and re-queues itself after dispatch. Keeps UI reactive without goroutines owning UI state.

**WindowSizeMsg rebase:** on every `tea.WindowSizeMsg`, the panel also calls `rt.Lifecycle.List()` to reconcile in case the Subscribe channel dropped an event — cheap, eventual consistency.

## Error handling

| Failure | Detection | Behavior | User sees |
| --- | --- | --- | --- |
| `cmd.Start` fails | error from `LaunchHosted` | `StateErrored { Err: "start: <msg>" }` | Red row; `r` retries. |
| Handshake timeout (>5s) | Watcher goroutine | `ctx.cancel` kills process; `StateErrored { Err: "handshake timeout" }` | Red row; `r` retries. |
| Handshake protocol mismatch | host returns `ErrCodeHandshakeFailed`, process exits | RPC close observed; `StateErrored { Err: <proto msg> }` | Red row with protocol error. |
| Post-handshake crash | `cmd.Wait` on watcher | `StateErrored { Err: "process exited: <code>" }` | Red row; last stderr line if captured. |
| `approvals.json` malformed | initial read during `mutateApprovals` fails to parse | mutation aborts before any write; returns `lifecycle.Error{Op: "approve", Err: <parse-err>}` | Toast: "approve failed: corrupt approvals.json at <path> — fix and press R". |
| `approvals.json` write fails | `atomicWrite` error | no state change; returns `lifecycle.Error` | Error toast with path + errno. |
| `node` missing for hosted-ts | `buildCommand` preflight | `StateErrored { Err: "node not on PATH" }` | Row pending; clears on restart after PATH fix. |
| Subscribe channel full | publisher detects full buffer | drops event + logs warning | Briefly out of date; rebased on next `WindowSizeMsg`. |
| Stop timeout | 3s bounded wait | `Process.Kill` + `StateErrored { Err: "forced kill on shutdown" }` | Brief red flash before `StateStopped`. |

## Testing

**lifecycle package (fast, no subprocesses):**

- `approvals_test.go` — table-driven: approve / deny / revoke starting from empty / partial / unknown-field-rich fixtures; asserts unknown fields round-trip, atomic-write leaves no `.tmp`, output stable-sorted.
- `service_test.go` — real `host.Manager` + `host.Gate` with temp approvals file. Asserts state transitions, event ordering (approve emits `EventApprovalChanged` before `EventStateChanged`), idempotent Stop/Restart, `ErrCompiledIn` for compiled-in IDs.
- `autostart_test.go` — `StartApproved` with a fake `launchFunc` injected so no processes spawn. Asserts parallelism, per-extension error capture, one extension's failure doesn't block others.

**Integration (slower, real subprocess) — one per mode:**

- `lifecycle_e2e_hosted_go_test.go` — uses the existing `examples/extensions/hosted-hello-go`; symlink into temp HOME, approve via Service, autostart, assert `StateRunning`, Restart, Stop. Skip when `go` not on PATH or symlink fails.
- `lifecycle_e2e_hosted_ts_test.go` — same with node + vendored host. Skip when `node` missing.

**TUI (teatest):**

- `extension_panel_test.go` — keystroke scripts: open panel → navigate → approve → verify row becomes ready → start → verify running. Uses a fake `lifecycle.Service` recording calls and publishing canned events.
- `extension_approval_dialog_test.go` — space/enter/esc flows; partial-grants (uncheck one before Enter).
- `extension_toast_test.go` — appears with ≥1 pending, disappears when all pending clear, dismissable on any non-`e` key.

**Coverage targets:** lifecycle ≥ 85% line coverage (it's the SDK-exposure surface). TUI tests are interaction-focused; coverage is secondary.

## Future exposure via `piapi`

The `Service` interface was chosen to be mechanical to expose:

```go
// Future pkg/piapi/api.go addition (NOT this spec)
type API interface {
    // ...existing methods
    Extensions() ExtensionsAPI    // returns lifecycle.Service-shaped surface
}

type ExtensionsAPI interface {
    List() []ExtensionInfo
    Approve(ctx context.Context, id string, grants []string) error
    // ... one-to-one with lifecycle.Service
}
```

Hosted extensions would reach this via a new `extensions` service in the handshake. Third-party extensions would be denied by default; first-party tagged ones could gate behind a `extensions.manage` capability. Again — not this spec. Designing the shape now so that spec is additive, not a rewrite.

## Implementer notes

- `host.Manager.Get/List/SetState` are already sufficient — no changes needed.
- `host.Gate.Reload()` already re-reads the file; no `Gate.Write` needed (writing is lifecycle's concern).
- `host.LaunchHosted` signature unchanged.
- `Runtime.Lifecycle` is nil-safe: CLI codepaths that don't need it (e.g. `pi audit`) simply skip it.
- bubbletea v2 and lipgloss v2 are already in use — match the patterns in `slash_command_overlay.go` and `model_picker.go`.

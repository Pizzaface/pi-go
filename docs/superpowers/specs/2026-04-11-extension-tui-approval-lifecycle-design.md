---
name: extension-tui-approval-lifecycle
description: In-TUI approval and lifecycle management for hosted extensions, replacing the file-only approval flow and the startup hang on unapproved extensions.
status: implemented
created: 2026-04-11T00:00:00Z
updated: 2026-04-12T00:00:00Z
---

# Extension TUI Approval & Lifecycle

## Goals

1. An unapproved hosted extension must never block go-pi startup. Today, a hosted extension that is missing from
   `approvals.json` either hard-fails `BuildRuntime` (earlier manager behavior) or, when approval is present but its
   subprocess is slow to handshake, hangs the TUI forever on "loading: tools...". Both are unacceptable.
2. Extensions are approved interactively through the TUI instead of requiring the user to hand-edit
   `~/.go-pi/extensions/approvals.json`.
3. Hosted extensions can be approved, denied, started, stopped, restarted, revoked, and reloaded from a running go-pi
   session without restarting the process.
4. A single place (`/extensions`) to see every extension's current state.

## Non-goals

- Filesystem watcher / daemon-style auto-discovery. Reload is user-initiated only.
- A richer per-capability grant UX beyond "approve the whole set the extension declared." Incremental capability expansion is out of scope for this pass.
- Migrating declarative or compiled-in extensions into the lifecycle state machine. They remain trusted-by-default as they are today.
- Reworking the `docs/approvals.json` file layout or schema; it is extended with no breaking changes.

## Current state

- `internal/extension/runtime.go` (`BuildRuntime`) loads approvals from disk, registers manifests, then calls `StartHostedExtensions` synchronously while the TUI shows `loading: tools...`.
- `internal/extension/manager.go` (`RegisterManifest`, line ~344) hard-rejects any hosted extension whose id has no entry in `approvals.json`. `BuildRuntime` surfaces the error to the TUI, which quits.
- When an approval does exist but the subprocess handshake times out, `StartHostedExtensions` calls `client.Shutdown(context.Background())`. `Process.Shutdown` (`internal/extension/hostruntime/process.go`) sends `os.Interrupt` (unsupported on Windows), does not close the stdio pipes, and waits on `p.Wait()` with an unbounded context. Result: the goroutine hangs forever, `send("tools", true)` never fires, the TUI freezes on its loading indicator.
- `internal/tui/extension_msgs.go` already defines an `extensionDialogState`, but it is purely informational — `handleExtensionDialogKey` dismisses it on any key with no yes/no return path, so it cannot be reused verbatim for approval.

## Design

### 1. Manager state machine

Replace the implicit "registered = always ready" assumption with an explicit per-extension state:

```go
type ExtensionState string
const (
    StatePending ExtensionState = "pending_approval" // manifest known, no approval
    StateReady   ExtensionState = "ready"            // approved, not yet launched
    StateRunning ExtensionState = "running"          // hosted process alive + handshake OK
    StateStopped ExtensionState = "stopped"          // stopped by user
    StateErrored ExtensionState = "errored"          // launch/handshake/runtime failure
    StateDenied  ExtensionState = "denied"           // user explicitly denied this session
)

type extensionRegistration struct {
    manifest  Manifest
    trust     TrustClass
    state     ExtensionState
    lastError string
    startedAt time.Time
}
```

**Initial-state rules at registration time:**

| Manifest type              | Approval present | Initial state |
|----------------------------|------------------|---------------|
| Declarative                | n/a              | `Ready`       |
| Compiled-in                | n/a              | `Ready`       |
| Hosted stdio JSON-RPC      | yes              | `Ready`       |
| Hosted stdio JSON-RPC      | no               | `Pending`     |

`RegisterManifest` no longer returns an error for an unapproved hosted extension — this is the fix that unblocks startup. Capability validation that today happens inside `RegisterManifest` moves to a helper `validateApprovedCapabilities(manifest, trust)` that is called only for ready/running extensions; pending extensions carry their requested caps through to the approval dialog untouched.

### 2. Lifecycle API

New public methods on `Manager` (all thread-safe; mutations held behind `m.mu`; launch/handshake never holds the mutex):

| Method | Behavior |
|--|--|
| `StartExtension(ctx, id) error` | Launches + handshakes if state is `Ready`, `Stopped`, or `Errored`. Idempotent no-op if `Running`. Error if `Pending`/`Denied`. |
| `StopExtension(ctx, id) error` | Calls `client.Shutdown` with a bounded context, transitions to `Stopped`, runs `UnregisterExtension` side-effects so dynamic commands/tools/renderers owned by the ext go away. |
| `RestartExtension(ctx, id) error` | Stop then Start. Preserves id; updates `startedAt`. |
| `ReloadManifests(dirs ...string) (added, removed []string, err error)` | Re-scans dirs, diffs against current registrations. New → registered. Missing → stopped + removed. Changed runtime or capabilities → stopped, re-registered as `Pending` (cap change invalidates prior approval). |
| `Extensions() []ExtensionInfo` | Read-only snapshot consumed by the TUI panel and the status line. Includes `id`, `state`, `trust`, `requestedCapabilities`, `lastError`, `startedAt`. |
| `GrantApproval(GrantInput) error` | Persists to `approvals.json` via `Permissions.Upsert`, transitions `Pending` → `Ready`. Does NOT auto-start — the caller decides (the TUI approval Cmd calls `StartExtension` after `GrantApproval`). |
| `DenyApproval(id) error` | Transitions to `Denied`. In-memory only; not persisted. |
| `RevokeApproval(ctx, id) error` | `Permissions.Delete(id)`, `StopExtension` if running, transitions back to `Pending`. |

`StartHostedExtensions` iterates `Ready` extensions and is **partial-failure tolerant**: on a per-extension failure it records the error, transitions to `Errored`, and continues. It no longer returns per-extension errors up to `BuildRuntime`. Callers observe failures via `Extensions()`.

### 3. Hang fixes

Three compounding root causes, fixed independently so no single regression can re-hang the TUI.

**Fix 3a — `Process.Shutdown` closes pipes before waiting.**

```go
func (p *Process) Shutdown(ctx context.Context) error {
    if p == nil { return nil }
    select { case <-p.waitDone: return nil; default: }

    // Close stdin first — a well-behaved extension sees EOF on its
    // decoder and Serve returns cleanly. Works on every platform.
    if p.stdin != nil { _ = p.stdin.Close() }

    // Best-effort interrupt. No-op on Windows (os.Interrupt unsupported
    // for child processes) but clean on Unix.
    if runtime.GOOS != "windows" && p.cmd != nil && p.cmd.Process != nil {
        _ = p.cmd.Process.Signal(os.Interrupt)
    }

    done := make(chan struct{})
    go func() { _ = p.Wait(); close(done) }()

    select {
    case <-done:
        if p.stdout != nil { _ = p.stdout.Close() }
        return nil
    case <-ctx.Done():
        if p.cmd != nil && p.cmd.Process != nil {
            _ = p.cmd.Process.Kill() // last resort; works on Windows
        }
        <-done
        if p.stdout != nil { _ = p.stdout.Close() }
        return ctx.Err()
    }
}
```

**Fix 3b — Bounded shutdown contexts at every call site.** New `const HostedShutdownTimeout = 3 * time.Second` in `manager.go`. Every `client.Shutdown(...)` call wraps with `context.WithTimeout(parent, HostedShutdownTimeout)`:

- `startOneHosted` error path (previously `context.Background()`)
- `StopExtension`
- `ShutdownHostedExtensions` (global teardown)

**Fix 3c — `StartHostedExtensions` partial-failure tolerance** (already covered in §2) — one slow/broken extension cannot abort `BuildRuntime`.

**Also:** `HostedHandshakeTimeout = 5 * time.Second` becomes a named const next to `HostedShutdownTimeout` instead of the inline `5*time.Second` today.

**Explicit non-fix:** `go run .` first-compile slowness for the example hosted-hello extension. An extension whose runtime command exceeds the handshake budget will show up in the `/extensions` panel as `Errored` with message `"handshake timeout after 5s"`. The user can fix the extension (prebuild, change the command) or retry via the panel. The hang path is gone either way.

### 4. TUI integration

**Status line indicator** (`internal/tui/status.go`). `StatusInput` gains `ExtensionsSummary {Pending, Running, Errored int}`. Rendered compactly as `ext: 1! 2✓ 1✗` only when any count > 0. Populated from `m.cfg.ExtensionManager.Extensions()` on each view render — cheap in-memory snapshot.

**Slash command `/extensions`.** Added to `defaultBuiltinSlashCommands` in `manager.go`. Handled in the slash-command dispatcher (same place as `/skill-list`, `/login`, etc.):

| Form | Effect |
|--|--|
| `/extensions` | Opens the panel |
| `/extensions reload` | Rescans manifest dirs and refreshes |
| `/extensions approve <id>` | Scriptable approve + start, no modal |
| `/extensions deny <id>` | Scriptable deny |
| `/extensions stop <id>` / `restart <id>` / `revoke <id>` | Scriptable lifecycle |

**Panel state** (`internal/tui/extensions_panel.go`, new file):

```go
type extensionsPanelState struct {
    rows      []extensionRow     // flattened, pending first
    cursor    int
    subDialog *extensionApprovalDialogState // non-nil when approving/revoking
}

type extensionRow struct {
    id            string
    state         extension.ExtensionState
    trust         extension.TrustClass
    requestedCaps []extension.Capability
    lastError     string
}
```

Rendered in `tui_view.go` as a bordered box above the input, same layer as `extensionDialog` (mutually exclusive). Rows grouped into **Pending approval**, **Running**, **Stopped**, **Errored**. The selected row shows a keybind hint line below the group.

**Key handling** (`handleExtensionsPanelKey` in `tui_keys_modals.go`):

| Key | Action |
|--|--|
| `↑` `↓` `k` `j` | Navigate rows |
| `Enter` | Context-sensitive primary action: approval sub-dialog (pending), restart (running/stopped/errored), no-op (denied) |
| `a` | Approve (pending only) |
| `d` | Deny (pending only) |
| `r` | Restart (running/stopped/errored) |
| `s` | Stop (running) |
| `x` | Revoke (running/stopped) — confirm sub-dialog |
| `R` | Reload manifests |
| `Esc` / `Ctrl+C` | Close panel |

**Approval sub-dialog.** Opened when the user approves a pending row:

```
Approve extension: hosted-hello
  Trust class:    hosted_third_party
  Runtime:        go run .
  Requested caps: ui.status, commands.register

  [Enter] approve and start   [Esc] cancel
```

`Enter` dispatches an `extensionGrantMsg{id, capabilities}` through the existing Cmd pipeline. The Cmd calls `Manager.GrantApproval` then, **only if grant succeeded**, `Manager.StartExtension` in a background goroutine so the TUI stays responsive during the 5s handshake. If grant fails, start is not attempted and the extension stays in `Pending` with `lastError` set. A trailing `extensionLifecycleResultMsg{id, op, err}` updates the panel with success or the error string. `op` distinguishes `"grant"` from `"start"` so the panel can show which step failed.

### 5. Data flow walkthroughs

**Flow A — first run with unapproved hosted-hello.**

1. `BuildRuntime` loads permissions (no record), registers `hosted-hello` as `Pending`.
2. `StartHostedExtensions` sees no `Ready` hosted extensions → returns immediately.
3. `send("tools", true)` fires, TUI becomes usable in ~200ms. Status line shows `ext: 1!`.
4. User types `/extensions`, panel opens with hosted-hello in Pending.
5. User approves via sub-dialog. Background Cmd: `GrantApproval` writes approvals.json, `StartExtension` launches + handshakes (bounded), transitions to `Running`.
6. Panel refreshes, status line becomes `ext: 1✓`. Extension's `OnReady` registers `/hello` and pushes its status line via `host_call`.

Total user-visible hang time: **zero**.

**Flow B — handshake timeout on a legitimate launch.**

1. `StartExtension` launches process + starts 5s handshake.
2. 5s elapses, `Handshake` returns `ctx.Err()`. `startOneHosted` calls `client.Shutdown` with a 3s bounded context.
3. `Process.Shutdown` closes stdin → well-behaved extension exits; misbehaving one is killed via `cmd.Process.Kill()` at the 3s boundary.
4. State transitions to `Errored`, `lastError = "handshake timeout after 5s"`.
5. Panel shows the ext in Errored with the message. `r` retries.

**Flow C — `/extensions reload` after dropping a new manifest.**

1. User drops `.go-pi/extensions/foo/extension.json` while go-pi is running.
2. User presses `R` in the panel (or runs `/extensions reload`).
3. `ReloadManifests` diffs current state against the re-scanned dirs:
   - New → registered (pending or ready per approvals.json).
   - Missing → stopped if running, removed.
   - Changed runtime or caps → stopped, re-registered as pending.
4. Returns the added/removed id lists for the panel to flash an indicator.

**Flow D — revoke on a running extension.**

1. User selects the row, presses `x`, confirms.
2. `RevokeApproval`: `StopExtension` (bounded), `Permissions.Delete`, transition to `Pending`.
3. `UnregisterExtension` side-effects drop the extension's commands/tools/renderers. Panel refreshes.

### 6. Persistence

**`Permissions.Upsert(path, record)` and `Permissions.Delete(path, id)`.** Both write atomically — temp file in the same directory, fsync, rename — to avoid partial writes on crash. The manager caches `approvalsPath` at construction (`ManagerOptions.ApprovalsPath`) so callers don't re-compute `DefaultApprovalsPath()` on every grant.

**`Denied` state is in-memory only.** A mistaken deny shouldn't force the user to hand-edit `approvals.json` to recover;
after a go-pi restart, a denied extension is back in `Pending`. Users who want a permanent block can remove the
extension directory.

### 7. Testing

**Unit — `internal/extension/manager_test.go`:**

| Test | Assertion |
|--|--|
| `RegisterManifest` routes hosted extensions to `Pending` when no approval exists | Replaces the current "returns error" assertion. |
| `GrantApproval` writes approvals.json and transitions `Pending` → `Ready` | Round-trips through `Permissions.Upsert`. |
| `StartExtension` rejects non-`Ready` states | Typed error. |
| `RestartExtension` is `Stop` + `Start`, updates `startedAt` | — |
| `StartHostedExtensions` partial-failure tolerance | One failing launcher marks that ext `Errored`; the rest start; no returned error. |
| `ReloadManifests` add/remove/change | New added, missing removed, changed re-pended. |
| `RevokeApproval` removes from approvals.json, stops if running, transitions to `Pending` | — |

**Unit — `internal/extension/permissions_test.go`:** `Upsert`/`Delete` round-trip including atomic rename on a fake file system.

**Unit — `internal/extension/hostruntime/process_test.go`:**

| Test | Mechanism |
|--|--|
| `TestProcessShutdown_ClosesStdinFirst` | Extension stub blocks on `stdin.Read`. Shutdown closes stdin. Process exits cleanly. Completes under 500ms. |
| `TestProcessShutdown_KillsOnTimeout` | Extension stub ignores stdin close and signals. Shutdown ctx with 300ms. Verifies `Kill` fired and Wait returned within budget. |
| `TestProcessShutdown_IdempotentAfterExit` | Already-exited process: shutdown returns nil immediately. |

**Integration — `internal/extension/hosted_hello_e2e_test.go`:** the existing test continues to pass (approved extension path unchanged). New sub-test `TestHostedHelloE2E_PendingApprovalDoesNotHang` removes the approval, calls `BuildRuntime`, asserts return within 2s with hosted-hello in `StatePending`. This is the direct regression test for today's bug.

**TUI — `internal/tui/extensions_panel_test.go` (new):**

| Test | Assertion |
|--|--|
| Panel renders grouped sections from a fake manager snapshot | Pending, Running, Stopped, Errored groups in order. |
| `a` on a pending row opens approval sub-dialog | — |
| Enter in sub-dialog emits grant + start Cmds in order | — |
| `r` on an errored row emits restart Cmd | — |
| `/extensions reload` emits `ReloadManifests` Cmd and refreshes rows | — |

**TUI — `internal/tui/status_test.go` (extend):** status line shows `ext: N! M✓ K✗` when counts > 0, hidden otherwise.

**Test doubles.** The existing `fakeHostedLauncher`/`fakeHostedClient` in `manager_test.go` gain `BlockOnHandshake bool` and `FailHandshake error` so hang and failure paths are deterministic. A new `fakeExtensionManager` interface matches the subset of `Manager` methods the panel calls, keeping TUI tests independent of the real manager.

## Migration & compatibility

- `~/.go-pi/extensions/approvals.json` format is unchanged. Existing users' approvals continue to work.
- Declarative and compiled-in extensions are unaffected — they still go straight to `Ready`.
- The existing `extensionDialog` (informational) stays as-is; it's a different dialog type from the approval sub-dialog. `handleExtensionDialogKey` is untouched.
- `internal/extension/hosted_hello_e2e_test.go` continues to run the approved-path scenario.

## Open questions

None remaining.

## Implementation notes

**v2 handshake protocol fix (discovered during implementation).** After the TUI approval flow was wired up, approving
an extension still hung the TUI. Root cause: a pre-existing v2 protocol deadlock. Both the host and extension sent
`HandshakeRequest` messages, and neither side responded to the other's request. The in-process test fakes bypassed the
wire protocol entirely, so the bug was invisible to tests.

Fix: `Client.Handshake` now reads the first raw JSON message from the extension and probes for a `method` field. If
present, the message is an extension-initiated `HandshakeRequest` — the host validates it, builds a `HandshakeResponse`,
and sends it back. If absent (has `result` field instead), it is a response to the host's own request (legacy path used
by in-process fakes). This makes the host handle both extension-initiated and host-initiated flows.

New helpers added to `internal/extension/hostruntime/client.go`: `receiveRaw`, `handleExtensionInitiatedHandshake`,
`handleHostInitiatedHandshake`, `sendHandshakeResponse`.

The e2e test (`TestHostedHello_V2_EndToEnd`) was updated to use a `pipeLauncher` that exercises the `HostedClient`
interface directly (including `ServeInbound`), keeping the test cross-platform without needing a real subprocess.

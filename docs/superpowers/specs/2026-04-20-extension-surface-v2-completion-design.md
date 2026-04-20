---
name: extension-surface-v2-completion
created: 2026-04-20T19:58:04Z
updated: 2026-04-20T19:58:04Z
status: draft
branch: feature/hosted-tool-invocation
---

# Extension Surface v2 Completion — Design

## Motivation

The `feature/hosted-tool-invocation` branch landed the hosted tool surface
(registration, collision rejection, `pi.Ready()`, dynamic approval, adapter
dispatch). The intended Extension Platform v2 surface (spec
`2026-04-11-extension-platform-v2-design.md`) named nine services; five are
unimplemented. This design completes the non-`agent` subset in the same
branch so the three example extensions named in v2 (plan-mode, todos,
session-name) are unblocked on everything except mode/prompt composition.

**In scope:** `state`, `commands`, `ui`, `sigils`, and session metadata
methods (`get_metadata` / `set_name` / `set_tags`).

**Out of scope (separate specs):**

- `agent` service (mode registration + system-prompt composition — real
  design decisions around composition ordering, conflict resolution).
- Tool interception (`intercept` / `on_call` / `on_intercept`).
- The three example extensions themselves.

## Architecture Overview

All five services plug into the existing hosted RPC dispatcher in
`internal/extension/api/hosted.go`. Each service is backed by a host-side
registry (where state is required) and reuses the existing
`host.Dispatcher` + `extension_event` channel for host→extension callbacks.

```
Extension ──host_call──▶ HostedAPIHandler ──▶ { StateStore(+Patch),
                                                CommandRegistry,
                                                UIService,
                                                SigilRegistry,
                                                SessionBridge(+metadata) }
                                                    │
Extension ◀──extension_event──── Dispatcher ◀──────┘
                 (sigils/resolve, sigils/action,
                  ui.dialog.resolved, commands.invoke)
```

Host-side registries follow the same pattern as `HostedToolRegistry`:
per-extension ownership, collision rejection on register, revocation on
extension shutdown.

## Protocol Additions

### `hostproto/protocol.go`

Bump `ProtocolVersion` to `"2.2"`.

New service constants:

```go
const (
    ServiceState    = "state"
    ServiceCommands = "commands"
    ServiceUI       = "ui"
    ServiceSigils   = "sigils"
    // session metadata reuses ServiceSession
)
```

New method constants:

```go
// state
MethodStateGet    = "get"
MethodStateSet    = "set"
MethodStatePatch  = "patch"
MethodStateDelete = "delete"

// commands
MethodCommandsRegister   = "register"
MethodCommandsUnregister = "unregister"
MethodCommandsList       = "list"

// ui
MethodUIStatus       = "status"
MethodUIClearStatus  = "clear_status"
MethodUIWidget       = "widget"
MethodUIClearWidget  = "clear_widget"
MethodUINotify       = "notify"
MethodUIDialog       = "dialog"

// sigils
MethodSigilsRegister   = "register"
MethodSigilsUnregister = "unregister"
MethodSigilsList       = "list"

// session metadata (added to existing session service)
MethodSessionGetMetadata = "get_metadata"
MethodSessionSetName     = "set_name"
MethodSessionSetTags     = "set_tags"
```

New error codes:

```go
ErrCodeCommandNameCollision  = -32096
ErrCodeSigilPrefixCollision  = -32095
ErrCodeDialogCancelled       = -32094
```

Dispatchable events added:

- `commands.invoke` — v1
- `sigils/resolve` — v1
- `sigils/action` — v1
- `ui.dialog.resolved` — v1

Payload structs for every new method (shapes below in per-service sections).

## `state` Service

### Capabilities

`state.read` (covers `get`), `state.write` (covers `set`, `patch`, `delete`).

### Methods

| Method | Payload | Result |
|---|---|---|
| `get` | `{}` | `{value: any, exists: bool}` |
| `set` | `{value: any}` | `{}` |
| `patch` | `{patch: any}` (RFC 7396) | `{}` |
| `delete` | `{}` | `{}` |

### Implementation

Extends existing `internal/extension/state_store.go`. Add:

```go
func (n StateNamespace) Patch(merge json.RawMessage) error
```

implementing RFC 7396 JSON Merge Patch:

- Read current value (empty object if absent).
- Recursively merge: for each key in patch, if value is `null` delete the
  key; if object, recurse; otherwise replace.
- Arrays and non-object values replace wholesale.
- Write back atomically.

`HostedAPIHandler` gains `handleState(method, payload)` dispatching the four
methods against `stateStore.Namespace(h.reg.ID)`. `NewHostedHandler`
accepts a `*StateStore` (already present in the runtime; wire through).

### Isolation

Each extension writes to
`sessions/{sid}/state/extensions/{extID}.json`. No cross-extension access.

## `commands` Service

### Capability

`commands.manage`.

### Methods

| Method | Payload | Result |
|---|---|---|
| `register` | `{name, label, description, arg_hint?}` | `{registered: true}` |
| `unregister` | `{name}` | `{unregistered: true}` |
| `list` | `{}` | `{commands: [{name, owner, source, label, description}]}` |

### Host→Extension Event

`commands.invoke`:

```go
type CommandsInvokeEvent struct {
    Name    string `json:"name"`
    Args    string `json:"args"`
    EntryID string `json:"entry_id"`
}

type CommandsInvokeResult struct {
    Handled bool   `json:"handled"`
    Message string `json:"message,omitempty"`  // optional status text
    Silent  bool   `json:"silent,omitempty"`   // suppress default echo
}
```

### Implementation

New file `internal/extension/api/command_registry.go` with a
`CommandRegistry` mirroring `HostedToolRegistry`:

```go
type CommandRegistry struct {
    mu      sync.RWMutex
    entries map[string]commandEntry // key: name
}

type commandEntry struct {
    Name, Label, Description, ArgHint string
    OwnerID string
    Source  string // "manifest" | "runtime"
    Reg     *host.Registration
}

func (r *CommandRegistry) Add(ownerID string, cmd SlashCommand, source string, reg *host.Registration) error
func (r *CommandRegistry) Remove(ownerID, name string) error
func (r *CommandRegistry) RemoveAllByOwner(ownerID string)
func (r *CommandRegistry) List() []commandEntry
func (r *CommandRegistry) Invoke(ctx context.Context, name, args, entryID string) (CommandsInvokeResult, error)
```

`Add` rejects with `ErrCodeCommandNameCollision` if a different owner holds
the name. Same owner re-register replaces its own entry.

### Coexistence with `cfg.ExtensionCommands`

At runtime startup, the existing static `cfg.ExtensionCommands` are seeded
into `CommandRegistry` with `source = "manifest"` under the owning
extension ID. TUI `internal/tui/commands.go` reads from the registry
instead of `cfg.ExtensionCommands` directly. Runtime register/unregister
layers on with the same collision rule.

When a command is invoked in the TUI, if its entry's owner has a live
hosted registration, `CommandRegistry.Invoke` dispatches
`commands.invoke` via the existing dispatcher path. Otherwise falls
back to the compiled-in behavior that exists today.

## `ui` Service

### Capabilities (separate, per surface)

- `ui.status`
- `ui.widget`
- `ui.notify`
- `ui.dialog`

### Methods

| Method | Payload | Result |
|---|---|---|
| `status` | `{text, style?}` | `{}` |
| `clear_status` | `{}` | `{}` |
| `widget` | `{id, title?, lines: []string, style?, position}` | `{}` |
| `clear_widget` | `{id}` | `{}` |
| `notify` | `{level, text, timeout_ms?}` | `{}` |
| `dialog` | `{title, fields, buttons}` | `{dialog_id}` |

### Position model (widget)

```go
type Position struct {
    Mode    string `json:"mode"`    // static | relative | absolute | sticky | fixed
    Anchor  string `json:"anchor"`  // top | bottom | left | right (for sticky/fixed)
    OffsetX int    `json:"offset_x,omitempty"`
    OffsetY int    `json:"offset_y,omitempty"`
    Z       int    `json:"z,omitempty"`
}
```

Semantics:

- `static` — flow into the extension panel section (default).
- `relative` — panel section with offset applied.
- `absolute` — positioned within chat viewport, offsets from top-left.
- `sticky` — pinned to `anchor` edge of chat viewport as chat scrolls.
- `fixed` — pinned to `anchor` edge of terminal viewport.

Host clamps to viewport; `z` breaks overlap (higher wins, tie → registration
order). Unsupported combinations fall back to `static` with a log warning.

### Dialog (async event-driven)

`ui.dialog` returns immediately with an opaque `{dialog_id}`. Host stores
pending dialog, renders it blocking user input. When user submits or
cancels, host dispatches `ui.dialog.resolved`:

```go
type UIDialogResolvedEvent struct {
    DialogID  string         `json:"dialog_id"`
    Values    map[string]any `json:"values"`
    Cancelled bool           `json:"cancelled"`
    ButtonID  string         `json:"button_id,omitempty"`
}
```

One dialog at a time per session (FIFO queue). Dialogs owned by an
extension are auto-cancelled on extension shutdown or session end (fires
`ui.dialog.resolved` with `cancelled: true`).

Fields supported in v1: `text`, `password`, `choice` (select), `bool`
(checkbox). Buttons: `[{id, label, style?}]`.

### Backing

New `internal/extension/api/ui_service.go` holds per-extension status
slot, widget map, notify queue, dialog queue. TUI reads via new methods on
`SessionBridge` (`SetExtensionStatus`, `SetExtensionWidget`,
`EnqueueNotify`, `ShowDialog`). Dialog resolution flows back through
`HostedAPIHandler` which dispatches the event.

## `sigils` Service

### Capability

`sigils.manage`.

### Syntax

`[[prefix:id]]` — wiki brackets, colon-separated namespaced identifier.
Extensions register one or more prefixes. `prefix` matches `[a-z][a-z0-9-]*`.
`id` is any non-bracket, non-whitespace sequence (opaque to the host).

### Methods

| Method | Payload | Result |
|---|---|---|
| `register` | `{prefixes: []string}` | `{registered: []string}` |
| `unregister` | `{prefixes: []string}` | `{unregistered: []string}` |
| `list` | `{}` | `{prefixes: [{prefix, owner}]}` |

### Host→Extension Events

`sigils/resolve`:

```go
type SigilResolveEvent struct {
    Prefix  string `json:"prefix"`
    ID      string `json:"id"`
    Context string `json:"context,omitempty"` // "chat" | "input" | ...
}

type SigilResolveResult struct {
    Display string         `json:"display"`     // rendered text
    Style   string         `json:"style,omitempty"`
    Hover   string         `json:"hover,omitempty"`
    Actions []string       `json:"actions,omitempty"`
    Meta    map[string]any `json:"meta,omitempty"`
}
```

`sigils/action`:

```go
type SigilActionEvent struct {
    Prefix string `json:"prefix"`
    ID     string `json:"id"`
    Action string `json:"action"` // "click" | "submit" | ext-defined
}

type SigilActionResult struct {
    Handled bool `json:"handled"`
}
```

### Implementation

New package `internal/tui/sigils/` with a parser that scans rendered chat
content for `[[\w[-\w]*:\S+?]]`, a `SigilRegistry` mapping prefix →
extensionID (collision rejection), and a resolution cache (`prefix + id →
SigilResolveResult`, short TTL; invalidated on extension shutdown or
on-demand).

On first render the parser collects unresolved matches and fires
`sigils/resolve` for each in a batch goroutine; the rendered output shows
raw text until resolution lands, then re-renders. Click/keybind on a
rendered sigil triggers `sigils/action`.

Prefix collisions return `ErrCodeSigilPrefixCollision`. Unregistering a
prefix clears cache entries with that prefix.

## Session Metadata (additive)

### Capabilities

`session.metadata.read` (for `get_metadata`), `session.metadata.write`
(for `set_name`, `set_tags`).

### Methods

| Method | Payload | Result |
|---|---|---|
| `get_metadata` | `{}` | `{name, title, tags, created_at, updated_at}` |
| `set_name` | `{name}` | `{}` |
| `set_tags` | `{tags: []string}` | `{}` |

### Session struct changes

In `internal/session/store.go`, add additive fields to the on-disk session
representation:

```go
type sessionFile struct {
    // existing fields...
    Name string   `json:"name,omitempty"`
    Tags []string `json:"tags,omitempty"`
}
```

Readers tolerate missing fields (zero value `""` and `nil`). No migration.

Semantics:

- `name` — stable identifier an extension can set (e.g., a branch-like
  short handle). Distinct from `title` (human summary).
- `tags` — ordered, deduped (on write) string slice.

### SessionBridge

Extend `SessionBridge` interface with:

```go
GetSessionMetadata() piapi.SessionMetadata
SetSessionName(name string) error
SetSessionTags(tags []string) error
```

`piapi.SessionMetadata` = `{Name, Title, Tags, CreatedAt, UpdatedAt}`.

`handleSession` in `hosted.go` gains the three new cases.

## Capability Map Summary

| Service | Capabilities |
|---|---|
| state | `state.read`, `state.write` |
| commands | `commands.manage` |
| ui | `ui.status`, `ui.widget`, `ui.notify`, `ui.dialog` |
| sigils | `sigils.manage` |
| session metadata | `session.metadata.read`, `session.metadata.write` |

All capabilities gated by the existing `host.Gate` in
`HostedAPIHandler.handleHostCall`.

## Testing Strategy

### Unit tests

- `state_store_test.go` — add `TestStateNamespace_Patch` covering merge,
  null-deletes-key, array-replace, nested merge.
- `command_registry_test.go` — register/unregister/collision/owner
  revocation/list.
- `ui_service_test.go` — status per-extension isolation, widget
  position fallback, notify queue ordering, dialog queue + cancellation on
  shutdown.
- `sigils/parser_test.go` — syntax matching (positive + negative cases).
- `sigils/registry_test.go` — prefix collision, owner revocation.
- `session/store_test.go` — old-session-without-name/tags loads fine,
  round-trip with both fields.

### End-to-end tests

Mirror `e2e_hosted_tool_invocation_test.go` style (spawn a hosted Go
fixture extension, exercise the surface, assert host side-effects):

- `e2e_hosted_state_test.go` — set → patch → get → delete; two extensions
  don't see each other's blobs.
- `e2e_hosted_commands_test.go` — register, invoke via TUI seam, assert
  `commands.invoke` dispatched and result returned; collision rejected;
  unregister on shutdown.
- `e2e_hosted_ui_test.go` — status + clear; widget per position mode;
  notify timeout; dialog returns id then resolves via event; dialog
  cancelled on shutdown.
- `e2e_hosted_sigils_test.go` — register two prefixes, render content,
  assert resolve called, assert action round-trip, collision rejection
  between extensions.
- `e2e_hosted_session_metadata_test.go` — set_name → get_metadata;
  set_tags dedupes; old session file without name/tags still loads.

### Fixture extensions

Add minimal Go fixture extensions under `internal/extension/testdata/`
that exercise each surface. These are not the planned example
extensions — just smoke-test harnesses.

## Rollout / Protocol Compat

- Bump `ProtocolVersion` to `"2.2"`.
- Handshake already filters unknown services gracefully; older extensions
  are unaffected.
- TUI features degrade cleanly if no extension uses them (empty widget
  slots, no sigils to resolve, etc.).

## Follow-ups Explicitly Deferred

- `agent` service — separate design doc covering mode registration,
  system-prompt composition ordering, multi-mode conflict resolution.
- Tool interception (`tools.intercept`, `tools.on_call`,
  `tools.on_intercept`) — separate design.
- Example extensions (plan-mode, todos, session-name) — separate branch
  once these services land.

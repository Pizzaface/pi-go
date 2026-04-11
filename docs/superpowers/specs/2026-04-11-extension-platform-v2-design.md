---
name: extension-platform-v2
description: Generic host_call RPC envelope, namespaced service registry, sigil tokens, and three example extensions (plan-mode, todos, session-name)
created: 2026-04-11T00:48:38Z
updated: 2026-04-11T00:48:38Z
---

# Extension Platform v2: Generic Host Calls + Sigils

## Summary

Replace the extension platform's hard-coded RPC method set with a generic `host_call` envelope dispatching to a namespaced service registry. Introduce sigils — structured inline tokens (`[[type:id key=value]]`) that extensions register, the TUI parses and renders as styled chips, with live `resolve` and `action` callbacks. Bump protocol to `2.0.0` as a clean break (the only extant example, `hosted-hello`, is rewritten in the same change). Ship three new example extensions under `examples/extensions/` — `plan-mode`, `todos`, and `session-name` — that each motivate and exercise the new surface end-to-end.

This spec extends [`2026-04-05-rich-tui-extension-platform-design.md`](./2026-04-05-rich-tui-extension-platform-design.md). It replaces the hosted protocol section of that document; everything else (manager, approvals file shape, runtime lifecycle) carries over.

## Goals

1. Replace per-capability RPC methods with a generic `host_call` envelope dispatching to a namespaced service registry.
2. Define the initial service set needed by the three motivating examples: `session`, `agent`, `state`, `commands`, `tools`, `ui`, `chat`, `events`, `sigils`.
3. Ship three example extensions in `examples/extensions/` that each exercise the new surface end-to-end.
4. Bump protocol to `2.0.0`; the host refuses 1.x handshakes.
5. Rewrite `hosted-hello` against the new protocol as a proof that the simplest extension is no harder than before.
6. Introduce sigils as a first-class capability: inline structured tokens in chat text with registration, rendering, resolve, and action support.

## Non-goals

- Codegen / typed client stubs. Deferred until a non-Go extension author appears.
- Runtime service discovery beyond the handshake catalog. No "list available services" RPC at arbitrary points.
- Cross-session state sharing. State remains scoped to `(session_id, extension_id)`.
- New sandboxing or isolation boundaries beyond what v1 already provides.
- Turning plan-mode / todos / session-name into built-in pi-go features. They exist only as examples.
- Web-style hover cards or graphical icons. Terminal-only rendering.
- Parser support for nested sigils (`[[foo:[[bar]]]]` stays invalid).
- A migration tool for third-party 1.x extensions (none in the wild).
- A UI for granting new capabilities interactively; initial approval stays in `approvals.json`.

## Current state

- `internal/extension/hostproto/protocol.go` defines eight methods: `handshake`, `event`, `intent`, `register_command`, `register_tool`, `render`, `health`, `shutdown`, `reload`. Each has its own hand-written payload struct in the same file.
- `examples/extensions/hosted-hello` is the only extension implemented against the protocol and uses three of the eight methods.
- `internal/extension/state_store.go` provides per-session JSON storage per extension, but it is a Go API only — there is no RPC method that stdio extensions can use to reach it.
- The session store has a `sessionID` but no user-facing name or tags.
- The TUI has no concept of an agent "mode"; the system prompt is composed once per turn from `BaseInstruction` and `cwd`.
- There is no parser for inline structured tokens in chat messages.

## Architecture

### Two RPC methods replace eight

The v2 protocol has only two new top-level methods on the wire:

```go
const (
    JSONRPCVersion  = "2.0"
    ProtocolVersion = "2.0.0"

    MethodHandshake = "pi.extension/handshake"  // unchanged semantics, new params
    MethodHostCall  = "pi.extension/host_call"  // ext → host
    MethodExtCall   = "pi.extension/ext_call"   // host → ext (mirror)
    MethodShutdown  = "pi.extension/shutdown"   // unchanged
)
```

Everything that used to be `register_command`, `register_tool`, `intent`, `render`, `health`, `event` is now a `host_call` or an `ext_call` to a named service and method. The v1 methods are deleted — the host rejects any request using them.

### Envelopes

```go
type HostCallParams struct {
    Service string          `json:"service"`
    Method  string          `json:"method"`
    Version int             `json:"version"`
    Payload json.RawMessage `json:"payload,omitempty"`
}

type ExtCallParams struct {
    Service string          `json:"service"`
    Method  string          `json:"method"`
    Version int             `json:"version"`
    Payload json.RawMessage `json:"payload,omitempty"`
}
```

Response shape is vanilla JSON-RPC: `result` (raw JSON the handler produces) or `error` (standard RPC error with code + message). Error codes:

```
-32601  method not found         (unknown service or method)
-32602  invalid params            (unmarshal/validate failure)
-32000  service error             (handler returned error)
-32001  capability not granted    (service used without handshake declaration)
-32002  service not supported     (host doesn't implement this service/version)
```

### Handshake declares the service manifest

The extension declares everything it intends to use during the handshake. The host validates against `approvals.json` and either accepts (with a possibly-trimmed grant list) or rejects before the extension writes a single `host_call`.

```go
type HandshakeRequest struct {
    ProtocolVersion   string           `json:"protocol_version"`  // must be "2.x"
    ExtensionID       string           `json:"extension_id"`
    Mode              string           `json:"mode"`              // "hosted_stdio"
    RequestedServices []ServiceRequest `json:"requested_services"`
}

type ServiceRequest struct {
    Service string   `json:"service"`
    Version int      `json:"version"`
    Methods []string `json:"methods,omitempty"` // optional narrowing
}

type HandshakeResponse struct {
    ProtocolVersion string            `json:"protocol_version"`
    Accepted        bool              `json:"accepted"`
    Message         string            `json:"message,omitempty"`
    GrantedServices []ServiceGrant    `json:"granted_services,omitempty"`
    DeniedServices  []ServiceDenial   `json:"denied_services,omitempty"`
    HostServices    []HostServiceInfo `json:"host_services"`
}
```

The `HostServices` field tells the extension what it can actually use. An extension built against a newer spec can detect missing services at handshake and degrade gracefully instead of failing at call time.

### Service registry (host side)

Each service is a Go type implementing a small interface:

```go
// internal/extension/services/service.go
type Service interface {
    Name() string
    Version() int
    Capabilities() []extension.Capability
    Dispatch(ctx context.Context, call Call) (json.RawMessage, error)
}

type Call struct {
    ExtensionID string
    Method      string
    Version     int
    Payload     json.RawMessage
    Session     *SessionContext
}
```

A `Registry` (sibling to the existing `Manager`) holds the set of services and dispatches `host_call` requests:

```go
// internal/extension/services/registry.go
type Registry struct {
    services map[string]Service
    perms    *extension.Permissions
}

func (r *Registry) Dispatch(ctx context.Context, extID string, params HostCallParams, sess *SessionContext) (json.RawMessage, error) {
    svc, ok := r.services[params.Service]
    if !ok                                                     { return nil, rpcErr(-32002, "unknown service") }
    if svc.Version() < params.Version                          { return nil, rpcErr(-32002, "service version too low") }
    if !r.perms.AllowsService(extID, params.Service, params.Method) { return nil, rpcErr(-32001, "capability not granted") }
    return svc.Dispatch(ctx, Call{ExtensionID: extID, Method: params.Method, Version: params.Version, Payload: params.Payload, Session: sess})
}
```

Each service lives under `internal/extension/services/<name>/service.go`. Adding a new service is one PR touching one directory; no protocol changes.

### Bidirectional mirror

The mirror channel handles everything that needs to flow host→extension: command invocations, tool calls, sigil resolves, sigil actions, lifecycle events. The extension dispatches incoming `ext_call` requests using the same shape as the host — a small in-extension registry.

A new SDK package `internal/extension/sdk` exposes:

```go
func RegisterHandler(service, method string, version int, fn HandlerFunc)
func HostCall(service, method string, version int, payload any) (json.RawMessage, error)
func Serve(ctx context.Context) error
```

Extension authors write handlers, not JSON-RPC plumbing. `hosted-hello` v2 fits in roughly 60 lines.

### Permission integration

The existing `Capability` enum gains new entries:

```go
CapabilitySessionRead     Capability = "session.read"
CapabilitySessionWrite    Capability = "session.write"
CapabilityAgentMode       Capability = "agent.mode"
CapabilityStateRead       Capability = "state.read"
CapabilityStateWrite      Capability = "state.write"
CapabilityChatAppend      Capability = "chat.append"
CapabilitySigilsRegister  Capability = "sigils.register"
CapabilitySigilsResolve   Capability = "sigils.resolve"
CapabilitySigilsAction    Capability = "sigils.action"
```

`AllowsService(extID, service, method)` consults the existing `approvals.json` map. Grants are per-capability-string, so `approvals.json` stays human-readable. Declarative/compiled-in extensions remain trusted by default.

### Sigil pipeline

1. **Registration.** Extension calls `host_call("sigils", "register", 1, {types: [...]})` inside its handshake handler.
2. **Parsing.** When the TUI renders an assistant or user message, it runs the text through `sigils.Parse(text)` (new package `internal/tui/sigils`, ported from the PizzaPi regex + code-range logic).
3. **Rendering.** Each match is rendered as an inline chip: `⟨glyph⟩ label:id` with an ANSI color mapped from the registered `color_hint`. Built-in types ship with sensible defaults.
4. **Resolve.** If the sigil's type has a resolver and the owning extension is alive, the TUI asynchronously issues `ext_call("sigils", "on_resolve", 1, {type, id, params})`. Result can override `label`, add `status_text`, or supply `detail` for a focus-mode footer. Resolves are cached per `(session, type, id, params)`.
5. **Focus & action.** A new TUI focus mode (`focusMode == "sigil"`) lets the user press `Tab` (tentatively) on the last assistant message to navigate sigils; arrow keys move, Enter fires `ext_call("sigils", "on_action", 1, {...})`, Esc exits.
6. **Cleanup.** On extension shutdown, the sigils service drops the extension's registered types and its resolve cache entries. Orphaned sigils render with fallback dim styling.

### Data flow overview

```
┌──────────────┐          handshake           ┌──────────────┐
│  Extension   │ ───────────────────────────▶ │     Host     │
│  (stdio)     │ ◀─── granted/denied + catalog│  (pi-go TUI) │
│              │                              │              │
│              │     host_call (ext → host)   │              │
│              │ ───────────────────────────▶ │   service    │
│              │ ◀────────── result/error ─── │   registry   │
│              │                              │              │
│              │     ext_call (host → ext)    │              │
│              │ ◀─────────────────────────── │  (mirror:    │
│              │ ─────── result/error ──────▶ │   resolve,   │
│              │                              │   action,    │
│              │                              │   events)    │
└──────────────┘                              └──────────────┘
```

## Service inventory (v1)

All services start at version 1. `→` = ext calls host; `←` = host calls ext (mirror).

### `session` — session metadata

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `get_metadata` | → | `session.read` | Returns `{id, name, created_at, updated_at, tags}` |
| `set_name` | → | `session.write` | Sets the session's display name |
| `set_tags` | → | `session.write` | Replaces the tag set |
| `on_change` | ← | — | Fired when any metadata changes |

Implementation note: requires adding `Name` and `Tags` fields to `internal/session/fileSession` and threading them through `FileService`. The TUI renders the name at the top of the chat pane, next to the model name.

### `agent` — mode and prompt fragments

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `get_mode` | → | — | Returns current mode string (`""` = default) |
| `set_mode` | → | `agent.mode` | Switches to a named mode |
| `list_modes` | → | — | Returns registered modes with descriptions |
| `register_mode` | → | `agent.mode` | Declare a mode (name, description, optional system prompt fragment) |
| `unregister_mode` | → | `agent.mode` | Remove a previously-registered mode |
| `on_mode_change` | ← | — | Fired when mode changes (old → new) |

When the agent composes its system prompt, it appends all fragments registered for the current mode *after* the base instruction. Multiple extensions can register fragments for the same mode; they concatenate in registration order. Mode changes take effect on the next turn boundary.

### `state` — per-extension durable state

Exposes the existing `StateStore` over RPC. State is scoped to `(session_id, extension_id)`.

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `get` | → | `state.read` | Returns `{value, exists}` |
| `set` | → | `state.write` | Replaces the blob |
| `patch` | → | `state.write` | Shallow-merges into the top-level map |
| `delete` | → | `state.write` | Removes the blob |

### `commands` — slash commands

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `register` | → | `commands.register` | Declare `{name, description, prompt?, kind}` where `kind ∈ {prompt, callback}` |
| `unregister` | → | `commands.register` | Remove |
| `on_invoke` | ← | — | Fired when user runs a `kind: "callback"` command; params include raw args |

`kind: "prompt"` is the v1 behavior — the command expands to a template sent to the LLM. `kind: "callback"` hands off to the extension via `on_invoke` instead.

### `tools` — LLM-callable tools

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `register` | → | `tools.register` | Declare `{name, description, input_schema}` |
| `unregister` | → | `tools.register` | Remove |
| `intercept` | → | `tools.intercept` | Attach a before/after hook to a host tool |
| `on_call` | ← | — | Fired when the LLM invokes a registered tool; extension returns `{result, is_error?}` |
| `on_intercept` | ← | — | Fired for intercepted host tools; extension returns `{allow, mutated_input?, result_override?}` |

### `ui` — transient UI intents

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `status` | → | `ui.status` | Set a status-line entry (extension_id keyed) |
| `clear_status` | → | `ui.status` | Clear this extension's status entry |
| `widget` | → | `ui.widget` | Show a widget `above_editor` / `below_editor` |
| `clear_widget` | → | `ui.widget` | Clear the widget |
| `notify` | → | `ui.notification` | Fire a transient notification |
| `dialog` | → | `ui.dialog` | Show a modal dialog; returns the user's choice |

### `chat` — transcript manipulation

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `append_message` | → | `chat.append` | Insert `{role ∈ system\|assistant, content, meta?}` at the current tail of the transcript. Not fed back to the LLM on the next turn. |

### `events` — lifecycle

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `on_event` | ← | — | Fired for `{startup, session_start, command_invoked, tool_start, tool_result, tool_error, reload, shutdown}` |

### `sigils` — structured inline tokens

| Method | Dir | Cap | Purpose |
|---|---|---|---|
| `register` | → | `sigils.register` | Declare one or more sigil types |
| `unregister` | → | `sigils.register` | Remove |
| `list` | → | — | Enumerate currently-registered types |
| `on_resolve` | ← | `sigils.resolve` | Resolve a sigil to `{label?, status_text?, detail?, color_hint?}` |
| `on_action` | ← | `sigils.action` | Invoked on Enter in focus mode; returns optional follow-up intent |

A registered sigil type is a `SigilDef`:

```go
type SigilDef struct {
    Type        string          `json:"type"`
    Label       string          `json:"label"`
    Aliases     []string        `json:"aliases,omitempty"`
    Description string          `json:"description,omitempty"`
    Glyph       string          `json:"glyph,omitempty"`      // Nerd Font code point or ASCII
    ColorHint   string          `json:"color_hint,omitempty"` // red|green|yellow|blue|magenta|cyan|white|gray
    Schema      json.RawMessage `json:"schema,omitempty"`
    HasResolve  bool            `json:"has_resolve,omitempty"`
    HasAction   bool            `json:"has_action,omitempty"`
}
```

Built-in types, shipped by pi-go and registered at startup by a compiled-in extension:

| Type | Label | Glyph | Color |
|---|---|---|---|
| `file` | File | `` | cyan |
| `cmd` | Command | `` | yellow |
| `error` | Error | `` | red |
| `cost` | Cost | `$` | green |
| `model` | Model | `` | magenta |
| `session` | Session | `` | blue |
| `time` | Time | `` | gray |

Nerd Font support is opt-in via config (`nerdFontGlyphs: true`, default `false`), not auto-detected — there is no standard terminal query for Nerd Font availability. With glyphs disabled, chips render using a short ASCII badge derived from the type label (e.g. `[file]`, `[todo]`).

### Capability → service matrix

```
session.read         → session.get_metadata
session.write        → session.set_name, session.set_tags
agent.mode           → agent.set_mode, agent.register_mode, agent.unregister_mode
state.read           → state.get
state.write          → state.set, state.patch, state.delete
commands.register    → commands.register, commands.unregister
tools.register       → tools.register, tools.unregister
tools.intercept      → tools.intercept
ui.status            → ui.status, ui.clear_status
ui.widget            → ui.widget, ui.clear_widget
ui.notification      → ui.notify
ui.dialog            → ui.dialog
chat.append          → chat.append_message
sigils.register      → sigils.register, sigils.unregister
sigils.resolve       → (extension offers on_resolve)
sigils.action        → (extension offers on_action)
```

`events.on_event`, `commands.on_invoke`, `tools.on_call`, and the `get_*`/`list_*` methods have no capability gate.

## Example extensions

Each example lives at `examples/extensions/<name>/` with the same file layout as `hosted-hello`: `extension.json`, `main.go`, `README.md`, plus `testdata/` if needed.

### `plan-mode`

Demonstrates: `agent.register_mode`, `agent.set_mode`, `commands.register` (callback), `ui.status`, `chat.append_message`, `events.on_event`.

**Handshake:**
```json
{
  "protocol_version": "2.0.0",
  "extension_id": "plan-mode",
  "mode": "hosted_stdio",
  "requested_services": [
    {"service": "agent",    "version": 1, "methods": ["register_mode","set_mode","unregister_mode"]},
    {"service": "commands", "version": 1, "methods": ["register","unregister"]},
    {"service": "ui",       "version": 1, "methods": ["status","clear_status"]},
    {"service": "chat",     "version": 1, "methods": ["append_message"]}
  ]
}
```

**Approvals entry:**
```json
{
  "extension_id": "plan-mode",
  "trust_class": "hosted_third_party",
  "hosted_required": true,
  "granted_capabilities": ["agent.mode", "commands.register", "ui.status", "chat.append"]
}
```

**Behavior:**
- `/plan` toggles plan mode on/off.
- `/plan done` exits plan mode with a "plan approved, executing" marker.
- When active: status bar shows `plan mode`; the agent's system prompt gains the registered fragment.

**RPC trace — startup → `/plan` → `/plan done`:**
```
1. ext → host  handshake{requested_services:[…]}
2. host → ext  handshake{accepted:true, granted:[…], host_services:[…]}
3. ext → host  host_call agent.register_mode v1 {name:"plan", description:"Planning only; no execution", system_prompt_fragment:"You are in planning mode …"}
4. host → ext  result{ok:true}
5. ext → host  host_call commands.register v1 {name:"plan", description:"Toggle plan mode", kind:"callback"}
6. host → ext  result{ok:true}

// user: /plan
7.  host → ext  ext_call commands.on_invoke v1 {name:"plan", args:[]}
8.  ext → host  host_call agent.set_mode v1 {mode:"plan"}
9.  host → ext  result{previous:""}
10. ext → host  host_call ui.status v1 {text:"plan mode", color:"yellow"}
11. ext → host  host_call chat.append_message v1 {role:"system", content:"**Plan mode on.** I'll propose a plan and wait for approval."}
12. ext → host  result to 7: {handled:true}

// user: /plan done
13. host → ext  ext_call commands.on_invoke v1 {name:"plan", args:["done"]}
14. ext → host  host_call agent.set_mode v1 {mode:""}
15. ext → host  host_call ui.clear_status v1 {}
16. ext → host  host_call chat.append_message v1 {role:"system", content:"**Plan approved.** Executing."}
17. ext → host  result to 13: {handled:true}
```

### `todos`

Demonstrates: `state.*`, `commands.register` (callback), `sigils.register`, `sigils.on_resolve`, `sigils.on_action`, `chat.append_message`.

**Handshake:**
```json
{
  "protocol_version": "2.0.0",
  "extension_id": "todos",
  "mode": "hosted_stdio",
  "requested_services": [
    {"service": "state",    "version": 1},
    {"service": "commands", "version": 1},
    {"service": "sigils",   "version": 1},
    {"service": "chat",     "version": 1, "methods": ["append_message"]}
  ]
}
```

**Approvals entry:**
```json
{
  "extension_id": "todos",
  "trust_class": "hosted_third_party",
  "hosted_required": true,
  "granted_capabilities": ["state.read","state.write","commands.register","sigils.register","sigils.resolve","sigils.action","chat.append"]
}
```

**State shape (`state.set` payload):**
```json
{
  "next_id": 43,
  "items": [
    {"id": 42, "text": "buy milk", "done": false, "created_at": "2026-04-10T12:34:56Z"},
    {"id": 41, "text": "write design spec", "done": true, "created_at": "2026-04-10T11:00:00Z"}
  ]
}
```

**Behavior:**
- `/todo add <text>` — appends an item, echoes `Added [[todo:42]]`.
- `/todo list` — emits `Your todos: [[todo:42]] [[todo:41]]`.
- `/todo done <id>` — marks the todo done.
- Agent can write `[[todo:42]]` anywhere; TUI resolves it to `☐ buy milk` or `☑ buy milk`.
- **Focus mode:** `Tab` enters focus, arrow-key onto a `[[todo:42]]`, Enter toggles `done` via `sigils.on_action`.

**Sigil registration (during handshake handler):**
```json
{
  "types": [{
    "type": "todo",
    "label": "Todo",
    "aliases": ["task"],
    "description": "A todo item tracked by the todos extension",
    "glyph": "",
    "color_hint": "yellow",
    "has_resolve": true,
    "has_action": true,
    "schema": {"type":"object","properties":{"status":{"type":"string","enum":["done","pending"]}}}
  }]
}
```

**RPC trace — `/todo add buy milk`:**
```
1. host → ext  ext_call commands.on_invoke v1 {name:"todo", args:["add","buy","milk"]}
2. ext → host  host_call state.get v1 {}
3. host → ext  result{value:{next_id:42,items:[…]}, exists:true}
4. ext → host  host_call state.set v1 {value:{next_id:43, items:[…]}}
5. host → ext  result{ok:true}
6. ext → host  host_call chat.append_message v1 {role:"system", content:"Added [[todo:42]]"}
7. ext → host  result to 1: {handled:true}
```

**RPC trace — sigil resolve (TUI rendering `[[todo:42]]`):**
```
1. host → ext  ext_call sigils.on_resolve v1 {type:"todo", id:"42", params:{}}
2. ext → host  host_call state.get v1 {}
3. host → ext  result{value:{…}, exists:true}
4. ext → host  result to 1: {label:"buy milk", status_text:"☐", color_hint:"yellow"}
```

**RPC trace — sigil action (Enter on focused `[[todo:42]]`):**
```
1. host → ext  ext_call sigils.on_action v1 {type:"todo", id:"42", params:{}}
2. ext → host  host_call state.get v1 {}
3. host → ext  result{value:{…}, exists:true}
4. ext → host  host_call state.patch v1 {value:{items:[…with id:42 flipped to done:true…]}}
5. host → ext  result{ok:true}
6. ext → host  result to 1: {follow_up:{type:"ui.notify", payload:{level:"info", content:{kind:"text", content:"marked 'buy milk' done"}}}}
```

### `session-name`

Demonstrates: `session.set_name`, `session.get_metadata`, `commands.register` (callback), `sigils.register`, `sigils.on_resolve`, `ui.notify`.

**Handshake:**
```json
{
  "protocol_version": "2.0.0",
  "extension_id": "session-name",
  "mode": "hosted_stdio",
  "requested_services": [
    {"service": "session",  "version": 1, "methods": ["get_metadata","set_name"]},
    {"service": "commands", "version": 1},
    {"service": "sigils",   "version": 1},
    {"service": "ui",       "version": 1, "methods": ["notify"]}
  ]
}
```

**Approvals entry:**
```json
{
  "extension_id": "session-name",
  "trust_class": "hosted_third_party",
  "hosted_required": true,
  "granted_capabilities": ["session.read","session.write","commands.register","sigils.register","sigils.resolve","ui.notification"]
}
```

**Behavior:**
- `/name` — shows current session name (or "untitled").
- `/name <new name>` — renames.
- `[[session:current]]` renders as the current name. On rename, the resolve cache is invalidated via `session.on_change`.

**Sigil registration:**
```json
{
  "types": [{
    "type": "session",
    "label": "Session",
    "glyph": "",
    "color_hint": "blue",
    "has_resolve": true
  }]
}
```

Note: `session` is also a built-in sigil type. When an extension registers a colliding type, its definition wins for as long as the extension is active. On shutdown, the built-in is restored.

**RPC trace — `/name "My project"`:**
```
1. host → ext  ext_call commands.on_invoke v1 {name:"name", args:["My project"]}
2. ext → host  host_call session.set_name v1 {name:"My project"}
3. host → ext  result{previous:"untitled"}
4. ext → host  host_call ui.notify v1 {level:"info", content:{kind:"text", content:"Session renamed to 'My project'"}}
5. ext → host  result to 1: {handled:true}
```

### `hosted-hello` rewrite

The existing example is rewritten against v2 as a sanity check: if the simplest case is *harder* under v2 than v1, the spec has too much ceremony and needs revision. Target: ≤ 65 lines of `main.go` using the new SDK helpers.

### What each example proves

| Example | Proves |
|---|---|
| plan-mode | Mode registration + system-prompt composition; `commands` callback flow; status-line intents; session-scoped chat injection. |
| todos | `state.*` exposes the existing `StateStore` cleanly over RPC; full sigils lifecycle (register + resolve + action); focus mode works end-to-end. |
| session-name | Host-side mutation of core session metadata; sigil override of a built-in type; resolve caching + invalidation via events. |
| hosted-hello (rewrite) | The minimum viable extension isn't harder under v2 than v1. |

## Error handling, edge cases, and migration

### Error taxonomy

| Situation | Code | Caller response |
|---|---|---|
| Unknown service | `-32002` | Check `host_services` from handshake and degrade |
| Service version too low | `-32002` | Degrade gracefully |
| Unknown method on known service | `-32601` | Bug in extension/SDK |
| Payload unmarshal fails | `-32602` | Bug in extension |
| Payload validation fails | `-32602` | Bug in extension |
| Service handler returned error | `-32000` | Extension logs; surfaces via `chat.append_message` if allowed |
| Capability not declared in handshake | `-32001` | Declare it in `requested_services` |
| Capability denied by `approvals.json` | `-32001` | User must grant; extension should `ui.notify` if permitted |
| Extension handler panicked (mirror call) | `-32000` | Host logs stack; session continues |

### Recovery

**Extension side:**
- The SDK wraps each handler in `recover()`. Panics become `-32000` errors on the wire; the process stays alive.
- Unparseable `ext_call` → `-32602` reply, continue.
- EOF on stdin triggers clean shutdown: context cancel, ~500ms grace for in-flight handlers, then exit.

**Host side:**
- Each service handler has a 5-second default per-call timeout (overridable via `Service.Timeout()`). Timeout → `-32000` with `message: "timeout"`.
- If an extension's stdin write buffer blocks for >2s during a mirror call, the host cancels, marks the extension unhealthy, and may restart it.
- Crashes during an `ext_call` surface as `-32000` to the caller and trigger existing lifecycle teardown.

### Sigil edge cases

| Case | Behavior |
|---|---|
| Unregistered sigil type | Parser still matches; renders as gray chip with type name as label. |
| Registering extension disappears mid-session | Types stay visible but dimmed; `on_resolve` is skipped; `on_action` is a no-op with a status notification. |
| Resolve call times out (>1s) | Use cached value if present; otherwise render raw `type:id`. Marked stale so the next render retries. |
| Invalid `color_hint` | Normalized to `gray`, warning logged. |
| Two extensions register the same type | Last-wins for the session; a warning is logged. Previous extension's def is kept but shadowed until the winner unregisters or exits. |
| User focuses a sigil whose owner has no `on_action` | Enter is a no-op; status line shows "sigil is not interactive". |
| Sigil inside fenced code block or inline code span | Skipped by parser. |
| Malformed sigil (`[[foo:]]`, `[[:bar]]`) | No regex match; rendered as literal text. |

### Cross-service consistency

- `agent.set_mode` mid-turn takes effect on the next turn boundary, not the in-flight turn.
- `chat.append_message` always lands at the tail — no retroactive insertion above earlier messages. Rendered with an "extension" badge showing which extension appended it.
- `chat.append_message` content is **not** fed back into the LLM's next-turn context. Extensions cannot manipulate agent memory this way.

### Migration from v1 (clean break)

- Delete from `hostproto/protocol.go`: `MethodEvent`, `MethodIntent`, `MethodRegisterCommand`, `MethodRegisterTool`, `MethodRender`, `MethodHealth`, `MethodReload`. Keep `MethodHandshake` and `MethodShutdown`.
- Add `MethodHostCall`, `MethodExtCall`.
- Bump `ProtocolVersion` from `1.0.0` → `2.0.0`; `ValidateProtocolCompatibility` rejects `1.x`.
- Delete `hostproto.EventPayload`, `hostproto.IntentEnvelope`, `hostproto.CommandRegistration`, `hostproto.ToolRegistration`, `hostproto.RenderPayload`, `hostproto.HealthNotification`, `hostproto.ReloadControl`. Each moves to a service-specific payload under `internal/extension/services/<name>/types.go`.
- `extension.UIIntent` shrinks to an in-process type for the compiled-in TUI renderer only; it no longer appears on the wire.
- `manifest.Manifest` gains an optional `requested_services` field mirroring the handshake declaration.
- `approvals.json` schema is unchanged, but `granted_capabilities` gains the new values listed in §"Permission integration". Old entries carry over verbatim.
- Rewrite `examples/extensions/hosted-hello` against the new protocol.

Every deletion maps one-to-one to a new service method. If during implementation something in the old protocol can't be expressed in the new shape, that's a blocker — stop and revise the spec.

## Testing

### Unit tests (host side)

One `_test.go` per new service, covering happy path, validation errors, capability gates, and version mismatches:

- `internal/extension/services/session/service_test.go`
- `internal/extension/services/agent/service_test.go`
- `internal/extension/services/state/service_test.go`
- `internal/extension/services/commands/service_test.go`
- `internal/extension/services/tools/service_test.go`
- `internal/extension/services/ui/service_test.go`
- `internal/extension/services/chat/service_test.go`
- `internal/extension/services/sigils/service_test.go`

Registry-level tests in `internal/extension/services/registry_test.go`: unknown service, version downgrade, capability gate, concurrent dispatch, timeout enforcement.

### Parser tests

`internal/tui/sigils/parser_test.go` is a near-port of PizzaPi's `parser.test.ts`:
- Simple match, multi-match, with-params, quoted params.
- Skip inside fenced blocks and inline spans.
- Malformed tokens rendered as text.
- Alias resolution via registry.
- Unicode in `id` and params.

### Renderer tests

`internal/tui/sigils/render_test.go`:
- Chip rendering with Nerd Font enabled/disabled.
- `color_hint` → ANSI palette mapping.
- Orphaned-type dim styling.
- Resolve cache hit/miss/invalidation.
- Focus navigation: Tab enters, arrows move, Enter fires action, Esc exits.

### Example extension tests

Each example gets:
- `main_test.go` — handler logic against a fake host via SDK test helpers (no stdio).
- `e2e_test.go` — spawns the real binary with a mock host and asserts the RPC trace matches the spec line-for-line. Gated behind `//go:build e2e` with a runtime skip on Windows (matches the existing `claudecli/e2e_test.go` pattern).

### Cross-cutting integration tests

- `internal/extension/integration_test.go`: spin up `plan-mode` against a real host + stub LLM, verify `/plan` toggles the mode and the system prompt gains the fragment next turn.
- `internal/tui/sigils/integration_test.go`: render a message containing `[[todo:42]]`, verify the parser identifies it, the renderer calls a fake sigils service, and the resolved label shows in output.

### SDK tests

`internal/extension/sdk/sdk_test.go`:
- `RegisterHandler` / `HostCall` / `Serve` round-trip with an in-memory mock stdio pair.
- Handshake auto-negotiation: SDK writes handshake with declared services and crashes if the host denies a required one.
- Panic recovery: handler panic → `-32000` on the wire, process stays alive.

### Explicitly not tested

- Performance / benchmarks.
- Fuzz tests against the parser.
- Multi-extension conflict scenarios beyond the documented last-wins rule for sigils.

## Open questions (carried into implementation)

1. **Status-line multi-extension rendering.** Candidate: key by extension_id, render as `[plan-mode] planning | [todos] 3 open` in insertion order.
2. **Dialog return path.** `ui.dialog` needs a reply from TUI → extension. Candidate: synchronous RPC reply on the original `host_call`, blocking up to 2 minutes.
3. **Sigil focus key binding.** Proposed `Tab` may conflict with autocompletion. Alternative: `Ctrl+[`.
4. **Resolve cache TTL.** Currently "invalidate on `session.on_change`". Cross-extension invalidation for cases like `[[todo:42]]` after `/todo done 42` without the extension firing explicit invalidation is an open question.
5. **Sigil action follow-up intents.** Section "todos / on_action" has the action returning `{follow_up: {type:"ui.notify", payload:{…}}}`. Whether this is a generic "call any service" escape hatch or a narrow enum of allowed follow-ups is undecided.

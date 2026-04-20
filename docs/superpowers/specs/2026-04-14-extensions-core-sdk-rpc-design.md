---
name: extensions-core-sdk-rpc
description: Core SDK + RPC schema that brings pi-mono's ExtensionAPI shape to go-pi
status: draft
created: 2026-04-14T18:40:03Z
updated: 2026-04-14T18:59:22Z
---

# Extensions: Core SDK + RPC Schema (Spec #1)

Foundation spec for reshaping go-pi's extension system around the pi-mono `ExtensionAPI` shape (see
`packages/coding-agent/docs/extensions.md` in `badlogic/pi-mono`). This is spec #1 of a six-spec sequence; the other
five are deferred to their own spec → plan → implementation cycles.

## Sub-project Roadmap

1. **Core SDK + RPC schema** — this spec.
2. `registerCommand` + command events + argument completions + `pi -e <path>` ad-hoc CLI flag for quick-testing a single extension file.
3. Agent-loop & lifecycle events (~15 semantic events with cancel/transform/block return values).
4. Interactive UI surface: `ctx.ui.confirm`, `select`, `input`, `editor`, `notify`, `setStatus`, `setWorkingMessage`, `setWidget`, `setTitle`, `setEditorText`, `pasteToEditor`, `setEditorComponent`, `setFooter`, `getAllThemes`/`getTheme`/`setTheme`, and `ctx.ui.custom<T>()` overlay components.
5. State, messaging, session navigation (`sendMessage`, `appendEntry`, `setLabel`, `waitForIdle`, `newSession`, `fork`, `navigateTree`, `switchSession`, user-facing `/reload`).
6. Extended registrations (`registerShortcut`/`Flag`/`MessageRenderer`, custom editor components, `registerProvider` including OAuth login flows and `streamSimple` custom streaming, `unregisterProvider`).

## 1. Scope & Non-Goals

**In scope:**
- Go `piapi.API` interface + `Context` / `CommandContext` — the shape every handler receives.
- Go compiled-in entrypoint: `func Register(pi piapi.API) error`.
- TS hosted entrypoint: `export default function(pi: ExtensionAPI)`.
- RPC wire protocol v2.1 — extends current v2 with bidirectional dispatch so events flow host→ext with typed return shapes (cancel/transform/block).
- Metadata: `piapi.Metadata` Go struct + `pi.toml` (hosted Go) + `package.json` `"pi"` block (hosted TS).
- Loader & discovery: four-layer last-write-wins with `settings.json` overrides.
- Tiered trust: compiled-in trusted by construction; hosted keeps capability gates + `approvals.json`.
- Reference Node host binary (`@go-pi/extension-host`) + npm SDK package (`@go-pi/extension-sdk`).
- Proof-of-life: one registration (`registerTool`) and one event (`session_start`) wired end-to-end.
- Two demo extensions: `hosted-hello-go` and `hosted-hello-ts`.

**Out of scope (deferred to specs #2–#6):**
- Full event catalog and cancel/transform/block semantics beyond `session_start`.
- `registerCommand/Shortcut/Flag/Provider/MessageRenderer`.
- `ctx.ui.*` interactive surface.
- `sendMessage`, `appendEntry`, `setLabel`, fork/navigateTree/switchSession.
- Custom editor components.
- `npm:` / `git:` package resolution in `settings.json`.

**Deletions (greenfield; no migration):** the existing `internal/extension/` code is thrown out wholesale and replaced. See §11.

## 2. Architecture: Runtime Classes & Transport

Three delivery modes across two transports:

| Mode | Language | Process | Transport | Trust |
|---|---|---|---|---|
| Compiled-in | Go | in-process | direct calls on `piapi.API` | trusted (in the binary) |
| Hosted Go | Go | out-of-process | stdio JSON-RPC v2.1 | gated (capability grants + approvals) |
| Hosted TS | TypeScript | out-of-process | stdio JSON-RPC v2.1 | gated (capability grants + approvals) |

**Shared `piapi.API` interface across all three:**
- Compiled-in: direct Go struct implementing `piapi.API`. Zero serialization.
- Hosted Go: `pkg/piext` provides an RPC-backed implementation of the same interface. The user writes `func Register(pi piapi.API) error` in their Go binary and it works identically to a compiled-in one, except calls go over JSON-RPC.
- Hosted TS: `@go-pi/extension-sdk` is the TS mirror — same shape, different language.

**Entrypoint consistency:**
```go
// compiled-in OR hosted-go
func Register(pi piapi.API) error {
    pi.RegisterTool(...)
    pi.On("session_start", func(evt piapi.Event, ctx piapi.Context) (piapi.EventResult, error) { ... })
    return nil
}
```
```ts
// hosted-ts
export default function (pi: ExtensionAPI) {
  pi.registerTool({...});
  pi.on("session_start", async (evt, ctx) => {...});
}
```

**Mode determination:**

- Compiled-in: the extension package is imported into the go-pi binary and registered in `compiled.Compiled` at build
  time. No metadata file on disk.
- Hosted Go: `pi.toml` declares `runtime = "hosted"` and `command = ["go", "run", "."]` (or a pre-built binary path).
- Hosted TS: `package.json` with a `"pi"` block. Node host spawns it.

**Packaging boundary:** `pkg/piapi` is a separate Go module so external authors can depend on it without pulling the host. `pkg/piext` is likewise separate and depends only on `piapi`.

## 3. Extension API Surface (Spec #1 Subset)

The full surface is declared in spec #1 so naming and shape are locked; only a proof-of-life slice is implemented. Unimplemented methods return `piapi.ErrNotImplemented`.

### `pkg/piapi/api.go`

```go
package piapi

type API interface {
    Name() string
    Version() string

    // Registrations
    RegisterTool(ToolDescriptor) error                         // spec #1 implemented
    RegisterCommand(string, CommandDescriptor) error            // spec #2
    RegisterShortcut(string, ShortcutDescriptor) error          // spec #6
    RegisterFlag(string, FlagDescriptor) error                  // spec #6
    RegisterProvider(string, ProviderDescriptor) error          // spec #6
    UnregisterProvider(name string) error                       // spec #6
    RegisterMessageRenderer(string, RendererDescriptor) error   // spec #6

    // Event subscription
    On(eventName string, handler EventHandler) error            // spec #1: session_start only

    // Inter-extension bus
    Events() EventBus                                           // spec #3

    // Messaging & state
    SendMessage(CustomMessage, SendOptions) error               // spec #5
    SendUserMessage(UserMessage, SendOptions) error             // spec #5
    AppendEntry(kind string, payload any) error                 // spec #5
    SetSessionName(string) error                                // spec #5
    GetSessionName() string                                     // spec #5
    SetLabel(entryID, label string) error                       // spec #5

    // Tool & model management
    GetActiveTools() []string                                   // spec #3
    GetAllTools() []ToolInfo                                    // spec #3
    SetActiveTools([]string) error                              // spec #3
    SetModel(ModelRef) (bool, error)                            // spec #3
    GetThinkingLevel() ThinkingLevel                            // spec #3
    SetThinkingLevel(ThinkingLevel) error                       // spec #3

    // Utilities
    Exec(ctx context.Context, cmd string, args []string, opts ExecOptions) (ExecResult, error) // spec #1
    GetCommands() []CommandInfo                                 // spec #2
    GetFlag(name string) any                                    // spec #6
}

type EventHandler func(evt Event, ctx Context) (EventResult, error)
```

### `pkg/piapi/context.go`

Two context types. Event handlers receive `Context`; command handlers receive `CommandContext`. The split exists because command-only methods (`WaitForIdle`, `NewSession`, `Fork`, `NavigateTree`, `SwitchSession`, `Reload`) deadlock if called from event handlers.

```go
type Context interface {
    HasUI() bool
    CWD() string
    Signal() <-chan struct{}
    Model() ModelRef
    ModelRegistry() ModelRegistry
    IsIdle() bool
    Abort()
    HasPendingMessages() bool
    Shutdown()
    GetContextUsage() *ContextUsage
    GetSystemPrompt() string
    Compact(CompactOptions)
    UI() UI                   // spec #4 stubs (ErrNotImplemented)
    Session() SessionView     // spec #5 stubs
}

type CommandContext interface {
    Context
    WaitForIdle(ctx context.Context) error     // spec #5
    NewSession(NewSessionOptions) (NewSessionResult, error) // spec #5
    Fork(entryID string) (ForkResult, error)    // spec #5
    NavigateTree(targetID string, NavigateOptions) (NavigateResult, error) // spec #5
    SwitchSession(sessionPath string) (SwitchResult, error) // spec #5
    Reload(ctx context.Context) error           // spec #1 plumbing; user-facing in spec #5
}
```

### `pkg/piapi/tools.go`

```go
type ToolDescriptor struct {
    Name             string
    Label            string
    Description      string
    PromptSnippet    string
    PromptGuidelines []string
    Parameters       json.RawMessage // JSON Schema draft-7
    PrepareArguments func(raw json.RawMessage) (json.RawMessage, error) // optional compat shim
    Execute          func(ctx context.Context, call ToolCall, onUpdate UpdateFunc) (ToolResult, error)
    // RenderCall / RenderResult deferred to spec #6
}

type UpdateFunc func(partial ToolResult)
```

### Event shapes (spec #1 only)

```go
type SessionStartEvent struct {
    Reason              string // "startup" | "reload" | "new" | "resume" | "fork"
    PreviousSessionFile string
}
// session_start has no return-value controls; EventResult is ignored by dispatcher.
```

Cancel/transform/block shapes land in spec #3 with the events that need them.

### TS mirror (`@go-pi/extension-sdk`)

```ts
export interface ExtensionAPI {
  name(): string;
  version(): string;

  registerTool(desc: ToolDescriptor): void;
  registerCommand(name: string, desc: CommandDescriptor): void;   // throws NotImplemented in spec #1
  registerShortcut(shortcut: string, desc: ShortcutDescriptor): void;
  registerFlag(name: string, desc: FlagDescriptor): void;
  registerProvider(name: string, config: ProviderConfig): void;
  unregisterProvider(name: string): void;
  registerMessageRenderer(customType: string, renderer: RendererFn): void;

  on<E extends EventName>(event: E, handler: EventHandler<E>): void;
  events: EventBus;

  sendMessage(msg: CustomMessage, opts?: SendOptions): void;
  sendUserMessage(content: string | ContentPart[], opts?: SendOptions): void;
  appendEntry(customType: string, data?: unknown): void;
  setSessionName(name: string): void;
  getSessionName(): string | undefined;
  setLabel(entryId: string, label: string | undefined): void;

  getActiveTools(): string[];
  getAllTools(): ToolInfo[];
  setActiveTools(names: string[]): void;

  setModel(model: ModelRef): Promise<boolean>;
  getThinkingLevel(): ThinkingLevel;
  setThinkingLevel(level: ThinkingLevel): void;

  exec(cmd: string, args: string[], opts?: ExecOptions): Promise<ExecResult>;
  getCommands(): CommandInfo[];
  getFlag(name: string): unknown;
}
```

## 4. Extension Entrypoints & Metadata

### Compiled-in Go

```go
// internal/extensions/hello/hello.go
package hello

import "github.com/pizzaface/go-pi/pkg/piapi"

var Metadata = piapi.Metadata{
    Name: "hello", Version: "0.1.0",
    Description: "Compiled-in hello demo",
    RequestedCapabilities: []string{"tools.register", "events.session_start", "events.tool_execute"},
}

func Register(pi piapi.API) error {
    pi.RegisterTool(piapi.ToolDescriptor{ Name: "greet", /* ... */ })
    pi.On("session_start", func(evt piapi.Event, ctx piapi.Context) (piapi.EventResult, error) {
        return nil, nil
    })
    return nil
}
```

Wired at build time:

```go
// internal/extension/compiled/registry.go
var Compiled = []compiled.Entry{
    {Name: "hello", Register: hello.Register, Metadata: hello.Metadata},
}
```

Compiled-in metadata is trusted as-is.

### Hosted Go

```go
// examples/extensions/hosted-hello-go/main.go
package main

import (
	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{ /* ... */ }

func main() {
    piext.Run(Metadata, func(pi piapi.API) error {
        pi.RegisterTool(piapi.ToolDescriptor{ Name: "greet", /* ... */ })
        return nil
    })
}
```

`piext.Run` handles stdio JSON-RPC handshake and backs `piapi.API` with a transport client. The user's `Register` closure signature is identical to compiled-in.

Metadata in `pi.toml`:
```toml
name = "hosted-hello-go"
version = "0.1.0"
description = "Hosted Go hello"
runtime = "hosted"
command = ["go", "run", "."]
requested_capabilities = ["tools.register", "events.session_start", "events.tool_execute"]
```

### Hosted TS

```ts
// src/index.ts
import type {ExtensionAPI} from "@go-pi/extension-sdk";
import {Type} from "@go-pi/extension-sdk";

export default function (pi: ExtensionAPI) {
  pi.registerTool({
    name: "greet",
    label: "Greet",
    description: "Greet someone",
    parameters: Type.Object({ name: Type.String() }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      return { content: [{ type: "text", text: `Hi, ${params.name}!` }] };
    },
  });
}
```

```json
{
  "name": "hello",
  "version": "0.1.0",
  "dependencies": {
    "@go-pi/extension-sdk": "^0.1.0"
  },
  "pi": {
    "entry": "./src/index.ts",
    "description": "TS hello",
    "requested_capabilities": ["tools.register", "events.session_start", "events.tool_execute"]
  }
}
```

Single-file TS (`~/.go-pi/extensions/hello.ts`) is supported; metadata is inferred (name from filename, no declared
capabilities means none requested).

### `piapi.Metadata`

```go
type Metadata struct {
    Name                  string
    Version               string
    Description           string
    Prompt                string   // appended to system prompt when extension is active
    RequestedCapabilities []string // evaluated against approvals.json for hosted; trusted for compiled-in
    Entry                 string   // hosted only: path or command
}
```

**System prompt injection timing.** `Metadata.Prompt` is appended to the system prompt under a `# Extension: <name>`
heading, matching the current go-pi runtime behavior. Injection is **per-active-extension, per-turn**: the `context`
event (spec #3) assembles the system prompt at the start of every LLM turn, re-reading each registered extension's
current `Metadata.Prompt`. Extensions can mutate their prompt at runtime by returning a modified `Metadata` from a
future `updateMetadata` RPC (not in spec #1); spec #1 treats `Prompt` as static after `Register()` returns.

### Discovery → registration flow

1. `loader.Discover(cwd)` walks the four paths + `settings.json` packages and returns a slice of `loader.Candidate{Mode, Dir, Metadata}`.
2. For each candidate:
   - Compiled-in: lookup in `compiled.Compiled` by name.
   - Hosted Go / TS: resolve `command` (from `pi.toml` or `package.json pi.entry` + Node host).
3. `host.Manager.Register(candidate)` creates the `piapi.API` binding (direct struct for compiled-in, RPC client for hosted) and calls `Register()` in-process or dispatches to the child's entrypoint.
4. For hosted: handshake exchanges `requested_capabilities` vs `approvals.json`; denied extensions go to `pending_approval`.

## 5. RPC Wire Protocol v2.1

Extends current v2 with **bidirectional dispatch** so events flow host→ext with typed return shapes.

### Methods

| Method | Direction | Purpose |
|---|---|---|
| `pi.extension/handshake` | both | Extension declares requested services; host responds with grants + host service catalog |
| `pi.extension/host_call` | ext → host | Extension calls a host service method (existing v2) |
| `pi.extension/subscribe_event` | ext → host (notify) | Extension declares which events it wants dispatched |
| `pi.extension/extension_event` | host → ext | Host dispatches an event; extension returns typed result |
| `pi.extension/tool_update` | ext → host (notify) | Streaming tool progress updates keyed by toolCallId |
| `pi.extension/log` | ext → host (notify) | Diagnostic log redirected from stdout |
| `pi.extension/shutdown` | host → ext | Graceful teardown (existing v2) |

### Handshake

Extension-initiated as before; version bumped to 2.1.

```json
// extension → host
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "pi.extension/handshake",
  "params": {
    "protocol_version": "2.1",
    "extension_id": "hosted-hello",
    "extension_version": "0.1.0",
    "requested_services": [{"service": "tools", "version": 1, "methods": ["register"]}]
  }
}
```

```json
// host → extension
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocol_version": "2.1",
    "granted_services": [{"service": "tools", "version": 1, "methods": ["register"]}],
    "host_services": [
      {"service": "tools", "version": 1, "methods": ["register"]},
      {"service": "events", "version": 1, "methods": ["subscribe"]}
    ],
    "dispatchable_events": [{"name": "session_start", "version": 1}]
  }
}
```

Unapproved capabilities are dropped from `granted_services` (may carry a `denied_reason`).

### Event subscription

```json
{
  "jsonrpc": "2.0",
  "method": "pi.extension/subscribe_event",
  "params": {"events": [{"name": "session_start", "version": 1}]}
}
```

Subscribing to an event requires the corresponding capability (e.g. `events.session_start`). Unsubscribed events are never dispatched.

### Event dispatch

```json
// host → extension
{
  "jsonrpc": "2.0",
  "id": 42,
  "method": "pi.extension/extension_event",
  "params": {
    "event": "session_start",
    "version": 1,
    "payload": {"reason": "startup", "previous_session_file": null},
    "context": {
      "has_ui": true,
      "cwd": "/home/jordan/code/foo",
      "model_ref": {"provider": "anthropic", "id": "claude-opus-4-6"},
      "is_idle": true
    },
    "deadline_ms": 30000
  }
}
```

```json
// extension → host
{"jsonrpc": "2.0", "id": 42, "result": {"control": null}}
```

**Return-value shapes (locked in schema now, used in spec #3+):**
```json
{"control": {"cancel": true}}                                  // session_before_* events
{"control": {"block": true, "reason": "..."}}                  // tool_call
{"control": {"transform": {"content": [...], "details": {}}}}  // tool_result
{"control": {"context": {"messages": [...]}}}                  // context
{"control": {"action": "handled"}}                             // input
```

**Dispatch rules:**
- Per-extension ordered (one event at a time per extension, FIFO).
- Fan-out parallel across extensions; aggregation per-event:
  - `cancel`: any extension returning `{cancel: true}` cancels.
  - `block`: first extension returning `{block: true}` wins; remaining handlers still observe with `was_blocked: true` in context.
  - `transform`: composed left-to-right in subscription order.
- Default timeout 30s per dispatch; repeat timeouts flip the extension to `errored`.

### Tool execution

When the LLM calls an extension-registered tool, the host issues an `extension_event` with event name `tool_execute`, payload `{toolCallId, args, timeout_ms}`, response `{content, details, is_error}`. Streaming `onUpdate` calls become `pi.extension/tool_update` notifications keyed by `toolCallId`.

**Subscription is implicit.** Users do not call `pi.on("tool_execute", ...)`. Calling `RegisterTool` registers the tool with the host *and* automatically subscribes the extension to `tool_execute` dispatches for that tool's `toolCallId`s. The `events.tool_execute` capability is required to complete `RegisterTool` — denial at the gate fails the registration itself, not a later `on()` call.

### Errors

| Code | Name | Meaning |
|---|---|---|
| -32001 | ServiceUnsupported | Service or version not offered by host |
| -32002 | MethodNotFound | Method not in service |
| -32003 | CapabilityDenied | Extension not granted this service.method |
| -32004 | EventNotSupported | Event name unknown or wrong version |
| -32005 | HandlerTimeout | Extension didn't respond within deadline |
| -32006 | HandshakeFailed | Protocol mismatch or malformed handshake |

### Framing & transport

- Line-delimited JSON over stdio.
- Extensions never write to stdout directly — SDK redirects `console.log`/`fmt.Println` to `pi.extension/log`.
- Host buffers child stdout and expects JSON; non-JSON lines flip extension to `errored`.

## 6. Loader & Discovery

### Discovery roots (last-write-wins)

1. `~/.go-pi/packages/*/extensions/*`
2. `~/.go-pi/extensions/*`
3. `.go-pi/packages/*/extensions/*`
4. `.go-pi/extensions/*`
5. `settings.json` → `extensions: [...]` (highest precedence)

### Candidate shapes

| Shape | Detection | Mode |
|---|---|---|
| `P.ts` single file | `.ts` extension | hosted-ts |
| `P/index.ts` | dir + `index.ts` | hosted-ts |
| `P/package.json` with `"pi"` block | dir + package.json parse | hosted-ts |
| `P/pi.toml` or `P/pi.json` | dir + config parse | hosted-go |
| name match in `compiled.Compiled` | startup registry lookup | compiled-in |

Compiled-in names are reserved — a disk candidate with the same name is rejected (hard conflict).

### `settings.json` additions

```json
{
  "packages": [
    "npm:@foo/bar@1.0.0",
    "git:github.com/user/repo@v1",
    "file:/abs/path/to/package"
  ],
  "extensions": [
    "/abs/path/to/standalone.ts",
    "/abs/path/to/standalone-dir"
  ],
  "disabled_extensions": ["name1"]
}
```

Spec #1 implements `file:` packages and absolute `extensions` paths. `npm:` / `git:` schemes are declared and return `ErrNotImplemented` at install time.

### Ad-hoc CLI flag

Spec #2 adds `go-pi -e <path>` (or `--extension`) to register a single extension file for the duration of one
invocation — equivalent to adding it to `settings.json.extensions` for that process only. Useful for quick-testing an
in-development extension without copying files into discovery roots. Not implemented in spec #1; called out here so the
loader interface accommodates it without refactor.

### Node host & TS loading

`@go-pi/extension-host` (npm) is a Node binary (`go-pi-extension-host`):
- Uses `jiti` for on-the-fly `.ts` compilation.
- Resolves `node_modules/` from the extension directory outward.
- Launched per hosted-ts extension: `node <host> --entry <path>`.

### npm package resolution

For `package.json`-bearing extensions the host assumes `node_modules/` is present. go-pi does **not** run `npm install`
automatically. Missing `node_modules/` flips the extension to `errored` with a clear message.

### Vendored host for single-file extensions

- Build-time `go:embed packages/extension-host/dist/**` pulls an esbuild bundle into the binary.
- First run extracts to `~/.go-pi/cache/extension-host/<version>/host.js` and spawns with `node`.
- Requires `node` on PATH. Missing `node` → single-file TS rejected with clear message.

**Build ordering.** The Go build depends on the bundled host existing at `packages/extension-host/dist/`. Build pipeline is: (1) `npm install` + `npm run build` in `packages/extension-host/` produces `dist/host.js`; (2) `go build` embeds it. CI enforces this order; the Go build fails loudly if `dist/` is missing so developers notice immediately. Local development uses a `make build` (or equivalent) that runs both steps.

### Hot-reload plumbing

`loader.Reload(ctx) error` added to the spine:
1. Snapshot current registered extensions.
2. Emit `session_shutdown` (spec #3; spec #1 just disconnects).
3. Re-run `Discover` + `Register`.
4. Emit `session_start` with `reason: "reload"` (spec #3).

Spec #1 wires the bones; user-facing `/reload` lands in spec #5.

### Duplicate command names

When spec #2 lands, multiple extensions registering the same command name keep all registrations with numeric suffixes (`/review:1`, `/review:2`) in load order — declared here for forward compat.

## 7. Tiered Trust & Capability Model

### Trust classes

| Class | Applies to | Gate |
|---|---|---|
| `compiled_in` | In-binary Go | No gate |
| `hosted_first_party` | Marked in `approvals.json` or under `packages/official/*` | Gate enabled; defaults auto-granted; `tools.intercept` / `render.*` still explicit |
| `hosted_third_party` | All other hosted | Gate enabled; every requested capability explicit |

### Capabilities (spec #1 minimum)

Implemented:
- `tools.register`
- `events.session_start`
- `events.tool_execute`
- `exec.shell`

Full catalog locked in schema, evaluated when each spec lands:
```
commands.register                    // spec #2
shortcuts.register                   // spec #6
flags.register                       // spec #6
providers.register / unregister      // spec #6
renderers.register                   // spec #6
messaging.send / send_user           // spec #5
state.append                         // spec #5
session.set_name / get_name          // spec #5
session.set_label                    // spec #5
tools.set_active / get_active        // spec #3
tools.intercept                      // spec #3 (third-party denied by default)
model.set / model.thinking           // spec #3
events.<event_name>                  // one per event in spec #3
ui.status / widget / dialog          // spec #4
render.text / render.markdown        // spec #6
```

### `approvals.json` schema (v2)

```json
{
  "version": 2,
  "extensions": {
    "hosted-hello": {
      "trust_class": "hosted_third_party",
      "first_party": false,
      "approved": true,
      "approved_at": "2026-04-14T...",
      "granted_capabilities": [
        "tools.register",
        "events.session_start",
        "events.tool_execute",
        "exec.shell"
      ],
      "denied_capabilities": []
    }
  }
}
```

Only the v2 schema is recognized; there is no v1 compatibility since this is a greenfield rewrite.

### Evaluation points

1. **Handshake:** `requested_services` ∩ grants.
2. **`host_call`:** re-check (handshake is a cache; `approvals.json` is authority).
3. **`subscribe_event`:** subscription rejected if event capability not granted.
4. **Event dispatch:** no re-check (gated at subscribe time).

### Gate interface

```go
type Gate interface {
    Allowed(extensionID, capability string) (bool, string)
    Grants(extensionID string) []string
}
```

Live-reads `approvals.json` with file-watching so changes take effect without restart. Compiled-in extensions bind to an `alwaysAllow` Gate, so `API` binding needs no special case.

## 8. Node Host + SDK Packaging

### `@go-pi/extension-sdk`

**Location:** `packages/extension-sdk/` in go-pi repo.

**Exports:**
```ts
export type { ExtensionAPI, ExtensionContext, ExtensionCommandContext } from "./api";
export type { ToolDescriptor, ToolCall, ToolResult } from "./tools";
export type { EventName, EventPayload, EventResult } from "./events";
export type { Metadata, ModelRef, ExecOptions, ExecResult } from "./types";
export { Type } from "@sinclair/typebox";
export { connectStdio, Transport } from "./transport";
export { NotImplementedError, CapabilityDeniedError } from "./errors";
```

`@sinclair/typebox` is a peerDependency so extensions get the same version the host uses. Most exports are types; runtime code is transport + error classes + Type re-export.

### `@go-pi/extension-host`

**Location:** `packages/extension-host/`.

**CLI:**
```
go-pi-extension-host --entry <path-to-extension.ts>
                     [--name <override>]
                     [--cwd <dir>]
                     [--log-level debug|info|warn|error]
```

**Responsibilities:**
1. Parse args, resolve entry.
2. Handshake with parent via SDK transport.
3. Load entry via `jiti` with project-local `node_modules/` resolution.
4. Instantiate `ExtensionAPI` implementation; call user's default export.
5. Route `extension_event` → user's `pi.on(...)` subscribers.
6. Route `host_call` → SDK proxy methods.
7. Redirect `console.*` to `pi.extension/log`.
8. Handle `pi.extension/shutdown`.

### `pkg/piapi` (separate Go module)

```
pkg/piapi/
├── go.mod
├── api.go          # API interface
├── context.go      # Context / CommandContext interfaces
├── tools.go        # ToolDescriptor, ToolCall, ToolResult, UpdateFunc
├── events.go       # Event name constants + payload structs
├── metadata.go     # Metadata struct
├── errors.go       # ErrNotImplemented, ErrCapabilityDenied, etc.
└── doc.go
```

### `pkg/piext` (separate Go module)

```
pkg/piext/
├── go.mod          # depends on pkg/piapi
├── run.go          # piext.Run(metadata, register)
├── transport.go    # stdio JSON-RPC v2.1 client
├── rpc_api.go      # piapi.API backed by Transport
├── schema.go       # SchemaFromStruct helper via invopop/jsonschema
└── example_test.go
```

Host-side code imports `pkg/piapi` via root `go.mod` `replace` for local dev.

### Version pinning

- Protocol version `2.1` is the wire contract.
- SDK major versions bump only on breaking `ExtensionAPI` changes.
- go-pi records `min_protocol_version` / `max_protocol_version`. Mismatched extensions refuse to handshake.

## 9. Proof of Life

Two demo extensions replace `examples/extensions/hosted-hello/`:

### `examples/extensions/hosted-hello-go/`

```
hosted-hello-go/
├── go.mod
├── main.go
├── pi.toml
└── README.md
```

See §4 for source.

### `examples/extensions/hosted-hello-ts/`

```
hosted-hello-ts/
├── package.json
├── package-lock.json
├── src/index.ts
└── README.md
```

See §4 for source.

### Acceptance tests (gate for spec #1 completion)

1. **Compiled-in:** in-tree extension registers `greet`; `BuildRuntime` surfaces tool in `Runtime.Tools`; `session_start` fires on startup; tool invocation returns expected content.
2. **Hosted Go:** `go run .` spawn; v2.1 handshake; capabilities granted when approved; tool registration surfaces; `session_start` fires; tool invocation returns content.
3. **Hosted TS:** `go-pi-extension-host --entry ...` spawn (vendored or local host); same assertions as hosted Go.
   Skipped on CI runners without `node`.
4. **Capability denial:** empty `approvals.json`; handshake succeeds with empty `granted_services`; tool registration returns `CapabilityDenied`; extension enters `errored`.
5. **Tiered trust:** compiled-in passes without `approvals.json`; hosted fails with `pending_approval` without approval entry.
6. **Protocol downgrade:** host 2.1, extension 2.0-only → handshake fails with `HandshakeFailed` (no 2.0 compat in spec #1).

## 10. Testing Strategy

### Unit tests (Go)

`pkg/piapi/`:
- `TestMetadata_Validate` — required fields, name regex, capability list well-formed.
- `TestEventResult_Marshal` — all `control` shapes round-trip JSON.

`pkg/piext/`:
- `TestTransport_Handshake` — happy path + version mismatch + malformed first message.
- `TestRPCAPI_RegisterTool` — call lands as `host_call` with correct payload.
- `TestRPCAPI_OnDispatch` — subscribed handler fires; unsubscribed dropped; handler panic → RPC error.
- `TestRun_GracefulShutdown` — shutdown closes transport.

`internal/extension/host/`:
- `TestManager_RegisterAll` — correct order; name collision rejected.
- `TestCapabilityGate_Tiered` — compiled-in always allowed; first-party defaults; third-party explicit.
- `TestDispatch_FanoutOrdering` — cancel/block/transform aggregation.
- `TestDispatch_Timeout` — `HandlerTimeout` error.
- `TestReload` — shutdown → re-discover → re-register; grants re-read.

`internal/extension/loader/`:
- `TestDiscover_LayeredOverrides`.
- `TestDiscover_CompiledInCollision`.
- `TestDiscover_CandidateShapes`.

### Unit tests (TS)

`packages/extension-sdk/`:
- `transport.test.ts`, `api.test.ts`, `errors.test.ts`.

`packages/extension-host/`:
- `jiti-load.test.ts`, `stdout-redirect.test.ts`, `cli.test.ts`.

### E2E tests

Six scenarios from §9.

### Fixtures

```
internal/extension/host/testdata/
├── approvals_empty.json
├── approvals_granted_hello.json
├── approvals_denied_hello.json
└── approvals_v1_legacy.json

packages/extension-sdk/test/fixtures/
├── minimal-extension.ts
├── npm-import-extension/
└── throws-on-load-extension.ts
```

### Coverage targets

- `pkg/piapi`, `pkg/piext`, `internal/extension/{host,loader}` — ≥85% line coverage.
- Transport round-trip paths — ≥95%.

### CI matrix

| Runner | piapi | piext | internal/* | SDK | Host | E2E compiled | E2E go | E2E ts |
|---|---|---|---|---|---|---|---|---|
| Linux Go | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓† |
| Linux Node | — | — | — | ✓ | ✓ | — | — | — |
| Windows Go | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓† |
| macOS Go | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓† |

† skipped (not failed) on runners without `node`.

## 11. Target Layout (Greenfield)

Existing `internal/extension/` is thrown out wholesale. No migration path, no backward-compat shims.

```
pkg/
├── piapi/                   # public Go types (separate go.mod)
└── piext/                   # hosted-Go SDK (separate go.mod)

packages/
├── extension-sdk/           # @go-pi/extension-sdk (npm)
└── extension-host/          # @go-pi/extension-host (npm binary)

internal/extension/
├── api/                     # host-side piapi.API implementations
│   ├── compiled.go
│   └── hosted.go
├── compiled/
│   └── registry.go
├── loader/
│   ├── discover.go
│   ├── candidate.go
│   ├── reload.go
│   └── resources.go
├── host/
│   ├── manager.go
│   ├── rpc.go
│   ├── dispatch.go
│   └── capability.go
├── hostproto/               # v2.1 wire types
│   └── protocol.go
├── mcp.go                   # unchanged
├── provider_registry.go     # unchanged in spec #1
├── state_store.go           # unchanged in spec #1
└── runtime.go               # BuildRuntime — rewired

internal/extensions/         # compiled-in extensions (empty in spec #1 beyond test fixture)

examples/extensions/
├── hosted-hello-go/
└── hosted-hello-ts/

docs/extensions.md           # rewritten
```

**Deleted outright:** every file in current `internal/extension/` not in the layout above; `examples/extensions/hosted-hello/`; old `docs/extensions.md`.

**Build order (dependency-driven, not migration-driven):**
1. `pkg/piapi` module.
2. `pkg/piext` module.
3. `packages/extension-sdk` + `packages/extension-host` npm packages.
4. `internal/extension/{host,loader,api,compiled}`.
5. Rewire `runtime.BuildRuntime`.
6. Delete old files.
7. Add demo extensions.
8. Rewrite `docs/extensions.md`.

No "both systems working" state required.

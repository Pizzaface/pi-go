# Rich TUI Extension Platform Design

Date: 2026-04-05  
Status: Approved in conversation, revised after spec review

## Summary

go-pi currently supports a narrow TUI extension seam via manifest-contributed slash commands. This design expands that into a richer extension platform that preserves go-pi's small-core philosophy while enabling meaningful TUI extensibility.

The recommended architecture is a **hybrid host model**:
- **first-party/internal extensions** run **in-process** via compiled-in Go registration
- **third-party extensions** run **out-of-process** behind a versioned RPC boundary
- both map onto one **canonical extension capability model** so the app has a single architectural contract

The goal is **conceptual parity** with the richer pi-mono extension model, not TypeScript source compatibility.

## Goals

- Add real extension runtime capabilities beyond static manifests
- Support rich TUI behaviors, not just slash-command injection
- Preserve app control over session state, tool policy, and TUI composition
- Isolate third-party extensions behind a process boundary
- Keep first-party extension development fast and ergonomic
- Work in both interactive and non-interactive modes where applicable
- Support reload, failure isolation, and permissioned capabilities

## Non-goals

- Full source compatibility with pi-mono TypeScript extensions
- Letting extensions directly mutate Bubble Tea model internals
- Unlimited arbitrary screen ownership by extensions
- Making provider extensibility the first target of this work
- Using Go's `plugin` package or other ABI-fragile dynamic loading mechanisms

## Current State

Today the rewrite provides:
- manifest discovery from loose directories and packages
- prompt contributions
- lifecycle hooks
- before/after tool hooks
- MCP server registration
- extension-local skills
- narrow `tui.commands` slash-command contributions

This is a useful resource/runtime seam, but not yet a full TUI extension platform.

## Key Decisions

### 1. In-process means compiled-in registration, not dynamic Go plugins

For first-party/internal extensions, **in-process** means compiled Go code that registers itself through an internal extension interface during startup.

It does **not** mean:
- `plugin.Open`
- runtime-loaded `.so` plugins
- ABI-sensitive dynamic linking

Reasoning:
- Go's plugin story is brittle across platforms and build environments
- the repo already favors predictable startup and small-core composition
- compiled-in registration keeps first-party iteration fast while remaining explicit

### 2. Hosted extensions use versioned stdio JSON-RPC

Third-party extensions communicate with go-pi over **JSON-RPC 2.0 over stdio**, using one process per extension.

Reasoning:
- fits local extension execution well
- avoids socket lifecycle complexity for extension hosting
- aligns conceptually with the repo's existing `internal/jsonrpc/` usage and JSON event vocabulary
- easy to supervise, timeout, and restart

A new internal package should define the hosted extension protocol, for example:
- `internal/extension/hostproto`
- `internal/extension/hostruntime`

The protocol must include:
- semantic version handshake
- mode/capability negotiation
- event delivery
- intent responses
- renderer payloads
- health/error reporting

### 3. Hosted rendering uses app-owned structured text payloads

Out-of-process extensions do **not** return raw Bubble Tea components, Lip Gloss code, or arbitrary ANSI layouts.

Instead, hosted renderers return a constrained payload for defined surfaces such as:
- `text`
- `markdown`
- optional small structured sections/items metadata

The app owns final styling, layout, truncation, and fallback rendering.

This keeps rendering practical with Bubble Tea's synchronous render model and prevents hostile or brittle screen ownership.

## Architectural Fit With Small-Core Direction

The architecture adds new pieces, but they should be organized so the default startup path remains predictable.

### New subsystems

1. **Extension manager**
   - location: `internal/extension/`
   - responsibility: discovery, capability registration, lifecycle supervision
   - startup role: **startup-critical**, but only for loading extension metadata and first-party registrants

2. **Hosted extension runtime**
   - location: `internal/extension/hostruntime/`
   - responsibility: child-process management, JSON-RPC transport, handshake, timeouts
   - startup role: **lazy per hosted extension**; only starts for enabled hosted extensions

3. **Capability gateway**
   - location: `internal/extension/`
   - responsibility: validate intents against mode, permissions, and policy
   - startup role: **startup-critical**, but narrow in scope

4. **Event bus**
   - location: `internal/extension/`
   - responsibility: typed fan-out of app/session/tool events to subscribed extensions
   - startup role: **startup-critical**, but internal-only and small

5. **TUI bridge**
   - location: `internal/tui/` plus thin types in `internal/extension/`
   - responsibility: translate approved UI/render intents into Bubble Tea model updates
   - startup role: **interactive-only**

### Small-core rule

The app should still boot fine with:
- no hosted extensions running
- no rich TUI surfaces active
- no third-party permissions granted

In other words, richer extensions add optional behavior, but they do not redefine the product's default bootstrap.

## Core Design

### 1. Extension manager

Introduce an extension manager that:
- discovers manifests and package contributions using the existing resource loading rules
- resolves each extension to a runtime mode:
  - `compiled_in`
  - `hosted_stdio_jsonrpc`
- starts hosted extensions and manages their lifecycle
- tracks registered commands, tools, renderers, event subscriptions, widgets, and health state
- centralizes reload/unload behavior

### 2. Canonical extension API

Define a Go-native capability contract that covers:
- command registration
- tool registration
- lifecycle subscriptions
- tool observation/interception hooks
- TUI capabilities
- message/tool rendering
- state persistence
- permissions and metadata

This is the single conceptual API. In-process extensions implement it directly; hosted extensions implement it through JSON-RPC messages.

### 3. Capability gateway

Extensions should not directly manipulate internal TUI state. Instead, they emit requests through a capability gateway.

Examples:
- register/unregister command
- register/unregister tool
- show dialog
- set status
- mount widget
- contribute message or tool-row renderer
- veto or transform a tool call when authorized
- send a session message

The application validates, scopes, and applies these requests.

### 4. Event bus

Expose a structured event bus so extensions can subscribe to events such as:
- startup
- session start/switch/fork/shutdown
- user input submitted
- command invoked
- tool start/result/error
- model change
- reload/unload

Extensions should subscribe only to the events they need.

### 5. TUI bridge

Keep ownership of Bubble Tea state inside `internal/tui/`.

The TUI bridge translates approved extension intents into model updates. This prevents extensions from poking arbitrary internal state while still allowing rich contribution surfaces.

## BuildRuntime Migration Path

Today, `internal/extension.BuildRuntime()` is the single assembly point for extension/resource loading.

### Proposed migration

`BuildRuntime()` should **remain the top-level assembly boundary**, but its role changes:
- it continues to load manifest-driven resources
- it discovers compiled-in extension registrants
- it creates the extension manager
- it wires the capability gateway and event bus
- it returns a richer runtime object that the CLI/TUI can consume

So the extension manager does **not** replace `BuildRuntime()` as the public startup seam. Instead, `BuildRuntime()` becomes the constructor/orchestrator for the richer runtime.

### New startup sequence

1. build core tools and provider registry
2. discover resource directories and manifests
3. build manifest-driven contributions as today
4. create extension manager
5. register compiled-in extensions
6. start approved hosted extensions
7. finalize runtime with commands/tools/renderers/hooks
8. hand runtime to CLI/TUI startup

That preserves the repo's current shape while letting richer extension behavior grow inside the existing boundary.

## Runtime Model

### Compiled-in extensions

Use for:
- built-in extensions
- first-party/internal extensions
- fast iteration
- features where low latency is valuable

Benefits:
- low overhead
- simpler development
- easy access to internal contracts

Trade-off:
- should not be the default trust model for third-party code

### Hosted extensions

Use for:
- third-party extensions
- restricted/trust-gated capabilities
- stronger crash isolation and permission enforcement

Benefits:
- cleaner security boundary
- better failure isolation
- protocol can become a stable external contract

Trade-off:
- protocol design, lifecycle management, and rendering callbacks are more complex

## Hosted Protocol Sketch

Hosted extensions use **JSON-RPC 2.0 over stdio**.

### Handshake

At startup the app sends a handshake request containing:
- protocol version
- extension id
- app version
- working directory
- mode (`interactive`, `json`, `print`, `rpc`)
- capability mask available in the current mode
- granted permissions

The extension responds with:
- supported protocol versions
- requested subscriptions
- declared commands/tools/renderers
- optional health metadata

### Versioning

- break wire compatibility only on major protocol versions
- allow additive fields in minor versions
- reject startup if there is no compatible major version overlap

### Message classes

- lifecycle events
- session/tool/model/input events
- capability intents
- render requests/responses
- health notifications
- shutdown/reload control

## Data Flow

### Startup

1. Discover extension manifests from existing paths
2. Validate manifest metadata and declared capabilities
3. Resolve runtime mode for each extension
4. Start hosted extensions with handshake and capability negotiation
5. Register approved commands, tools, subscriptions, and render surfaces

### Event handling

1. App emits an event
2. Extension manager fans out only to subscribed extensions
3. Extensions respond with intents
4. Capability gateway validates intents against:
   - trust level
   - granted permissions
   - app mode
   - current session/tool context
5. App applies accepted intents and emits any follow-up events

### Rendering

Extensions do not own the whole screen. They contribute to defined surfaces.

Proposed render surfaces:
- custom message renderer
- tool call row renderer
- tool result renderer
- widget above editor
- widget below editor
- modal/overlay region

Hosted renderers return constrained payloads such as plain text or markdown plus optional small metadata. The app owns final composition and style.

## State Model

Separate state into two categories.

### Durable state

Durable extension state should live in session storage under a dedicated namespace, for example:
- `~/.pi-go/sessions/<session-id>/state/extensions/<extension-id>.json`

If branch-specific state is needed, the stored value should include branch-keyed data rather than inventing a second storage layout immediately.

Used for:
- workflow state
- command/tool state that must survive resume/fork/reload
- extension metadata needed for consistent behavior

The app should expose a small persistence contract:
- `get(key)`
- `set(key, value)`
- `delete(key)`
- serialized as JSON under the extension namespace

### Ephemeral state

In-memory only, used for:
- transient UI state
- renderer-local caches
- active dialog state
- non-persistent widgets

Hosted extensions may cache locally, but durable state remains app-owned.

## Permissions and Trust

### Trust classes

Define three trust classes:
1. `core` — built-in / shipped with app
2. `trusted` — locally installed and explicitly trusted
3. `hosted_third_party` — isolated by default and capability-restricted

### Example capabilities

Extensions may request capabilities such as:
- `commands.register`
- `tools.register`
- `tools.observe`
- `tools.intercept`
- `ui.dialog`
- `ui.status`
- `ui.widget`
- `ui.render.message`
- `ui.render.tool`
- `session.state`
- `session.events`

### Permission storage

Basic permission grants must exist in the first implementation slice, not later.

Store extension approval state in an app-owned file, for example:
- `~/.pi-go/extensions/approvals.json`

This file records, by extension id and version/range as needed:
- trust class
- granted capabilities
- whether hosted execution is required
- last approval timestamp

### Initial policy defaults

Until a richer approval UX exists:
- `core` extensions may use all shipped capabilities
- `trusted` extensions may request most capabilities with explicit approval
- `hosted_third_party` extensions are denied `tools.intercept` by default
- hosted third-party renderers and dialogs require explicit approval

## Failure Handling

### Gate classification

#### Fail closed
Use fail-closed behavior for:
- tool interception that exists to block or transform dangerous actions
- permission checks
- incompatible protocol/version handshakes

#### Fail open
Use fail-open behavior for:
- passive event observers
- status/widget updates
- non-critical notifications
- optional render enrichments when a default renderer exists

### Specific failures

- **Hosted extension crash**: mark unhealthy, notify user, keep app alive
- **Renderer crash**: fall back to default renderer for that surface
- **Passive observer failure**: fail open and log
- **Policy/interception failure**: fail closed when the extension was acting as a safety gate
- **Timeouts**: enforce bounded deadlines for extension callbacks, especially rendering and interception
- **Reload/unload**: detach commands, tools, renderers, and widgets cleanly

## Mode Behavior

### Interactive mode
Full TUI capabilities may be available, subject to permissions.

### Non-interactive modes
Commands, tools, and lifecycle hooks may still work, but TUI-specific capabilities degrade through an explicit mode capability mask rather than silent assumptions.

At handshake/registration time, the app provides which capability classes are available in the current mode.

Examples:
- `ui.*` unavailable in `print` mode
- renderers ignored outside interactive mode
- commands/tools/hooks may remain available in `json` and `rpc` modes where meaningful

Extensions must be able to tell at load time which capability families are available.

## Rollout Plan

### Stage 1a — canonical in-process foundation
- canonical Go extension contract
- extension manager and event bus
- compiled-in registration path
- capability declaration and permission storage
- basic lifecycle events
- BuildRuntime integration

### Stage 1b — hosted transport foundation
- stdio JSON-RPC hosted runtime
- protocol handshake and versioning
- supervised child-process lifecycle
- timeout/error handling for hosted calls

### Stage 2 — behavioral parity
- command registration
- tool registration
- tool observation hooks
- restricted interception hooks
- session-scoped extension state
- non-interactive capability masking

### Stage 3 — TUI parity
- dialogs/status/widgets
- custom message renderers
- tool call/result renderers
- modal/overlay surface
- extension conflict resolution

### Stage 4 — productization
- packaging/distribution story
- richer trust prompts and approval UX
- diagnostics and debug tooling
- developer docs/examples
- conformance and compatibility tests

## Testing Strategy

### Contract tests
A shared suite that validates both compiled-in and hosted extensions against the same logical contract.

### Integration tests
Cover:
- discovery and loading
- runtime mode selection
- permission enforcement
- crash/reload behavior
- registration and deregistration of capabilities
- BuildRuntime assembly path

### TUI behavior tests
Cover:
- command help/completion exposure
- widget composition
- renderer fallback behavior
- overlay/modal lifecycle

### Protocol/golden tests
Cover:
- handshake correctness
- event → intent → applied-state flows
- renderer payload handling
- incompatible protocol negotiation

## v1 Success Criteria

A third-party hosted extension can:
- register a command and a tool
- receive lifecycle and tool events
- show dialogs, status, and widgets in interactive mode
- render a custom message or tool row through constrained payloads
- fail without breaking the app
- reload cleanly

That would represent real TUI extensibility rather than simple slash-command injection.

## Key Architectural Rules

- Keep the app in control of session state, tool policy, and final TUI composition
- Prefer capabilities and intents over direct internal mutation
- Use one conceptual extension model across runtime modes
- Treat isolation as a first-class requirement for third-party extensions
- Preserve go-pi's small-core philosophy by making extensions additive, not invasive

## Recommendation

Proceed with the **hybrid host model**:
- compiled-in registration for first-party/internal extensions
- hosted stdio JSON-RPC for third-party extensions
- constrained, app-owned rendering surfaces
- BuildRuntime retained as the main startup assembly boundary

This best balances:
- fast first-party iteration
- strong third-party isolation
- conceptual parity with a richer extension ecosystem
- compatibility with the current rewrite's minimal, manifest-driven architecture

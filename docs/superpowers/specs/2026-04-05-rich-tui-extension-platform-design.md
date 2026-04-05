# Rich TUI Extension Platform Design

Date: 2026-04-05
Status: Approved in conversation

## Summary

go-pi currently supports a narrow TUI extension seam via manifest-contributed slash commands. This design expands that into a richer extension platform that preserves go-pi's small-core philosophy while enabling meaningful TUI extensibility.

The recommended architecture is a **hybrid host model**:
- **first-party/internal extensions** may run in-process for speed and iteration
- **third-party extensions** should run behind a process boundary
- both are exposed through one **canonical extension capability model** so the app has a single architectural contract

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
- Turning provider registration into the first implementation target
- Unlimited arbitrary screen ownership by extensions

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

## Core Design

### 1. Extension manager

Introduce an extension manager that:
- discovers manifests and package contributions using the existing resource loading rules
- resolves each extension to a runtime mode:
  - `in_process`
  - `hosted`
- starts hosted extensions and manages their lifecycle
- tracks registered commands, tools, renderers, event subscriptions, widgets, and health state

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

This is the single conceptual API. In-process extensions implement it directly; hosted extensions implement it through RPC/protocol messages.

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

## Runtime Model

### In-process extensions

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

This gives meaningful TUI power without letting extensions destabilize the main update/render loop.

## State Model

Separate state into two categories:

### Durable state
Namespaced by:
- extension id
- session id
- branch id when relevant

Used for:
- workflow state
- command/tool state that must survive resume/fork/reload
- extension metadata needed for consistent behavior

### Ephemeral state
In-memory only, used for:
- transient UI state
- renderer-local caches
- active dialog state
- non-persistent widgets

Hosted extensions may cache locally, but durable state should remain app-owned or use an app-controlled persistence contract.

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

Permissions are granted or denied at load time and surfaced clearly to the user.

## Failure Handling

- **Hosted extension crash**: mark unhealthy, notify user, keep app alive
- **Renderer crash**: fall back to default renderer for that surface
- **Passive observer failure**: fail open and log
- **Policy/interception failure**: fail closed for dangerous gates
- **Timeouts**: enforce bounded deadlines for extension callbacks, especially rendering and input hooks
- **Reload/unload**: detach commands, tools, renderers, and widgets cleanly

## Mode Behavior

### Interactive mode
Full TUI capabilities may be available, subject to permissions.

### Non-interactive modes
Commands, tools, and lifecycle hooks may still work, but TUI-specific capabilities degrade gracefully:
- dialogs/widgets/overlays unavailable or no-op
- custom renderers ignored outside interactive mode
- extension code must not assume a TUI exists

## Rollout Plan

### Stage 1 — extension host foundation
- canonical extension contract
- runtime mode selection
- hosted extension handshake/RPC
- capability declaration and permission checks
- basic lifecycle events
- reload/unload plumbing

### Stage 2 — behavioral parity
- command registration
- tool registration
- tool observation/interception hooks
- session-scoped extension state
- non-interactive compatibility behavior

### Stage 3 — TUI parity
- dialogs/status/widgets
- custom message renderers
- tool call/result renderers
- modal/overlay surface
- extension conflict resolution

### Stage 4 — productization
- packaging/distribution story
- trust prompts and approval UX
- diagnostics and debug tooling
- developer docs/examples
- conformance and compatibility tests

## Testing Strategy

### Contract tests
A shared suite that validates both in-process and hosted extensions against the same contract.

### Integration tests
Cover:
- discovery and loading
- runtime mode selection
- permission enforcement
- crash/reload behavior
- registration and deregistration of capabilities

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
- renderer outputs where snapshots are practical

## v1 Success Criteria

A third-party hosted extension can:
- register a command and a tool
- receive lifecycle and tool events
- show dialogs, status, and widgets in interactive mode
- render a custom message or tool row
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

Proceed with the **hybrid host model**. It best balances:
- fast first-party iteration
- strong third-party isolation
- conceptual parity with a richer extension ecosystem
- compatibility with the current rewrite's minimal, manifest-driven architecture

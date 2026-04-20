---
name: hosted-tool-invocation
description: End-to-end invocation, hot reload, and dynamic approval for tools registered by hosted extensions
status: draft
created: 2026-04-20T14:01:52Z
updated: 2026-04-20T14:01:52Z
---

# Hosted Extension Tool Invocation

## 1. Problem

Hosted extensions call `pi.RegisterTool` and that RPC lands in `HostedAPIHandler.registerTool` (`internal/extension/api/hosted.go:112`), which stores the descriptor in a local map and replies `{registered: true}`. Nothing downstream surfaces those descriptors to the ADK agent, so the LLM never sees the tool.

Compiled-in extensions work because `extension.BuildRuntime` calls `Register(api)` synchronously, then walks `extapi.CompiledTools(api)` and appends `NewPiapiToolAdapter(t)` results to `coreTools` before the agent is constructed (`internal/extension/runtime.go:140`). Hosted extensions aren't launched during `BuildRuntime` — `Lifecycle.StartApproved` launches them asynchronously, after the agent's static tool list is already frozen.

This design closes that gap, and in the same stroke enables:

- Hot reload of tools when an extension is stopped, restarted, or re-registers.
- Dynamic approval of not-yet-approved extensions mid-session.
- Mid-turn exposure of new tools.
- A `tools.unregister` wire method for graceful removal without process restart.

## 2. Architecture

### 2.1 Dynamic toolset, not static list

ADK's `tool.Toolset` interface (`google.golang.org/adk@v1.0.0/tool/tool.go:106`) resolves `Tools(ctx)` per invocation. A single `HostedToolset` backed by a mutable registry gives the LLM a fresh snapshot each time the runner asks — hot reload is a free consequence.

The compiled-in path stays unchanged. Static tools remain in `coreTools`; dynamic tools arrive through `Toolsets`.

### 2.2 Components

```
┌─────────────────────────────────────────┐
│ extension process (hosted-hello-go)     │
│                                          │
│  pi.RegisterTool(desc)                  │
│    ├── store Execute closure locally    │
│    └── RPC: tools.register (metadata)   │
│                                          │
│  handleToolExecute(payload)             │
│    └── invoke stored Execute closure    │
└────────────┬────────────────────────────┘
             │ JSON-RPC / stdio
┌────────────▼────────────────────────────┐
│ host process (pi)                       │
│                                          │
│ ┌─────────────────────────────────────┐ │
│ │ HostedAPIHandler                    │ │
│ │   registerTool  → registry.Add      │ │
│ │   unregisterTool → registry.Remove  │ │
│ │   handleToolStreamUpdate → bridge   │ │
│ └─────────────────────────────────────┘ │
│ ┌─────────────────────────────────────┐ │
│ │ HostedToolRegistry (mutable)        │ │
│ │   map[tool_name] → {extID, desc,    │ │
│ │                     reg (*Conn)}    │ │
│ └─────────────────────────────────────┘ │
│ ┌─────────────────────────────────────┐ │
│ │ HostedToolset  (tool.Toolset)       │ │
│ │   Tools(ctx) → snapshot registry    │ │
│ │              → [NewHostedAdapter]   │ │
│ └─────────────────────────────────────┘ │
│ ┌─────────────────────────────────────┐ │
│ │ ADK Runner                          │ │
│ │   coreTools + Toolsets (dynamic)    │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### 2.3 `HostedToolRegistry`

New file `internal/extension/api/hosted_registry.go`.

```go
type HostedToolEntry struct {
    ExtID   string
    Desc    piapi.ToolDescriptor // Execute is nil
    Reg     *host.Registration   // holds Conn
    Manager *host.Manager        // for gating
}

type HostedToolRegistry struct {
    mu    sync.RWMutex
    tools map[string]HostedToolEntry  // key: tool name (global namespace)
}

func (r *HostedToolRegistry) Add(extID string, desc piapi.ToolDescriptor,
                                  reg *host.Registration, mgr *host.Manager) error
func (r *HostedToolRegistry) Remove(extID, toolName string) error
func (r *HostedToolRegistry) RemoveExt(extID string)
func (r *HostedToolRegistry) Snapshot() []HostedToolEntry
func (r *HostedToolRegistry) OnChange(fn func(Change))   // for TUI panel
```

`Add` semantics:

- If no entry for that name: insert, return nil.
- If entry exists and owner is the same extension: **replace** (supports a single extension updating its own tool — required for hot reload of descriptors).
- If entry exists and owner is a different extension: **reject** with a structured error carrying `{conflict_with: existingExtID}`. Caller (HostedAPIHandler) maps this to JSON-RPC error code `-32099` (domain: `ToolNameCollision`) and also emits a `Change{Kind: CollisionRejected}` notification so the TUI extensions panel can render a warning row.

`Remove` semantics:

- Entry exists and owner matches `extID`: delete.
- Entry exists and owner differs: permission error.
- Entry missing: not-an-error (idempotent).

### 2.4 `HostedToolset`

```go
type HostedToolset struct{ r *HostedToolRegistry }

func (t *HostedToolset) Name() string { return "go-pi-hosted-extensions" }

func (t *HostedToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
    snap := t.r.Snapshot()
    out := make([]tool.Tool, 0, len(snap))
    for _, e := range snap {
        adapter, err := NewHostedToolAdapter(e)
        if err != nil {
            continue // malformed descriptor; skip with log
        }
        out = append(out, adapter)
    }
    return out, nil
}
```

### 2.5 `NewHostedToolAdapter`

New file `internal/extension/api/adapter_hosted.go`. Shape mirrors `adapter.go` but the handler crosses the process boundary.

```go
func NewHostedToolAdapter(e HostedToolEntry) (tool.Tool, error) {
    // Parse schema once per adapter creation (cheap; Toolset.Tools re-creates per turn).
    var schema *jsonschema.Schema
    if len(e.Desc.Parameters) > 0 {
        schema = &jsonschema.Schema{}
        if err := json.Unmarshal(e.Desc.Parameters, schema); err != nil {
            return nil, fmt.Errorf("hosted adapter %q: %w", e.Desc.Name, err)
        }
    }
    handler := func(ctx tool.Context, args map[string]any) (map[string]any, error) {
        // 1. Gate check.
        if ok, reason := e.Manager.Gate().Allowed(e.ExtID, "events.tool_execute", e.Reg.Trust); !ok {
            return nil, fmt.Errorf("events.tool_execute denied: %s", reason)
        }
        // 2. Build outbound RPC.
        rawArgs, _ := json.Marshal(args)
        params := hostproto.ExtensionEventParams{
            Event:   "tool_execute",
            Version: 1,
            Payload: mustMarshal(map[string]any{
                "tool_call_id": ctx.FunctionCallID(),
                "name":         e.Desc.Name,
                "args":         json.RawMessage(rawArgs),
                "timeout_ms":   30000,
            }),
        }
        // 3. Dispatch; ctx-cancel aborts the RPC.
        var resp struct {
            Content []piapi.ContentPart `json:"content"`
            Details map[string]any      `json:"details"`
            IsError bool                `json:"is_error"`
        }
        if err := e.Reg.Conn.Call(ctx, hostproto.MethodExtensionEvent, params, &resp); err != nil {
            return nil, err
        }
        // 4. Shape.
        return toolRespToMap(resp), nil
    }
    return functiontool.New[map[string]any, map[string]any](
        functiontool.Config{Name: e.Desc.Name, Description: e.Desc.Description, InputSchema: schema},
        handler,
    )
}
```

Streaming updates aren't the adapter's concern: while the extension executes, it sends `pi.extension/tool_update` notifications keyed by `tool_call_id`. The host handles those in `HostedAPIHandler.handleToolStreamUpdate` and bridges to the TUI via `SessionBridge.EmitToolUpdate`. The adapter's job ends when the final RPC reply arrives.

### 2.6 Wiring

- `BuildRuntime` constructs one `HostedToolRegistry` and places it on `*Runtime`.
- Each `HostedAPIHandler` receives the registry and the extension's `*host.Registration`; `registerTool` calls `registry.Add`, `unregisterTool` calls `registry.Remove`.
- `BuildRuntime` creates one `HostedToolset{registry}` and appends it to the agent config's `Toolsets`.
- `host.Manager` already observes connection close for lifecycle state. Add a close-hook callback that calls `registry.RemoveExt(extID)` so dead extensions don't linger in the toolset.

## 3. Protocol additions

### 3.1 `tools.unregister` (ext → host)

```
Service: tools
Method:  unregister
Payload: {"name": "<tool_name>"}
Response: {"unregistered": true}
Errors:
  -32003 CapabilityDenied  — tools.register not granted
  -32097 ToolNotOwned      — extension did not register this tool
  -32098 ToolNotFound      — no tool with that name
```

Not a new capability; reuses `tools.register` gate. (An extension able to add its own tools should be able to drop them.)

### 3.2 `-32099 ToolNameCollision` (new error domain)

Returned by `tools.register` when the tool name is already owned by a different extension. Payload includes `{conflict_with: "<ext_id>"}` so the TUI extension panel can render a precise diagnostic.

### 3.3 Extension SDK surface

Go (`pkg/piext/rpc_api.go`):

```go
func (a *rpcAPI) UnregisterTool(name string) error
```

TypeScript (`@go-pi/extension-sdk`):

```ts
pi.unregisterTool(name: string): Promise<void>
```

Both drop the local descriptor, send the `tools.unregister` RPC, and return the host's error verbatim.

## 4. Startup barrier

Purpose: avoid a race where the user's first prompt fires before hosted extensions have finished handshaking and registering tools.

### 4.1 Mechanism

- `Lifecycle.StartApproved` already kicks off launch goroutines per extension. Extend it to return a `*HostedReadiness` handle that tracks per-extension state: `{launching, ready, errored, timed_out}`.
- Readiness signal per extension = **whichever fires first** of:
  1. The extension calls `pi.Ready()` (explicit, deterministic — preferred).
  2. Quiescence: no `tools.register`/`tools.unregister` call arrives for `250ms` after handshake (fallback for extensions that don't call `Ready()`).
  3. The `initial_registration` timeout elapses (default `5s`; the extension becomes `timed_out` but the barrier releases so other extensions don't hang the host).
- `Runtime.WaitForHostedReady(ctx, timeout)` (default `5s`) blocks until every launched-at-startup extension is in a terminal state (`ready | errored | timed_out`).

### 4.2 `pi.Ready()` — explicit readiness signal

Extensions that do slow startup work (remote schema fetches, config loads, cache warms) should call `pi.Ready()` as the last step of their `register` function. This tells the host "I'm fully initialized; any tools I intend to register at startup have been registered."

Wire method:

```
Service: ext
Method:  ready
Payload: {}
Response: {"acknowledged": true}
```

Not gated by a capability — it's infrastructure, not a privileged operation.

SDK surface:

```go
// Go (pkg/piext)
func (a *rpcAPI) Ready() error

// TS (@go-pi/extension-sdk)
pi.ready(): Promise<void>
```

Calling `Ready()` is optional but recommended. Extensions that don't call it rely on the 250 ms quiescence heuristic, which is fine for synchronous registration but risks a false-positive ready state during slow async setup.

Once `Ready()` is received, the host marks the extension `ready`, cancels the quiescence timer, and any subsequent `tools.register` calls are treated as normal dynamic registrations (hot reload path) rather than startup registrations. This means `Ready()` is a one-way transition: the extension commits to a baseline toolset at that point.

Example:

```go
func register(pi piapi.API) error {
    schema, err := loadRemoteSchema()  // slow
    if err != nil { return err }
    for _, tool := range schema.Tools {
        if err := pi.RegisterTool(tool); err != nil { return err }
    }
    return pi.Ready()
}
```

### 4.3 Integration points

- TUI (`internal/cli/interactive.go`): before enabling the prompt, show "Loading extensions…" with the list of still-pending IDs. Transition when `WaitForHostedReady` returns.
- CLI headless (`internal/cli/cli.go`): wait silently, but log per-extension timeouts at `WARN`.

Extensions approved **after** startup do not re-arm the barrier — see §5.

## 5. Dynamic approval mid-session

### 5.1 New lifecycle operations

- `Lifecycle.ApproveAndStart(ctx, extID, caps)` — approves, launches, attaches transport. Returns after the extension reaches a terminal readiness state.
- `Lifecycle.Revoke(ctx, extID)` — kills the process, drops caps, calls `registry.RemoveExt(extID)`, unsubscribes dispatcher.

Both are invoked by the existing extension approval TUI panel (`internal/tui/…`). No new UI surface; the panel already knows how to list/approve/revoke.

### 5.2 Turn semantics

When the user approves a new extension **mid-turn**:

- Launch + handshake proceed in background; they do not pause the running LLM step.
- `Toolset.Tools(ctx)` is called by ADK on every LLM request within the turn. Once the new extension's tools land in the registry, the **next LLM step** within the same turn sees them.
- This matches the philosophy of dynamic toolsets: the agent picks up changes at the natural ADK boundary (per-request tool resolution) without needing explicit turn demarcation.

Revoke is symmetric: the moment `registry.RemoveExt` runs, the next `Tools(ctx)` call omits those tools. An in-flight tool call on a revoked extension fails when `Conn.Call` hits the closed connection; surface as a normal tool error to the LLM.

## 6. Extension panel: tools sub-view

### 6.1 Read-only by design

The panel gains a "Tools" sub-view showing:

```
Tool              Owner              Status
────────────────────────────────────────────────
greet             hosted-hello-go    ✓ available
ext_info          hosted-showcase-go ✓ available
ext_rpc_ping      hosted-showcase-go ✓ available
greet             hosted-conflict-x  ⚠ rejected (collision with hosted-hello-go)
```

Data sources: `registry.Snapshot()` for accepted tools, `registry.OnChange` + a ring buffer for rejection history.

### 6.2 Collision handling

Collisions are surfaced, not resolved. When a user hits a collision they must fix the extension. A "force rename" override was considered and **rejected**: it introduces a config-persisted mapping that disagrees with the extension's self-declared metadata, and that divergence causes bug reports that look like "my tool disappeared" when the user forgets the rename is in effect. The simple rule — names are global, first-registered wins — is load-bearing for predictable behavior.

## 7. Error handling

| Situation | Behavior |
|---|---|
| Extension crashes during tool invocation | `Conn.Call` returns error; adapter surfaces `{is_error: true, content: [{text: "extension terminated"}]}` |
| Context cancel (user `Ctrl-C` mid-tool) | `Call(ctx, ...)` aborts, tool returns a cancel error to the LLM |
| Gate denied (events.tool_execute not granted) | Adapter handler returns a tool-level error before dispatching — tool appears in listing but fails on use |
| Timeout (extension doesn't respond within `timeout_ms`) | `Call` returns timeout error; tool errored to LLM; extension **not** flipped to errored (single call timeout ≠ extension failure) |
| Name collision | `tools.register` RPC returns `-32099`; extension's `pi.RegisterTool` returns error; SDK logs; panel shows row |
| Connection close mid-call | `Call` returns io error; treated as a generic tool error |

## 8. Testing

### 8.1 Unit

- `HostedToolRegistry`: add/replace-same-ext/reject-other-ext/remove-owned/remove-not-owned/remove-missing/concurrent add+snapshot.
- `HostedToolset.Tools`: empty, populated, registry mutated between successive calls.
- `NewHostedToolAdapter`: against a fake `host.Registration{Conn: fakeConn}` — success path, gate denial, ctx cancel, malformed schema rejection.
- `tools.unregister` handler: ownership check, not-found idempotency.

### 8.2 Integration

Extend `internal/extension/e2e_hosted_go_spec5_test.go` into `e2e_hosted_go_tool_invocation_test.go`:

1. Launch `hosted-hello-go`, wait for `WaitForHostedReady`.
2. Drive the agent with a stub LLM that emits a `greet` tool call with args `{"name": "pi"}`.
3. Assert the final text contains `"Hello, pi!"`.
4. Hot reload: stop extension → assert `greet` no longer in `Toolset.Tools(ctx)` snapshot.
5. Restart extension exposing a second tool → assert both `greet` and the new tool appear.
6. Unregister: invoke `pi.UnregisterTool("greet")` from within the extension → assert next `Tools(ctx)` omits it.

### 8.3 Collision test

New fixture `examples/extensions/hosted-collide/` registering `greet`. Launch alongside `hosted-hello-go`; assert second registration returns `-32099`, first tool remains available, panel event emitted.

### 8.4 Ready signal test

New fixture `examples/extensions/hosted-slow-ready-go/` that sleeps 300 ms, registers a tool, then calls `pi.Ready()`. Assert:

- With `Ready()` absent and 250 ms quiescence, the extension would have been marked ready prematurely and the tool would be missing from the first-turn snapshot.
- With `Ready()` called after the sleep, `WaitForHostedReady` blocks until the explicit signal lands and the tool is present on turn one.

### 8.5 Dynamic approval test

1. Boot with only `hosted-hello-go` approved.
2. Via Lifecycle API, approve+start `hosted-showcase-go` mid-session.
3. Assert `ext_info` appears in the next `Tools(ctx)` call.
4. Revoke it; assert tools disappear and an in-flight call fails cleanly.

## 9. Migration / compatibility

- No breaking changes to existing extension code. `pi.RegisterTool` keeps its signature; extensions that worked under spec #5 keep working (they just start being actually invocable).
- `HostedAPIHandler.tools` field is retained as a cache but `HostedToolRegistry` becomes the source of truth. The field is what `handleToolStreamUpdate` uses for bookkeeping; it's updated in lockstep with the registry inside `registerTool` / `unregisterTool`.
- Existing tests that assert `HostedAPIHandler.Tools()` contents continue to pass.

## 10. Non-goals

- **Cross-extension tool invocation.** Extensions calling each other's tools. Not required for LLM→tool invocation; reconsider if a real use case emerges.
- **Tool-level permissions.** Per-tool capability gates (approving `greet` but not `ext_shell`). The existing extension-level capability model suffices; finer-grained gating is spec #6+ territory.
- **Per-extension tool namespacing UI.** Rejected in §6.2.

# Hosted Showcase Go Extension

## Summary

A hosted Go extension at `examples/extensions/hosted-showcase-go/` that demonstrates every working Spec #1 API pattern:
multi-tool registration with varying parameter schemas, session_start event subscription, streaming progress updates,
and system introspection. Serves as a reference implementation richer than the existing `hosted-hello-go` fixture.

## Module Layout

```
examples/extensions/hosted-showcase-go/
├── pi.toml       # Extension metadata + requested capabilities
├── go.mod        # Module with piapi/piext replace directives
├── go.sum
└── main.go       # Register() → 4 tools + session_start handler
```

Follows the same structure as `hosted-hello-go`: single `main.go`, `piext.Run()` entrypoint, `pi.toml` for loader
metadata.

## Capabilities

```toml
requested_capabilities = [
    "tools.register",
    "events.session_start",
    "events.tool_execute",
]
```

Same capability set as `hosted-hello-go`. No new capabilities required.

## Tools

### 1. `ext_info` — Extension Self-Report

**Purpose:** Demonstrate a zero-parameter tool that returns extension metadata and runtime state.

**Parameters:** None (empty JSON Schema object).

**Output:** Extension name, version, description, granted capabilities list, Go version, PID, uptime since
session_start.

**API patterns demonstrated:**

- Tool with no arguments
- Accessing package-level state (Metadata, session start time)
- Multi-line structured text output

### 2. `ext_echo` — Argument Validation

**Purpose:** Demonstrate complex JSON Schema parameters with required and optional fields.

**Parameters:**

- `message` (string, required) — text to echo back
- `repeat` (integer, optional, default 1, min 1, max 100) — number of repetitions
- `uppercase` (boolean, optional, default false) — whether to uppercase the output

**Output:** The message repeated `repeat` times, optionally uppercased.

**API patterns demonstrated:**

- `SchemaFromStruct()` with `json` and `jsonschema` struct tags
- Required vs optional fields
- Input validation and error returns (`IsError: true`)
- Default value handling

### 3. `ext_sysinfo` — System Introspection

**Purpose:** Demonstrate a tool that gathers runtime/OS information using Go stdlib.

**Parameters:** None.

**Output:** Hostname, GOOS, GOARCH, Go version, NumCPU, PID, working directory, executable path.

**API patterns demonstrated:**

- Using `os`, `runtime` stdlib packages
- Structured multi-field output
- No `Exec()` needed — pure Go introspection

### 4. `ext_rpc_ping` — Streaming Progress Updates

**Purpose:** Demonstrate the `UpdateFunc` streaming callback by measuring internal operation timing.

**Parameters:**

- `count` (integer, optional, default 3, min 1, max 20) — number of pings

**Behavior:** For each ping iteration, records a timestamp, performs a trivial operation, and calls `UpdateFunc` with
partial progress (e.g., "Ping 1/3: 0.42ms"). After all pings, returns a summary with min/max/avg timing.

**API patterns demonstrated:**

- `UpdateFunc` for streaming partial results to the host
- Timing/benchmarking pattern
- Optional integer parameter with constraints

## Event Handler

### `session_start`

**Behavior:** Records `time.Now()` in a package-level variable (used by `ext_info` for uptime calculation). Logs the
event reason to stderr via `piext.Log()`.

**API patterns demonstrated:**

- `pi.On(piapi.EventSessionStart, ...)` subscription
- `piext.Log()` for host-visible logging
- Cross-concern state sharing (start time used by `ext_info` tool)

## Implementation Notes

- All state (session start time, metadata) is package-level. No global mutable state beyond the start timestamp set once
  in session_start.
- `ext_rpc_ping` measures Go-side operation time, not actual RPC round-trip (the tool execute call is already an inbound
  RPC — we can't self-call). The timing demonstrates the `UpdateFunc` pattern; the values themselves are informational.
- Error handling: `ext_echo` validates `repeat` range and returns `IsError: true` for out-of-bounds values. Other tools
  have no failure modes beyond OS calls.
- The `go.mod` uses the same `replace` directives as `hosted-hello-go` pointing to `../../../pkg/piapi` and
  `../../../pkg/piext`.

# Extensions Core SDK + RPC Schema Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the existing declarative-manifest extension system with a pi-mono-style `ExtensionAPI` surface — a shared Go interface implemented by compiled-in, hosted-Go, and hosted-TS extensions over JSON-RPC v2.1 with bidirectional event dispatch.

**Architecture:** Two separate Go modules (`pkg/piapi` for public types, `pkg/piext` for the hosted-Go SDK). Two npm packages (`@pi-go/extension-sdk` for TS types + transport, `@pi-go/extension-host` for the Node runner binary). Host-side internals split into `internal/extension/{api,compiled,loader,host}`. Tiered trust: compiled-in bypasses the capability gate, hosted goes through `approvals.json` (v2 schema). Greenfield — old `internal/extension/` files are deleted wholesale.

**Tech Stack:** Go 1.22+, TypeScript 5+, Node.js 20+, jiti (TS on-the-fly loading), esbuild (host bundling), invopop/jsonschema (Go schema generation), TypeBox (TS schema), JSON-RPC 2.0 stdio framing.

**Spec:** `docs/superpowers/specs/2026-04-14-extensions-core-sdk-rpc-design.md`

---

## File Structure

**New files (Go):**
- `pkg/piapi/go.mod` — separate Go module
- `pkg/piapi/api.go` — `API` interface
- `pkg/piapi/context.go` — `Context` / `CommandContext` interfaces
- `pkg/piapi/tools.go` — `ToolDescriptor`, `ToolCall`, `ToolResult`, `UpdateFunc`
- `pkg/piapi/events.go` — event name constants + payload structs
- `pkg/piapi/metadata.go` — `Metadata` struct + validation
- `pkg/piapi/errors.go` — `ErrNotImplemented`, `ErrCapabilityDenied`
- `pkg/piapi/doc.go` — package documentation
- `pkg/piext/go.mod` — separate Go module, depends on `pkg/piapi`
- `pkg/piext/run.go` — `Run(metadata, register)` entrypoint
- `pkg/piext/transport.go` — stdio JSON-RPC v2.1 client
- `pkg/piext/rpc_api.go` — `piapi.API` implementation backed by transport
- `pkg/piext/schema.go` — `SchemaFromStruct` helper
- `pkg/piext/log.go` — stdout redirect to `pi.extension/log`
- `internal/extension/api/compiled.go` — direct in-process `piapi.API` implementation
- `internal/extension/api/hosted.go` — RPC-backed `piapi.API` for hosted extensions
- `internal/extension/compiled/registry.go` — `[]Entry{Name, Register, Metadata}` table
- `internal/extension/loader/candidate.go` — `Candidate` + mode detection
- `internal/extension/loader/discover.go` — four-layer walker
- `internal/extension/loader/reload.go` — `Reload(ctx)` orchestration
- `internal/extension/loader/resources.go` — skills/prompts/themes discovery (moved from current location)
- `internal/extension/host/manager.go` — state machine + registration
- `internal/extension/host/rpc.go` — per-extension JSON-RPC server
- `internal/extension/host/dispatch.go` — event fanout + aggregation
- `internal/extension/host/capability.go` — tiered trust `Gate`
- `internal/extension/host/embed.go` — `go:embed` vendored host + extraction
- `internal/extension/hostproto/protocol.go` — rewritten for v2.1 wire types

**New files (TS/npm):**
- `packages/extension-sdk/package.json`
- `packages/extension-sdk/tsconfig.json`
- `packages/extension-sdk/src/api.ts` — `ExtensionAPI` interface
- `packages/extension-sdk/src/tools.ts` — tool types
- `packages/extension-sdk/src/events.ts` — event type map
- `packages/extension-sdk/src/types.ts` — misc shared types
- `packages/extension-sdk/src/errors.ts` — `NotImplementedError`, `CapabilityDeniedError`
- `packages/extension-sdk/src/transport.ts` — stdio transport + Transport class
- `packages/extension-sdk/src/api-impl.ts` — RPC-backed `ExtensionAPI` implementation
- `packages/extension-sdk/src/index.ts` — public exports
- `packages/extension-sdk/test/*.test.ts` — unit tests
- `packages/extension-host/package.json`
- `packages/extension-host/tsconfig.json`
- `packages/extension-host/src/cli.ts` — CLI argparse + entrypoint
- `packages/extension-host/src/loader.ts` — jiti + node_modules resolution
- `packages/extension-host/src/runtime.ts` — instantiate API, call user default export
- `packages/extension-host/build.mjs` — esbuild bundle script
- `packages/extension-host/test/*.test.ts` — unit tests

**New example files:**
- `examples/extensions/hosted-hello-go/go.mod`
- `examples/extensions/hosted-hello-go/main.go`
- `examples/extensions/hosted-hello-go/pi.toml`
- `examples/extensions/hosted-hello-go/README.md`
- `examples/extensions/hosted-hello-ts/package.json`
- `examples/extensions/hosted-hello-ts/src/index.ts`
- `examples/extensions/hosted-hello-ts/README.md`
- `internal/extensions/hello/hello.go` — test fixture compiled-in extension (used only in E2E tests)

**Modified files:**
- `internal/extension/runtime.go` — `BuildRuntime` rewired to use loader + host
- `go.mod` — add `replace` for `pkg/piapi` and `pkg/piext`
- `go.work` (new) — include root module + `pkg/piapi` + `pkg/piext`

**Deleted files:**
- `internal/extension/manifest.go`, `manifest_test.go`
- `internal/extension/hooks.go`, `hooks_test.go`
- `internal/extension/skill_template.go`, `skill_template.md`
- `internal/extension/manager.go`, `manager_test.go` (replaced by `host/manager.go`)
- `internal/extension/registry.go` (replaced by `compiled/registry.go`)
- `internal/extension/permissions.go`, `permissions_test.go` (replaced by `host/capability.go`)
- `internal/extension/events.go`, `intents.go` (replaced by `host/dispatch.go`)
- `internal/extension/packages.go`
- `internal/extension/sdk/` (old) — replaced by `pkg/piext`
- `internal/extension/services/` — replaced by `internal/extension/host/`
- `internal/extension/hostruntime/` — replaced by `internal/extension/host/`
- `internal/extension/resources.go` — moved to `loader/resources.go`
- `internal/extension/state_store.go` — keep until spec #5 uses it (not deleted in #1)
- `internal/extension/mcp.go` — kept unchanged in spec #1
- `internal/extension/provider_registry.go` — kept unchanged in spec #1
- `examples/extensions/hosted-hello/` — replaced by the two new demo directories
- `internal/extension/hosted_hello_e2e_test.go` — rewritten as part of Task 43
- `internal/extension/test_helpers_test.go` — rewritten alongside E2E tests
- `docs/extensions.md` — rewritten in Task 46

---

## Build Ordering

1. Tasks 1-8 — `pkg/piapi` module, no external deps.
2. Tasks 9-14 — `pkg/piext` module, depends on `pkg/piapi`.
3. Tasks 15-20 — `@pi-go/extension-sdk`, no Go deps.
4. Tasks 21-25 — `@pi-go/extension-host`, depends on extension-sdk.
5. Tasks 26-30 — `internal/extension/hostproto` + `loader`, depends on `pkg/piapi`.
6. Tasks 31-35 — `internal/extension/host` (capability, rpc, dispatch, manager, embed), depends on previous.
7. Tasks 36-37 — `internal/extension/api` (compiled, hosted) + `internal/extension/compiled`.
8. Task 38 — Delete legacy files.
9. Task 39 — Rewire `BuildRuntime`.
10. Tasks 40-41 — Demo extensions.
11. Tasks 42-46 — E2E tests.
12. Task 47 — Rewrite `docs/extensions.md`.

Each task ends in a commit; the tree is green at every task boundary.

---

## Task 1: Scaffold `pkg/piapi` module

**Files:**
- Create: `pkg/piapi/go.mod`
- Create: `pkg/piapi/doc.go`
- Create: `go.work`
- Modify: `go.mod`

- [ ] **Step 1: Create the piapi go.mod**

Create `pkg/piapi/go.mod`:
```
module github.com/dimetron/pi-go/pkg/piapi

go 1.22
```

- [ ] **Step 2: Create the piapi package doc**

Create `pkg/piapi/doc.go`:
```go
// Package piapi defines the public types used by pi-go extensions.
//
// This package is imported by:
//   - host-side code in internal/extension to wire implementations,
//   - the hosted-Go SDK in pkg/piext to provide an RPC-backed API,
//   - external extension authors who compile against the interface.
//
// It declares no implementations and has no dependencies beyond the
// standard library, so external consumers can depend on it without
// pulling in the full pi-go host.
package piapi
```

- [ ] **Step 3: Create go.work to stitch the modules locally**

Create `go.work` at repo root:
```
go 1.22

use (
    .
    ./pkg/piapi
    ./pkg/piext
)
```

- [ ] **Step 4: Add replace directive in root go.mod**

Edit `go.mod` to add at the bottom:
```
require github.com/dimetron/pi-go/pkg/piapi v0.0.0

replace github.com/dimetron/pi-go/pkg/piapi => ./pkg/piapi
```

- [ ] **Step 5: Verify build**

Run: `cd pkg/piapi && go build ./... && cd ../.. && go build ./...`
Expected: no errors, both modules compile.

- [ ] **Step 6: Commit**

```bash
rtk git add pkg/piapi/go.mod pkg/piapi/doc.go go.work go.mod
rtk git commit -m "feat(piapi): scaffold pkg/piapi module for public extension types"
```

---

## Task 2: `piapi.Metadata` struct + validation

**Files:**
- Create: `pkg/piapi/metadata.go`
- Create: `pkg/piapi/metadata_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piapi/metadata_test.go`:
```go
package piapi

import "testing"

func TestMetadata_Validate(t *testing.T) {
    cases := []struct {
        name    string
        meta    Metadata
        wantErr bool
    }{
        {"valid minimal", Metadata{Name: "hello", Version: "0.1.0"}, false},
        {"empty name", Metadata{Version: "0.1.0"}, true},
        {"invalid name chars", Metadata{Name: "has spaces", Version: "0.1.0"}, true},
        {"empty version", Metadata{Name: "hello"}, true},
        {"dotted capability", Metadata{Name: "h", Version: "0.1.0", RequestedCapabilities: []string{"tools.register"}}, false},
        {"malformed capability", Metadata{Name: "h", Version: "0.1.0", RequestedCapabilities: []string{"no_dot"}}, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := tc.meta.Validate()
            if (err != nil) != tc.wantErr {
                t.Fatalf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piapi && go test -run TestMetadata_Validate ./...`
Expected: FAIL with "undefined: Metadata" or similar.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piapi/metadata.go`:
```go
package piapi

import (
    "fmt"
    "regexp"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var capRe = regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)

// Metadata describes a single extension.
type Metadata struct {
    Name                  string
    Version               string
    Description           string
    Prompt                string
    RequestedCapabilities []string
    Entry                 string
}

// Validate returns a non-nil error if the metadata is incomplete or
// malformed. Called at registration time for compiled-in extensions and
// at handshake time for hosted ones.
func (m Metadata) Validate() error {
    if !nameRe.MatchString(m.Name) {
        return fmt.Errorf("piapi: invalid name %q (must match %s)", m.Name, nameRe)
    }
    if m.Version == "" {
        return fmt.Errorf("piapi: version is required")
    }
    for _, cap := range m.RequestedCapabilities {
        if !capRe.MatchString(cap) {
            return fmt.Errorf("piapi: malformed capability %q (must be service.method)", cap)
        }
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piapi && go test -run TestMetadata_Validate ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piapi/metadata.go pkg/piapi/metadata_test.go
rtk git commit -m "feat(piapi): Metadata struct with name/version/capability validation"
```

---

## Task 3: `piapi` errors

**Files:**
- Create: `pkg/piapi/errors.go`
- Create: `pkg/piapi/errors_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piapi/errors_test.go`:
```go
package piapi

import (
    "errors"
    "testing"
)

func TestErrNotImplemented_Is(t *testing.T) {
    err := ErrNotImplemented{Method: "RegisterCommand", Spec: "#2"}
    if !errors.Is(err, ErrNotImplementedSentinel) {
        t.Fatal("errors.Is should match sentinel")
    }
    if err.Error() == "" {
        t.Fatal("Error() should include method and spec")
    }
}

func TestErrCapabilityDenied_Is(t *testing.T) {
    err := ErrCapabilityDenied{Capability: "tools.register", Reason: "not approved"}
    if !errors.Is(err, ErrCapabilityDeniedSentinel) {
        t.Fatal("errors.Is should match sentinel")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piapi && go test -run TestErr ./...`
Expected: FAIL with "undefined: ErrNotImplemented".

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piapi/errors.go`:
```go
package piapi

import "errors"

// ErrNotImplementedSentinel is returned via errors.Is by ErrNotImplemented.
var ErrNotImplementedSentinel = errors.New("piapi: not implemented")

// ErrNotImplemented is returned by API methods whose implementation is
// deferred to a later spec. The Spec field identifies which spec
// adds the implementation.
type ErrNotImplemented struct {
    Method string
    Spec   string
}

func (e ErrNotImplemented) Error() string {
    return "piapi: " + e.Method + " not implemented (deferred to spec " + e.Spec + ")"
}

func (e ErrNotImplemented) Is(target error) bool {
    return target == ErrNotImplementedSentinel
}

// ErrCapabilityDeniedSentinel is returned via errors.Is by ErrCapabilityDenied.
var ErrCapabilityDeniedSentinel = errors.New("piapi: capability denied")

// ErrCapabilityDenied is returned when a host_call or event subscription
// is rejected because the extension was not granted the capability.
type ErrCapabilityDenied struct {
    Capability string
    Reason     string
}

func (e ErrCapabilityDenied) Error() string {
    s := "piapi: capability denied: " + e.Capability
    if e.Reason != "" {
        s += " (" + e.Reason + ")"
    }
    return s
}

func (e ErrCapabilityDenied) Is(target error) bool {
    return target == ErrCapabilityDeniedSentinel
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piapi && go test -run TestErr ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piapi/errors.go pkg/piapi/errors_test.go
rtk git commit -m "feat(piapi): ErrNotImplemented and ErrCapabilityDenied error types"
```

---

## Task 4: `piapi` event types

**Files:**
- Create: `pkg/piapi/events.go`
- Create: `pkg/piapi/events_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piapi/events_test.go`:
```go
package piapi

import (
    "encoding/json"
    "testing"
)

func TestSessionStartEvent_Name(t *testing.T) {
    evt := SessionStartEvent{Reason: "startup"}
    if evt.EventName() != EventSessionStart {
        t.Fatalf("EventName() = %q, want %q", evt.EventName(), EventSessionStart)
    }
}

func TestEventResult_MarshalControl(t *testing.T) {
    cases := []struct {
        name    string
        result  EventResult
        wantKey string
    }{
        {"nil", EventResult{}, ""},
        {"cancel", EventResult{Control: &EventControl{Cancel: true}}, "cancel"},
        {"block", EventResult{Control: &EventControl{Block: true, Reason: "nope"}}, "block"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            b, err := json.Marshal(tc.result)
            if err != nil {
                t.Fatal(err)
            }
            if tc.wantKey != "" && !contains(string(b), tc.wantKey) {
                t.Fatalf("Marshal(%+v) = %s; expected %q key", tc.result, b, tc.wantKey)
            }
        })
    }
}

func contains(s, sub string) bool {
    for i := 0; i+len(sub) <= len(s); i++ {
        if s[i:i+len(sub)] == sub {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piapi && go test -run TestSessionStart ./...`
Expected: FAIL with "undefined: SessionStartEvent".

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piapi/events.go`:
```go
package piapi

// Event name constants. Each event has a single stable string name used
// in both the Go API (pi.On("session_start", ...)) and on the wire.
const (
    EventSessionStart = "session_start"
    EventToolExecute  = "tool_execute"
    // Future events (declared here so spec #3 doesn't need renames):
    EventSessionBeforeSwitch  = "session_before_switch"
    EventSessionBeforeFork    = "session_before_fork"
    EventSessionBeforeCompact = "session_before_compact"
    EventSessionCompact       = "session_compact"
    EventSessionBeforeTree    = "session_before_tree"
    EventSessionTree          = "session_tree"
    EventSessionShutdown      = "session_shutdown"
    EventResourcesDiscover    = "resources_discover"
    EventBeforeAgentStart     = "before_agent_start"
    EventAgentStart           = "agent_start"
    EventAgentEnd             = "agent_end"
    EventTurnStart            = "turn_start"
    EventTurnEnd              = "turn_end"
    EventMessageStart         = "message_start"
    EventMessageUpdate        = "message_update"
    EventMessageEnd           = "message_end"
    EventContext              = "context"
    EventBeforeProviderReq    = "before_provider_request"
    EventToolExecutionStart   = "tool_execution_start"
    EventToolExecutionUpdate  = "tool_execution_update"
    EventToolExecutionEnd     = "tool_execution_end"
    EventToolCall             = "tool_call"
    EventToolResult           = "tool_result"
    EventUserBash             = "user_bash"
    EventInput                = "input"
    EventModelSelect          = "model_select"
)

// Event is the interface implemented by all event payload structs.
// Handlers receive the concrete type; subscribers can type-assert.
type Event interface {
    EventName() string
}

// SessionStartEvent fires when a session is started, loaded, or reloaded.
// Spec #1 implements only this event end-to-end.
type SessionStartEvent struct {
    Reason              string `json:"reason"`
    PreviousSessionFile string `json:"previous_session_file,omitempty"`
}

func (SessionStartEvent) EventName() string { return EventSessionStart }

// EventControl carries the return-value shape for events that support
// cancel/block/transform semantics. Each event type documents which
// fields it honors; fields not documented for that event are ignored.
//
// Spec #1 only emits session_start which ignores all control fields;
// the struct is defined now so the wire format is locked.
type EventControl struct {
    Cancel    bool            `json:"cancel,omitempty"`
    Block     bool            `json:"block,omitempty"`
    Reason    string          `json:"reason,omitempty"`
    Transform *ToolResult     `json:"transform,omitempty"`
    Action    string          `json:"action,omitempty"`
    // Free-form payload for event-specific shapes (e.g. context.messages).
    Extras map[string]any `json:"-"`
}

// EventResult is the return value of an event handler. Nil Control means
// observe-only.
type EventResult struct {
    Control *EventControl `json:"control,omitempty"`
}

// EventHandler is the signature every event subscriber implements.
type EventHandler func(evt Event, ctx Context) (EventResult, error)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piapi && go test -run TestSessionStart ./... && go test -run TestEventResult ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piapi/events.go pkg/piapi/events_test.go
rtk git commit -m "feat(piapi): event name constants, SessionStartEvent, EventControl"
```

---

## Task 5: `piapi` tool types

**Files:**
- Create: `pkg/piapi/tools.go`
- Create: `pkg/piapi/tools_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piapi/tools_test.go`:
```go
package piapi

import (
    "context"
    "encoding/json"
    "testing"
)

func TestToolDescriptor_Validate(t *testing.T) {
    valid := ToolDescriptor{
        Name:        "greet",
        Label:       "Greet",
        Description: "Greet someone",
        Parameters:  json.RawMessage(`{"type":"object"}`),
        Execute: func(_ context.Context, _ ToolCall, _ UpdateFunc) (ToolResult, error) {
            return ToolResult{}, nil
        },
    }
    if err := valid.Validate(); err != nil {
        t.Fatalf("valid tool failed validation: %v", err)
    }

    bad := valid
    bad.Name = "has spaces"
    if err := bad.Validate(); err == nil {
        t.Fatal("invalid name should fail validation")
    }

    noExec := valid
    noExec.Execute = nil
    if err := noExec.Validate(); err == nil {
        t.Fatal("missing Execute should fail validation")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piapi && go test -run TestToolDescriptor ./...`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piapi/tools.go`:
```go
package piapi

import (
    "context"
    "encoding/json"
    "fmt"
    "regexp"
)

var toolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ToolCall is the payload delivered to a tool's Execute function.
type ToolCall struct {
    ID   string          `json:"id"`
    Name string          `json:"name"`
    Args json.RawMessage `json:"args"`
}

// ContentPart is a single fragment of tool output.
type ContentPart struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
}

// ToolResult is what Execute returns.
type ToolResult struct {
    Content []ContentPart  `json:"content"`
    Details map[string]any `json:"details,omitempty"`
    IsError bool           `json:"is_error,omitempty"`
}

// UpdateFunc is the streaming-progress callback passed to Execute.
// Nil means the caller is not listening for updates.
type UpdateFunc func(partial ToolResult)

// ToolDescriptor is the registration payload for pi.RegisterTool.
type ToolDescriptor struct {
    Name             string
    Label            string
    Description      string
    PromptSnippet    string
    PromptGuidelines []string
    Parameters       json.RawMessage
    PrepareArguments func(raw json.RawMessage) (json.RawMessage, error)
    Execute          func(ctx context.Context, call ToolCall, onUpdate UpdateFunc) (ToolResult, error)
}

// Validate returns non-nil if the descriptor is missing required fields.
func (d ToolDescriptor) Validate() error {
    if !toolNameRe.MatchString(d.Name) {
        return fmt.Errorf("piapi: invalid tool name %q", d.Name)
    }
    if d.Description == "" {
        return fmt.Errorf("piapi: tool %q: description is required", d.Name)
    }
    if d.Execute == nil {
        return fmt.Errorf("piapi: tool %q: Execute is required", d.Name)
    }
    return nil
}

// ToolInfo is the read-only shape returned by API.GetAllTools (spec #3).
type ToolInfo struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
    SourceInfo  SourceInfo      `json:"source_info"`
}

// SourceInfo describes where a tool or command came from.
type SourceInfo struct {
    Path   string `json:"path"`
    Source string `json:"source"`
    Scope  string `json:"scope"`
    Origin string `json:"origin"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piapi && go test -run TestToolDescriptor ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piapi/tools.go pkg/piapi/tools_test.go
rtk git commit -m "feat(piapi): ToolDescriptor, ToolCall, ToolResult, UpdateFunc"
```

---

## Task 6: `piapi` Context + CommandContext interfaces

**Files:**
- Create: `pkg/piapi/context.go`

- [ ] **Step 1: Create the file**

Interfaces only — no tests required at this layer (implementations are tested in the host package).

Create `pkg/piapi/context.go`:
```go
package piapi

import "context"

// ModelRef identifies a provider+model pair.
type ModelRef struct {
    Provider string `json:"provider"`
    ID       string `json:"id"`
}

// ContextUsage reports current token usage for the active model.
type ContextUsage struct {
    Tokens int `json:"tokens"`
}

// ThinkingLevel is the reasoning intensity dial.
type ThinkingLevel string

const (
    ThinkingOff     ThinkingLevel = "off"
    ThinkingMinimal ThinkingLevel = "minimal"
    ThinkingLow     ThinkingLevel = "low"
    ThinkingMedium  ThinkingLevel = "medium"
    ThinkingHigh    ThinkingLevel = "high"
    ThinkingXHigh   ThinkingLevel = "xhigh"
)

// UI is the spec #4 interactive surface. Spec #1 uses it as a stub;
// every method returns ErrNotImplemented.
type UI interface{}

// SessionView is the spec #5 read-only session accessor. Spec #1 stubs
// it; every method returns ErrNotImplemented.
type SessionView interface{}

// ModelRegistry is the spec #3 model catalog. Spec #1 stubs.
type ModelRegistry interface{}

// CompactOptions controls ctx.Compact (spec #5).
type CompactOptions struct {
    CustomInstructions string
    OnComplete         func(any)
    OnError            func(error)
}

// NewSessionOptions, ForkResult, NavigateOptions, NavigateResult,
// SwitchResult — spec #5 types; stubs for spec #1.
type NewSessionOptions struct{}
type NewSessionResult struct{ Cancelled bool }
type ForkResult struct{ Cancelled bool }
type NavigateOptions struct{}
type NavigateResult struct{ Cancelled bool }
type SwitchResult struct{ Cancelled bool }

// Context is the handler-facing side of the extension API. Every event
// handler and tool Execute call receives one.
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
    UI() UI
    Session() SessionView
}

// CommandContext extends Context with session-control methods that
// would deadlock if called from event handlers. Only command handlers
// receive this.
type CommandContext interface {
    Context
    WaitForIdle(ctx context.Context) error
    NewSession(NewSessionOptions) (NewSessionResult, error)
    Fork(entryID string) (ForkResult, error)
    NavigateTree(targetID string, opts NavigateOptions) (NavigateResult, error)
    SwitchSession(sessionPath string) (SwitchResult, error)
    Reload(ctx context.Context) error
}
```

- [ ] **Step 2: Verify build**

Run: `cd pkg/piapi && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
rtk git add pkg/piapi/context.go
rtk git commit -m "feat(piapi): Context and CommandContext interfaces"
```

---

## Task 7: `piapi.API` interface

**Files:**
- Create: `pkg/piapi/api.go`

- [ ] **Step 1: Create the file**

Interface-only; tests land in Task 8.

Create `pkg/piapi/api.go`:
```go
package piapi

import "context"

// CustomMessage is the payload for API.SendMessage (spec #5 stub).
type CustomMessage struct {
    CustomType string
    Content    string
    Display    bool
    Details    map[string]any
}

// UserMessage is the payload for API.SendUserMessage (spec #5 stub).
type UserMessage struct {
    Content []ContentPart
}

// SendOptions controls message delivery (spec #5 stub).
type SendOptions struct {
    DeliverAs    string // "steer" | "followUp" | "nextTurn"
    TriggerTurn  bool
}

// CommandDescriptor — spec #2.
type CommandDescriptor struct {
    Description            string
    Handler                func(args string, ctx CommandContext) error
    GetArgumentCompletions func(prefix string) []AutocompleteItem
}

// AutocompleteItem — spec #2.
type AutocompleteItem struct {
    Value string
    Label string
}

// ShortcutDescriptor — spec #6.
type ShortcutDescriptor struct {
    Description string
    Handler     func(ctx CommandContext) error
}

// FlagDescriptor — spec #6.
type FlagDescriptor struct {
    Description string
    Type        string
    Default     any
}

// ProviderDescriptor — spec #6.
type ProviderDescriptor struct {
    BaseURL string
    APIKey  string
    API     string
    // Remaining fields land in spec #6.
}

// RendererDescriptor — spec #6.
type RendererDescriptor struct {
    Kind    string // "text" | "markdown"
    Handler any    // placeholder; shape finalized in spec #6
}

// CommandInfo is the read shape returned by API.GetCommands (spec #2).
type CommandInfo struct {
    Name        string     `json:"name"`
    Description string     `json:"description,omitempty"`
    Source      string     `json:"source"`
    SourceInfo  SourceInfo `json:"source_info"`
}

// ExecOptions controls API.Exec.
type ExecOptions struct {
    Timeout int // milliseconds; 0 means no timeout
}

// ExecResult is the output of API.Exec.
type ExecResult struct {
    Stdout string
    Stderr string
    Code   int
    Killed bool
}

// EventBus is the inter-extension pubsub (spec #3 stub).
type EventBus interface {
    On(event string, handler func(any)) error
    Emit(event string, payload any) error
}

// API is the surface every extension uses. Compiled-in extensions
// receive a direct implementation; hosted extensions receive an
// RPC-backed implementation.
//
// Methods tagged "spec #N" return ErrNotImplemented until that spec
// lands. Callers should check with errors.Is(err, ErrNotImplementedSentinel).
type API interface {
    // Identity — set during Register, read-only thereafter.
    Name() string
    Version() string

    // Registrations.
    RegisterTool(ToolDescriptor) error                         // spec #1
    RegisterCommand(string, CommandDescriptor) error            // spec #2
    RegisterShortcut(string, ShortcutDescriptor) error          // spec #6
    RegisterFlag(string, FlagDescriptor) error                  // spec #6
    RegisterProvider(string, ProviderDescriptor) error          // spec #6
    UnregisterProvider(string) error                            // spec #6
    RegisterMessageRenderer(string, RendererDescriptor) error   // spec #6

    // Event subscription (spec #1 supports session_start; others in spec #3).
    On(eventName string, handler EventHandler) error

    // Inter-extension bus (spec #3).
    Events() EventBus

    // Messaging & state (spec #5).
    SendMessage(CustomMessage, SendOptions) error
    SendUserMessage(UserMessage, SendOptions) error
    AppendEntry(kind string, payload any) error
    SetSessionName(string) error
    GetSessionName() string
    SetLabel(entryID, label string) error

    // Tool & model management (spec #3).
    GetActiveTools() []string
    GetAllTools() []ToolInfo
    SetActiveTools([]string) error
    SetModel(ModelRef) (bool, error)
    GetThinkingLevel() ThinkingLevel
    SetThinkingLevel(ThinkingLevel) error

    // Utilities.
    Exec(ctx context.Context, cmd string, args []string, opts ExecOptions) (ExecResult, error) // spec #1
    GetCommands() []CommandInfo                                                                 // spec #2
    GetFlag(name string) any                                                                    // spec #6
}

// Register is the entrypoint signature every compiled-in and hosted-Go
// extension implements.
type Register func(pi API) error
```

- [ ] **Step 2: Verify build**

Run: `cd pkg/piapi && go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
rtk git add pkg/piapi/api.go
rtk git commit -m "feat(piapi): API interface declaring full extension surface"
```

---

## Task 8: `piapi` full module test run

**Files:** none (verification only)

- [ ] **Step 1: Run full piapi test suite**

Run: `cd pkg/piapi && go test ./... -v -count=1`
Expected: all tests pass.

- [ ] **Step 2: Run go vet**

Run: `cd pkg/piapi && go vet ./...`
Expected: no issues.

- [ ] **Step 3: Check coverage**

Run: `cd pkg/piapi && go test -cover ./...`
Expected: ≥85% coverage.

If below 85%, add tests for any uncovered functions. Most of this module is interfaces and types that don't need coverage; the 85% bar applies to functions with logic (Validate, Error, etc.).

- [ ] **Step 4: Commit (if tests were added)**

```bash
rtk git add pkg/piapi/
rtk git commit -m "test(piapi): coverage pass"
```

Skip this commit if no tests were added.

---

## Task 9: Scaffold `pkg/piext` module

**Files:**
- Create: `pkg/piext/go.mod`
- Create: `pkg/piext/doc.go`
- Modify: `go.mod`

- [ ] **Step 1: Create the piext go.mod**

Create `pkg/piext/go.mod`:
```
module github.com/dimetron/pi-go/pkg/piext

go 1.22

require github.com/dimetron/pi-go/pkg/piapi v0.0.0

replace github.com/dimetron/pi-go/pkg/piapi => ../piapi
```

- [ ] **Step 2: Create the piext package doc**

Create `pkg/piext/doc.go`:
```go
// Package piext is the hosted-Go SDK for pi-go extensions.
//
// A hosted-Go extension is a separate Go binary that pi-go spawns over
// stdio and talks to via JSON-RPC v2.1. From the extension author's
// perspective the shape is identical to a compiled-in extension:
//
//     func main() {
//         piext.Run(Metadata, func(pi piapi.API) error {
//             pi.RegisterTool(...)
//             return nil
//         })
//     }
//
// piext.Run handles the stdio wiring, handshake, and backs the piapi.API
// implementation with a transport client.
package piext
```

- [ ] **Step 3: Add piext to root go.mod and go.work**

Edit `go.mod` at repo root to add:
```
require github.com/dimetron/pi-go/pkg/piext v0.0.0

replace github.com/dimetron/pi-go/pkg/piext => ./pkg/piext
```

Verify `go.work` already includes `./pkg/piext` (added in Task 1).

- [ ] **Step 4: Verify build**

Run: `cd pkg/piext && go build ./... && cd ../.. && go build ./...`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piext/go.mod pkg/piext/doc.go go.mod
rtk git commit -m "feat(piext): scaffold hosted-Go SDK module"
```

---

## Task 10: `piext` stdio transport

**Files:**
- Create: `pkg/piext/transport.go`
- Create: `pkg/piext/transport_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piext/transport_test.go`:
```go
package piext

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "strings"
    "testing"
)

func TestTransport_SendRecv(t *testing.T) {
    in := &bytes.Buffer{}
    out := &bytes.Buffer{}
    tr := newTransport(io.NopCloser(in), writeCloser{out})
    defer tr.Close()

    // Write a fake incoming message into `in` (what the host would send).
    in.WriteString(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n")

    // Send a request from our side (goes to `out`).
    ctx := context.Background()
    var result map[string]any
    if err := tr.Call(ctx, "pi.extension/handshake", map[string]any{"protocol_version": "2.1"}, &result); err != nil {
        t.Fatalf("Call err: %v", err)
    }
    if result["ok"] != true {
        t.Fatalf("result=%v; want ok:true", result)
    }

    sent := out.String()
    if !strings.Contains(sent, `"method":"pi.extension/handshake"`) {
        t.Fatalf("outgoing missing method; got %q", sent)
    }
    if !strings.Contains(sent, `"protocol_version":"2.1"`) {
        t.Fatalf("outgoing missing params; got %q", sent)
    }
}

func TestTransport_NotifyDoesNotAwaitResponse(t *testing.T) {
    in := &bytes.Buffer{}
    out := &bytes.Buffer{}
    tr := newTransport(io.NopCloser(in), writeCloser{out})
    defer tr.Close()

    err := tr.Notify("pi.extension/log", map[string]any{"message": "hi"})
    if err != nil {
        t.Fatalf("Notify err: %v", err)
    }
    sent := out.String()
    // Notifications have no "id" field.
    var parsed map[string]any
    if err := json.Unmarshal([]byte(strings.TrimSpace(sent)), &parsed); err != nil {
        t.Fatalf("sent not JSON: %v", err)
    }
    if _, has := parsed["id"]; has {
        t.Fatalf("notification must not have id; got %v", parsed)
    }
}

type writeCloser struct{ *bytes.Buffer }

func (w writeCloser) Close() error { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piext && go test -run TestTransport ./...`
Expected: FAIL with "undefined: newTransport".

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piext/transport.go`:
```go
package piext

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "sync"
    "sync/atomic"
)

// Transport is the line-delimited JSON-RPC client used by piext.Run.
type Transport struct {
    in          io.ReadCloser
    out         io.WriteCloser
    scanner     *bufio.Scanner
    writeMu     sync.Mutex
    nextID      atomic.Uint64
    pending     sync.Map // id -> chan *rawResponse
    handlersMu  sync.RWMutex
    handlers    map[string]RequestHandler
    closed      atomic.Bool
}

// RequestHandler is invoked when the host sends a request to us.
// Used for extension_event dispatch from host → extension.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

type rawRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      *json.Number    `json:"id,omitempty"`
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

type rawResponse struct {
    Result json.RawMessage
    Error  *rpcError
}

func newTransport(in io.ReadCloser, out io.WriteCloser) *Transport {
    scanner := bufio.NewScanner(in)
    scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
    t := &Transport{
        in:       in,
        out:      out,
        scanner:  scanner,
        handlers: make(map[string]RequestHandler),
    }
    go t.readLoop()
    return t
}

// Connect starts a Transport over the process's stdin/stdout.
func Connect() *Transport {
    return newTransport(stdinReadCloser{}, stdoutWriteCloser{})
}

func (t *Transport) Close() error {
    if !t.closed.CompareAndSwap(false, true) {
        return nil
    }
    _ = t.in.Close()
    _ = t.out.Close()
    return nil
}

// Call sends a request and blocks until the response arrives or ctx cancels.
func (t *Transport) Call(ctx context.Context, method string, params, result any) error {
    id := t.nextID.Add(1)
    ch := make(chan *rawResponse, 1)
    t.pending.Store(id, ch)
    defer t.pending.Delete(id)

    if err := t.writeJSON(map[string]any{
        "jsonrpc": "2.0",
        "id":      id,
        "method":  method,
        "params":  params,
    }); err != nil {
        return err
    }
    select {
    case <-ctx.Done():
        return ctx.Err()
    case resp := <-ch:
        if resp.Error != nil {
            return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
        }
        if result == nil || len(resp.Result) == 0 {
            return nil
        }
        return json.Unmarshal(resp.Result, result)
    }
}

// Notify sends a notification (no id, no response).
func (t *Transport) Notify(method string, params any) error {
    return t.writeJSON(map[string]any{
        "jsonrpc": "2.0",
        "method":  method,
        "params":  params,
    })
}

// HandleRequest registers a handler for inbound requests.
func (t *Transport) HandleRequest(method string, h RequestHandler) {
    t.handlersMu.Lock()
    t.handlers[method] = h
    t.handlersMu.Unlock()
}

func (t *Transport) writeJSON(v any) error {
    b, err := json.Marshal(v)
    if err != nil {
        return err
    }
    t.writeMu.Lock()
    defer t.writeMu.Unlock()
    _, err = t.out.Write(append(b, '\n'))
    return err
}

func (t *Transport) readLoop() {
    for t.scanner.Scan() {
        line := t.scanner.Bytes()
        if len(line) == 0 {
            continue
        }
        var msg rawRequest
        if err := json.Unmarshal(line, &msg); err != nil {
            continue
        }
        if msg.Method != "" {
            // inbound request or notification
            t.handleInbound(msg)
        } else if msg.ID != nil {
            // response
            id, err := msg.ID.Int64()
            if err != nil {
                continue
            }
            ch, ok := t.pending.Load(uint64(id))
            if !ok {
                continue
            }
            ch.(chan *rawResponse) <- &rawResponse{Result: msg.Result, Error: msg.Error}
        }
    }
}

func (t *Transport) handleInbound(msg rawRequest) {
    t.handlersMu.RLock()
    h := t.handlers[msg.Method]
    t.handlersMu.RUnlock()
    if msg.ID == nil {
        if h != nil {
            _, _ = h(context.Background(), msg.Params)
        }
        return
    }
    go func() {
        var result any
        var err error
        if h != nil {
            result, err = h(context.Background(), msg.Params)
        } else {
            err = fmt.Errorf("unknown method %q", msg.Method)
        }
        resp := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
        if err != nil {
            resp["error"] = rpcError{Code: -32601, Message: err.Error()}
        } else {
            resp["result"] = result
        }
        _ = t.writeJSON(resp)
    }()
}
```

Create also `pkg/piext/stdio.go`:
```go
package piext

import "os"

type stdinReadCloser struct{}

func (stdinReadCloser) Read(p []byte) (int, error) { return os.Stdin.Read(p) }
func (stdinReadCloser) Close() error               { return os.Stdin.Close() }

type stdoutWriteCloser struct{}

func (stdoutWriteCloser) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdoutWriteCloser) Close() error                { return os.Stdout.Close() }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piext && go test -run TestTransport ./... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piext/transport.go pkg/piext/stdio.go pkg/piext/transport_test.go
rtk git commit -m "feat(piext): stdio JSON-RPC 2.0 transport with Call/Notify/HandleRequest"
```

---

## Task 11: `piext` handshake + Run bootstrap

**Files:**
- Create: `pkg/piext/run.go`
- Create: `pkg/piext/run_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piext/run_test.go`:
```go
package piext

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/dimetron/pi-go/pkg/piapi"
)

func TestRun_Handshake(t *testing.T) {
    // Simulate the host: reads the extension's handshake request, writes a response.
    extIn, hostOut := io.Pipe()
    hostIn, extOut := io.Pipe()

    meta := piapi.Metadata{
        Name:    "test",
        Version: "0.1.0",
        RequestedCapabilities: []string{"tools.register"},
    }
    registerCalled := make(chan struct{})
    go func() {
        _ = runInternal(extIn, extOut, meta, func(pi piapi.API) error {
            close(registerCalled)
            return nil
        })
    }()

    // Read extension's handshake.
    buf := make([]byte, 4096)
    n, _ := hostIn.Read(buf)
    var req map[string]any
    if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &req); err != nil {
        t.Fatalf("handshake not JSON: %v", err)
    }
    params := req["params"].(map[string]any)
    if params["protocol_version"] != "2.1" {
        t.Fatalf("protocol_version=%v; want 2.1", params["protocol_version"])
    }
    if params["extension_id"] != "test" {
        t.Fatalf("extension_id=%v; want test", params["extension_id"])
    }

    // Send handshake response.
    resp := `{"jsonrpc":"2.0","id":` + toString(req["id"]) + `,"result":{"protocol_version":"2.1","granted_services":[{"service":"tools","version":1,"methods":["register"]}],"host_services":[],"dispatchable_events":[]}}` + "\n"
    _, _ = hostOut.Write([]byte(resp))

    select {
    case <-registerCalled:
    case <-time.After(2 * time.Second):
        t.Fatal("register callback not invoked within 2s")
    }
    _ = hostIn.Close()
    _ = hostOut.Close()
}

func toString(v any) string {
    b, _ := json.Marshal(v)
    return string(b)
}

var _ = sync.Mutex{} // keep import
var _ = strings.Contains
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd pkg/piext && go test -run TestRun_Handshake ./...`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piext/run.go`:
```go
package piext

import (
    "context"
    "io"
    "os"
    "time"

    "github.com/dimetron/pi-go/pkg/piapi"
)

const protocolVersion = "2.1"
const handshakeTimeout = 5 * time.Second

// Run is the entrypoint for a hosted-Go extension. It performs the
// handshake, instantiates a piapi.API backed by stdio JSON-RPC, and
// invokes the user's register callback. Blocks until the host sends
// pi.extension/shutdown.
func Run(metadata piapi.Metadata, register piapi.Register) error {
    return runInternal(stdinReadCloser{}, stdoutWriteCloser{}, metadata, register)
}

func runInternal(in io.ReadCloser, out io.WriteCloser, metadata piapi.Metadata, register piapi.Register) error {
    if err := metadata.Validate(); err != nil {
        return err
    }
    transport := newTransport(in, out)
    defer transport.Close()

    ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
    defer cancel()

    requested := make([]map[string]any, 0, len(metadata.RequestedCapabilities))
    seen := map[string]map[string]any{}
    for _, cap := range metadata.RequestedCapabilities {
        svc, method := splitCap(cap)
        entry, ok := seen[svc]
        if !ok {
            entry = map[string]any{"service": svc, "version": 1, "methods": []string{}}
            seen[svc] = entry
            requested = append(requested, entry)
        }
        entry["methods"] = append(entry["methods"].([]string), method)
    }

    var hsResp handshakeResponse
    err := transport.Call(ctx, "pi.extension/handshake", map[string]any{
        "protocol_version":   protocolVersion,
        "extension_id":       metadata.Name,
        "extension_version":  metadata.Version,
        "requested_services": requested,
    }, &hsResp)
    if err != nil {
        return err
    }
    if hsResp.ProtocolVersion != protocolVersion {
        return &handshakeError{got: hsResp.ProtocolVersion}
    }

    api := newRPCAPI(transport, metadata, hsResp.GrantedServices)
    if err := register(api); err != nil {
        return err
    }

    shutdownCh := make(chan struct{})
    transport.HandleRequest("pi.extension/shutdown", func(_ context.Context, _ []byte) (any, error) {
        close(shutdownCh)
        return map[string]any{}, nil
    })

    select {
    case <-shutdownCh:
        return nil
    }
}

type handshakeResponse struct {
    ProtocolVersion    string           `json:"protocol_version"`
    GrantedServices    []GrantedService `json:"granted_services"`
    HostServices       []GrantedService `json:"host_services"`
    DispatchableEvents []DispatchEvent  `json:"dispatchable_events"`
}

// GrantedService is the post-handshake view of what the host granted us.
type GrantedService struct {
    Service string   `json:"service"`
    Version int      `json:"version"`
    Methods []string `json:"methods"`
}

// DispatchEvent is an event the host is willing to dispatch to us.
type DispatchEvent struct {
    Name    string `json:"name"`
    Version int    `json:"version"`
}

type handshakeError struct{ got string }

func (e *handshakeError) Error() string {
    return "piext: protocol version mismatch: host returned " + e.got
}

func splitCap(cap string) (service, method string) {
    for i := 0; i < len(cap); i++ {
        if cap[i] == '.' {
            return cap[:i], cap[i+1:]
        }
    }
    return cap, ""
}

// Log returns a writer that emits each write as a pi.extension/log
// notification to the host. Extensions should NOT write to os.Stdout
// directly (the transport owns stdout).
func Log() io.Writer {
    return logWriter{}
}

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
    _, _ = os.Stderr.Write(p)
    return len(p), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piext && go test -run TestRun_Handshake ./... -v`
Expected: PASS (may need to add a minimal `rpcAPI` stub; see next task).

If test fails due to undefined `newRPCAPI`, stub it temporarily:
```go
// pkg/piext/rpc_api_stub.go  (delete in Task 12)
package piext

import "github.com/dimetron/pi-go/pkg/piapi"

func newRPCAPI(t *Transport, m piapi.Metadata, granted []GrantedService) piapi.API {
    return nil
}
```

- [ ] **Step 5: Commit**

```bash
rtk git add pkg/piext/run.go pkg/piext/run_test.go pkg/piext/rpc_api_stub.go
rtk git commit -m "feat(piext): Run bootstrap with handshake + shutdown loop"
```

---

## Task 12: `piext` RPC-backed `piapi.API` implementation

**Files:**
- Create: `pkg/piext/rpc_api.go` (replaces stub from Task 11)
- Create: `pkg/piext/rpc_api_test.go`
- Delete: `pkg/piext/rpc_api_stub.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/piext/rpc_api_test.go`:
```go
package piext

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "strings"
    "testing"

    "github.com/dimetron/pi-go/pkg/piapi"
)

func TestRPCAPI_RegisterTool_SendsHostCall(t *testing.T) {
    extIn, hostOut := io.Pipe()
    hostIn, extOut := io.Pipe()
    transport := newTransport(extIn, extOut)
    api := newRPCAPI(transport, piapi.Metadata{Name: "t", Version: "0.1"}, []GrantedService{
        {Service: "tools", Version: 1, Methods: []string{"register"}},
    })

    go func() {
        buf := make([]byte, 4096)
        n, _ := hostIn.Read(buf)
        // reply OK
        var req map[string]any
        _ = json.Unmarshal(buf[:n], &req)
        resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
        _, _ = hostOut.Write(append(resp, '\n'))
    }()

    err := api.RegisterTool(piapi.ToolDescriptor{
        Name: "greet", Description: "greet", Parameters: json.RawMessage(`{"type":"object"}`),
        Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
            return piapi.ToolResult{}, nil
        },
    })
    if err != nil {
        t.Fatalf("RegisterTool err: %v", err)
    }
    _ = hostIn.Close()
    _ = hostOut.Close()
    _ = transport.Close()
}

func TestRPCAPI_NotImplementedStubs(t *testing.T) {
    transport := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{})
    api := newRPCAPI(transport, piapi.Metadata{Name: "t", Version: "0.1"}, nil)

    err := api.RegisterCommand("x", piapi.CommandDescriptor{})
    if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
        t.Fatalf("RegisterCommand: got %v; want ErrNotImplemented", err)
    }
    err = api.SendMessage(piapi.CustomMessage{}, piapi.SendOptions{})
    if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
        t.Fatalf("SendMessage: got %v; want ErrNotImplemented", err)
    }
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
```

- [ ] **Step 2: Delete the stub**

Run: `rm pkg/piext/rpc_api_stub.go`

- [ ] **Step 3: Write minimal implementation**

Create `pkg/piext/rpc_api.go`:
```go
package piext

import (
    "context"
    "encoding/json"
    "os/exec"
    "sync"

    "github.com/dimetron/pi-go/pkg/piapi"
)

type rpcAPI struct {
    transport *Transport
    metadata  piapi.Metadata
    granted   map[string]map[string]bool // service → method → true

    mu        sync.Mutex
    tools     map[string]piapi.ToolDescriptor
    handlers  map[string][]piapi.EventHandler
}

func newRPCAPI(t *Transport, meta piapi.Metadata, granted []GrantedService) *rpcAPI {
    gmap := make(map[string]map[string]bool)
    for _, svc := range granted {
        methods := make(map[string]bool, len(svc.Methods))
        for _, m := range svc.Methods {
            methods[m] = true
        }
        gmap[svc.Service] = methods
    }
    api := &rpcAPI{
        transport: t,
        metadata:  meta,
        granted:   gmap,
        tools:     map[string]piapi.ToolDescriptor{},
        handlers:  map[string][]piapi.EventHandler{},
    }
    t.HandleRequest("pi.extension/extension_event", api.onEvent)
    return api
}

func (a *rpcAPI) Name() string    { return a.metadata.Name }
func (a *rpcAPI) Version() string { return a.metadata.Version }

func (a *rpcAPI) checkGrant(service, method string) error {
    m, ok := a.granted[service]
    if !ok || !m[method] {
        return piapi.ErrCapabilityDenied{Capability: service + "." + method}
    }
    return nil
}

func (a *rpcAPI) hostCall(method string, payload any, result any) error {
    // Extract service + method from the fully-qualified name.
    svc, m := splitCap(method)
    if err := a.checkGrant(svc, m); err != nil {
        return err
    }
    return a.transport.Call(context.Background(), "pi.extension/host_call", map[string]any{
        "service": svc, "version": 1, "method": m, "payload": payload,
    }, result)
}

// --- Registrations (spec #1 implements RegisterTool) ---

func (a *rpcAPI) RegisterTool(desc piapi.ToolDescriptor) error {
    if err := desc.Validate(); err != nil {
        return err
    }
    a.mu.Lock()
    a.tools[desc.Name] = desc
    a.mu.Unlock()

    payload := map[string]any{
        "name":              desc.Name,
        "label":             desc.Label,
        "description":       desc.Description,
        "prompt_snippet":    desc.PromptSnippet,
        "prompt_guidelines": desc.PromptGuidelines,
        "parameters":        json.RawMessage(desc.Parameters),
    }
    var result map[string]any
    return a.hostCall("tools.register", payload, &result)
}

func (a *rpcAPI) RegisterCommand(_ string, _ piapi.CommandDescriptor) error {
    return piapi.ErrNotImplemented{Method: "RegisterCommand", Spec: "#2"}
}
func (a *rpcAPI) RegisterShortcut(_ string, _ piapi.ShortcutDescriptor) error {
    return piapi.ErrNotImplemented{Method: "RegisterShortcut", Spec: "#6"}
}
func (a *rpcAPI) RegisterFlag(_ string, _ piapi.FlagDescriptor) error {
    return piapi.ErrNotImplemented{Method: "RegisterFlag", Spec: "#6"}
}
func (a *rpcAPI) RegisterProvider(_ string, _ piapi.ProviderDescriptor) error {
    return piapi.ErrNotImplemented{Method: "RegisterProvider", Spec: "#6"}
}
func (a *rpcAPI) UnregisterProvider(_ string) error {
    return piapi.ErrNotImplemented{Method: "UnregisterProvider", Spec: "#6"}
}
func (a *rpcAPI) RegisterMessageRenderer(_ string, _ piapi.RendererDescriptor) error {
    return piapi.ErrNotImplemented{Method: "RegisterMessageRenderer", Spec: "#6"}
}

// --- Event subscription ---

func (a *rpcAPI) On(eventName string, handler piapi.EventHandler) error {
    if eventName != piapi.EventSessionStart {
        return piapi.ErrNotImplemented{Method: "On(" + eventName + ")", Spec: "#3"}
    }
    if err := a.checkGrant("events", "session_start"); err != nil {
        return err
    }
    a.mu.Lock()
    a.handlers[eventName] = append(a.handlers[eventName], handler)
    a.mu.Unlock()
    var result map[string]any
    return a.transport.Call(context.Background(), "pi.extension/subscribe_event", map[string]any{
        "events": []map[string]any{{"name": eventName, "version": 1}},
    }, &result)
}

func (a *rpcAPI) onEvent(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        Event   string          `json:"event"`
        Version int             `json:"version"`
        Payload json.RawMessage `json:"payload"`
    }
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }
    a.mu.Lock()
    handlers := append([]piapi.EventHandler(nil), a.handlers[req.Event]...)
    a.mu.Unlock()

    // Decode payload to the right event type.
    var evt piapi.Event
    switch req.Event {
    case piapi.EventSessionStart:
        var e piapi.SessionStartEvent
        if err := json.Unmarshal(req.Payload, &e); err != nil {
            return nil, err
        }
        evt = e
    case piapi.EventToolExecute:
        return a.handleToolExecute(req.Payload)
    default:
        return map[string]any{"control": nil}, nil
    }

    var result piapi.EventResult
    for _, h := range handlers {
        r, err := h(evt, nil)
        if err != nil {
            return nil, err
        }
        if r.Control != nil {
            result = r
        }
    }
    return result, nil
}

func (a *rpcAPI) handleToolExecute(payload json.RawMessage) (any, error) {
    var call struct {
        ToolCallID string          `json:"tool_call_id"`
        Name       string          `json:"name"`
        Args       json.RawMessage `json:"args"`
    }
    if err := json.Unmarshal(payload, &call); err != nil {
        return nil, err
    }
    a.mu.Lock()
    desc, ok := a.tools[call.Name]
    a.mu.Unlock()
    if !ok {
        return map[string]any{"is_error": true, "content": []piapi.ContentPart{{Type: "text", Text: "unknown tool: " + call.Name}}}, nil
    }
    onUpdate := func(p piapi.ToolResult) {
        _ = a.transport.Notify("pi.extension/tool_update", map[string]any{
            "tool_call_id": call.ToolCallID,
            "partial":      p,
        })
    }
    result, err := desc.Execute(context.Background(), piapi.ToolCall{
        ID: call.ToolCallID, Name: call.Name, Args: call.Args,
    }, onUpdate)
    if err != nil {
        return map[string]any{"is_error": true, "content": []piapi.ContentPart{{Type: "text", Text: err.Error()}}}, nil
    }
    return result, nil
}

// --- Unimplemented methods ---

func (a *rpcAPI) Events() piapi.EventBus { return noopBus{} }

func (a *rpcAPI) SendMessage(_ piapi.CustomMessage, _ piapi.SendOptions) error {
    return piapi.ErrNotImplemented{Method: "SendMessage", Spec: "#5"}
}
func (a *rpcAPI) SendUserMessage(_ piapi.UserMessage, _ piapi.SendOptions) error {
    return piapi.ErrNotImplemented{Method: "SendUserMessage", Spec: "#5"}
}
func (a *rpcAPI) AppendEntry(_ string, _ any) error {
    return piapi.ErrNotImplemented{Method: "AppendEntry", Spec: "#5"}
}
func (a *rpcAPI) SetSessionName(_ string) error {
    return piapi.ErrNotImplemented{Method: "SetSessionName", Spec: "#5"}
}
func (a *rpcAPI) GetSessionName() string       { return "" }
func (a *rpcAPI) SetLabel(_, _ string) error {
    return piapi.ErrNotImplemented{Method: "SetLabel", Spec: "#5"}
}
func (a *rpcAPI) GetActiveTools() []string    { return nil }
func (a *rpcAPI) GetAllTools() []piapi.ToolInfo { return nil }
func (a *rpcAPI) SetActiveTools(_ []string) error {
    return piapi.ErrNotImplemented{Method: "SetActiveTools", Spec: "#3"}
}
func (a *rpcAPI) SetModel(_ piapi.ModelRef) (bool, error) {
    return false, piapi.ErrNotImplemented{Method: "SetModel", Spec: "#3"}
}
func (a *rpcAPI) GetThinkingLevel() piapi.ThinkingLevel { return piapi.ThinkingOff }
func (a *rpcAPI) SetThinkingLevel(_ piapi.ThinkingLevel) error {
    return piapi.ErrNotImplemented{Method: "SetThinkingLevel", Spec: "#3"}
}
func (a *rpcAPI) Exec(ctx context.Context, cmd string, args []string, opts piapi.ExecOptions) (piapi.ExecResult, error) {
    if err := a.checkGrant("exec", "shell"); err != nil {
        return piapi.ExecResult{}, err
    }
    c := exec.CommandContext(ctx, cmd, args...)
    var stdout, stderr []byte
    var err error
    stdout, err = c.Output()
    if ee, ok := err.(*exec.ExitError); ok {
        stderr = ee.Stderr
    }
    code := 0
    if c.ProcessState != nil {
        code = c.ProcessState.ExitCode()
    }
    return piapi.ExecResult{
        Stdout: string(stdout), Stderr: string(stderr), Code: code,
        Killed: ctx.Err() != nil,
    }, nil
}
func (a *rpcAPI) GetCommands() []piapi.CommandInfo { return nil }
func (a *rpcAPI) GetFlag(_ string) any             { return nil }

type noopBus struct{}

func (noopBus) On(string, func(any)) error   { return piapi.ErrNotImplemented{Method: "Events.On", Spec: "#3"} }
func (noopBus) Emit(string, any) error       { return piapi.ErrNotImplemented{Method: "Events.Emit", Spec: "#3"} }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd pkg/piext && go test ./... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git rm pkg/piext/rpc_api_stub.go
rtk git add pkg/piext/rpc_api.go pkg/piext/rpc_api_test.go
rtk git commit -m "feat(piext): RPC-backed piapi.API with RegisterTool, On, Exec, tool_execute dispatch"
```

---

## Task 13: `piext` schema helper

**Files:**
- Create: `pkg/piext/schema.go`
- Create: `pkg/piext/schema_test.go`
- Modify: `pkg/piext/go.mod` (add invopop/jsonschema)

- [ ] **Step 1: Add dependency**

Run: `cd pkg/piext && go get github.com/invopop/jsonschema@latest`

- [ ] **Step 2: Write the failing test**

Create `pkg/piext/schema_test.go`:
```go
package piext

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestSchemaFromStruct(t *testing.T) {
    type input struct {
        Name string `json:"name" jsonschema:"description=Name to greet"`
    }
    schema := SchemaFromStruct(input{})
    var parsed map[string]any
    if err := json.Unmarshal(schema, &parsed); err != nil {
        t.Fatalf("schema not JSON: %v", err)
    }
    s := string(schema)
    if !strings.Contains(s, `"name"`) {
        t.Fatalf("schema missing name property: %s", s)
    }
    if !strings.Contains(s, `"object"`) {
        t.Fatalf("schema missing object type: %s", s)
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd pkg/piext && go test -run TestSchemaFromStruct ./...`
Expected: FAIL.

- [ ] **Step 4: Write minimal implementation**

Create `pkg/piext/schema.go`:
```go
package piext

import (
    "encoding/json"

    "github.com/invopop/jsonschema"
)

// SchemaFromStruct generates a JSON Schema (draft-2020-12) for a Go
// struct. Use struct tags to annotate:
//
//     type args struct {
//         Name string `json:"name" jsonschema:"description=Name to greet"`
//     }
//     pi.RegisterTool(piapi.ToolDescriptor{
//         Parameters: piext.SchemaFromStruct(args{}),
//         ...
//     })
//
// Panics if the schema cannot be generated (should be impossible for
// well-formed structs; callers can treat it as programmer error).
func SchemaFromStruct(v any) json.RawMessage {
    r := &jsonschema.Reflector{
        ExpandedStruct: true,
        DoNotReference: true,
    }
    schema := r.Reflect(v)
    b, err := json.Marshal(schema)
    if err != nil {
        panic("piext: SchemaFromStruct: " + err.Error())
    }
    return json.RawMessage(b)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd pkg/piext && go test -run TestSchemaFromStruct ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add pkg/piext/schema.go pkg/piext/schema_test.go pkg/piext/go.mod pkg/piext/go.sum
rtk git commit -m "feat(piext): SchemaFromStruct helper via invopop/jsonschema"
```

---

## Task 14: `piext` full module test run

**Files:** none

- [ ] **Step 1: Run full piext test suite with coverage**

Run: `cd pkg/piext && go test ./... -cover -v`
Expected: all tests pass, coverage ≥85% on `transport.go`, `rpc_api.go`, `run.go`.

- [ ] **Step 2: Run go vet**

Run: `cd pkg/piext && go vet ./...`
Expected: clean.

- [ ] **Step 3: Commit any additional tests if coverage was below bar**

If no changes needed, skip this step. Otherwise:
```bash
rtk git add pkg/piext/
rtk git commit -m "test(piext): coverage pass"
```

---

## Task 15: Scaffold `@pi-go/extension-sdk`

**Files:**
- Create: `packages/extension-sdk/package.json`
- Create: `packages/extension-sdk/tsconfig.json`
- Create: `packages/extension-sdk/src/index.ts`
- Create: `packages/extension-sdk/.gitignore`

- [ ] **Step 1: Create package.json**

Create `packages/extension-sdk/package.json`:
```json
{
  "name": "@pi-go/extension-sdk",
  "version": "0.1.0",
  "description": "TypeScript SDK for authoring pi-go hosted extensions",
  "type": "module",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js"
    }
  },
  "files": ["dist"],
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "test": "node --test dist/test",
    "clean": "rm -rf dist"
  },
  "peerDependencies": {
    "@sinclair/typebox": "^0.34.0"
  },
  "devDependencies": {
    "@sinclair/typebox": "^0.34.0",
    "@types/node": "^20.0.0",
    "typescript": "^5.4.0"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

Create `packages/extension-sdk/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "resolveJsonModule": true
  },
  "include": ["src/**/*.ts"]
}
```

- [ ] **Step 3: Create placeholder index.ts**

Create `packages/extension-sdk/src/index.ts`:
```typescript
// Populated in subsequent tasks.
export {};
```

- [ ] **Step 4: Create .gitignore**

Create `packages/extension-sdk/.gitignore`:
```
node_modules
dist
*.log
```

- [ ] **Step 5: Install and build**

Run: `cd packages/extension-sdk && npm install && npm run build`
Expected: builds a minimal `dist/index.js`.

- [ ] **Step 6: Commit**

```bash
rtk git add packages/extension-sdk/package.json packages/extension-sdk/tsconfig.json packages/extension-sdk/src/index.ts packages/extension-sdk/.gitignore
rtk git commit -m "feat(sdk): scaffold @pi-go/extension-sdk"
```

---

## Task 16: SDK core types

**Files:**
- Create: `packages/extension-sdk/src/types.ts`
- Create: `packages/extension-sdk/src/tools.ts`
- Create: `packages/extension-sdk/src/events.ts`
- Create: `packages/extension-sdk/src/errors.ts`

- [ ] **Step 1: Create types.ts**

Create `packages/extension-sdk/src/types.ts`:
```typescript
export interface Metadata {
  name: string;
  version: string;
  description?: string;
  prompt?: string;
  requestedCapabilities: string[];
  entry?: string;
}

export interface ModelRef {
  provider: string;
  id: string;
}

export interface ContextUsage {
  tokens: number;
}

export type ThinkingLevel = "off" | "minimal" | "low" | "medium" | "high" | "xhigh";

export interface ExecOptions {
  signal?: AbortSignal;
  timeout?: number;
}

export interface ExecResult {
  stdout: string;
  stderr: string;
  code: number;
  killed: boolean;
}

export interface SourceInfo {
  path: string;
  source: string;
  scope: "user" | "project" | "temporary";
  origin: "package" | "top-level";
  baseDir?: string;
}
```

- [ ] **Step 2: Create tools.ts**

Create `packages/extension-sdk/src/tools.ts`:
```typescript
import type { SourceInfo } from "./types.js";

export interface ContentPart {
  type: "text";
  text: string;
}

export interface ToolResult {
  content: ContentPart[];
  details?: Record<string, unknown>;
  isError?: boolean;
}

export type UpdateFn = (partial: ToolResult) => void;

export interface ToolDescriptor<TParams = unknown> {
  name: string;
  label: string;
  description: string;
  promptSnippet?: string;
  promptGuidelines?: string[];
  parameters: unknown; // JSON Schema
  prepareArguments?: (args: unknown) => unknown;
  execute: (
    toolCallId: string,
    params: TParams,
    signal: AbortSignal | undefined,
    onUpdate: UpdateFn | undefined,
    ctx: unknown,
  ) => Promise<ToolResult>;
}

export interface ToolInfo {
  name: string;
  description: string;
  parameters: unknown;
  sourceInfo: SourceInfo;
}
```

- [ ] **Step 3: Create events.ts**

Create `packages/extension-sdk/src/events.ts`:
```typescript
export const EventNames = {
  SessionStart: "session_start",
  ToolExecute: "tool_execute",
} as const;

export type EventName = (typeof EventNames)[keyof typeof EventNames];

export interface SessionStartEvent {
  reason: "startup" | "reload" | "new" | "resume" | "fork";
  previousSessionFile?: string;
}

export interface EventControl {
  cancel?: boolean;
  block?: boolean;
  reason?: string;
  transform?: unknown;
  action?: string;
}

export interface EventResult {
  control?: EventControl | null;
}

export type EventHandler<TEvent> = (
  event: TEvent,
  ctx: unknown,
) => Promise<EventResult | void> | EventResult | void;
```

- [ ] **Step 4: Create errors.ts**

Create `packages/extension-sdk/src/errors.ts`:
```typescript
export class NotImplementedError extends Error {
  readonly method: string;
  readonly spec: string;
  constructor(method: string, spec: string) {
    super(`@pi-go/extension-sdk: ${method} not implemented (deferred to spec ${spec})`);
    this.name = "NotImplementedError";
    this.method = method;
    this.spec = spec;
  }
}

export class CapabilityDeniedError extends Error {
  readonly capability: string;
  readonly reason?: string;
  constructor(capability: string, reason?: string) {
    super(`@pi-go/extension-sdk: capability denied: ${capability}${reason ? ` (${reason})` : ""}`);
    this.name = "CapabilityDeniedError";
    this.capability = capability;
    this.reason = reason;
  }
}
```

- [ ] **Step 5: Verify build**

Run: `cd packages/extension-sdk && npm run build`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
rtk git add packages/extension-sdk/src/types.ts packages/extension-sdk/src/tools.ts packages/extension-sdk/src/events.ts packages/extension-sdk/src/errors.ts
rtk git commit -m "feat(sdk): core types, events, tool types, errors"
```

---

## Task 17: SDK Transport

**Files:**
- Create: `packages/extension-sdk/src/transport.ts`
- Create: `packages/extension-sdk/src/test/transport.test.ts`

- [ ] **Step 1: Write the failing test**

Create `packages/extension-sdk/src/test/transport.test.ts`:
```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import { Transport } from "../transport.js";

test("Transport.call sends JSON-RPC request and receives result", async () => {
  const out: Buffer[] = [];
  const writer = new Writable({
    write(chunk, _enc, cb) {
      out.push(chunk);
      cb();
    },
  });
  const reader = new Readable({ read() {} });
  const t = new Transport(reader, writer);

  const resultPromise = t.call("test.method", { x: 1 });

  // Wait one microtask for the write to flush.
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(out).toString().trim());
  assert.equal(sent.method, "test.method");
  assert.deepEqual(sent.params, { x: 1 });
  assert.equal(typeof sent.id, "number");

  reader.push(JSON.stringify({ jsonrpc: "2.0", id: sent.id, result: { ok: true } }) + "\n");

  const result = await resultPromise;
  assert.deepEqual(result, { ok: true });
  t.close();
});

test("Transport.notify sends request without id", async () => {
  const out: Buffer[] = [];
  const writer = new Writable({
    write(chunk, _enc, cb) {
      out.push(chunk);
      cb();
    },
  });
  const reader = new Readable({ read() {} });
  const t = new Transport(reader, writer);

  t.notify("test.log", { message: "hi" });
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(out).toString().trim());
  assert.equal(sent.method, "test.log");
  assert.equal(sent.id, undefined);
  t.close();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd packages/extension-sdk && npm run build`
Expected: compile error (Transport not defined yet).

- [ ] **Step 3: Write minimal implementation**

Create `packages/extension-sdk/src/transport.ts`:
```typescript
import { Readable, Writable } from "node:stream";
import { createInterface } from "node:readline";

export type RequestHandler = (params: unknown) => unknown | Promise<unknown>;

export class Transport {
  private nextId = 1;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private handlers = new Map<string, RequestHandler>();
  private closed = false;

  constructor(
    private readonly input: Readable,
    private readonly output: Writable,
  ) {
    const rl = createInterface({ input });
    rl.on("line", (line) => {
      if (!line.trim()) return;
      try {
        const msg = JSON.parse(line);
        this.handleMessage(msg);
      } catch {
        // ignore malformed
      }
    });
  }

  call<T = unknown>(method: string, params: unknown): Promise<T> {
    if (this.closed) return Promise.reject(new Error("transport closed"));
    const id = this.nextId++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
      this.write({ jsonrpc: "2.0", id, method, params });
    });
  }

  notify(method: string, params: unknown): void {
    if (this.closed) return;
    this.write({ jsonrpc: "2.0", method, params });
  }

  handle(method: string, handler: RequestHandler): void {
    this.handlers.set(method, handler);
  }

  close(): void {
    this.closed = true;
    for (const { reject } of this.pending.values()) {
      reject(new Error("transport closed"));
    }
    this.pending.clear();
  }

  private write(obj: unknown): void {
    this.output.write(JSON.stringify(obj) + "\n");
  }

  private async handleMessage(msg: {
    id?: number;
    method?: string;
    params?: unknown;
    result?: unknown;
    error?: { code: number; message: string };
  }): Promise<void> {
    if (msg.method) {
      const handler = this.handlers.get(msg.method);
      if (msg.id === undefined) {
        if (handler) await handler(msg.params);
        return;
      }
      try {
        const result = handler ? await handler(msg.params) : null;
        this.write({ jsonrpc: "2.0", id: msg.id, result });
      } catch (err) {
        this.write({
          jsonrpc: "2.0",
          id: msg.id,
          error: { code: -32603, message: err instanceof Error ? err.message : String(err) },
        });
      }
      return;
    }
    if (msg.id !== undefined) {
      const entry = this.pending.get(msg.id);
      if (!entry) return;
      this.pending.delete(msg.id);
      if (msg.error) {
        entry.reject(new Error(`rpc ${msg.error.code}: ${msg.error.message}`));
      } else {
        entry.resolve(msg.result);
      }
    }
  }
}

export function connectStdio(): Transport {
  return new Transport(process.stdin, process.stdout);
}
```

- [ ] **Step 4: Verify build and test**

Run: `cd packages/extension-sdk && npm run build && node --test dist/test/transport.test.js`
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add packages/extension-sdk/src/transport.ts packages/extension-sdk/src/test/transport.test.ts
rtk git commit -m "feat(sdk): Transport class with call/notify/handle"
```

---

## Task 18: SDK API interface + RPC implementation

**Files:**
- Create: `packages/extension-sdk/src/api.ts`
- Create: `packages/extension-sdk/src/api-impl.ts`
- Create: `packages/extension-sdk/src/test/api-impl.test.ts`

- [ ] **Step 1: Create the API interface**

Create `packages/extension-sdk/src/api.ts`:
```typescript
import type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo,
} from "./extended.js";
import type { ToolDescriptor, ToolInfo } from "./tools.js";
import type { EventHandler, EventName } from "./events.js";
import type {
  ModelRef, ExecOptions, ExecResult, ThinkingLevel,
} from "./types.js";

export interface CustomMessage {
  customType: string;
  content: string;
  display?: boolean;
  details?: Record<string, unknown>;
}

export interface SendOptions {
  deliverAs?: "steer" | "followUp" | "nextTurn";
  triggerTurn?: boolean;
}

export interface EventBus {
  on(event: string, handler: (data: unknown) => void): void;
  emit(event: string, data: unknown): void;
}

export interface ExtensionAPI {
  name(): string;
  version(): string;

  registerTool(desc: ToolDescriptor): void;
  registerCommand(name: string, desc: CommandDescriptor): void;
  registerShortcut(shortcut: string, desc: ShortcutDescriptor): void;
  registerFlag(name: string, desc: FlagDescriptor): void;
  registerProvider(name: string, config: ProviderDescriptor): void;
  unregisterProvider(name: string): void;
  registerMessageRenderer(customType: string, renderer: RendererDescriptor): void;

  on<E extends EventName>(event: E, handler: EventHandler<unknown>): void;
  events: EventBus;

  sendMessage(msg: CustomMessage, opts?: SendOptions): void;
  sendUserMessage(content: string, opts?: SendOptions): void;
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

- [ ] **Step 2: Create the extended types stub**

Create `packages/extension-sdk/src/extended.ts`:
```typescript
import type { SourceInfo } from "./types.js";

export interface CommandDescriptor {
  description: string;
  handler: (args: string, ctx: unknown) => Promise<void> | void;
  getArgumentCompletions?: (prefix: string) => AutocompleteItem[] | null;
}

export interface AutocompleteItem {
  value: string;
  label: string;
}

export interface ShortcutDescriptor {
  description: string;
  handler: (ctx: unknown) => Promise<void> | void;
}

export interface FlagDescriptor {
  description: string;
  type: "boolean" | "string" | "number";
  default?: unknown;
}

export interface ProviderDescriptor {
  baseUrl?: string;
  apiKey?: string;
  api?: string;
  headers?: Record<string, string>;
  authHeader?: boolean;
}

export interface RendererDescriptor {
  kind: "text" | "markdown";
  handler: (message: unknown, options: unknown, theme: unknown) => unknown;
}

export interface CommandInfo {
  name: string;
  description?: string;
  source: "extension" | "prompt" | "skill";
  sourceInfo: SourceInfo;
}
```

- [ ] **Step 3: Create the RPC-backed API implementation**

Create `packages/extension-sdk/src/api-impl.ts`:
```typescript
import type { Transport } from "./transport.js";
import type { ExtensionAPI, CustomMessage, SendOptions, EventBus } from "./api.js";
import type { ToolDescriptor, ToolInfo, ToolResult, UpdateFn } from "./tools.js";
import type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo,
} from "./extended.js";
import type { EventHandler, EventName, SessionStartEvent } from "./events.js";
import { EventNames } from "./events.js";
import type { Metadata, ModelRef, ExecOptions, ExecResult, ThinkingLevel } from "./types.js";
import { NotImplementedError, CapabilityDeniedError } from "./errors.js";

export interface GrantedService {
  service: string;
  version: number;
  methods: string[];
}

export function createExtensionAPI(
  transport: Transport,
  metadata: Metadata,
  granted: GrantedService[],
): ExtensionAPI {
  const grantMap = new Map<string, Set<string>>();
  for (const g of granted) {
    grantMap.set(g.service, new Set(g.methods));
  }
  const tools = new Map<string, ToolDescriptor>();
  const handlers = new Map<string, EventHandler<unknown>[]>();

  function ensureGrant(service: string, method: string): void {
    const set = grantMap.get(service);
    if (!set || !set.has(method)) {
      throw new CapabilityDeniedError(`${service}.${method}`);
    }
  }

  async function hostCall(capability: string, payload: unknown): Promise<unknown> {
    const [service, method] = capability.split(".");
    ensureGrant(service, method);
    return transport.call("pi.extension/host_call", {
      service, version: 1, method, payload,
    });
  }

  transport.handle("pi.extension/extension_event", async (params) => {
    const p = params as { event: string; payload: unknown };
    if (p.event === EventNames.ToolExecute) {
      return handleToolExecute(tools, p.payload, transport);
    }
    if (p.event === EventNames.SessionStart) {
      const evt = p.payload as SessionStartEvent;
      for (const h of handlers.get(p.event) ?? []) {
        const r = await h(evt, null);
        if (r && typeof r === "object" && "control" in r && r.control) {
          return { control: r.control };
        }
      }
      return { control: null };
    }
    return { control: null };
  });

  const notImpl = (method: string, spec: string) => () => { throw new NotImplementedError(method, spec); };

  return {
    name: () => metadata.name,
    version: () => metadata.version,

    registerTool: (desc) => {
      if (!desc.name || !desc.execute) throw new Error("registerTool: name and execute are required");
      tools.set(desc.name, desc);
      hostCall("tools.register", {
        name: desc.name,
        label: desc.label,
        description: desc.description,
        prompt_snippet: desc.promptSnippet,
        prompt_guidelines: desc.promptGuidelines,
        parameters: desc.parameters,
      }).catch((err) => { throw err; });
    },
    registerCommand: notImpl("registerCommand", "#2") as (name: string, desc: CommandDescriptor) => void,
    registerShortcut: notImpl("registerShortcut", "#6") as (s: string, d: ShortcutDescriptor) => void,
    registerFlag: notImpl("registerFlag", "#6") as (n: string, d: FlagDescriptor) => void,
    registerProvider: notImpl("registerProvider", "#6") as (n: string, c: ProviderDescriptor) => void,
    unregisterProvider: notImpl("unregisterProvider", "#6") as (n: string) => void,
    registerMessageRenderer: notImpl("registerMessageRenderer", "#6") as (t: string, r: RendererDescriptor) => void,

    on: (event, handler) => {
      if (event !== EventNames.SessionStart) {
        throw new NotImplementedError(`on(${event})`, "#3");
      }
      ensureGrant("events", "session_start");
      const list = handlers.get(event) ?? [];
      list.push(handler as EventHandler<unknown>);
      handlers.set(event, list);
      transport.call("pi.extension/subscribe_event", {
        events: [{ name: event, version: 1 }],
      });
    },

    events: {
      on: notImpl("events.on", "#3") as (event: string, handler: (d: unknown) => void) => void,
      emit: notImpl("events.emit", "#3") as (event: string, data: unknown) => void,
    } as EventBus,

    sendMessage: notImpl("sendMessage", "#5") as (m: CustomMessage, o?: SendOptions) => void,
    sendUserMessage: notImpl("sendUserMessage", "#5") as (c: string, o?: SendOptions) => void,
    appendEntry: notImpl("appendEntry", "#5") as (t: string, d?: unknown) => void,
    setSessionName: notImpl("setSessionName", "#5") as (n: string) => void,
    getSessionName: () => undefined,
    setLabel: notImpl("setLabel", "#5") as (e: string, l: string | undefined) => void,

    getActiveTools: () => [],
    getAllTools: (): ToolInfo[] => [],
    setActiveTools: notImpl("setActiveTools", "#3") as (names: string[]) => void,
    setModel: async () => { throw new NotImplementedError("setModel", "#3"); },
    getThinkingLevel: () => "off" as ThinkingLevel,
    setThinkingLevel: notImpl("setThinkingLevel", "#3") as (l: ThinkingLevel) => void,

    exec: async (cmd: string, args: string[], opts?: ExecOptions): Promise<ExecResult> => {
      ensureGrant("exec", "shell");
      const result = (await hostCall("exec.shell", { cmd, args, timeout: opts?.timeout })) as ExecResult;
      return result;
    },
    getCommands: (): CommandInfo[] => [],
    getFlag: () => undefined,
  };
}

async function handleToolExecute(
  tools: Map<string, ToolDescriptor>,
  payload: unknown,
  transport: Transport,
): Promise<ToolResult> {
  const p = payload as { tool_call_id: string; name: string; args: unknown };
  const desc = tools.get(p.name);
  if (!desc) {
    return { content: [{ type: "text", text: `unknown tool: ${p.name}` }], isError: true };
  }
  const onUpdate: UpdateFn = (partial) => {
    transport.notify("pi.extension/tool_update", { tool_call_id: p.tool_call_id, partial });
  };
  try {
    return await desc.execute(p.tool_call_id, p.args, undefined, onUpdate, null);
  } catch (err) {
    return {
      content: [{ type: "text", text: err instanceof Error ? err.message : String(err) }],
      isError: true,
    };
  }
}
```

- [ ] **Step 4: Create the test**

Create `packages/extension-sdk/src/test/api-impl.test.ts`:
```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import { Transport } from "../transport.js";
import { createExtensionAPI } from "../api-impl.js";
import { NotImplementedError, CapabilityDeniedError } from "../errors.js";

function pair(): { reader: Readable; writer: Writable; written: Buffer[] } {
  const written: Buffer[] = [];
  return {
    reader: new Readable({ read() {} }),
    writer: new Writable({ write(c, _e, cb) { written.push(c); cb(); } }),
    written,
  };
}

test("registerTool with grant sends host_call", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] },
    [{ service: "tools", version: 1, methods: ["register"] }]);
  api.registerTool({
    name: "greet", label: "g", description: "g", parameters: {}, execute: async () => ({ content: [] }),
  });
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(written).toString().trim());
  assert.equal(sent.method, "pi.extension/host_call");
  assert.equal(sent.params.service, "tools");
  assert.equal(sent.params.method, "register");
  t.close();
});

test("registerTool without grant throws CapabilityDenied", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] }, []);
  await assert.rejects(async () => {
    api.registerTool({
      name: "greet", label: "g", description: "g", parameters: {}, execute: async () => ({ content: [] }),
    });
    // trigger microtask
    await new Promise((r) => setImmediate(r));
  }, CapabilityDeniedError);
  t.close();
});

test("registerCommand throws NotImplementedError", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] }, []);
  assert.throws(() => api.registerCommand("x", { description: "d", handler: async () => {} }), NotImplementedError);
  t.close();
});
```

- [ ] **Step 5: Build and test**

Run: `cd packages/extension-sdk && npm run build && node --test dist/test/api-impl.test.js`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add packages/extension-sdk/src/api.ts packages/extension-sdk/src/extended.ts packages/extension-sdk/src/api-impl.ts packages/extension-sdk/src/test/api-impl.test.ts
rtk git commit -m "feat(sdk): ExtensionAPI interface and RPC-backed implementation"
```

---

## Task 19: SDK index + Type re-export

**Files:**
- Modify: `packages/extension-sdk/src/index.ts`

- [ ] **Step 1: Replace index.ts**

Replace `packages/extension-sdk/src/index.ts`:
```typescript
export type { ExtensionAPI, CustomMessage, SendOptions, EventBus } from "./api.js";
export type {
  ToolDescriptor, ToolResult, ToolInfo, ContentPart, UpdateFn,
} from "./tools.js";
export type {
  EventHandler, EventName, EventResult, EventControl,
  SessionStartEvent,
} from "./events.js";
export { EventNames } from "./events.js";
export type {
  Metadata, ModelRef, ContextUsage, ThinkingLevel,
  ExecOptions, ExecResult, SourceInfo,
} from "./types.js";
export type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo, AutocompleteItem,
} from "./extended.js";
export { Transport, connectStdio } from "./transport.js";
export { createExtensionAPI } from "./api-impl.js";
export type { GrantedService } from "./api-impl.js";
export { NotImplementedError, CapabilityDeniedError } from "./errors.js";
// Re-export TypeBox for parameter schemas.
export { Type } from "@sinclair/typebox";
```

- [ ] **Step 2: Build**

Run: `cd packages/extension-sdk && npm run build`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
rtk git add packages/extension-sdk/src/index.ts
rtk git commit -m "feat(sdk): public exports including Type re-export"
```

---

## Task 20: SDK full test run

**Files:** none

- [ ] **Step 1: Run full SDK test suite**

Run: `cd packages/extension-sdk && npm run build && node --test dist/test/*.test.js`
Expected: all tests pass.

- [ ] **Step 2: Commit (if changes needed)**

Skip if no changes. Otherwise:
```bash
rtk git add packages/extension-sdk/
rtk git commit -m "test(sdk): full test pass"
```

---

## Task 21: Scaffold `@pi-go/extension-host`

**Files:**
- Create: `packages/extension-host/package.json`
- Create: `packages/extension-host/tsconfig.json`
- Create: `packages/extension-host/src/cli.ts`
- Create: `packages/extension-host/.gitignore`

- [ ] **Step 1: Create package.json**

Create `packages/extension-host/package.json`:
```json
{
  "name": "@pi-go/extension-host",
  "version": "0.1.0",
  "description": "Node runtime for pi-go hosted TypeScript extensions",
  "type": "module",
  "bin": {
    "pi-go-extension-host": "./dist/cli.js"
  },
  "main": "./dist/cli.js",
  "files": ["dist"],
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "bundle": "node build.mjs",
    "test": "node --test dist/test",
    "clean": "rm -rf dist"
  },
  "dependencies": {
    "@pi-go/extension-sdk": "file:../extension-sdk",
    "@sinclair/typebox": "^0.34.0",
    "jiti": "^2.4.0"
  },
  "devDependencies": {
    "@types/node": "^20.0.0",
    "esbuild": "^0.24.0",
    "typescript": "^5.4.0"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

Create `packages/extension-host/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "declaration": false,
    "sourceMap": true,
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true
  },
  "include": ["src/**/*.ts"]
}
```

- [ ] **Step 3: Create placeholder cli.ts**

Create `packages/extension-host/src/cli.ts`:
```typescript
#!/usr/bin/env node
// Populated in Task 22.
console.error("@pi-go/extension-host not yet implemented");
process.exit(1);
```

- [ ] **Step 4: Create .gitignore**

```
node_modules
dist
*.log
```

- [ ] **Step 5: Install and build**

Run: `cd packages/extension-host && npm install && npm run build`

- [ ] **Step 6: Commit**

```bash
rtk git add packages/extension-host/package.json packages/extension-host/tsconfig.json packages/extension-host/src/cli.ts packages/extension-host/.gitignore
rtk git commit -m "feat(host): scaffold @pi-go/extension-host"
```

---

## Task 22: Host CLI + jiti loader + runtime

**Files:**
- Create: `packages/extension-host/src/loader.ts`
- Create: `packages/extension-host/src/runtime.ts`
- Modify: `packages/extension-host/src/cli.ts`
- Create: `packages/extension-host/src/test/cli.test.ts`

- [ ] **Step 1: Write loader.ts**

Create `packages/extension-host/src/loader.ts`:
```typescript
import { createJiti } from "jiti";
import { pathToFileURL } from "node:url";

export interface LoadedExtension {
  register: (pi: unknown) => void | Promise<void>;
}

export async function loadExtension(entry: string, cwd: string): Promise<LoadedExtension> {
  const jiti = createJiti(pathToFileURL(cwd + "/").href, { interopDefault: true });
  const mod = (await jiti.import(entry)) as { default?: (pi: unknown) => void };
  if (typeof mod.default !== "function") {
    throw new Error(`extension at ${entry} does not export a default function`);
  }
  return { register: mod.default };
}
```

- [ ] **Step 2: Write runtime.ts**

Create `packages/extension-host/src/runtime.ts`:
```typescript
import {
  connectStdio, createExtensionAPI, Transport,
  type GrantedService, type Metadata,
} from "@pi-go/extension-sdk";
import { loadExtension } from "./loader.js";

export interface RuntimeOptions {
  entry: string;
  name: string;
  cwd: string;
}

export async function runExtensionHost(opts: RuntimeOptions): Promise<void> {
  const transport = connectStdio();
  redirectConsole(transport);

  // Handshake: wait for the host's response after we send our request.
  // The SDK driver sends the handshake; here we handle wiring.
  const loaded = await loadExtension(opts.entry, opts.cwd);

  // We need to handshake first. For spec #1 we assume the extension file
  // declares its metadata implicitly via package.json; for single-file
  // .ts extensions we derive minimal metadata from the CLI args.
  const metadata: Metadata = {
    name: opts.name,
    version: "0.0.0",
    requestedCapabilities: [],
  };

  // Build handshake request. Since TS SDK doesn't own metadata parsing
  // in spec #1, we request broad capabilities and rely on approvals.json
  // to filter at the host side.
  const hsResult = (await transport.call("pi.extension/handshake", {
    protocol_version: "2.1",
    extension_id: metadata.name,
    extension_version: metadata.version,
    requested_services: [
      { service: "tools", version: 1, methods: ["register"] },
      { service: "events", version: 1, methods: ["session_start"] },
      { service: "exec", version: 1, methods: ["shell"] },
    ],
  })) as { granted_services: GrantedService[]; protocol_version: string };

  if (hsResult.protocol_version !== "2.1") {
    throw new Error(`unsupported protocol version: ${hsResult.protocol_version}`);
  }

  const api = createExtensionAPI(transport, metadata, hsResult.granted_services);
  await loaded.register(api);

  // Wait for shutdown.
  await new Promise<void>((resolve) => {
    transport.handle("pi.extension/shutdown", () => {
      resolve();
      return {};
    });
  });
}

function redirectConsole(transport: Transport): void {
  const redirect = (level: string) => (...args: unknown[]) => {
    const message = args.map((a) => (typeof a === "string" ? a : JSON.stringify(a))).join(" ");
    transport.notify("pi.extension/log", { level, message });
  };
  console.log = redirect("info");
  console.info = redirect("info");
  console.warn = redirect("warn");
  console.error = redirect("error");
}
```

- [ ] **Step 3: Write cli.ts**

Replace `packages/extension-host/src/cli.ts`:
```typescript
#!/usr/bin/env node
import { runExtensionHost } from "./runtime.js";

interface ParsedArgs {
  entry?: string;
  name?: string;
  cwd?: string;
}

function parseArgs(argv: string[]): ParsedArgs {
  const out: ParsedArgs = {};
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--entry") out.entry = argv[++i];
    else if (arg === "--name") out.name = argv[++i];
    else if (arg === "--cwd") out.cwd = argv[++i];
  }
  return out;
}

const args = parseArgs(process.argv.slice(2));
if (!args.entry) {
  process.stderr.write("pi-go-extension-host: --entry is required\n");
  process.exit(2);
}

const name = args.name ?? deriveName(args.entry);
const cwd = args.cwd ?? process.cwd();

function deriveName(entry: string): string {
  const base = entry.split(/[\\/]/).pop() ?? "extension";
  return base.replace(/\.[^.]+$/, "").replace(/[^a-z0-9_-]/gi, "-").toLowerCase() || "extension";
}

runExtensionHost({ entry: args.entry, name, cwd }).catch((err) => {
  process.stderr.write(`pi-go-extension-host: ${err instanceof Error ? err.message : String(err)}\n`);
  process.exit(1);
});
```

- [ ] **Step 4: Write CLI test**

Create `packages/extension-host/src/test/cli.test.ts`:
```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const cli = resolve(__dirname, "../cli.js");

test("CLI exits 2 when --entry missing", () => {
  const res = spawnSync("node", [cli], { encoding: "utf8" });
  assert.equal(res.status, 2);
  assert.match(res.stderr, /--entry is required/);
});
```

- [ ] **Step 5: Build and test**

Run: `cd packages/extension-host && npm install && npm run build && node --test dist/test/cli.test.js`
Expected: CLI test PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add packages/extension-host/src/cli.ts packages/extension-host/src/loader.ts packages/extension-host/src/runtime.ts packages/extension-host/src/test/cli.test.ts packages/extension-host/package-lock.json
rtk git commit -m "feat(host): CLI, jiti loader, runtime wiring with stdout redirect"
```

---

## Task 23: Host esbuild bundle for embedding

**Files:**
- Create: `packages/extension-host/build.mjs`
- Create: `packages/extension-host/dist/.keep` (placeholder so directory is tracked)

- [ ] **Step 1: Write build.mjs**

Create `packages/extension-host/build.mjs`:
```javascript
import { build } from "esbuild";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));

await build({
  entryPoints: [resolve(__dirname, "src/cli.ts")],
  bundle: true,
  platform: "node",
  target: "node20",
  format: "esm",
  outfile: resolve(__dirname, "dist/host.bundle.js"),
  external: ["jiti"],
  sourcemap: false,
  minify: true,
  banner: {
    js: "#!/usr/bin/env node",
  },
});

console.log("Built dist/host.bundle.js");
```

- [ ] **Step 2: Run bundle**

Run: `cd packages/extension-host && npm run bundle`
Expected: `dist/host.bundle.js` produced.

- [ ] **Step 3: Smoke test the bundle**

Run: `node packages/extension-host/dist/host.bundle.js`
Expected: exits with code 2 and "--entry is required" on stderr.

- [ ] **Step 4: Commit**

```bash
rtk git add packages/extension-host/build.mjs
rtk git commit -m "build(host): esbuild bundle for embedding into pi-go binary"
```

---

## Task 24: Hostproto v2.1 wire types

**Files:**
- Delete: existing `internal/extension/hostproto/protocol.go` contents
- Rewrite: `internal/extension/hostproto/protocol.go`
- Rewrite: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Write the new protocol.go**

Replace `internal/extension/hostproto/protocol.go`:
```go
package hostproto

import "encoding/json"

// ProtocolVersion is the wire contract between pi-go and extensions.
const ProtocolVersion = "2.1"

// Error codes.
const (
    ErrCodeServiceUnsupported = -32001
    ErrCodeMethodNotFound     = -32002
    ErrCodeCapabilityDenied   = -32003
    ErrCodeEventNotSupported  = -32004
    ErrCodeHandlerTimeout     = -32005
    ErrCodeHandshakeFailed    = -32006
)

// Method names.
const (
    MethodHandshake      = "pi.extension/handshake"
    MethodHostCall       = "pi.extension/host_call"
    MethodSubscribeEvent = "pi.extension/subscribe_event"
    MethodExtensionEvent = "pi.extension/extension_event"
    MethodToolUpdate     = "pi.extension/tool_update"
    MethodLog            = "pi.extension/log"
    MethodShutdown       = "pi.extension/shutdown"
)

// HandshakeRequest is the payload the extension sends first.
type HandshakeRequest struct {
    ProtocolVersion   string             `json:"protocol_version"`
    ExtensionID       string             `json:"extension_id"`
    ExtensionVersion  string             `json:"extension_version"`
    RequestedServices []RequestedService `json:"requested_services"`
}

// RequestedService is a single service/method set the extension wants.
type RequestedService struct {
    Service string   `json:"service"`
    Version int      `json:"version"`
    Methods []string `json:"methods"`
}

// HandshakeResponse is the host's reply.
type HandshakeResponse struct {
    ProtocolVersion    string           `json:"protocol_version"`
    GrantedServices    []GrantedService `json:"granted_services"`
    HostServices       []HostServiceInfo `json:"host_services"`
    DispatchableEvents []DispatchableEvent `json:"dispatchable_events"`
}

// GrantedService is a post-filter view of a requested service.
type GrantedService struct {
    Service      string   `json:"service"`
    Version      int      `json:"version"`
    Methods      []string `json:"methods"`
    DeniedReason string   `json:"denied_reason,omitempty"`
}

// HostServiceInfo describes a service offered by the host.
type HostServiceInfo struct {
    Service string   `json:"service"`
    Version int      `json:"version"`
    Methods []string `json:"methods"`
}

// DispatchableEvent is an event the host is willing to dispatch.
type DispatchableEvent struct {
    Name    string `json:"name"`
    Version int    `json:"version"`
}

// HostCallParams is the payload for host_call.
type HostCallParams struct {
    Service string          `json:"service"`
    Version int             `json:"version"`
    Method  string          `json:"method"`
    Payload json.RawMessage `json:"payload"`
}

// SubscribeEventParams is the payload for subscribe_event.
type SubscribeEventParams struct {
    Events []EventSubscription `json:"events"`
}

// EventSubscription identifies one event the extension wants dispatched.
type EventSubscription struct {
    Name    string `json:"name"`
    Version int    `json:"version"`
}

// ExtensionEventParams is the payload for extension_event (host → ext).
type ExtensionEventParams struct {
    Event      string          `json:"event"`
    Version    int             `json:"version"`
    Payload    json.RawMessage `json:"payload"`
    Context    json.RawMessage `json:"context,omitempty"`
    DeadlineMs int             `json:"deadline_ms,omitempty"`
}
```

- [ ] **Step 2: Write tests**

Replace `internal/extension/hostproto/protocol_test.go`:
```go
package hostproto

import (
    "encoding/json"
    "testing"
)

func TestHandshakeRequest_RoundTrip(t *testing.T) {
    req := HandshakeRequest{
        ProtocolVersion:  ProtocolVersion,
        ExtensionID:      "hello",
        ExtensionVersion: "0.1.0",
        RequestedServices: []RequestedService{
            {Service: "tools", Version: 1, Methods: []string{"register"}},
        },
    }
    b, err := json.Marshal(req)
    if err != nil {
        t.Fatal(err)
    }
    var round HandshakeRequest
    if err := json.Unmarshal(b, &round); err != nil {
        t.Fatal(err)
    }
    if round.ProtocolVersion != "2.1" {
        t.Fatalf("protocol_version lost in round trip: %q", round.ProtocolVersion)
    }
    if len(round.RequestedServices) != 1 {
        t.Fatalf("services lost")
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/extension/hostproto/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
rtk git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go
rtk git commit -m "refactor(hostproto): rewrite for v2.1 with bidirectional dispatch types"
```

---

## Task 25: Loader — Candidate types

**Files:**
- Create: `internal/extension/loader/candidate.go`
- Create: `internal/extension/loader/candidate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/loader/candidate_test.go`:
```go
package loader

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDetectMode_SingleTS(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "hello.ts")
    if err := os.WriteFile(path, []byte("export default () => {}"), 0644); err != nil {
        t.Fatal(err)
    }
    m, err := detectMode(path)
    if err != nil {
        t.Fatal(err)
    }
    if m != ModeHostedTS {
        t.Fatalf("expected hosted-ts; got %v", m)
    }
}

func TestDetectMode_PackageJSON(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "package.json"),
        []byte(`{"name":"x","pi":{"entry":"./src/index.ts"}}`), 0644); err != nil {
        t.Fatal(err)
    }
    m, err := detectMode(dir)
    if err != nil {
        t.Fatal(err)
    }
    if m != ModeHostedTS {
        t.Fatalf("expected hosted-ts from package.json; got %v", m)
    }
}

func TestDetectMode_PiToml(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "pi.toml"),
        []byte(`name="x"` + "\n" + `version="0.1"` + "\n" + `runtime="hosted"`), 0644); err != nil {
        t.Fatal(err)
    }
    m, err := detectMode(dir)
    if err != nil {
        t.Fatal(err)
    }
    if m != ModeHostedGo {
        t.Fatalf("expected hosted-go; got %v", m)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extension/loader/...`
Expected: FAIL.

- [ ] **Step 3: Write implementation**

Create `internal/extension/loader/candidate.go`:
```go
package loader

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/dimetron/pi-go/pkg/piapi"
)

// Mode identifies how a candidate is executed.
type Mode int

const (
    ModeUnknown Mode = iota
    ModeCompiledIn
    ModeHostedGo
    ModeHostedTS
)

func (m Mode) String() string {
    switch m {
    case ModeCompiledIn:
        return "compiled-in"
    case ModeHostedGo:
        return "hosted-go"
    case ModeHostedTS:
        return "hosted-ts"
    default:
        return "unknown"
    }
}

// Candidate is a discovered extension waiting to be registered.
type Candidate struct {
    Mode     Mode
    Path     string // file or directory
    Dir      string // containing directory
    Metadata piapi.Metadata
    Command  []string // hosted only
}

func detectMode(path string) (Mode, error) {
    info, err := os.Stat(path)
    if err != nil {
        return ModeUnknown, err
    }
    if !info.IsDir() {
        if strings.HasSuffix(path, ".ts") {
            return ModeHostedTS, nil
        }
        return ModeUnknown, fmt.Errorf("loader: unsupported single-file extension %q", path)
    }
    // Directory: inspect contents in priority order.
    if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
        var pkg struct {
            Pi *struct {
                Entry string `json:"entry"`
            } `json:"pi"`
        }
        data, err := os.ReadFile(filepath.Join(path, "package.json"))
        if err == nil {
            _ = json.Unmarshal(data, &pkg)
            if pkg.Pi != nil && pkg.Pi.Entry != "" {
                return ModeHostedTS, nil
            }
        }
    }
    if _, err := os.Stat(filepath.Join(path, "pi.toml")); err == nil {
        return ModeHostedGo, nil
    }
    if _, err := os.Stat(filepath.Join(path, "pi.json")); err == nil {
        return ModeHostedGo, nil
    }
    if _, err := os.Stat(filepath.Join(path, "index.ts")); err == nil {
        return ModeHostedTS, nil
    }
    return ModeUnknown, fmt.Errorf("loader: cannot determine mode for %q", path)
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/extension/loader/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/loader/candidate.go internal/extension/loader/candidate_test.go
rtk git commit -m "feat(loader): Mode detection for single-file TS, package.json, pi.toml"
```

---

## Task 26: Loader Discover + metadata parsing

**Files:** `internal/extension/loader/discover.go`, `loader/metadata.go`, `loader/discover_test.go`; modify `go.mod` to add `github.com/BurntSushi/toml`.

- [ ] Add dep: `go get github.com/BurntSushi/toml@latest`
- [ ] Write `metadata.go` with `parsePiToml`, `parsePackageJSON`, `parseMetadataFromFile` returning `(piapi.Metadata, []string, error)`. `pi.toml` fields: name, version, description, prompt, runtime, command, requested_capabilities. `package.json` reads the "pi" block (entry, description, prompt, requested_capabilities).
- [ ] Write `discover.go` with `Discover(cwd) ([]Candidate, error)` that walks `~/.pi-go/packages`, `~/.pi-go/extensions`, `.pi-go/packages`, `.pi-go/extensions` in order, dedups by name (last wins), sorts output. Export helper `UserHome()` for reuse — handles Windows `USERPROFILE`.
- [ ] Test: `TestDiscover_LayeredOverrides` writes ext-a v0.1 to home and v0.2 to project, asserts project wins. `TestDiscover_HomeOnly` asserts home extensions are discovered when project has none.
- [ ] Run: `go test ./internal/extension/loader/...` → PASS.
- [ ] Commit: `feat(loader): four-layer Discover with metadata parsing`

---

## Task 27: Host Capability Gate

**Files:** `internal/extension/host/capability.go`, `capability_test.go`.

- [ ] Define `TrustClass` constants: `TrustCompiledIn`, `TrustFirstParty`, `TrustThirdParty`.
- [ ] `Gate` struct with path + mu + data. `approvalsFile` holds Version int and Extensions map. `approvalsEntry` fields: TrustClass, FirstParty, Approved, ApprovedAt, GrantedCapabilities, DeniedCapabilities.
- [ ] `NewGate(path)` loads file (missing file is fine — starts empty). `Reload()` re-reads. `Allowed(id, capability, trust) (bool, string)` returns true for `TrustCompiledIn`; otherwise requires `Approved=true` and membership in granted minus denied. `Grants(id, trust) []string` returns granted caps (or a star sentinel for compiled-in).
- [ ] Tests: compiled-in always allowed; third-party with `tools.register` granted but `events.session_start` denied.
- [ ] Commit: `feat(host): tiered trust Gate reading approvals.json v2`

---

## Task 28: Host RPC connection

**Files:** `internal/extension/host/rpc.go`, `rpc_test.go`.

- [ ] `RPCConn` exposes `Call(ctx, method, params, result)`, `Notify(method, params)`, `Close()`. Inbound handler signature is `func(method string, params json.RawMessage) (any, error)` installed via `NewRPCConn(in, out, handler)`. JSON-RPC 2.0 line-delimited framing, mutex around writes, response routing via pending map keyed by id.
- [ ] Error codes taken from `hostproto` package constants. Inbound handler errors are encoded as `{error:{code,message}}` responses.
- [ ] Tests: `TestRPCConn_Call` pipes a simulated extension that returns `{ok:true}` and asserts `Call` unmarshals the result. `TestRPCConn_Notify` asserts notification has no id field. `TestRPCConn_InboundRequest` installs handler returning a map and asserts correctly-shaped response.
- [ ] Commit: `feat(host): per-extension JSON-RPC 2.0 connection`

---

## Task 29: Host Event Dispatcher

**Files:** `internal/extension/host/dispatch.go`, `dispatch_test.go`.

- [ ] `Subscriber { ExtensionID string; Call func(ctx, params) (piapi.EventResult, error) }`. `Dispatcher` exposes `Subscribe(event, sub)`, `Unsubscribe(extensionID)` (removes all subscriptions for that extension), `Dispatch(ctx, event, payload, contextInfo) DispatchResult`.
- [ ] Fan-out: spawn a goroutine per subscriber with a 30s timeout each, await all, aggregate. `aggregate(results)` implements spec §5 rules: `Cancelled` if any subscriber returns cancel; `Blocked` + first `Reason` when any returns block. Transform composition is a spec #3 concern — leave the field on the result struct, don't populate it in spec #1.
- [ ] Tests: cancel aggregation (2 subscribers, one cancels → cancelled result); block-first-wins (2 blockers → first reason wins); no-subscribers returns empty result.
- [ ] Commit: `feat(host): event dispatch fanout with cancel/block aggregation`

---

## Task 30: Host Manager

**Files:** `internal/extension/host/manager.go`, `manager_test.go`.

- [ ] `State` constants: `StatePending`, `StateReady`, `StateRunning`, `StateStopped`, `StateErrored`, `StateDenied`. `Registration { ID, Mode, Trust, Metadata, State, Err, API, Conn }`.
- [ ] `Manager` wraps gate, dispatcher, regs map. `NewManager(gate)`, `Register(*Registration) error` (compiled-in → Ready; hosted with grants → Ready; hosted without grants → Pending; duplicate id rejected). `Get`, `List`, `SetState(id, state, err)`, `Shutdown(ctx)` notifies shutdown and closes all connections.
- [ ] Tests: compiled-in ready; hosted without approval pending; duplicate id rejected.
- [ ] Commit: `feat(host): Manager with state machine and capability-gate integration`

---

## Task 31: api/compiled.go — direct piapi.API

**Files:** `internal/extension/api/compiled.go`, `compiled_test.go`.

- [ ] `compiledAPI` implements every `piapi.API` method. `RegisterTool` validates + stores. `On(EventSessionStart, h)` subscribes to manager dispatcher. All non-spec-#1 methods return `piapi.ErrNotImplemented{Method, Spec}` with the correct spec tag per the §3 table. `Exec` runs via `os/exec` — no gate (compiled-in is trusted).
- [ ] Expose `Tools() map[string]piapi.ToolDescriptor` and `Handlers() map[string][]piapi.EventHandler` on the concrete type for the runtime assembler to read.
- [ ] Tests: `RegisterTool` → tool lands in map; `SendMessage` returns `ErrNotImplementedSentinel`.
- [ ] Commit: `feat(api): compiled-in piapi.API implementation (direct, in-process)`

---

## Task 32: api/hosted.go — host-side inbound handler

**Files:** `internal/extension/api/hosted.go`.

- [ ] `HostedAPIHandler { manager, reg, tools }`. `Handle(method, params)` dispatches by JSON-RPC method name:
  - `pi.extension/host_call` → gate-check `service.method` → handle `tools.register` (unmarshal descriptor, store in `h.tools`, `Execute` dispatches back via `reg.Conn.Call(MethodExtensionEvent, tool_execute, payload)`).
  - `pi.extension/subscribe_event` → gate-check each `events.<name>` capability → subscribe to `manager.Dispatcher()` with a Call that forwards via `reg.Conn.Call`.
  - `pi.extension/tool_update` and `pi.extension/log` → no-ops in spec #1.
- [ ] `Tools()` accessor for the runtime assembler.
- [ ] Commit: `feat(api): host-side handler for hosted-extension inbound RPC`

---

## Task 33: Compiled registry

**Files:** `internal/extension/compiled/registry.go`.

- [ ] `Entry { Name string; Register piapi.Register; Metadata piapi.Metadata }`. `var Compiled []Entry`. Extensions under `internal/extensions/*` append to `Compiled` via their package `init()` functions.
- [ ] Commit: `feat(compiled): registry for compiled-in extensions`

---

## Task 34: go:embed vendored host

**Files:** `internal/extension/host/embed.go`, `embed_test.go`, `embedded/host.bundle.js` (copied from Task 23 output).

- [ ] `//go:embed embedded/host.bundle.js` + `var embeddedHost []byte`. `ExtractedHostPath(version) (string, error)` uses `sync.Once` to extract to `~/.pi-go/cache/extension-host/<version>/host.bundle.js`. Windows uses `%LOCALAPPDATA%/pi-go/cache/...`.
- [ ] Before Go build, copy: `cp packages/extension-host/dist/host.bundle.js internal/extension/host/embedded/`. Document this build step in the repo README or makefile.
- [ ] Test with temp HOME, verify extracted file exists.
- [ ] Commit: `feat(host): go:embed vendored Node host bundle with lazy extraction`

---

## Task 35: Move resources discovery into loader

**Files:** move `internal/extension/resources.go` and `resources_test.go` into `internal/extension/loader/`.

- [ ] `git mv` both files, change the package declaration to `loader` at top of each. Update callers via `rtk grep "extension.DiscoverResourceDirs"` and replace with `loader.DiscoverResourceDirs`.
- [ ] Build + test. Commit: `refactor(extension): move resources discovery into loader package`

---

## Task 36: Loader Reload

**Files:** `internal/extension/loader/reload.go`, `reload_test.go`.

- [ ] `Reload(ctx, *host.Manager, cwd) ([]Candidate, error)` → `m.Shutdown(ctx)`, `m.Gate().Reload()`, `return Discover(cwd)`. Spec #5 will wire this into the user-facing `ctx.Reload()`.
- [ ] Test: empty temp cwd → empty candidates, no error.
- [ ] Commit: `feat(loader): Reload orchestrating shutdown + re-discover`

---

## Task 37: Delete legacy files

Remove via `git rm`:
- `internal/extension/manifest.go`, `manifest_test.go`
- `internal/extension/hooks.go`, `hooks_test.go`
- `internal/extension/skill_template.go`, `skill_template.md`
- `internal/extension/manager.go`, `manager_test.go`
- `internal/extension/registry.go`
- `internal/extension/permissions.go`, `permissions_test.go`
- `internal/extension/events.go`, `intents.go`, `packages.go`
- `internal/extension/sdk/` (entire dir)
- `internal/extension/services/` (entire dir)
- `internal/extension/hostruntime/` (entire dir)
- `internal/extension/test_helpers_test.go`
- `examples/extensions/hosted-hello/` (entire dir)

**Keep:** `runtime.go` (rewritten in Task 38), `mcp.go`, `provider_registry.go`, `state_store.go`, `hosted_hello_e2e_test.go` (rewritten in Task 43).

- [ ] `go build ./...` → expected FAIL (missing types). Task 38 fixes.
- [ ] Commit: `chore(extension): remove legacy manifest/hooks/services code`

---

## Task 38: Rewire BuildRuntime

**Files:** rewrite `internal/extension/runtime.go`; create `internal/extension/api/adapter.go`; fix TUI callers.

- [ ] `BuildRuntime(ctx, RuntimeConfig) (*Runtime, error)` creates core tools, discovers candidates via `loader.Discover`, creates `host.NewGate(approvalsPath)` + `host.NewManager(gate)`. Registers each `compiled.Compiled` entry: `manager.Register(reg)` → `api.NewCompiled(reg, manager)` → `entry.Register(api)`. For each hosted candidate: `manager.Register(reg)` (starting hosted processes is the caller's responsibility via Task 39 `LaunchHosted`).
- [ ] `Runtime` struct exposes: `Extensions []*host.Registration`, `Tools []tool.Tool`, `Manager *host.Manager`, `Instruction string` (prepends `# Extension: <name>\n\n<prompt>` per active compiled-in extension), plus `PromptTemplates`, `ProviderRegistry`, `SkillDirs`, `ThemeDirs`. Legacy caller-facing fields (`SlashCommands`, `BeforeToolCallbacks`, `AfterToolCallbacks`, `LifecycleHooks`) are preserved as empty slices to keep callers compiling.
- [ ] `adapter.go`: `newPiapiToolAdapter(piapi.ToolDescriptor) tool.Tool`. **Implementer must match the actual ADK `tool.Tool` interface by reading an existing core tool in `internal/tools/*.go`.** Wrap `desc.Execute` as the Call implementation; expose `desc.Name`, `desc.Description`, `desc.Parameters` via the matching interface methods.
- [ ] Fix TUI callers: `rtk grep -l "extension.SlashCommand\|extension.Manifest\|extension.HookConfig\|BeforeToolCallbacks\|AfterToolCallbacks\|LifecycleHooks"` — for each match replace `extension.SlashCommand` with `loader.SlashCommand`, drop references to the legacy callback slices (they stay empty), and adjust imports accordingly.
- [ ] Create `loader/types.go` with placeholder `SlashCommand`, `Skill`, `PromptTemplate` types (with a `Render([]string) string` method) and a `LoadPromptTemplates(...dirs) ([]PromptTemplate, error)` stub — **only if** the moved `resources.go` from Task 35 doesn't already define them. Check first.
- [ ] Build: `go build ./...` → clean.
- [ ] Commit: `refactor(extension): rewire BuildRuntime to loader+host+api packages`

---

## Task 39: Hosted process launch

**Files:** `internal/extension/host/launch.go`.

- [ ] `LaunchHosted(ctx, reg *Registration, manager *Manager, command []string) error`: `exec.CommandContext(ctx, command[0], command[1:]...)`, pipe stdin/stdout, `cmd.Start()`, wrap in `NewRPCConn` with an inbound handler that routes `hostproto.MethodHandshake` to a local handshake builder (reads `manager.Gate().Grants(reg.ID, reg.Trust)`, constructs `HandshakeResponse` with `ProtocolVersion=2.1`, granted services grouped by service name, and the set of dispatchable events — for spec #1 just `session_start`). All other methods delegate to `api.NewHostedHandler(manager, reg).Handle`. Set `reg.Conn = conn`; `manager.SetState(reg.ID, StateRunning, nil)`.
- [ ] Unit tests thin; E2E tests in Tasks 43–44 exercise the full flow.
- [ ] Commit: `feat(host): LaunchHosted with stdio pipe wiring and handshake response`

---

## Task 40: hosted-hello-go example

**Files:** `examples/extensions/hosted-hello-go/{go.mod,main.go,pi.toml,README.md}`.

- [ ] `go.mod` for module `.../examples/extensions/hosted-hello-go` with replace directives to `../../../pkg/piapi` and `../../../pkg/piext`.
- [ ] `main.go`: package-level `Metadata` var with name, version, description, `RequestedCapabilities: []string{"tools.register", "events.session_start", "events.tool_execute"}`. `main()` calls `piext.Run(Metadata, register)`. `register` subscribes to `session_start` (logs via `piext.Log()`) and registers a `greet` tool whose parameters come from `piext.SchemaFromStruct` of a `struct{Name string}` with appropriate struct tags. `Execute` unmarshals args, returns `{Type: "text", Text: "Hello, <name>!"}`.
- [ ] `pi.toml`: runtime=hosted, command=["go", "run", "."], full capabilities list matching `Metadata.RequestedCapabilities`.
- [ ] `README.md` documents build + standalone run.
- [ ] Verify: `cd examples/extensions/hosted-hello-go && go build .` → builds.
- [ ] Commit: `feat(examples): hosted-hello-go E2E fixture`

---

## Task 41: hosted-hello-ts example

**Files:** `examples/extensions/hosted-hello-ts/{package.json,tsconfig.json,src/index.ts,README.md}`.

- [ ] `package.json` with name, version, `type:"module"`, dependency on `@pi-go/extension-sdk` via `file:../../../packages/extension-sdk`, and a `pi` block containing entry, description, requested_capabilities.
- [ ] `src/index.ts`: `export default function(pi: ExtensionAPI)` subscribes to `session_start` (logs via `console.log`) and registers `greet` tool with `Type.Object({ name: Type.String({ description: "Name to greet" }) })`. Execute returns the greeting.
- [ ] Run `npm install` once in the extension directory to vendor deps.
- [ ] Commit: `feat(examples): hosted-hello-ts E2E fixture`

---

## Task 42: E2E compiled-in

**Files:** `internal/extensions/hello/hello.go`, `internal/extension/e2e_compiled_test.go`.

- [ ] `hello.go`: `Metadata` var with name `hello`, version `0.1`. `Register(pi piapi.API) error` subscribes to `session_start` (returns no-op EventResult) and registers `greet` tool returning a single text content part `"hi"`. Package `init()` appends the entry to `compiled.Compiled`.
- [ ] `e2e_compiled_test.go`: blank-import the hello package via `_ "github.com/dimetron/pi-go/internal/extensions/hello"`. `TestE2E_CompiledIn` calls `BuildRuntime(ctx, RuntimeConfig{WorkDir: t.TempDir()})` and asserts that `rt.Tools` contains a tool named `greet`.
- [ ] Commit: `test(e2e): compiled-in hello extension registers tool via BuildRuntime`

---

## Task 43: E2E hosted-go

**Files:** `internal/extension/e2e_hosted_go_test.go`, `internal/extension/testdata/approvals_granted_hello.json`.

- [ ] Approvals v2 with `hosted-hello-go` approved, granted `tools.register`, `events.session_start`, `events.tool_execute`.
- [ ] `TestE2E_HostedGo`: build temp HOME, symlink `examples/extensions/hosted-hello-go` into `<tmp>/.pi-go/extensions/hosted-hello-go` (skip on symlink failure — Windows without admin). Copy approvals into `<tmp>/.pi-go/extensions/approvals.json`. Set `HOME` and `USERPROFILE` env. Call `BuildRuntime(cwd=tmp)`. Assert `hosted-hello-go` registration is present. Call `host.LaunchHosted(ctx, reg, manager, []string{"go", "run", "."})`. Sleep 500ms for handshake. `manager.Shutdown(ctx)`. Assert state transitions went pending → ready → running.
- [ ] Commit: `test(e2e): hosted-go discovery, launch, handshake`

---

## Task 44: E2E hosted-ts

**Files:** `internal/extension/e2e_hosted_ts_test.go`.

- [ ] Skip the test when `exec.LookPath("node")` fails. Same setup as Task 43 but for `hosted-hello-ts`. After discovery, call `host.ExtractedHostPath("test")` to extract the vendored bundle, then `LaunchHosted(ctx, reg, manager, []string{"node", hostPath, "--entry", abs(entry.ts), "--name", "hosted-hello-ts"})`. Sleep 1s to allow Node startup + handshake.
- [ ] Commit: `test(e2e): hosted-ts via pi-go-extension-host with node`

---

## Task 45: E2E trust scenarios

**Files:** `internal/extension/e2e_trust_test.go`, `internal/extension/testdata/approvals_empty.json`.

- [ ] `TestE2E_CompiledInBypassesGate`: no `approvals.json` file present; compiled-in hello must still reach `StateReady`.
- [ ] `TestE2E_HostedWithoutApprovalPending`: symlink `hosted-hello-go` into `.pi-go/extensions/` with no `approvals.json`; assert state is `StatePending`.
- [ ] `TestE2E_ProtocolDowngrade` (unit-level): directly invoke the inbound handshake handler with `{"protocol_version":"2.0"}` params; assert the host replies `HandshakeFailed` (preferred) or responds with `"2.1"` and the extension is expected to hang up. Add an explicit protocol mismatch check in the handshake handler to return `ErrCodeHandshakeFailed`.
- [ ] Commit: `test(e2e): tiered trust + protocol version scenarios`

---

## Task 46: Rewrite docs/extensions.md

**Files:** `docs/extensions.md`.

- [ ] Sections: intro (three modes with trust tiers table), Quick Start (hosted TS), Quick Start (hosted Go), Discovery paths, `settings.json` additions, Trust & Approvals explanation, link to `docs/superpowers/specs/2026-04-14-extensions-core-sdk-rpc-design.md` for full reference. Drop all declarative-manifest content.
- [ ] Commit: `docs(extensions): rewrite for new ExtensionAPI surface`

---

## Final Verification

- [ ] `go test ./... -count=1` → all pass
- [ ] `cd packages/extension-sdk && npm run build && node --test dist/test/*.test.js` → pass
- [ ] `cd packages/extension-host && npm run build && node --test dist/test/*.test.js` → pass
- [ ] `go build ./...` → clean
- [ ] `go vet ./...` → clean
- [ ] Spec §9 acceptance (all 6 scenarios): compiled-in, hosted-go, hosted-ts, capability denial, tiered trust, protocol downgrade

---

## Self-Review

**Spec coverage:**
- §1 Scope / non-goals → Task 1 (API stubs carry deferred-to-spec tags)
- §2 Architecture → Tasks 1, 9, 15, 21, 28, 39
- §3 API surface → Tasks 2–7, 16–19
- §4 Entrypoints + metadata → Tasks 25, 26, 40, 41, 42
- §5 RPC protocol v2.1 → Tasks 10, 11, 12, 17, 18, 24, 28, 29, 39
- §6 Loader + discovery → Tasks 25, 26, 35, 36, 37
- §7 Tiered trust → Tasks 27, 30, 39, 45
- §8 Packaging → Tasks 15–19, 21–23, 34
- §9 Proof of life → Tasks 40–45
- §10 Testing → Tasks 8, 14, 20, 42–45
- §11 Layout / greenfield → Task 37 deletions + file structure in header

**Known implementer placeholders (intentional, flagged):**
- Task 38 `newPiapiToolAdapter` requires matching the actual ADK `tool.Tool` interface — implementer reads `internal/tools/core_tools.go` (or equivalent) to mirror pattern.
- Task 39 inbound non-handshake delegation to `api.HostedAPIHandler.Handle` spelled as behavior, not literal wire code.

**Type consistency:**
- `piapi.Register = func(pi piapi.API) error` — Tasks 7, 33, 40, 42.
- `piapi.EventHandler = func(evt Event, ctx Context) (EventResult, error)` — Tasks 4, 7, 31, 42.
- `hostproto.ProtocolVersion = "2.1"` — Tasks 11, 24, 39.
- `host.TrustClass` values (`TrustCompiledIn`, `TrustFirstParty`, `TrustThirdParty`) — Tasks 27, 30, 31, 32, 38, 45.
- `host.State` values (`StatePending`, `StateReady`, `StateRunning`, `StateStopped`, `StateErrored`, `StateDenied`) — Tasks 30, 38, 45.

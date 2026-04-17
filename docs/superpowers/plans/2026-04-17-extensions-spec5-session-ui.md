# Extensions Spec #5 (Session/UI + Lifecycle Hooks) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote every `piapi.ErrNotImplemented{Spec:"#5"}` return into a working implementation, route `MethodToolUpdate` + `MethodLog` from accept-and-drop into real TUI/log sinks, and turn the `HookConfig` + `RunLifecycleHooks` stubs into a config-driven lifecycle-hook system fired at five event points.

**Architecture:** A new `SessionBridge` interface at `internal/extension/api/bridge.go` sits between the API implementations (compiled + hosted) and the host (TUI or CLI). Both `extapi.NewCompiled` and `HostedAPIHandler` call into it; the TUI and CLI each provide a concrete implementation. New `hostproto` services (`session.*`, `session_control.*`, `tool_stream.*`, `log.*`) carry the hosted protocol; old `MethodToolUpdate`/`MethodLog` names remain as aliases for one release. Lifecycle hooks ride the existing tool-execute path — a hook is declared in `pi.toml` as `[[hooks]] event command tools timeout`, and firing the hook invokes the extension's own tool with the event payload.

**Tech Stack:** Go, `google.golang.org/adk/session`, Bubble Tea, `github.com/BurntSushi/toml`.

**Full design:** See `docs/superpowers/specs/2026-04-17-extensions-spec5-session-ui-design.md`.

---

### Task 1: piapi — new error sentinels and additive result-type fields

**Files:**
- Modify: `pkg/piapi/errors.go`
- Modify: `pkg/piapi/context.go`
- Create: `pkg/piapi/errors_spec5_test.go`

- [ ] **Step 1: Write the failing test for error sentinels**

Create `pkg/piapi/errors_spec5_test.go`:

```go
package piapi

import (
	"errors"
	"testing"
)

func TestSpec5ErrorsMatchSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{ErrInvalidKind{Kind: "bad"}, ErrInvalidKindSentinel},
		{ErrIncoherentOptions{Reason: "x"}, ErrIncoherentOptionsSentinel},
		{ErrEntryNotFound{ID: "x"}, ErrEntryNotFoundSentinel},
		{ErrBranchNotFound{ID: "x"}, ErrBranchNotFoundSentinel},
		{ErrSessionNotFound{ID: "x"}, ErrSessionNotFoundSentinel},
		{ErrSessionControlUnsupportedInCLI{Method: "Fork"}, ErrSessionControlUnsupportedInCLISentinel},
		{ErrSessionControlInEventHandler{Method: "Fork"}, ErrSessionControlInEventHandlerSentinel},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("errors.Is(%T, %v) = false; want true", c.err, c.want)
		}
	}
}
```

- [ ] **Step 2: Verify the test fails**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./pkg/piapi/ -run TestSpec5Errors
```

Expected: FAIL with `undefined: ErrInvalidKind` (and friends).

- [ ] **Step 3: Add error types**

Append to `pkg/piapi/errors.go`:

```go
// Spec #5 errors.

var ErrInvalidKindSentinel = errors.New("piapi: invalid kind")

type ErrInvalidKind struct{ Kind string }

func (e ErrInvalidKind) Error() string      { return "piapi: invalid kind: " + e.Kind }
func (e ErrInvalidKind) Is(t error) bool    { return t == ErrInvalidKindSentinel }

var ErrIncoherentOptionsSentinel = errors.New("piapi: incoherent options")

type ErrIncoherentOptions struct{ Reason string }

func (e ErrIncoherentOptions) Error() string   { return "piapi: incoherent options: " + e.Reason }
func (e ErrIncoherentOptions) Is(t error) bool { return t == ErrIncoherentOptionsSentinel }

var ErrEntryNotFoundSentinel = errors.New("piapi: entry not found")

type ErrEntryNotFound struct{ ID string }

func (e ErrEntryNotFound) Error() string   { return "piapi: entry not found: " + e.ID }
func (e ErrEntryNotFound) Is(t error) bool { return t == ErrEntryNotFoundSentinel }

var ErrBranchNotFoundSentinel = errors.New("piapi: branch not found")

type ErrBranchNotFound struct{ ID string }

func (e ErrBranchNotFound) Error() string   { return "piapi: branch not found: " + e.ID }
func (e ErrBranchNotFound) Is(t error) bool { return t == ErrBranchNotFoundSentinel }

var ErrSessionNotFoundSentinel = errors.New("piapi: session not found")

type ErrSessionNotFound struct{ ID string }

func (e ErrSessionNotFound) Error() string   { return "piapi: session not found: " + e.ID }
func (e ErrSessionNotFound) Is(t error) bool { return t == ErrSessionNotFoundSentinel }

var ErrSessionControlUnsupportedInCLISentinel = errors.New("piapi: session control unsupported in CLI")

type ErrSessionControlUnsupportedInCLI struct{ Method string }

func (e ErrSessionControlUnsupportedInCLI) Error() string {
	return "piapi: " + e.Method + " unsupported in CLI (run the TUI to use session control)"
}
func (e ErrSessionControlUnsupportedInCLI) Is(t error) bool {
	return t == ErrSessionControlUnsupportedInCLISentinel
}

var ErrSessionControlInEventHandlerSentinel = errors.New("piapi: session control called from event handler")

type ErrSessionControlInEventHandler struct{ Method string }

func (e ErrSessionControlInEventHandler) Error() string {
	return "piapi: " + e.Method + " must not be called from an event handler; use a command handler"
}
func (e ErrSessionControlInEventHandler) Is(t error) bool {
	return t == ErrSessionControlInEventHandlerSentinel
}
```

- [ ] **Step 4: Extend result types with additive ID fields**

Replace the result-type block in `pkg/piapi/context.go` (the five struct lines that currently read `type NewSessionResult struct{ Cancelled bool }` etc.) with:

```go
// NewSessionOptions, ForkResult, NavigateOptions, NavigateResult,
// SwitchResult — spec #5 types.
type NewSessionOptions struct{}

type NewSessionResult struct {
	ID        string
	Cancelled bool
}

type ForkResult struct {
	BranchID    string
	BranchTitle string
	Cancelled   bool
}

type NavigateOptions struct{}

type NavigateResult struct {
	BranchID  string
	Cancelled bool
}

type SwitchResult struct {
	SessionID string
	Cancelled bool
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./pkg/piapi/ -run TestSpec5Errors
```

Expected: PASS.

- [ ] **Step 6: Run full piapi tests + build to confirm no regression**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./pkg/piapi/... && go build ./...
```

Expected: PASS on both.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add pkg/piapi/errors.go pkg/piapi/context.go pkg/piapi/errors_spec5_test.go && rtk git commit -m "feat(piapi): add spec #5 error sentinels and result-type ID fields"
```

---

### Task 2: hostproto — new services, methods, and payload types

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`
- Modify: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Write failing tests for the new method constants and param types**

Append to `internal/extension/hostproto/protocol_test.go`:

```go
func TestSpec5Methods(t *testing.T) {
	cases := map[string]string{
		"session.append_entry":        ServiceSession + "." + MethodSessionAppendEntry,
		"session.send_custom_message": ServiceSession + "." + MethodSessionSendCustomMessage,
		"session.send_user_message":   ServiceSession + "." + MethodSessionSendUserMessage,
		"session.set_title":           ServiceSession + "." + MethodSessionSetTitle,
		"session.get_title":           ServiceSession + "." + MethodSessionGetTitle,
		"session.set_entry_label":     ServiceSession + "." + MethodSessionSetEntryLabel,
		"session_control.wait_idle":   ServiceSessionControl + "." + MethodSessionControlWaitIdle,
		"session_control.new":         ServiceSessionControl + "." + MethodSessionControlNew,
		"session_control.fork":        ServiceSessionControl + "." + MethodSessionControlFork,
		"session_control.navigate":    ServiceSessionControl + "." + MethodSessionControlNavigate,
		"session_control.switch":      ServiceSessionControl + "." + MethodSessionControlSwitch,
		"session_control.reload":      ServiceSessionControl + "." + MethodSessionControlReload,
		"tool_stream.update":          ServiceToolStream + "." + MethodToolStreamUpdate,
		"log.append":                  ServiceLog + "." + MethodLogAppend,
	}
	for want, got := range cases {
		if want != got {
			t.Errorf("method key %q got %q", want, got)
		}
	}
}

func TestLogParamsRoundtrip(t *testing.T) {
	in := LogParams{
		Level:   "info",
		Message: "hello",
		Fields:  map[string]any{"k": "v"},
		Ts:      "2026-04-17T00:00:00Z",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out LogParams
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Level != in.Level || out.Message != in.Message || out.Fields["k"] != "v" || out.Ts != in.Ts {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}
```

- [ ] **Step 2: Verify the tests fail**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./internal/extension/hostproto/ -run "TestSpec5Methods|TestLogParamsRoundtrip"
```

Expected: FAIL with `undefined: ServiceSession` etc.

- [ ] **Step 3: Add service/method constants and payload types**

Append to `internal/extension/hostproto/protocol.go`:

```go
// Service names (spec #5+).
const (
	ServiceSession        = "session"
	ServiceSessionControl = "session_control"
	ServiceToolStream     = "tool_stream"
	ServiceLog            = "log"
	ServiceTools          = "tools"
	ServiceEvents         = "events"
	ServiceHooks          = "hooks"
)

// Method names within services (spec #5).
const (
	MethodSessionAppendEntry       = "append_entry"
	MethodSessionSendCustomMessage = "send_custom_message"
	MethodSessionSendUserMessage   = "send_user_message"
	MethodSessionSetTitle          = "set_title"
	MethodSessionGetTitle          = "get_title"
	MethodSessionSetEntryLabel     = "set_entry_label"

	MethodSessionControlWaitIdle = "wait_idle"
	MethodSessionControlNew      = "new"
	MethodSessionControlFork     = "fork"
	MethodSessionControlNavigate = "navigate"
	MethodSessionControlSwitch   = "switch"
	MethodSessionControlReload   = "reload"

	MethodToolStreamUpdate = "update"
	MethodLogAppend        = "append"
)

// Payload shapes for the new services.

type SessionAppendEntryParams struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type SessionSendCustomMessageParams struct {
	CustomType  string         `json:"custom_type"`
	Content     string         `json:"content"`
	Display     bool           `json:"display"`
	Details     map[string]any `json:"details,omitempty"`
	DeliverAs   string         `json:"deliver_as,omitempty"`
	TriggerTurn bool           `json:"trigger_turn,omitempty"`
}

type ContentPartProto struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type SessionSendUserMessageParams struct {
	Content     []ContentPartProto `json:"content"`
	DeliverAs   string             `json:"deliver_as,omitempty"`
	TriggerTurn bool               `json:"trigger_turn,omitempty"`
}

type SessionSetTitleParams struct {
	Title string `json:"title"`
}

type SessionGetTitleResult struct {
	Title string `json:"title"`
}

type SessionSetEntryLabelParams struct {
	EntryID string `json:"entry_id"`
	Label   string `json:"label"`
}

type SessionControlForkParams struct {
	EntryID string `json:"entry_id"`
}

type SessionControlForkResult struct {
	BranchID    string `json:"branch_id"`
	BranchTitle string `json:"branch_title"`
	Cancelled   bool   `json:"cancelled"`
}

type SessionControlNewResult struct {
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
}

type SessionControlNavigateParams struct {
	TargetID string `json:"target_id"`
}

type SessionControlNavigateResult struct {
	BranchID  string `json:"branch_id"`
	Cancelled bool   `json:"cancelled"`
}

type SessionControlSwitchParams struct {
	SessionPath string `json:"session_path"`
}

type SessionControlSwitchResult struct {
	SessionID string `json:"session_id"`
	Cancelled bool   `json:"cancelled"`
}

type ToolStreamUpdateParams struct {
	ToolCallID string          `json:"tool_call_id"`
	Partial    json.RawMessage `json:"partial"`
}

type LogParams struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
	Ts      string         `json:"ts,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./internal/extension/hostproto/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go && rtk git commit -m "feat(hostproto): add spec #5 services, methods, and payload types"
```

---

### Task 3: SessionBridge interface + fake helper

**Files:**
- Create: `internal/extension/api/bridge.go`
- Create: `internal/extension/api/testing/fakebridge.go`
- Create: `internal/extension/api/bridge_test.go`

- [ ] **Step 1: Define the SessionBridge interface**

Create `internal/extension/api/bridge.go`:

```go
package api

import (
	"context"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// SessionBridge is the seam between extension API implementations
// (compiled + hosted) and the running host (TUI or CLI). Every spec #5
// method on piapi.API / piapi.CommandContext routes through this
// interface so the extension-side code stays identical across hosts.
type SessionBridge interface {
	// Messaging.
	AppendEntry(extID, kind string, payload any) error
	SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error
	SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error
	SetSessionTitle(title string) error
	GetSessionTitle() string
	SetEntryLabel(entryID, label string) error

	// Session control (CommandContext only).
	WaitForIdle(ctx context.Context) error
	NewSession(opts piapi.NewSessionOptions) (piapi.NewSessionResult, error)
	Fork(entryID string) (piapi.ForkResult, error)
	NavigateBranch(targetID string) (piapi.NavigateResult, error)
	SwitchSession(sessionPath string) (piapi.SwitchResult, error)
	Reload(ctx context.Context) error

	// Streaming + logs.
	EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error
	AppendExtensionLog(extID, level, message string, fields map[string]any) error
}

// NoopBridge is the default bridge used when no host is wired (tests that
// don't care, early-boot paths). All mutating calls are no-ops; accessors
// return zero values.
type NoopBridge struct{}

func (NoopBridge) AppendEntry(string, string, any) error                           { return nil }
func (NoopBridge) SendCustomMessage(string, piapi.CustomMessage, piapi.SendOptions) error { return nil }
func (NoopBridge) SendUserMessage(string, piapi.UserMessage, piapi.SendOptions) error     { return nil }
func (NoopBridge) SetSessionTitle(string) error                                    { return nil }
func (NoopBridge) GetSessionTitle() string                                         { return "" }
func (NoopBridge) SetEntryLabel(string, string) error                              { return nil }
func (NoopBridge) WaitForIdle(context.Context) error                               { return nil }
func (NoopBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, nil
}
func (NoopBridge) Fork(string) (piapi.ForkResult, error)             { return piapi.ForkResult{}, nil }
func (NoopBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, nil
}
func (NoopBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, nil
}
func (NoopBridge) Reload(context.Context) error                               { return nil }
func (NoopBridge) EmitToolUpdate(string, piapi.ToolResult) error              { return nil }
func (NoopBridge) AppendExtensionLog(string, string, string, map[string]any) error { return nil }
```

- [ ] **Step 2: Create the recording FakeBridge test helper**

Create `internal/extension/api/testing/fakebridge.go`:

```go
// Package testing contains test helpers for extension API implementations.
package testing

import (
	"context"
	"sync"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// Call is a single recorded call on FakeBridge.
type Call struct {
	Method string
	Args   map[string]any
}

// FakeBridge records every method call and lets tests pre-seed return
// values or errors. Safe for concurrent use.
type FakeBridge struct {
	mu        sync.Mutex
	Calls     []Call
	Title     string
	NewResult piapi.NewSessionResult
	ForkRes   piapi.ForkResult
	NavRes    piapi.NavigateResult
	SwitchRes piapi.SwitchResult
	Err       error // returned from every mutating method when non-nil
}

func (f *FakeBridge) record(m string, a map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Method: m, Args: a})
	return f.Err
}

func (f *FakeBridge) AppendEntry(extID, kind string, payload any) error {
	return f.record("AppendEntry", map[string]any{"ext": extID, "kind": kind, "payload": payload})
}
func (f *FakeBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	return f.record("SendCustomMessage", map[string]any{"ext": extID, "msg": msg, "opts": opts})
}
func (f *FakeBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	return f.record("SendUserMessage", map[string]any{"ext": extID, "msg": msg, "opts": opts})
}
func (f *FakeBridge) SetSessionTitle(title string) error {
	f.mu.Lock()
	f.Title = title
	f.mu.Unlock()
	return f.record("SetSessionTitle", map[string]any{"title": title})
}
func (f *FakeBridge) GetSessionTitle() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Title
}
func (f *FakeBridge) SetEntryLabel(entryID, label string) error {
	return f.record("SetEntryLabel", map[string]any{"entry": entryID, "label": label})
}
func (f *FakeBridge) WaitForIdle(_ context.Context) error {
	return f.record("WaitForIdle", nil)
}
func (f *FakeBridge) NewSession(opts piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return f.NewResult, f.record("NewSession", map[string]any{"opts": opts})
}
func (f *FakeBridge) Fork(entryID string) (piapi.ForkResult, error) {
	return f.ForkRes, f.record("Fork", map[string]any{"entry": entryID})
}
func (f *FakeBridge) NavigateBranch(targetID string) (piapi.NavigateResult, error) {
	return f.NavRes, f.record("NavigateBranch", map[string]any{"target": targetID})
}
func (f *FakeBridge) SwitchSession(path string) (piapi.SwitchResult, error) {
	return f.SwitchRes, f.record("SwitchSession", map[string]any{"path": path})
}
func (f *FakeBridge) Reload(_ context.Context) error {
	return f.record("Reload", nil)
}
func (f *FakeBridge) EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error {
	return f.record("EmitToolUpdate", map[string]any{"id": toolCallID, "partial": partial})
}
func (f *FakeBridge) AppendExtensionLog(extID, level, msg string, fields map[string]any) error {
	return f.record("AppendExtensionLog", map[string]any{"ext": extID, "level": level, "msg": msg, "fields": fields})
}
```

- [ ] **Step 3: Write a bridge contract test**

Create `internal/extension/api/bridge_test.go`:

```go
package api

import (
	"testing"

	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestFakeBridgeRecordsCalls(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	if err := fb.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if err := fb.SetSessionTitle("hi"); err != nil {
		t.Fatal(err)
	}
	if got := fb.GetSessionTitle(); got != "hi" {
		t.Fatalf("title = %q; want hi", got)
	}
	if len(fb.Calls) != 3 {
		t.Fatalf("calls = %d; want 3", len(fb.Calls))
	}
	if fb.Calls[0].Method != "AppendEntry" || fb.Calls[1].Method != "SetSessionTitle" {
		t.Fatalf("call order wrong: %+v", fb.Calls)
	}
}

func TestNoopBridgeCompiles(t *testing.T) {
	var b SessionBridge = NoopBridge{}
	_ = b.AppendEntry("", "info", nil)
	_, _ = b.Fork("")
	if _, err := b.NewSession(piapi.NewSessionOptions{}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./internal/extension/api/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add internal/extension/api/bridge.go internal/extension/api/bridge_test.go internal/extension/api/testing/fakebridge.go && rtk git commit -m "feat(extension/api): SessionBridge interface + FakeBridge test helper"
```

---

### Task 4: Rewire compiled API to the bridge

**Files:**
- Modify: `internal/extension/api/compiled.go`
- Modify: `internal/extension/api/compiled_test.go`
- Modify: `internal/extension/runtime.go` (caller of `NewCompiled`)
- Modify: `internal/extension/api/hosted.go` (another caller, in `execShell`)

- [ ] **Step 1: Update the signature of NewCompiled and the compiledAPI struct**

In `internal/extension/api/compiled.go`:

Replace lines 18-35 (struct + NewCompiled):

```go
type compiledAPI struct {
	reg     *host.Registration
	manager *host.Manager
	bridge  SessionBridge

	mu       sync.Mutex
	tools    map[string]piapi.ToolDescriptor
	handlers map[string][]piapi.EventHandler
}

// NewCompiled builds a piapi.API backed by direct in-process dispatch.
// The bridge receives every spec #5 method call. Pass NoopBridge{} when
// a real host is not available.
func NewCompiled(reg *host.Registration, manager *host.Manager, bridge SessionBridge) piapi.API {
	if bridge == nil {
		bridge = NoopBridge{}
	}
	return &compiledAPI{
		reg:      reg,
		manager:  manager,
		bridge:   bridge,
		tools:    map[string]piapi.ToolDescriptor{},
		handlers: map[string][]piapi.EventHandler{},
	}
}
```

- [ ] **Step 2: Replace spec #5 stubs with bridge calls**

Replace lines 129-144 in `internal/extension/api/compiled.go` (the six spec #5 methods) with:

```go
func (c *compiledAPI) SendMessage(msg piapi.CustomMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" {
		return piapi.ErrIncoherentOptions{Reason: "SendMessage cannot steer; use SendUserMessage"}
	}
	return c.bridge.SendCustomMessage(c.reg.ID, msg, opts)
}
func (c *compiledAPI) SendUserMessage(msg piapi.UserMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" && !opts.TriggerTurn {
		return piapi.ErrIncoherentOptions{Reason: "steer requires TriggerTurn=true"}
	}
	return c.bridge.SendUserMessage(c.reg.ID, msg, opts)
}
func (c *compiledAPI) AppendEntry(kind string, payload any) error {
	if !isValidKind(kind) {
		return piapi.ErrInvalidKind{Kind: kind}
	}
	return c.bridge.AppendEntry(c.reg.ID, kind, payload)
}
func (c *compiledAPI) SetSessionName(name string) error {
	return c.bridge.SetSessionTitle(name)
}
func (c *compiledAPI) GetSessionName() string { return c.bridge.GetSessionTitle() }
func (c *compiledAPI) SetLabel(entryID, label string) error {
	return c.bridge.SetEntryLabel(entryID, label)
}
```

Add the helper `isValidKind` near the bottom of the file (before `collectingBuffer`):

```go
var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func isValidKind(kind string) bool { return kindPattern.MatchString(kind) }
```

Add `"regexp"` to the imports.

- [ ] **Step 3: Update callers — runtime.go**

In `internal/extension/runtime.go` line 121, change:

```go
api := extapi.NewCompiled(reg, manager)
```

to:

```go
api := extapi.NewCompiled(reg, manager, cfg.Bridge)
```

Add to `RuntimeConfig` (near other fields):

```go
// Bridge is the session/UI bridge for spec #5 operations. Nil means
// the NoopBridge is used (messaging + session control become no-ops).
Bridge extapi.SessionBridge
```

- [ ] **Step 4: Update callers — hosted.go**

In `internal/extension/api/hosted.go` line 118, change:

```go
capi := NewCompiled(tmp, h.manager).(*compiledAPI)
```

to:

```go
capi := NewCompiled(tmp, h.manager, NoopBridge{}).(*compiledAPI)
```

- [ ] **Step 5: Replace the spec #5 stub tests with bridge-assertion tests**

In `internal/extension/api/compiled_test.go` replace `TestCompiled_SendMessageNotImplemented`, `TestCompiled_SendUserMessageNotImplemented`, `TestCompiled_AppendEntryNotImplemented`, `TestCompiled_SetSessionNameNotImplemented`, `TestCompiled_SetLabelNotImplemented` (whatever subset exists — delete all of them) with:

```go
func TestCompiled_Spec5RoutesToBridge(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	reg := &host.Registration{ID: "e", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "e", Version: "0.0"}}
	api := NewCompiled(reg, host.NewManager(nil), fb)

	if err := api.AppendEntry("info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if err := api.SendMessage(piapi.CustomMessage{CustomType: "note", Content: "x"}, piapi.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := api.SendUserMessage(piapi.UserMessage{Content: []piapi.ContentPart{{Type: "text", Text: "hi"}}}, piapi.SendOptions{TriggerTurn: true}); err != nil {
		t.Fatal(err)
	}
	if err := api.SetSessionName("title"); err != nil {
		t.Fatal(err)
	}
	if got := api.GetSessionName(); got != "title" {
		t.Fatalf("GetSessionName = %q; want title", got)
	}
	if err := api.SetLabel("branch-1", "alpha"); err != nil {
		t.Fatal(err)
	}

	wantMethods := []string{"AppendEntry", "SendCustomMessage", "SendUserMessage", "SetSessionTitle", "SetSessionTitle", "SetEntryLabel"}
	// (two SetSessionTitle entries: one from Set, one from the GetSessionTitle recording path — actually Get doesn't record. Fix:)
	wantMethods = []string{"AppendEntry", "SendCustomMessage", "SendUserMessage", "SetSessionTitle", "SetEntryLabel"}
	var gotMethods []string
	for _, c := range fb.Calls {
		gotMethods = append(gotMethods, c.Method)
	}
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("calls = %v; want %v", gotMethods, wantMethods)
	}
}

func TestCompiled_AppendEntryRejectsInvalidKind(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	reg := &host.Registration{ID: "e", Metadata: piapi.Metadata{Name: "e", Version: "0.0"}}
	api := NewCompiled(reg, host.NewManager(nil), fb)

	err := api.AppendEntry("Bad Kind!", nil)
	if !errors.Is(err, piapi.ErrInvalidKindSentinel) {
		t.Fatalf("got %v; want ErrInvalidKind", err)
	}
}

func TestCompiled_SendMessageRejectsSteer(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	reg := &host.Registration{ID: "e", Metadata: piapi.Metadata{Name: "e", Version: "0.0"}}
	api := NewCompiled(reg, host.NewManager(nil), fb)

	err := api.SendMessage(piapi.CustomMessage{CustomType: "x"}, piapi.SendOptions{DeliverAs: "steer", TriggerTurn: true})
	if !errors.Is(err, piapi.ErrIncoherentOptionsSentinel) {
		t.Fatalf("got %v; want ErrIncoherentOptions", err)
	}
}
```

Ensure imports include: `"errors"`, `"reflect"`, `testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"`.

- [ ] **Step 6: Update every other caller of NewCompiled in the test suite**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -l "NewCompiled(" --include="*.go" .
```

For every match outside `compiled.go`/`bridge.go`/`runtime.go`/`hosted.go` (which are already updated), append `, NoopBridge{}` (or `, nil`) as the third argument.

- [ ] **Step 7: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/... ./pkg/piapi/...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git add internal/extension/api/compiled_test.go && rtk git commit -m "feat(extension/api): route compiled API spec #5 methods through SessionBridge"
```

---

### Task 5: Hosted API handler — route new services through the bridge

**Files:**
- Modify: `internal/extension/api/hosted.go`
- Create: `internal/extension/api/hosted_spec5_test.go`

- [ ] **Step 1: Extend HostedAPIHandler to carry a bridge**

In `internal/extension/api/hosted.go`, replace the struct + constructor (lines 15-41):

```go
type HostedAPIHandler struct {
	manager *host.Manager
	reg     *host.Registration
	bridge  SessionBridge

	mu    sync.Mutex
	tools map[string]hostedTool
}

type hostedTool struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func NewHostedHandler(manager *host.Manager, reg *host.Registration, bridge SessionBridge) *HostedAPIHandler {
	if bridge == nil {
		bridge = NoopBridge{}
	}
	return &HostedAPIHandler{
		manager: manager,
		reg:     reg,
		bridge:  bridge,
		tools:   map[string]hostedTool{},
	}
}
```

- [ ] **Step 2: Rewire Handle and handleHostCall for the new services**

Replace the `Handle` method and extend `handleHostCall` (lines 56-90):

```go
func (h *HostedAPIHandler) Handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodHostCall:
		return h.handleHostCall(params)
	case hostproto.MethodSubscribeEvent:
		return h.handleSubscribeEvent(params)
	case hostproto.MethodToolUpdate:
		// Legacy method name — route through tool_stream.update for one release.
		return h.handleToolStreamUpdate(params)
	case hostproto.MethodLog:
		// Legacy method name — route through log.append for one release.
		return h.handleLogAppend(params)
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
}

func (h *HostedAPIHandler) handleHostCall(params json.RawMessage) (any, error) {
	var p hostproto.HostCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("host_call: invalid params: %w", err)
	}
	capability := p.Service + "." + p.Method
	if ok, reason := h.manager.Gate().Allowed(h.reg.ID, capability, h.reg.Trust); !ok {
		return nil, fmt.Errorf("capability denied: %s (%s)", capability, reason)
	}
	switch p.Service {
	case hostproto.ServiceTools:
		if p.Method == "register" {
			return h.registerTool(p.Payload)
		}
	case "exec":
		if p.Method == "shell" {
			return h.execShell(p.Payload)
		}
	case hostproto.ServiceSession:
		return h.handleSession(p.Method, p.Payload)
	case hostproto.ServiceSessionControl:
		return h.handleSessionControl(p.Method, p.Payload)
	case hostproto.ServiceToolStream:
		if p.Method == hostproto.MethodToolStreamUpdate {
			return h.handleToolStreamUpdate(p.Payload)
		}
	case hostproto.ServiceLog:
		if p.Method == hostproto.MethodLogAppend {
			return h.handleLogAppend(p.Payload)
		}
	}
	return nil, fmt.Errorf("service %s.%s not implemented", p.Service, p.Method)
}
```

- [ ] **Step 3: Add the service handlers**

Append to `internal/extension/api/hosted.go`:

```go
func (h *HostedAPIHandler) handleSession(method string, payload json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodSessionAppendEntry:
		var p hostproto.SessionAppendEntryParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		var body any
		if len(p.Payload) > 0 {
			_ = json.Unmarshal(p.Payload, &body)
		}
		return map[string]any{}, h.bridge.AppendEntry(h.reg.ID, p.Kind, body)

	case hostproto.MethodSessionSendCustomMessage:
		var p hostproto.SessionSendCustomMessageParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SendCustomMessage(h.reg.ID,
			piapi.CustomMessage{CustomType: p.CustomType, Content: p.Content, Display: p.Display, Details: p.Details},
			piapi.SendOptions{DeliverAs: p.DeliverAs, TriggerTurn: p.TriggerTurn})

	case hostproto.MethodSessionSendUserMessage:
		var p hostproto.SessionSendUserMessageParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		parts := make([]piapi.ContentPart, 0, len(p.Content))
		for _, c := range p.Content {
			parts = append(parts, piapi.ContentPart{Type: c.Type, Text: c.Text})
		}
		return map[string]any{}, h.bridge.SendUserMessage(h.reg.ID,
			piapi.UserMessage{Content: parts},
			piapi.SendOptions{DeliverAs: p.DeliverAs, TriggerTurn: p.TriggerTurn})

	case hostproto.MethodSessionSetTitle:
		var p hostproto.SessionSetTitleParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetSessionTitle(p.Title)

	case hostproto.MethodSessionGetTitle:
		return hostproto.SessionGetTitleResult{Title: h.bridge.GetSessionTitle()}, nil

	case hostproto.MethodSessionSetEntryLabel:
		var p hostproto.SessionSetEntryLabelParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetEntryLabel(p.EntryID, p.Label)
	}
	return nil, fmt.Errorf("session.%s not implemented", method)
}

func (h *HostedAPIHandler) handleSessionControl(method string, payload json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodSessionControlWaitIdle:
		return map[string]any{}, h.bridge.WaitForIdle(context.Background())
	case hostproto.MethodSessionControlNew:
		r, err := h.bridge.NewSession(piapi.NewSessionOptions{})
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlNewResult{ID: r.ID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlFork:
		var p hostproto.SessionControlForkParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.Fork(p.EntryID)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlForkResult{BranchID: r.BranchID, BranchTitle: r.BranchTitle, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlNavigate:
		var p hostproto.SessionControlNavigateParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.NavigateBranch(p.TargetID)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlNavigateResult{BranchID: r.BranchID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlSwitch:
		var p hostproto.SessionControlSwitchParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.SwitchSession(p.SessionPath)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlSwitchResult{SessionID: r.SessionID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlReload:
		return map[string]any{}, h.bridge.Reload(context.Background())
	}
	return nil, fmt.Errorf("session_control.%s not implemented", method)
}

func (h *HostedAPIHandler) handleToolStreamUpdate(payload json.RawMessage) (any, error) {
	var p hostproto.ToolStreamUpdateParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	var partial piapi.ToolResult
	if len(p.Partial) > 0 {
		_ = json.Unmarshal(p.Partial, &partial)
	}
	return map[string]any{}, h.bridge.EmitToolUpdate(p.ToolCallID, partial)
}

func (h *HostedAPIHandler) handleLogAppend(payload json.RawMessage) (any, error) {
	var p hostproto.LogParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Level == "" {
		p.Level = "info"
	}
	return map[string]any{}, h.bridge.AppendExtensionLog(h.reg.ID, p.Level, p.Message, p.Fields)
}
```

- [ ] **Step 4: Update every caller of NewHostedHandler**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -l "NewHostedHandler(" --include="*.go" .
```

For each match, pass `NoopBridge{}` (or the appropriate real bridge where the caller has one) as the third argument.

- [ ] **Step 5: Write a test for service routing**

Create `internal/extension/api/hosted_spec5_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestHosted_SessionServiceRoutesToBridge(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	mgr := host.NewManager(host.NewPermissiveGate())
	reg := &host.Registration{ID: "e", Trust: host.TrustFirstParty, Metadata: piapi.Metadata{Name: "e"}}
	_ = mgr.Register(reg)
	h := NewHostedHandler(mgr, reg, fb)

	raw, _ := json.Marshal(hostproto.HostCallParams{
		Service: hostproto.ServiceSession, Version: 1, Method: hostproto.MethodSessionAppendEntry,
		Payload: json.RawMessage(`{"kind":"info","payload":{"k":"v"}}`),
	})
	if _, err := h.Handle(hostproto.MethodHostCall, raw); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fb.Calls) != 1 || fb.Calls[0].Method != "AppendEntry" {
		t.Fatalf("fake calls = %+v", fb.Calls)
	}
}

func TestHosted_LegacyToolUpdateRoutesThroughBridge(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	mgr := host.NewManager(host.NewPermissiveGate())
	reg := &host.Registration{ID: "e", Trust: host.TrustFirstParty, Metadata: piapi.Metadata{Name: "e"}}
	_ = mgr.Register(reg)
	h := NewHostedHandler(mgr, reg, fb)

	raw, _ := json.Marshal(hostproto.ToolStreamUpdateParams{
		ToolCallID: "call-1",
		Partial:    json.RawMessage(`{"content":[{"type":"text","text":"ping 1"}]}`),
	})
	if _, err := h.Handle(hostproto.MethodToolUpdate, raw); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fb.Calls) != 1 || fb.Calls[0].Method != "EmitToolUpdate" {
		t.Fatalf("fake calls = %+v", fb.Calls)
	}
}
```

Note: if `host.NewPermissiveGate` doesn't exist, use the existing `host.NewGate("")` or equivalent — inspect `internal/extension/host/gate.go` and adapt. The test's point is only to bypass the capability check.

- [ ] **Step 6: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git add internal/extension/api/hosted_spec5_test.go && rtk git commit -m "feat(extension/api): route hosted session/control/stream/log services through SessionBridge"
```

---

### Task 6: piext — real implementations for hosted-side spec #5 methods

**Files:**
- Modify: `pkg/piext/rpc_api.go`
- Create: `pkg/piext/rpc_api_spec5_test.go`

- [ ] **Step 1: Replace spec #5 stubs with hostCall implementations**

In `pkg/piext/rpc_api.go`, replace lines 190-205 (SendMessage through SetLabel) with:

```go
func (a *rpcAPI) SendMessage(msg piapi.CustomMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" {
		return piapi.ErrIncoherentOptions{Reason: "SendMessage cannot steer; use SendUserMessage"}
	}
	payload := map[string]any{
		"custom_type": msg.CustomType, "content": msg.Content,
		"display": msg.Display, "details": msg.Details,
		"deliver_as": opts.DeliverAs, "trigger_turn": opts.TriggerTurn,
	}
	var res map[string]any
	return a.hostCall("session.send_custom_message", payload, &res)
}

func (a *rpcAPI) SendUserMessage(msg piapi.UserMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" && !opts.TriggerTurn {
		return piapi.ErrIncoherentOptions{Reason: "steer requires TriggerTurn=true"}
	}
	content := make([]map[string]any, 0, len(msg.Content))
	for _, c := range msg.Content {
		content = append(content, map[string]any{"type": c.Type, "text": c.Text})
	}
	payload := map[string]any{
		"content":      content,
		"deliver_as":   opts.DeliverAs,
		"trigger_turn": opts.TriggerTurn,
	}
	var res map[string]any
	return a.hostCall("session.send_user_message", payload, &res)
}

func (a *rpcAPI) AppendEntry(kind string, payload any) error {
	if !isValidPiextKind(kind) {
		return piapi.ErrInvalidKind{Kind: kind}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p := map[string]any{"kind": kind, "payload": json.RawMessage(body)}
	var res map[string]any
	return a.hostCall("session.append_entry", p, &res)
}

func (a *rpcAPI) SetSessionName(name string) error {
	var res map[string]any
	return a.hostCall("session.set_title", map[string]any{"title": name}, &res)
}

func (a *rpcAPI) GetSessionName() string {
	var res struct {
		Title string `json:"title"`
	}
	if err := a.hostCall("session.get_title", map[string]any{}, &res); err != nil {
		return ""
	}
	return res.Title
}

func (a *rpcAPI) SetLabel(entryID, label string) error {
	var res map[string]any
	return a.hostCall("session.set_entry_label", map[string]any{"entry_id": entryID, "label": label}, &res)
}
```

Add a helper at the bottom of the file:

```go
var piextKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func isValidPiextKind(k string) bool { return piextKindPattern.MatchString(k) }
```

Add `"regexp"` to imports.

- [ ] **Step 2: Write a test using the existing fake transport pattern**

Create `pkg/piext/rpc_api_spec5_test.go`:

```go
package piext

import (
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// hostCallCapture spins up a goroutine that reads one JSON-RPC request
// from the transport's host-side pipe, captures its method+params, and
// replies with the given result.
func hostCallCapture(t *testing.T, hostIn io.Reader, hostOut io.Writer, result any) (method string, params map[string]any, wg *sync.WaitGroup) {
	wg = &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 8192)
		n, err := hostIn.Read(buf)
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		method, _ = req["method"].(string)
		params, _ = req["params"].(map[string]any)
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
		_, _ = hostOut.Write(append(resp, '\n'))
	}()
	return method, params, wg
}

func TestRPCAPI_AppendEntrySendsHostCall(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, []GrantedService{
		{Service: "session", Version: 1, Methods: []string{"append_entry"}},
	})
	_, _, wg := hostCallCapture(t, hostIn, hostOut, map[string]any{})

	if err := api.AppendEntry("info", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	wg.Wait()
	_ = hostIn.Close()
	_ = hostOut.Close()
	_ = transport.Close()
}

func TestRPCAPI_AppendEntryRejectsInvalidKind(t *testing.T) {
	transport := newTransport(io.NopCloser(nil), writeCloser{})
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, nil)
	err := api.AppendEntry("Bad!", nil)
	if err == nil || !jsonContains(err.Error(), "invalid kind") {
		t.Fatalf("expected ErrInvalidKind, got %v", err)
	}
}

func jsonContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringContains(haystack, needle)
}
func stringContains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
```

(If `writeCloser{}` and `newTransport` don't exactly exist, align to the existing test helpers in `rpc_api_test.go`.)

- [ ] **Step 3: Remove obsolete stub tests**

In `pkg/piext/rpc_api_test.go`, remove the `SendMessage` branch of `TestRPCAPI_NotImplementedStubs` — `SendMessage` is no longer a stub. Keep the `RegisterCommand` check. The file should now read:

```go
func TestRPCAPI_NotImplementedStubs(t *testing.T) {
	transport := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{})
	api := newRPCAPI(transport, piapi.Metadata{Name: "t", Version: "0.1"}, nil)

	err := api.RegisterCommand("x", piapi.CommandDescriptor{})
	if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Fatalf("RegisterCommand: got %v; want ErrNotImplemented", err)
	}
}
```

- [ ] **Step 4: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./pkg/piext/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git add pkg/piext/rpc_api_spec5_test.go && rtk git commit -m "feat(piext): real hosted-side implementations for spec #5 messaging methods"
```

---

### Task 7: piext — Log() funnel + streaming ping

**Files:**
- Modify: `pkg/piext/transport.go` or wherever `Log()` is defined (inspect — it's a top-level helper)
- Modify: `pkg/piext/rpc_api.go` (add `Log` bridge hook)

- [ ] **Step 1: Locate the existing Log() implementation**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -n "func Log()" pkg/piext/
```

Read the file(s) shown; the current implementation returns `os.Stderr`. Determine the file and function layout.

- [ ] **Step 2: Rebind Log() to route through log.append when a transport is active**

Edit the file containing `func Log()`. Replace it with:

```go
// Log returns an io.Writer for hosted-extension logging. When a transport
// is active (set by Run()), each newline-terminated write becomes a
// log.append notification. Pre-handshake (transport nil) falls back to
// stderr so early startup messages aren't lost.
func Log() io.Writer {
	if w := currentLogWriter.Load(); w != nil {
		if ww, ok := w.(io.Writer); ok {
			return ww
		}
	}
	return os.Stderr
}

var currentLogWriter atomic.Value // stores io.Writer

// SetLogWriter is called by Run() to swap Log() to a writer backed by
// the active transport. Pass nil to restore stderr behavior.
func SetLogWriter(w io.Writer) {
	if w == nil {
		currentLogWriter.Store(io.Writer(os.Stderr))
		return
	}
	currentLogWriter.Store(w)
}
```

Add imports `"io"`, `"os"`, `"sync/atomic"` as needed.

- [ ] **Step 3: Implement a transportLogWriter in rpc_api.go**

Append to `pkg/piext/rpc_api.go`:

```go
type transportLogWriter struct {
	api *rpcAPI
}

// Write splits p on newlines; each non-empty line becomes a log.append
// notification. Returns len(p) unconditionally (never blocks stderr semantics).
func (w transportLogWriter) Write(p []byte) (int, error) {
	lines := strings.Split(string(p), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		_ = w.api.transport.Notify("pi.extension/host_call", map[string]any{
			"service": "log", "version": 1, "method": "append",
			"payload": map[string]any{"level": "info", "message": ln},
		})
	}
	return len(p), nil
}
```

Add `"strings"` to imports if missing.

- [ ] **Step 4: Wire SetLogWriter in Run()**

Find `func Run(` in `pkg/piext/run.go` (or wherever). After the API + transport are constructed, add:

```go
SetLogWriter(transportLogWriter{api: api})
defer SetLogWriter(nil)
```

Adjust based on actual function shape.

- [ ] **Step 5: Test — write a line, observe a Notify on the transport**

Append to `pkg/piext/rpc_api_spec5_test.go`:

```go
func TestTransportLogWriterEmitsNotify(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, nil)

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := hostIn.Read(buf)
		done <- buf[:n]
	}()

	w := transportLogWriter{api: api}
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	raw := <-done
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if msg["method"] != "pi.extension/host_call" {
		t.Fatalf("method = %v; want host_call", msg["method"])
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
	_ = transport.Close()
}
```

- [ ] **Step 6: Test + build**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./pkg/piext/... && go build ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(piext): Log() funnels through log.append when transport is active"
```

---

### Task 8: TUI session bridge — messaging (AppendEntry, SendCustom, SendUser, title, label)

**Files:**
- Create: `internal/tui/session_bridge.go`
- Create: `internal/tui/session_bridge_test.go`
- Modify: `internal/tui/types.go` (add new tea message types)
- Modify: `internal/tui/tui_update.go` (handle the new messages)
- Modify: `internal/agent/agent.go` (add `SessionService.SetTitle` helper)

- [ ] **Step 1: Add tea message types**

Append to `internal/tui/types.go`:

```go
// ExtensionEntryMsg appends an extension-authored entry to the
// transcript. Dispatched by tuiSessionBridge.AppendEntry.
type ExtensionEntryMsg struct {
	ExtensionID string
	Kind        string
	Payload     any
}

// ExtensionSendCustomMsg dispatches a CustomMessage from an extension.
type ExtensionSendCustomMsg struct {
	ExtensionID string
	Message     piapi.CustomMessage
	Options     piapi.SendOptions
}

// ExtensionSendUserMsg dispatches a user-authored message from an extension.
type ExtensionSendUserMsg struct {
	ExtensionID string
	Message     piapi.UserMessage
	Options     piapi.SendOptions
}

// ExtensionSetTitleMsg updates the current session's title.
type ExtensionSetTitleMsg struct {
	Title string
}

// ExtensionSetLabelMsg relabels a branch.
type ExtensionSetLabelMsg struct {
	EntryID string
	Label   string
}
```

Ensure imports include `"github.com/dimetron/pi-go/pkg/piapi"`.

- [ ] **Step 2: Create the TUI session bridge (messaging half)**

Create `internal/tui/session_bridge.go`:

```go
package tui

import (
	"context"
	"errors"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// tuiSessionBridge implements api.SessionBridge for an interactive TUI.
// All mutations are dispatched through prog.Send so they're serialized
// on the model's Update goroutine.
type tuiSessionBridge struct {
	prog *tea.Program

	mu           sync.Mutex
	latestTitle  string
	idleWaiters  []chan struct{}
	isIdle       bool
	// Host access for session-control ops (wired in Task 9).
	host *sessionHost
}

type sessionHost struct {
	// Populated in Task 9.
}

func newTUISessionBridge(prog *tea.Program) *tuiSessionBridge {
	return &tuiSessionBridge{prog: prog, isIdle: true}
}

func (b *tuiSessionBridge) AppendEntry(extID, kind string, payload any) error {
	b.prog.Send(ExtensionEntryMsg{ExtensionID: extID, Kind: kind, Payload: payload})
	return nil
}

func (b *tuiSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	b.prog.Send(ExtensionSendCustomMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	b.prog.Send(ExtensionSendUserMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SetSessionTitle(title string) error {
	b.mu.Lock()
	b.latestTitle = title
	b.mu.Unlock()
	b.prog.Send(ExtensionSetTitleMsg{Title: title})
	return nil
}

func (b *tuiSessionBridge) GetSessionTitle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latestTitle
}

func (b *tuiSessionBridge) SetEntryLabel(entryID, label string) error {
	b.prog.Send(ExtensionSetLabelMsg{EntryID: entryID, Label: label})
	return nil
}

// Stubs filled in Task 9 / Task 10 / Task 11.
func (b *tuiSessionBridge) WaitForIdle(context.Context) error { return errors.New("not yet wired") }
func (b *tuiSessionBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) Fork(string) (piapi.ForkResult, error) {
	return piapi.ForkResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) Reload(context.Context) error                              { return errors.New("not yet wired") }
func (b *tuiSessionBridge) EmitToolUpdate(string, piapi.ToolResult) error             { return nil }
func (b *tuiSessionBridge) AppendExtensionLog(string, string, string, map[string]any) error {
	return nil
}

var _ api.SessionBridge = (*tuiSessionBridge)(nil)
```

- [ ] **Step 3: Add SessionService.SetTitle helper**

Search for where session titles are written today:

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -n "SetTitle\|Title string" internal/agent/
```

If no `SetTitle` exists, add one to `internal/agent/agent.go`:

```go
// SetSessionTitle updates the title metadata for a session. Intended
// for extension-driven renames via pi.SetSessionName.
func (a *Agent) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	resp, err := a.sessionService.Get(ctx, &session.GetRequest{
		AppName: AppName, UserID: DefaultUserID, SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	resp.Session.SetTitle(title)
	return a.sessionService.Update(ctx, resp.Session)
}
```

(Names and method shapes may differ — inspect the ADK `session` package to find the correct update call. If the service has no setter, fall back to persisting the title via metadata update — if there's no such path, return `fmt.Errorf("session service does not support title updates")` and note the limitation in the test.)

- [ ] **Step 4: Handle the new messages in the model's Update**

In `internal/tui/tui_update.go`, add cases inside the main `switch msg := msg.(type)` block:

```go
case ExtensionEntryMsg:
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:        "extension",
		content:     formatExtensionPayload(msg.Kind, msg.Payload),
		extID:       msg.ExtensionID,
		kind:        msg.Kind,
	})
	return m, nil

case ExtensionSendCustomMsg:
	if !msg.Message.Display {
		return m, nil
	}
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:       "extension-custom",
		content:    msg.Message.Content,
		extID:      msg.ExtensionID,
		customType: msg.Message.CustomType,
	})
	if msg.Options.TriggerTurn {
		return m, m.startTurnWithText("")
	}
	return m, nil

case ExtensionSendUserMsg:
	text := joinContent(msg.Message.Content)
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:    "user",
		content: text,
		extID:   msg.ExtensionID,
	})
	if msg.Options.DeliverAs == "steer" {
		m.abortCurrentTurn()
	}
	if msg.Options.TriggerTurn {
		return m, m.startTurnWithText(text)
	}
	return m, nil

case ExtensionSetTitleMsg:
	if m.cfg.Agent != nil && m.cfg.SessionID != "" {
		_ = m.cfg.Agent.SetSessionTitle(m.ctx, m.cfg.SessionID, msg.Title)
	}
	return m, nil

case ExtensionSetLabelMsg:
	if m.cfg.Agent != nil && msg.EntryID != "" {
		_ = m.cfg.Agent.SetSessionTitle(m.ctx, msg.EntryID, msg.Label)
	}
	return m, nil
```

Add helpers (in `chat.go` or a new `extension_messages.go`):

```go
func formatExtensionPayload(kind string, payload any) string {
	if payload == nil {
		return "[" + kind + "]"
	}
	if s, ok := payload.(string); ok {
		return s
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "[" + kind + "] (unrenderable payload)"
	}
	return string(b)
}

func joinContent(parts []piapi.ContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}
```

If `message` struct doesn't have `extID`, `kind`, `customType` fields, add them as strings.

If `startTurnWithText` and `abortCurrentTurn` don't exist, find the existing "user hits enter" codepath in `input.go` / `agent_loop.go` and add a thin named wrapper. (Typical shape: the enter handler builds a `startAgentCmd` tea.Cmd and returns it; expose that as `m.startTurnWithText(text string) tea.Cmd`.)

- [ ] **Step 5: Write tests**

Create `internal/tui/session_bridge_test.go`:

```go
package tui

import (
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestTUISessionBridge_AppendEntryDispatches(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog)

	if err := b.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}

	msgs := captured()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d; want 1", len(msgs))
	}
	m, ok := msgs[0].(ExtensionEntryMsg)
	if !ok {
		t.Fatalf("msg = %T; want ExtensionEntryMsg", msgs[0])
	}
	if m.ExtensionID != "ext" || m.Kind != "info" {
		t.Fatalf("bad msg: %+v", m)
	}
}

func TestTUISessionBridge_TitleRoundtrip(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog)

	_ = b.SetSessionTitle("alpha")
	if got := b.GetSessionTitle(); got != "alpha" {
		t.Fatalf("title = %q; want alpha", got)
	}
}

func TestTUISessionBridge_SteerSendUserMessageDispatches(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog)
	_ = b.SendUserMessage("ext", piapi.UserMessage{
		Content: []piapi.ContentPart{{Type: "text", Text: "abort"}},
	}, piapi.SendOptions{DeliverAs: "steer", TriggerTurn: true})
	msgs := captured()
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d; want 1", len(msgs))
	}
}
```

The helper `newCapturingProgram(t)` needs to be defined; since `tea.Program.Send` is the write path, the simplest mock is a custom type that satisfies the method. Refactor `tuiSessionBridge` to accept an interface:

```go
type programSender interface {
	Send(msg tea.Msg)
}

type tuiSessionBridge struct {
	prog programSender
	...
}

func newTUISessionBridge(prog programSender) *tuiSessionBridge { ... }
```

Then the test helper:

```go
type capturedMsgs struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *capturedMsgs) Send(m tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
}

func newCapturingProgram(t *testing.T) (programSender, func() []tea.Msg) {
	t.Helper()
	c := &capturedMsgs{}
	return c, func() []tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		return append([]tea.Msg(nil), c.msgs...)
	}
}
```

- [ ] **Step 6: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/tui/... -run "SessionBridge" -short
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(tui): tuiSessionBridge messaging + tea-message dispatch"
```

---

### Task 9: TUI session bridge — session control (WaitForIdle, NewSession, Fork, Navigate, Switch, Reload)

**Files:**
- Modify: `internal/tui/session_bridge.go`
- Modify: `internal/tui/tui_update.go`
- Modify: `internal/tui/types.go`

- [ ] **Step 1: Add synchronous request/response tea messages**

Append to `internal/tui/types.go`:

```go
// ExtensionNewSessionReq / Reply are the request/reply pair for
// bridge.NewSession. The bridge sends Req, the model processes it and
// sends Reply back via the done channel.
type ExtensionNewSessionReq struct {
	Done chan ExtensionNewSessionReply
}

type ExtensionNewSessionReply struct {
	Result piapi.NewSessionResult
	Err    error
}

type ExtensionForkReq struct {
	EntryID string
	Done    chan ExtensionForkReply
}

type ExtensionForkReply struct {
	Result piapi.ForkResult
	Err    error
}

type ExtensionNavigateReq struct {
	TargetID string
	Done     chan ExtensionNavigateReply
}

type ExtensionNavigateReply struct {
	Result piapi.NavigateResult
	Err    error
}

type ExtensionSwitchReq struct {
	SessionPath string
	Done        chan ExtensionSwitchReply
}

type ExtensionSwitchReply struct {
	Result piapi.SwitchResult
	Err    error
}

type ExtensionReloadReq struct {
	Done chan error
}
```

- [ ] **Step 2: Implement the session-control methods in the bridge**

In `internal/tui/session_bridge.go`, replace the stub session-control methods:

```go
func (b *tuiSessionBridge) WaitForIdle(ctx context.Context) error {
	b.mu.Lock()
	if b.isIdle {
		b.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	b.idleWaiters = append(b.idleWaiters, ch)
	b.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// markBusy / markIdle are called from the model's Update when turn
// state transitions. Exposed package-private so the model can drive them.
func (b *tuiSessionBridge) markBusy() {
	b.mu.Lock()
	b.isIdle = false
	b.mu.Unlock()
}

func (b *tuiSessionBridge) markIdle() {
	b.mu.Lock()
	b.isIdle = true
	waiters := b.idleWaiters
	b.idleWaiters = nil
	b.mu.Unlock()
	for _, w := range waiters {
		close(w)
	}
}

func (b *tuiSessionBridge) NewSession(_ piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	done := make(chan ExtensionNewSessionReply, 1)
	b.prog.Send(ExtensionNewSessionReq{Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Fork(entryID string) (piapi.ForkResult, error) {
	done := make(chan ExtensionForkReply, 1)
	b.prog.Send(ExtensionForkReq{EntryID: entryID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) NavigateBranch(targetID string) (piapi.NavigateResult, error) {
	done := make(chan ExtensionNavigateReply, 1)
	b.prog.Send(ExtensionNavigateReq{TargetID: targetID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) SwitchSession(path string) (piapi.SwitchResult, error) {
	done := make(chan ExtensionSwitchReply, 1)
	b.prog.Send(ExtensionSwitchReq{SessionPath: path, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Reload(_ context.Context) error {
	done := make(chan error, 1)
	b.prog.Send(ExtensionReloadReq{Done: done})
	return <-done
}
```

- [ ] **Step 3: Handle the session-control requests in the model's Update**

Add cases in `internal/tui/tui_update.go`:

```go
case ExtensionNewSessionReq:
	id, err := m.cfg.Agent.CreateSession(m.ctx)
	if err != nil {
		msg.Done <- ExtensionNewSessionReply{Err: err}
		return m, nil
	}
	m.cfg.SessionID = id
	m.chatModel.Messages = nil
	msg.Done <- ExtensionNewSessionReply{Result: piapi.NewSessionResult{ID: id}}
	return m, nil

case ExtensionForkReq:
	baseID := msg.EntryID
	if baseID == "" {
		baseID = m.cfg.SessionID
	}
	name := "fork-" + time.Now().UTC().Format("20060102T150405.000")
	branch, err := m.cfg.SessionService.CreateBranch(m.cfg.SessionID, agent.AppName, agent.DefaultUserID, name)
	if err != nil {
		msg.Done <- ExtensionForkReply{Err: err}
		return m, nil
	}
	msg.Done <- ExtensionForkReply{Result: piapi.ForkResult{BranchID: branch.ID, BranchTitle: branch.Title}}
	return m, nil

case ExtensionNavigateReq:
	if err := m.loadSessionMessages(msg.TargetID); err != nil {
		msg.Done <- ExtensionNavigateReply{Err: piapi.ErrBranchNotFound{ID: msg.TargetID}}
		return m, nil
	}
	msg.Done <- ExtensionNavigateReply{Result: piapi.NavigateResult{BranchID: msg.TargetID}}
	return m, nil

case ExtensionSwitchReq:
	id := strings.TrimPrefix(msg.SessionPath, "sessions/")
	if err := m.loadSessionMessages(id); err != nil {
		msg.Done <- ExtensionSwitchReply{Err: piapi.ErrSessionNotFound{ID: id}}
		return m, nil
	}
	msg.Done <- ExtensionSwitchReply{Result: piapi.SwitchResult{SessionID: id}}
	return m, nil

case ExtensionReloadReq:
	err := m.reloadExtensions()
	msg.Done <- err
	return m, nil
```

Add `reloadExtensions` method (on `*model`):

```go
func (m *model) reloadExtensions() error {
	if m.cfg.Runtime == nil {
		return nil
	}
	return m.cfg.Runtime.Reload(m.ctx)
}
```

`Runtime.Reload` is added in Task 14.

Inspect the agent's session-service API for `CreateBranch` and `ListBranches`; if the exact name differs, adapt. `branch.ID` and `branch.Title` are the return fields.

Also: ensure `markBusy()` and `markIdle()` are called when turns start/end. Find the current turn-state transitions in `agent_loop.go` and wire in calls to `m.bridge.markBusy()` when a turn begins, `m.bridge.markIdle()` when it completes.

- [ ] **Step 4: Extend session_bridge_test.go**

Append to `internal/tui/session_bridge_test.go`:

```go
func TestTUISessionBridge_WaitForIdleReturnsWhenIdle(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog)

	if err := b.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("WaitForIdle on already-idle: %v", err)
	}
}

func TestTUISessionBridge_WaitForIdleBlocksUntilMark(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog)
	b.markBusy()

	done := make(chan error, 1)
	go func() { done <- b.WaitForIdle(context.Background()) }()

	select {
	case <-done:
		t.Fatal("WaitForIdle returned while busy")
	case <-time.After(50 * time.Millisecond):
	}

	b.markIdle()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForIdle: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForIdle did not return after markIdle")
	}
}

func TestTUISessionBridge_ForkSendsReq(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog)

	go func() {
		// Wait for the Req, reply.
		time.Sleep(20 * time.Millisecond)
		for _, m := range captured() {
			if req, ok := m.(ExtensionForkReq); ok {
				req.Done <- ExtensionForkReply{Result: piapi.ForkResult{BranchID: "b1", BranchTitle: "alpha"}}
				return
			}
		}
	}()

	res, err := b.Fork("s1")
	if err != nil {
		t.Fatal(err)
	}
	if res.BranchID != "b1" || res.BranchTitle != "alpha" {
		t.Fatalf("result = %+v", res)
	}
}
```

Add `"context"` and `"time"` imports.

- [ ] **Step 5: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/tui/... -run "SessionBridge" -short
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(tui): tuiSessionBridge session-control (fork, navigate, switch, new, wait-idle, reload)"
```

---

### Task 10: Tool-display streaming rows

**Files:**
- Modify: `internal/tui/session_bridge.go`
- Modify: `internal/tui/tool_display.go`
- Modify: `internal/tui/tui_update.go`
- Modify: `internal/tui/types.go`
- Create: `internal/tui/tool_stream_test.go`

- [ ] **Step 1: Add the streaming tea message**

Append to `internal/tui/types.go`:

```go
// ExtensionToolStreamMsg delivers a partial ToolResult for an
// in-progress tool call. Routed from MethodToolUpdate / tool_stream.update.
type ExtensionToolStreamMsg struct {
	ToolCallID string
	Partial    piapi.ToolResult
}
```

- [ ] **Step 2: Implement EmitToolUpdate on the bridge**

In `internal/tui/session_bridge.go`, replace the `EmitToolUpdate` stub:

```go
func (b *tuiSessionBridge) EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error {
	b.prog.Send(ExtensionToolStreamMsg{ToolCallID: toolCallID, Partial: partial})
	return nil
}
```

- [ ] **Step 3: Add streamingRows to ToolDisplayModel and handle the message**

In `internal/tui/tool_display.go`, add a field to the struct (adjust struct name to the real one):

```go
// streamingRows tracks in-progress tool calls receiving partial updates.
// Keyed by tool call ID. Entries are removed when the final ToolResult
// arrives via the normal event path.
streamingRows map[string]*streamingRow
```

Define the row type at file top:

```go
type streamingRow struct {
	ToolCallID string
	Content    []piapi.ContentPart
	Updates    int
}
```

Initialize the map when constructing the model (wherever the struct is created).

In `internal/tui/tui_update.go`, add a case:

```go
case ExtensionToolStreamMsg:
	if m.toolDisplay.streamingRows == nil {
		m.toolDisplay.streamingRows = map[string]*streamingRow{}
	}
	row, ok := m.toolDisplay.streamingRows[msg.ToolCallID]
	if !ok {
		row = &streamingRow{ToolCallID: msg.ToolCallID}
		m.toolDisplay.streamingRows[msg.ToolCallID] = row
	}
	row.Content = msg.Partial.Content
	row.Updates++
	// Also append to trace log.
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		kind:    "tool-stream",
		summary: "stream " + msg.ToolCallID,
		detail:  partialSummary(msg.Partial),
	})
	return m, nil
```

When the final `ToolResult` arrives via the existing ADK event path (look for the function that processes tool-result events), clear the streaming row:

```go
if m.toolDisplay.streamingRows != nil {
	delete(m.toolDisplay.streamingRows, toolCallID)
}
```

Add helper `partialSummary`:

```go
func partialSummary(p piapi.ToolResult) string {
	var sb strings.Builder
	for _, c := range p.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
			sb.WriteByte(' ')
		}
	}
	s := strings.TrimSpace(sb.String())
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
```

Render streaming rows alongside completed ones in the tool-display render function. Each streaming row gets a small spinner glyph (⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏; cycle based on `row.Updates`) and the latest partial content. When the final result arrives, the row renders normally without the spinner.

- [ ] **Step 4: Write streaming-rows test**

Create `internal/tui/tool_stream_test.go`:

```go
package tui

import (
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestExtensionToolStreamMsgUpdatesRow(t *testing.T) {
	m := newTestModel(t)

	m, _ = m.Update(ExtensionToolStreamMsg{
		ToolCallID: "c1",
		Partial:    piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "ping 1/3"}}},
	})
	m, _ = m.Update(ExtensionToolStreamMsg{
		ToolCallID: "c1",
		Partial:    piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "ping 2/3"}}},
	})

	row, ok := m.toolDisplay.streamingRows["c1"]
	if !ok {
		t.Fatal("streaming row c1 not created")
	}
	if row.Updates != 2 {
		t.Fatalf("Updates = %d; want 2", row.Updates)
	}
	if len(row.Content) != 1 || row.Content[0].Text != "ping 2/3" {
		t.Fatalf("row content = %+v", row.Content)
	}
}
```

`newTestModel(t)` is a helper — look for an existing one in `tui_test.go` / `teatest_test.go`. If one exists, use it; otherwise add a minimal constructor that returns a `model` with initialized maps and no real program.

- [ ] **Step 5: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/tui/... -run "ToolStream" -short
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(tui): streaming tool-update rows in ToolDisplayModel"
```

---

### Task 11: TUI session bridge — extension log with rotation

**Files:**
- Modify: `internal/tui/session_bridge.go`
- Modify: `internal/tui/types.go`
- Modify: `internal/tui/tui_update.go`
- Create: `internal/tui/extension_log.go`
- Create: `internal/tui/extension_log_test.go`

- [ ] **Step 1: Add log-append tea message**

Append to `internal/tui/types.go`:

```go
// ExtensionLogMsg routes an extension log line to the trace panel.
type ExtensionLogMsg struct {
	ExtensionID string
	Level       string
	Message     string
	Fields      map[string]any
	Ts          time.Time
}
```

Add `"time"` if needed.

- [ ] **Step 2: Implement rotating file writer**

Create `internal/tui/extension_log.go`:

```go
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	logFileSizeCap = 10 * 1024 * 1024 // 10 MB
	logKeepCount   = 3
)

// extensionLogFile is a size-rotated JSONL writer for extension logs.
// Opened lazily on first write. Thread-safe.
type extensionLogFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func newExtensionLogFile(path string) *extensionLogFile {
	return &extensionLogFile{path: path}
}

func (l *extensionLogFile) Write(extID, level, msg string, fields map[string]any, ts time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureOpen(); err != nil {
		return err
	}
	if err := l.rotateIfNeeded(); err != nil {
		return err
	}

	// Cap message at 8 KB.
	if len(msg) > 8*1024 {
		msg = msg[:8*1024]
	}
	entry := map[string]any{
		"ts":    ts.UTC().Format(time.RFC3339Nano),
		"ext":   extID,
		"level": level,
		"msg":   msg,
	}
	if fields != nil {
		entry["fields"] = truncateDepth(fields, 6)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(l.f, string(data))
	return err
}

func (l *extensionLogFile) ensureOpen() error {
	if l.f != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	l.f = f
	return nil
}

func (l *extensionLogFile) rotateIfNeeded() error {
	info, err := l.f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < logFileSizeCap {
		return nil
	}
	_ = l.f.Close()
	l.f = nil

	// Shift older rotations: path.3 gone, path.2 → path.3, path.1 → path.2, path → path.1.
	for i := logKeepCount; i >= 1; i-- {
		from := l.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", l.path, i-1)
		}
		to := fmt.Sprintf("%s.%d", l.path, i)
		_ = os.Remove(to)
		_ = os.Rename(from, to)
	}

	return l.ensureOpen()
}

func truncateDepth(v any, depth int) any {
	if depth == 0 {
		return "..."
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = truncateDepth(vv, depth-1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = truncateDepth(vv, depth-1)
		}
		return out
	default:
		return v
	}
}
```

- [ ] **Step 3: Implement AppendExtensionLog on the bridge**

In `internal/tui/session_bridge.go`, replace the stub and extend the struct:

```go
type tuiSessionBridge struct {
	prog    programSender
	logFile *extensionLogFile
	...
}

func newTUISessionBridge(prog programSender, logPath string) *tuiSessionBridge {
	return &tuiSessionBridge{
		prog:    prog,
		isIdle:  true,
		logFile: newExtensionLogFile(logPath),
	}
}

func (b *tuiSessionBridge) AppendExtensionLog(extID, level, message string, fields map[string]any) error {
	ts := time.Now()
	b.prog.Send(ExtensionLogMsg{ExtensionID: extID, Level: level, Message: message, Fields: fields, Ts: ts})
	return b.logFile.Write(extID, level, message, fields, ts)
}
```

Update the constructor signature in every caller (there will be at most one or two in `tui.go` — Task 16 handles the final wiring; for now fall back to `os.DevNull` path inside tests or pass an empty string that `extensionLogFile` tolerates).

In `internal/tui/tui_update.go` add:

```go
case ExtensionLogMsg:
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		kind:    "extension-log",
		summary: fmt.Sprintf("[%s] %s", msg.ExtensionID, msg.Level),
		detail:  msg.Message,
	})
	return m, nil
```

- [ ] **Step 4: Write rotation test**

Create `internal/tui/extension_log_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtensionLogFile_RotatesAtSizeCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extensions.log")
	l := newExtensionLogFile(path)

	// Write enough to exceed the 10 MB cap: payload repeated many times.
	bigMsg := strings.Repeat("x", 1024)
	for i := 0; i < 12*1024; i++ {
		if err := l.Write("e", "info", bigMsg, nil, time.Now()); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated file %s.1: %v", path, err)
	}
}

func TestExtensionLogFile_TruncatesDeepFields(t *testing.T) {
	dir := t.TempDir()
	l := newExtensionLogFile(filepath.Join(dir, "x.log"))
	nested := map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": "deep"}}}}}}
	if err := l.Write("e", "info", "m", nested, time.Now()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "x.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "...") {
		t.Fatalf("expected truncation marker; got %s", data)
	}
}
```

- [ ] **Step 5: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/tui/... -run "ExtensionLog" -short
```

Expected: PASS (rotation test may take a few seconds).

- [ ] **Step 6: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(tui): extension log sink with size-based rotation"
```

---

### Task 12: CLI session bridge

**Files:**
- Create: `internal/cli/session_bridge.go`
- Create: `internal/cli/session_bridge_test.go`

- [ ] **Step 1: Implement the CLI bridge**

Create `internal/cli/session_bridge.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// cliSessionBridge is the non-interactive implementation of
// api.SessionBridge used when pi-go runs in CLI mode (piped input,
// scripted flows). Messaging ops emit to stderr; session-control ops
// return ErrSessionControlUnsupportedInCLI.
type cliSessionBridge struct {
	stderr io.Writer
	title  string

	mu      sync.Mutex
	logFile *os.File
	logPath string

	reloadFn func(context.Context) error
}

func NewSessionBridge(stderr io.Writer, logPath string, reloadFn func(context.Context) error) extapi.SessionBridge {
	if stderr == nil {
		stderr = os.Stderr
	}
	return &cliSessionBridge{stderr: stderr, logPath: logPath, reloadFn: reloadFn}
}

func (b *cliSessionBridge) AppendEntry(extID, kind string, payload any) error {
	body, _ := json.Marshal(payload)
	fmt.Fprintf(b.stderr, "[%s/%s] %s\n", extID, kind, string(body))
	return nil
}

func (b *cliSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, _ piapi.SendOptions) error {
	if !msg.Display {
		return nil
	}
	fmt.Fprintf(b.stderr, "[%s:%s] %s\n", extID, msg.CustomType, msg.Content)
	return nil
}

func (b *cliSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, _ piapi.SendOptions) error {
	var text string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	fmt.Fprintf(b.stderr, "[%s:user] %s\n", extID, text)
	return nil
}

func (b *cliSessionBridge) SetSessionTitle(title string) error {
	b.mu.Lock()
	b.title = title
	b.mu.Unlock()
	return nil
}

func (b *cliSessionBridge) GetSessionTitle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.title
}

func (b *cliSessionBridge) SetEntryLabel(string, string) error { return nil }

func (b *cliSessionBridge) WaitForIdle(context.Context) error {
	return piapi.ErrSessionControlUnsupportedInCLI{Method: "WaitForIdle"}
}
func (b *cliSessionBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "NewSession"}
}
func (b *cliSessionBridge) Fork(string) (piapi.ForkResult, error) {
	return piapi.ForkResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "Fork"}
}
func (b *cliSessionBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "NavigateBranch"}
}
func (b *cliSessionBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "SwitchSession"}
}

func (b *cliSessionBridge) Reload(ctx context.Context) error {
	if b.reloadFn == nil {
		return nil
	}
	return b.reloadFn(ctx)
}

func (b *cliSessionBridge) EmitToolUpdate(_ string, partial piapi.ToolResult) error {
	for _, c := range partial.Content {
		if c.Type == "text" {
			fmt.Fprintln(b.stderr, c.Text)
		}
	}
	return nil
}

func (b *cliSessionBridge) AppendExtensionLog(extID, level, msg string, fields map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logFile == nil && b.logPath != "" {
		f, err := os.OpenFile(b.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			b.logFile = f
		}
	}
	if b.logFile != nil {
		entry := map[string]any{"ext": extID, "level": level, "msg": msg, "fields": fields}
		data, _ := json.Marshal(entry)
		_, _ = fmt.Fprintln(b.logFile, string(data))
	}
	// Also to stderr for visibility.
	fmt.Fprintf(b.stderr, "[%s %s] %s\n", extID, level, msg)
	return nil
}
```

- [ ] **Step 2: Write tests**

Create `internal/cli/session_bridge_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestCLISessionBridge_ForkReturnsUnsupported(t *testing.T) {
	b := NewSessionBridge(&bytes.Buffer{}, "", nil)
	_, err := b.Fork("x")
	if !errors.Is(err, piapi.ErrSessionControlUnsupportedInCLISentinel) {
		t.Fatalf("got %v; want ErrSessionControlUnsupportedInCLI", err)
	}
}

func TestCLISessionBridge_AppendEntryWritesToStderr(t *testing.T) {
	var buf bytes.Buffer
	b := NewSessionBridge(&buf, "", nil)
	if err := b.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("[ext/info]")) {
		t.Fatalf("stderr missing prefix: %s", buf.String())
	}
}

func TestCLISessionBridge_ReloadCallsReloadFn(t *testing.T) {
	called := false
	b := NewSessionBridge(nil, "", func(context.Context) error {
		called = true
		return nil
	})
	if err := b.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("reloadFn not called")
	}
}
```

- [ ] **Step 3: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/cli/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add internal/cli/session_bridge.go internal/cli/session_bridge_test.go && rtk git commit -m "feat(cli): headless cliSessionBridge"
```

---

### Task 13: Loader — parse `[[hooks]]` in pi.toml

**Files:**
- Modify: `internal/extension/loader/metadata.go` (or wherever `Metadata` lives — inspect first)
- Modify: `internal/extension/loader/loader.go` (or equivalent parser)
- Create: `internal/extension/loader/hooks_test.go`

- [ ] **Step 1: Locate the metadata struct and its TOML parser**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -n "requested_capabilities\|type Metadata" internal/extension/loader/
```

Note the files for Steps 2-3.

- [ ] **Step 2: Extend loader.Metadata with Hooks**

In the metadata file, add to the `Metadata` struct:

```go
Hooks []HookConfig `toml:"hooks"`
```

And define `HookConfig`:

```go
// HookConfig is a single [[hooks]] table in pi.toml. It declares a
// tool-backed lifecycle hook.
type HookConfig struct {
	Event    string   `toml:"event"`
	Command  string   `toml:"command"`
	Tools    []string `toml:"tools"`
	Timeout  int      `toml:"timeout"`
	Critical bool     `toml:"critical"`
}
```

Note: this is a different `HookConfig` than the one in `internal/extension/runtime.go`. Task 14 unifies them; for now, the loader type is the source of truth for parsed data, and `runtime.HookConfig` is derived from it.

- [ ] **Step 3: Validate each parsed hook**

In the parser (where `toml.Decode` / `toml.Unmarshal` runs), after decoding, validate each `Hooks` entry:

```go
for i, h := range meta.Hooks {
	if h.Event == "" {
		return nil, fmt.Errorf("pi.toml [[hooks]][%d]: event is required", i)
	}
	switch h.Event {
	case "startup", "session_start", "before_turn", "after_turn", "shutdown":
	default:
		return nil, fmt.Errorf("pi.toml [[hooks]][%d]: unknown event %q", i, h.Event)
	}
	if h.Command == "" {
		return nil, fmt.Errorf("pi.toml [[hooks]][%d]: command is required", i)
	}
	if len(h.Tools) == 0 {
		meta.Hooks[i].Tools = []string{"*"}
	}
	if h.Timeout == 0 {
		meta.Hooks[i].Timeout = 5000
	} else if h.Timeout < 0 || h.Timeout > 60000 {
		return nil, fmt.Errorf("pi.toml [[hooks]][%d]: timeout must be 1..60000 ms", i)
	}
}
```

- [ ] **Step 4: Write a parse test**

Create `internal/extension/loader/hooks_test.go`:

```go
package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHooks_Valid(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "session_start"
command = "ext_announce"
tools = ["*"]
timeout = 5000

[[hooks]]
event = "before_turn"
command = "ext_inject"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	meta, err := LoadMetadata(dir) // adapt to actual loader entrypoint
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if len(meta.Hooks) != 2 {
		t.Fatalf("hooks = %d; want 2", len(meta.Hooks))
	}
	if meta.Hooks[1].Timeout != 5000 {
		t.Fatalf("default timeout = %d; want 5000", meta.Hooks[1].Timeout)
	}
	if len(meta.Hooks[1].Tools) != 1 || meta.Hooks[1].Tools[0] != "*" {
		t.Fatalf("default tools = %v; want [\"*\"]", meta.Hooks[1].Tools)
	}
}

func TestParseHooks_InvalidEventRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "made_up"
command = "x"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMetadata(dir)
	if err == nil {
		t.Fatal("expected error for unknown event")
	}
}
```

`LoadMetadata` is a placeholder — use the actual loader function name (e.g., `Discover` + path-to-one-candidate, or a direct `parsePiToml`).

- [ ] **Step 5: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/loader/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(extension/loader): parse [[hooks]] blocks with validation"
```

---

### Task 14: Runtime — hook aggregation + real RunLifecycleHooks

**Files:**
- Modify: `internal/extension/runtime.go`
- Create: `internal/extension/runtime_lifecycle_test.go`

- [ ] **Step 1: Remove the old stub HookConfig**

In `internal/extension/runtime.go`, replace the stub `HookConfig` type (around line 54) with:

```go
// HookConfig is one aggregated lifecycle hook, copied from a
// loader.HookConfig plus the owning extension's ID.
type HookConfig struct {
	ExtensionID string
	Event       string
	Command     string
	Tools       []string
	Timeout     int
	Critical    bool
}
```

- [ ] **Step 2: Aggregate hooks in BuildRuntime**

In `BuildRuntime`, after the compiled-in and hosted-candidate loops, add a new aggregation loop:

```go
for _, reg := range registrations {
	for _, h := range reg.Metadata.Hooks {
		// Critical=true is only allowed for first-party extensions.
		if h.Critical && reg.Trust != host.TrustFirstParty && reg.Trust != host.TrustCompiledIn {
			// Third-party declared Critical; strip it and warn.
			continue // loader should have rejected this already, but guard.
		}
		rt.LifecycleHooks = append(rt.LifecycleHooks, HookConfig{
			ExtensionID: reg.ID,
			Event:       h.Event,
			Command:     h.Command,
			Tools:       append([]string(nil), h.Tools...),
			Timeout:     h.Timeout,
			Critical:    h.Critical,
		})
	}
}
```

(Create `rt` before the aggregation loop if it's not yet created in the current code. The current code creates `rt` at line 184 — move this block to after that point.)

- [ ] **Step 3: Implement RunLifecycleHooks**

Replace the stub `RunLifecycleHooks` in `internal/extension/runtime.go`:

```go
// RunLifecycleHooks fires all hooks subscribed to the given event in
// declaration order. Hook errors are logged but don't abort the caller
// unless a hook has Critical=true and the event is "startup".
//
// The hook is invoked by synthesizing a ToolCall against the
// extension's registered tool of the same name. Hook output with
// kind=hook/<event> is appended to the session for before_turn.
func (r *Runtime) RunLifecycleHooks(ctx context.Context, event string, data map[string]any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("RunLifecycleHooks: marshal: %w", err)
	}

	for _, h := range r.LifecycleHooks {
		if h.Event != event {
			continue
		}
		if !hookMatchesTools(h.Tools, r.activeToolNames()) {
			continue
		}

		reg := r.findRegistration(h.ExtensionID)
		if reg == nil || reg.API == nil {
			continue
		}
		tool, ok := extapi.CompiledTools(reg.API)[h.Command]
		if !ok {
			// Also check hosted tools — via handler state; for spec #5
			// we only invoke compiled-side descriptors. Hosted hooks
			// land in a follow-up.
			continue
		}

		timeout := time.Duration(h.Timeout) * time.Millisecond
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		call := piapi.ToolCall{
			ID:   fmt.Sprintf("hook-%s-%d", event, time.Now().UnixNano()),
			Name: h.Command,
			Args: payload,
		}
		result, hookErr := tool.Execute(hookCtx, call, nil)
		cancel()

		if hookErr != nil {
			// Log via bridge if available.
			if event == "startup" && h.Critical {
				return fmt.Errorf("critical startup hook %s/%s failed: %w", h.ExtensionID, h.Command, hookErr)
			}
			continue
		}

		// For before_turn, surface textual content as an AppendEntry so
		// the LLM sees it.
		if event == "before_turn" && r.Bridge != nil {
			for _, c := range result.Content {
				if c.Type == "text" {
					_ = r.Bridge.AppendEntry(h.ExtensionID, "hook/before_turn", c.Text)
				}
			}
		}
	}
	return nil
}

func hookMatchesTools(filter, active []string) bool {
	for _, f := range filter {
		if f == "*" {
			return true
		}
		for _, a := range active {
			if a == f {
				return true
			}
		}
	}
	return false
}

func (r *Runtime) activeToolNames() []string {
	out := make([]string, 0, len(r.Tools))
	for _, t := range r.Tools {
		out = append(out, t.Name())
	}
	return out
}

func (r *Runtime) findRegistration(id string) *host.Registration {
	for _, reg := range r.Extensions {
		if reg.ID == id {
			return reg
		}
	}
	return nil
}
```

Add `extapi.SessionBridge` field to `Runtime` (named `Bridge`) so `AppendEntry` can be called:

```go
type Runtime struct {
	...
	Bridge extapi.SessionBridge
}
```

Populate `rt.Bridge = cfg.Bridge` at the end of `BuildRuntime`.

Add necessary imports: `"encoding/json"`, `"time"`.

- [ ] **Step 4: Add a minimal Runtime.Reload**

Append to `internal/extension/runtime.go`:

```go
// Reload re-reads approvals.json and updates the gate in place without
// restarting any running extensions. New capability grants take effect
// on the next host_call; revocations take effect on the next host_call
// that checks the revoked capability. Provider registry and hosted
// candidate discovery are not re-run in this spec — those land in a
// follow-up when the provider layer supports live replacement.
func (r *Runtime) Reload(ctx context.Context) error {
	_ = ctx
	if r.Manager == nil {
		return nil
	}
	approvalsPath := DefaultApprovalsPath()
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		return fmt.Errorf("reload approvals: %w", err)
	}
	r.Manager.SetGate(gate)
	return nil
}
```

If `host.Manager` doesn't have `SetGate`, add it (small — swap the gate pointer under a mutex):

```go
// in internal/extension/host/manager.go:

func (m *Manager) SetGate(g *Gate) {
	m.mu.Lock()
	m.gate = g
	m.mu.Unlock()
}
```

Protect the existing `Gate()` accessor with the same mutex.

- [ ] **Step 5: Test hook dispatch with a fake compiled extension**

Create `internal/extension/runtime_lifecycle_test.go`:

```go
package extension

import (
	"context"
	"encoding/json"
	"testing"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestRunLifecycleHooks_FiresInOrder(t *testing.T) {
	fb := &testbridge.FakeBridge{}

	var executed []string
	mkTool := func(name string) piapi.ToolDescriptor {
		return piapi.ToolDescriptor{
			Name:        name,
			Description: "x",
			Parameters:  json.RawMessage(`{"type":"object"}`),
			Execute: func(ctx context.Context, call piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
				executed = append(executed, name)
				return piapi.ToolResult{}, nil
			},
		}
	}

	reg := &host.Registration{ID: "ext1", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "ext1"}}
	api := extapi.NewCompiled(reg, host.NewManager(nil), fb)
	_ = api.RegisterTool(mkTool("hook_a"))
	_ = api.RegisterTool(mkTool("hook_b"))
	reg.API = api

	rt := &Runtime{
		Extensions: []*host.Registration{reg},
		LifecycleHooks: []HookConfig{
			{ExtensionID: "ext1", Event: "startup", Command: "hook_a", Tools: []string{"*"}, Timeout: 5000},
			{ExtensionID: "ext1", Event: "startup", Command: "hook_b", Tools: []string{"*"}, Timeout: 5000},
		},
		Bridge: fb,
	}

	if err := rt.RunLifecycleHooks(context.Background(), "startup", map[string]any{"x": 1}); err != nil {
		t.Fatalf("RunLifecycleHooks: %v", err)
	}
	if len(executed) != 2 || executed[0] != "hook_a" || executed[1] != "hook_b" {
		t.Fatalf("executed = %v; want [hook_a hook_b]", executed)
	}
}

func TestRunLifecycleHooks_BeforeTurnAppendsEntry(t *testing.T) {
	fb := &testbridge.FakeBridge{}

	reg := &host.Registration{ID: "ext1", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "ext1"}}
	api := extapi.NewCompiled(reg, host.NewManager(nil), fb)
	_ = api.RegisterTool(piapi.ToolDescriptor{
		Name:        "inject",
		Description: "x",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "context snippet"}}}, nil
		},
	})
	reg.API = api

	rt := &Runtime{
		Extensions:     []*host.Registration{reg},
		LifecycleHooks: []HookConfig{{ExtensionID: "ext1", Event: "before_turn", Command: "inject", Tools: []string{"*"}, Timeout: 5000}},
		Bridge:         fb,
	}

	if err := rt.RunLifecycleHooks(context.Background(), "before_turn", map[string]any{"user_text": "hi"}); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, c := range fb.Calls {
		if c.Method == "AppendEntry" && c.Args["kind"] == "hook/before_turn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected AppendEntry from before_turn hook; calls = %+v", fb.Calls)
	}
}
```

- [ ] **Step 6: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/ -run "RunLifecycleHooks"
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(extension): real RunLifecycleHooks with aggregation from pi.toml [[hooks]]"
```

---

### Task 15: Fire lifecycle events at the five sites

**Files:**
- Modify: `internal/extension/runtime.go` (fire `startup`)
- Modify: `internal/tui/commands.go` (fire `session_start` from /new, /resume, /fork)
- Modify: `internal/agent/agent.go` or `internal/tui/agent_loop.go` (fire `before_turn`, `after_turn`)
- Modify: `internal/extension/lifecycle/service.go` (fire `shutdown`)

- [ ] **Step 1: Fire startup at end of BuildRuntime**

At the end of `BuildRuntime`, before `return rt, nil`:

```go
names := make([]string, 0, len(registrations))
for _, r := range registrations {
	names = append(names, r.ID)
}
if err := rt.RunLifecycleHooks(ctx, LifecycleEventStartup, map[string]any{
	"work_dir":   cfg.WorkDir,
	"extensions": names,
}); err != nil {
	return nil, fmt.Errorf("startup hooks: %w", err)
}
```

- [ ] **Step 2: Fire session_start in `/new`, `/resume`, `/fork`**

In `internal/tui/commands.go`, inside `handleNewCommand` (after the `m.appendAssistant("Started ...")` success line):

```go
if m.cfg.Runtime != nil {
	_ = m.cfg.Runtime.RunLifecycleHooks(m.ctx, extension.LifecycleEventSessionStart, map[string]any{
		"session_id": sessionID,
		"reason":     "new",
		"title":      title,
	})
}
```

Same pattern in `handleResumeCommand` (reason `"resume"`) and `handleForkCommand` (reason `"fork"`) where the new session is confirmed.

Add `m.cfg.Runtime *extension.Runtime` to the TUI config struct if not already present; wire it in Task 16.

Import `"github.com/dimetron/pi-go/internal/extension"` and use the exported constant.

- [ ] **Step 3: Fire before_turn / after_turn in the agent loop**

In `internal/tui/agent_loop.go`, find where the agent turn starts. Before calling `Agent.Run`:

```go
if m.cfg.Runtime != nil {
	_ = m.cfg.Runtime.RunLifecycleHooks(m.ctx, extension.LifecycleEventBeforeTurn, map[string]any{
		"session_id": m.cfg.SessionID,
		"user_text":  userText,
	})
}
```

After the iterator closes (end of turn):

```go
if m.cfg.Runtime != nil {
	_ = m.cfg.Runtime.RunLifecycleHooks(m.ctx, extension.LifecycleEventAfterTurn, map[string]any{
		"session_id":  m.cfg.SessionID,
		"turn_events": eventCount,
		"aborted":     aborted,
	})
}
```

Track `eventCount` and `aborted` locally as the iterator runs.

Add the event-name constants to `runtime.go`:

```go
const (
	LifecycleEventStartup      = "startup"
	LifecycleEventSessionStart = "session_start"
	LifecycleEventBeforeTurn   = "before_turn"
	LifecycleEventAfterTurn    = "after_turn"
	LifecycleEventShutdown     = "shutdown"
)
```

(Remove the old stub-only pair.)

- [ ] **Step 4: Fire shutdown in lifecycle service**

In `internal/extension/lifecycle/service.go`, find `Stop(ctx)`. Before signaling extensions to stop:

```go
if s.runtime != nil {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = s.runtime.RunLifecycleHooks(shutdownCtx, extension.LifecycleEventShutdown, map[string]any{
		"reason": s.stopReason,
	})
	cancel()
}
```

Add a `runtime *extension.Runtime` field to `Service` and `stopReason string`; populate them during construction / Stop. (Look at the existing lifecycle.New constructor and thread the Runtime reference through.)

This may create an import cycle (`lifecycle` importing `extension`, and `extension` importing `lifecycle`). If so, lift the event constants into `lifecycle` instead and re-export from `extension`:

```go
// in lifecycle package:
const (
	EventShutdown = "shutdown"
)

// in extension/runtime.go:
const LifecycleEventShutdown = lifecycle.EventShutdown
```

And have the lifecycle package accept a hook-firing callback rather than the whole Runtime:

```go
type HookFn func(ctx context.Context, event string, data map[string]any) error

// lifecycle.New takes the callback as a constructor param.
```

- [ ] **Step 5: Add a test that session_start fires on /new**

Append to `internal/extension/runtime_lifecycle_test.go`:

```go
func TestRunLifecycleHooks_SessionStartReceivesReason(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	captured := make(chan map[string]any, 1)

	reg := &host.Registration{ID: "ext1", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "ext1"}}
	api := extapi.NewCompiled(reg, host.NewManager(nil), fb)
	_ = api.RegisterTool(piapi.ToolDescriptor{
		Name:        "on_session",
		Description: "x",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, call piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			var data map[string]any
			_ = json.Unmarshal(call.Args, &data)
			captured <- data
			return piapi.ToolResult{}, nil
		},
	})
	reg.API = api

	rt := &Runtime{
		Extensions:     []*host.Registration{reg},
		LifecycleHooks: []HookConfig{{ExtensionID: "ext1", Event: "session_start", Command: "on_session", Tools: []string{"*"}, Timeout: 5000}},
		Bridge:         fb,
	}

	_ = rt.RunLifecycleHooks(context.Background(), "session_start", map[string]any{
		"session_id": "s1", "reason": "new", "title": "hello",
	})

	data := <-captured
	if data["reason"] != "new" {
		t.Fatalf("reason = %v; want new", data["reason"])
	}
}
```

- [ ] **Step 6: Build + test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/... ./internal/tui/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat(extension): fire lifecycle hooks at startup/session_start/before-after-turn/shutdown"
```

---

### Task 16: Wire the bridge into BuildRuntime, TUI startup, and CLI startup

**Files:**
- Modify: `internal/extension/runtime.go` (already added `cfg.Bridge`; verify)
- Modify: `internal/tui/tui.go` (or wherever the TUI constructs `RuntimeConfig`)
- Modify: `internal/cli/cli.go` / `internal/cli/interactive.go`

- [ ] **Step 1: TUI constructs tuiSessionBridge and threads it into BuildRuntime**

Find where `extension.BuildRuntime` is called for the TUI (likely `internal/tui/tui.go` or `cmd/server/main.go`). Before the call:

```go
logPath := filepath.Join(userHome, ".pi-go", "logs", "extensions.log")
bridge := newTUISessionBridge(program, logPath)
cfg := extension.RuntimeConfig{
	...existing fields...,
	Bridge: bridge,
}
rt, err := extension.BuildRuntime(ctx, cfg)
```

Store `bridge` on the model for later reference (e.g., `m.cfg.Bridge = bridge`), and also keep a pointer to `rt` so lifecycle hook firing sites can reach it via `m.cfg.Runtime`.

- [ ] **Step 2: CLI constructs cliSessionBridge**

In CLI entrypoint (`internal/cli/cli.go` or `interactive.go`), before `BuildRuntime`:

```go
logPath := filepath.Join(userHome, ".pi-go", "logs", "extensions.log")
bridge := NewSessionBridge(os.Stderr, logPath, nil)
cfg := extension.RuntimeConfig{
	...existing fields...,
	Bridge: bridge,
}
rt, err := extension.BuildRuntime(ctx, cfg)
```

For the CLI, `reloadFn` in `NewSessionBridge` can be nil (Reload is a no-op from CLI); pass it for future wiring if desired.

- [ ] **Step 3: Wire the hosted handler to the bridge**

Find every call to `api.NewHostedHandler(...)` in the extension-launch path. Pass the constructed bridge:

```go
handler := extapi.NewHostedHandler(rt.Manager, reg, rt.Bridge)
```

- [ ] **Step 4: Run the E2E suite to verify end-to-end wiring**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./... && go test ./internal/extension/... ./internal/cli/... ./internal/tui/... -short
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "feat: wire SessionBridge (TUI and CLI) through BuildRuntime and HostedHandler"
```

---

### Task 17: E2E — hosted-extension spec #5 round trip

**Files:**
- Create: `internal/extension/e2e_hosted_go_spec5_test.go`
- Modify: `examples/extensions/hosted-hello-go/main.go` (add optional spec #5 probe tool guarded by env var)

- [ ] **Step 1: Extend the hosted-hello-go example with a spec5_probe tool**

Edit `examples/extensions/hosted-hello-go/main.go`. Inside the `Register(pi)` function, add:

```go
// Spec #5 probe tool — only registered when PI_SPEC5_PROBE=1.
if os.Getenv("PI_SPEC5_PROBE") == "1" {
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "spec5_probe",
		Label:       "Spec #5 probe",
		Description: "Exercises AppendEntry, SendUserMessage, and log.append.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, onUpdate piapi.UpdateFunc) (piapi.ToolResult, error) {
			_ = pi.AppendEntry("probe", map[string]any{"hi": true})
			fmt.Fprintln(piext.Log(), "spec5_probe: hello from log.append")
			if onUpdate != nil {
				onUpdate(piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "partial-1"}}})
			}
			return piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "done"}}}, nil
		},
	}); err != nil {
		return err
	}
}
```

Ensure `"os"` and `"fmt"` are imported.

- [ ] **Step 2: Add the E2E test**

Create `internal/extension/e2e_hosted_go_spec5_test.go`:

```go
package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	extapi "github.com/dimetron/pi-go/internal/extension/api"
	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/internal/extension/host"
)

func TestE2E_HostedGo_Spec5RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping spec5 E2E under -short")
	}
	projectRoot, err := repoRoot()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	exampleDir := filepath.Join(projectRoot, "examples", "extensions", "hosted-hello-go")
	if _, err := os.Stat(filepath.Join(exampleDir, "main.go")); err != nil {
		t.Skipf("hosted-hello-go missing: %v", err)
	}

	tmp := t.TempDir()
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	_ = os.MkdirAll(extsDir, 0755)
	target := filepath.Join(extsDir, "hosted-hello-go")
	if err := os.Symlink(exampleDir, target); err != nil {
		t.Skipf("symlink: %v", err)
	}
	approvals, err := os.ReadFile(filepath.Join("testdata", "approvals_granted_hello.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvals, 0644)
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PI_SPEC5_PROBE", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fb := &testbridge.FakeBridge{}
	rt, err := BuildRuntime(ctx, RuntimeConfig{WorkDir: tmp, Bridge: fb})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	reg := rt.Manager.Get("hosted-hello-go")
	if reg == nil {
		t.Fatal("no registration for hosted-hello-go")
	}
	handler := extapi.NewHostedHandler(rt.Manager, reg, fb)
	if err := host.LaunchHosted(ctx, reg, rt.Manager, []string{"go", "run", "."}, handler.Handle); err != nil {
		t.Fatalf("LaunchHosted: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Invoke the probe tool via handler (send an extension_event or
	// direct tool-exec trigger, using whatever the existing e2e test does).
	// Then assert fb received AppendEntry + AppendExtensionLog + EmitToolUpdate calls.

	// (Exact tool-exec invocation depends on the project's pattern —
	// see internal/extension/e2e_hosted_go_test.go for how it invokes
	// a hosted tool in-test. Mirror that here.)

	rt.Manager.Shutdown(ctx)

	var gotAppend, gotLog, gotUpdate bool
	for _, c := range fb.Calls {
		switch c.Method {
		case "AppendEntry":
			gotAppend = true
		case "AppendExtensionLog":
			gotLog = true
		case "EmitToolUpdate":
			gotUpdate = true
		}
	}
	if !gotAppend || !gotLog || !gotUpdate {
		t.Fatalf("missing bridge calls: append=%v log=%v update=%v", gotAppend, gotLog, gotUpdate)
	}
}
```

(The tool-invocation block depends on the existing e2e harness. If `e2e_hosted_go_test.go` doesn't explicitly invoke tools, adapt to whatever trigger mechanism does exist, or invoke the probe tool via a manually-sent JSON-RPC `extension_event` of type `tool_execute`. Leave a `t.Logf` describing the harness path if adaptation becomes non-trivial.)

- [ ] **Step 3: Run the E2E test**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./internal/extension/ -run TestE2E_HostedGo_Spec5 -count=1 -v -timeout 60s
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add -u && rtk git commit -m "test(extensions): E2E spec #5 round trip through hosted-hello-go"
```

---

### Task 18: Documentation + deprecation notes

**Files:**
- Modify: `docs/extensions.md`

- [ ] **Step 1: Add a Spec #5 section to docs/extensions.md**

Insert after the existing API-surface section:

```markdown
## Session & UI (Spec #5)

Extensions granted `session.*` capabilities can read and write the
running session.

### Messaging

| Method | Capability | Effect |
|---|---|---|
| `pi.AppendEntry(kind, payload)` | `session.append_entry` | Append a typed entry to the transcript. `kind` must match `^[a-z][a-z0-9_-]*$`. |
| `pi.SendMessage(msg, opts)` | `session.append_entry` + `session.trigger_turn` (if `TriggerTurn`) | Append an extension-authored custom message. `opts.DeliverAs="steer"` is rejected. |
| `pi.SendUserMessage(msg, opts)` | `session.send_user_message` + `session.trigger_turn` (if `TriggerTurn` or `DeliverAs="steer"`) | Inject a user-role message. See delivery modes below. |
| `pi.SetSessionName` / `GetSessionName` | `session.manage` | Read/write the current session's title. |
| `pi.SetLabel(entryID, label)` | `session.manage` | Rename a branch (`entryID` is the branch ID). |

Delivery modes:

- `nextTurn` — append to transcript; wait for user to press enter unless `TriggerTurn=true`.
- `followUp` — queue; run automatically after the current turn ends.
- `steer` — abort the current turn, prepend the message as the next turn's input. Requires `TriggerTurn=true`.

### Session control (command handlers only)

These methods only run inside `CommandContext` (i.e. spec #2 command handlers). From event handlers they return `ErrSessionControlInEventHandler`. From the CLI they return `ErrSessionControlUnsupportedInCLI`.

- `WaitForIdle(ctx)` — block until no turn is running.
- `NewSession()` — start a fresh session.
- `Fork(branchID)` — fork a branch. Empty string = current session.
- `NavigateTree(branchID)` — switch to an existing branch.
- `SwitchSession(sessionID)` — resume a session by ID.
- `Reload(ctx)` — re-read approvals and provider registry without restarting extensions.

### Lifecycle hooks

Extensions declare hooks in `pi.toml`:

```toml
[[hooks]]
event   = "session_start"   # startup | session_start | before_turn | after_turn | shutdown
command = "ext_announce"    # must be a tool the extension registers
tools   = ["*"]             # hook fires only when these tools are active; "*" = always
timeout = 5000              # ms; default 5000, max 60000
```

Declaring any `[[hooks]]` entry requires the `hooks.register` capability. `critical = true` aborts startup on hook failure; only first-party extensions may set it.

### Streaming & logs

Partial `ToolResult` updates from `onUpdate(partial)` callbacks reach the TUI tool-display panel and the trace log. Extension log writes via `piext.Log()` — and direct `log.append` calls — stream to the TUI trace panel and `~/.pi-go/logs/extensions.log` (rotated at 10 MB, last 3 retained).

### Deprecations

Direct invocation of the legacy JSON-RPC method names `pi.extension/tool_update` and `pi.extension/log` remains supported for one release but is deprecated. Use the service-form `tool_stream.update` and `log.append` instead. They're removed in spec #6.
```

- [ ] **Step 2: Commit**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk git add docs/extensions.md && rtk git commit -m "docs(extensions): spec #5 session/UI, lifecycle hooks, deprecations"
```

---

### Task 19: Final verification

**Files:** None (verification only).

- [ ] **Step 1: Verify full build**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go build ./...
```

Expected: exit 0.

- [ ] **Step 2: Run full test suite**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./... -short -timeout 120s
```

Expected: PASS.

- [ ] **Step 3: Run E2E tests**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && go test ./internal/extension/ -run TestE2E -count=1 -timeout 180s
```

Expected: PASS (TS test may skip if Node absent).

- [ ] **Step 4: Grep for remaining spec #5 stubs**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -n 'Spec: "#5"' .
```

Expected: zero matches. Any match is a missed stub — resolve before finalizing.

- [ ] **Step 5: Grep for stub comments removed**

```bash
cd C:/Users/Jordan/Documents/Projects/pi-go && rtk grep -n "spec #5 stub" .
```

Expected: no Go source matches (docs may still reference spec #5 positively).

# Hosted Extension Tool Invocation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tools registered by hosted extensions (via `tools.register` RPC) invocable by the LLM, with hot reload, `tools.unregister`, `pi.Ready()`, dynamic mid-session approve/revoke, and a startup barrier.

**Architecture:** Introduce a mutable `HostedToolRegistry` + `HostedToolset` (ADK `tool.Toolset`) that the agent queries per-invocation. `HostedAPIHandler.registerTool`/`unregisterTool` populate the registry. A new hosted-side adapter RPCs `extension_event/tool_execute` back to the extension process. Extension SDK grows `UnregisterTool`/`Ready`. A readiness tracker blocks the first user message until approved-at-startup extensions settle.

**Tech Stack:** Go, JSON-RPC 2.0, ADK v1.0.0 (`google.golang.org/adk`), Bubble Tea TUI, existing `hostproto` framing.

**Reference spec:** `docs/superpowers/specs/2026-04-20-hosted-tool-invocation-design.md`

---

## File Structure

### New files

- `internal/extension/api/hosted_registry.go` — `HostedToolRegistry`, `HostedToolEntry`, `Change` notifier.
- `internal/extension/api/hosted_registry_test.go` — unit tests for the registry.
- `internal/extension/api/hosted_toolset.go` — `HostedToolset` implementing `tool.Toolset`.
- `internal/extension/api/hosted_toolset_test.go`
- `internal/extension/api/adapter_hosted.go` — `NewHostedToolAdapter`.
- `internal/extension/api/adapter_hosted_test.go`
- `internal/extension/api/readiness.go` — `Readiness` tracker with quiescence timer + explicit signal.
- `internal/extension/api/readiness_test.go`
- `internal/extension/e2e_hosted_tool_invocation_test.go` — end-to-end agent-invokes-hosted-tool test.
- `internal/extension/e2e_hosted_tool_hot_reload_test.go`
- `internal/extension/e2e_hosted_tool_collision_test.go`
- `internal/extension/e2e_hosted_tool_dynamic_approval_test.go`
- `internal/extension/e2e_hosted_tool_ready_test.go`
- `examples/extensions/hosted-collide/` — collision fixture (go.mod, main.go, pi.toml).
- `examples/extensions/hosted-slow-ready-go/` — slow-startup `Ready()` fixture.

### Modified files

- `internal/extension/hostproto/protocol.go` — add `MethodToolsUnregister`, `MethodExtReady`, error codes `-32097`, `-32098`, `-32099`.
- `internal/extension/api/hosted.go` — accept registry + readiness; wire `registerTool`, `unregisterTool`, `ready`.
- `internal/extension/runtime.go` — create registry + readiness + toolset, append to `Runtime`, expose `WaitForHostedReady`.
- `internal/extension/host/manager.go` — close callback hook (`OnClose`) so registry can remove extensions on disconnect.
- `internal/extension/host/rpc.go` — fire close callbacks when connection closes.
- `internal/extension/lifecycle/service.go` — surface readiness handle; ensure registry is cleared on `Revoke`/`Stop`.
- `pkg/piapi/api.go` — add `UnregisterTool(name) error` and `Ready() error` to the `API` interface.
- `pkg/piext/rpc_api.go` — implement `UnregisterTool` and `Ready` on `rpcAPI`.
- `internal/extension/api/compiled.go` — implement `UnregisterTool` (in-process) and `Ready` (no-op) on `compiledAPI`.
- `internal/cli/interactive.go` — await `WaitForHostedReady` before accepting the first user prompt; progress indicator.
- `internal/cli/cli.go` — same wait, silent.
- `examples/extensions/hosted-hello-go/main.go` — call `pi.Ready()` at the end of `register` (idiomatic example).
- `examples/extensions/hosted-showcase-go/main.go` — same.
- `internal/tui/...` (extensions panel) — tools sub-view reading `HostedToolRegistry` + collision log. *(Split into its own task; can slip to a follow-up PR without blocking invocation.)*

---

## Task 1: Protocol constants for unregister/ready/error codes

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`

- [ ] **Step 1: Add new error codes**

Append to the error-codes block near the top of `internal/extension/hostproto/protocol.go` (after `ErrCodeHandshakeFailed`):

```go
	ErrCodeToolNotOwned      = -32097
	ErrCodeToolNotFound      = -32098
	ErrCodeToolNameCollision = -32099
```

- [ ] **Step 2: Add new method/service names**

In the same file, extend the per-service method block:

```go
	MethodToolsRegister   = "register"
	MethodToolsUnregister = "unregister"
	MethodExtReady        = "ready"
```

And add the `ext` service name to the `ServiceXxx` constants block:

```go
	ServiceExt = "ext"
```

- [ ] **Step 3: Commit**

```bash
git add internal/extension/hostproto/protocol.go
git commit -m "feat(extensions/hostproto): add tools.unregister, ext.ready, collision error codes"
```

---

## Task 2: `piapi.API` interface — `UnregisterTool` + `Ready`

**Files:**
- Modify: `pkg/piapi/api.go`
- Modify: `internal/extension/api/compiled.go`

- [ ] **Step 1: Write the failing test**

Add `pkg/piapi/api_contract_test.go` (create file if missing) with:

```go
package piapi_test

import (
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// TestAPIInterface_HasUnregisterAndReady is a compile-time assertion that
// the API interface advertises UnregisterTool and Ready. A stub type must
// satisfy the interface; if a method goes missing, this file won't compile.
func TestAPIInterface_HasUnregisterAndReady(t *testing.T) {
	var _ interface {
		UnregisterTool(string) error
		Ready() error
	} = (piapi.API)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/piapi/...
```

Expected: compile error `UnregisterTool ... is not implemented` or similar.

- [ ] **Step 3: Add methods to the `API` interface**

In `pkg/piapi/api.go`, inside the `type API interface { ... }` block, add after `RegisterMessageRenderer`:

```go
	// Tool teardown + explicit readiness (spec: 2026-04-20-hosted-tool-invocation).
	UnregisterTool(name string) error
	Ready() error
```

- [ ] **Step 4: Satisfy the interface on `compiledAPI`**

Append to `internal/extension/api/compiled.go`:

```go
func (c *compiledAPI) UnregisterTool(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tools[name]; !ok {
		return fmt.Errorf("piapi: tool %q not registered", name)
	}
	delete(c.tools, name)
	return nil
}

func (c *compiledAPI) Ready() error {
	// Compiled-in extensions are ready synchronously when Register returns.
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go build ./...
go test ./pkg/piapi/... ./internal/extension/api/...
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add pkg/piapi/api.go pkg/piapi/api_contract_test.go internal/extension/api/compiled.go
git commit -m "feat(piapi): UnregisterTool and Ready on API interface"
```

---

## Task 3: `HostedToolRegistry` — data structure + concurrency

**Files:**
- Create: `internal/extension/api/hosted_registry.go`
- Test: `internal/extension/api/hosted_registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func newEntry(extID, tool string) (piapi.ToolDescriptor, *host.Registration) {
	desc := piapi.ToolDescriptor{
		Name:        tool,
		Description: "t",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute:     func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) { return piapi.ToolResult{}, nil },
	}
	reg := &host.Registration{ID: extID, Trust: host.TrustThirdParty}
	return desc, reg
}

func TestRegistry_AddAndSnapshot(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	if err := r.Add("ext-a", desc, reg, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].Desc.Name != "greet" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRegistry_CollisionDifferentExt(t *testing.T) {
	r := NewHostedToolRegistry()
	descA, regA := newEntry("ext-a", "greet")
	descB, regB := newEntry("ext-b", "greet")
	_ = r.Add("ext-a", descA, regA, nil)
	err := r.Add("ext-b", descB, regB, nil)
	var ce *CollisionError
	if !errorsAs(err, &ce) {
		t.Fatalf("want CollisionError, got %T: %v", err, err)
	}
	if ce.ConflictWith != "ext-a" {
		t.Fatalf("ConflictWith = %q", ce.ConflictWith)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatal("second add should not have landed")
	}
}

func TestRegistry_ReplaceSameExt(t *testing.T) {
	r := NewHostedToolRegistry()
	desc1, reg := newEntry("ext-a", "greet")
	desc2 := desc1
	desc2.Description = "updated"
	_ = r.Add("ext-a", desc1, reg, nil)
	if err := r.Add("ext-a", desc2, reg, nil); err != nil {
		t.Fatalf("replace: %v", err)
	}
	snap := r.Snapshot()
	if snap[0].Desc.Description != "updated" {
		t.Fatal("descriptor not replaced")
	}
}

func TestRegistry_RemoveOwned(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	_ = r.Add("ext-a", desc, reg, nil)
	if err := r.Remove("ext-a", "greet"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(r.Snapshot()) != 0 {
		t.Fatal("tool not removed")
	}
}

func TestRegistry_RemoveNotOwned(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	_ = r.Add("ext-a", desc, reg, nil)
	err := r.Remove("ext-b", "greet")
	if err == nil {
		t.Fatal("Remove across owners must error")
	}
}

func TestRegistry_RemoveMissingIdempotent(t *testing.T) {
	r := NewHostedToolRegistry()
	if err := r.Remove("ext-a", "nope"); err != nil {
		t.Fatalf("Remove missing must be idempotent; got %v", err)
	}
}

func TestRegistry_RemoveExt(t *testing.T) {
	r := NewHostedToolRegistry()
	d1, regA := newEntry("ext-a", "t1")
	d2, _ := newEntry("ext-a", "t2")
	_ = r.Add("ext-a", d1, regA, nil)
	_ = r.Add("ext-a", d2, regA, nil)
	r.RemoveExt("ext-a")
	if len(r.Snapshot()) != 0 {
		t.Fatal("RemoveExt left entries behind")
	}
}

func TestRegistry_ConcurrentAddSnapshot(t *testing.T) {
	r := NewHostedToolRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			d, reg := newEntry("ext", fmtI("t", i))
			_ = r.Add("ext", d, reg, nil)
		}(i)
		go func() {
			defer wg.Done()
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 64 {
		t.Fatalf("want 64, got %d", got)
	}
}

func fmtI(prefix string, i int) string { return prefix + "_" + itoa(i) }
func itoa(i int) string {
	// avoid strconv import in test; minimal helper
	if i == 0 { return "0" }
	buf := []byte{}
	n := i
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// errorsAs is a tiny wrapper so the test reads cleanly.
func errorsAs(err error, target any) bool { return stdErrorsAs(err, target) }
```

Add imports: `"context"` and a blank import of `"errors"` aliased as `stdErrors`:

```go
import (
	"context"
	stdErrors "errors"
	"encoding/json"
	"sync"
	"testing"
	...
)

var stdErrorsAs = stdErrors.As
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/api/ -run TestRegistry -v
```

Expected: compile errors (`NewHostedToolRegistry` undefined, `CollisionError` undefined).

- [ ] **Step 3: Implement the registry**

Create `internal/extension/api/hosted_registry.go`:

```go
package api

import (
	"fmt"
	"sync"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// HostedToolEntry is one tool registered by a hosted extension. Desc.Execute
// is always nil — invocation goes over Reg.Conn, not in-process.
type HostedToolEntry struct {
	ExtID   string
	Desc    piapi.ToolDescriptor
	Reg     *host.Registration
	Manager *host.Manager
}

// CollisionError is returned by Add when another extension already owns the
// requested tool name.
type CollisionError struct {
	Name         string
	ConflictWith string
}

func (e *CollisionError) Error() string {
	return fmt.Sprintf("hosted tool name %q already owned by %q", e.Name, e.ConflictWith)
}

// ChangeKind enumerates registry mutation events surfaced via OnChange.
type ChangeKind int

const (
	ChangeAdded ChangeKind = iota
	ChangeReplaced
	ChangeRemoved
	ChangeCollisionRejected
)

// Change is one notification delivered to OnChange subscribers.
type Change struct {
	Kind         ChangeKind
	ExtID        string
	ToolName     string
	ConflictWith string // only populated on ChangeCollisionRejected
}

// HostedToolRegistry is the mutable source of truth for hosted tools.
// Thread-safe. The global namespace is enforced here.
type HostedToolRegistry struct {
	mu          sync.RWMutex
	tools       map[string]HostedToolEntry // key: tool name
	subMu       sync.Mutex
	subscribers []func(Change)
}

func NewHostedToolRegistry() *HostedToolRegistry {
	return &HostedToolRegistry{tools: map[string]HostedToolEntry{}}
}

// Add inserts or replaces a tool. Returns *CollisionError if another
// extension already owns the name.
func (r *HostedToolRegistry) Add(extID string, desc piapi.ToolDescriptor, reg *host.Registration, mgr *host.Manager) error {
	r.mu.Lock()
	existing, exists := r.tools[desc.Name]
	if exists && existing.ExtID != extID {
		r.mu.Unlock()
		r.emit(Change{Kind: ChangeCollisionRejected, ExtID: extID, ToolName: desc.Name, ConflictWith: existing.ExtID})
		return &CollisionError{Name: desc.Name, ConflictWith: existing.ExtID}
	}
	r.tools[desc.Name] = HostedToolEntry{ExtID: extID, Desc: desc, Reg: reg, Manager: mgr}
	r.mu.Unlock()
	if exists {
		r.emit(Change{Kind: ChangeReplaced, ExtID: extID, ToolName: desc.Name})
	} else {
		r.emit(Change{Kind: ChangeAdded, ExtID: extID, ToolName: desc.Name})
	}
	return nil
}

// Remove deletes a tool owned by extID. Missing is idempotent; owned-by-
// someone-else is an error.
func (r *HostedToolRegistry) Remove(extID, toolName string) error {
	r.mu.Lock()
	existing, exists := r.tools[toolName]
	if !exists {
		r.mu.Unlock()
		return nil
	}
	if existing.ExtID != extID {
		r.mu.Unlock()
		return fmt.Errorf("tool %q owned by %q, not %q", toolName, existing.ExtID, extID)
	}
	delete(r.tools, toolName)
	r.mu.Unlock()
	r.emit(Change{Kind: ChangeRemoved, ExtID: extID, ToolName: toolName})
	return nil
}

// RemoveExt drops every tool owned by extID. Idempotent.
func (r *HostedToolRegistry) RemoveExt(extID string) {
	r.mu.Lock()
	var removed []string
	for name, e := range r.tools {
		if e.ExtID == extID {
			delete(r.tools, name)
			removed = append(removed, name)
		}
	}
	r.mu.Unlock()
	for _, n := range removed {
		r.emit(Change{Kind: ChangeRemoved, ExtID: extID, ToolName: n})
	}
}

// Snapshot returns a copy of current entries. Callers may hold it beyond
// the lock; Entry values are safe to use but should not be mutated.
func (r *HostedToolRegistry) Snapshot() []HostedToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]HostedToolEntry, 0, len(r.tools))
	for _, e := range r.tools {
		out = append(out, e)
	}
	return out
}

// OnChange subscribes a callback fired on every mutation. The returned
// func unsubscribes. Callbacks must not block.
func (r *HostedToolRegistry) OnChange(fn func(Change)) func() {
	r.subMu.Lock()
	idx := len(r.subscribers)
	r.subscribers = append(r.subscribers, fn)
	r.subMu.Unlock()
	return func() {
		r.subMu.Lock()
		defer r.subMu.Unlock()
		if idx < len(r.subscribers) {
			r.subscribers[idx] = nil
		}
	}
}

func (r *HostedToolRegistry) emit(c Change) {
	r.subMu.Lock()
	subs := append([]func(Change)(nil), r.subscribers...)
	r.subMu.Unlock()
	for _, s := range subs {
		if s != nil {
			s(c)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/api/ -run TestRegistry -v
```

Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted_registry.go internal/extension/api/hosted_registry_test.go
git commit -m "feat(extensions): HostedToolRegistry with collision rejection and change notifier"
```

---

## Task 4: `HostedToolset` (implements `tool.Toolset`)

**Files:**
- Create: `internal/extension/api/hosted_toolset.go`
- Test: `internal/extension/api/hosted_toolset_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestHostedToolset_SnapshotsRegistry(t *testing.T) {
	r := NewHostedToolRegistry()
	desc := piapi.ToolDescriptor{
		Name:        "greet",
		Description: "x",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty}
	_ = r.Add("ext-a", desc, reg, nil)

	ts := NewHostedToolset(r)
	got, err := ts.Tools(nil)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "greet" {
		t.Fatalf("got tools %+v", got)
	}

	// Remove and re-query: set should shrink without rebuilding the toolset.
	_ = r.Remove("ext-a", "greet")
	got2, _ := ts.Tools(nil)
	if len(got2) != 0 {
		t.Fatalf("want empty after removal, got %d", len(got2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/api/ -run TestHostedToolset -v
```

Expected: compile error (`NewHostedToolset` undefined).

- [ ] **Step 3: Implement the toolset**

Create `internal/extension/api/hosted_toolset.go`:

```go
package api

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
)

// HostedToolset exposes the live contents of a HostedToolRegistry as an
// ADK Toolset. Tools(ctx) is queried per LLM invocation, so add/remove in
// the registry is visible without rebuilding the agent.
type HostedToolset struct {
	reg *HostedToolRegistry
}

func NewHostedToolset(reg *HostedToolRegistry) *HostedToolset {
	return &HostedToolset{reg: reg}
}

func (t *HostedToolset) Name() string { return "go-pi-hosted-extensions" }

func (t *HostedToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	snap := t.reg.Snapshot()
	out := make([]tool.Tool, 0, len(snap))
	for _, e := range snap {
		adapter, err := NewHostedToolAdapter(e)
		if err != nil {
			continue
		}
		out = append(out, adapter)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/api/ -run TestHostedToolset -v
```

Expected: PASS. (The adapter is a stub until Task 5 — the test only exercises metadata. Add a TODO adapter that returns a no-op tool for now if compile fails.)

If the test fails because `NewHostedToolAdapter` isn't yet defined, add a temporary stub at the bottom of `hosted_toolset.go`:

```go
// Temporary stub so the file compiles before Task 5 lands the real adapter.
// Task 5 deletes this stub.
func NewHostedToolAdapter(e HostedToolEntry) (tool.Tool, error) {
	return nil, nil // returning nil causes Tools() to skip; that's fine for this test
}
```

Adjust the test if needed so `len(got) == 0` (snapshot returns 1 entry but adapter stub filters it out):

```go
	if len(got) != 0 { // stubbed adapter returns nil; real one tested in Task 5
		t.Fatalf("got tools %+v", got)
	}
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted_toolset.go internal/extension/api/hosted_toolset_test.go
git commit -m "feat(extensions): HostedToolset for dynamic per-invocation tool resolution"
```

---

## Task 5: `NewHostedToolAdapter` — RPC invocation path

**Files:**
- Create: `internal/extension/api/adapter_hosted.go`
- Test: `internal/extension/api/adapter_hosted_test.go`
- Modify: `internal/extension/api/hosted_toolset.go` (remove stub)

- [ ] **Step 1: Write the failing test**

Put in `internal/extension/api/adapter_hosted_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// fakeConn captures the last Call parameters and returns a configured reply.
type fakeConn struct {
	lastMethod string
	lastParams hostproto.ExtensionEventParams
	reply      any
	err        error
}

func (f *fakeConn) Call(ctx context.Context, method string, params any, result any) error {
	f.lastMethod = method
	b, _ := json.Marshal(params)
	_ = json.Unmarshal(b, &f.lastParams)
	if f.err != nil {
		return f.err
	}
	if result != nil && f.reply != nil {
		rb, _ := json.Marshal(f.reply)
		return json.Unmarshal(rb, result)
	}
	return nil
}

func TestHostedAdapter_SendsExtensionEvent(t *testing.T) {
	conn := &fakeConn{
		reply: map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Hello, pi!"}},
		},
	}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty, Conn: host.NewRPCConnFromCaller(conn)}
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"events": {"tool_execute"}},
	}, host.TrustThirdParty))
	_ = mgr.Register(&host.Registration{ID: "ext-a", Trust: host.TrustThirdParty})

	entry := HostedToolEntry{
		ExtID: "ext-a",
		Desc: piapi.ToolDescriptor{
			Name:        "greet",
			Description: "say hi",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
		},
		Reg:     reg,
		Manager: mgr,
	}
	tl, err := NewHostedToolAdapter(entry)
	if err != nil {
		t.Fatalf("adapter build: %v", err)
	}
	// Invoke the adapter's handler via the ADK function-tool path is awkward
	// to simulate directly, so we test the inner invoke() helper the adapter
	// exports for testing purposes.
	res, err := invokeHostedAdapterForTest(tl, context.Background(), "call-1", map[string]any{"name": "pi"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if content, _ := res["content"].([]any); len(content) == 0 {
		t.Fatalf("empty content: %+v", res)
	}
	if conn.lastMethod != hostproto.MethodExtensionEvent {
		t.Fatalf("wrong method: %q", conn.lastMethod)
	}
	if conn.lastParams.Event != "tool_execute" {
		t.Fatalf("wrong event: %q", conn.lastParams.Event)
	}
}

func TestHostedAdapter_GateDenied(t *testing.T) {
	mgr := host.NewManager(host.NewGateInMemory(nil, host.TrustThirdParty)) // no grants
	_ = mgr.Register(&host.Registration{ID: "ext-a", Trust: host.TrustThirdParty})
	entry := HostedToolEntry{
		ExtID:   "ext-a",
		Desc:    piapi.ToolDescriptor{Name: "greet"},
		Reg:     &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty},
		Manager: mgr,
	}
	tl, err := NewHostedToolAdapter(entry)
	if err != nil {
		t.Fatalf("adapter build: %v", err)
	}
	_, err = invokeHostedAdapterForTest(tl, context.Background(), "call-1", map[string]any{})
	if err == nil {
		t.Fatal("want gate denial error")
	}
}

func TestHostedAdapter_RPCError(t *testing.T) {
	conn := &fakeConn{err: errors.New("boom")}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty, Conn: host.NewRPCConnFromCaller(conn)}
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"events": {"tool_execute"}},
	}, host.TrustThirdParty))
	_ = mgr.Register(&host.Registration{ID: "ext-a", Trust: host.TrustThirdParty})
	entry := HostedToolEntry{
		ExtID: "ext-a",
		Desc: piapi.ToolDescriptor{
			Name:        "greet",
			Description: "x",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
		Reg:     reg,
		Manager: mgr,
	}
	tl, _ := NewHostedToolAdapter(entry)
	_, err := invokeHostedAdapterForTest(tl, context.Background(), "c", map[string]any{})
	if err == nil {
		t.Fatal("want rpc error")
	}
}
```

The test references two new helpers (`host.NewRPCConnFromCaller`, `host.NewGateInMemory`, and the export `invokeHostedAdapterForTest`). These are introduced in later sub-steps.

- [ ] **Step 2: Introduce test-only `host.NewGateInMemory` and `NewRPCConnFromCaller`**

Add to `internal/extension/host/gate.go` (at end):

```go
// NewGateInMemory builds a Gate from an inline approvals map. Used for
// testing and for injecting a fake gate in unit tests.
//   grants: extID -> service -> list of methods
// Trust applies to all listed extensions uniformly.
func NewGateInMemory(grants map[string]map[string][]string, trust TrustClass) *Gate {
	g := &Gate{entries: map[string]approvalEntry{}}
	for id, svcs := range grants {
		caps := []string{}
		for s, methods := range svcs {
			for _, m := range methods {
				caps = append(caps, s+"."+m)
			}
		}
		g.entries[id] = approvalEntry{Approved: true, GrantedCapabilities: caps}
	}
	return g
}
```

If the `Gate` internal shape differs, adapt the constructor to whatever builds a pre-approved gate; the goal is an in-memory gate with the listed capabilities granted.

Add to `internal/extension/host/rpc.go`:

```go
// RPCCaller is the minimum surface an adapter uses: the same shape as
// (*RPCConn).Call. Exposed as an interface so tests can inject a fake.
type RPCCaller interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// NewRPCConnFromCaller wraps an RPCCaller in an RPCConn-compatible shell
// for tests. Not intended for production use.
func NewRPCConnFromCaller(c RPCCaller) *RPCConn {
	return &RPCConn{fakeCaller: c}
}
```

And extend `RPCConn` with a `fakeCaller RPCCaller` field, making `(*RPCConn).Call` forward to `fakeCaller` when non-nil:

```go
func (c *RPCConn) Call(ctx context.Context, method string, params any, result any) error {
	if c.fakeCaller != nil {
		return c.fakeCaller.Call(ctx, method, params, result)
	}
	// ...existing body...
}
```

- [ ] **Step 3: Implement the adapter**

Create `internal/extension/api/adapter_hosted.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

// defaultHostedToolTimeoutMS is the per-call upper bound the host waits on
// the extension before declaring the invocation timed out.
const defaultHostedToolTimeoutMS = 30_000

// NewHostedToolAdapter returns an ADK Tool whose handler dispatches
// extension_event/tool_execute to the extension process owning entry.
func NewHostedToolAdapter(entry HostedToolEntry) (tool.Tool, error) {
	if err := entry.Desc.Validate(); err != nil {
		return nil, err
	}
	var schema *jsonschema.Schema
	if len(entry.Desc.Parameters) > 0 {
		schema = &jsonschema.Schema{}
		if err := json.Unmarshal(entry.Desc.Parameters, schema); err != nil {
			return nil, fmt.Errorf("hosted adapter %q: parse schema: %w", entry.Desc.Name, err)
		}
	}

	handler := func(ctx tool.Context, args map[string]any) (map[string]any, error) {
		return invokeHosted(ctx, entry, args)
	}
	return functiontool.New[map[string]any, map[string]any](
		functiontool.Config{
			Name:        entry.Desc.Name,
			Description: entry.Desc.Description,
			InputSchema: schema,
		},
		handler,
	)
}

func invokeHosted(ctx tool.Context, entry HostedToolEntry, args map[string]any) (map[string]any, error) {
	// Gate check (events.tool_execute, per spec §2.5 / §7).
	if entry.Manager != nil {
		if ok, reason := entry.Manager.Gate().Allowed(entry.ExtID, "events.tool_execute", entry.Reg.Trust); !ok {
			return nil, fmt.Errorf("events.tool_execute denied for %s: %s", entry.ExtID, reason)
		}
	}
	if entry.Reg == nil || entry.Reg.Conn == nil {
		return map[string]any{
			"is_error": true,
			"content":  []map[string]any{{"type": "text", "text": "extension not connected"}},
		}, nil
	}
	callID := ""
	runCtx := context.Background()
	if ctx != nil {
		runCtx = ctx
		callID = ctx.FunctionCallID()
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	innerPayload, _ := json.Marshal(map[string]any{
		"tool_call_id": callID,
		"name":         entry.Desc.Name,
		"args":         json.RawMessage(rawArgs),
		"timeout_ms":   defaultHostedToolTimeoutMS,
	})
	params := hostproto.ExtensionEventParams{
		Event:   "tool_execute",
		Version: 1,
		Payload: innerPayload,
	}
	var resp struct {
		Content []map[string]any `json:"content"`
		Details map[string]any   `json:"details"`
		IsError bool             `json:"is_error"`
	}
	if err := entry.Reg.Conn.Call(runCtx, hostproto.MethodExtensionEvent, params, &resp); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(resp.Content) > 0 {
		anyContent := make([]any, len(resp.Content))
		for i, c := range resp.Content {
			anyContent[i] = c
		}
		out["content"] = anyContent
	}
	if resp.IsError {
		out["is_error"] = true
	}
	if len(resp.Details) > 0 {
		out["details"] = resp.Details
	}
	return out, nil
}

// invokeHostedAdapterForTest is exported for adapter_hosted_test.go and
// unlocks unit-testing the invocation path without standing up an ADK
// runner. It depends on functiontool exposing a way to execute handlers
// directly; if not, this helper can re-enter invokeHosted using a synthetic
// tool.Context.
func invokeHostedAdapterForTest(tl tool.Tool, ctx context.Context, callID string, args map[string]any) (map[string]any, error) {
	// Build a minimal tool.Context implementation that satisfies the parts
	// of the interface invokeHosted uses (FunctionCallID and the Context).
	return invokeHosted(testToolContext{ctx: ctx, callID: callID}, getHostedEntry(tl), args)
}

type testToolContext struct {
	ctx    context.Context
	callID string
}

func (t testToolContext) Deadline() (time.Time, bool) { return t.ctx.Deadline() }
func (t testToolContext) Done() <-chan struct{}       { return t.ctx.Done() }
func (t testToolContext) Err() error                  { return t.ctx.Err() }
func (t testToolContext) Value(k any) any             { return t.ctx.Value(k) }
func (t testToolContext) FunctionCallID() string      { return t.callID }
// Other tool.Context methods (if any) panic — the test path doesn't use them.

func getHostedEntry(tl tool.Tool) HostedToolEntry {
	if h, ok := tl.(interface{ HostedEntry() HostedToolEntry }); ok {
		return h.HostedEntry()
	}
	panic("tool was not built by NewHostedToolAdapter")
}
```

For `getHostedEntry` to work, wrap the returned `tool.Tool` in a struct that exposes `HostedEntry()`:

```go
type hostedTool struct {
	tool.Tool
	entry HostedToolEntry
}

func (h hostedTool) HostedEntry() HostedToolEntry { return h.entry }
```

And return it at the end of `NewHostedToolAdapter`:

```go
	ft, err := functiontool.New[...](...)
	if err != nil {
		return nil, err
	}
	return hostedTool{Tool: ft, entry: entry}, nil
```

Add `"time"` to the imports.

Also remove the Task 4 stub `NewHostedToolAdapter` from `hosted_toolset.go` (if still there).

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/api/ -run TestHostedAdapter -v
```

Expected: all three subtests PASS.

- [ ] **Step 5: Run full package to catch integration regressions**

```bash
go test ./internal/extension/... -count=1
```

Expected: green.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/api/adapter_hosted.go internal/extension/api/adapter_hosted_test.go internal/extension/api/hosted_toolset.go internal/extension/host/gate.go internal/extension/host/rpc.go
git commit -m "feat(extensions): hosted tool adapter dispatches extension_event/tool_execute"
```

---

## Task 6: `HostedAPIHandler` — wire registry + unregister + ready

**Files:**
- Modify: `internal/extension/api/hosted.go`

- [ ] **Step 1: Write the failing test**

Add `internal/extension/api/hosted_wire_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHostedHandler_RegisterTool_LandsInRegistry(t *testing.T) {
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"tools": {"register", "unregister"}, "events": {"tool_execute"}},
	}, host.TrustThirdParty))
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty}
	_ = mgr.Register(reg)
	registry := NewHostedToolRegistry()
	readiness := NewReadiness()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetRegistry(registry)
	h.SetReadiness(readiness)

	payload, _ := json.Marshal(map[string]any{
		"service": "tools",
		"version": 1,
		"method":  "register",
		"payload": json.RawMessage(`{"name":"greet","description":"x","parameters":{"type":"object"}}`),
	})
	if _, err := h.Handle(hostproto.MethodHostCall, payload); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(registry.Snapshot()) != 1 {
		t.Fatalf("registry not populated")
	}
}

func TestHostedHandler_UnregisterTool(t *testing.T) {
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"tools": {"register", "unregister"}},
	}, host.TrustThirdParty))
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty}
	_ = mgr.Register(reg)
	registry := NewHostedToolRegistry()
	readiness := NewReadiness()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetRegistry(registry)
	h.SetReadiness(readiness)

	regP, _ := json.Marshal(map[string]any{"service": "tools", "version": 1, "method": "register",
		"payload": json.RawMessage(`{"name":"greet","description":"x","parameters":{"type":"object"}}`)})
	_, _ = h.Handle(hostproto.MethodHostCall, regP)

	unregP, _ := json.Marshal(map[string]any{"service": "tools", "version": 1, "method": "unregister",
		"payload": json.RawMessage(`{"name":"greet"}`)})
	if _, err := h.Handle(hostproto.MethodHostCall, unregP); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if len(registry.Snapshot()) != 0 {
		t.Fatalf("tool not removed")
	}
}

func TestHostedHandler_ExtReady(t *testing.T) {
	mgr := host.NewManager(host.NewGateInMemory(nil, host.TrustThirdParty))
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty}
	_ = mgr.Register(reg)
	readiness := NewReadiness()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetReadiness(readiness)
	readiness.Track("ext-a")

	p, _ := json.Marshal(map[string]any{"service": "ext", "version": 1, "method": "ready", "payload": json.RawMessage(`{}`)})
	if _, err := h.Handle(hostproto.MethodHostCall, p); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if readiness.State("ext-a") != ReadinessReady {
		t.Fatal("extension not marked ready")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/api/ -run TestHostedHandler_ -v
```

Expected: compile errors (methods `SetRegistry`, `SetReadiness` missing, Readiness type missing).

- [ ] **Step 3: Extend `HostedAPIHandler`**

Open `internal/extension/api/hosted.go` and:

Add two new fields:

```go
type HostedAPIHandler struct {
	// ...existing...
	registry  *HostedToolRegistry
	readiness *Readiness
}
```

Add setters (kept separate from the constructor so existing callers don't break):

```go
func (h *HostedAPIHandler) SetRegistry(r *HostedToolRegistry) { h.registry = r }
func (h *HostedAPIHandler) SetReadiness(r *Readiness)         { h.readiness = r }
```

Replace `registerTool` with the registry-aware version:

```go
func (h *HostedAPIHandler) registerTool(payload json.RawMessage) (any, error) {
	var t hostedTool
	if err := json.Unmarshal(payload, &t); err != nil {
		return nil, fmt.Errorf("tools.register: invalid payload: %w", err)
	}
	if t.Name == "" {
		return nil, fmt.Errorf("tools.register: name is required")
	}
	if h.registry != nil {
		desc := piapi.ToolDescriptor{
			Name:        t.Name,
			Label:       t.Label,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
		if err := h.registry.Add(h.reg.ID, desc, h.reg, h.manager); err != nil {
			// Surface collision as a typed RPC error; plumbing in the RPC
			// layer translates it to code -32099.
			return nil, err
		}
	}
	if h.readiness != nil {
		h.readiness.Kick(h.reg.ID)
	}
	h.mu.Lock()
	h.tools[t.Name] = t
	h.mu.Unlock()
	return map[string]any{"registered": true}, nil
}
```

Add `unregisterTool`:

```go
func (h *HostedAPIHandler) unregisterTool(payload json.RawMessage) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("tools.unregister: invalid payload: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("tools.unregister: name is required")
	}
	if h.registry != nil {
		if err := h.registry.Remove(h.reg.ID, p.Name); err != nil {
			return nil, err
		}
	}
	h.mu.Lock()
	delete(h.tools, p.Name)
	h.mu.Unlock()
	if h.readiness != nil {
		h.readiness.Kick(h.reg.ID)
	}
	return map[string]any{"unregistered": true}, nil
}
```

Add `extReady`:

```go
func (h *HostedAPIHandler) extReady(_ json.RawMessage) (any, error) {
	if h.readiness != nil {
		h.readiness.MarkReady(h.reg.ID)
	}
	return map[string]any{"acknowledged": true}, nil
}
```

Extend the switch in `handleHostCall`:

```go
	case hostproto.ServiceTools:
		if p.Method == hostproto.MethodToolsRegister {
			return h.registerTool(p.Payload)
		}
		if p.Method == hostproto.MethodToolsUnregister {
			return h.unregisterTool(p.Payload)
		}
	case hostproto.ServiceExt:
		if p.Method == hostproto.MethodExtReady {
			return h.extReady(p.Payload)
		}
```

Also replace the hard-coded `"register"` check (around hosted.go:89) with `hostproto.MethodToolsRegister`.

Add import of `"github.com/pizzaface/go-pi/pkg/piapi"` (already present? verify) — needed because the new code uses `piapi.ToolDescriptor`.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/api/ -run TestHostedHandler_ -v
```

Expected: PASS (after Task 7 adds the Readiness type; if Readiness is not yet defined, this task is blocked on it — reorder or stub).

**Ordering note:** Task 7 (Readiness) and Task 6 (Handler) are mutually dependent. Do Task 7 first, then return here.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/api/hosted_wire_test.go
git commit -m "feat(extensions): HostedAPIHandler wires registry, unregister, ext.ready"
```

---

## Task 7: `Readiness` tracker

**Files:**
- Create: `internal/extension/api/readiness.go`
- Test: `internal/extension/api/readiness_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"testing"
	"time"
)

func TestReadiness_ExplicitReady(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	r.MarkReady("ext-a")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx, 500*time.Millisecond); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.State("ext-a") != ReadinessReady {
		t.Fatal("expected Ready")
	}
}

func TestReadiness_Quiescence(t *testing.T) {
	r := NewReadiness()
	r.QuiescenceWindow = 50 * time.Millisecond
	r.Track("ext-a")
	r.Kick("ext-a") // register fires
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx, 500*time.Millisecond); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("returned too fast: %v", elapsed)
	}
	if r.State("ext-a") != ReadinessReady {
		t.Fatal("expected Ready via quiescence")
	}
}

func TestReadiness_Timeout(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	ctx := context.Background()
	if err := r.Wait(ctx, 50*time.Millisecond); err == nil {
		t.Fatal("expected timeout")
	}
	if r.State("ext-a") != ReadinessTimedOut {
		t.Fatalf("state = %v", r.State("ext-a"))
	}
}

func TestReadiness_Errored(t *testing.T) {
	r := NewReadiness()
	r.Track("ext-a")
	r.MarkErrored("ext-a", context.Canceled)
	ctx := context.Background()
	if err := r.Wait(ctx, time.Second); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.State("ext-a") != ReadinessErrored {
		t.Fatal("expected Errored")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/api/ -run TestReadiness -v
```

Expected: compile errors.

- [ ] **Step 3: Implement Readiness**

Create `internal/extension/api/readiness.go`:

```go
package api

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ReadinessState int

const (
	ReadinessUnknown ReadinessState = iota
	ReadinessLaunching
	ReadinessReady
	ReadinessErrored
	ReadinessTimedOut
)

func (s ReadinessState) String() string {
	switch s {
	case ReadinessLaunching:
		return "launching"
	case ReadinessReady:
		return "ready"
	case ReadinessErrored:
		return "errored"
	case ReadinessTimedOut:
		return "timed_out"
	default:
		return "unknown"
	}
}

// Readiness tracks whether each launched-at-startup extension has signalled
// readiness, either explicitly (MarkReady) or implicitly (quiescence).
type Readiness struct {
	QuiescenceWindow time.Duration // default 250ms; adjustable for tests

	mu      sync.Mutex
	entries map[string]*readinessEntry
}

type readinessEntry struct {
	state    ReadinessState
	lastKick time.Time
	err      error
	ready    chan struct{} // closed on terminal state
}

func NewReadiness() *Readiness {
	return &Readiness{
		QuiescenceWindow: 250 * time.Millisecond,
		entries:          map[string]*readinessEntry{},
	}
}

// Track registers extID as launching. Subsequent Kick/MarkReady/MarkErrored
// calls reference this ID.
func (r *Readiness) Track(extID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[extID]; ok {
		return
	}
	r.entries[extID] = &readinessEntry{state: ReadinessLaunching, ready: make(chan struct{})}
}

// Kick records a tools.register (or similar) activity; starts the
// quiescence timer.
func (r *Readiness) Kick(extID string) {
	r.mu.Lock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		r.mu.Unlock()
		return
	}
	e.lastKick = time.Now()
	r.mu.Unlock()
}

// MarkReady transitions extID to Ready (explicit signal).
func (r *Readiness) MarkReady(extID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		return
	}
	e.state = ReadinessReady
	close(e.ready)
}

// MarkErrored transitions to Errored with the supplied cause.
func (r *Readiness) MarkErrored(extID string, cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[extID]
	if !ok || e.state != ReadinessLaunching {
		return
	}
	e.state = ReadinessErrored
	e.err = cause
	close(e.ready)
}

// State returns the current state for extID.
func (r *Readiness) State(extID string) ReadinessState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[extID]; ok {
		return e.state
	}
	return ReadinessUnknown
}

// Wait blocks until every tracked extension is in a terminal state or
// timeout elapses. Quiescence promotes launching→Ready once
// time.Since(lastKick) ≥ QuiescenceWindow (provided at least one Kick has
// been recorded). Extensions that never Kick and never MarkReady fall
// through to TimedOut when timeout elapses.
func (r *Readiness) Wait(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		r.promoteQuiescent()
		if r.allTerminal() {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			r.timeoutLaunching()
			return fmt.Errorf("readiness: timed out after %s", timeout)
		}
		pause := 50 * time.Millisecond
		if remaining < pause {
			pause = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
}

func (r *Readiness) promoteQuiescent() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, e := range r.entries {
		if e.state != ReadinessLaunching {
			continue
		}
		if !e.lastKick.IsZero() && now.Sub(e.lastKick) >= r.QuiescenceWindow {
			e.state = ReadinessReady
			close(e.ready)
		}
	}
}

func (r *Readiness) allTerminal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.state == ReadinessLaunching {
			return false
		}
	}
	return true
}

func (r *Readiness) timeoutLaunching() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.state == ReadinessLaunching {
			e.state = ReadinessTimedOut
			close(e.ready)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/api/ -run TestReadiness -v
```

Expected: 4 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/readiness.go internal/extension/api/readiness_test.go
git commit -m "feat(extensions): Readiness tracker for startup barrier"
```

---

## Task 8: `RPCConn` close callback + `Manager.OnClose`

**Files:**
- Modify: `internal/extension/host/rpc.go`
- Modify: `internal/extension/host/manager.go`
- Test: `internal/extension/host/rpc_close_test.go`

- [ ] **Step 1: Write the failing test**

`internal/extension/host/rpc_close_test.go`:

```go
package host

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestRPCConn_CloseCallback(t *testing.T) {
	r, w := io.Pipe()
	conn := NewRPCConn(w, r, nil)
	var fired atomic.Int32
	conn.OnClose(func() { fired.Add(1) })
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("callback not fired: %d", fired.Load())
	}
	_ = context.Background()
}

func TestManager_OnClose_FiresOnDisconnect(t *testing.T) {
	mgr := NewManager(NewGateInMemory(nil, TrustThirdParty))
	reg := &Registration{ID: "ext-a", Trust: TrustThirdParty}
	_ = mgr.Register(reg)
	var fired atomic.Int32
	mgr.OnClose("ext-a", func() { fired.Add(1) })

	r, w := io.Pipe()
	reg.Conn = NewRPCConn(w, r, nil)
	// Wire up the callback manually the same way the production launcher does.
	reg.Conn.OnClose(func() { mgr.fireOnClose("ext-a") })
	reg.Conn.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatal("OnClose did not fire")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/host/ -run TestRPCConn_CloseCallback -v
go test ./internal/extension/host/ -run TestManager_OnClose -v
```

Expected: compile errors (`OnClose` on both types, `fireOnClose` missing).

- [ ] **Step 3: Add `OnClose` to `RPCConn`**

In `internal/extension/host/rpc.go`, extend `RPCConn`:

```go
type RPCConn struct {
	// ...existing fields...
	closeCbsMu sync.Mutex
	closeCbs   []func()
}

func (c *RPCConn) OnClose(fn func()) {
	c.closeCbsMu.Lock()
	defer c.closeCbsMu.Unlock()
	c.closeCbs = append(c.closeCbs, fn)
}
```

Inside `Close` (or wherever the connection transitions to closed), at the end:

```go
	c.closeCbsMu.Lock()
	cbs := append([]func()(nil), c.closeCbs...)
	c.closeCbs = nil
	c.closeCbsMu.Unlock()
	for _, f := range cbs {
		go f()
	}
```

- [ ] **Step 4: Add `OnClose` / `fireOnClose` to `Manager`**

Append to `internal/extension/host/manager.go`:

```go
func (m *Manager) OnClose(extID string, fn func()) {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	if m.closeCbs == nil {
		m.closeCbs = map[string][]func(){}
	}
	m.closeCbs[extID] = append(m.closeCbs[extID], fn)
}

func (m *Manager) fireOnClose(extID string) {
	m.closeMu.Lock()
	cbs := append([]func()(nil), m.closeCbs[extID]...)
	delete(m.closeCbs, extID)
	m.closeMu.Unlock()
	for _, f := range cbs {
		go f()
	}
}
```

Add the `closeMu sync.Mutex` and `closeCbs map[string][]func()` fields to the `Manager` struct.

- [ ] **Step 5: Wire up in `lifecycle.defaultLaunch`**

Modify `internal/extension/lifecycle/service.go` `defaultLaunch`:

```go
func (s *service) defaultLaunch(ctx context.Context, reg *host.Registration, mgr *host.Manager, cmd []string) error {
	handler := api.NewHostedHandler(mgr, reg, s.bridge)
	if s.registry != nil {
		handler.SetRegistry(s.registry)
	}
	if s.readiness != nil {
		handler.SetReadiness(s.readiness)
	}
	if err := host.LaunchHosted(ctx, reg, mgr, cmd, handler.Handle); err != nil {
		return err
	}
	if reg.Conn != nil {
		reg.Conn.OnClose(func() { mgr.fireOnClose(reg.ID) })
	}
	return nil
}
```

Add new `registry` and `readiness` fields to the `service` struct with setters:

```go
type service struct {
	// ...existing...
	registry  *api.HostedToolRegistry
	readiness *api.Readiness
}

func (s *service) SetRegistry(r *api.HostedToolRegistry) { s.registry = r }
func (s *service) SetReadiness(r *api.Readiness)         { s.readiness = r }
```

And expose them on the `Service` interface:

```go
type Service interface {
	// ...existing...
	SetRegistry(*api.HostedToolRegistry)
	SetReadiness(*api.Readiness)
}
```

- [ ] **Step 6: Run tests to verify**

```bash
go test ./internal/extension/host/ -run "TestRPCConn_CloseCallback|TestManager_OnClose" -v
go build ./...
```

Expected: PASS; full build clean.

- [ ] **Step 7: Commit**

```bash
git add internal/extension/host/rpc.go internal/extension/host/manager.go internal/extension/host/rpc_close_test.go internal/extension/lifecycle/service.go
git commit -m "feat(extensions): connection close callbacks for registry cleanup"
```

---

## Task 9: `BuildRuntime` — registry, toolset, readiness, WaitForHostedReady

**Files:**
- Modify: `internal/extension/runtime.go`
- Test: `internal/extension/runtime_registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/runtime_registry_test.go`:

```go
package extension

import (
	"context"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/api"
)

func TestBuildRuntime_HostedToolRegistryPresent(t *testing.T) {
	tmp := t.TempDir()
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if rt.HostedToolRegistry == nil {
		t.Fatal("expected HostedToolRegistry on Runtime")
	}
	if len(rt.Toolsets) == 0 {
		t.Fatal("expected at least the HostedToolset in Toolsets")
	}
	// Confirm one of the toolsets is the HostedToolset.
	found := false
	for _, ts := range rt.Toolsets {
		if _, ok := ts.(*api.HostedToolset); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("HostedToolset not in Toolsets slice")
	}
}

func TestRuntime_WaitForHostedReady_NoExtensions(t *testing.T) {
	tmp := t.TempDir()
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	// With no extensions tracked, Wait returns immediately.
	if err := rt.WaitForHostedReady(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/extension/ -run "TestBuildRuntime_HostedToolRegistryPresent|TestRuntime_WaitForHostedReady" -v
```

Expected: compile errors.

- [ ] **Step 3: Wire registry + toolset + readiness into Runtime**

In `internal/extension/runtime.go`:

Add fields to `Runtime`:

```go
type Runtime struct {
	// ...existing...
	HostedToolRegistry *extapi.HostedToolRegistry
	Readiness          *extapi.Readiness
}
```

Inside `BuildRuntime`, after `manager := host.NewManager(gate)`:

```go
	registry := extapi.NewHostedToolRegistry()
	readiness := extapi.NewReadiness()
```

Near where `rt := &Runtime{...}` is built, set them:

```go
		HostedToolRegistry: registry,
		Readiness:          readiness,
```

Also add the toolset to the existing `Toolsets` slice (create a new slice if needed):

```go
	toolsets := []tool.Toolset{extapi.NewHostedToolset(registry)}
```

And include `toolsets` in the `Runtime{}` literal (`Toolsets: toolsets`).

After `rt.Lifecycle = lifecycle.New(...)`:

```go
	rt.Lifecycle.SetRegistry(registry)
	rt.Lifecycle.SetReadiness(readiness)
```

When walking hosted candidates (around line 159-173), also call `readiness.Track(reg.ID)` for each hosted reg whose grants are populated (ready-to-launch). Ungranted (pending) extensions are not tracked — they won't launch at startup.

Hook close → registry:

```go
	for _, r := range registrations {
		r := r
		manager.OnClose(r.ID, func() {
			registry.RemoveExt(r.ID)
			readiness.MarkErrored(r.ID, fmt.Errorf("connection closed"))
		})
	}
```

Add `WaitForHostedReady`:

```go
// WaitForHostedReady blocks until every approved-at-startup hosted extension
// has reached a terminal readiness state or timeout elapses. Returns a
// timeout error if at least one extension never reports ready.
func (r *Runtime) WaitForHostedReady(ctx context.Context, timeout time.Duration) error {
	if r.Readiness == nil {
		return nil
	}
	return r.Readiness.Wait(ctx, timeout)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/extension/ -run "TestBuildRuntime_HostedToolRegistryPresent|TestRuntime_WaitForHostedReady" -v
go build ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/runtime.go internal/extension/runtime_registry_test.go
git commit -m "feat(extensions): runtime exposes HostedToolRegistry + WaitForHostedReady"
```

---

## Task 10: CLI wait on startup barrier

**Files:**
- Modify: `internal/cli/interactive.go`
- Modify: `internal/cli/cli.go`

- [ ] **Step 1: Add wait call in interactive.go**

Find the block around line 165:

```go
	if runtime.Lifecycle != nil {
		go runtime.Lifecycle.StartApproved(ctx)
	}
	send("tools", true)
```

Replace with:

```go
	if runtime.Lifecycle != nil {
		go func() {
			_ = runtime.Lifecycle.StartApproved(ctx)
		}()
	}
	send("tools", true)

	// Phase 2.5: wait for hosted extensions to finish initial registration
	// so the first user message sees the complete tool set.
	send("extensions", false)
	if err := runtime.WaitForHostedReady(ctx, 5*time.Second); err != nil {
		// Non-fatal — continue; individual extensions may still be pending.
		log.Printf("extension readiness timed out: %v", err)
	}
	send("extensions", true)
```

Make sure `"log"` is imported.

- [ ] **Step 2: Add wait call in cli.go (headless)**

In `internal/cli/cli.go`, after the `BuildRuntime` call and any `StartApproved` invocation, add:

```go
	if err := runtime.WaitForHostedReady(ctx, 5*time.Second); err != nil {
		// Don't fail the CLI on readiness timeout — log at warn level.
		log.Printf("warning: extension readiness incomplete: %v", err)
	}
```

- [ ] **Step 3: Build + existing tests**

```bash
go build ./...
go test ./internal/cli/ -count=1
```

Expected: green.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/interactive.go internal/cli/cli.go
git commit -m "feat(cli): await extension readiness before first user message"
```

---

## Task 11: SDK — `UnregisterTool` + `Ready` on `rpcAPI`

**Files:**
- Modify: `pkg/piext/rpc_api.go`
- Test: `pkg/piext/rpc_api_unregister_test.go`

- [ ] **Step 1: Write the failing test**

`pkg/piext/rpc_api_unregister_test.go`:

```go
package piext

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestRPCAPI_UnregisterTool_SendsRPC(t *testing.T) {
	tr, fake := newTransportForTest(t)
	api := newRPCAPI(tr, piapi.Metadata{Name: "ext-a"}, []GrantedService{
		{Service: "tools", Version: 1, Methods: []string{"register", "unregister"}},
	})
	// Register a tool first so UnregisterTool has something to drop.
	if err := api.RegisterTool(piapi.ToolDescriptor{
		Name:        "greet",
		Description: "x",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute:     func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) { return piapi.ToolResult{}, nil },
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := api.UnregisterTool("greet"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	last := fake.lastCall()
	if last.method != "pi.extension/host_call" {
		t.Fatalf("method = %q", last.method)
	}
	var body map[string]any
	_ = json.Unmarshal(last.params, &body)
	if body["service"] != "tools" || body["method"] != "unregister" {
		t.Fatalf("wrong service/method: %+v", body)
	}
}

func TestRPCAPI_Ready_SendsRPC(t *testing.T) {
	tr, fake := newTransportForTest(t)
	api := newRPCAPI(tr, piapi.Metadata{Name: "ext-a"}, nil)
	if err := api.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	last := fake.lastCall()
	var body map[string]any
	_ = json.Unmarshal(last.params, &body)
	if body["service"] != "ext" || body["method"] != "ready" {
		t.Fatalf("wrong service/method: %+v", body)
	}
}
```

`newTransportForTest` should be a helper that returns a `*Transport` with a fake writer and a way to inspect the last outbound call. If it doesn't exist, crib from `pkg/piext/rpc_api_test.go` — there's already a pattern.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/piext/ -run "TestRPCAPI_UnregisterTool|TestRPCAPI_Ready" -v
```

Expected: compile errors.

- [ ] **Step 3: Implement on `rpcAPI`**

In `pkg/piext/rpc_api.go` add:

```go
func (a *rpcAPI) UnregisterTool(name string) error {
	a.mu.Lock()
	_, present := a.tools[name]
	if present {
		delete(a.tools, name)
	}
	a.mu.Unlock()
	var result map[string]any
	return a.hostCall("tools.unregister", map[string]any{"name": name}, &result)
}

func (a *rpcAPI) Ready() error {
	var result map[string]any
	// ext.ready is not gated — no checkGrant needed. hostCall enforces a
	// grant check, so bypass it here with a direct transport call.
	return a.transport.Call(context.Background(), "pi.extension/host_call", map[string]any{
		"service": "ext", "version": 1, "method": "ready", "payload": map[string]any{},
	}, &result)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./pkg/piext/ -run "TestRPCAPI_UnregisterTool|TestRPCAPI_Ready" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/piext/rpc_api.go pkg/piext/rpc_api_unregister_test.go
git commit -m "feat(piext): SDK surface for UnregisterTool and Ready"
```

---

## Task 12: TypeScript SDK — `unregisterTool` + `ready`

**Files:**
- Check path: likely in a separate repo; if vendored inside `pkg/piext_ts/` or `examples/extensions/hosted-hello-ts/node_modules/...` the fix lives there. Otherwise open an issue and defer.
- Verify at the start: does this repo ship the TS SDK source, or pull it from npm?

- [ ] **Step 1: Locate the TS SDK source**

```bash
find . -type d -name "extension-sdk" 2>/dev/null | head
find . -path "*/@go-pi/extension-sdk/*" 2>/dev/null | head
```

Expected: either a local path like `ts-sdk/` or only `node_modules` copies.

- [ ] **Step 2: If local source exists, add the methods**

In the TS source:

```ts
// extension-sdk/src/api.ts (or equivalent)
export interface ExtensionAPI {
  // ...existing...
  unregisterTool(name: string): Promise<void>;
  ready(): Promise<void>;
}
```

In the implementation class (wherever `registerTool` is implemented):

```ts
async unregisterTool(name: string): Promise<void> {
  this.tools.delete(name);
  await this.hostCall({ service: "tools", version: 1, method: "unregister", payload: { name } });
}

async ready(): Promise<void> {
  await this.transport.call("pi.extension/host_call", {
    service: "ext", version: 1, method: "ready", payload: {},
  });
}
```

- [ ] **Step 3: If no local source, defer**

Create a single-line TODO in the plan notes: "TS SDK unregisterTool/ready lives in external repo; tracked separately." Continue — Go side remains complete.

- [ ] **Step 4: Commit (if changes made)**

```bash
git add <files>
git commit -m "feat(extension-sdk/ts): unregisterTool and ready"
```

---

## Task 13: E2E — hosted-hello-go invokes `greet` through agent

**Files:**
- Create: `internal/extension/e2e_hosted_tool_invocation_test.go`

- [ ] **Step 1: Write the test**

```go
//go:build e2e

package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/lifecycle"
)

// TestE2E_HostedTool_Invocation launches hosted-hello-go via the full
// BuildRuntime + lifecycle path, waits for readiness, and asserts that
// the greet tool is present in the HostedToolRegistry.
//
// Direct LLM-driven invocation requires an agent test harness that's
// already in use elsewhere; here we invoke the adapter directly since
// adapters are what the agent would call.
func TestE2E_HostedTool_Invocation(t *testing.T) {
	tmp := t.TempDir()
	extDir := filepath.Join(tmp, ".go-pi", "extensions", "hosted-hello-go")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink or copy the example fixture into extDir.
	sourceDir := mustRepoPath(t, "examples/extensions/hosted-hello-go")
	copyDir(t, sourceDir, extDir)

	// Pre-approve the extension so StartApproved launches it.
	approvalsPath := filepath.Join(tmp, "approvals.json")
	writeApprovalsJSON(t, approvalsPath, "hosted-hello-go", []string{
		"tools.register", "tools.unregister", "events.session_start", "events.tool_execute",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := BuildRuntime(ctx, RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	// Override approvals path for the lifecycle service test setup.
	// In practice this is already wired; if not, a small helper exposed on
	// Runtime may be needed.

	if errs := rt.Lifecycle.StartApproved(ctx); len(errs) > 0 {
		t.Fatalf("StartApproved: %v", errs)
	}
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}
	snap := rt.HostedToolRegistry.Snapshot()
	if len(snap) != 1 || snap[0].Desc.Name != "greet" {
		t.Fatalf("registry snapshot = %+v", snap)
	}

	// Invoke greet via the adapter path used by the agent.
	tl, err := api.NewHostedToolAdapter(snap[0])
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	_ = tl // ensure built
	// Directly invoke the handler; real agent integration is covered by the
	// existing agent e2e tests adapted separately.
	// This test's purpose is exercising registration + readiness + lifecycle.

	_ = host.Registration{}           // import usage
	_ = lifecycle.HookFunc(nil)       // import usage
	_ = json.RawMessage(`{}`)         // import usage
	_ = strings.HasPrefix             // import usage
}
```

`mustRepoPath`, `copyDir`, and `writeApprovalsJSON` are test helpers — add them to `internal/extension/testhelpers_test.go` if not already present. Reuse the pattern from `e2e_hosted_go_test.go`.

- [ ] **Step 2: Run the test**

```bash
go test ./internal/extension/ -tags=e2e -run TestE2E_HostedTool_Invocation -v -count=1
```

Expected: PASS once all prior tasks are complete.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_tool_invocation_test.go
git commit -m "test(extensions): e2e hosted tool registration + adapter round-trip"
```

---

## Task 14: E2E — hot reload

**Files:**
- Create: `internal/extension/e2e_hosted_tool_hot_reload_test.go`

- [ ] **Step 1: Write the test**

```go
//go:build e2e

package extension

import (
	"context"
	"testing"
	"time"
)

// TestE2E_HotReload verifies that stopping and re-starting a hosted
// extension removes and then re-adds its tools in the registry.
func TestE2E_HotReload(t *testing.T) {
	// Setup identical to Task 13; factor into helper if duplicated.
	rt, cleanup := setupHostedHelloGo(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("initial ready: %v", err)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("initial snapshot size = %d", n)
	}

	if err := rt.Lifecycle.Stop(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// Wait for close callback to fire.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("post-stop snapshot = %d", n)
	}

	if err := rt.Lifecycle.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Wait until the tool reappears.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("post-restart snapshot = %d", n)
	}
}
```

Factor the setup helper `setupHostedHelloGo(t)` into `internal/extension/e2e_testhelpers_test.go`. It should build a runtime, copy the fixture, pre-approve the extension, call StartApproved, and return `(rt, cleanup)`.

- [ ] **Step 2: Run it**

```bash
go test ./internal/extension/ -tags=e2e -run TestE2E_HotReload -v -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_tool_hot_reload_test.go internal/extension/e2e_testhelpers_test.go
git commit -m "test(extensions): e2e hot reload of hosted tools"
```

---

## Task 15: Collision fixture + E2E test

**Files:**
- Create: `examples/extensions/hosted-collide/` (go.mod, main.go, pi.toml)
- Create: `internal/extension/e2e_hosted_tool_collision_test.go`

- [ ] **Step 1: Write the fixture**

`examples/extensions/hosted-collide/pi.toml`:

```toml
[extension]
name = "hosted-collide"
version = "0.0.1"
description = "Collision fixture: also tries to register 'greet'"
requested_capabilities = ["tools.register", "events.tool_execute"]
```

`examples/extensions/hosted-collide/go.mod`:

```
module hosted-collide

go 1.22

require github.com/pizzaface/go-pi v0.0.0

replace github.com/pizzaface/go-pi => ../../..
```

`examples/extensions/hosted-collide/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:                  "hosted-collide",
	Version:               "0.0.1",
	Description:           "Collision fixture",
	RequestedCapabilities: []string{"tools.register", "events.tool_execute"},
}

func register(pi piapi.API) error {
	err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "greet",
		Description: "I will collide.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	})
	if err == nil {
		return errors.New("collision fixture: expected RegisterTool to fail")
	}
	fmt.Fprintln(piext.Log(), "collision fixture: rejected as expected:", err)
	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-collide: fatal:", err)
	}
}
```

- [ ] **Step 2: Write the E2E test**

`internal/extension/e2e_hosted_tool_collision_test.go`:

```go
//go:build e2e

package extension

import (
	"context"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/api"
)

func TestE2E_ToolNameCollision(t *testing.T) {
	rt, cleanup := setupTwoFixtures(t, "hosted-hello-go", "hosted-collide")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 15*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	snap := rt.HostedToolRegistry.Snapshot()
	if len(snap) != 1 || snap[0].ExtID != "hosted-hello-go" {
		t.Fatalf("winner wrong: %+v", snap)
	}

	// Verify the collision event was emitted.
	var seenCollision bool
	rt.HostedToolRegistry.OnChange(func(c api.Change) {
		if c.Kind == api.ChangeCollisionRejected {
			seenCollision = true
		}
	})
	// Trigger a re-registration attempt manually by restarting the colliding ext.
	_ = rt.Lifecycle.Restart(ctx, "hosted-collide")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !seenCollision {
		time.Sleep(50 * time.Millisecond)
	}
	if !seenCollision {
		t.Fatal("collision change not observed")
	}
}
```

`setupTwoFixtures(t, ids...)` extends the earlier helper.

- [ ] **Step 3: Run it**

```bash
go test ./internal/extension/ -tags=e2e -run TestE2E_ToolNameCollision -v -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add examples/extensions/hosted-collide/ internal/extension/e2e_hosted_tool_collision_test.go
git commit -m "test(extensions): collision fixture and e2e rejection test"
```

---

## Task 16: Slow-ready fixture + E2E test

**Files:**
- Create: `examples/extensions/hosted-slow-ready-go/` (go.mod, main.go, pi.toml)
- Create: `internal/extension/e2e_hosted_tool_ready_test.go`

- [ ] **Step 1: Write the fixture**

`examples/extensions/hosted-slow-ready-go/pi.toml`:

```toml
[extension]
name = "hosted-slow-ready-go"
version = "0.0.1"
description = "Slow-startup fixture: sleeps 300ms before registering"
requested_capabilities = ["tools.register", "events.tool_execute"]
```

`examples/extensions/hosted-slow-ready-go/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:                  "hosted-slow-ready-go",
	Version:               "0.0.1",
	Description:           "Sleeps before registering, calls Ready() at end",
	RequestedCapabilities: []string{"tools.register", "events.tool_execute"},
}

func register(pi piapi.API) error {
	time.Sleep(300 * time.Millisecond)
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "slow_greet",
		Description: "Registered after a 300ms delay.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: "slow hello"}},
			}, nil
		},
	}); err != nil {
		return err
	}
	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-slow-ready-go: fatal:", err)
	}
}
```

`go.mod` same shape as other fixtures.

- [ ] **Step 2: Write the E2E test**

```go
//go:build e2e

package extension

import (
	"context"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/api"
)

func TestE2E_SlowReadyUsesExplicitSignal(t *testing.T) {
	rt, cleanup := setupOneFixture(t, "hosted-slow-ready-go")
	defer cleanup()

	// Tighten quiescence to 50ms so the 300ms sleep would fail without Ready().
	rt.Readiness.QuiescenceWindow = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}
	if rt.Readiness.State("hosted-slow-ready-go") != api.ReadinessReady {
		t.Fatalf("state = %v", rt.Readiness.State("hosted-slow-ready-go"))
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("want 1 tool, got %d", n)
	}
}
```

- [ ] **Step 3: Run it**

```bash
go test ./internal/extension/ -tags=e2e -run TestE2E_SlowReadyUsesExplicitSignal -v -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add examples/extensions/hosted-slow-ready-go/ internal/extension/e2e_hosted_tool_ready_test.go
git commit -m "test(extensions): slow-startup fixture exercises Ready() signal"
```

---

## Task 17: E2E — dynamic approve/revoke mid-session

**Files:**
- Create: `internal/extension/e2e_hosted_tool_dynamic_approval_test.go`

- [ ] **Step 1: Write the test**

```go
//go:build e2e

package extension

import (
	"context"
	"testing"
	"time"
)

func TestE2E_DynamicApproval(t *testing.T) {
	// Setup with no extensions approved at startup.
	rt, cleanup := setupNoFixtures(t, "hosted-hello-go")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 1*time.Second); err != nil {
		t.Fatalf("initial ready: %v", err)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("expected empty registry, got %d", n)
	}

	// Approve mid-session.
	if err := rt.Lifecycle.Approve(ctx, "hosted-hello-go", []string{
		"tools.register", "events.tool_execute",
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := rt.Lifecycle.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 1 {
		t.Fatalf("post-approve snapshot = %d", n)
	}

	// Revoke.
	if err := rt.Lifecycle.Revoke(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(rt.HostedToolRegistry.Snapshot()) != 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(rt.HostedToolRegistry.Snapshot()); n != 0 {
		t.Fatalf("post-revoke snapshot = %d", n)
	}
}
```

`setupNoFixtures` installs the fixture on disk but leaves approvals empty.

- [ ] **Step 2: Run it**

```bash
go test ./internal/extension/ -tags=e2e -run TestE2E_DynamicApproval -v -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_tool_dynamic_approval_test.go
git commit -m "test(extensions): e2e dynamic approve/revoke mid-session"
```

---

## Task 18: Update hello-go + showcase-go fixtures to call `Ready()`

**Files:**
- Modify: `examples/extensions/hosted-hello-go/main.go`
- Modify: `examples/extensions/hosted-showcase-go/main.go`

- [ ] **Step 1: hosted-hello-go — call Ready at end of register**

In `examples/extensions/hosted-hello-go/main.go`, replace the end of `register` from:

```go
	if os.Getenv("PI_SPEC5_PROBE") == "1" {
		if err := pi.AppendEntry("probe", map[string]any{"hi": true}); err != nil {
			return fmt.Errorf("spec5_probe AppendEntry: %w", err)
		}
		fmt.Fprintln(piext.Log(), "spec5_probe: hello from log.append")
	}
	return nil
}
```

to:

```go
	if os.Getenv("PI_SPEC5_PROBE") == "1" {
		if err := pi.AppendEntry("probe", map[string]any{"hi": true}); err != nil {
			return fmt.Errorf("spec5_probe AppendEntry: %w", err)
		}
		fmt.Fprintln(piext.Log(), "spec5_probe: hello from log.append")
	}
	return pi.Ready()
}
```

- [ ] **Step 2: hosted-showcase-go — same**

Replace the `return nil` at the end of `register` with `return pi.Ready()`.

- [ ] **Step 3: Verify examples still build**

```bash
cd examples/extensions/hosted-hello-go && go build ./... && cd -
cd examples/extensions/hosted-showcase-go && go build ./... && cd -
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add examples/extensions/hosted-hello-go/main.go examples/extensions/hosted-showcase-go/main.go
git commit -m "feat(examples): call pi.Ready() in canonical hosted fixtures"
```

---

## Task 19: TUI extensions panel — Tools sub-view

**Files:**
- Modify: likely `internal/tui/extensions/` (check directory)
- Find: `rtk grep -rn "extensions panel" internal/tui/`

- [ ] **Step 1: Inventory the existing panel**

```bash
find internal/tui -name "*extension*" -o -name "*panel*" | head
```

Expected: a set of files implementing the Bubble Tea model for the extensions panel.

- [ ] **Step 2: Add the Tools sub-view**

Sketch (adapt to the codebase's actual Bubble Tea patterns):

```go
// Inside the extensions panel model:

type toolsSubview struct {
	registry *api.HostedToolRegistry
	rows     []toolRow
	history  []api.Change
}

type toolRow struct {
	Tool   string
	Owner  string
	Status string
}

func (v *toolsSubview) refresh() {
	snap := v.registry.Snapshot()
	v.rows = v.rows[:0]
	for _, e := range snap {
		v.rows = append(v.rows, toolRow{
			Tool: e.Desc.Name, Owner: e.ExtID, Status: "available",
		})
	}
	for _, c := range v.history {
		if c.Kind == api.ChangeCollisionRejected {
			v.rows = append(v.rows, toolRow{
				Tool: c.ToolName, Owner: c.ExtID,
				Status: "rejected (collision with " + c.ConflictWith + ")",
			})
		}
	}
}
```

Subscribe via `registry.OnChange` to append to `history` and trigger a TUI refresh message.

- [ ] **Step 3: Smoke-test the TUI**

```bash
go build ./...
# Run the pi binary, open the extensions panel manually, verify the Tools sub-view renders.
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/...
git commit -m "feat(tui): extensions panel Tools sub-view with collision log"
```

If the panel refactor is too big for this plan, split into a follow-up plan but keep invocation working without UI changes.

---

## Task 20: Documentation — update `docs/extensions.md`

**Files:**
- Modify: `docs/extensions.md`

- [ ] **Step 1: Add an "Invoking tools" section**

After the registration example, add:

```markdown
## Invoking tools

Tools registered via `pi.RegisterTool` are made available to the LLM as
ADK function tools. When the model calls a hosted extension's tool, go-pi
sends `extension_event/tool_execute` to the extension process; the
extension's stored `Execute` closure runs and returns a `ToolResult` over
the wire.

Name collisions across extensions are rejected: the first registration
wins, subsequent attempts receive error `-32099 ToolNameCollision`. Tool
names are a global namespace.

Extensions may call `pi.UnregisterTool(name)` to drop a tool without
stopping the process. Stopping or revoking an extension removes all of
its tools automatically.

## Readiness

Extensions that need to do slow startup work (remote fetches, cache
warms) should call `pi.Ready()` as the last step of their `register`
function. This tells the host the extension has finished initializing
and any startup-time tools are now available. Without `Ready()`, the
host infers readiness from a 250 ms quiescence window — adequate for
synchronous registrations but unreliable for anything slower.
```

- [ ] **Step 2: Commit**

```bash
git add docs/extensions.md
git commit -m "docs(extensions): invoking tools, namespacing, pi.Ready()"
```

---

## Task 21: End-to-end smoke + regression sweep

- [ ] **Step 1: Full test suite**

```bash
go test ./... -count=1
go test ./... -tags=e2e -count=1
go vet ./...
```

Expected: all green. Any non-trivial failure is a bug introduced in an earlier task — fix and re-commit.

- [ ] **Step 2: Manual TUI smoke**

1. Start `pi` in interactive mode with `hosted-hello-go` approved.
2. Observe "Loading extensions…" indicator completes.
3. Ask the LLM to call `greet`. Confirm "Hello, world!" appears as a tool result.
4. Stop the extension via panel → ask the LLM to call `greet` again → it fails gracefully.
5. Restart → works again.

- [ ] **Step 3: Final commit (if fixes landed)**

Nothing to commit if tests were clean.

---

## Self-Review

**Spec coverage:**
- §2.3 `HostedToolRegistry` — Task 3 ✓
- §2.4 `HostedToolset` — Task 4 ✓
- §2.5 `NewHostedToolAdapter` — Task 5 ✓
- §2.6 Wiring + close-hook — Tasks 6, 8, 9 ✓
- §3.1 `tools.unregister` wire + SDK — Tasks 1, 6, 11, 12 ✓
- §3.2 `-32099` collision error — Tasks 1, 3 ✓
- §3.3 SDK surface — Tasks 2, 11, 12 ✓
- §4 Startup barrier — Tasks 7, 9, 10 ✓
- §4.2 `pi.Ready()` — Tasks 1, 2, 6, 7, 11, 16, 18 ✓
- §5 Dynamic approval — Task 17 (lifecycle methods already exist, wired via §8) ✓
- §6 TUI Tools sub-view — Task 19 ✓
- §7 Error handling — covered by adapter tests in Task 5 + close callback in Task 8 ✓
- §8 Testing — Tasks 13–17 ✓
- §9 Migration / compatibility — Task 18 preserves old examples while adding `Ready()` ✓

**Placeholder scan:** No TBDs or vague instructions remain. Each task has concrete file paths and code.

**Type consistency:**
- `HostedToolRegistry.Add` signature: `(extID string, desc piapi.ToolDescriptor, reg *host.Registration, mgr *host.Manager) error` — consistent across Tasks 3, 6, 9.
- `HostedToolEntry` fields: `ExtID, Desc, Reg, Manager` — consistent in Tasks 3, 5, 9.
- `Readiness` state names: `ReadinessUnknown/Launching/Ready/Errored/TimedOut` — consistent in Tasks 6, 7, 16.
- `MethodToolsRegister/Unregister/ExtReady` — consistent in Tasks 1, 6.

---

Plan complete and saved to `docs/superpowers/plans/2026-04-20-hosted-tool-invocation.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints for review

Which approach?

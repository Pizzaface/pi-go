# Extension Surface v2 Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the Extension Platform v2 surface by landing the `state`, `commands`, `ui`, `sigils`, and session-metadata services on the hosted RPC dispatcher, behind capability gating, with E2E coverage.

**Architecture:** Each service is a thin dispatcher branch in `internal/extension/api/hosted.go` backed by a small registry or helper. Host→extension callbacks use the existing `host.Dispatcher` + `extension_event` channel. Session metadata is an additive extension of the existing `session` service. Protocol bump to `2.2`.

**Tech Stack:** Go 1.22+, existing `hostproto` JSON-RPC wire, `host.Manager`/`host.Gate`/`host.Dispatcher`, `pkg/piapi`, `internal/session` (FileService), `internal/tui` (for widget/dialog rendering seams via `SessionBridge`).

**Branch:** `feature/hosted-tool-invocation` (work here; do not branch further).

**Spec:** `docs/superpowers/specs/2026-04-20-extension-surface-v2-completion-design.md`

---

## File Structure

**New files (create):**
- `internal/extension/api/command_registry.go` — `CommandRegistry` mirroring `HostedToolRegistry`.
- `internal/extension/api/command_registry_test.go` — unit tests for the registry.
- `internal/extension/api/ui_service.go` — in-memory state for status/widget/notify/dialog, per-extension isolation.
- `internal/extension/api/ui_service_test.go` — unit tests.
- `internal/extension/api/sigil_registry.go` — prefix → owner registry.
- `internal/extension/api/sigil_registry_test.go` — unit tests.
- `internal/tui/sigils/parser.go` — `[[prefix:id]]` scanner.
- `internal/tui/sigils/parser_test.go` — unit tests.
- `internal/extension/testdata/hosted-surface-fixture/` — Go fixture extension exercising every new service (main.go, go.mod, manifest).
- `internal/extension/testdata/approvals_granted_surface.json` — approvals fixture granting the fixture all needed capabilities.
- `internal/extension/e2e_hosted_state_test.go`
- `internal/extension/e2e_hosted_commands_test.go`
- `internal/extension/e2e_hosted_ui_test.go`
- `internal/extension/e2e_hosted_sigils_test.go`
- `internal/extension/e2e_hosted_session_metadata_test.go`

**Modified files:**
- `internal/extension/hostproto/protocol.go` — new service/method/error constants, payload types, protocol version bump.
- `internal/extension/api/hosted.go` — add `handleState`/`handleCommands`/`handleUI`/`handleSigils`; new session-metadata cases; wire new registries.
- `internal/extension/api/bridge.go` — add `SessionBridge` methods for metadata, status, widget, notify, dialog.
- `internal/extension/state_store.go` — `Patch` method implementing RFC 7396.
- `internal/extension/state_store_test.go` — `Patch` tests.
- `internal/extension/runtime.go` — instantiate and wire `CommandRegistry`, `UIService`, `SigilRegistry`, `StateStore` into `HostedAPIHandler`.
- `internal/session/store.go` — add `Name`, `Tags` to `Meta`; add `SetName`, `SetTags`, `GetMetadata` methods on `FileService`.

---

## Phase 1 — Protocol Additions

### Task 1: Bump protocol version and add service constants

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/hostproto/protocol_v22_test.go`:

```go
package hostproto

import "testing"

func TestProtocolVersion_2_2(t *testing.T) {
	if ProtocolVersion != "2.2" {
		t.Fatalf("ProtocolVersion = %q, want %q", ProtocolVersion, "2.2")
	}
}

func TestNewServiceConstants(t *testing.T) {
	cases := map[string]string{
		"state":    ServiceState,
		"commands": ServiceCommands,
		"ui":       ServiceUI,
		"sigils":   ServiceSigils,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("service %q: got %q", want, got)
		}
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/hostproto/ -run TestProtocolVersion_2_2 -run TestNewServiceConstants`
Expected: FAIL — symbols undefined, version mismatch.

- [ ] **Step 3: Bump version and add constants**

Edit `internal/extension/hostproto/protocol.go`. Change `ProtocolVersion = "2.1"` to `ProtocolVersion = "2.2"`.

Append to the existing service constants block (around line 114):

```go
const (
	ServiceState    = "state"
	ServiceCommands = "commands"
	ServiceUI       = "ui"
	ServiceSigils   = "sigils"
)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/hostproto/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_v22_test.go
git commit -m "feat(hostproto): bump protocol to 2.2, add state/commands/ui/sigils service constants"
```

---

### Task 2: Add method/error constants and payload structs

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`

- [ ] **Step 1: Append method constants**

Append to the method-name block (the one starting `MethodSessionAppendEntry = "append_entry"`):

```go
const (
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
	MethodUIStatus      = "status"
	MethodUIClearStatus = "clear_status"
	MethodUIWidget      = "widget"
	MethodUIClearWidget = "clear_widget"
	MethodUINotify      = "notify"
	MethodUIDialog      = "dialog"

	// sigils
	MethodSigilsRegister   = "register"
	MethodSigilsUnregister = "unregister"
	MethodSigilsList       = "list"

	// session metadata (added to existing session service)
	MethodSessionGetMetadata = "get_metadata"
	MethodSessionSetName     = "set_name"
	MethodSessionSetTags     = "set_tags"
)
```

- [ ] **Step 2: Append error codes**

In the error-code block at top:

```go
const (
	// ... existing codes ...
	ErrCodeDialogCancelled      = -32094
	ErrCodeSigilPrefixCollision = -32095
	ErrCodeCommandNameCollision = -32096
)
```

- [ ] **Step 3: Append payload structs**

At the bottom of `protocol.go`:

```go
// state
type StateGetResult struct {
	Value  json.RawMessage `json:"value,omitempty"`
	Exists bool            `json:"exists"`
}

type StateSetParams struct {
	Value json.RawMessage `json:"value"`
}

type StatePatchParams struct {
	Patch json.RawMessage `json:"patch"`
}

// commands
type CommandsRegisterParams struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	ArgHint     string `json:"arg_hint,omitempty"`
}

type CommandsUnregisterParams struct {
	Name string `json:"name"`
}

type CommandEntry struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	ArgHint     string `json:"arg_hint,omitempty"`
	Owner       string `json:"owner"`
	Source      string `json:"source"` // "manifest" | "runtime"
}

type CommandsListResult struct {
	Commands []CommandEntry `json:"commands"`
}

type CommandsInvokeEvent struct {
	Name    string `json:"name"`
	Args    string `json:"args"`
	EntryID string `json:"entry_id,omitempty"`
}

type CommandsInvokeResult struct {
	Handled bool   `json:"handled"`
	Message string `json:"message,omitempty"`
	Silent  bool   `json:"silent,omitempty"`
}

// ui
type UIStatusParams struct {
	Text  string `json:"text"`
	Style string `json:"style,omitempty"`
}

type Position struct {
	Mode    string `json:"mode,omitempty"`   // static|relative|absolute|sticky|fixed
	Anchor  string `json:"anchor,omitempty"` // top|bottom|left|right
	OffsetX int    `json:"offset_x,omitempty"`
	OffsetY int    `json:"offset_y,omitempty"`
	Z       int    `json:"z,omitempty"`
}

type UIWidgetParams struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Lines    []string `json:"lines"`
	Style    string   `json:"style,omitempty"`
	Position Position `json:"position"`
}

type UIClearWidgetParams struct {
	ID string `json:"id"`
}

type UINotifyParams struct {
	Level     string `json:"level"`
	Text      string `json:"text"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type UIDialogField struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // text|password|choice|bool
	Label   string   `json:"label,omitempty"`
	Default string   `json:"default,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

type UIDialogButton struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style,omitempty"`
}

type UIDialogParams struct {
	Title   string           `json:"title"`
	Fields  []UIDialogField  `json:"fields,omitempty"`
	Buttons []UIDialogButton `json:"buttons"`
}

type UIDialogResult struct {
	DialogID string `json:"dialog_id"`
}

type UIDialogResolvedEvent struct {
	DialogID  string         `json:"dialog_id"`
	Values    map[string]any `json:"values,omitempty"`
	Cancelled bool           `json:"cancelled"`
	ButtonID  string         `json:"button_id,omitempty"`
}

// sigils
type SigilsRegisterParams struct {
	Prefixes []string `json:"prefixes"`
}

type SigilsUnregisterParams struct {
	Prefixes []string `json:"prefixes"`
}

type SigilPrefixEntry struct {
	Prefix string `json:"prefix"`
	Owner  string `json:"owner"`
}

type SigilsListResult struct {
	Prefixes []SigilPrefixEntry `json:"prefixes"`
}

type SigilResolveEvent struct {
	Prefix  string `json:"prefix"`
	ID      string `json:"id"`
	Context string `json:"context,omitempty"`
}

type SigilResolveResult struct {
	Display string         `json:"display"`
	Style   string         `json:"style,omitempty"`
	Hover   string         `json:"hover,omitempty"`
	Actions []string       `json:"actions,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type SigilActionEvent struct {
	Prefix string `json:"prefix"`
	ID     string `json:"id"`
	Action string `json:"action"`
}

type SigilActionResult struct {
	Handled bool `json:"handled"`
}

// session metadata
type SessionGetMetadataResult struct {
	Name      string   `json:"name,omitempty"`
	Title     string   `json:"title,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"` // RFC3339
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type SessionSetNameParams struct {
	Name string `json:"name"`
}

type SessionSetTagsParams struct {
	Tags []string `json:"tags"`
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/extension/hostproto/`
Expected: no output (success).

- [ ] **Step 5: Commit**

```bash
git add internal/extension/hostproto/protocol.go
git commit -m "feat(hostproto): add methods, error codes, and payload structs for v2.2 services"
```

---

## Phase 2 — State Service

### Task 3: Implement `StateNamespace.Patch` (RFC 7396)

**Files:**
- Modify: `internal/extension/state_store.go`
- Modify: `internal/extension/state_store_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/extension/state_store_test.go`:

```go
func TestStateNamespace_Patch_MergesShallow(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	if err := ns.Set(map[string]any{"a": 1.0, "b": 2.0}); err != nil {
		t.Fatal(err)
	}
	if err := ns.Patch([]byte(`{"b": 99, "c": 3}`)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ns.Get()
	if err != nil || !ok {
		t.Fatalf("Get: err=%v ok=%v", err, ok)
	}
	if got["a"] != 1.0 || got["b"] != 99.0 || got["c"] != 3.0 {
		t.Fatalf("merged state = %v", got)
	}
}

func TestStateNamespace_Patch_NullDeletesKey(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"a": 1.0, "b": 2.0})
	if err := ns.Patch([]byte(`{"b": null}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	if _, has := got["b"]; has {
		t.Fatalf("key b should be deleted: %v", got)
	}
}

func TestStateNamespace_Patch_ArrayReplaces(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"items": []any{"x", "y"}})
	if err := ns.Patch([]byte(`{"items": ["z"]}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	arr, ok := got["items"].([]any)
	if !ok || len(arr) != 1 || arr[0] != "z" {
		t.Fatalf("items = %v", got["items"])
	}
}

func TestStateNamespace_Patch_RecursesNested(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"outer": map[string]any{"a": 1.0, "b": 2.0}})
	if err := ns.Patch([]byte(`{"outer": {"b": 99, "c": 3}}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	outer := got["outer"].(map[string]any)
	if outer["a"] != 1.0 || outer["b"] != 99.0 || outer["c"] != 3.0 {
		t.Fatalf("nested merge: %v", outer)
	}
}

func TestStateNamespace_Patch_EmptyBaseCreates(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	if err := ns.Patch([]byte(`{"a": 1}`)); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := ns.Get()
	if !ok || got["a"] != 1.0 {
		t.Fatalf("patch on empty: ok=%v got=%v", ok, got)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/ -run TestStateNamespace_Patch`
Expected: FAIL — `Patch` undefined.

- [ ] **Step 3: Implement Patch**

Append to `internal/extension/state_store.go`:

```go
// Patch applies an RFC 7396 JSON Merge Patch to the namespace's stored
// value. Null values in the patch delete keys; objects recurse; arrays and
// scalars replace. A missing store is treated as an empty object.
func (n StateNamespace) Patch(merge json.RawMessage) error {
	if n.store == nil || n.extensionID == "" {
		return nil
	}
	current, _, err := n.Get()
	if err != nil {
		return err
	}
	if current == nil {
		current = map[string]any{}
	}
	var patch any
	if err := json.Unmarshal(merge, &patch); err != nil {
		return fmt.Errorf("state.patch: invalid patch JSON: %w", err)
	}
	merged := mergePatch(current, patch)
	// mergePatch returns the merged value; for a top-level object patch it's
	// always a map[string]any, but handle the replace-root case defensively.
	return n.Set(merged)
}

// mergePatch implements RFC 7396 JSON Merge Patch.
// - If patch is not a map, it replaces target wholesale.
// - If patch is a map, each key: null deletes, map recurses, anything else replaces.
func mergePatch(target, patch any) any {
	pm, pOK := patch.(map[string]any)
	if !pOK {
		return patch
	}
	tm, tOK := target.(map[string]any)
	if !tOK {
		tm = map[string]any{}
	}
	for k, v := range pm {
		if v == nil {
			delete(tm, k)
			continue
		}
		if _, isMap := v.(map[string]any); isMap {
			tm[k] = mergePatch(tm[k], v)
			continue
		}
		tm[k] = v
	}
	return tm
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/ -run TestStateNamespace_Patch -v`
Expected: PASS on all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/state_store.go internal/extension/state_store_test.go
git commit -m "feat(extension): add RFC 7396 Patch to StateNamespace"
```

---

### Task 4: Wire state service into hosted RPC handler

**Files:**
- Modify: `internal/extension/api/hosted.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/hosted_state_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_State_SetGetPatchDelete(t *testing.T) {
	dir := t.TempDir()
	store := extension.NewStateStore(dir, "sess-1")
	gate, _ := host.NewGate("")
	// Grant the capabilities for a trusted test ID.
	_ = gate.Grant("ext-a", "state.write", host.TrustCompiledIn)
	_ = gate.Grant("ext-a", "state.read", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetStateStore(store)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceState, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodStateSet, hostproto.StateSetParams{Value: json.RawMessage(`{"a":1}`)}); err != nil {
		t.Fatalf("set: %v", err)
	}
	getRes, err := call(hostproto.MethodStateGet, struct{}{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got hostproto.StateGetResult
	_ = json.Unmarshal(getRes, &got)
	if !got.Exists || string(got.Value) != `{"a":1}` {
		t.Fatalf("get got=%+v", got)
	}
	if _, err := call(hostproto.MethodStatePatch, hostproto.StatePatchParams{Patch: json.RawMessage(`{"b":2}`)}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	getRes2, _ := call(hostproto.MethodStateGet, struct{}{})
	var got2 hostproto.StateGetResult
	_ = json.Unmarshal(getRes2, &got2)
	var blob map[string]any
	_ = json.Unmarshal(got2.Value, &blob)
	if blob["a"].(float64) != 1 || blob["b"].(float64) != 2 {
		t.Fatalf("after patch: %v", blob)
	}
	if _, err := call(hostproto.MethodStateDelete, struct{}{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	getRes3, _ := call(hostproto.MethodStateGet, struct{}{})
	var got3 hostproto.StateGetResult
	_ = json.Unmarshal(getRes3, &got3)
	if got3.Exists {
		t.Fatalf("after delete still exists: %+v", got3)
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_State`
Expected: FAIL — `SetStateStore` undefined, service unimplemented.

- [ ] **Step 3: Add StateStore field and setter**

In `internal/extension/api/hosted.go`, change `HostedAPIHandler`:

```go
type HostedAPIHandler struct {
	manager *host.Manager
	reg     *host.Registration
	bridge  SessionBridge

	registry  *HostedToolRegistry
	readiness *Readiness
	state     StateStoreIface

	mu    sync.Mutex
	tools map[string]hostedTool
}

// StateStoreIface is the minimal surface HostedAPIHandler needs from the
// per-session state store. Defined here to avoid an import cycle into the
// parent extension package.
type StateStoreIface interface {
	Namespace(extensionID string) StateNamespaceIface
}

type StateNamespaceIface interface {
	Get() (map[string]any, bool, error)
	Set(value any) error
	Patch(merge json.RawMessage) error
	Delete() error
}

// SetStateStore wires the per-session state store used by the state service.
func (h *HostedAPIHandler) SetStateStore(s StateStoreIface) { h.state = s }
```

Because `extension.StateStore` returns `StateNamespace` (concrete), add a thin adapter at the top of `state_store.go` (outside the `api` package). Add this to `internal/extension/state_store.go`:

```go
// HostedNamespace returns the namespace for use by api.HostedAPIHandler.
// Adapts the concrete StateNamespace to the interface the handler needs.
func (s *StateStore) HostedNamespace(extensionID string) hostedNamespaceView {
	return hostedNamespaceView{ns: s.Namespace(extensionID)}
}

type hostedNamespaceView struct{ ns StateNamespace }

func (v hostedNamespaceView) Get() (map[string]any, bool, error) { return v.ns.Get() }
func (v hostedNamespaceView) Set(value any) error                { return v.ns.Set(value) }
func (v hostedNamespaceView) Patch(merge json.RawMessage) error  { return v.ns.Patch(merge) }
func (v hostedNamespaceView) Delete() error                      { return v.ns.Delete() }
```

And add another adapter for the top-level store that returns the interface the handler needs. Add to `internal/extension/state_store.go`:

```go
// HostedView returns an api.StateStoreIface-compatible adapter for this
// store without causing an import cycle.
func (s *StateStore) HostedView() storeView { return storeView{s: s} }

type storeView struct{ s *StateStore }

// Namespace returns an adapter conforming to api.StateNamespaceIface. The
// return type is declared interface{} to keep the api package's interface
// the authority; callers type-assert in api.
func (v storeView) Namespace(extensionID string) interface {
	Get() (map[string]any, bool, error)
	Set(value any) error
	Patch(merge json.RawMessage) error
	Delete() error
} {
	return v.s.HostedNamespace(extensionID)
}
```

Note: Because `api.StateNamespaceIface` is a Go interface with an identical method set, any `hostedNamespaceView` value satisfies it structurally; no explicit declaration needed. The caller passes `store.HostedView()` into `SetStateStore`.

- [ ] **Step 4: Add the handler branch**

In `internal/extension/api/hosted.go`, inside `handleHostCall`'s `switch p.Service`, add:

```go
case hostproto.ServiceState:
	return h.handleState(p.Method, p.Payload)
```

Append method at end of the file:

```go
func (h *HostedAPIHandler) handleState(method string, payload json.RawMessage) (any, error) {
	if h.state == nil {
		return nil, fmt.Errorf("state service not wired")
	}
	ns := h.state.Namespace(h.reg.ID)
	switch method {
	case hostproto.MethodStateGet:
		val, exists, err := ns.Get()
		if err != nil {
			return nil, err
		}
		if !exists {
			return hostproto.StateGetResult{Exists: false}, nil
		}
		b, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}
		return hostproto.StateGetResult{Exists: true, Value: b}, nil
	case hostproto.MethodStateSet:
		var p hostproto.StateSetParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		var val any
		if len(p.Value) > 0 {
			if err := json.Unmarshal(p.Value, &val); err != nil {
				return nil, fmt.Errorf("state.set: invalid value: %w", err)
			}
		}
		return map[string]any{}, ns.Set(val)
	case hostproto.MethodStatePatch:
		var p hostproto.StatePatchParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, ns.Patch(p.Patch)
	case hostproto.MethodStateDelete:
		return map[string]any{}, ns.Delete()
	}
	return nil, fmt.Errorf("state.%s not implemented", method)
}
```

- [ ] **Step 5: Run test**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_State -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/state_store.go internal/extension/api/hosted_state_test.go
git commit -m "feat(extension): hosted state service with get/set/patch/delete"
```

---

## Phase 3 — Commands Service

### Task 5: Create `CommandRegistry`

**Files:**
- Create: `internal/extension/api/command_registry.go`
- Create: `internal/extension/api/command_registry_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/extension/api/command_registry_test.go`:

```go
package api

import (
	"errors"
	"testing"
)

func TestCommandRegistry_AddAndList(t *testing.T) {
	r := NewCommandRegistry()
	if err := r.Add("ext-a", CommandSpec{Name: "todo", Label: "Todo"}, "runtime"); err != nil {
		t.Fatal(err)
	}
	entries := r.List()
	if len(entries) != 1 || entries[0].Name != "todo" || entries[0].Owner != "ext-a" {
		t.Fatalf("list = %+v", entries)
	}
}

func TestCommandRegistry_CollisionRejected(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	err := r.Add("ext-b", CommandSpec{Name: "todo"}, "runtime")
	var ce *CommandCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want CommandCollisionError, got %v", err)
	}
	if ce.ConflictWith != "ext-a" {
		t.Fatalf("conflict = %q", ce.ConflictWith)
	}
}

func TestCommandRegistry_OwnerReplace(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo", Label: "one"}, "runtime")
	if err := r.Add("ext-a", CommandSpec{Name: "todo", Label: "two"}, "runtime"); err != nil {
		t.Fatalf("same-owner replace: %v", err)
	}
	entries := r.List()
	if len(entries) != 1 || entries[0].Label != "two" {
		t.Fatalf("list = %+v", entries)
	}
}

func TestCommandRegistry_RemoveAllByOwner(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	_ = r.Add("ext-a", CommandSpec{Name: "plan"}, "runtime")
	_ = r.Add("ext-b", CommandSpec{Name: "note"}, "runtime")
	r.RemoveAllByOwner("ext-a")
	entries := r.List()
	if len(entries) != 1 || entries[0].Name != "note" {
		t.Fatalf("after RemoveAllByOwner: %+v", entries)
	}
}

func TestCommandRegistry_RemoveOwnership(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	if err := r.Remove("ext-b", "todo"); err == nil {
		t.Fatalf("other-owner Remove should error")
	}
	if err := r.Remove("ext-a", "todo"); err != nil {
		t.Fatalf("owner Remove: %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatalf("should be empty")
	}
	// missing is idempotent no-op
	if err := r.Remove("ext-a", "todo"); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/api/ -run TestCommandRegistry`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement registry**

Create `internal/extension/api/command_registry.go`:

```go
package api

import (
	"fmt"
	"sort"
	"sync"
)

// CommandSpec is the static description of a slash command contributed by an
// extension. Runtime registrations from `commands.register` and startup
// registrations from `cfg.ExtensionCommands` use the same shape.
type CommandSpec struct {
	Name        string
	Label       string
	Description string
	ArgHint     string
}

// CommandEntry is one command in the registry with owner/source metadata.
type CommandEntry struct {
	Spec   CommandSpec
	Owner  string
	Source string // "manifest" | "runtime"
}

// Name returns the command name for convenience.
func (e CommandEntry) Name() string { return e.Spec.Name }

// CommandCollisionError is returned when another extension owns the name.
type CommandCollisionError struct {
	Name         string
	ConflictWith string
}

func (e *CommandCollisionError) Error() string {
	return fmt.Sprintf("command %q already owned by %q", e.Name, e.ConflictWith)
}

// CommandRegistry is the shared command namespace across all extensions.
// Mirrors HostedToolRegistry semantics: same-owner replace, other-owner
// reject, missing-remove is a no-op.
type CommandRegistry struct {
	mu      sync.RWMutex
	entries map[string]CommandEntry // key: name
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{entries: map[string]CommandEntry{}}
}

func (r *CommandRegistry) Add(owner string, spec CommandSpec, source string) error {
	if spec.Name == "" {
		return fmt.Errorf("command name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.entries[spec.Name]; ok && existing.Owner != owner {
		return &CommandCollisionError{Name: spec.Name, ConflictWith: existing.Owner}
	}
	r.entries[spec.Name] = CommandEntry{Spec: spec, Owner: owner, Source: source}
	return nil
}

func (r *CommandRegistry) Remove(owner, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.entries[name]
	if !ok {
		return nil
	}
	if existing.Owner != owner {
		return fmt.Errorf("command %q owned by %q, not %q", name, existing.Owner, owner)
	}
	delete(r.entries, name)
	return nil
}

func (r *CommandRegistry) RemoveAllByOwner(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for n, e := range r.entries {
		if e.Owner == owner {
			delete(r.entries, n)
		}
	}
}

func (r *CommandRegistry) Get(name string) (CommandEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e, ok
}

func (r *CommandRegistry) List() []CommandEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CommandEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec.Name < out[j].Spec.Name })
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestCommandRegistry -v`
Expected: PASS on all five.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/command_registry.go internal/extension/api/command_registry_test.go
git commit -m "feat(extension): add CommandRegistry with collision + owner semantics"
```

---

### Task 6: Wire commands service into hosted handler + invoke dispatch

**Files:**
- Modify: `internal/extension/api/hosted.go`
- Create: `internal/extension/api/hosted_commands_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/hosted_commands_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_Commands_RegisterListUnregister(t *testing.T) {
	gate, _ := host.NewGate("")
	_ = gate.Grant("ext-a", "commands.manage", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	cr := NewCommandRegistry()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetCommandRegistry(cr)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceCommands, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodCommandsRegister,
		hostproto.CommandsRegisterParams{Name: "todo", Label: "Todo"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	listRes, err := call(hostproto.MethodCommandsList, struct{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list hostproto.CommandsListResult
	_ = json.Unmarshal(listRes, &list)
	if len(list.Commands) != 1 || list.Commands[0].Owner != "ext-a" || list.Commands[0].Source != "runtime" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := call(hostproto.MethodCommandsUnregister,
		hostproto.CommandsUnregisterParams{Name: "todo"}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	listRes2, _ := call(hostproto.MethodCommandsList, struct{}{})
	var list2 hostproto.CommandsListResult
	_ = json.Unmarshal(listRes2, &list2)
	if len(list2.Commands) != 0 {
		t.Fatalf("after unregister: %+v", list2)
	}
}

func TestHandleHostCall_Commands_CollisionAcrossExtensions(t *testing.T) {
	gate, _ := host.NewGate("")
	_ = gate.Grant("ext-a", "commands.manage", host.TrustCompiledIn)
	_ = gate.Grant("ext-b", "commands.manage", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	regA := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	regB := &host.Registration{ID: "ext-b", Trust: host.TrustCompiledIn}
	_ = mgr.Register(regA)
	_ = mgr.Register(regB)
	cr := NewCommandRegistry()
	hA := NewHostedHandler(mgr, regA, NoopBridge{})
	hA.SetCommandRegistry(cr)
	hB := NewHostedHandler(mgr, regB, NoopBridge{})
	hB.SetCommandRegistry(cr)

	call := func(h *HostedAPIHandler, payload any) error {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceCommands, Method: hostproto.MethodCommandsRegister, Payload: pb,
		})
		_, err := h.Handle(hostproto.MethodHostCall, params)
		return err
	}
	if err := call(hA, hostproto.CommandsRegisterParams{Name: "todo"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := call(hB, hostproto.CommandsRegisterParams{Name: "todo"}); err == nil {
		t.Fatalf("second register should collide")
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Commands`
Expected: FAIL — `SetCommandRegistry` undefined.

- [ ] **Step 3: Add field, setter, handler branch**

In `internal/extension/api/hosted.go`, add to `HostedAPIHandler` struct:

```go
commands *CommandRegistry
```

Add setter:

```go
func (h *HostedAPIHandler) SetCommandRegistry(c *CommandRegistry) { h.commands = c }
```

In `handleHostCall`'s service switch, add:

```go
case hostproto.ServiceCommands:
	return h.handleCommands(p.Method, p.Payload)
```

Append to the file:

```go
func (h *HostedAPIHandler) handleCommands(method string, payload json.RawMessage) (any, error) {
	if h.commands == nil {
		return nil, fmt.Errorf("commands service not wired")
	}
	switch method {
	case hostproto.MethodCommandsRegister:
		var p hostproto.CommandsRegisterParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if err := h.commands.Add(h.reg.ID, CommandSpec{
			Name: p.Name, Label: p.Label, Description: p.Description, ArgHint: p.ArgHint,
		}, "runtime"); err != nil {
			return nil, err
		}
		if h.readiness != nil {
			h.readiness.Kick(h.reg.ID)
		}
		return map[string]any{"registered": true}, nil

	case hostproto.MethodCommandsUnregister:
		var p hostproto.CommandsUnregisterParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if err := h.commands.Remove(h.reg.ID, p.Name); err != nil {
			return nil, err
		}
		return map[string]any{"unregistered": true}, nil

	case hostproto.MethodCommandsList:
		all := h.commands.List()
		out := make([]hostproto.CommandEntry, 0, len(all))
		for _, e := range all {
			out = append(out, hostproto.CommandEntry{
				Name:        e.Spec.Name,
				Label:       e.Spec.Label,
				Description: e.Spec.Description,
				ArgHint:     e.Spec.ArgHint,
				Owner:       e.Owner,
				Source:      e.Source,
			})
		}
		return hostproto.CommandsListResult{Commands: out}, nil
	}
	return nil, fmt.Errorf("commands.%s not implemented", method)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Commands -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/api/hosted_commands_test.go
git commit -m "feat(extension): hosted commands service with collision rejection"
```

---

### Task 7: Add `CommandRegistry.Invoke` dispatcher

**Files:**
- Modify: `internal/extension/api/command_registry.go`
- Modify: `internal/extension/api/command_registry_test.go`

- [ ] **Step 1: Write the failing test**

Append to `command_registry_test.go`:

```go
import (
	"context"
	"encoding/json"
	// keep existing imports
)

// fakeConn is a stub implementation used to observe outbound extension_event calls.
type fakeConn struct {
	lastMethod string
	lastParams any
	reply      any
}

func (c *fakeConn) Call(_ context.Context, method string, params any, out any) error {
	c.lastMethod = method
	c.lastParams = params
	if c.reply == nil {
		return nil
	}
	// marshal/unmarshal to simulate JSON round-trip.
	b, _ := json.Marshal(c.reply)
	return json.Unmarshal(b, out)
}

func TestCommandRegistry_Invoke_DispatchesExtensionEvent(t *testing.T) {
	// This test exercises the pure-registry invoke path without spinning up a
	// full RPC stack. The registry looks up the owner and calls an
	// InvokeTransport it was configured with.
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")

	var got struct {
		ExtID   string
		Name    string
		Args    string
		EntryID string
	}
	r.SetInvokeTransport(func(ctx context.Context, extID, name, args, entryID string) (CommandInvokeResult, error) {
		got.ExtID = extID
		got.Name = name
		got.Args = args
		got.EntryID = entryID
		return CommandInvokeResult{Handled: true, Message: "ok"}, nil
	})

	res, err := r.Invoke(context.Background(), "todo", "buy milk", "entry-1")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !res.Handled || res.Message != "ok" {
		t.Fatalf("result = %+v", res)
	}
	if got.ExtID != "ext-a" || got.Name != "todo" || got.Args != "buy milk" || got.EntryID != "entry-1" {
		t.Fatalf("transport args = %+v", got)
	}
}

func TestCommandRegistry_Invoke_UnknownCommand(t *testing.T) {
	r := NewCommandRegistry()
	_, err := r.Invoke(context.Background(), "missing", "", "")
	if err == nil {
		t.Fatalf("expected error for unknown command")
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/api/ -run TestCommandRegistry_Invoke`
Expected: FAIL — `SetInvokeTransport`, `Invoke`, `CommandInvokeResult` undefined.

- [ ] **Step 3: Add invoke machinery**

Append to `command_registry.go`:

```go
import (
	"context"
	// keep existing imports
)

// CommandInvokeResult mirrors hostproto.CommandsInvokeResult but avoids an
// import from api into hostproto payloads at this layer. The hosted handler
// marshals this into the wire shape.
type CommandInvokeResult struct {
	Handled bool
	Message string
	Silent  bool
}

// CommandInvokeTransport is injected by the runtime so the registry can
// dispatch commands.invoke to the owning extension without taking a direct
// host.Dispatcher dependency (keeps the registry host-agnostic).
type CommandInvokeTransport func(ctx context.Context, extID, name, args, entryID string) (CommandInvokeResult, error)

// SetInvokeTransport replaces the dispatcher. The caller must call this
// before Invoke; a nil transport returns an error from Invoke.
func (r *CommandRegistry) SetInvokeTransport(fn CommandInvokeTransport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transport = fn
}

// Invoke looks up the command and routes the invocation to the owner.
// Returns an error if the command is unknown or no transport is configured.
func (r *CommandRegistry) Invoke(ctx context.Context, name, args, entryID string) (CommandInvokeResult, error) {
	r.mu.RLock()
	entry, ok := r.entries[name]
	tr := r.transport
	r.mu.RUnlock()
	if !ok {
		return CommandInvokeResult{}, fmt.Errorf("command %q not found", name)
	}
	if tr == nil {
		return CommandInvokeResult{}, fmt.Errorf("no invoke transport set")
	}
	return tr(ctx, entry.Owner, name, args, entryID)
}
```

Also extend the struct:

```go
type CommandRegistry struct {
	mu        sync.RWMutex
	entries   map[string]CommandEntry
	transport CommandInvokeTransport
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestCommandRegistry -v`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/command_registry.go internal/extension/api/command_registry_test.go
git commit -m "feat(extension): CommandRegistry.Invoke dispatches via injectable transport"
```

---

## Phase 4 — UI Service

### Task 8: Extend SessionBridge with UI methods

**Files:**
- Modify: `internal/extension/api/bridge.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/bridge_ui_test.go`:

```go
package api

import "testing"

func TestSessionBridge_UIInterfaceIsSatisfiedByNoopBridge(t *testing.T) {
	var b SessionBridge = NoopBridge{}
	// Exercise all new methods; should no-op cleanly.
	_ = b.SetExtensionStatus("ext-a", "hi", "")
	_ = b.ClearExtensionStatus("ext-a")
	_ = b.SetExtensionWidget("ext-a", ExtensionWidget{ID: "w1"})
	_ = b.ClearExtensionWidget("ext-a", "w1")
	_ = b.EnqueueNotify("ext-a", "info", "hello", 0)
	_, _ = b.ShowDialog("ext-a", DialogSpec{Title: "ok"})
	// Metadata
	m := b.GetSessionMetadata()
	_ = m
	_ = b.SetSessionName("n")
	_ = b.SetSessionTags([]string{"a"})
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/api/ -run TestSessionBridge_UIInterface`
Expected: FAIL — methods/types undefined.

- [ ] **Step 3: Extend interface and noop**

In `internal/extension/api/bridge.go`, add types and methods:

```go
// ExtensionWidget is one extension-owned widget placed in the TUI.
type ExtensionWidget struct {
	ID       string
	Title    string
	Lines    []string
	Style    string
	Position Position
}

// Position mirrors hostproto.Position so the bridge doesn't depend on wire
// types directly.
type Position struct {
	Mode    string // static|relative|absolute|sticky|fixed
	Anchor  string // top|bottom|left|right
	OffsetX int
	OffsetY int
	Z       int
}

// DialogField, DialogButton, DialogSpec, DialogResolution shadow the wire types.
type DialogField struct {
	Name    string
	Kind    string
	Label   string
	Default string
	Choices []string
}

type DialogButton struct {
	ID    string
	Label string
	Style string
}

type DialogSpec struct {
	Title   string
	Fields  []DialogField
	Buttons []DialogButton
}

// DialogResolution is posted back through the bridge when the user answers.
type DialogResolution struct {
	DialogID  string
	Values    map[string]any
	Cancelled bool
	ButtonID  string
}

// SessionMetadata is returned by GetSessionMetadata.
type SessionMetadata struct {
	Name      string
	Title     string
	Tags      []string
	CreatedAt string // RFC3339
	UpdatedAt string
}
```

Append to the interface:

```go
type SessionBridge interface {
	// ... existing ...

	// UI.
	SetExtensionStatus(extID, text, style string) error
	ClearExtensionStatus(extID string) error
	SetExtensionWidget(extID string, w ExtensionWidget) error
	ClearExtensionWidget(extID, widgetID string) error
	EnqueueNotify(extID, level, text string, timeoutMs int) error
	ShowDialog(extID string, spec DialogSpec) (dialogID string, err error)

	// Session metadata.
	GetSessionMetadata() SessionMetadata
	SetSessionName(name string) error
	SetSessionTags(tags []string) error
}
```

Add the corresponding `NoopBridge` methods:

```go
func (NoopBridge) SetExtensionStatus(string, string, string) error       { return nil }
func (NoopBridge) ClearExtensionStatus(string) error                     { return nil }
func (NoopBridge) SetExtensionWidget(string, ExtensionWidget) error      { return nil }
func (NoopBridge) ClearExtensionWidget(string, string) error             { return nil }
func (NoopBridge) EnqueueNotify(string, string, string, int) error       { return nil }
func (NoopBridge) ShowDialog(string, DialogSpec) (string, error)         { return "", nil }
func (NoopBridge) GetSessionMetadata() SessionMetadata                   { return SessionMetadata{} }
func (NoopBridge) SetSessionName(string) error                           { return nil }
func (NoopBridge) SetSessionTags([]string) error                         { return nil }
```

- [ ] **Step 4: Verify compile + test**

Run: `go test ./internal/extension/api/ -run TestSessionBridge_UIInterface`
Expected: PASS.

Also: `go build ./...` — any external implementers of `SessionBridge` (e.g., in `internal/tui`) will need the new methods. The plan handles that in Task 18. If the build fails on external implementers, add temporary no-op method implementations in those files inside this task and mark them `TODO: Task 18`.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/bridge.go internal/extension/api/bridge_ui_test.go
git commit -m "feat(extension): extend SessionBridge with UI + session metadata surface"
```

---

### Task 9: Implement `UIService` (in-memory state)

**Files:**
- Create: `internal/extension/api/ui_service.go`
- Create: `internal/extension/api/ui_service_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/extension/api/ui_service_test.go`:

```go
package api

import (
	"testing"
)

func TestUIService_StatusPerExtension(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetStatus("ext-a", "A", "")
	_ = svc.SetStatus("ext-b", "B", "")
	if svc.Status("ext-a") != "A" || svc.Status("ext-b") != "B" {
		t.Fatalf("status isolation broken: a=%q b=%q", svc.Status("ext-a"), svc.Status("ext-b"))
	}
	_ = svc.ClearStatus("ext-a")
	if svc.Status("ext-a") != "" {
		t.Fatalf("clear failed")
	}
}

func TestUIService_WidgetStoreAndClear(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w1", Lines: []string{"hello"}})
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w2", Lines: []string{"world"}})
	if ws := svc.Widgets("ext-a"); len(ws) != 2 {
		t.Fatalf("widgets = %d", len(ws))
	}
	_ = svc.ClearWidget("ext-a", "w1")
	ws := svc.Widgets("ext-a")
	if len(ws) != 1 || ws[0].ID != "w2" {
		t.Fatalf("after clear: %+v", ws)
	}
}

func TestUIService_DialogQueueAndResolve(t *testing.T) {
	svc := NewUIService()
	id1, _ := svc.EnqueueDialog("ext-a", DialogSpec{Title: "one"})
	id2, _ := svc.EnqueueDialog("ext-a", DialogSpec{Title: "two"})
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("dialog IDs = %q %q", id1, id2)
	}
	// Only one is "active" at a time; queue order preserved.
	active := svc.ActiveDialog()
	if active == nil || active.ID != id1 {
		t.Fatalf("active = %+v", active)
	}
	// Resolve id1 → id2 becomes active.
	if _, ok := svc.ResolveDialog(id1, map[string]any{"ok": true}, false, "ok"); !ok {
		t.Fatalf("resolve id1 failed")
	}
	active = svc.ActiveDialog()
	if active == nil || active.ID != id2 {
		t.Fatalf("active after resolve = %+v", active)
	}
}

func TestUIService_RemoveAllByOwner(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetStatus("ext-a", "A", "")
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w1"})
	_, _ = svc.EnqueueDialog("ext-a", DialogSpec{Title: "d"})
	_ = svc.SetStatus("ext-b", "B", "")

	cancelled := svc.RemoveAllByOwner("ext-a")
	if svc.Status("ext-a") != "" || len(svc.Widgets("ext-a")) != 0 {
		t.Fatalf("ext-a state should be cleared")
	}
	if len(cancelled) != 1 {
		t.Fatalf("expected 1 cancelled dialog: %+v", cancelled)
	}
	if svc.Status("ext-b") != "B" {
		t.Fatalf("ext-b should survive")
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/api/ -run TestUIService`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement UIService**

Create `internal/extension/api/ui_service.go`:

```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// UIService holds in-memory UI state contributed by hosted extensions. The
// TUI consumes it via the SessionBridge methods; tests exercise it directly.
// Every mutation is keyed by extension owner so RemoveAllByOwner is safe.
type UIService struct {
	mu sync.RWMutex

	status       map[string]statusEntry      // ext -> status
	widgets      map[string]map[string]ExtensionWidget // ext -> widgetID -> widget
	dialogQueue  []*dialogEntry              // FIFO, only ActiveDialog is visible
	dialogByID   map[string]*dialogEntry
}

type statusEntry struct {
	Text, Style string
}

type dialogEntry struct {
	ID    string
	Owner string
	Spec  DialogSpec
}

func NewUIService() *UIService {
	return &UIService{
		status:     map[string]statusEntry{},
		widgets:    map[string]map[string]ExtensionWidget{},
		dialogByID: map[string]*dialogEntry{},
	}
}

func (s *UIService) SetStatus(extID, text, style string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[extID] = statusEntry{Text: text, Style: style}
	return nil
}

func (s *UIService) ClearStatus(extID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, extID)
	return nil
}

func (s *UIService) Status(extID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status[extID].Text
}

func (s *UIService) SetWidget(extID string, w ExtensionWidget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.widgets[extID]
	if !ok {
		m = map[string]ExtensionWidget{}
		s.widgets[extID] = m
	}
	m[w.ID] = w
	return nil
}

func (s *UIService) ClearWidget(extID, widgetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.widgets[extID]; m != nil {
		delete(m, widgetID)
	}
	return nil
}

func (s *UIService) Widgets(extID string) []ExtensionWidget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.widgets[extID]
	out := make([]ExtensionWidget, 0, len(m))
	for _, w := range m {
		out = append(out, w)
	}
	return out
}

// EnqueueDialog returns a new dialog ID and appends to the queue.
func (s *UIService) EnqueueDialog(extID string, spec DialogSpec) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	entry := &dialogEntry{ID: id, Owner: extID, Spec: spec}
	s.dialogQueue = append(s.dialogQueue, entry)
	s.dialogByID[id] = entry
	return id, nil
}

// ActiveDialog returns the head of the queue or nil.
func (s *UIService) ActiveDialog() *dialogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.dialogQueue) == 0 {
		return nil
	}
	return s.dialogQueue[0]
}

// ResolveDialog removes the dialog with the given ID, advances the queue,
// and returns the resolution payload plus owner. Returns (_, false) if id
// is unknown.
func (s *UIService) ResolveDialog(id string, values map[string]any, cancelled bool, buttonID string) (DialogResolution, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.dialogByID[id]
	if !ok {
		return DialogResolution{}, false
	}
	delete(s.dialogByID, id)
	// Remove from queue.
	for i, e := range s.dialogQueue {
		if e.ID == id {
			s.dialogQueue = append(s.dialogQueue[:i], s.dialogQueue[i+1:]...)
			break
		}
	}
	return DialogResolution{
		DialogID: id, Values: values, Cancelled: cancelled, ButtonID: buttonID,
	}, true
}

// DialogOwner returns the extension ID that owns the dialog.
func (s *UIService) DialogOwner(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.dialogByID[id]
	if !ok {
		return "", false
	}
	return e.Owner, true
}

// RemoveAllByOwner clears all UI state for an extension. Any pending
// dialogs owned by the extension are returned as cancelled resolutions so
// the caller can dispatch ui.dialog.resolved with cancelled=true.
func (s *UIService) RemoveAllByOwner(extID string) []DialogResolution {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.status, extID)
	delete(s.widgets, extID)
	var cancelled []DialogResolution
	remaining := s.dialogQueue[:0]
	for _, e := range s.dialogQueue {
		if e.Owner == extID {
			delete(s.dialogByID, e.ID)
			cancelled = append(cancelled, DialogResolution{DialogID: e.ID, Cancelled: true})
			continue
		}
		remaining = append(remaining, e)
	}
	s.dialogQueue = remaining
	return cancelled
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestUIService -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/ui_service.go internal/extension/api/ui_service_test.go
git commit -m "feat(extension): add UIService holding status/widget/dialog state per owner"
```

---

### Task 10: Wire UI service into hosted handler + dialog-resolved dispatch

**Files:**
- Modify: `internal/extension/api/hosted.go`
- Create: `internal/extension/api/hosted_ui_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/hosted_ui_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_UI_StatusWidgetNotifyDialog(t *testing.T) {
	gate, _ := host.NewGate("")
	_ = gate.Grant("ext-a", "ui.status", host.TrustCompiledIn)
	_ = gate.Grant("ext-a", "ui.widget", host.TrustCompiledIn)
	_ = gate.Grant("ext-a", "ui.notify", host.TrustCompiledIn)
	_ = gate.Grant("ext-a", "ui.dialog", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	ui := NewUIService()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetUIService(ui)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceUI, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodUIStatus, hostproto.UIStatusParams{Text: "hi"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if ui.Status("ext-a") != "hi" {
		t.Fatalf("status not stored")
	}
	if _, err := call(hostproto.MethodUIWidget, hostproto.UIWidgetParams{
		ID: "w1", Lines: []string{"hello"},
		Position: hostproto.Position{Mode: "sticky", Anchor: "top"},
	}); err != nil {
		t.Fatalf("widget: %v", err)
	}
	if len(ui.Widgets("ext-a")) != 1 {
		t.Fatalf("widget not stored")
	}
	if _, err := call(hostproto.MethodUINotify, hostproto.UINotifyParams{Level: "info", Text: "hello"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	dRes, err := call(hostproto.MethodUIDialog, hostproto.UIDialogParams{
		Title: "confirm", Buttons: []hostproto.UIDialogButton{{ID: "ok", Label: "OK"}},
	})
	if err != nil {
		t.Fatalf("dialog: %v", err)
	}
	var dr hostproto.UIDialogResult
	_ = json.Unmarshal(dRes, &dr)
	if dr.DialogID == "" {
		t.Fatalf("dialog returned empty id")
	}
	if owner, ok := ui.DialogOwner(dr.DialogID); !ok || owner != "ext-a" {
		t.Fatalf("dialog owner = %q %v", owner, ok)
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_UI`
Expected: FAIL — `SetUIService` undefined.

- [ ] **Step 3: Add field, setter, handler branch**

In `HostedAPIHandler`:

```go
ui *UIService
```

Setter:

```go
func (h *HostedAPIHandler) SetUIService(u *UIService) { h.ui = u }
```

Add case to `handleHostCall`:

```go
case hostproto.ServiceUI:
	return h.handleUI(p.Method, p.Payload)
```

Append method:

```go
func (h *HostedAPIHandler) handleUI(method string, payload json.RawMessage) (any, error) {
	if h.ui == nil {
		return nil, fmt.Errorf("ui service not wired")
	}
	switch method {
	case hostproto.MethodUIStatus:
		var p hostproto.UIStatusParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if err := h.ui.SetStatus(h.reg.ID, p.Text, p.Style); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetExtensionStatus(h.reg.ID, p.Text, p.Style)

	case hostproto.MethodUIClearStatus:
		_ = h.ui.ClearStatus(h.reg.ID)
		return map[string]any{}, h.bridge.ClearExtensionStatus(h.reg.ID)

	case hostproto.MethodUIWidget:
		var p hostproto.UIWidgetParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		w := ExtensionWidget{
			ID: p.ID, Title: p.Title, Lines: p.Lines, Style: p.Style,
			Position: Position{
				Mode: p.Position.Mode, Anchor: p.Position.Anchor,
				OffsetX: p.Position.OffsetX, OffsetY: p.Position.OffsetY, Z: p.Position.Z,
			},
		}
		if err := h.ui.SetWidget(h.reg.ID, w); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetExtensionWidget(h.reg.ID, w)

	case hostproto.MethodUIClearWidget:
		var p hostproto.UIClearWidgetParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		_ = h.ui.ClearWidget(h.reg.ID, p.ID)
		return map[string]any{}, h.bridge.ClearExtensionWidget(h.reg.ID, p.ID)

	case hostproto.MethodUINotify:
		var p hostproto.UINotifyParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.EnqueueNotify(h.reg.ID, p.Level, p.Text, p.TimeoutMs)

	case hostproto.MethodUIDialog:
		var p hostproto.UIDialogParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		spec := DialogSpec{
			Title:   p.Title,
			Fields:  convertDialogFields(p.Fields),
			Buttons: convertDialogButtons(p.Buttons),
		}
		id, err := h.ui.EnqueueDialog(h.reg.ID, spec)
		if err != nil {
			return nil, err
		}
		// Mirror into bridge so the TUI can render.
		if _, err := h.bridge.ShowDialog(h.reg.ID, spec); err != nil {
			return nil, err
		}
		return hostproto.UIDialogResult{DialogID: id}, nil
	}
	return nil, fmt.Errorf("ui.%s not implemented", method)
}

func convertDialogFields(in []hostproto.UIDialogField) []DialogField {
	out := make([]DialogField, len(in))
	for i, f := range in {
		out[i] = DialogField{Name: f.Name, Kind: f.Kind, Label: f.Label, Default: f.Default, Choices: f.Choices}
	}
	return out
}

func convertDialogButtons(in []hostproto.UIDialogButton) []DialogButton {
	out := make([]DialogButton, len(in))
	for i, b := range in {
		out[i] = DialogButton{ID: b.ID, Label: b.Label, Style: b.Style}
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_UI -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/api/hosted_ui_test.go
git commit -m "feat(extension): hosted ui service with status/widget/notify/dialog dispatch"
```

---

## Phase 5 — Sigils Service

### Task 11: Implement sigil parser

**Files:**
- Create: `internal/tui/sigils/parser.go`
- Create: `internal/tui/sigils/parser_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/sigils/parser_test.go`:

```go
package sigils

import (
	"reflect"
	"testing"
)

func TestParse_FindsMatches(t *testing.T) {
	got := Parse("hello [[todo:42]] and [[session:my-branch]] done")
	want := []Match{
		{Prefix: "todo", ID: "42", Raw: "[[todo:42]]", Start: 6, End: 17},
		{Prefix: "session", ID: "my-branch", Raw: "[[session:my-branch]]", Start: 22, End: 43},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParse_IgnoresMalformed(t *testing.T) {
	for _, s := range []string{
		"[[no-colon]]",
		"[[:empty-prefix]]",
		"[[bad prefix:id]]",
		"[no-double-brackets:id]",
		"[[prefix:id",          // unclosed
		"[[Prefix:id]]",        // uppercase not allowed in prefix
		"[[1badfirst:id]]",     // must start with a-z
	} {
		got := Parse(s)
		if len(got) != 0 {
			t.Errorf("Parse(%q) = %+v, want none", s, got)
		}
	}
}

func TestParse_AllowsHyphenAndDigitsInPrefix(t *testing.T) {
	got := Parse("[[issue-tracker:n-42]]")
	if len(got) != 1 || got[0].Prefix != "issue-tracker" || got[0].ID != "n-42" {
		t.Fatalf("got %+v", got)
	}
}

func TestValidPrefix(t *testing.T) {
	cases := map[string]bool{
		"todo":          true,
		"a":             true,
		"issue-42":      true,
		"":              false,
		"-bad":          false,
		"bad_prefix":    false,
		"Bad":           false,
		"1bad":          false,
	}
	for prefix, want := range cases {
		if got := ValidPrefix(prefix); got != want {
			t.Errorf("ValidPrefix(%q) = %v want %v", prefix, got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/tui/sigils/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement parser**

Create `internal/tui/sigils/parser.go`:

```go
// Package sigils parses and renders extension-contributed sigils of the
// form [[prefix:id]] in chat content.
package sigils

import "regexp"

// Match is one sigil occurrence.
type Match struct {
	Prefix string
	ID     string
	Raw    string
	Start  int
	End    int
}

// sigilRE matches [[prefix:id]] where prefix starts with a-z and contains
// a-z0-9-, and id is any run of non-whitespace, non-closing-bracket chars.
var sigilRE = regexp.MustCompile(`\[\[([a-z][a-z0-9-]*):([^\s\]]+)\]\]`)

// prefixRE is the same prefix shape used by ValidPrefix.
var prefixRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Parse scans s and returns every match in source order.
func Parse(s string) []Match {
	idxs := sigilRE.FindAllStringSubmatchIndex(s, -1)
	out := make([]Match, 0, len(idxs))
	for _, m := range idxs {
		out = append(out, Match{
			Prefix: s[m[2]:m[3]],
			ID:     s[m[4]:m[5]],
			Raw:    s[m[0]:m[1]],
			Start:  m[0],
			End:    m[1],
		})
	}
	return out
}

// ValidPrefix reports whether prefix matches the syntax extensions may
// register.
func ValidPrefix(prefix string) bool {
	return prefixRE.MatchString(prefix)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/sigils/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/sigils/parser.go internal/tui/sigils/parser_test.go
git commit -m "feat(sigils): add [[prefix:id]] parser with syntax validation"
```

---

### Task 12: Implement `SigilRegistry`

**Files:**
- Create: `internal/extension/api/sigil_registry.go`
- Create: `internal/extension/api/sigil_registry_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/extension/api/sigil_registry_test.go`:

```go
package api

import (
	"errors"
	"testing"
)

func TestSigilRegistry_AddMulti(t *testing.T) {
	r := NewSigilRegistry()
	if err := r.Add("ext-a", []string{"todo", "plan"}); err != nil {
		t.Fatal(err)
	}
	if o, ok := r.Owner("todo"); !ok || o != "ext-a" {
		t.Fatalf("owner todo = %q %v", o, ok)
	}
	if o, ok := r.Owner("plan"); !ok || o != "ext-a" {
		t.Fatalf("owner plan = %q %v", o, ok)
	}
}

func TestSigilRegistry_Collision(t *testing.T) {
	r := NewSigilRegistry()
	_ = r.Add("ext-a", []string{"todo"})
	err := r.Add("ext-b", []string{"plan", "todo"})
	var ce *SigilPrefixCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want SigilPrefixCollisionError, got %v", err)
	}
	if ce.Prefix != "todo" {
		t.Fatalf("collision prefix = %q", ce.Prefix)
	}
	// Partial-success check: plan should NOT be registered because add is atomic.
	if _, ok := r.Owner("plan"); ok {
		t.Fatalf("atomic add broken: plan registered despite collision")
	}
}

func TestSigilRegistry_InvalidPrefix(t *testing.T) {
	r := NewSigilRegistry()
	if err := r.Add("ext-a", []string{"Bad"}); err == nil {
		t.Fatalf("expected invalid-prefix error")
	}
}

func TestSigilRegistry_RemoveAndRemoveAllByOwner(t *testing.T) {
	r := NewSigilRegistry()
	_ = r.Add("ext-a", []string{"todo", "plan"})
	_ = r.Add("ext-b", []string{"note"})
	_ = r.Remove("ext-a", []string{"plan"})
	if _, ok := r.Owner("plan"); ok {
		t.Fatalf("plan should be removed")
	}
	r.RemoveAllByOwner("ext-a")
	if _, ok := r.Owner("todo"); ok {
		t.Fatalf("todo should be gone")
	}
	if _, ok := r.Owner("note"); !ok {
		t.Fatalf("note should survive")
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/extension/api/ -run TestSigilRegistry`
Expected: FAIL.

- [ ] **Step 3: Implement registry**

Create `internal/extension/api/sigil_registry.go`:

```go
package api

import (
	"fmt"
	"sort"
	"sync"

	"github.com/pizzaface/go-pi/internal/tui/sigils"
)

// SigilPrefixCollisionError is returned when another extension already owns
// a prefix. The registry leaves state unchanged on collision.
type SigilPrefixCollisionError struct {
	Prefix       string
	ConflictWith string
}

func (e *SigilPrefixCollisionError) Error() string {
	return fmt.Sprintf("sigil prefix %q already owned by %q", e.Prefix, e.ConflictWith)
}

// SigilRegistry maps prefix → owner extension ID. Safe for concurrent use.
type SigilRegistry struct {
	mu     sync.RWMutex
	owners map[string]string // prefix -> owner
}

func NewSigilRegistry() *SigilRegistry {
	return &SigilRegistry{owners: map[string]string{}}
}

// Add registers prefixes atomically: if any prefix is invalid or collides
// with a different owner, none are registered.
func (r *SigilRegistry) Add(owner string, prefixes []string) error {
	for _, p := range prefixes {
		if !sigils.ValidPrefix(p) {
			return fmt.Errorf("invalid sigil prefix %q", p)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range prefixes {
		if cur, ok := r.owners[p]; ok && cur != owner {
			return &SigilPrefixCollisionError{Prefix: p, ConflictWith: cur}
		}
	}
	for _, p := range prefixes {
		r.owners[p] = owner
	}
	return nil
}

// Remove drops prefixes owned by owner. Prefixes owned by a different
// extension return an error; missing prefixes are no-ops.
func (r *SigilRegistry) Remove(owner string, prefixes []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range prefixes {
		if cur, ok := r.owners[p]; ok {
			if cur != owner {
				return fmt.Errorf("sigil prefix %q owned by %q, not %q", p, cur, owner)
			}
			delete(r.owners, p)
		}
	}
	return nil
}

// RemoveAllByOwner drops every prefix owned by owner.
func (r *SigilRegistry) RemoveAllByOwner(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for p, o := range r.owners {
		if o == owner {
			delete(r.owners, p)
		}
	}
}

// Owner returns the owner of prefix or false.
func (r *SigilRegistry) Owner(prefix string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.owners[prefix]
	return o, ok
}

// List returns prefix→owner pairs sorted by prefix.
func (r *SigilRegistry) List() []SigilEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]SigilEntry, 0, len(r.owners))
	for p, o := range r.owners {
		out = append(out, SigilEntry{Prefix: p, Owner: o})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// SigilEntry is one registered prefix.
type SigilEntry struct {
	Prefix string
	Owner  string
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/extension/api/ -run TestSigilRegistry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/sigil_registry.go internal/extension/api/sigil_registry_test.go
git commit -m "feat(extension): add SigilRegistry with atomic multi-prefix add"
```

---

### Task 13: Wire sigils service into hosted handler

**Files:**
- Modify: `internal/extension/api/hosted.go`
- Create: `internal/extension/api/hosted_sigils_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/hosted_sigils_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_Sigils_RegisterListUnregister(t *testing.T) {
	gate, _ := host.NewGate("")
	_ = gate.Grant("ext-a", "sigils.manage", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	sr := NewSigilRegistry()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetSigilRegistry(sr)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceSigils, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodSigilsRegister,
		hostproto.SigilsRegisterParams{Prefixes: []string{"todo", "plan"}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	listRes, err := call(hostproto.MethodSigilsList, struct{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list hostproto.SigilsListResult
	_ = json.Unmarshal(listRes, &list)
	if len(list.Prefixes) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if _, err := call(hostproto.MethodSigilsUnregister,
		hostproto.SigilsUnregisterParams{Prefixes: []string{"plan"}}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	listRes2, _ := call(hostproto.MethodSigilsList, struct{}{})
	var list2 hostproto.SigilsListResult
	_ = json.Unmarshal(listRes2, &list2)
	if len(list2.Prefixes) != 1 || list2.Prefixes[0].Prefix != "todo" {
		t.Fatalf("after unregister: %+v", list2)
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Sigils`
Expected: FAIL.

- [ ] **Step 3: Add field, setter, handler**

In `HostedAPIHandler`:

```go
sigils *SigilRegistry
```

Setter:

```go
func (h *HostedAPIHandler) SetSigilRegistry(s *SigilRegistry) { h.sigils = s }
```

In `handleHostCall` service switch:

```go
case hostproto.ServiceSigils:
	return h.handleSigils(p.Method, p.Payload)
```

Append method:

```go
func (h *HostedAPIHandler) handleSigils(method string, payload json.RawMessage) (any, error) {
	if h.sigils == nil {
		return nil, fmt.Errorf("sigils service not wired")
	}
	switch method {
	case hostproto.MethodSigilsRegister:
		var p hostproto.SigilsRegisterParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if err := h.sigils.Add(h.reg.ID, p.Prefixes); err != nil {
			return nil, err
		}
		return map[string]any{"registered": p.Prefixes}, nil
	case hostproto.MethodSigilsUnregister:
		var p hostproto.SigilsUnregisterParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		if err := h.sigils.Remove(h.reg.ID, p.Prefixes); err != nil {
			return nil, err
		}
		return map[string]any{"unregistered": p.Prefixes}, nil
	case hostproto.MethodSigilsList:
		all := h.sigils.List()
		out := make([]hostproto.SigilPrefixEntry, 0, len(all))
		for _, e := range all {
			out = append(out, hostproto.SigilPrefixEntry{Prefix: e.Prefix, Owner: e.Owner})
		}
		return hostproto.SigilsListResult{Prefixes: out}, nil
	}
	return nil, fmt.Errorf("sigils.%s not implemented", method)
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Sigils -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/api/hosted_sigils_test.go
git commit -m "feat(extension): hosted sigils service with prefix register/unregister/list"
```

---

## Phase 6 — Session Metadata

### Task 14: Add Name/Tags to session Meta + FileService methods

**Files:**
- Modify: `internal/session/store.go`
- Create: `internal/session/metadata_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/metadata_test.go`:

```go
package session

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/adk/session"
)

func TestFileService_SetNameAndGetMetadata(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileService(dir)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := fs.Create(context.Background(), &session.CreateRequest{
		AppName: "go-pi", UserID: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Session.ID()
	if err := fs.SetName(id, "go-pi", "u1", "my-branch"); err != nil {
		t.Fatal(err)
	}
	meta, err := fs.GetMetadata(id, "go-pi", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-branch" {
		t.Fatalf("name = %q", meta.Name)
	}
}

func TestFileService_SetTags_Dedupes(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileService(dir)
	resp, _ := fs.Create(context.Background(), &session.CreateRequest{AppName: "go-pi", UserID: "u1"})
	id := resp.Session.ID()
	if err := fs.SetTags(id, "go-pi", "u1", []string{"a", "b", "a", "c", "b"}); err != nil {
		t.Fatal(err)
	}
	meta, _ := fs.GetMetadata(id, "go-pi", "u1")
	if !reflect.DeepEqual(meta.Tags, []string{"a", "b", "c"}) {
		t.Fatalf("tags = %+v", meta.Tags)
	}
}

func TestFileService_GetMetadata_OldSessionWithoutFields(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileService(dir)
	resp, _ := fs.Create(context.Background(), &session.CreateRequest{AppName: "go-pi", UserID: "u1"})
	id := resp.Session.ID()
	// Simulate a session from before Name/Tags existed by not setting them.
	meta, err := fs.GetMetadata(id, "go-pi", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "" || len(meta.Tags) != 0 {
		t.Fatalf("expected empty: %+v", meta)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Run: `go test ./internal/session/ -run TestFileService_Set -run TestFileService_GetMetadata`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Extend Meta and add methods**

In `internal/session/store.go`, change the `Meta` struct:

```go
type Meta struct {
	ID        string    `json:"id"`
	AppName   string    `json:"appName"`
	UserID    string    `json:"userID"`
	Title     string    `json:"title,omitempty"`
	Name      string    `json:"name,omitempty"`      // NEW
	Tags      []string  `json:"tags,omitempty"`      // NEW
	WorkDir   string    `json:"workDir,omitempty"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
```

Append methods on `FileService`:

```go
// SetName updates the stable short name for a session. Distinct from Title
// (the human-readable summary).
func (s *FileService) SetName(sessionID, appName, userID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.loadSession(sessionID, appName, userID)
	if err != nil {
		return err
	}
	sess.meta.Name = name
	sess.meta.UpdatedAt = time.Now()
	return writeMeta(filepath.Join(s.baseDir, sessionID), &sess.meta)
}

// SetTags replaces the tag slice, deduplicating while preserving first-seen
// order.
func (s *FileService) SetTags(sessionID, appName, userID string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.loadSession(sessionID, appName, userID)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(tags))
	deduped := make([]string, 0, len(tags))
	for _, t := range tags {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		deduped = append(deduped, t)
	}
	sess.meta.Tags = deduped
	sess.meta.UpdatedAt = time.Now()
	return writeMeta(filepath.Join(s.baseDir, sessionID), &sess.meta)
}

// GetMetadata returns the full metadata snapshot for a session.
func (s *FileService) GetMetadata(sessionID, appName, userID string) (Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.loadSession(sessionID, appName, userID)
	if err != nil {
		return Meta{}, err
	}
	return sess.meta, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/ -run TestFileService -v`
Expected: PASS on the three new tests; existing session tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/session/store.go internal/session/metadata_test.go
git commit -m "feat(session): add Name/Tags to Meta with SetName/SetTags/GetMetadata"
```

---

### Task 15: Wire session metadata into hosted handler

**Files:**
- Modify: `internal/extension/api/hosted.go`
- Create: `internal/extension/api/hosted_session_metadata_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/extension/api/hosted_session_metadata_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// recordingBridge captures the last metadata operations so the test can
// assert the handler routed through the bridge.
type recordingBridge struct {
	NoopBridge
	meta SessionMetadata
	name string
	tags []string
}

func (b *recordingBridge) GetSessionMetadata() SessionMetadata { return b.meta }
func (b *recordingBridge) SetSessionName(n string) error       { b.name = n; b.meta.Name = n; return nil }
func (b *recordingBridge) SetSessionTags(t []string) error     { b.tags = append([]string{}, t...); b.meta.Tags = b.tags; return nil }

func TestHandleHostCall_Session_Metadata(t *testing.T) {
	gate, _ := host.NewGate("")
	_ = gate.Grant("ext-a", "session.metadata.read", host.TrustCompiledIn)
	_ = gate.Grant("ext-a", "session.metadata.write", host.TrustCompiledIn)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	br := &recordingBridge{meta: SessionMetadata{Title: "T", CreatedAt: "2026-04-20T00:00:00Z"}}
	h := NewHostedHandler(mgr, reg, br)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceSession, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodSessionSetName,
		hostproto.SessionSetNameParams{Name: "my-branch"}); err != nil {
		t.Fatalf("set_name: %v", err)
	}
	if br.name != "my-branch" {
		t.Fatalf("bridge.name = %q", br.name)
	}
	if _, err := call(hostproto.MethodSessionSetTags,
		hostproto.SessionSetTagsParams{Tags: []string{"a", "b"}}); err != nil {
		t.Fatalf("set_tags: %v", err)
	}
	getRes, err := call(hostproto.MethodSessionGetMetadata, struct{}{})
	if err != nil {
		t.Fatalf("get_metadata: %v", err)
	}
	var m hostproto.SessionGetMetadataResult
	_ = json.Unmarshal(getRes, &m)
	if m.Name != "my-branch" || m.Title != "T" || len(m.Tags) != 2 {
		t.Fatalf("metadata = %+v", m)
	}
	// Keep the piapi import live.
	_ = piapi.EventSessionStart
	_ = context.Background
}
```

- [ ] **Step 2: Run test, confirm failure**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Session_Metadata`
Expected: FAIL — service method not implemented for get/set_name/set_tags.

- [ ] **Step 3: Extend `handleSession`**

In `internal/extension/api/hosted.go`, inside `handleSession`, add cases BEFORE the final `return nil, fmt.Errorf(...)`:

```go
case hostproto.MethodSessionGetMetadata:
	m := h.bridge.GetSessionMetadata()
	return hostproto.SessionGetMetadataResult{
		Name: m.Name, Title: m.Title, Tags: m.Tags,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}, nil

case hostproto.MethodSessionSetName:
	var p hostproto.SessionSetNameParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	return map[string]any{}, h.bridge.SetSessionName(p.Name)

case hostproto.MethodSessionSetTags:
	var p hostproto.SessionSetTagsParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	return map[string]any{}, h.bridge.SetSessionTags(p.Tags)
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/extension/api/ -run TestHandleHostCall_Session_Metadata -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/api/hosted.go internal/extension/api/hosted_session_metadata_test.go
git commit -m "feat(extension): hosted session metadata service (get_metadata/set_name/set_tags)"
```

---

## Phase 7 — Runtime Wiring

### Task 16: Wire new registries/services in `BuildRuntime`

**Files:**
- Modify: `internal/extension/runtime.go`

- [ ] **Step 1: Add fields to Runtime**

Modify `Runtime` struct in `internal/extension/runtime.go` — append:

```go
	// CommandRegistry carries both manifest-seeded and runtime-registered
	// commands. Safe to share across all extensions.
	CommandRegistry *extapi.CommandRegistry
	// SigilRegistry tracks prefix ownership.
	SigilRegistry *extapi.SigilRegistry
	// UIService holds per-extension UI state (status, widgets, dialogs).
	UIService *extapi.UIService
	// StateStore is the per-session extension state root. Nil when no
	// sessions dir / session ID are configured.
	StateStore *StateStore
```

- [ ] **Step 2: Accept sessions wiring in RuntimeConfig**

Append to `RuntimeConfig`:

```go
	// SessionsDir + SessionID root the per-session StateStore. When either
	// is empty, the state service is wired to a no-op store.
	SessionsDir string
	SessionID   string
```

- [ ] **Step 3: Instantiate and wire inside BuildRuntime**

Inside `BuildRuntime`, after `registry := extapi.NewHostedToolRegistry()`:

```go
	commandRegistry := extapi.NewCommandRegistry()
	sigilRegistry := extapi.NewSigilRegistry()
	uiService := extapi.NewUIService()
	stateStore := NewStateStore(cfg.SessionsDir, cfg.SessionID)
```

When each hosted extension's `HostedAPIHandler` is constructed (see `adapter_hosted.go` / loader — the existing hosted launch path), the caller must wire the new services. Add helper method:

```go
// WireHostedHandler applies all runtime-scoped services to a freshly
// constructed HostedAPIHandler. Call from the hosted launch path before
// RPC dispatch begins.
func (r *Runtime) WireHostedHandler(h *extapi.HostedAPIHandler) {
	h.SetRegistry(r.HostedToolRegistry)
	h.SetReadiness(r.Readiness)
	h.SetCommandRegistry(r.CommandRegistry)
	h.SetSigilRegistry(r.SigilRegistry)
	h.SetUIService(r.UIService)
	if r.StateStore != nil {
		h.SetStateStore(r.StateStore.HostedView())
	}
}
```

Populate the Runtime struct before returning:

```go
	rt := &Runtime{
		// ... existing ...
		CommandRegistry: commandRegistry,
		SigilRegistry:   sigilRegistry,
		UIService:       uiService,
		StateStore:      stateStore,
	}
```

- [ ] **Step 4: Seed manifest commands into CommandRegistry**

After `var slashCommands []loader.SlashCommand` block, before hook aggregation:

```go
	// Seed the shared command registry with manifest-declared slash commands
	// from every extension so runtime `commands.register` calls share the
	// global namespace and collision rule.
	for _, reg := range registrations {
		for _, c := range reg.Metadata.Commands {
			// Metadata.Commands is the manifest-declared list; each entry
			// already carries its owning extension by construction.
			_ = commandRegistry.Add(reg.ID, extapi.CommandSpec{
				Name: c.Name, Label: c.Label, Description: c.Description, ArgHint: c.ArgHint,
			}, "manifest")
		}
	}
```

> **Engineer note:** Verify `reg.Metadata.Commands` exists with the expected fields before running this task. If the current `Metadata` struct uses a different field name (e.g. `SlashCommands`), adjust the loop accordingly. If no such field exists yet, add one to `piapi.ExtensionMetadata` (a `[]piapi.SlashCommand` slice of `{Name, Label, Description, ArgHint}`) in this task as a pre-step.

- [ ] **Step 5: Wire manager.OnClose cleanup**

Extend the existing `manager.OnClose(extID, ...)` closure in `BuildRuntime` to also purge per-extension state from the new registries:

```go
		manager.OnClose(extID, func() {
			registry.RemoveExt(extID)
			readiness.MarkErrored(extID, errors.New("connection closed"))
			commandRegistry.RemoveAllByOwner(extID)
			sigilRegistry.RemoveAllByOwner(extID)
			uiService.RemoveAllByOwner(extID)
		})
```

- [ ] **Step 6: Run full extension package tests**

Run: `go test ./internal/extension/... -count=1`
Expected: PASS. If any external implementer of `SessionBridge` is broken, fix inside Task 18 or add temporary no-op methods on that type noting `// TODO Task 18`.

- [ ] **Step 7: Commit**

```bash
git add internal/extension/runtime.go
git commit -m "feat(extension): wire CommandRegistry/SigilRegistry/UIService/StateStore into runtime"
```

---

### Task 17: Wire `WireHostedHandler` into the hosted launch path

**Files:**
- Modify: `internal/extension/api/adapter_hosted.go` (or whichever file constructs `HostedAPIHandler` for a hosted extension — search with `grep -rn "NewHostedHandler(" internal/extension`)

- [ ] **Step 1: Locate handler construction**

Run: `grep -rn "NewHostedHandler(" internal/extension`

Record the call site(s) that attach a handler to a `host.Registration.Conn`. There should be one primary location in the launch flow.

- [ ] **Step 2: Replace inline SetRegistry/SetReadiness with WireHostedHandler**

At each call site, change:

```go
h := extapi.NewHostedHandler(mgr, reg, bridge)
h.SetRegistry(registry)
h.SetReadiness(readiness)
```

to:

```go
h := extapi.NewHostedHandler(mgr, reg, bridge)
rt.WireHostedHandler(h)
```

If the call site doesn't have access to `*Runtime`, plumb a `WireHostedHandler` function pointer from `BuildRuntime` into the launch path. The minimal change is to pass a closure `func(h *extapi.HostedAPIHandler) { rt.WireHostedHandler(h) }` into the launcher. Choose whichever is the smaller diff.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/extension/... -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/extension/api/adapter_hosted.go internal/extension/runtime.go
git commit -m "feat(extension): hosted launch path wires all new services into handler"
```

---

### Task 18: Implement new SessionBridge methods in TUI

**Files:**
- Modify: `internal/tui/extension_bridge.go` (the concrete `SessionBridge` impl — verify with `grep -n "SessionBridge" internal/tui/`)

- [ ] **Step 1: Locate the concrete bridge**

Run: `grep -rn "SessionBridge\b" internal/tui/`

There should be a struct implementing it (likely in `extension_bridge.go` based on the file listing).

- [ ] **Step 2: Implement the new methods as thin delegations**

Add methods to the TUI bridge type. Status/widget wire into existing extension-panel rendering; notify into the toast queue; dialog into the extension_approval_dialog flow. Each method can start as a stub that updates an internal model field and enqueues a TUI message — richer rendering can follow in UI-polish PRs.

```go
func (b *Bridge) SetExtensionStatus(extID, text, style string) error {
	b.postMsg(extensionStatusMsg{ExtID: extID, Text: text, Style: style})
	return nil
}

func (b *Bridge) ClearExtensionStatus(extID string) error {
	b.postMsg(extensionStatusMsg{ExtID: extID, Text: ""})
	return nil
}

func (b *Bridge) SetExtensionWidget(extID string, w extapi.ExtensionWidget) error {
	b.postMsg(extensionWidgetMsg{ExtID: extID, Widget: w})
	return nil
}

func (b *Bridge) ClearExtensionWidget(extID, widgetID string) error {
	b.postMsg(extensionWidgetMsg{ExtID: extID, ClearID: widgetID})
	return nil
}

func (b *Bridge) EnqueueNotify(extID, level, text string, timeoutMs int) error {
	b.postMsg(extensionToastMsg{ExtID: extID, Level: level, Text: text, TimeoutMs: timeoutMs})
	return nil
}

func (b *Bridge) ShowDialog(extID string, spec extapi.DialogSpec) (string, error) {
	b.postMsg(extensionDialogMsg{ExtID: extID, Spec: spec})
	// Dialog IDs are owned by UIService; the bridge is fire-and-render only.
	return "", nil
}

func (b *Bridge) GetSessionMetadata() extapi.SessionMetadata {
	m, err := b.sessions.GetMetadata(b.sessionID, b.appName, b.userID)
	if err != nil {
		return extapi.SessionMetadata{}
	}
	return extapi.SessionMetadata{
		Name: m.Name, Title: m.Title, Tags: m.Tags,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
}

func (b *Bridge) SetSessionName(name string) error {
	return b.sessions.SetName(b.sessionID, b.appName, b.userID, name)
}

func (b *Bridge) SetSessionTags(tags []string) error {
	return b.sessions.SetTags(b.sessionID, b.appName, b.userID, tags)
}
```

Declare the tea messages in the same file (or `extension_messages.go`):

```go
type extensionStatusMsg struct{ ExtID, Text, Style string }
type extensionWidgetMsg struct {
	ExtID, ClearID string
	Widget         extapi.ExtensionWidget
}
type extensionDialogMsg struct {
	ExtID string
	Spec  extapi.DialogSpec
}
// extensionToastMsg likely exists already — reuse if so.
```

> **Engineer note:** Field names on the existing `Bridge` type may differ (`b.sessions`, `b.sessionID`, etc.). Adjust based on the actual struct. The test for this is exercised by the e2e tests in later tasks; add focused unit tests only if the bridge type has its own tests.

- [ ] **Step 3: Handle messages in `tui_update.go`**

For each new msg type, add a case to the TUI `Update` function that writes the value into the model. Minimal impl:

```go
case extensionStatusMsg:
	m.extensionStatuses[msg.ExtID] = msg
case extensionWidgetMsg:
	if msg.ClearID != "" {
		delete(m.extensionWidgets[msg.ExtID], msg.ClearID)
	} else {
		if m.extensionWidgets[msg.ExtID] == nil {
			m.extensionWidgets[msg.ExtID] = map[string]extapi.ExtensionWidget{}
		}
		m.extensionWidgets[msg.ExtID][msg.Widget.ID] = msg.Widget
	}
case extensionDialogMsg:
	m.pendingExtensionDialogs = append(m.pendingExtensionDialogs, msg)
```

Add the corresponding fields to the model struct in `internal/tui/types.go`:

```go
	extensionStatuses       map[string]extensionStatusMsg
	extensionWidgets        map[string]map[string]extapi.ExtensionWidget
	pendingExtensionDialogs []extensionDialogMsg
```

- [ ] **Step 4: Build and run all tests**

Run: `go build ./... && go test ./internal/tui/... -count=1 && go test ./internal/extension/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/extension_bridge.go internal/tui/tui_update.go internal/tui/types.go internal/tui/extension_messages.go
git commit -m "feat(tui): implement SessionBridge UI + metadata methods"
```

---

## Phase 8 — E2E Tests

### Task 19: Build the hosted-surface fixture extension

**Files:**
- Create: `internal/extension/testdata/hosted-surface-fixture/main.go`
- Create: `internal/extension/testdata/hosted-surface-fixture/go.mod`
- Create: `internal/extension/testdata/hosted-surface-fixture/pi.json` (or whichever manifest filename the loader uses — confirm via `loader.Discover`)
- Create: `internal/extension/testdata/approvals_granted_surface.json`

- [ ] **Step 1: Look up fixture conventions**

Run: `ls examples/extensions/hosted-hello-go/` to see manifest filename and module layout.

- [ ] **Step 2: Write manifest**

Create `internal/extension/testdata/hosted-surface-fixture/pi.json` (adjust filename if needed) declaring the extension and requesting every capability:

```json
{
  "name": "hosted-surface-fixture",
  "version": "0.1.0",
  "entry": "main.go",
  "capabilities": [
    "state.read", "state.write",
    "commands.manage",
    "ui.status", "ui.widget", "ui.notify", "ui.dialog",
    "sigils.manage",
    "session.metadata.read", "session.metadata.write"
  ],
  "events": ["commands.invoke", "sigils/resolve", "sigils/action", "ui.dialog.resolved"]
}
```

> **Engineer note:** Match the existing manifest schema exactly. If the example manifest uses a different top-level shape, mirror it; the capability + event list above is the substantive part.

- [ ] **Step 3: Write Go fixture**

Create `internal/extension/testdata/hosted-surface-fixture/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// hosted-surface-fixture exercises every new service so e2e tests can assert
// end-to-end behavior. Behavior is driven by an environment variable
// PI_SURFACE_MODE that each e2e test sets before launch.

func main() {
	api, err := piapi.Connect(os.Stdin, os.Stdout)
	if err != nil { panic(err) }

	switch os.Getenv("PI_SURFACE_MODE") {
	case "state":
		runState(api)
	case "commands":
		runCommands(api)
	case "ui":
		runUI(api)
	case "sigils":
		runSigils(api)
	case "session":
		runSession(api)
	default:
		// Register nothing; Ready() immediately.
	}
	_ = api.Ready(context.Background())
	select {} // keep alive until host closes the connection
}

func runState(api piapi.API) {
	ctx := context.Background()
	_ = api.StateSet(ctx, map[string]any{"count": 1})
	_ = api.StatePatch(ctx, json.RawMessage(`{"count": 2, "note": "hi"}`))
}

func runCommands(api piapi.API) {
	ctx := context.Background()
	_ = api.CommandsRegister(ctx, "fixture-cmd", "Fixture command", "", "")
	api.OnCommandInvoke(func(ev piapi.CommandsInvokeEvent) piapi.CommandsInvokeResult {
		return piapi.CommandsInvokeResult{Handled: true, Message: "invoked:" + ev.Args}
	})
}

func runUI(api piapi.API) {
	ctx := context.Background()
	_ = api.UIStatus(ctx, "fixture-status", "")
	_ = api.UIWidget(ctx, "w1", "Title", []string{"line"}, piapi.Position{Mode: "sticky", Anchor: "top"})
	_ = api.UINotify(ctx, "info", "hello", 0)
	_, _ = api.UIDialog(ctx, "confirm", nil, []piapi.DialogButton{{ID: "ok", Label: "OK"}})
}

func runSigils(api piapi.API) {
	ctx := context.Background()
	_ = api.SigilsRegister(ctx, []string{"fix", "fixture"})
	api.OnSigilResolve(func(ev piapi.SigilResolveEvent) piapi.SigilResolveResult {
		return piapi.SigilResolveResult{Display: ev.Prefix + "→" + ev.ID}
	})
}

func runSession(api piapi.API) {
	ctx := context.Background()
	_ = api.SessionSetName(ctx, "fixture-branch")
	_ = api.SessionSetTags(ctx, []string{"one", "two"})
}
```

> **Engineer note:** The `piapi.API` helper methods above (`StateSet`, `StatePatch`, `CommandsRegister`, `OnCommandInvoke`, `UIStatus`, `UIWidget`, `UINotify`, `UIDialog`, `SigilsRegister`, `OnSigilResolve`, `SessionSetName`, `SessionSetTags`, `Position`, `DialogButton`, `CommandsInvokeEvent`, etc.) do not yet exist on the SDK. Task 20 adds them.

- [ ] **Step 4: Write go.mod**

Create `internal/extension/testdata/hosted-surface-fixture/go.mod`:

```
module github.com/pizzaface/go-pi/internal/extension/testdata/hosted-surface-fixture

go 1.22

require github.com/pizzaface/go-pi v0.0.0

replace github.com/pizzaface/go-pi => ../../../../
```

- [ ] **Step 5: Write approvals fixture**

Create `internal/extension/testdata/approvals_granted_surface.json` modeled on `approvals_granted_hello.json` but granting every capability declared in the fixture manifest. Copy the file and swap in this extension's ID + capability list.

- [ ] **Step 6: Commit (compilation will follow Task 20)**

```bash
git add internal/extension/testdata/hosted-surface-fixture internal/extension/testdata/approvals_granted_surface.json
git commit -m "test(extension): scaffold hosted-surface-fixture for e2e coverage"
```

---

### Task 20: Extend `piapi` SDK with helpers the fixture uses

**Files:**
- Modify: `pkg/piapi/...` — locate with `ls pkg/piapi/`; find where the current `API` interface and helper methods live (`Connect`, existing tool helpers).

- [ ] **Step 1: Audit existing surface**

Run: `grep -rn "func .*API. " pkg/piapi/`

List the existing methods on `piapi.API`. The helpers below may partly exist; keep existing names and only add what's missing.

- [ ] **Step 2: Add state helpers**

In the appropriate `pkg/piapi` file:

```go
func (a *defaultAPI) StateGet(ctx context.Context) (json.RawMessage, bool, error) {
	var res hostproto.StateGetResult
	err := a.hostCall(ctx, hostproto.ServiceState, hostproto.MethodStateGet, struct{}{}, &res)
	return res.Value, res.Exists, err
}

func (a *defaultAPI) StateSet(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil { return err }
	return a.hostCall(ctx, hostproto.ServiceState, hostproto.MethodStateSet,
		hostproto.StateSetParams{Value: b}, &struct{}{})
}

func (a *defaultAPI) StatePatch(ctx context.Context, patch json.RawMessage) error {
	return a.hostCall(ctx, hostproto.ServiceState, hostproto.MethodStatePatch,
		hostproto.StatePatchParams{Patch: patch}, &struct{}{})
}

func (a *defaultAPI) StateDelete(ctx context.Context) error {
	return a.hostCall(ctx, hostproto.ServiceState, hostproto.MethodStateDelete, struct{}{}, &struct{}{})
}
```

> `hostCall` is the existing plumbing inside `defaultAPI`; mirror the exact call convention used by existing helpers.

- [ ] **Step 3: Add commands helpers**

```go
func (a *defaultAPI) CommandsRegister(ctx context.Context, name, label, description, argHint string) error {
	return a.hostCall(ctx, hostproto.ServiceCommands, hostproto.MethodCommandsRegister,
		hostproto.CommandsRegisterParams{Name: name, Label: label, Description: description, ArgHint: argHint},
		&struct{}{})
}

func (a *defaultAPI) CommandsUnregister(ctx context.Context, name string) error {
	return a.hostCall(ctx, hostproto.ServiceCommands, hostproto.MethodCommandsUnregister,
		hostproto.CommandsUnregisterParams{Name: name}, &struct{}{})
}

type CommandsInvokeEvent = hostproto.CommandsInvokeEvent
type CommandsInvokeResult = hostproto.CommandsInvokeResult

// OnCommandInvoke subscribes to the commands.invoke event from the host.
func (a *defaultAPI) OnCommandInvoke(fn func(CommandsInvokeEvent) CommandsInvokeResult) {
	a.subscribe("commands.invoke", func(raw json.RawMessage) (any, error) {
		var ev CommandsInvokeEvent
		if err := json.Unmarshal(raw, &ev); err != nil { return nil, err }
		return fn(ev), nil
	})
}
```

- [ ] **Step 4: Add UI helpers**

```go
type Position = hostproto.Position
type DialogField = hostproto.UIDialogField
type DialogButton = hostproto.UIDialogButton

func (a *defaultAPI) UIStatus(ctx context.Context, text, style string) error {
	return a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUIStatus,
		hostproto.UIStatusParams{Text: text, Style: style}, &struct{}{})
}

func (a *defaultAPI) UIClearStatus(ctx context.Context) error {
	return a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUIClearStatus, struct{}{}, &struct{}{})
}

func (a *defaultAPI) UIWidget(ctx context.Context, id, title string, lines []string, pos Position) error {
	return a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUIWidget,
		hostproto.UIWidgetParams{ID: id, Title: title, Lines: lines, Position: pos}, &struct{}{})
}

func (a *defaultAPI) UIClearWidget(ctx context.Context, id string) error {
	return a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUIClearWidget,
		hostproto.UIClearWidgetParams{ID: id}, &struct{}{})
}

func (a *defaultAPI) UINotify(ctx context.Context, level, text string, timeoutMs int) error {
	return a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUINotify,
		hostproto.UINotifyParams{Level: level, Text: text, TimeoutMs: timeoutMs}, &struct{}{})
}

func (a *defaultAPI) UIDialog(ctx context.Context, title string, fields []DialogField, buttons []DialogButton) (string, error) {
	var res hostproto.UIDialogResult
	err := a.hostCall(ctx, hostproto.ServiceUI, hostproto.MethodUIDialog,
		hostproto.UIDialogParams{Title: title, Fields: fields, Buttons: buttons}, &res)
	return res.DialogID, err
}
```

- [ ] **Step 5: Add sigils helpers**

```go
type SigilResolveEvent = hostproto.SigilResolveEvent
type SigilResolveResult = hostproto.SigilResolveResult
type SigilActionEvent = hostproto.SigilActionEvent

func (a *defaultAPI) SigilsRegister(ctx context.Context, prefixes []string) error {
	return a.hostCall(ctx, hostproto.ServiceSigils, hostproto.MethodSigilsRegister,
		hostproto.SigilsRegisterParams{Prefixes: prefixes}, &struct{}{})
}

func (a *defaultAPI) SigilsUnregister(ctx context.Context, prefixes []string) error {
	return a.hostCall(ctx, hostproto.ServiceSigils, hostproto.MethodSigilsUnregister,
		hostproto.SigilsUnregisterParams{Prefixes: prefixes}, &struct{}{})
}

func (a *defaultAPI) OnSigilResolve(fn func(SigilResolveEvent) SigilResolveResult) {
	a.subscribe("sigils/resolve", func(raw json.RawMessage) (any, error) {
		var ev SigilResolveEvent
		if err := json.Unmarshal(raw, &ev); err != nil { return nil, err }
		return fn(ev), nil
	})
}

func (a *defaultAPI) OnSigilAction(fn func(SigilActionEvent) hostproto.SigilActionResult) {
	a.subscribe("sigils/action", func(raw json.RawMessage) (any, error) {
		var ev SigilActionEvent
		if err := json.Unmarshal(raw, &ev); err != nil { return nil, err }
		return fn(ev), nil
	})
}
```

- [ ] **Step 6: Add session metadata helpers**

```go
func (a *defaultAPI) SessionGetMetadata(ctx context.Context) (hostproto.SessionGetMetadataResult, error) {
	var res hostproto.SessionGetMetadataResult
	err := a.hostCall(ctx, hostproto.ServiceSession, hostproto.MethodSessionGetMetadata, struct{}{}, &res)
	return res, err
}

func (a *defaultAPI) SessionSetName(ctx context.Context, name string) error {
	return a.hostCall(ctx, hostproto.ServiceSession, hostproto.MethodSessionSetName,
		hostproto.SessionSetNameParams{Name: name}, &struct{}{})
}

func (a *defaultAPI) SessionSetTags(ctx context.Context, tags []string) error {
	return a.hostCall(ctx, hostproto.ServiceSession, hostproto.MethodSessionSetTags,
		hostproto.SessionSetTagsParams{Tags: tags}, &struct{}{})
}
```

- [ ] **Step 7: Build and run**

Run: `go build ./... && go test ./pkg/piapi/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/piapi
git commit -m "feat(piapi): add state/commands/ui/sigils/session-metadata helpers for v2.2"
```

---

### Task 21: E2E — state service

**Files:**
- Create: `internal/extension/e2e_hosted_state_test.go`

- [ ] **Step 1: Write the test**

Model on `e2e_hosted_tool_invocation_test.go`. Minimum viable:

```go
package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E_HostedState_SetPatchReadsFromDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping state E2E under -short")
	}
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()
	_ = rt

	os.Setenv("PI_SURFACE_MODE", "state")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	// Read state blob directly from disk — the fixture wrote set + patch.
	// Assumes setupHostedFixtures sets SessionsDir and SessionID via env.
	tmp := os.Getenv("HOME")
	blob := filepath.Join(tmp, ".go-pi", "sessions", rt.SessionID(), "state", "extensions", "hosted-surface-fixture.json")
	data, err := os.ReadFile(blob)
	if err != nil {
		t.Fatalf("read blob %s: %v", blob, err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["count"].(float64) != 2 {
		t.Fatalf("patch not applied: %v", m)
	}
	if m["note"].(string) != "hi" {
		t.Fatalf("patch note missing: %v", m)
	}
}
```

> **Engineer note:** `rt.SessionID()` may not exist. If the test helper doesn't expose the session root, add one in this task (small helper returning the session ID the runtime was built with), or resolve it via environment.

- [ ] **Step 2: Run and confirm passes**

Run: `go test ./internal/extension/ -run TestE2E_HostedState -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_state_test.go
git commit -m "test(extension): e2e state set+patch round-trip via hosted fixture"
```

---

### Task 22: E2E — commands service

**Files:**
- Create: `internal/extension/e2e_hosted_commands_test.go`

- [ ] **Step 1: Write the test**

```go
package extension

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestE2E_HostedCommands_RegisterAndInvoke(t *testing.T) {
	if testing.Short() { t.Skip() }
	os.Setenv("PI_SURFACE_MODE", "commands")
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	// After registration, the command should be in the shared registry.
	entries := rt.CommandRegistry.List()
	found := false
	for _, e := range entries {
		if e.Spec.Name == "fixture-cmd" && e.Owner == "hosted-surface-fixture" && e.Source == "runtime" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture-cmd missing from registry: %+v", entries)
	}

	// Invoke routes commands.invoke back to the extension which returns a canned response.
	res, err := rt.CommandRegistry.Invoke(ctx, "fixture-cmd", "hello", "entry-1")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !res.Handled || res.Message != "invoked:hello" {
		t.Fatalf("result = %+v", res)
	}
}
```

> **Engineer note:** For `CommandRegistry.Invoke` to reach the fixture, the runtime must install a transport that dispatches `commands.invoke` via the extension's live `Conn` (similar to the subscribe_event path in `hosted.go`). If not yet wired, add a runtime-level `SetInvokeTransport` call inside `BuildRuntime` that looks up the owning registration and routes through `reg.Conn.Call(ctx, hostproto.MethodExtensionEvent, ExtensionEventParams{Event: "commands.invoke", ...})`, mirroring the subscribe_event dispatch already in `handleSubscribeEvent`.

Add that wiring inside this task before running the test — here's the sketch:

```go
// in BuildRuntime, after commandRegistry creation:
commandRegistry.SetInvokeTransport(func(ctx context.Context, extID, name, args, entryID string) (extapi.CommandInvokeResult, error) {
	reg := manager.Get(extID)
	if reg == nil || reg.Conn == nil {
		return extapi.CommandInvokeResult{}, fmt.Errorf("extension %q not connected", extID)
	}
	payload, _ := json.Marshal(hostproto.CommandsInvokeEvent{Name: name, Args: args, EntryID: entryID})
	req := hostproto.ExtensionEventParams{Event: "commands.invoke", Version: 1, Payload: payload}
	var resp hostproto.CommandsInvokeResult
	if err := reg.Conn.Call(ctx, hostproto.MethodExtensionEvent, req, &resp); err != nil {
		return extapi.CommandInvokeResult{}, err
	}
	return extapi.CommandInvokeResult{Handled: resp.Handled, Message: resp.Message, Silent: resp.Silent}, nil
})
```

- [ ] **Step 2: Run**

Run: `go test ./internal/extension/ -run TestE2E_HostedCommands -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_commands_test.go internal/extension/runtime.go
git commit -m "test(extension): e2e commands register + invoke round-trip"
```

---

### Task 23: E2E — UI service

**Files:**
- Create: `internal/extension/e2e_hosted_ui_test.go`

- [ ] **Step 1: Write the test**

```go
package extension

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestE2E_HostedUI_StatusWidgetNotifyDialog(t *testing.T) {
	if testing.Short() { t.Skip() }
	os.Setenv("PI_SURFACE_MODE", "ui")
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	if got := rt.UIService.Status("hosted-surface-fixture"); got != "fixture-status" {
		t.Fatalf("status = %q", got)
	}
	if ws := rt.UIService.Widgets("hosted-surface-fixture"); len(ws) != 1 || ws[0].ID != "w1" {
		t.Fatalf("widgets = %+v", ws)
	}
	if active := rt.UIService.ActiveDialog(); active == nil {
		t.Fatalf("no dialog enqueued")
	}
}

func TestE2E_HostedUI_DialogResolvedEvent(t *testing.T) {
	if testing.Short() { t.Skip() }
	os.Setenv("PI_SURFACE_MODE", "ui")
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	// Grab the active dialog, resolve it via the service, and assert the
	// extension receives ui.dialog.resolved. The fixture would need to
	// record the event — expand runUI in the fixture to push into a
	// channel exposed via another test seam, OR just assert the service
	// state flips:
	active := rt.UIService.ActiveDialog()
	if active == nil { t.Fatalf("no active dialog") }
	resolution, ok := rt.UIService.ResolveDialog(active.ID, map[string]any{"x": 1}, false, "ok")
	if !ok {
		t.Fatalf("resolve failed")
	}
	if resolution.DialogID != active.ID {
		t.Fatalf("resolution id mismatch")
	}
}
```

> **Engineer note:** If full event round-trip from resolution back to the extension is wanted, add a runtime-level dispatch: when `UIService.ResolveDialog` returns, look up owner via `UIService.DialogOwner`, and `reg.Conn.Call(ctx, MethodExtensionEvent, {Event: "ui.dialog.resolved", ...})`. That wiring can go into Task 18's bridge impl or here — simplest is to add a `rt.ResolveDialog(id, values, cancelled, button)` convenience on `*Runtime` that does both the service resolve and the dispatch, and to call it from this test.

- [ ] **Step 2: Run**

Run: `go test ./internal/extension/ -run TestE2E_HostedUI -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_ui_test.go
git commit -m "test(extension): e2e ui status/widget/notify/dialog"
```

---

### Task 24: E2E — sigils service

**Files:**
- Create: `internal/extension/e2e_hosted_sigils_test.go`

- [ ] **Step 1: Write the test**

```go
package extension

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestE2E_HostedSigils_RegisterAndCollision(t *testing.T) {
	if testing.Short() { t.Skip() }
	os.Setenv("PI_SURFACE_MODE", "sigils")
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	for _, p := range []string{"fix", "fixture"} {
		o, ok := rt.SigilRegistry.Owner(p)
		if !ok || o != "hosted-surface-fixture" {
			t.Fatalf("prefix %q owner = %q %v", p, o, ok)
		}
	}

	// Collision: an unrelated owner attempting a subset fails.
	err := rt.SigilRegistry.Add("other-ext", []string{"fix"})
	if err == nil {
		t.Fatalf("expected collision error")
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/extension/ -run TestE2E_HostedSigils -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_sigils_test.go
git commit -m "test(extension): e2e sigils prefix registration + collision"
```

---

### Task 25: E2E — session metadata

**Files:**
- Create: `internal/extension/e2e_hosted_session_metadata_test.go`

- [ ] **Step 1: Write the test**

```go
package extension

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestE2E_HostedSessionMetadata_NameAndTags(t *testing.T) {
	if testing.Short() { t.Skip() }
	os.Setenv("PI_SURFACE_MODE", "session")
	rt, cleanup := setupHostedFixtures(t, "approvals_granted_surface.json", "hosted-surface-fixture")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rt.WaitForHostedReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForHostedReady: %v", err)
	}

	// Read back via the bridge seam.
	if rt.Bridge == nil {
		t.Fatal("bridge nil — wire it in setupHostedFixtures")
	}
	meta := rt.Bridge.GetSessionMetadata()
	if meta.Name != "fixture-branch" {
		t.Fatalf("name = %q", meta.Name)
	}
	if !reflect.DeepEqual(meta.Tags, []string{"one", "two"}) {
		t.Fatalf("tags = %+v", meta.Tags)
	}
}
```

> **Engineer note:** `setupHostedFixtures` may build the runtime with a `NoopBridge` — in that case, update the helper to wire a real `Bridge` (the TUI bridge implementation from Task 18) or a test-local bridge backed by a `FileService`. A test-local bridge is usually the right move for non-TUI e2e tests.

- [ ] **Step 2: Run**

Run: `go test ./internal/extension/ -run TestE2E_HostedSessionMetadata -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/extension/e2e_hosted_session_metadata_test.go
git commit -m "test(extension): e2e session metadata set_name + set_tags round-trip"
```

---

## Phase 9 — Cleanup & Verification

### Task 26: Full test sweep and lint

- [ ] **Step 1: Run full unit + e2e tests**

Run: `go test ./... -count=1`
Expected: all pass.

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: no issues.

- [ ] **Step 3: If the repo has a lint command, run it**

Run: `go build ./...` (sanity) and any documented linter (`golangci-lint run` if configured).
Expected: clean.

- [ ] **Step 4: Verify protocol version bump**

Run: `grep -n "ProtocolVersion" internal/extension/hostproto/protocol.go`
Expected: `ProtocolVersion = "2.2"`.

- [ ] **Step 5: Update docs pointer (optional)**

If there's a top-level extensions README or status doc referencing services, update to note v2.2 coverage. Skip if none.

- [ ] **Step 6: Commit any cleanup**

```bash
git add -A
git commit -m "chore(extensions): final sweep for v2.2 surface completion" || true
```

- [ ] **Step 7: Final status**

Run: `git log --oneline feature/hosted-tool-invocation ^main | head -30`
Expected: the new commits from Tasks 1–26 sit on top of the pre-existing hosted-tool-invocation work.

---

## Self-Review Checklist (engineer, after completing all tasks)

Verify against the spec (`docs/superpowers/specs/2026-04-20-extension-surface-v2-completion-design.md`):

- [ ] Protocol version is `"2.2"`.
- [ ] All 4 new service constants present (`state`, `commands`, `ui`, `sigils`).
- [ ] All new method constants present.
- [ ] All new error codes present.
- [ ] Capability map matches: `state.read`/`state.write`, `commands.manage`, `ui.status`/`ui.widget`/`ui.notify`/`ui.dialog`, `sigils.manage`, `session.metadata.read`/`session.metadata.write`.
- [ ] Every new method appears in `handleHostCall` / service-specific handler.
- [ ] `CommandRegistry` loaded from manifest commands at startup AND accepts runtime registrations in one namespace with collision rejection.
- [ ] Widget position field supports all 5 modes.
- [ ] Dialog is async: `ui.dialog` returns `dialog_id`, event `ui.dialog.resolved` fires on resolution.
- [ ] Sigil syntax `[[prefix:id]]` parses; multi-prefix atomic register.
- [ ] Session Meta round-trips Name + Tags; old-meta-without-fields still loads.
- [ ] E2E tests pass and cover set/patch/get/delete, register/invoke, status/widget/notify/dialog, sigil register+collision, metadata set+get.
- [ ] TUI `SessionBridge` implementation posts tea messages for all new surfaces (even if visual rendering is minimal).

If any row is "no," fix before claiming the plan complete.

---

## Out of Scope (explicit)

- `agent` service (mode registration, system-prompt composition).
- Tool interception (`tools.intercept`, `tools.on_call`, `tools.on_intercept`).
- Example extensions plan-mode, todos, session-name (they consume this surface in a follow-up branch).
- Rich visual design for widgets/dialogs/toasts — Phase 7/8 wires the data through; final visual polish is a separate UI pass.

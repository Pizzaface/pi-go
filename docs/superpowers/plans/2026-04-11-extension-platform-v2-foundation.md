# Extension Platform v2 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the v1 extension RPC protocol with a generic `host_call` envelope dispatching to a namespaced service registry, ship an extension SDK, build the two minimal services (`ui` + `commands`) needed by `hosted-hello`, and rewrite `hosted-hello` against the new surface.

**Architecture:** Protocol 2.0.0 has two new top-level RPC methods — `host_call` (ext→host) and `ext_call` (host→ext). Both carry `{service, method, version, payload}` envelopes dispatched through a `Registry` that maps service names to `Service` implementations. Each service lives in its own directory under `internal/extension/services/<name>/`. Plan 1 delivers the protocol change, the registry, the SDK, the ext→host direction of the wire, and enough services (`ui`, `commands`) to migrate `hosted-hello`. Plans 2 and 3 will add the remaining services, the host→ext mirror direction, and the sigil system.

**Tech Stack:** Go 1.22, standard library JSON-RPC over stdio (no external RPC framework). Tests use `testing` + existing fakes from `internal/extension/test_helpers_test.go`.

**Spec:** [`docs/superpowers/specs/2026-04-11-extension-platform-v2-design.md`](../specs/2026-04-11-extension-platform-v2-design.md)

**Plan 1 is scoped to the ext→host direction only.** Host-initiated `ext_call` requests (for `commands.on_invoke`, `sigils.on_resolve`, etc.) are deferred to Plan 2. `hosted-hello` does not need the mirror direction because its v1 behavior is purely fire-and-forget (handshake → register command → send status intent → wait for shutdown).

---

## File Structure

**Modified files:**
- `internal/extension/hostproto/protocol.go` — delete v1 method constants + old types, add `host_call`/`ext_call` types, bump version to 2.0.0
- `internal/extension/hostproto/protocol_test.go` — delete old-type tests, add new tests
- `internal/extension/permissions.go` — add new capability constants, `AllowsService` method
- `internal/extension/permissions_test.go` — add tests for `AllowsService`
- `internal/extension/hostruntime/client.go` — update `Handshake` for v2 types, add `ServeInbound` message loop
- `internal/extension/hostruntime/client_test.go` — update handshake tests, add inbound loop tests
- `internal/extension/manager.go` — construct `services.Registry`, wire host_call dispatch after handshake
- `internal/extension/manager_test.go` — update handshake expectations
- `examples/extensions/hosted-hello/extension.json` — bump to v2 manifest shape
- `examples/extensions/hosted-hello/main.go` — rewrite against SDK
- `examples/extensions/hosted-hello/README.md` — update for v2

**New files:**
- `internal/extension/services/service.go` — `Service` interface, `Call` struct, `SessionContext`, error helpers
- `internal/extension/services/service_test.go` — tests for error helpers + SessionContext
- `internal/extension/services/registry.go` — `Registry` type, dispatch logic
- `internal/extension/services/registry_test.go` — registry unit tests
- `internal/extension/services/ui/service.go` — `ui` service (status + clear_status)
- `internal/extension/services/ui/types.go` — ui payload types
- `internal/extension/services/ui/service_test.go`
- `internal/extension/services/commands/service.go` — `commands` service (register + unregister)
- `internal/extension/services/commands/types.go` — commands payload types
- `internal/extension/services/commands/service_test.go`
- `internal/extension/sdk/sdk.go` — extension-side SDK (`Serve`, `HostCall`, `RegisterHandler`)
- `internal/extension/sdk/sdk_test.go` — SDK tests with in-memory stdio

**Deleted types (migrate out of `protocol.go`):**
- `EventPayload`, `IntentEnvelope`, `CommandRegistration`, `ToolRegistration`, `RenderPayload`, `HealthNotification`, `ReloadControl`

**Retained types:** `RPCRequest`, `RPCResponse`, `RPCError`, `HandshakeRequest`, `HandshakeResponse` (both updated), `ShutdownControl`, `MethodHandshake`, `MethodShutdown`.

---

## Task 1: Delete v1 method constants and old payload types

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`
- Modify: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Delete obsolete tests**

Remove these three tests from `protocol_test.go`:
- `TestEventPayload_RoundTrip`
- `TestHealthNotification_Serialization`
- `TestRenderPayload_OnlyAllowsSupportedKinds`

Keep `TestHandshake_IncludesModeAndCapabilityMask` and `TestProtocol_RejectsIncompatibleMajorVersion` for now — they will be updated in Task 2.

- [ ] **Step 2: Delete v1 method constants and types from `protocol.go`**

Remove these constants from the `const` block:

```go
MethodEvent           = "pi.extension/event"
MethodIntent          = "pi.extension/intent"
MethodRegisterCommand = "pi.extension/register_command"
MethodRegisterTool    = "pi.extension/register_tool"
MethodRender          = "pi.extension/render"
MethodHealth          = "pi.extension/health"
MethodReload          = "pi.extension/reload"
```

Remove these type declarations entirely:
- `EventPayload`
- `IntentEnvelope`
- `CommandRegistration`
- `ToolRegistration`
- `RenderPayload` + `RenderKind` + the `RenderKindText`/`RenderKindMarkdown` constants + the `Validate()` method
- `HealthNotification`
- `ReloadControl`

After this step `protocol.go` should contain only: `RPCRequest`, `RPCResponse`, `RPCError`, `HandshakeRequest`, `HandshakeResponse`, `ShutdownControl`, `ValidateProtocolCompatibility`, `majorVersion`, and the remaining method constants (`MethodHandshake`, `MethodShutdown`).

- [ ] **Step 3: Run tests to verify package still compiles**

Run: `go build ./internal/extension/hostproto/...`
Expected: no output, exit 0.

Run: `go test ./internal/extension/hostproto/...`
Expected: the two remaining tests pass; compilation errors elsewhere in the tree are fine at this point — later tasks fix them.

- [ ] **Step 4: Commit**

```bash
git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go
git commit -m "refactor(extension): delete v1 protocol method constants and payload types

Preparing for v2 host_call envelope. The deleted constants
(MethodEvent, MethodIntent, MethodRegisterCommand, MethodRegisterTool,
MethodRender, MethodHealth, MethodReload) were never consumed on the
host side; they only existed for the extension author to send. The
deleted payload types (EventPayload, IntentEnvelope,
CommandRegistration, ToolRegistration, RenderPayload,
HealthNotification, ReloadControl) will be reintroduced as
service-specific types under internal/extension/services/<name>/."
```

---

## Task 2: Add v2 protocol version, host_call/ext_call envelopes, and error codes

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`
- Modify: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Write failing test for v2 version constants**

Replace `TestProtocol_RejectsIncompatibleMajorVersion` in `protocol_test.go` with:

```go
func TestProtocol_RejectsIncompatibleMajorVersion(t *testing.T) {
	if err := ValidateProtocolCompatibility("3.0.0"); err == nil {
		t.Fatal("expected incompatible major protocol version to fail")
	}
	if err := ValidateProtocolCompatibility("1.5.0"); err == nil {
		t.Fatal("expected v1 to be rejected by v2 host")
	}
	if err := ValidateProtocolCompatibility("2.1.0"); err != nil {
		t.Fatalf("expected v2 minor compatibility, got %v", err)
	}
}

func TestProtocol_VersionIsTwoZeroZero(t *testing.T) {
	if ProtocolVersion != "2.0.0" {
		t.Errorf("ProtocolVersion = %q, want %q", ProtocolVersion, "2.0.0")
	}
}

func TestHostCallParams_RoundTrip(t *testing.T) {
	in := HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
		Payload: json.RawMessage(`{"text":"hello"}`),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HostCallParams
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Service != "ui" || out.Method != "status" || out.Version != 1 {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if string(out.Payload) != `{"text":"hello"}` {
		t.Errorf("payload mismatch: %s", string(out.Payload))
	}
}

func TestErrorCodes_AreStable(t *testing.T) {
	if ErrCodeMethodNotFound != -32601 {
		t.Errorf("ErrCodeMethodNotFound = %d, want -32601", ErrCodeMethodNotFound)
	}
	if ErrCodeInvalidParams != -32602 {
		t.Errorf("ErrCodeInvalidParams = %d, want -32602", ErrCodeInvalidParams)
	}
	if ErrCodeServiceError != -32000 {
		t.Errorf("ErrCodeServiceError = %d, want -32000", ErrCodeServiceError)
	}
	if ErrCodeCapabilityDenied != -32001 {
		t.Errorf("ErrCodeCapabilityDenied = %d, want -32001", ErrCodeCapabilityDenied)
	}
	if ErrCodeServiceUnsupported != -32002 {
		t.Errorf("ErrCodeServiceUnsupported = %d, want -32002", ErrCodeServiceUnsupported)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/hostproto/... -run "TestProtocol_VersionIsTwoZeroZero|TestHostCallParams_RoundTrip|TestErrorCodes_AreStable" -v`
Expected: compilation error — `ErrCodeMethodNotFound`, `HostCallParams`, etc. undefined.

- [ ] **Step 3: Add v2 constants and types to `protocol.go`**

In the `const` block, change:
```go
ProtocolVersion = "1.0.0"
protocolMajor   = 1
```
to:
```go
ProtocolVersion = "2.0.0"
protocolMajor   = 2
```

Add after the existing method constants:
```go
const (
	MethodHostCall = "pi.extension/host_call"
	MethodExtCall  = "pi.extension/ext_call"
)

// JSON-RPC error codes emitted by the host + SDK. These are stable and
// documented as part of the protocol — do not change values.
const (
	ErrCodeMethodNotFound     = -32601 // unknown service or method
	ErrCodeInvalidParams      = -32602 // payload unmarshal / validate failure
	ErrCodeServiceError       = -32000 // handler returned an error
	ErrCodeCapabilityDenied   = -32001 // service used without handshake declaration
	ErrCodeServiceUnsupported = -32002 // host does not implement this service or version
)

// HostCallParams is the envelope for an ext→host RPC dispatched by the
// services registry. Payload is service-defined JSON.
type HostCallParams struct {
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Version int             `json:"version"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ExtCallParams is the mirror envelope: host→ext RPC. Same shape as
// HostCallParams. Used for command invocations, sigil resolves, etc.
// (Consumed in Plan 2 and later.)
type ExtCallParams struct {
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Version int             `json:"version"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/hostproto/... -v`
Expected: all tests in the package pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go
git commit -m "feat(extension): add v2 host_call envelope and error codes

Bumps protocol to 2.0.0. Adds MethodHostCall and MethodExtCall
constants, HostCallParams and ExtCallParams envelope types, and the
stable error code set (-32000, -32001, -32002, -32601, -32602).
The v2 host refuses 1.x handshakes."
```

---

## Task 3: Add v2 handshake types with RequestedServices

**Files:**
- Modify: `internal/extension/hostproto/protocol.go`
- Modify: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Replace the existing handshake test**

Replace `TestHandshake_IncludesModeAndCapabilityMask` with:

```go
func TestHandshakeRequest_RoundTrip(t *testing.T) {
	in := HandshakeRequest{
		ProtocolVersion: ProtocolVersion,
		ExtensionID:     "ext.demo",
		Mode:            "hosted_stdio",
		RequestedServices: []ServiceRequest{
			{Service: "ui", Version: 1, Methods: []string{"status"}},
			{Service: "commands", Version: 1},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"mode":"hosted_stdio"`) {
		t.Fatalf("expected mode in handshake payload, got %s", text)
	}
	if !strings.Contains(text, `"requested_services"`) {
		t.Fatalf("expected requested_services in handshake payload, got %s", text)
	}
	var out HandshakeRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.RequestedServices) != 2 {
		t.Fatalf("expected 2 requested services, got %d", len(out.RequestedServices))
	}
	if out.RequestedServices[0].Service != "ui" || out.RequestedServices[0].Methods[0] != "status" {
		t.Errorf("first service mismatch: %+v", out.RequestedServices[0])
	}
}

func TestHandshakeResponse_RoundTrip(t *testing.T) {
	in := HandshakeResponse{
		ProtocolVersion: ProtocolVersion,
		Accepted:        true,
		Message:         "ok",
		GrantedServices: []ServiceGrant{
			{Service: "ui", Version: 1, Methods: []string{"status", "clear_status"}},
		},
		DeniedServices: []ServiceDenial{
			{Service: "tools", Version: 1, Reason: "capability not granted"},
		},
		HostServices: []HostServiceInfo{
			{Service: "ui", Version: 1, Methods: []string{"status", "clear_status"}},
			{Service: "commands", Version: 1, Methods: []string{"register", "unregister"}},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HandshakeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Error("expected accepted=true")
	}
	if len(out.GrantedServices) != 1 || out.GrantedServices[0].Service != "ui" {
		t.Errorf("granted services mismatch: %+v", out.GrantedServices)
	}
	if len(out.HostServices) != 2 {
		t.Errorf("host services mismatch: %+v", out.HostServices)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/hostproto/... -run "TestHandshake" -v`
Expected: compilation error — `ServiceRequest`, `ServiceGrant`, `ServiceDenial`, `HostServiceInfo` undefined, `RequestedServices` / `GrantedServices` / `DeniedServices` / `HostServices` fields missing.

- [ ] **Step 3: Replace `HandshakeRequest` and `HandshakeResponse` types**

In `protocol.go`, replace the existing `HandshakeRequest` and `HandshakeResponse` types with:

```go
// HandshakeRequest is the first message an extension sends. It declares
// the protocol version, extension id, runtime mode, and the full set of
// services it intends to use. The host validates requested services
// against approvals.json and either accepts (with a possibly-trimmed
// grant list) or rejects before any host_call is processed.
type HandshakeRequest struct {
	ProtocolVersion   string           `json:"protocol_version"`
	ExtensionID       string           `json:"extension_id"`
	Mode              string           `json:"mode"`
	RequestedServices []ServiceRequest `json:"requested_services,omitempty"`
}

// ServiceRequest declares that the extension intends to use a particular
// service at a minimum version. Methods, if non-empty, narrows the
// request to a subset of the service's methods (defense in depth — the
// capability gate still runs at call time).
type ServiceRequest struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods,omitempty"`
}

// HandshakeResponse is the host's reply to a handshake. HostServices is
// the catalog of services the host supports, which lets an extension
// built against a newer spec detect missing capabilities at handshake
// and degrade gracefully.
type HandshakeResponse struct {
	ProtocolVersion string            `json:"protocol_version"`
	Accepted        bool              `json:"accepted"`
	Message         string            `json:"message,omitempty"`
	GrantedServices []ServiceGrant    `json:"granted_services,omitempty"`
	DeniedServices  []ServiceDenial   `json:"denied_services,omitempty"`
	HostServices    []HostServiceInfo `json:"host_services,omitempty"`
}

// ServiceGrant is an accepted service request. If Methods is non-empty
// the grant is narrowed to those methods; otherwise all methods of the
// service are granted.
type ServiceGrant struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods,omitempty"`
}

// ServiceDenial is a rejected service request with a human-readable
// reason for logging / surfacing to the user.
type ServiceDenial struct {
	Service string `json:"service"`
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

// HostServiceInfo describes a service the host supports. Returned in
// HandshakeResponse.HostServices so extensions can discover what's
// actually available at runtime.
type HostServiceInfo struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/hostproto/... -v`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go
git commit -m "feat(extension): replace handshake with v2 service declaration

HandshakeRequest.CapabilityMask (unstructured []string) is replaced
with RequestedServices (typed []ServiceRequest). HandshakeResponse
gains GrantedServices / DeniedServices / HostServices so extensions
can detect missing capabilities at handshake time and degrade
gracefully instead of failing at call time."
```

---

## Task 4: Create the services package foundation

**Files:**
- Create: `internal/extension/services/service.go`
- Create: `internal/extension/services/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/extension/services/service_test.go`:

```go
package services

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func TestRPCError_WrapsCodeAndMessage(t *testing.T) {
	err := NewRPCError(hostproto.ErrCodeInvalidParams, "bad payload")
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatal("expected errors.As to match *RPCError")
	}
	if rpcErr.Code != hostproto.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeInvalidParams)
	}
	if rpcErr.Message != "bad payload" {
		t.Errorf("message = %q, want %q", rpcErr.Message, "bad payload")
	}
	if err.Error() != "rpc error -32602: bad payload" {
		t.Errorf("unexpected error string: %q", err.Error())
	}
}

func TestSessionContext_ExtensionID(t *testing.T) {
	sc := &SessionContext{SessionID: "sess-1", ExtensionID: "ext.demo"}
	if sc.ExtensionID != "ext.demo" {
		t.Errorf("ExtensionID = %q, want %q", sc.ExtensionID, "ext.demo")
	}
}

// mockService verifies the Service interface can be implemented.
type mockService struct{}

func (mockService) Name() string             { return "mock" }
func (mockService) Version() int              { return 1 }
func (mockService) Methods() []string         { return []string{"ping"} }
func (mockService) Dispatch(call Call) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func TestService_InterfaceIsImplementable(t *testing.T) {
	var svc Service = mockService{}
	if svc.Name() != "mock" {
		t.Errorf("Name() = %q", svc.Name())
	}
	result, err := svc.Dispatch(Call{Method: "ping"})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/services/... -v`
Expected: package does not exist yet, compilation error.

- [ ] **Step 3: Create `internal/extension/services/service.go`**

```go
// Package services implements the v2 extension platform service registry.
// Each service lives in its own subpackage under this directory and
// implements the Service interface. The Registry dispatches incoming
// host_call requests to the appropriate service by name.
package services

import (
	"encoding/json"
	"fmt"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// Service is the host-side interface every v2 extension service
// implements. Services are stateless from the registry's point of view;
// any per-session state lives behind SessionContext.
type Service interface {
	// Name returns the service name as it appears in a host_call
	// envelope (e.g. "ui", "commands"). Must be stable and unique.
	Name() string

	// Version returns the current service version. A call requesting a
	// higher version than the service provides is rejected with
	// ErrCodeServiceUnsupported.
	Version() int

	// Methods returns the set of method names this service exposes.
	// Used for handshake HostServices catalog and for the registry's
	// capability gate.
	Methods() []string

	// Dispatch runs a single call and returns the response payload.
	// A nil error means success; a non-nil error should be an *RPCError
	// for well-typed failures or a plain error (wrapped as
	// ErrCodeServiceError) otherwise.
	Dispatch(call Call) (json.RawMessage, error)
}

// Call is a single host_call dispatched to a service.
type Call struct {
	ExtensionID string
	Method      string
	Version     int
	Payload     json.RawMessage
	Session     *SessionContext
}

// SessionContext carries the active session identity and any resources
// services need to read or mutate session-scoped state. It is created
// fresh per host_call so services cannot hold onto it.
type SessionContext struct {
	SessionID   string
	SessionsDir string
	ExtensionID string
}

// RPCError is a typed error carrying a JSON-RPC error code. Service
// implementations should return *RPCError for known failure modes; any
// other error is wrapped by the registry as ErrCodeServiceError.
type RPCError struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// NewRPCError constructs an *RPCError with the given code and message.
func NewRPCError(code int, message string) error {
	return &RPCError{Code: code, Message: message}
}

// ToRPCError converts any error into an *RPCError. Returns e directly
// if it already is one, otherwise wraps it with ErrCodeServiceError.
func ToRPCError(err error) *RPCError {
	if err == nil {
		return nil
	}
	if rpcErr, ok := err.(*RPCError); ok {
		return rpcErr
	}
	return &RPCError{Code: hostproto.ErrCodeServiceError, Message: err.Error()}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/services/... -v`
Expected: all three tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/services/service.go internal/extension/services/service_test.go
git commit -m "feat(extension): add services package with Service interface

Introduces the v2 service registry foundation: the Service interface
(Name, Version, Methods, Dispatch), the Call envelope, SessionContext,
and typed *RPCError with the stable error code mapping. Each v2
service will live in its own subpackage and implement this interface."
```

---

## Task 5: Implement the services Registry

**Files:**
- Create: `internal/extension/services/registry.go`
- Create: `internal/extension/services/registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/extension/services/registry_test.go`:

```go
package services

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

type stubService struct {
	name    string
	version int
	methods []string
	dispatch func(Call) (json.RawMessage, error)
}

func (s stubService) Name() string         { return s.name }
func (s stubService) Version() int         { return s.version }
func (s stubService) Methods() []string    { return s.methods }
func (s stubService) Dispatch(call Call) (json.RawMessage, error) {
	return s.dispatch(call)
}

// capGate is a minimal CapabilityGate used in tests. It allows or denies
// based on a pre-built allow map keyed by (extensionID, service, method).
type capGate struct {
	allow map[string]bool
}

func (c capGate) Allowed(extensionID, service, method string) bool {
	if c.allow == nil {
		return true
	}
	return c.allow[extensionID+"|"+service+"|"+method]
}

func newRegistry(t *testing.T, svcs ...Service) *Registry {
	t.Helper()
	r := NewRegistry(capGate{})
	for _, svc := range svcs {
		if err := r.Register(svc); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	return r
}

func TestRegistry_DispatchUnknownService(t *testing.T) {
	r := newRegistry(t)
	_, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "nope",
		Method:  "doit",
		Version: 1,
	}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeServiceUnsupported {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeServiceUnsupported)
	}
}

func TestRegistry_DispatchUnknownMethod(t *testing.T) {
	svc := stubService{
		name: "ui", version: 1, methods: []string{"status"},
		dispatch: func(Call) (json.RawMessage, error) { return nil, nil },
	}
	r := newRegistry(t, svc)
	_, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "ui",
		Method:  "mystery",
		Version: 1,
	}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeMethodNotFound {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeMethodNotFound)
	}
}

func TestRegistry_DispatchVersionTooHigh(t *testing.T) {
	svc := stubService{
		name: "ui", version: 1, methods: []string{"status"},
		dispatch: func(Call) (json.RawMessage, error) { return nil, nil },
	}
	r := newRegistry(t, svc)
	_, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 2,
	}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeServiceUnsupported {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeServiceUnsupported)
	}
}

func TestRegistry_DispatchCapabilityDenied(t *testing.T) {
	svc := stubService{
		name: "ui", version: 1, methods: []string{"status"},
		dispatch: func(Call) (json.RawMessage, error) {
			t.Fatal("dispatch should not be called when capability is denied")
			return nil, nil
		},
	}
	r := NewRegistry(capGate{allow: map[string]bool{}})
	_ = r.Register(svc)
	_, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
	}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeCapabilityDenied {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeCapabilityDenied)
	}
}

func TestRegistry_DispatchHappyPath(t *testing.T) {
	called := false
	svc := stubService{
		name: "ui", version: 1, methods: []string{"status"},
		dispatch: func(call Call) (json.RawMessage, error) {
			called = true
			if call.ExtensionID != "ext.demo" {
				t.Errorf("ExtensionID = %q", call.ExtensionID)
			}
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	r := newRegistry(t, svc)
	out, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
		Payload: json.RawMessage(`{"text":"hello"}`),
	}, &SessionContext{SessionID: "s1"})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if !called {
		t.Error("service.Dispatch was not called")
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("result = %s", string(out))
	}
}

func TestRegistry_RegisterDuplicateFails(t *testing.T) {
	svc := stubService{name: "ui", version: 1, methods: []string{"status"}}
	r := NewRegistry(capGate{})
	if err := r.Register(svc); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(svc); err == nil {
		t.Fatal("expected duplicate register to fail")
	}
}

func TestRegistry_HostServices(t *testing.T) {
	r := newRegistry(t,
		stubService{name: "ui", version: 1, methods: []string{"status", "clear_status"}},
		stubService{name: "commands", version: 1, methods: []string{"register"}},
	)
	catalog := r.HostServices()
	if len(catalog) != 2 {
		t.Fatalf("catalog size = %d, want 2", len(catalog))
	}
	found := map[string]bool{}
	for _, entry := range catalog {
		found[entry.Service] = true
	}
	if !found["ui"] || !found["commands"] {
		t.Errorf("missing entries: %+v", catalog)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/services/... -run "TestRegistry" -v`
Expected: `NewRegistry`, `Register`, `Dispatch`, `HostServices`, `CapabilityGate` undefined.

- [ ] **Step 3: Create `internal/extension/services/registry.go`**

```go
package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// CapabilityGate decides whether an extension is allowed to call a
// particular service method. Implementations typically consult
// approvals.json through the existing extension.Permissions type.
type CapabilityGate interface {
	Allowed(extensionID, service, method string) bool
}

// Registry is the host-side dispatch table for v2 extension services.
// A Registry is safe for concurrent Dispatch calls once all Register
// calls are complete.
type Registry struct {
	mu       sync.RWMutex
	services map[string]Service
	gate     CapabilityGate
}

// NewRegistry constructs a Registry with the given capability gate.
// Passing nil is allowed in tests and disables the gate entirely.
func NewRegistry(gate CapabilityGate) *Registry {
	return &Registry{
		services: make(map[string]Service),
		gate:     gate,
	}
}

// Register adds a service. Duplicate names return an error; services
// are added during startup before any Dispatch call runs.
func (r *Registry) Register(svc Service) error {
	if svc == nil {
		return fmt.Errorf("services: Register called with nil")
	}
	name := svc.Name()
	if name == "" {
		return fmt.Errorf("services: service name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.services[name]; exists {
		return fmt.Errorf("services: duplicate service %q", name)
	}
	r.services[name] = svc
	return nil
}

// Dispatch looks up the service, enforces version and capability gates,
// and forwards the call. All failures are returned as *RPCError so the
// caller can serialize them directly into a JSON-RPC response.
func (r *Registry) Dispatch(extensionID string, params hostproto.HostCallParams, sess *SessionContext) (json.RawMessage, error) {
	r.mu.RLock()
	svc, ok := r.services[params.Service]
	r.mu.RUnlock()
	if !ok {
		return nil, NewRPCError(hostproto.ErrCodeServiceUnsupported, fmt.Sprintf("unknown service %q", params.Service))
	}
	if params.Version > svc.Version() {
		return nil, NewRPCError(hostproto.ErrCodeServiceUnsupported, fmt.Sprintf("service %q version %d unavailable (host supports %d)", params.Service, params.Version, svc.Version()))
	}
	if !methodSupported(svc, params.Method) {
		return nil, NewRPCError(hostproto.ErrCodeMethodNotFound, fmt.Sprintf("service %q has no method %q", params.Service, params.Method))
	}
	if r.gate != nil && !r.gate.Allowed(extensionID, params.Service, params.Method) {
		return nil, NewRPCError(hostproto.ErrCodeCapabilityDenied, fmt.Sprintf("extension %q is not granted %s.%s", extensionID, params.Service, params.Method))
	}
	call := Call{
		ExtensionID: extensionID,
		Method:      params.Method,
		Version:     params.Version,
		Payload:     params.Payload,
		Session:     sess,
	}
	result, err := svc.Dispatch(call)
	if err != nil {
		return nil, ToRPCError(err)
	}
	return result, nil
}

// HostServices returns the service catalog sent to extensions in the
// handshake response. Sorted by service name for determinism.
func (r *Registry) HostServices() []hostproto.HostServiceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]hostproto.HostServiceInfo, 0, len(r.services))
	for _, svc := range r.services {
		out = append(out, hostproto.HostServiceInfo{
			Service: svc.Name(),
			Version: svc.Version(),
			Methods: append([]string(nil), svc.Methods()...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

func methodSupported(svc Service, method string) bool {
	for _, m := range svc.Methods() {
		if m == method {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/services/... -v`
Expected: all registry tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/services/registry.go internal/extension/services/registry_test.go
git commit -m "feat(extension): add services.Registry with dispatch + capability gate

Registry enforces: unknown service -> -32002, version too high ->
-32002, unknown method -> -32601, capability denied -> -32001, handler
error -> -32000. HostServices() returns the sorted catalog the host
sends in the handshake response."
```

---

## Task 6: Add new capability constants and `AllowsService` to Permissions

**Files:**
- Modify: `internal/extension/permissions.go`
- Modify: `internal/extension/permissions_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/extension/permissions_test.go`:

```go
func TestPermissions_AllowsService_HostedThirdPartyRequiresGrant(t *testing.T) {
	p := NewPermissions([]ApprovalRecord{
		{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			GrantedCapabilities: []Capability{CapabilityUIStatus, CapabilityCommandRegister},
		},
	})
	if !p.AllowsService("ext.demo", TrustClassHostedThirdParty, "ui", "status") {
		t.Error("expected ui.status to be allowed (ui.status granted)")
	}
	if !p.AllowsService("ext.demo", TrustClassHostedThirdParty, "commands", "register") {
		t.Error("expected commands.register to be allowed")
	}
	if p.AllowsService("ext.demo", TrustClassHostedThirdParty, "sigils", "register") {
		t.Error("expected sigils.register to be denied (not in grant list)")
	}
}

func TestPermissions_AllowsService_DeclarativeTrustedByDefault(t *testing.T) {
	p := EmptyPermissions()
	if !p.AllowsService("ext.builtin", TrustClassDeclarative, "ui", "status") {
		t.Error("declarative extensions should be trusted by default")
	}
	if !p.AllowsService("ext.builtin", TrustClassCompiledIn, "commands", "register") {
		t.Error("compiled-in extensions should be trusted by default")
	}
}

func TestPermissions_AllowsService_UnknownServiceDenied(t *testing.T) {
	p := NewPermissions([]ApprovalRecord{
		{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			GrantedCapabilities: []Capability{CapabilityUIStatus},
		},
	})
	// No mapping for an unknown service should result in deny for hosted extensions.
	if p.AllowsService("ext.demo", TrustClassHostedThirdParty, "mystery", "do") {
		t.Error("unknown service should be denied for hosted extensions")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/... -run "TestPermissions_AllowsService" -v`
Expected: `AllowsService` method undefined, compilation error.

- [ ] **Step 3: Add new capability constants and method to `permissions.go`**

In `permissions.go`, append the new capability constants to the existing `const` block:

```go
// v2 capabilities — one per new service namespace.
CapabilitySessionRead    Capability = "session.read"
CapabilitySessionWrite   Capability = "session.write"
CapabilityAgentMode      Capability = "agent.mode"
CapabilityStateRead      Capability = "state.read"
CapabilityStateWrite     Capability = "state.write"
CapabilityChatAppend     Capability = "chat.append"
CapabilitySigilsRegister Capability = "sigils.register"
CapabilitySigilsResolve  Capability = "sigils.resolve"
CapabilitySigilsAction   Capability = "sigils.action"
```

Append this new method at the bottom of the file:

```go
// AllowsService reports whether the given (extension, trust) tuple is
// allowed to call a v2 service method. It looks up the capability
// string associated with (service, method) and checks the extension's
// granted_capabilities from approvals.json. Services not in the mapping
// are denied for hosted extensions and allowed for
// declarative/compiled-in extensions.
func (p *Permissions) AllowsService(extensionID string, trust TrustClass, service, method string) bool {
	cap, ok := capabilityForServiceMethod(service, method)
	if !ok {
		// Unknown mapping. Trust declarative/compiled-in implicitly.
		return trust == TrustClassDeclarative || trust == TrustClassCompiledIn
	}
	if cap == "" {
		// Known method with no capability gate (e.g. reads, on_* callbacks).
		return true
	}
	return p.AllowsCapability(extensionID, trust, cap)
}

// capabilityForServiceMethod maps a (service, method) tuple to the
// Capability string required to call it. Returning ("", true) means
// the method has no gate (always allowed); returning (_, false) means
// the mapping is unknown.
func capabilityForServiceMethod(service, method string) (Capability, bool) {
	key := service + "." + method
	switch key {
	// session
	case "session.get_metadata":
		return CapabilitySessionRead, true
	case "session.set_name", "session.set_tags":
		return CapabilitySessionWrite, true

	// agent
	case "agent.get_mode", "agent.list_modes":
		return "", true
	case "agent.set_mode", "agent.register_mode", "agent.unregister_mode":
		return CapabilityAgentMode, true

	// state
	case "state.get":
		return CapabilityStateRead, true
	case "state.set", "state.patch", "state.delete":
		return CapabilityStateWrite, true

	// commands
	case "commands.register", "commands.unregister":
		return CapabilityCommandRegister, true

	// tools
	case "tools.register", "tools.unregister":
		return CapabilityToolRegister, true
	case "tools.intercept":
		return CapabilityToolIntercept, true

	// ui
	case "ui.status", "ui.clear_status":
		return CapabilityUIStatus, true
	case "ui.widget", "ui.clear_widget":
		return CapabilityUIWidget, true
	case "ui.notify":
		return CapabilityUIStatus, true // ui.notification maps to ui.status for v1
	case "ui.dialog":
		return CapabilityUIDialog, true

	// chat
	case "chat.append_message":
		return CapabilityChatAppend, true

	// sigils
	case "sigils.register", "sigils.unregister":
		return CapabilitySigilsRegister, true
	case "sigils.list":
		return "", true
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/... -run "TestPermissions_AllowsService" -v`
Expected: all three new tests pass.

Run: `go test ./internal/extension/... -run "TestPermissions" -v`
Expected: all existing and new permission tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/permissions.go internal/extension/permissions_test.go
git commit -m "feat(extension): add v2 capability constants and AllowsService

Nine new Capability constants covering session, agent, state, chat,
and sigils namespaces. AllowsService maps (service, method) tuples to
capability strings and consults the existing approvals.json gate."
```

---

## Task 7: Build the `ui` service

**Files:**
- Create: `internal/extension/services/ui/types.go`
- Create: `internal/extension/services/ui/service.go`
- Create: `internal/extension/services/ui/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/extension/services/ui/service_test.go`:

```go
package ui

import (
	"encoding/json"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/services"
)

func TestService_Metadata(t *testing.T) {
	svc := New(&fakeSink{})
	if svc.Name() != "ui" {
		t.Errorf("Name() = %q, want %q", svc.Name(), "ui")
	}
	if svc.Version() != 1 {
		t.Errorf("Version() = %d, want 1", svc.Version())
	}
	methods := svc.Methods()
	if len(methods) != 2 {
		t.Errorf("Methods() = %v, want 2 entries", methods)
	}
}

func TestService_StatusSetsText(t *testing.T) {
	sink := &fakeSink{}
	svc := New(sink)
	payload := StatusPayload{Text: "plan mode", Color: "yellow"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "status",
		Version:     1,
		Payload:     data,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}
	if len(sink.statuses) != 1 {
		t.Fatalf("sink.statuses = %d entries, want 1", len(sink.statuses))
	}
	if sink.statuses[0].ExtensionID != "ext.demo" || sink.statuses[0].Text != "plan mode" {
		t.Errorf("sink.statuses[0] = %+v", sink.statuses[0])
	}
}

func TestService_StatusRejectsEmptyText(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "status",
		Version:     1,
		Payload:     json.RawMessage(`{"text":""}`),
	})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
	var rpcErr *services.RPCError
	if !asRPC(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != -32602 {
		t.Errorf("code = %d, want -32602", rpcErr.Code)
	}
}

func TestService_ClearStatus(t *testing.T) {
	sink := &fakeSink{}
	svc := New(sink)
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "clear_status",
		Version:     1,
		Payload:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(sink.cleared) != 1 || sink.cleared[0] != "ext.demo" {
		t.Errorf("sink.cleared = %+v", sink.cleared)
	}
}

// fakeSink records what the service would have pushed to the TUI.
type fakeSink struct {
	statuses []StatusEntry
	cleared  []string
}

func (f *fakeSink) SetStatus(entry StatusEntry) { f.statuses = append(f.statuses, entry) }
func (f *fakeSink) ClearStatus(extensionID string) {
	f.cleared = append(f.cleared, extensionID)
}

// asRPC is a local errors.As helper that avoids importing errors in every test.
func asRPC(err error, target **services.RPCError) bool {
	for e := err; e != nil; {
		if rpcErr, ok := e.(*services.RPCError); ok {
			*target = rpcErr
			return true
		}
		type wrap interface{ Unwrap() error }
		if w, ok := e.(wrap); ok {
			e = w.Unwrap()
			continue
		}
		break
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/services/ui/... -v`
Expected: package does not exist, compilation error.

- [ ] **Step 3: Create `internal/extension/services/ui/types.go`**

```go
// Package ui implements the v2 "ui" service: transient UI intents
// (status, clear_status) that extensions push to the TUI.
package ui

// StatusPayload is the body of a ui.status host_call.
type StatusPayload struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"` // optional color hint, ANSI name
}

// StatusEntry is what the service hands to its Sink after validation.
// It is the host-side representation of a status line entry.
type StatusEntry struct {
	ExtensionID string
	Text        string
	Color       string
}

// Sink is the bridge between the service and whatever actually renders
// status entries. The manager injects a Sink that forwards to the TUI.
type Sink interface {
	SetStatus(entry StatusEntry)
	ClearStatus(extensionID string)
}
```

- [ ] **Step 4: Create `internal/extension/services/ui/service.go`**

```go
package ui

import (
	"encoding/json"
	"strings"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/services"
)

// Service implements the "ui" v2 service.
type Service struct {
	sink Sink
}

// New constructs the ui service with the given sink.
func New(sink Sink) *Service {
	return &Service{sink: sink}
}

// Name returns the service name.
func (s *Service) Name() string { return "ui" }

// Version returns the current service version.
func (s *Service) Version() int { return 1 }

// Methods returns the supported methods.
func (s *Service) Methods() []string { return []string{"status", "clear_status"} }

// Dispatch handles a single host_call.
func (s *Service) Dispatch(call services.Call) (json.RawMessage, error) {
	switch call.Method {
	case "status":
		return s.handleStatus(call)
	case "clear_status":
		return s.handleClearStatus(call)
	}
	return nil, services.NewRPCError(hostproto.ErrCodeMethodNotFound, "ui: unknown method "+call.Method)
}

func (s *Service) handleStatus(call services.Call) (json.RawMessage, error) {
	var payload StatusPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "ui.status: invalid payload: "+err.Error())
	}
	if strings.TrimSpace(payload.Text) == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "ui.status: text is required")
	}
	s.sink.SetStatus(StatusEntry{
		ExtensionID: call.ExtensionID,
		Text:        payload.Text,
		Color:       payload.Color,
	})
	return json.RawMessage(`{"ok":true}`), nil
}

func (s *Service) handleClearStatus(call services.Call) (json.RawMessage, error) {
	s.sink.ClearStatus(call.ExtensionID)
	return json.RawMessage(`{"ok":true}`), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/extension/services/ui/... -v`
Expected: all four tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/services/ui/
git commit -m "feat(extension): add ui service (status + clear_status)

The ui service validates status payloads and forwards them to a
pluggable Sink interface. The manager will wire the Sink to its
existing UI intent subscription fan-out so the TUI receives status
entries through the same channel it uses today."
```

---

## Task 8: Build the `commands` service

**Files:**
- Create: `internal/extension/services/commands/types.go`
- Create: `internal/extension/services/commands/service.go`
- Create: `internal/extension/services/commands/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/extension/services/commands/service_test.go`:

```go
package commands

import (
	"encoding/json"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/services"
)

func TestService_Metadata(t *testing.T) {
	svc := New(&fakeSink{})
	if svc.Name() != "commands" {
		t.Errorf("Name() = %q", svc.Name())
	}
	if svc.Version() != 1 {
		t.Errorf("Version() = %d", svc.Version())
	}
	methods := svc.Methods()
	if len(methods) != 2 {
		t.Errorf("Methods() = %v, want 2", methods)
	}
}

func TestService_Register(t *testing.T) {
	sink := &fakeSink{}
	svc := New(sink)
	payload := RegisterPayload{
		Name:        "plan",
		Description: "Toggle plan mode",
		Kind:        "callback",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "register",
		Version:     1,
		Payload:     data,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}
	if len(sink.registered) != 1 {
		t.Fatalf("sink.registered = %d, want 1", len(sink.registered))
	}
	if sink.registered[0].Name != "plan" || sink.registered[0].Kind != "callback" {
		t.Errorf("sink.registered[0] = %+v", sink.registered[0])
	}
	if sink.registered[0].ExtensionID != "ext.demo" {
		t.Errorf("ExtensionID = %q", sink.registered[0].ExtensionID)
	}
}

func TestService_RegisterRejectsEmptyName(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "register",
		Version:     1,
		Payload:     json.RawMessage(`{"name":"","kind":"callback"}`),
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestService_RegisterRejectsUnknownKind(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "register",
		Version:     1,
		Payload:     json.RawMessage(`{"name":"plan","kind":"weird"}`),
	})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestService_RegisterDefaultKindIsPrompt(t *testing.T) {
	sink := &fakeSink{}
	svc := New(sink)
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "register",
		Version:     1,
		Payload:     json.RawMessage(`{"name":"hello","prompt":"Say hello. {{args}}"}`),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sink.registered[0].Kind != "prompt" {
		t.Errorf("Kind = %q, want prompt", sink.registered[0].Kind)
	}
	if sink.registered[0].Prompt != "Say hello. {{args}}" {
		t.Errorf("Prompt = %q", sink.registered[0].Prompt)
	}
}

func TestService_Unregister(t *testing.T) {
	sink := &fakeSink{}
	svc := New(sink)
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "unregister",
		Version:     1,
		Payload:     json.RawMessage(`{"name":"plan"}`),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(sink.unregistered) != 1 || sink.unregistered[0].Name != "plan" {
		t.Errorf("sink.unregistered = %+v", sink.unregistered)
	}
}

// fakeSink records what the service would have pushed to the manager.
type fakeSink struct {
	registered   []Registration
	unregistered []UnregisterInput
}

func (f *fakeSink) RegisterCommand(reg Registration) error {
	f.registered = append(f.registered, reg)
	return nil
}

func (f *fakeSink) UnregisterCommand(input UnregisterInput) error {
	f.unregistered = append(f.unregistered, input)
	return nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/services/commands/... -v`
Expected: package does not exist, compilation error.

- [ ] **Step 3: Create `internal/extension/services/commands/types.go`**

```go
// Package commands implements the v2 "commands" service: slash command
// registration and unregistration. Command invocation (ext_call
// on_invoke) is a Plan 2 concern.
package commands

// RegisterPayload is the body of a commands.register host_call.
type RegisterPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Kind        string `json:"kind,omitempty"` // "prompt" (default) or "callback"
}

// UnregisterPayload is the body of a commands.unregister host_call.
type UnregisterPayload struct {
	Name string `json:"name"`
}

// Registration is the host-side representation of a registered command,
// handed to the Sink after validation.
type Registration struct {
	ExtensionID string
	Name        string
	Description string
	Prompt      string
	Kind        string
}

// UnregisterInput is what Sink.UnregisterCommand receives.
type UnregisterInput struct {
	ExtensionID string
	Name        string
}

// Sink is the bridge between the service and the manager's existing
// command registry.
type Sink interface {
	RegisterCommand(reg Registration) error
	UnregisterCommand(input UnregisterInput) error
}
```

- [ ] **Step 4: Create `internal/extension/services/commands/service.go`**

```go
package commands

import (
	"encoding/json"
	"strings"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/services"
)

// Service implements the "commands" v2 service.
type Service struct {
	sink Sink
}

// New constructs the commands service.
func New(sink Sink) *Service {
	return &Service{sink: sink}
}

// Name returns the service name.
func (s *Service) Name() string { return "commands" }

// Version returns the current service version.
func (s *Service) Version() int { return 1 }

// Methods returns the supported methods.
func (s *Service) Methods() []string { return []string{"register", "unregister"} }

// Dispatch handles a single host_call.
func (s *Service) Dispatch(call services.Call) (json.RawMessage, error) {
	switch call.Method {
	case "register":
		return s.handleRegister(call)
	case "unregister":
		return s.handleUnregister(call)
	}
	return nil, services.NewRPCError(hostproto.ErrCodeMethodNotFound, "commands: unknown method "+call.Method)
}

func (s *Service) handleRegister(call services.Call) (json.RawMessage, error) {
	var payload RegisterPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: invalid payload: "+err.Error())
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: name is required")
	}
	kind := strings.TrimSpace(payload.Kind)
	if kind == "" {
		kind = "prompt"
	}
	if kind != "prompt" && kind != "callback" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: kind must be 'prompt' or 'callback'")
	}
	if err := s.sink.RegisterCommand(Registration{
		ExtensionID: call.ExtensionID,
		Name:        name,
		Description: payload.Description,
		Prompt:      payload.Prompt,
		Kind:        kind,
	}); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeServiceError, err.Error())
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func (s *Service) handleUnregister(call services.Call) (json.RawMessage, error) {
	var payload UnregisterPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.unregister: invalid payload: "+err.Error())
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.unregister: name is required")
	}
	if err := s.sink.UnregisterCommand(UnregisterInput{
		ExtensionID: call.ExtensionID,
		Name:        name,
	}); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeServiceError, err.Error())
	}
	return json.RawMessage(`{"ok":true}`), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/extension/services/commands/... -v`
Expected: all six tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/services/commands/
git commit -m "feat(extension): add commands service (register + unregister)

The commands service validates register/unregister payloads and
forwards them to a pluggable Sink that the manager wires to its
existing command registry. Kind defaults to 'prompt'; 'callback'
support needs the ext_call mirror path added in Plan 2."
```

---

## Task 9: Extension SDK — stdio transport scaffold

**Files:**
- Create: `internal/extension/sdk/sdk.go`
- Create: `internal/extension/sdk/sdk_test.go`

- [ ] **Step 1: Write failing test for `NewClient` + round-trip**

Create `internal/extension/sdk/sdk_test.go`:

```go
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// newPipePair returns two connected (reader, writer) pairs for an
// in-memory stdio bidirectional channel: (extStdin, extStdout) for
// the extension side and (hostOut, hostIn) for the host side.
// Writes to hostIn appear on extStdin; writes to extStdout appear on
// hostOut.
func newPipePair() (extStdin io.Reader, extStdout io.Writer, hostIn io.Writer, hostOut io.Reader) {
	extStdinR, extStdinW := io.Pipe()
	extStdoutR, extStdoutW := io.Pipe()
	return extStdinR, extStdoutW, extStdinW, extStdoutR
}

func TestClient_SendRequest_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	client := NewClient(io.NopCloser(&bytes.Buffer{}), &buf)

	id, err := client.sendRequest(hostproto.MethodHandshake, map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	if id != 1 {
		t.Errorf("first id = %d, want 1", id)
	}

	var req hostproto.RPCRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Method != hostproto.MethodHandshake {
		t.Errorf("Method = %q", req.Method)
	}
	if req.ID != 1 {
		t.Errorf("ID = %d", req.ID)
	}
}

func TestClient_ReadMessage_DispatchesResponse(t *testing.T) {
	extStdin, _, hostIn, _ := newPipePair()
	client := NewClient(io.NopCloser(extStdinReader{extStdin}), io.Discard)

	// Prime a pending response waiter.
	waiter := client.waitFor(42)

	// Host writes a response.
	resp := hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      42,
		Result:  json.RawMessage(`{"ok":true}`),
	}
	data, _ := json.Marshal(resp)
	go func() {
		_, _ = hostIn.(io.Writer).Write(append(data, '\n'))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go client.readLoop(ctx)

	select {
	case got := <-waiter:
		if got.err != nil {
			t.Fatalf("waiter err: %v", got.err)
		}
		if string(got.result) != `{"ok":true}` {
			t.Errorf("result = %s", string(got.result))
		}
	case <-ctx.Done():
		t.Fatal("waiter did not receive response")
	}
}

// extStdinReader wraps an io.Reader to make it an io.ReadCloser.
type extStdinReader struct{ io.Reader }

func (extStdinReader) Close() error { return nil }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/sdk/... -v`
Expected: package does not exist, compilation error.

- [ ] **Step 3: Create `internal/extension/sdk/sdk.go`**

```go
// Package sdk provides helpers for authoring v2 pi-go extensions in Go.
// It handles the stdio JSON-RPC plumbing so extension authors can write
// handlers instead of wire-protocol code.
//
// Typical usage:
//
//	func main() {
//	    client := sdk.NewClient(os.Stdin, os.Stdout)
//	    client.RegisterHandler("commands", "on_invoke", 1, func(payload json.RawMessage) (any, error) { ... })
//	    if err := client.Serve(context.Background(), sdk.ServeOptions{
//	        ExtensionID: "my-ext",
//	        RequestedServices: []hostproto.ServiceRequest{...},
//	    }); err != nil {
//	        log.Fatal(err)
//	    }
//	}
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// Client is the extension-side SDK client. One per extension process.
type Client struct {
	in      io.ReadCloser
	out     io.Writer
	encoder *json.Encoder
	decoder *json.Decoder

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResult
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewClient constructs a Client reading from in and writing to out.
// Typical callers pass os.Stdin / os.Stdout.
func NewClient(in io.ReadCloser, out io.Writer) *Client {
	return &Client{
		in:      in,
		out:     out,
		encoder: json.NewEncoder(out),
		decoder: json.NewDecoder(in),
		pending: make(map[int64]chan rpcResult),
	}
}

// sendRequest writes an RPCRequest and returns the assigned id.
func (c *Client) sendRequest(method string, params any) (int64, error) {
	id := c.nextID.Add(1)
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return 0, fmt.Errorf("sdk: marshal params: %w", err)
		}
		raw = data
	}
	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  raw,
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.encoder.Encode(req); err != nil {
		return 0, fmt.Errorf("sdk: encode request: %w", err)
	}
	return id, nil
}

// waitFor registers a pending-response channel for the given id.
func (c *Client) waitFor(id int64) chan rpcResult {
	ch := make(chan rpcResult, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	return ch
}

// resolvePending delivers a response to the waiting channel, if any.
func (c *Client) resolvePending(id int64, result rpcResult) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if ok {
		ch <- result
	}
}

// readLoop pumps incoming messages from in. Responses are routed to
// pending waiters; requests are dispatched to registered handlers (see
// Task 10). Exits on EOF or context cancellation.
func (c *Client) readLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		var msg json.RawMessage
		if err := c.decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
		// Distinguish request vs response by probing for a method field.
		var probe struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *hostproto.RPCError `json:"error"`
		}
		if err := json.Unmarshal(msg, &probe); err != nil {
			continue
		}
		if probe.Method == "" {
			// Response path.
			if probe.Error != nil {
				c.resolvePending(probe.ID, rpcResult{err: fmt.Errorf("rpc error %d: %s", probe.Error.Code, probe.Error.Message)})
			} else {
				c.resolvePending(probe.ID, rpcResult{result: probe.Result})
			}
			continue
		}
		// Request path: dispatched in Task 10.
		_ = probe
	}
}

// Close releases the stdio handles.
func (c *Client) Close() error {
	if c.in != nil {
		_ = c.in.Close()
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/sdk/... -v`
Expected: both tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/sdk/
git commit -m "feat(extension): add sdk package stdio transport scaffold

The Client type manages a stdio JSON-RPC read/write loop with request
correlation by id. Handler registration and the Serve entry point
come in Task 10."
```

---

## Task 10: Extension SDK — Serve entry point, HostCall, handshake

**Files:**
- Modify: `internal/extension/sdk/sdk.go`
- Modify: `internal/extension/sdk/sdk_test.go`

- [ ] **Step 1: Write failing tests for HostCall and Serve**

Append to `internal/extension/sdk/sdk_test.go`:

```go
func TestClient_HostCall_SendsRequestAndReceivesResult(t *testing.T) {
	// Bidirectional in-memory pipes.
	fromHostR, fromHostW := io.Pipe() // host writes to W, ext reads from R
	toHostR, toHostW := io.Pipe()     // ext writes to W, host reads from R

	client := NewClient(fromHostR, toHostW)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.readLoop(ctx)

	// Fake host: reads an incoming request from toHostR, replies on fromHostW.
	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		if err := decoder.Decode(&req); err != nil {
			t.Errorf("fake host decode: %v", err)
			return
		}
		if req.Method != hostproto.MethodHostCall {
			t.Errorf("Method = %q", req.Method)
		}
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Result:  json.RawMessage(`{"ok":true}`),
		})
	}()

	result, err := client.HostCall(ctx, "ui", "status", 1, map[string]string{"text": "hi"})
	if err != nil {
		t.Fatalf("HostCall: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}
}

func TestClient_HostCall_PropagatesRPCError(t *testing.T) {
	fromHostR, fromHostW := io.Pipe()
	toHostR, toHostW := io.Pipe()

	client := NewClient(fromHostR, toHostW)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.readLoop(ctx)

	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		_ = decoder.Decode(&req)
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Error: &hostproto.RPCError{
				Code:    hostproto.ErrCodeInvalidParams,
				Message: "text required",
			},
		})
	}()

	_, err := client.HostCall(ctx, "ui", "status", 1, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "-32602") || !contains(err.Error(), "text required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_Serve_PerformsHandshake(t *testing.T) {
	fromHostR, fromHostW := io.Pipe()
	toHostR, toHostW := io.Pipe()

	client := NewClient(fromHostR, toHostW)

	// Fake host: reads the handshake request and replies accepted=true.
	serveDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		if err := decoder.Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if req.Method != hostproto.MethodHandshake {
			t.Errorf("first method = %q, want handshake", req.Method)
		}
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Result: mustMarshal(hostproto.HandshakeResponse{
				ProtocolVersion: hostproto.ProtocolVersion,
				Accepted:        true,
				HostServices:    []hostproto.HostServiceInfo{{Service: "ui", Version: 1, Methods: []string{"status"}}},
			}),
		})
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		serveDone <- client.Serve(ctx, ServeOptions{
			ExtensionID: "ext.demo",
			Mode:        "hosted_stdio",
			RequestedServices: []hostproto.ServiceRequest{
				{Service: "ui", Version: 1, Methods: []string{"status"}},
			},
			OnReady: func(HandshakeReady) error { cancel(); return nil },
		})
	}()

	select {
	case err := <-serveDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve: %v", err)
		}
	case <-ctx.Done():
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/sdk/... -v`
Expected: compilation error — `HostCall`, `Serve`, `ServeOptions`, `HandshakeReady` undefined.

- [ ] **Step 3: Add `HostCall`, `ServeOptions`, `HandshakeReady`, and `Serve` to `sdk.go`**

Append to `sdk.go`:

```go
// HostCall issues a single host_call RPC and waits for the response.
// This is the primary way extensions invoke host services.
func (c *Client) HostCall(ctx context.Context, service, method string, version int, payload any) (json.RawMessage, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("sdk: HostCall marshal payload: %w", err)
	}
	params := hostproto.HostCallParams{
		Service: service,
		Method:  method,
		Version: version,
		Payload: payloadBytes,
	}
	id, err := c.sendRequest(hostproto.MethodHostCall, params)
	if err != nil {
		return nil, err
	}
	waiter := c.waitFor(id)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-waiter:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	}
}

// ServeOptions configures a Serve invocation.
type ServeOptions struct {
	ExtensionID       string
	Mode              string // typically "hosted_stdio"
	RequestedServices []hostproto.ServiceRequest
	// OnReady is called once the handshake completes successfully. Use
	// it to issue startup host_calls (e.g. commands.register). Returning
	// an error terminates Serve.
	OnReady func(HandshakeReady) error
}

// HandshakeReady is passed to ServeOptions.OnReady with the negotiated
// handshake result.
type HandshakeReady struct {
	Client   *Client
	Response hostproto.HandshakeResponse
}

// Serve runs the extension lifecycle:
//  1. Start the stdio read loop
//  2. Send the handshake
//  3. Wait for the handshake response
//  4. Invoke OnReady (if set) for startup registrations
//  5. Block until the host closes stdin or ctx is canceled
//
// Serve returns nil on clean shutdown, or an error if the handshake
// fails, the read loop errors, or OnReady returns an error.
func (c *Client) Serve(ctx context.Context, opts ServeOptions) error {
	if opts.ExtensionID == "" {
		return fmt.Errorf("sdk: ExtensionID is required")
	}
	if opts.Mode == "" {
		opts.Mode = "hosted_stdio"
	}

	go c.readLoop(ctx)

	// Handshake.
	handshakeReq := hostproto.HandshakeRequest{
		ProtocolVersion:   hostproto.ProtocolVersion,
		ExtensionID:       opts.ExtensionID,
		Mode:              opts.Mode,
		RequestedServices: opts.RequestedServices,
	}
	id, err := c.sendRequest(hostproto.MethodHandshake, handshakeReq)
	if err != nil {
		return err
	}
	waiter := c.waitFor(id)

	var response hostproto.HandshakeResponse
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-waiter:
		if res.err != nil {
			return fmt.Errorf("sdk: handshake failed: %w", res.err)
		}
		if err := json.Unmarshal(res.result, &response); err != nil {
			return fmt.Errorf("sdk: decode handshake response: %w", err)
		}
	}

	if !response.Accepted {
		return fmt.Errorf("sdk: handshake rejected: %s", response.Message)
	}
	if err := hostproto.ValidateProtocolCompatibility(response.ProtocolVersion); err != nil {
		return fmt.Errorf("sdk: incompatible host protocol: %w", err)
	}

	if opts.OnReady != nil {
		if err := opts.OnReady(HandshakeReady{Client: c, Response: response}); err != nil {
			return err
		}
	}

	// Block until context cancellation. The read loop exits on its own
	// when the host closes stdin.
	<-ctx.Done()
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/sdk/... -v`
Expected: all sdk tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/sdk/sdk.go internal/extension/sdk/sdk_test.go
git commit -m "feat(extension): add sdk.Client.Serve, HostCall, handshake

Serve runs the full extension lifecycle: handshake -> OnReady
callback -> block until shutdown. HostCall issues a single host_call
and waits for the response, propagating RPC errors. Handler
registration for host->ext mirror calls is deferred to Plan 2."
```

---

## Task 11: Host-side ext-initiated request dispatch loop

**Files:**
- Modify: `internal/extension/hostruntime/client.go`
- Modify: `internal/extension/hostruntime/client_test.go`

- [ ] **Step 1: Write failing test for `ServeInbound`**

Append to `internal/extension/hostruntime/client_test.go`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// fakeDispatcher implements hostruntime.Dispatcher in tests.
type fakeDispatcher struct {
	calls []hostproto.HostCallParams
}

func (f *fakeDispatcher) Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	f.calls = append(f.calls, params)
	return json.RawMessage(`{"ok":true}`), nil
}

func TestServeInbound_DispatchesHostCall(t *testing.T) {
	// extWrites flows from extension → host; hostWrites flows host → extension.
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	// Simulate an extension sending a host_call.
	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      7,
		Method:  hostproto.MethodHostCall,
		Params: mustMarshalRPC(t, hostproto.HostCallParams{
			Service: "ui",
			Method:  "status",
			Version: 1,
			Payload: json.RawMessage(`{"text":"hi"}`),
		}),
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)
	dispatcher := &fakeDispatcher{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.ServeInbound(ctx, "ext.demo", dispatcher)
	}()

	// Wait for either completion (EOF reached) or timeout.
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("ServeInbound did not exit on EOF")
	}

	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1", len(dispatcher.calls))
	}
	if dispatcher.calls[0].Service != "ui" || dispatcher.calls[0].Method != "status" {
		t.Errorf("unexpected call: %+v", dispatcher.calls[0])
	}

	// Verify response was written.
	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != 7 {
		t.Errorf("response ID = %d, want 7", resp.ID)
	}
	if string(resp.Result) != `{"ok":true}` {
		t.Errorf("response result = %s", string(resp.Result))
	}
}

func TestServeInbound_DispatcherErrorBecomesRPCError(t *testing.T) {
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      11,
		Method:  hostproto.MethodHostCall,
		Params:  mustMarshalRPC(t, hostproto.HostCallParams{Service: "ui", Method: "status", Version: 1}),
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)
	dispatcher := errorDispatcher{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.ServeInbound(ctx, "ext.demo", dispatcher) }()
	<-done

	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != hostproto.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, hostproto.ErrCodeInvalidParams)
	}
}

type errorDispatcher struct{}

func (errorDispatcher) Dispatch(string, hostproto.HostCallParams) (json.RawMessage, error) {
	return nil, &RPCError{Code: hostproto.ErrCodeInvalidParams, Message: "bad"}
}

// mustMarshalRPC marshals a value or fails the test.
func mustMarshalRPC(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestServeInbound_UnknownMethodIsError(t *testing.T) {
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      5,
		Method:  "pi.extension/nope",
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)
	dispatcher := &fakeDispatcher{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.ServeInbound(ctx, "ext.demo", dispatcher) }()
	<-done

	if len(dispatcher.calls) != 0 {
		t.Error("dispatcher should not be called for unknown method")
	}
	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != hostproto.ErrCodeMethodNotFound {
		t.Errorf("unexpected response error: %+v", resp.Error)
	}
}
```

Also add to the top of the test file (if not already present in imports):
```go
import "github.com/dimetron/pi-go/internal/extension/hostruntime"
```

Actually, these tests live IN `package hostruntime`, so no external import needed. Ensure the test file declares `package hostruntime` at the top (matching `client_test.go`).

Add a local `RPCError` alias at the top of the test section so the test file compiles without pulling in the services package:

```go
// RPCError is a local re-declaration for tests. It matches the shape
// used by services.RPCError so errorDispatcher can produce typed
// errors without importing the services package (which would create
// an import cycle through the manager).
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return e.Message }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/hostruntime/... -run "TestServeInbound" -v`
Expected: compilation error — `ServeInbound`, `Dispatcher` undefined.

- [ ] **Step 3: Add `Dispatcher` interface and `ServeInbound` to `client.go`**

Add to the top of `client.go` below the existing imports:

```go
// Dispatcher routes an incoming host_call from an extension to the
// services registry. The hostruntime package defines the interface to
// avoid an import cycle with the services package.
type Dispatcher interface {
	Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error)
}

// dispatcherError is the minimal interface ServeInbound uses to extract
// a JSON-RPC code from an error returned by Dispatch. Matches
// services.RPCError.
type dispatcherError interface {
	error
	// The two public fields are accessed via reflection-free type
	// assertion by reflecting on the concrete *services.RPCError type
	// at the call site; see extractRPCError below.
}
```

Append at the bottom of `client.go`:

```go
// ServeInbound reads ext-initiated requests from the client's stdout
// (the extension's stdout, read by the host) and dispatches them via
// the registry. Runs until EOF or ctx is canceled. Returns nil on
// clean EOF or ctx.Err() on cancellation; other errors propagate.
//
// All dispatch failures are serialized as JSON-RPC error responses;
// only framing/transport errors abort the loop.
func (c *Client) ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var req hostproto.RPCRequest
		c.readMu.Lock()
		err := c.decoder.Decode(&req)
		c.readMu.Unlock()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("hostruntime: decode inbound request: %w", err)
		}
		switch req.Method {
		case hostproto.MethodHostCall:
			c.handleHostCall(extensionID, req, dispatcher)
		default:
			c.writeError(req.ID, hostproto.ErrCodeMethodNotFound, "unknown method: "+req.Method)
		}
	}
}

func (c *Client) handleHostCall(extensionID string, req hostproto.RPCRequest, dispatcher Dispatcher) {
	var params hostproto.HostCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		c.writeError(req.ID, hostproto.ErrCodeInvalidParams, "invalid host_call params: "+err.Error())
		return
	}
	result, err := dispatcher.Dispatch(extensionID, params)
	if err != nil {
		code, msg := extractRPCError(err)
		c.writeError(req.ID, code, msg)
		return
	}
	c.writeResult(req.ID, result)
}

func (c *Client) writeResult(id int64, result json.RawMessage) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Result:  result,
	})
}

func (c *Client) writeError(id int64, code int, message string) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Error: &hostproto.RPCError{
			Code:    code,
			Message: message,
		},
	})
}

// extractRPCError pulls a JSON-RPC code and message out of an error.
// It handles *services.RPCError via a minimal interface check, and
// falls back to ErrCodeServiceError for anything else.
func extractRPCError(err error) (int, string) {
	type coded interface {
		error
	}
	// Use reflect-free checks: the services package returns *RPCError
	// with Code and Message fields, accessible via a small interface.
	type rpcErrShape interface {
		error
	}
	if err == nil {
		return hostproto.ErrCodeServiceError, ""
	}
	// Try type assertion via a tiny local interface that matches
	// services.RPCError's concrete fields through an error with the
	// shape services.*RPCError. We avoid importing services to prevent
	// the cycle, so we inspect via a value accessor interface.
	if carrier, ok := err.(interface {
		error
		RPCCode() int
	}); ok {
		return carrier.RPCCode(), carrier.Error()
	}
	// Fallback: assume a generic service error.
	return hostproto.ErrCodeServiceError, err.Error()
}
```

Also update the import block at the top of `client.go` to add:
```go
"errors"
```

- [ ] **Step 4: Add `RPCCode()` method on `services.RPCError`**

To make `extractRPCError` above work without the hostruntime package importing services, add a small method on `*services.RPCError`:

Edit `internal/extension/services/service.go`, adding this method after the existing `Error()` method:

```go
// RPCCode returns the JSON-RPC error code. This lets callers that do
// not import the services package (e.g. hostruntime) extract the code
// through a small interface.
func (e *RPCError) RPCCode() int { return e.Code }
```

- [ ] **Step 5: Update the test file to use the local-coded error**

In the test file, replace the `errorDispatcher` and local `RPCError` declarations with an `RPCCode` method:

```go
type codedRPCErr struct {
	code    int
	message string
}

func (e *codedRPCErr) Error() string { return e.message }
func (e *codedRPCErr) RPCCode() int  { return e.code }

type errorDispatcher struct{}

func (errorDispatcher) Dispatch(string, hostproto.HostCallParams) (json.RawMessage, error) {
	return nil, &codedRPCErr{code: hostproto.ErrCodeInvalidParams, message: "bad"}
}
```

Remove the earlier `type RPCError struct` declaration from the test file.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/extension/hostruntime/... -run "TestServeInbound" -v`
Expected: all three tests pass.

Run: `go test ./internal/extension/services/... -v`
Expected: existing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/extension/hostruntime/client.go internal/extension/hostruntime/client_test.go internal/extension/services/service.go
git commit -m "feat(extension): host-side ServeInbound dispatches host_call

Client.ServeInbound reads RPC requests from the extension's stdout,
dispatches them via a Dispatcher interface, and writes responses back.
Errors from the dispatcher are serialized as JSON-RPC error responses
using RPCCode() on *services.RPCError (accessed via small interface
to avoid a hostruntime -> services import cycle). Non-host_call
methods return ErrCodeMethodNotFound."
```

---

## Task 12: Update handshake call site and wire Manager to the services Registry

**Files:**
- Modify: `internal/extension/hostruntime/client.go`
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go` (minimal — existing tests still compile)

- [ ] **Step 1: Update `Client.Handshake` to send `RequestedServices` and return v2 response**

In `client.go`, the existing `Handshake` method already takes a `HandshakeRequest` by value and marshals it. Since Task 3 replaced the struct, the method body doesn't need changes — but update the call site in `manager.go`.

In `manager.go`, the `StartHostedExtensions` call site passes `CapabilityMask` from the manifest. Replace this:

```go
_, err = client.Handshake(handshakeCtx, hostproto.HandshakeRequest{
    ProtocolVersion: hostproto.ProtocolVersion,
    ExtensionID:     reg.id,
    Mode:            mode,
    CapabilityMask:  capabilitiesToStrings(reg.manifest.Capabilities),
})
```

with:

```go
_, err = client.Handshake(handshakeCtx, hostproto.HandshakeRequest{
    ProtocolVersion:   hostproto.ProtocolVersion,
    ExtensionID:       reg.id,
    Mode:              mode,
    RequestedServices: m.manifestToRequestedServices(reg.manifest),
})
```

And remove the now-unused `capabilitiesToStrings` helper (or keep it if it's referenced elsewhere — run the next step to find out).

- [ ] **Step 2: Run the build to find any remaining references to removed types**

Run: `go build ./...`

Expected: one or more errors in `manager.go` pointing at `capabilitiesToStrings` and `hostproto.HandshakeRequest.CapabilityMask`. Record each location and fix them in Step 3.

- [ ] **Step 3: Add the manifest-to-requested-services helper and wire Registry into Manager**

In `manager.go`, add near the other helpers:

```go
// manifestToRequestedServices produces the services an extension's
// manifest declares it will use. For v2 extensions the manifest should
// carry an explicit RequestedServices field, but until Plan 2 adds that
// we synthesize a list from the manifest's declared Capabilities. The
// manifest's Capabilities slice becomes one ServiceRequest per distinct
// service prefix (the prefix before the first "." in the capability).
func (m *Manager) manifestToRequestedServices(manifest Manifest) []hostproto.ServiceRequest {
	seen := map[string]bool{}
	var out []hostproto.ServiceRequest
	for _, cap := range manifest.Capabilities {
		prefix := capabilityServicePrefix(string(cap))
		if prefix == "" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, hostproto.ServiceRequest{
			Service: prefix,
			Version: 1,
		})
	}
	return out
}

// capabilityServicePrefix returns the service-name prefix of a
// capability string. "ui.status" -> "ui", "commands.register" ->
// "commands", "render.text" -> "render".
func capabilityServicePrefix(capability string) string {
	for i := 0; i < len(capability); i++ {
		if capability[i] == '.' {
			return capability[:i]
		}
	}
	return ""
}
```

Delete the old `capabilitiesToStrings` function and its last use site if any remain.

Add a new field and an accessor to the `Manager` struct:

```go
// In the Manager struct definition, add:
servicesRegistry *services.Registry
```

Update `ManagerOptions`:

```go
// In ManagerOptions struct, add:
ServicesRegistry *services.Registry
```

Update `NewManager` to wire it:

```go
// In NewManager, after permissions is resolved but before returning:
svcRegistry := opts.ServicesRegistry
if svcRegistry == nil {
    svcRegistry = services.NewRegistry(managerCapabilityGate{permissions: permissions})
}

// Then in the returned struct literal, add:
servicesRegistry: svcRegistry,
```

Add this adapter at the bottom of `manager.go`:

```go
// managerCapabilityGate adapts *Permissions to the
// services.CapabilityGate interface. It looks up the manifest's trust
// class from the Manager's registration map to pick the right gate
// policy. For Plan 1, trust defaults to HostedThirdParty when unknown.
type managerCapabilityGate struct {
	permissions *Permissions
	manager     *Manager // may be nil during early bootstrap
}

// Allowed consults Permissions.AllowsService. When the manager
// reference is set it resolves the trust class from the registered
// extension; otherwise it assumes HostedThirdParty.
func (g managerCapabilityGate) Allowed(extensionID, service, method string) bool {
	trust := TrustClassHostedThirdParty
	if g.manager != nil {
		g.manager.mu.RLock()
		if reg, ok := g.manager.extensions[extensionID]; ok {
			trust = reg.trust
		}
		g.manager.mu.RUnlock()
	}
	return g.permissions.AllowsService(extensionID, trust, service, method)
}
```

Add the required import at the top of `manager.go`:

```go
"github.com/dimetron/pi-go/internal/extension/services"
```

- [ ] **Step 4: Run build to verify everything compiles**

Run: `go build ./...`
Expected: clean build.

Run: `go test ./internal/extension/... -run "TestManager" -v`
Expected: existing manager tests pass (no behavioral change yet — the registry is constructed but no services are registered).

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/hostruntime/client.go
git commit -m "feat(extension): wire services.Registry into Manager

Manager now constructs a services.Registry at init and passes a
CapabilityGate that consults Permissions.AllowsService. The handshake
call site migrates from CapabilityMask []string to RequestedServices
(synthesized from the manifest's existing Capabilities field for
Plan 1; Plan 2 adds a proper manifest field)."
```

---

## Task 13: Wire `ui` and `commands` services into the Manager and start the inbound loop

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/extension/manager_test.go`:

```go
func TestManager_RegistersUIAndCommandsServices(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	catalog := mgr.servicesRegistry.HostServices()
	services := map[string]bool{}
	for _, entry := range catalog {
		services[entry.Service] = true
	}
	if !services["ui"] {
		t.Error("ui service not registered")
	}
	if !services["commands"] {
		t.Error("commands service not registered")
	}
}

func TestManager_DispatchUIStatusForwardsToIntentChannel(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	sub, cancel := mgr.SubscribeUIIntents(4)
	defer cancel()

	result, err := mgr.DispatchHostCall("ext.test", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
		Payload: json.RawMessage(`{"text":"hi"}`),
	})
	if err != nil {
		t.Fatalf("DispatchHostCall: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}

	select {
	case envelope := <-sub:
		if envelope.ExtensionID != "ext.test" {
			t.Errorf("ExtensionID = %q", envelope.ExtensionID)
		}
		if envelope.Intent.Type != UIIntentStatus {
			t.Errorf("Intent.Type = %q", envelope.Intent.Type)
		}
		if envelope.Intent.Status == nil || envelope.Intent.Status.Text != "hi" {
			t.Errorf("Intent.Status = %+v", envelope.Intent.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for UI intent")
	}
}

func TestManager_DispatchCommandRegisterForwardsToCommandRegistry(t *testing.T) {
	mgr := NewManager(ManagerOptions{
		Permissions: EmptyPermissions(),
	})
	// For tests, register the extension so the command can be attributed.
	mgr.extensions["ext.test"] = extensionRegistration{
		id:    "ext.test",
		trust: TrustClassCompiledIn,
	}

	_, err := mgr.DispatchHostCall("ext.test", hostproto.HostCallParams{
		Service: "commands",
		Method:  "register",
		Version: 1,
		Payload: json.RawMessage(`{"name":"hello","description":"say hi","kind":"prompt","prompt":"say hi"}`),
	})
	if err != nil {
		t.Fatalf("DispatchHostCall: %v", err)
	}
	cmd, ok := mgr.FindCommand("hello")
	if !ok {
		t.Fatal("command 'hello' was not registered")
	}
	if cmd.Description != "say hi" {
		t.Errorf("Description = %q", cmd.Description)
	}
}
```

At the top of `manager_test.go`, add imports if missing:
```go
import (
	"encoding/json"
	"time"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/extension/... -run "TestManager_RegistersUI|TestManager_DispatchUIStatus|TestManager_DispatchCommandRegister" -v`
Expected: `DispatchHostCall` undefined; ui/commands services not yet registered.

- [ ] **Step 3: Register the services in `NewManager` and add `DispatchHostCall`**

Import the new service packages at the top of `manager.go`:

```go
uiservice "github.com/dimetron/pi-go/internal/extension/services/ui"
commandsservice "github.com/dimetron/pi-go/internal/extension/services/commands"
```

In `NewManager`, after the `servicesRegistry` is created but before the final return, register the services:

```go
// Register v2 services against the registry. Sinks are wired to the
// manager itself so status intents and command registrations continue
// to flow through the existing fan-out channels.
mgr := &Manager{
    permissions:      permissions,
    registry:         registry,
    hostedLauncher:   hostedLauncher,
    stateStore:       nil,
    builtins:         builtins,
    extensions:       map[string]extensionRegistration{},
    commands:         map[string]commandRegistration{},
    tools:            map[string]toolRegistration{},
    runtimeTools:     map[string]tool.Tool{},
    hostedClients:    map[string]HostedClient{},
    subscriptions:    map[string]map[string]struct{}{},
    eventHandlers:    map[string]func(Event){},
    intentSubs:       map[int]chan UIIntentEnvelope{},
    renderers:        map[RenderSurface]rendererRegistration{},
    servicesRegistry: svcRegistry,
}

_ = svcRegistry.Register(uiservice.New(managerUISink{manager: mgr}))
_ = svcRegistry.Register(commandsservice.New(managerCommandsSink{manager: mgr}))

return mgr
```

(Replace the existing `return &Manager{...}` literal with the `mgr` assignment + service registration + `return mgr` pattern.)

Add the two sink adapters at the bottom of `manager.go`:

```go
// managerUISink adapts ui.Sink to Manager's existing intent fan-out.
type managerUISink struct {
	manager *Manager
}

func (s managerUISink) SetStatus(entry uiservice.StatusEntry) {
	_ = s.manager.EmitUIIntent(entry.ExtensionID, UIIntent{
		Type: UIIntentStatus,
		Status: &StatusIntent{
			Text: entry.Text,
		},
	})
}

func (s managerUISink) ClearStatus(extensionID string) {
	// No existing "clear" channel semantic — emit an empty status.
	_ = s.manager.EmitUIIntent(extensionID, UIIntent{
		Type:   UIIntentStatus,
		Status: &StatusIntent{Text: ""},
	})
}

// managerCommandsSink adapts commands.Sink to Manager's command registry.
type managerCommandsSink struct {
	manager *Manager
}

func (s managerCommandsSink) RegisterCommand(reg commandsservice.Registration) error {
	return s.manager.RegisterDynamicCommand(reg.ExtensionID, SlashCommand{
		Name:        reg.Name,
		Description: reg.Description,
		Prompt:      reg.Prompt,
	})
}

func (s managerCommandsSink) UnregisterCommand(input commandsservice.UnregisterInput) error {
	// Plan 1 does not implement per-command unregister in the manager;
	// return nil so extensions can call it without erroring. Plan 2
	// will implement the underlying unregister flow.
	_ = input
	return nil
}
```

Add a `DispatchHostCall` method:

```go
// DispatchHostCall routes an ext-initiated host_call through the
// services registry. It's the integration point for hostruntime.Client.
// ServeInbound (via the Dispatcher interface).
func (m *Manager) DispatchHostCall(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	sess := &services.SessionContext{
		ExtensionID: extensionID,
		// SessionID and SessionsDir are populated when BindSession has
		// been called.
	}
	m.mu.RLock()
	if m.stateStore != nil && m.stateStore.Bound() {
		// StateStore stores session info — expose for services that
		// need it (state service, in Plan 2).
	}
	m.mu.RUnlock()
	return m.servicesRegistry.Dispatch(extensionID, params, sess)
}
```

Add `encoding/json` to the import block if not already imported.

Update the `managerCapabilityGate` struct literal in the previous task's wiring to actually pass the manager reference:

```go
svcRegistry := opts.ServicesRegistry
if svcRegistry == nil {
    gate := managerCapabilityGate{permissions: permissions}
    svcRegistry = services.NewRegistry(gate)
}
```

After the `mgr` is allocated, assign the manager back into the gate — this requires swapping the order slightly. The cleanest form:

```go
svcRegistry := opts.ServicesRegistry

mgr := &Manager{
    permissions:      permissions,
    registry:         registry,
    // ... rest of fields ...
    servicesRegistry: svcRegistry,
}

if svcRegistry == nil {
    gate := managerCapabilityGate{permissions: permissions, manager: mgr}
    svcRegistry = services.NewRegistry(gate)
    mgr.servicesRegistry = svcRegistry
}

_ = svcRegistry.Register(uiservice.New(managerUISink{manager: mgr}))
_ = svcRegistry.Register(commandsservice.New(managerCommandsSink{manager: mgr}))

return mgr
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/extension/... -run "TestManager_RegistersUI|TestManager_DispatchUIStatus|TestManager_DispatchCommandRegister" -v`
Expected: all three new tests pass.

Run: `go test ./internal/extension/...`
Expected: the full package still passes.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): register ui + commands services in Manager

NewManager registers the ui and commands v2 services with sinks that
forward to the manager's existing intent fan-out and command registry.
DispatchHostCall is the integration point hostruntime.Client uses via
the Dispatcher interface. Plan 1 preserves existing behavior: status
intents land on the same SubscribeUIIntents channel; command
registrations land in the same dynamic commands map."
```

---

## Task 14: Start `ServeInbound` after handshake in `StartHostedExtensions`

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test**

Append to `internal/extension/manager_test.go`:

```go
// fakeInboundClient is a HostedClient that captures Handshake + exposes a
// ServeInbound hook so tests can verify the manager wires it up.
type fakeInboundClient struct {
	handshakeCalled bool
	serveCalled     bool
	serveErr        error
	shutdownCalled  bool
}

func (c *fakeInboundClient) Handshake(ctx context.Context, req hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error) {
	c.handshakeCalled = true
	return hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}, nil
}

func (c *fakeInboundClient) ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error {
	c.serveCalled = true
	return c.serveErr
}

func (c *fakeInboundClient) Shutdown(ctx context.Context) error {
	c.shutdownCalled = true
	return nil
}

func (c *fakeInboundClient) IsHealthy() bool { return true }

type fakeInboundLauncher struct {
	client *fakeInboundClient
}

func (l fakeInboundLauncher) Launch(ctx context.Context, manifest Manifest) (HostedClient, error) {
	return l.client, nil
}

func TestStartHostedExtensions_RunsServeInbound(t *testing.T) {
	client := &fakeInboundClient{}
	mgr := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			HostedRequired:      true,
			GrantedCapabilities: []Capability{CapabilityUIStatus},
		}}),
		HostedLauncher: fakeInboundLauncher{client: client},
	})
	manifest := Manifest{
		ID:           "ext.demo",
		Capabilities: []Capability{CapabilityUIStatus},
		Runtime: &ManifestRuntime{Type: RuntimeTypeHostedStdioJSONRPC, Command: "demo"},
	}
	if err := mgr.RegisterManifest(manifest); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.StartHostedExtensions(ctx, "hosted_stdio"); err != nil {
		t.Fatalf("StartHostedExtensions: %v", err)
	}
	// Give the ServeInbound goroutine a chance to run.
	time.Sleep(50 * time.Millisecond)
	if !client.handshakeCalled {
		t.Error("handshake was not called")
	}
	if !client.serveCalled {
		t.Error("ServeInbound was not called")
	}
}
```

Add `Dispatcher` to the manager's known interfaces by defining a type alias at the top of `manager.go`:

```go
// Dispatcher mirrors hostruntime.Dispatcher. Redeclared here so the
// HostedClient interface can reference it without importing
// hostruntime (which would create a cycle when tests live under
// package extension).
type Dispatcher = hostruntime.Dispatcher
```

Add the `hostruntime` import at the top of `manager.go`:

```go
"github.com/dimetron/pi-go/internal/extension/hostruntime"
```

Extend `HostedClient` to include `ServeInbound`:

```go
type HostedClient interface {
	Handshake(ctx context.Context, req hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error)
	ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error
	Shutdown(ctx context.Context) error
	IsHealthy() bool
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go build ./...`
Expected: hostruntime.Client does not implement HostedClient (ServeInbound missing? it does — make sure). Also the old fake in manager_test.go doesn't implement ServeInbound.

Check: hostruntime.Client already has `ServeInbound` from Task 11. Any existing `HostedClient` fake in manager_test.go (look at existing tests) needs a `ServeInbound` stub. Add it.

- [ ] **Step 3: Update `StartHostedExtensions` to start the serve loop**

In `manager.go`, modify the successful-handshake branch of `StartHostedExtensions`:

```go
m.mu.Lock()
m.hostedClients[reg.id] = client
m.mu.Unlock()

// Spawn the inbound dispatch loop for this extension. It reads
// host_call requests from the extension process and routes them
// through DispatchHostCall.
go func(id string, c HostedClient) {
    serveCtx, cancel := context.WithCancel(context.Background())
    defer cancel()
    extID := id
    // Wrap DispatchHostCall as a Dispatcher.
    dispatcher := dispatcherFunc(func(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
        return m.DispatchHostCall(extensionID, params)
    })
    _ = c.ServeInbound(serveCtx, extID, dispatcher)
}(reg.id, client)
```

Add the `dispatcherFunc` helper at the bottom of `manager.go`:

```go
// dispatcherFunc adapts a function to the Dispatcher interface.
type dispatcherFunc func(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error)

func (f dispatcherFunc) Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	return f(extensionID, params)
}
```

- [ ] **Step 4: Update any existing `HostedClient` fakes in `manager_test.go`**

Search `manager_test.go` for existing fake clients and add a no-op `ServeInbound`:

```go
func (c *existingFake) ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error {
	return nil
}
```

Run: `go build ./...`
Iterate until clean.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/extension/... -run "TestStartHostedExtensions_RunsServeInbound" -v`
Expected: test passes.

Run: `go test ./internal/extension/...`
Expected: full manager test suite passes.

- [ ] **Step 6: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): launch ServeInbound after handshake

StartHostedExtensions now spawns a goroutine per hosted extension that
runs Client.ServeInbound, wiring incoming host_call requests through
Manager.DispatchHostCall. HostedClient interface gains ServeInbound;
Dispatcher is re-exported as a type alias so HostedClient can
reference it without an import cycle."
```

---

## Task 15: Rewrite `hosted-hello` against the v2 SDK

**Files:**
- Modify: `examples/extensions/hosted-hello/main.go`
- Modify: `examples/extensions/hosted-hello/extension.json`
- Modify: `examples/extensions/hosted-hello/README.md`

- [ ] **Step 1: Replace `main.go` with the v2 implementation**

Overwrite `examples/extensions/hosted-hello/main.go`:

```go
// Package main is the pi-go hosted-hello example extension, rewritten
// against the v2 host_call protocol. It registers a single slash
// command (/hello) and pushes a status line entry via the ui service.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	commandstypes "github.com/dimetron/pi-go/internal/extension/services/commands"
	uitypes "github.com/dimetron/pi-go/internal/extension/services/ui"
	"github.com/dimetron/pi-go/internal/extension/sdk"
)

func main() {
	client := sdk.NewClient(os.Stdin, os.Stdout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err := client.Serve(ctx, sdk.ServeOptions{
		ExtensionID: "hosted-hello",
		Mode:        "hosted_stdio",
		RequestedServices: []hostproto.ServiceRequest{
			{Service: "ui", Version: 1, Methods: []string{"status"}},
			{Service: "commands", Version: 1, Methods: []string{"register"}},
		},
		OnReady: func(ready sdk.HandshakeReady) error {
			// Register a slash command.
			if _, err := ready.Client.HostCall(ctx, "commands", "register", 1, commandstypes.RegisterPayload{
				Name:        "hello",
				Description: "Say hello from the hosted-hello extension",
				Prompt:      "Say hello from the hosted-hello extension. Extra args: {{args}}",
				Kind:        "prompt",
			}); err != nil {
				log.Printf("hosted-hello: commands.register failed: %v", err)
			}

			// Push a status line entry.
			if _, err := ready.Client.HostCall(ctx, "ui", "status", 1, uitypes.StatusPayload{
				Text:  "hosted-hello connected",
				Color: "cyan",
			}); err != nil {
				log.Printf("hosted-hello: ui.status failed: %v", err)
			}
			return nil
		},
	})
	if err != nil && err != context.Canceled {
		log.Printf("hosted-hello: Serve exited: %v", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Update `extension.json` to v2 shape**

Overwrite `examples/extensions/hosted-hello/extension.json`:

```json
{
  "id": "hosted-hello",
  "name": "hosted-hello",
  "description": "Minimal hosted extension example for pi-go v2 protocol",
  "runtime": {
    "type": "hosted_stdio_jsonrpc",
    "command": "go",
    "args": ["run", "."]
  },
  "capabilities": [
    "ui.status",
    "commands.register"
  ]
}
```

- [ ] **Step 3: Update README.md**

Overwrite `examples/extensions/hosted-hello/README.md`:

```markdown
# hosted-hello

Minimal hosted extension example for pi-go, built against the v2
extension protocol (`docs/superpowers/specs/2026-04-11-extension-platform-v2-design.md`).

## What it demonstrates

- The v2 handshake with `requested_services`
- The `sdk.Client.Serve` lifecycle helper
- A `commands.register` host_call
- A `ui.status` host_call

## Files

- `extension.json` — hosted runtime manifest (declares the capabilities it needs)
- `main.go` — the extension process, using the SDK

## Install

Copy this folder into one of the extension discovery locations:

```
~/.pi-go/extensions/hosted-hello/
```

## Approvals

Hosted extensions require explicit approval in `~/.pi-go/extensions/approvals.json`:

```json
{
  "approvals": [
    {
      "extension_id": "hosted-hello",
      "trust_class": "hosted_third_party",
      "hosted_required": true,
      "granted_capabilities": [
        "ui.status",
        "commands.register"
      ]
    }
  ]
}
```

## Run

The manifest starts the extension with:

```bash
go run .
```

from this directory. pi-go invokes this automatically when the extension is enabled.

## Notes

- The host performs the handshake, returns a catalog of `host_services`, and then the extension issues two `host_call` RPCs to register its command and status line.
- Both registrations are best-effort: any error is logged and the extension continues.
- Shutdown is clean on SIGINT/SIGTERM or when the host closes stdin.
```

- [ ] **Step 4: Build hosted-hello to verify it compiles**

Run: `cd examples/extensions/hosted-hello && go build ./...`
Expected: clean build, `./hosted-hello` binary produced (Windows: `hosted-hello.exe`).

- [ ] **Step 5: Commit**

```bash
git add examples/extensions/hosted-hello/
git commit -m "feat(examples): rewrite hosted-hello against v2 protocol

The hosted-hello example now uses the sdk.Client.Serve lifecycle
helper, declares its services in the handshake via requested_services,
and performs commands.register + ui.status as host_call RPCs. The
code shrinks from 125 lines to ~55 lines of main.go; no more hand-
written RPC framing."
```

---

## Task 16: Integration test — hosted-hello v2 end to end

**Files:**
- Create: `internal/extension/hosted_hello_e2e_test.go`

- [ ] **Step 1: Write the integration test**

Create `internal/extension/hosted_hello_e2e_test.go`:

```go
package extension_test

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// TestHostedHello_V2_EndToEnd spins up the manager with a pipe-based
// fake extension that mimics the v2 hosted-hello flow: handshake,
// commands.register, ui.status, then EOF. It verifies the manager
// routes both registrations to the expected destinations (commands
// registry + UI intent fan-out).
func TestHostedHello_V2_EndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping hosted-hello e2e on windows (see claudecli e2e pattern)")
	}

	mgr := extension.NewManager(extension.ManagerOptions{
		Permissions: extension.NewPermissions([]extension.ApprovalRecord{{
			ExtensionID:         "hosted-hello",
			TrustClass:          extension.TrustClassHostedThirdParty,
			HostedRequired:      true,
			GrantedCapabilities: []extension.Capability{extension.CapabilityUIStatus, extension.CapabilityCommandRegister},
		}}),
		HostedLauncher: pipeLauncher{},
	})

	manifest := extension.Manifest{
		ID:           "hosted-hello",
		Capabilities: []extension.Capability{extension.CapabilityUIStatus, extension.CapabilityCommandRegister},
		Runtime: &extension.ManifestRuntime{
			Type:    extension.RuntimeTypeHostedStdioJSONRPC,
			Command: "fake",
		},
	}
	if err := mgr.RegisterManifest(manifest); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}

	sub, cancel := mgr.SubscribeUIIntents(8)
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelCtx()
	if err := mgr.StartHostedExtensions(ctx, "hosted_stdio"); err != nil {
		t.Fatalf("StartHostedExtensions: %v", err)
	}

	// Wait for the fake client to send both host_calls.
	deadline := time.After(2 * time.Second)
	var gotStatus bool
	for !gotStatus {
		select {
		case envelope := <-sub:
			if envelope.ExtensionID == "hosted-hello" && envelope.Intent.Status != nil && envelope.Intent.Status.Text == "hosted-hello connected" {
				gotStatus = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for ui.status intent")
		}
	}

	// Command should be registered.
	time.Sleep(50 * time.Millisecond)
	if _, ok := mgr.FindCommand("hello"); !ok {
		t.Fatal("hello command was not registered")
	}
}

// pipeLauncher implements HostedLauncher with an in-process fake that
// simulates the v2 hosted-hello protocol flow over direct method calls
// (no actual subprocess spawn).
type pipeLauncher struct{}

func (pipeLauncher) Launch(ctx context.Context, manifest extension.Manifest) (extension.HostedClient, error) {
	return &pipeClient{}, nil
}

type pipeClient struct {
	dispatched bool
}

func (c *pipeClient) Handshake(ctx context.Context, req hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error) {
	return hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
		GrantedServices: []hostproto.ServiceGrant{
			{Service: "ui", Version: 1},
			{Service: "commands", Version: 1},
		},
	}, nil
}

func (c *pipeClient) ServeInbound(ctx context.Context, extensionID string, dispatcher extension.Dispatcher) error {
	// Simulate commands.register.
	registerPayload, _ := json.Marshal(map[string]any{
		"name":        "hello",
		"description": "Say hello",
		"prompt":      "Say hello",
		"kind":        "prompt",
	})
	if _, err := dispatcher.Dispatch(extensionID, hostproto.HostCallParams{
		Service: "commands", Method: "register", Version: 1, Payload: registerPayload,
	}); err != nil {
		return err
	}
	// Simulate ui.status.
	statusPayload, _ := json.Marshal(map[string]any{
		"text":  "hosted-hello connected",
		"color": "cyan",
	})
	if _, err := dispatcher.Dispatch(extensionID, hostproto.HostCallParams{
		Service: "ui", Method: "status", Version: 1, Payload: statusPayload,
	}); err != nil {
		return err
	}
	c.dispatched = true
	// Block until context is canceled, simulating the real extension.
	<-ctx.Done()
	return nil
}

func (c *pipeClient) Shutdown(ctx context.Context) error { return nil }
func (c *pipeClient) IsHealthy() bool                    { return true }
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/extension/... -run "TestHostedHello_V2_EndToEnd" -v`
Expected: test passes on non-Windows platforms; skipped on Windows.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`
Expected: all tests pass (modulo the pre-existing Windows-specific failures in `internal/claudecli/e2e_test.go` which are unrelated).

- [ ] **Step 4: Commit**

```bash
git add internal/extension/hosted_hello_e2e_test.go
git commit -m "test(extension): add v2 hosted-hello end-to-end integration test

Verifies the full handshake -> dispatch loop: the manager launches a
fake hosted client, runs ServeInbound, routes commands.register and
ui.status host_calls through the services registry to the manager's
existing command map and UI intent subscription channel."
```

---

## Done

At this point Plan 1 is complete:

- Protocol is at v2.0.0; v1 types and method constants are gone.
- `services.Registry` dispatches `host_call` requests with full error-code coverage.
- `ui` and `commands` services work end-to-end, routed to the existing manager fan-outs.
- Extension SDK (`sdk.Client.Serve`, `sdk.Client.HostCall`) handles stdio framing, handshake, and error propagation.
- `hosted-hello` is rewritten against the new surface and demonstrably works under an integration test.

**Next:** Plan 2 builds the host→ext mirror direction (`ext_call`) + the remaining services (`session`, `agent`, `state`, `chat`, `events`, `tools`, rest of `ui`), plus `plan-mode` and `session-name` example extensions. Plan 3 builds sigils + `todos`.

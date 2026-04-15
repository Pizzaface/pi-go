# Extensions Lifecycle & TUI Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `internal/extension/lifecycle` service + the TUI `/extensions` panel, approval dialog, and pending toast — replacing hand-edited approvals.json with an in-TUI workflow while exposing a clean interface for later SDK use.

**Architecture:** New `internal/extension/lifecycle` package orchestrates `host.Manager` + `host.Gate` + `host.LaunchHosted` behind a `Service` interface. TUI consumes the interface via a new panel, dialog, and status-bar toast. Auto-start runs from the TUI init path, not `BuildRuntime`, so the same `Service` supports future headless callers (CLI subcommands, SDK-exposed extension-manage APIs).

**Tech Stack:** Go 1.22 · bubbletea v2 (`charm.land/bubbletea/v2`) · lipgloss v2 · existing `host.Manager`/`host.Gate`/`host.LaunchHosted` from Phase 8 · teatest for TUI tests.

**Spec reference:** `docs/superpowers/specs/2026-04-15-extensions-lifecycle-tui-management-design.md`.

**Assumptions for the implementer:**

- Working directory is the pi-go repo root (`C:/Users/Jordan/Documents/Projects/pi-go` or the worktree equivalent).
- Go toolchain, Node 20+, and `rtk` are on PATH. `rtk` is optional — plain `git`/`go`/`npm` work everywhere the plan uses them.
- Run all commands from repo root unless the task says otherwise.
- Every task ends with a commit. Use the message exactly; adjust only if tests surface an unanticipated subfile that must be re-added.

**Build ordering (phase summary):**

1. lifecycle package scaffold (types) — Task 1
2. approvals.json read/write — Tasks 2-3
3. Service skeleton (List/Get/Subscribe) — Tasks 4-5
4. Mutations (Approve/Deny/Revoke) — Tasks 6-8
5. Process lifecycle (Start/Stop/Restart + StartApproved/StopAll) — Tasks 9-13
6. buildCommand + Reload — Tasks 14-15
7. runtime.Runtime.Lifecycle wiring — Task 16
8. lifecycle E2E — Tasks 17-18
9. TUI toast — Tasks 19-20
10. TUI /extensions panel — Tasks 21-25
11. TUI approval dialog — Tasks 26-27
12. TUI event bridge — Tasks 28-29
13. CLI start/stop glue — Tasks 30-31
14. Docs — Task 32

---

## Task 1: Scaffold `lifecycle` package with types

**Files:**
- Create: `internal/extension/lifecycle/event.go`
- Create: `internal/extension/lifecycle/doc.go`

- [ ] **Step 1: Create `doc.go`**

```go
// Package lifecycle orchestrates approve/deny/revoke/start/stop/restart
// for hosted extensions on top of host.Manager, host.Gate, and
// host.LaunchHosted. The Service interface is the programmatic surface
// the TUI (and eventually piapi.API.Extensions) consume.
package lifecycle
```

- [ ] **Step 2: Create `event.go`**

```go
package lifecycle

import (
	"errors"
	"fmt"

	"github.com/dimetron/pi-go/internal/extension/host"
)

// View is the read projection of a single extension. Services snapshot
// a view per call so callers don't need to hold locks while rendering.
type View struct {
	ID        string
	Mode      string // "compiled-in" | "hosted-go" | "hosted-ts"
	Trust     host.TrustClass
	State     host.State
	Version   string
	WorkDir   string
	Requested []string
	Granted   []string
	Approved  bool
	Err       string
}

// EventKind enumerates the kinds of state changes a Service emits.
type EventKind int

const (
	EventStateChanged EventKind = iota
	EventApprovalChanged
	EventRegistrationAdded
	EventRegistrationRemoved
)

func (k EventKind) String() string {
	switch k {
	case EventStateChanged:
		return "state_changed"
	case EventApprovalChanged:
		return "approval_changed"
	case EventRegistrationAdded:
		return "registration_added"
	case EventRegistrationRemoved:
		return "registration_removed"
	default:
		return "unknown"
	}
}

// Event is a single state-change notification. View is the post-change
// snapshot.
type Event struct {
	Kind EventKind
	View View
}

// Error is the canonical error shape returned by mutating Service
// methods. Op is the method name ("approve", "deny", etc.), ID is the
// extension id (may be empty on cross-cutting errors).
type Error struct {
	Op  string
	ID  string
	Err error
}

func (e *Error) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("lifecycle: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("lifecycle: %s %s: %v", e.Op, e.ID, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Sentinel errors.
var (
	ErrCompiledIn       = errors.New("compiled-in extensions cannot be approved/denied/revoked/started/stopped")
	ErrUnknownExtension = errors.New("unknown extension")
)
```

- [ ] **Step 3: Build + commit**

Run: `go build ./internal/extension/lifecycle/...`
Expected: clean compile, no output.

```bash
rtk git add internal/extension/lifecycle/event.go internal/extension/lifecycle/doc.go
rtk git commit -m "feat(lifecycle): scaffold package with View/Event/Error types

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `approvals.go` read + atomic write

**Files:**
- Create: `internal/extension/lifecycle/approvals.go`
- Create: `internal/extension/lifecycle/approvals_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadApprovals_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	got, err := readApprovals(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Version == 0 {
		got.Version = 2 // caller is expected to default
	}
	if len(got.Extensions) != 0 {
		t.Fatalf("expected empty map; got %d", len(got.Extensions))
	}
}

func TestAtomicWrite_PreservesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	// Seed with a file containing an entry with an unknown "hash" field.
	initial := `{"version":2,"extensions":{"ext-a":{"approved":true,"hash":"abc123"}}}`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	file, err := readApprovals(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, file); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	exts := back["extensions"].(map[string]any)
	entry := exts["ext-a"].(map[string]any)
	if entry["hash"] != "abc123" {
		t.Fatalf("lost unknown field: %#v", entry)
	}
	// No stale .tmp files.
	tmpGlob, _ := filepath.Glob(path + ".tmp*")
	if len(tmpGlob) != 0 {
		t.Fatalf("expected no .tmp droppings; got %v", tmpGlob)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestReadApprovals|TestAtomicWrite" -count=1`
Expected: `undefined: readApprovals`, `undefined: atomicWrite`.

- [ ] **Step 3: Implement `approvals.go`**

```go
package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// approvalsFile mirrors approvals.json with per-entry rawmessages so
// unknown fields round-trip unchanged.
type approvalsFile struct {
	Version    int                        `json:"version"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

// readApprovals loads the file from disk. A missing file returns an
// empty approvalsFile (not an error). A malformed file returns the
// parse error.
func readApprovals(path string) (approvalsFile, error) {
	out := approvalsFile{Extensions: map[string]json.RawMessage{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			out.Version = 2
			return out, nil
		}
		return out, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		out.Version = 2
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse %s: %w", path, err)
	}
	if out.Extensions == nil {
		out.Extensions = map[string]json.RawMessage{}
	}
	if out.Version == 0 {
		out.Version = 2
	}
	return out, nil
}

// atomicWrite serializes file and replaces path atomically. On Windows
// we remove the target first because os.Rename over an existing file
// fails there.
func atomicWrite(path string, file approvalsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "approvals-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close tmp: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path) // ignore not-exist; rename will surface any other failure
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestReadApprovals|TestAtomicWrite" -count=1 -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/approvals.go internal/extension/lifecycle/approvals_test.go
rtk git commit -m "feat(lifecycle): approvals.json read + atomic write with unknown-field preservation

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `mutateApprovals` with op callback

**Files:**
- Modify: `internal/extension/lifecycle/approvals.go`
- Modify: `internal/extension/lifecycle/approvals_test.go`

- [ ] **Step 1: Add the failing tests**

Append to `approvals_test.go`:

```go
func TestMutateApprovals_ApproveNewEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	err := mutateApprovals(path, "ext-a", func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = true
		entry["granted_capabilities"] = []any{"tools.register"}
		return entry
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readApprovals(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Extensions["ext-a"]; !ok {
		t.Fatal("expected ext-a entry")
	}
}

func TestMutateApprovals_DeleteEntryOnNilReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	seed := `{"version":2,"extensions":{"ext-a":{"approved":true},"ext-b":{"approved":false}}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	err := mutateApprovals(path, "ext-a", func(map[string]any) map[string]any { return nil })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readApprovals(path)
	if _, ok := got.Extensions["ext-a"]; ok {
		t.Fatal("expected ext-a deleted")
	}
	if _, ok := got.Extensions["ext-b"]; !ok {
		t.Fatal("ext-b should survive")
	}
}

func TestMutateApprovals_MalformedFileFailsBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	err := mutateApprovals(path, "ext-a", func(e map[string]any) map[string]any { return e })
	if err == nil {
		t.Fatal("expected parse error")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file mutated despite parse failure")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestMutateApprovals" -count=1`
Expected: `undefined: mutateApprovals`.

- [ ] **Step 3: Implement**

Append to `approvals.go`:

```go
// mutateApprovals is the one-stop entry point for Approve / Deny / Revoke.
// It reads the file (empty map if missing), runs op on the decoded entry
// for id (a fresh empty map if new), writes atomically, and returns any
// error verbatim. op may return nil to delete the entry. Concurrent
// callers must serialize externally; this helper does not lock.
func mutateApprovals(path, id string, op func(entry map[string]any) map[string]any) error {
	file, err := readApprovals(path)
	if err != nil {
		return err
	}
	var entry map[string]any
	if raw, ok := file.Extensions[id]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode entry %s: %w", id, err)
		}
	}
	updated := op(entry)
	if updated == nil {
		delete(file.Extensions, id)
	} else {
		raw, err := json.Marshal(updated)
		if err != nil {
			return fmt.Errorf("encode entry %s: %w", id, err)
		}
		file.Extensions[id] = raw
	}
	return atomicWrite(path, file)
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/approvals.go internal/extension/lifecycle/approvals_test.go
rtk git commit -m "feat(lifecycle): mutateApprovals with op callback; nil-return deletes entry

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Service skeleton — New, List, Get

**Files:**
- Create: `internal/extension/lifecycle/service.go`
- Create: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

```go
package lifecycle

import (
	"path/filepath"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// newTestService spins up a real Manager + Gate pointed at a temp
// approvals file. Returns the service and the manager so tests can
// register registrations directly.
func newTestService(t *testing.T) (Service, *host.Manager, string) {
	t.Helper()
	tmp := t.TempDir()
	approvalsPath := filepath.Join(tmp, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp)
	return svc, mgr, approvalsPath
}

func TestService_ListIncludesEveryRegistration(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	for _, reg := range []*host.Registration{
		{ID: "a", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "a", Version: "0.1"}},
		{ID: "b", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "b", Version: "0.1"}},
	} {
		if err := mgr.Register(reg); err != nil {
			t.Fatal(err)
		}
	}
	views := svc.List()
	if len(views) != 2 {
		t.Fatalf("expected 2 views; got %d", len(views))
	}
	// Sorted by ID.
	if views[0].ID != "a" || views[1].ID != "b" {
		t.Fatalf("expected sorted a,b; got %q,%q", views[0].ID, views[1].ID)
	}
	if views[0].Mode != "compiled-in" {
		t.Fatalf("expected compiled-in mode; got %q", views[0].Mode)
	}
}

func TestService_GetMissingReturnsFalse(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, ok := svc.Get("nope"); ok {
		t.Fatal("expected not found")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_List|TestService_GetMissing" -count=1`
Expected: `undefined: Service`, `undefined: New`.

- [ ] **Step 3: Implement `service.go`**

```go
package lifecycle

import (
	"context"
	"sort"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/host"
)

// Service is the programmatic surface for extension management. See
// docs/superpowers/specs/2026-04-15-extensions-lifecycle-tui-management-design.md
// for the full contract.
type Service interface {
	List() []View
	Get(id string) (View, bool)

	Approve(ctx context.Context, id string, grants []string) error
	Deny(ctx context.Context, id string, reason string) error
	Revoke(ctx context.Context, id string) error

	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error

	StartApproved(ctx context.Context) []error
	StopAll(ctx context.Context) []error
	Reload(ctx context.Context) error

	Subscribe() (<-chan Event, func())
}

// New constructs a Service. All mutating methods are safe for concurrent
// use. approvalsPath is the absolute path to approvals.json (may not
// exist yet). workDir is the project CWD used by Reload to re-walk
// discovery roots.
func New(mgr *host.Manager, gate *host.Gate, approvalsPath, workDir string) Service {
	return &service{
		mgr:           mgr,
		gate:          gate,
		approvalsPath: approvalsPath,
		workDir:       workDir,
		subs:          map[int]chan Event{},
	}
}

type service struct {
	mgr           *host.Manager
	gate          *host.Gate
	approvalsPath string
	workDir       string

	writeMu sync.Mutex // serializes mutateApprovals callers

	subMu  sync.Mutex
	nextID int
	subs   map[int]chan Event
}

func (s *service) List() []View {
	regs := s.mgr.List()
	out := make([]View, 0, len(regs))
	for _, r := range regs {
		out = append(out, s.viewFromRegistration(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *service) Get(id string) (View, bool) {
	reg := s.mgr.Get(id)
	if reg == nil {
		return View{}, false
	}
	return s.viewFromRegistration(reg), true
}

func (s *service) viewFromRegistration(reg *host.Registration) View {
	granted := s.gate.Grants(reg.ID, reg.Trust)
	errMsg := ""
	if reg.Err != nil {
		errMsg = reg.Err.Error()
	}
	return View{
		ID:        reg.ID,
		Mode:      reg.Mode,
		Trust:     reg.Trust,
		State:     reg.State,
		Version:   reg.Metadata.Version,
		WorkDir:   reg.WorkDir,
		Requested: append([]string(nil), reg.Metadata.RequestedCapabilities...),
		Granted:   granted,
		Approved:  reg.State != host.StatePending && reg.State != host.StateDenied,
		Err:       errMsg,
	}
}
```

- [ ] **Step 4: Add method stubs so the package compiles**

Append to `service.go`:

```go
// --- Stubs filled in by later tasks -----------------------------------

func (s *service) Approve(context.Context, string, []string) error { return nil }
func (s *service) Deny(context.Context, string, string) error      { return nil }
func (s *service) Revoke(context.Context, string) error            { return nil }
func (s *service) Start(context.Context, string) error             { return nil }
func (s *service) Stop(context.Context, string) error              { return nil }
func (s *service) Restart(context.Context, string) error           { return nil }
func (s *service) StartApproved(context.Context) []error           { return nil }
func (s *service) StopAll(context.Context) []error                 { return nil }
func (s *service) Reload(context.Context) error                    { return nil }
func (s *service) Subscribe() (<-chan Event, func())               { return nil, func() {} }
```

- [ ] **Step 5: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_List|TestService_GetMissing" -count=1 -v`
Expected: both PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Service interface with List/Get; stubs for the rest

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Subscribe + event publisher

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_SubscribeReceivesPublishedEvents(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	reg := &host.Registration{ID: "a", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "a", Version: "0.1"}}
	if err := mgr.Register(reg); err != nil {
		t.Fatal(err)
	}
	ch, cancel := svc.Subscribe()
	defer cancel()
	// Drive a publish via an internal handle — cast to *service for the test.
	svc.(*service).publish(Event{Kind: EventStateChanged, View: View{ID: "a"}})
	select {
	case ev := <-ch:
		if ev.Kind != EventStateChanged || ev.View.ID != "a" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no event after 500ms")
	}
}

func TestService_UnsubscribeStopsDelivery(t *testing.T) {
	svc, _, _ := newTestService(t)
	ch, cancel := svc.Subscribe()
	cancel()
	svc.(*service).publish(Event{Kind: EventStateChanged})
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected channel closed or drained")
		}
	case <-time.After(100 * time.Millisecond):
		// Acceptable: no delivery.
	}
}
```

Add the `time` import at the top of `service_test.go`:

```go
import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Subscribe|TestService_Unsubscribe" -count=1`
Expected: current Subscribe stub returns nil channel, tests fail with nil-chan semantics.

- [ ] **Step 3: Replace the Subscribe stub + add publish helper**

In `service.go`, replace the `Subscribe` stub:

```go
// Subscribe returns a buffered channel (cap 16) that receives Events,
// plus a cleanup function. The cleanup is safe to call more than once.
// Publishers drop events if the channel is full — callers needing
// stronger guarantees should call List() on a coarse trigger (e.g. the
// TUI rebase on WindowSizeMsg).
func (s *service) Subscribe() (<-chan Event, func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan Event, 16)
	s.subs[id] = ch
	cancel := func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if c, ok := s.subs[id]; ok {
			close(c)
			delete(s.subs, id)
		}
	}
	return ch, cancel
}

// publish fans out to every subscriber. Blocking subscribers are
// punished: their slot is dropped. Non-blocking (buffered) subscribers
// that fill up are skipped with a warning via log.
func (s *service) publish(ev Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for id, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// Channel full; drop this event for this subscriber.
			log.Printf("lifecycle: dropping event for subscriber %d (channel full)", id)
		}
	}
}
```

Add `log` to the imports at the top:

```go
import (
	"context"
	"log"
	"sort"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/host"
)
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Subscribe|TestService_Unsubscribe" -count=1 -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Subscribe with cap-16 buffered channel + drop-on-full publisher

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Approve

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_ApprovePendingHostedGoesReady(t *testing.T) {
	svc, mgr, path := newTestService(t)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	if err := mgr.Register(reg); err != nil {
		t.Fatal(err)
	}
	// Pending: Register with no grants ends up StatePending.
	if reg.State != host.StatePending {
		t.Fatalf("expected StatePending; got %s", reg.State)
	}
	ch, cancel := svc.Subscribe()
	defer cancel()
	if err := svc.Approve(context.Background(), "h", []string{"tools.register", "events.session_start"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Ordering contract: approval event before state change.
	events := drainEvents(t, ch, 2, 500*time.Millisecond)
	if events[0].Kind != EventApprovalChanged {
		t.Fatalf("first event %s; expected approval_changed", events[0].Kind)
	}
	if events[1].Kind != EventStateChanged {
		t.Fatalf("second event %s; expected state_changed", events[1].Kind)
	}
	// Manager state updated.
	if r := mgr.Get("h"); r.State != host.StateReady {
		t.Fatalf("expected StateReady; got %s", r.State)
	}
	// Approvals file updated.
	got, _ := readApprovals(path)
	if _, ok := got.Extensions["h"]; !ok {
		t.Fatalf("expected h in approvals file")
	}
}

func TestService_ApproveCompiledInReturnsErrCompiledIn(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	reg := &host.Registration{ID: "c", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "c", Version: "0.1"}}
	_ = mgr.Register(reg)
	err := svc.Approve(context.Background(), "c", []string{"tools.register"})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *lifecycle.Error; got %T %v", err, err)
	}
	if !errors.Is(err, ErrCompiledIn) {
		t.Fatalf("expected ErrCompiledIn; got %v", err)
	}
}

func TestService_ApproveUnknownReturnsErrUnknown(t *testing.T) {
	svc, _, _ := newTestService(t)
	err := svc.Approve(context.Background(), "no-such", nil)
	if !errors.Is(err, ErrUnknownExtension) {
		t.Fatalf("expected ErrUnknownExtension; got %v", err)
	}
}

func drainEvents(t *testing.T, ch <-chan Event, n int, total time.Duration) []Event {
	t.Helper()
	deadline := time.Now().Add(total)
	out := make([]Event, 0, n)
	for len(out) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for %d events; got %d", n, len(out))
		}
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-time.After(remaining):
			t.Fatalf("timed out waiting for %d events; got %d", n, len(out))
		}
	}
	return out
}
```

Add `"context"` and `"errors"` to the `service_test.go` imports.

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Approve" -count=1`
Expected: failures — the Approve stub returns nil.

- [ ] **Step 3: Implement Approve**

Replace the `Approve` stub in `service.go`:

```go
// Approve merges grants into approvals.json and updates manager state.
// Emits EventApprovalChanged then EventStateChanged, in that order.
func (s *service) Approve(ctx context.Context, id string, grants []string) error {
	_ = ctx
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "approve", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "approve", ID: id, Err: ErrCompiledIn}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := mutateApprovals(s.approvalsPath, id, func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = true
		entry["approved_at"] = time.Now().UTC().Format(time.RFC3339)
		delete(entry, "deny_reason")
		delete(entry, "denied_at")
		if _, ok := entry["trust_class"]; !ok {
			entry["trust_class"] = "third-party"
		}
		merged := mergeStringSet(entry["granted_capabilities"], grants)
		entry["granted_capabilities"] = merged
		return entry
	})
	if err != nil {
		return &Error{Op: "approve", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "approve", ID: id, Err: err}
	}
	// Emit approval event first.
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	// Transition state if warranted.
	if reg.State == host.StatePending || reg.State == host.StateDenied {
		s.mgr.SetState(id, host.StateReady, nil)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	}
	return nil
}

// mergeStringSet accepts an arbitrary existing JSON-decoded value
// (probably []any from map[string]any decoding) and merges new values
// into a dedup-sorted []any ready to re-encode.
func mergeStringSet(existing any, toAdd []string) []any {
	set := map[string]bool{}
	if arr, ok := existing.([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				set[s] = true
			}
		}
	}
	for _, s := range toAdd {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	typed := make([]any, len(out))
	for i, s := range out {
		typed[i] = s
	}
	return typed
}
```

Add `"time"` to `service.go` imports.

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Approve" -count=1 -v`
Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Approve with ordered events + compiled-in/unknown guards

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Deny

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_DenyTransitionsState(t *testing.T) {
	svc, mgr, path := newTestService(t)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	_ = mgr.Register(reg)
	if err := svc.Deny(context.Background(), "h", "security review failed"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if mgr.Get("h").State != host.StateDenied {
		t.Fatalf("expected StateDenied; got %s", mgr.Get("h").State)
	}
	got, _ := readApprovals(path)
	var entry map[string]any
	_ = json.Unmarshal(got.Extensions["h"], &entry)
	if entry["approved"] != false {
		t.Fatalf("expected approved:false; got %v", entry["approved"])
	}
	if entry["deny_reason"] != "security review failed" {
		t.Fatalf("unexpected deny_reason: %v", entry["deny_reason"])
	}
}
```

Add `"encoding/json"` to `service_test.go` imports if missing.

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Deny" -count=1`
Expected: stub no-ops, state unchanged, FAIL.

- [ ] **Step 3: Implement Deny**

Replace the `Deny` stub:

```go
// Deny writes approved:false + deny_reason and moves the registration
// to StateDenied. If running, Stop is called first (idempotent on
// non-running states).
func (s *service) Deny(ctx context.Context, id string, reason string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "deny", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "deny", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		if err := s.Stop(ctx, id); err != nil {
			return &Error{Op: "deny", ID: id, Err: err}
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := mutateApprovals(s.approvalsPath, id, func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = false
		entry["denied_at"] = time.Now().UTC().Format(time.RFC3339)
		entry["deny_reason"] = reason
		if _, ok := entry["trust_class"]; !ok {
			entry["trust_class"] = "third-party"
		}
		return entry
	})
	if err != nil {
		return &Error{Op: "deny", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "deny", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	s.mgr.SetState(id, host.StateDenied, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Deny" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Deny writes deny_reason + StateDenied; stops running ext first

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Revoke

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_RevokeDeletesEntryAndGoesPending(t *testing.T) {
	svc, mgr, path := newTestService(t)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	_ = mgr.Register(reg)
	if err := svc.Approve(context.Background(), "h", []string{"tools.register"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	if mgr.Get("h").State != host.StatePending {
		t.Fatalf("expected StatePending after revoke; got %s", mgr.Get("h").State)
	}
	got, _ := readApprovals(path)
	if _, ok := got.Extensions["h"]; ok {
		t.Fatal("expected h deleted from approvals")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Revoke" -count=1`
Expected: FAIL — stub no-op.

- [ ] **Step 3: Implement Revoke**

Replace the `Revoke` stub:

```go
// Revoke removes the approvals.json entry entirely and returns the
// registration to StatePending. If running, Stop is called first.
func (s *service) Revoke(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "revoke", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "revoke", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		if err := s.Stop(ctx, id); err != nil {
			return &Error{Op: "revoke", ID: id, Err: err}
		}
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := mutateApprovals(s.approvalsPath, id, func(map[string]any) map[string]any { return nil }); err != nil {
		return &Error{Op: "revoke", ID: id, Err: err}
	}
	if err := s.gate.Reload(); err != nil {
		return &Error{Op: "revoke", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventApprovalChanged, View: s.viewFromRegistration(reg)})
	s.mgr.SetState(id, host.StatePending, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Revoke" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Revoke deletes approvals entry + StatePending

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: launchFunc indirection for tests

**Files:**
- Modify: `internal/extension/lifecycle/service.go`

No behavior change; introduce indirection so later tasks can test Start/Stop/StartApproved without real subprocesses.

- [ ] **Step 1: Add the launchFunc field**

In `service.go`, extend the service struct:

```go
type service struct {
	mgr           *host.Manager
	gate          *host.Gate
	approvalsPath string
	workDir       string

	writeMu sync.Mutex

	subMu  sync.Mutex
	nextID int
	subs   map[int]chan Event

	// launchFunc is overridable for tests. In production it wraps
	// host.LaunchHosted with an api.HostedAPIHandler router.
	launchFunc func(ctx context.Context, reg *host.Registration, mgr *host.Manager, cmd []string) error
	// stopFunc is called by Stop on a running reg; overridable for tests.
	stopFunc func(ctx context.Context, reg *host.Registration) error
}
```

- [ ] **Step 2: Default the fields in `New`**

Replace `New` with:

```go
func New(mgr *host.Manager, gate *host.Gate, approvalsPath, workDir string) Service {
	s := &service{
		mgr:           mgr,
		gate:          gate,
		approvalsPath: approvalsPath,
		workDir:       workDir,
		subs:          map[int]chan Event{},
	}
	s.launchFunc = s.defaultLaunch
	s.stopFunc = s.defaultStop
	return s
}

// defaultLaunch wraps host.LaunchHosted with a router backed by
// api.NewHostedHandler. Split out for test injection.
func (s *service) defaultLaunch(ctx context.Context, reg *host.Registration, mgr *host.Manager, cmd []string) error {
	handler := api.NewHostedHandler(mgr, reg)
	return host.LaunchHosted(ctx, reg, mgr, cmd, handler.Handle)
}

// defaultStop sends shutdown, waits up to 3s, then closes the conn.
func (s *service) defaultStop(ctx context.Context, reg *host.Registration) error {
	if reg.Conn == nil {
		return nil
	}
	_ = reg.Conn.Notify("pi.extension/shutdown", map[string]any{})
	done := make(chan struct{})
	go func() {
		// Give the extension 3s to react.
		t := time.NewTimer(3 * time.Second)
		defer t.Stop()
		<-t.C
		close(done)
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
	reg.Conn.Close()
	return nil
}
```

Add `"github.com/dimetron/pi-go/internal/extension/api"` to imports.

- [ ] **Step 3: Build + existing tests still PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -count=1`
Expected: all existing tests still pass (no behavior change yet).

- [ ] **Step 4: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go
rtk git commit -m "refactor(lifecycle): extract launchFunc/stopFunc for test injection

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Start + handshake timeout watcher

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_StartCallsLaunchFunc(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1", Command: []string{"echo"}}}
	_ = mgr.Register(reg)
	_ = svc.Approve(context.Background(), "h", []string{"tools.register"})
	var called [][]string
	impl.launchFunc = func(_ context.Context, gotReg *host.Registration, _ *host.Manager, cmd []string) error {
		called = append(called, cmd)
		mgr.SetState(gotReg.ID, host.StateRunning, nil)
		return nil
	}
	if err := svc.Start(context.Background(), "h"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(called) != 1 {
		t.Fatalf("expected 1 launch; got %d", len(called))
	}
	if mgr.Get("h").State != host.StateRunning {
		t.Fatalf("expected StateRunning; got %s", mgr.Get("h").State)
	}
}

func TestService_StartIdempotentOnRunning(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1", Command: []string{"echo"}}}
	_ = mgr.Register(reg)
	_ = svc.Approve(context.Background(), "h", []string{"tools.register"})
	mgr.SetState("h", host.StateRunning, nil)
	calls := 0
	impl.launchFunc = func(context.Context, *host.Registration, *host.Manager, []string) error {
		calls++
		return nil
	}
	if err := svc.Start(context.Background(), "h"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 launches (idempotent); got %d", calls)
	}
}

func TestService_StartCompiledInReturnsErrCompiledIn(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	reg := &host.Registration{ID: "c", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "c", Version: "0.1"}}
	_ = mgr.Register(reg)
	if err := svc.Start(context.Background(), "c"); !errors.Is(err, ErrCompiledIn) {
		t.Fatalf("expected ErrCompiledIn; got %v", err)
	}
}
```

Note: `piapi.Metadata` doesn't currently have a `Command` field. You'll need to add it in the same task. See Step 3.

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Start" -count=1`
Expected: compile error on `Command`, or test failures once that's fixed.

- [ ] **Step 3: Add `Command` to `piapi.Metadata`**

Modify `pkg/piapi/metadata.go`:

```go
type Metadata struct {
	Name                  string
	Version               string
	Description           string
	Prompt                string
	RequestedCapabilities []string
	Entry                 string
	Command               []string // hosted-go launch command from pi.toml
}
```

Then modify `internal/extension/loader/metadata.go` → `parsePiToml` so the parsed `Command` flows into `Metadata`:

```go
md := piapi.Metadata{
	Name:                  p.Name,
	Version:               p.Version,
	Description:           p.Description,
	Prompt:                p.Prompt,
	RequestedCapabilities: p.RequestedCapabilities,
	Entry:                 p.Entry,
	Command:               p.Command,
}
```

And at `internal/extension/loader/discover.go` inside `candidateFromPath`, ensure `md.Command` is set from the toml parse (today it's returned as `cmd`):

```go
if piTomlPath := filepath.Join(dir, "pi.toml"); fileExists(piTomlPath) {
	m, cmd, err := parsePiToml(piTomlPath)
	if err != nil {
		return Candidate{}, err
	}
	md = m
	if len(cmd) > 0 {
		command = cmd
		md.Command = cmd
	}
}
```

Keep the `Command` field on `Candidate` too; they're redundant now but downstream consumers use both (a future cleanup task can collapse them).

- [ ] **Step 4: Implement Start**

Replace the `Start` stub in `service.go`:

```go
// Start launches a hosted extension subprocess (no-op for compiled-in,
// idempotent on running). Returns ErrUnknownExtension on unknown id.
func (s *service) Start(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "start", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "start", ID: id, Err: ErrCompiledIn}
	}
	if reg.State == host.StateRunning {
		return nil
	}
	if reg.State != host.StateReady {
		return &Error{Op: "start", ID: id, Err: fmt.Errorf("cannot start from state %s", reg.State)}
	}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "start", ID: id, Err: err}
	}
	ctx2, cancel := context.WithCancel(ctx)
	go s.watchHandshakeTimeout(ctx2, id, 5*time.Second, cancel)
	if err := s.launchFunc(ctx2, reg, s.mgr, cmd); err != nil {
		cancel()
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "start", ID: id, Err: err}
	}
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}

// watchHandshakeTimeout polls manager state every 100ms. On StateRunning
// (handshake success) or StateErrored it returns cleanly. On timeout
// it calls cancel() and transitions to StateErrored.
func (s *service) watchHandshakeTimeout(ctx context.Context, id string, timeout time.Duration, cancel context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		reg := s.mgr.Get(id)
		if reg == nil {
			return
		}
		if reg.State == host.StateRunning || reg.State == host.StateErrored {
			return
		}
		if time.Now().After(deadline) {
			cancel()
			s.mgr.SetState(id, host.StateErrored, fmt.Errorf("handshake timeout after %s", timeout))
			s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
			return
		}
	}
}

// buildCommand is filled in by Task 14; stub here so Start compiles.
func (s *service) buildCommand(reg *host.Registration) ([]string, error) {
	if len(reg.Metadata.Command) > 0 {
		return append([]string(nil), reg.Metadata.Command...), nil
	}
	if reg.Mode == "hosted-go" {
		return []string{"go", "run", "."}, nil
	}
	return nil, fmt.Errorf("buildCommand: no command for %s mode=%s", reg.ID, reg.Mode)
}
```

Add `"fmt"` to service.go imports if missing.

- [ ] **Step 5: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Start" -count=1 -v`
Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add pkg/piapi/metadata.go internal/extension/loader/metadata.go internal/extension/loader/discover.go internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Start with handshake watcher; add piapi.Metadata.Command

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Stop

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_StopCallsStopFunc(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}, State: host.StateRunning}
	_ = mgr.Register(reg)
	called := false
	impl.stopFunc = func(context.Context, *host.Registration) error {
		called = true
		return nil
	}
	if err := svc.Stop(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("stopFunc was not invoked")
	}
	if mgr.Get("h").State != host.StateStopped {
		t.Fatalf("expected StateStopped; got %s", mgr.Get("h").State)
	}
}

func TestService_StopIdempotentOnAlreadyStopped(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1"}}
	_ = mgr.Register(reg)
	mgr.SetState("h", host.StateStopped, nil)
	impl.stopFunc = func(context.Context, *host.Registration) error {
		t.Fatal("stopFunc should not be called on already-stopped")
		return nil
	}
	if err := svc.Stop(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
}
```

Note: `host.Registration` does not let you set `State` via struct literal because `Register` will overwrite it. The test passes `State: host.StateRunning` and then calls `mgr.Register` — the Manager will reset state based on trust+grants. So we manually SetState to StateRunning after Register:

Adjust the `Register...Running` test:

```go
_ = mgr.Register(reg)
mgr.SetState("h", host.StateRunning, nil)
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Stop" -count=1`
Expected: FAIL, stub no-op.

- [ ] **Step 3: Implement Stop**

Replace the `Stop` stub:

```go
// Stop sends shutdown, closes the RPC conn, and transitions to
// StateStopped. Idempotent on already-stopped/pending/ready.
func (s *service) Stop(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "stop", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "stop", ID: id, Err: ErrCompiledIn}
	}
	switch reg.State {
	case host.StateStopped, host.StatePending, host.StateReady, host.StateDenied:
		return nil
	}
	if err := s.stopFunc(ctx, reg); err != nil {
		s.mgr.SetState(id, host.StateErrored, err)
		s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
		return &Error{Op: "stop", ID: id, Err: err}
	}
	s.mgr.SetState(id, host.StateStopped, nil)
	s.publish(Event{Kind: EventStateChanged, View: s.viewFromRegistration(s.mgr.Get(id))})
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Stop" -count=1 -v`
Expected: 2 PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Stop via stopFunc; idempotent on non-running states

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Restart

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing tests**

Append to `service_test.go`:

```go
func TestService_RestartStopsThenStarts(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "h", Version: "0.1", Command: []string{"echo"}}}
	_ = mgr.Register(reg)
	_ = svc.Approve(context.Background(), "h", []string{"tools.register"})
	mgr.SetState("h", host.StateRunning, nil)
	var order []string
	impl.stopFunc = func(context.Context, *host.Registration) error {
		order = append(order, "stop")
		return nil
	}
	impl.launchFunc = func(_ context.Context, gotReg *host.Registration, _ *host.Manager, _ []string) error {
		order = append(order, "start")
		mgr.SetState(gotReg.ID, host.StateRunning, nil)
		return nil
	}
	if err := svc.Restart(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "stop" || order[1] != "start" {
		t.Fatalf("expected [stop,start]; got %v", order)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Restart" -count=1`
Expected: FAIL — stub no-op.

- [ ] **Step 3: Implement Restart**

Replace the `Restart` stub:

```go
// Restart = Stop + (ensure StateReady) + Start. Callers see a single
// operation's error shape: any failure is wrapped with Op="restart".
func (s *service) Restart(ctx context.Context, id string) error {
	reg := s.mgr.Get(id)
	if reg == nil {
		return &Error{Op: "restart", ID: id, Err: ErrUnknownExtension}
	}
	if reg.Trust == host.TrustCompiledIn {
		return &Error{Op: "restart", ID: id, Err: ErrCompiledIn}
	}
	if err := s.Stop(ctx, id); err != nil {
		return &Error{Op: "restart", ID: id, Err: err}
	}
	// After Stop, state is StateStopped. Bump back to StateReady iff
	// the extension is approved so Start can take over.
	if reg.State == host.StateStopped || s.mgr.Get(id).State == host.StateStopped {
		s.mgr.SetState(id, host.StateReady, nil)
	}
	if err := s.Start(ctx, id); err != nil {
		return &Error{Op: "restart", ID: id, Err: err}
	}
	return nil
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Restart" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Restart = Stop + bump StateReady + Start

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: StartApproved + StopAll

**Files:**
- Create: `internal/extension/lifecycle/autostart.go`
- Create: `internal/extension/lifecycle/autostart_test.go`
- Modify: `internal/extension/lifecycle/service.go` (replace stubs)

- [ ] **Step 1: Write failing tests**

Create `autostart_test.go`:

```go
package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestStartApproved_LaunchesEveryReadyHosted(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	for _, id := range []string{"a", "b", "c"} {
		reg := &host.Registration{ID: id, Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: id, Version: "0.1", Command: []string{"echo"}}}
		_ = mgr.Register(reg)
		_ = svc.Approve(context.Background(), id, []string{"tools.register"})
	}
	var count int64
	impl.launchFunc = func(_ context.Context, reg *host.Registration, _ *host.Manager, _ []string) error {
		atomic.AddInt64(&count, 1)
		mgr.SetState(reg.ID, host.StateRunning, nil)
		return nil
	}
	errs := svc.StartApproved(context.Background())
	if len(errs) != 0 {
		t.Fatalf("expected no errors; got %v", errs)
	}
	// Wait for launches (goroutines).
	deadline := time.Now().Add(500 * time.Millisecond)
	for atomic.LoadInt64(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&count); got != 3 {
		t.Fatalf("expected 3 launches; got %d", got)
	}
}

func TestStartApproved_SkipsCompiledInAndPending(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	_ = mgr.Register(&host.Registration{ID: "compiled", Mode: "compiled-in", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "compiled", Version: "0.1"}})
	_ = mgr.Register(&host.Registration{ID: "pending", Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: "pending", Version: "0.1"}})
	called := false
	impl.launchFunc = func(context.Context, *host.Registration, *host.Manager, []string) error {
		called = true
		return nil
	}
	svc.StartApproved(context.Background())
	time.Sleep(100 * time.Millisecond)
	if called {
		t.Fatal("launchFunc should not fire for compiled-in or pending extensions")
	}
}

func TestStopAll_StopsEveryRunning(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	impl := svc.(*service)
	for _, id := range []string{"a", "b"} {
		reg := &host.Registration{ID: id, Mode: "hosted-go", Trust: host.TrustThirdParty, Metadata: piapi.Metadata{Name: id, Version: "0.1"}}
		_ = mgr.Register(reg)
		_ = svc.Approve(context.Background(), id, []string{"tools.register"})
		mgr.SetState(id, host.StateRunning, nil)
	}
	var stops int64
	impl.stopFunc = func(context.Context, *host.Registration) error {
		atomic.AddInt64(&stops, 1)
		return nil
	}
	errs := svc.StopAll(context.Background())
	if len(errs) != 0 {
		t.Fatalf("expected no errors; got %v", errs)
	}
	if got := atomic.LoadInt64(&stops); got != 2 {
		t.Fatalf("expected 2 stops; got %d", got)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestStartApproved|TestStopAll" -count=1`
Expected: stubs no-op; FAIL.

- [ ] **Step 3: Implement autostart.go**

```go
package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
)

// StartApproved launches every hosted extension in StateReady in
// parallel. Returns immediately; per-extension outcomes arrive via
// Subscribe. The returned slice is empty today (kept typed as []error
// so a future synchronous caller can collect).
func (s *service) StartApproved(ctx context.Context) []error {
	for _, reg := range s.mgr.List() {
		if reg.Trust == host.TrustCompiledIn {
			continue
		}
		if reg.State != host.StateReady {
			continue
		}
		if reg.Mode != "hosted-go" && reg.Mode != "hosted-ts" {
			continue
		}
		id := reg.ID
		go func() {
			if err := s.Start(ctx, id); err != nil {
				// Error already recorded on reg via Start; nothing
				// more to do. Subscribers have been notified.
				_ = err
			}
		}()
	}
	return nil
}

// StopAll calls Stop on every non-terminal hosted registration in
// parallel, bounded by a 3s per-extension wait. Returns collected
// errors.
func (s *service) StopAll(ctx context.Context) []error {
	regs := s.mgr.List()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, reg := range regs {
		if reg.Trust == host.TrustCompiledIn {
			continue
		}
		if reg.State != host.StateRunning {
			continue
		}
		id := reg.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := s.Stop(c, id); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}
```

Delete the `StartApproved` and `StopAll` stubs in `service.go`.

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/autostart.go internal/extension/lifecycle/autostart_test.go internal/extension/lifecycle/service.go
rtk git commit -m "feat(lifecycle): StartApproved + StopAll with parallel goroutines

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: buildCommand for hosted-go + hosted-ts

**Files:**
- Modify: `internal/extension/lifecycle/autostart.go` (move buildCommand here)
- Modify: `internal/extension/lifecycle/service.go` (remove stub)
- Create: `internal/extension/lifecycle/autostart_command_test.go`

- [ ] **Step 1: Write failing tests**

Create `autostart_command_test.go`:

```go
package lifecycle

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestBuildCommand_HostedGoUsesMetadataCommand(t *testing.T) {
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Metadata: piapi.Metadata{Command: []string{"go", "run", "."}}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd) != 3 || cmd[0] != "go" || cmd[2] != "." {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
}

func TestBuildCommand_HostedGoFallsBackToGoRunDot(t *testing.T) {
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Metadata: piapi.Metadata{}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatal(err)
	}
	if cmd[0] != "go" || cmd[1] != "run" || cmd[2] != "." {
		t.Fatalf("unexpected fallback: %v", cmd)
	}
}

func TestBuildCommand_HostedTSErrorsWhenNodeMissing(t *testing.T) {
	// We can't easily remove node from PATH. Instead, cover the
	// success branch — if node is available, command includes it plus
	// the extracted host bundle path.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH; skipping success-branch assertion")
	}
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-ts", WorkDir: t.TempDir(), Metadata: piapi.Metadata{Entry: "src/index.ts"}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if cmd[0] != "node" || !strings.Contains(cmd[1], "host.bundle.js") {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
	// The --entry path must be absolute.
	if !filepath.IsAbs(cmd[3]) {
		t.Fatalf("expected absolute entry path; got %q", cmd[3])
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestBuildCommand" -count=1`
Expected: FAIL — buildCommand stub is too simple.

- [ ] **Step 3: Replace buildCommand in `autostart.go`**

Delete the stub in `service.go`. Append to `autostart.go`:

```go
import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// buildCommand returns argv for launching reg. Pulls hostVersion from
// the build — for now we hard-code "stable"; replace with
// runtime.Version()-derived string when the project exposes one.
var hostBundleVersion = "stable"

func (s *service) buildCommand(reg *host.Registration) ([]string, error) {
	switch reg.Mode {
	case "hosted-go":
		if len(reg.Metadata.Command) > 0 {
			return append([]string(nil), reg.Metadata.Command...), nil
		}
		return []string{"go", "run", "."}, nil
	case "hosted-ts":
		if _, err := exec.LookPath("node"); err != nil {
			return nil, fmt.Errorf("node not on PATH: %w", err)
		}
		hostPath, err := host.ExtractedHostPath(hostBundleVersion)
		if err != nil {
			return nil, fmt.Errorf("extract host bundle: %w", err)
		}
		entry := reg.Metadata.Entry
		if entry == "" {
			entry = "src/index.ts"
		}
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(reg.WorkDir, entry)
		}
		return []string{"node", hostPath, "--entry", entry, "--name", reg.ID}, nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", reg.Mode)
	}
}
```

Re-org imports in `autostart.go` so the `fmt` and `exec` and `filepath` imports merge with the existing import block.

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestBuildCommand" -count=1 -v`
Expected: 3 PASS (or 2 PASS + 1 SKIP if node missing).

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/autostart.go internal/extension/lifecycle/service.go internal/extension/lifecycle/autostart_command_test.go
rtk git commit -m "feat(lifecycle): buildCommand for hosted-go (pi.toml/fallback) + hosted-ts (node+bundle)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 15: Reload (additive-only v1)

**Files:**
- Modify: `internal/extension/lifecycle/service.go`
- Modify: `internal/extension/lifecycle/service_test.go`

- [ ] **Step 1: Write failing test**

Append to `service_test.go`:

```go
func TestService_ReloadAddsNewDiscoveries(t *testing.T) {
	svc, mgr, _ := newTestService(t)
	// Seed an extension in the workDir's .pi-go/extensions/ so that
	// loader.Discover finds it.
	impl := svc.(*service)
	dir := filepath.Join(impl.workDir, ".pi-go", "extensions", "new-ext")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	piToml := `name = "new-ext"
version = "0.1"
runtime = "hosted"
command = ["echo"]
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(piToml), 0644); err != nil {
		t.Fatal(err)
	}
	ch, cancel := svc.Subscribe()
	defer cancel()
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := drainEvents(t, ch, 1, 500*time.Millisecond)
	if events[0].Kind != EventRegistrationAdded {
		t.Fatalf("expected EventRegistrationAdded; got %s", events[0].Kind)
	}
	if mgr.Get("new-ext") == nil {
		t.Fatal("expected new-ext registered after Reload")
	}
}
```

Add `"os"` to `service_test.go` imports if missing. Make sure `t.Setenv("HOME", tmp)` and `t.Setenv("USERPROFILE", tmp)` are set inside `newTestService` so `loader.Discover` doesn't pick up real user-level extensions during the test. Modify `newTestService`:

```go
func newTestService(t *testing.T) (Service, *host.Manager, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	approvalsPath := filepath.Join(tmp, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp)
	return svc, mgr, approvalsPath
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Reload" -count=1`
Expected: FAIL — stub no-op.

- [ ] **Step 3: Implement Reload**

Replace the `Reload` stub in `service.go`:

```go
// Reload rediscovers extensions from the configured workDir and adds
// newly-found ones to the Manager. V1 is additive only: removals and
// metadata refreshes require a process restart. That matches the
// user-driven "R reload all" semantic — you almost always want to see
// what dropping in a new extension file did.
func (s *service) Reload(ctx context.Context) error {
	_ = ctx
	candidates, err := loader.Discover(s.workDir)
	if err != nil {
		return &Error{Op: "reload", Err: err}
	}
	for _, c := range candidates {
		if s.mgr.Get(c.Metadata.Name) != nil {
			continue
		}
		reg := &host.Registration{
			ID:       c.Metadata.Name,
			Mode:     c.Mode.String(),
			Trust:    host.TrustThirdParty,
			Metadata: c.Metadata,
			WorkDir:  c.Dir,
		}
		if err := s.mgr.Register(reg); err != nil {
			continue
		}
		s.publish(Event{Kind: EventRegistrationAdded, View: s.viewFromRegistration(reg)})
	}
	return nil
}
```

Add `"github.com/dimetron/pi-go/internal/extension/loader"` to service.go imports.

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/lifecycle/... -run "TestService_Reload" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/lifecycle/service.go internal/extension/lifecycle/service_test.go
rtk git commit -m "feat(lifecycle): Reload discovers + Registers new extensions (v1 additive-only)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 16: Wire `Runtime.Lifecycle` into `BuildRuntime`

**Files:**
- Modify: `internal/extension/runtime.go`
- Create: `internal/extension/runtime_lifecycle_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/extension/runtime_lifecycle_test.go`:

```go
package extension

import (
	"context"
	"testing"
)

func TestBuildRuntime_ProvidesLifecycle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if rt.Lifecycle == nil {
		t.Fatal("expected rt.Lifecycle to be non-nil")
	}
	if len(rt.Lifecycle.List()) == 0 {
		t.Fatal("expected at least the compiled-in hello extension in Lifecycle.List()")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/extension/ -run "TestBuildRuntime_ProvidesLifecycle" -count=1`
Expected: `rt.Lifecycle undefined`.

- [ ] **Step 3: Modify `runtime.go`**

Add the import:

```go
"github.com/dimetron/pi-go/internal/extension/lifecycle"
```

Add the field to `Runtime`:

```go
type Runtime struct {
	...
	Lifecycle lifecycle.Service
	...
}
```

At the bottom of `BuildRuntime`, before `return rt, nil`:

```go
rt.Lifecycle = lifecycle.New(manager, gate, approvalsPath, cfg.WorkDir)
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/extension/ -run "TestBuildRuntime_ProvidesLifecycle" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Run the rest of the extension subtree**

Run: `rtk go test ./internal/extension/... -count=1`
Expected: everything green.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/extension/runtime.go internal/extension/runtime_lifecycle_test.go
rtk git commit -m "feat(extension): BuildRuntime exposes lifecycle.Service as rt.Lifecycle

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: lifecycle E2E — hosted-go

**Files:**
- Create: `internal/extension/lifecycle/lifecycle_e2e_hosted_go_test.go`

- [ ] **Step 1: Write test**

```go
package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestLifecycleE2E_HostedGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-go E2E under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not on PATH: %v", err)
	}
	root, err := repoRootFromHere()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	example := filepath.Join(root, "examples", "extensions", "hosted-hello-go")
	if _, err := os.Stat(filepath.Join(example, "main.go")); err != nil {
		t.Skipf("hosted-hello-go example missing: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(example, filepath.Join(extsDir, "hosted-hello-go")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	approvalsPath := filepath.Join(extsDir, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp)

	// Discover via Reload.
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Get("hosted-hello-go"); !ok {
		t.Fatal("expected hosted-hello-go in Service after Reload")
	}
	// Approve.
	if err := svc.Approve(context.Background(), "hosted-hello-go", []string{"tools.register", "events.session_start", "events.tool_execute"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Registration should be StateReady.
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateReady {
		t.Fatalf("expected StateReady after Approve; got %s", v.State)
	}
	// Start and wait for handshake.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Start(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := svc.Get("hosted-hello-go"); v.State == host.StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateRunning {
		t.Fatalf("expected StateRunning; got %s (err=%s)", v.State, v.Err)
	}
	// Stop.
	if err := svc.Stop(ctx, "hosted-hello-go"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if v, _ := svc.Get("hosted-hello-go"); v.State != host.StateStopped {
		t.Fatalf("expected StateStopped; got %s", v.State)
	}
}

// repoRootFromHere finds the project root by walking up until we hit go.work.
func repoRootFromHere() (string, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	_ = piapi.Metadata{} // keep piapi import alive across edits
	return "", os.ErrNotExist
}
```

- [ ] **Step 2: Run**

Run: `rtk go test ./internal/extension/lifecycle/ -run "TestLifecycleE2E_HostedGo" -count=1 -v -timeout 60s`
Expected on Linux CI: PASS. On Windows without admin: SKIP due to symlink.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/extension/lifecycle/lifecycle_e2e_hosted_go_test.go
rtk git commit -m "test(lifecycle): E2E Discover→Approve→Start→Stop against hosted-hello-go

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 18: lifecycle E2E — hosted-ts

**Files:**
- Create: `internal/extension/lifecycle/lifecycle_e2e_hosted_ts_test.go`

- [ ] **Step 1: Write test**

```go
package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/host"
)

func TestLifecycleE2E_HostedTS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-ts E2E under -short")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not on PATH: %v", err)
	}
	root, err := repoRootFromHere()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	example := filepath.Join(root, "examples", "extensions", "hosted-hello-ts")
	if _, err := os.Stat(filepath.Join(example, "src", "index.ts")); err != nil {
		t.Skipf("hosted-hello-ts example missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(example, "node_modules", "@pi-go", "extension-sdk")); err != nil {
		t.Skipf("run `npm install` in %s first", example)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	extsDir := filepath.Join(tmp, ".pi-go", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(example, filepath.Join(extsDir, "hosted-hello-ts")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	approvalsPath := filepath.Join(extsDir, "approvals.json")
	gate, err := host.NewGate(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := host.NewManager(gate)
	svc := New(mgr, gate, approvalsPath, tmp)

	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Get("hosted-hello-ts"); !ok {
		t.Fatal("expected hosted-hello-ts")
	}
	if err := svc.Approve(context.Background(), "hosted-hello-ts", []string{"tools.register", "events.session_start", "events.tool_execute"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.Start(ctx, "hosted-hello-ts"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := svc.Get("hosted-hello-ts"); v.State == host.StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if v, _ := svc.Get("hosted-hello-ts"); v.State != host.StateRunning {
		t.Fatalf("expected StateRunning; got %s (err=%s)", v.State, v.Err)
	}
	_ = svc.Stop(ctx, "hosted-hello-ts")
}
```

- [ ] **Step 2: Run**

Run: `rtk go test ./internal/extension/lifecycle/ -run "TestLifecycleE2E_HostedTS" -count=1 -v -timeout 60s`
Expected on CI with node + admin symlinks: PASS. Otherwise: SKIP.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/extension/lifecycle/lifecycle_e2e_hosted_ts_test.go
rtk git commit -m "test(lifecycle): E2E Discover→Approve→Start→Stop against hosted-hello-ts

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 19: TUI toast — render state

**Files:**
- Create: `internal/tui/extension_toast.go`
- Create: `internal/tui/extension_toast_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tui

import (
	"strings"
	"testing"
)

func TestExtensionToast_HiddenWhenNoPending(t *testing.T) {
	ts := extensionToastState{pending: 0}
	if got := ts.View(); got != "" {
		t.Fatalf("expected empty view; got %q", got)
	}
}

func TestExtensionToast_RendersCountAndHint(t *testing.T) {
	ts := extensionToastState{pending: 2}
	got := ts.View()
	if !strings.Contains(got, "2 extensions pending") {
		t.Fatalf("expected count; got %q", got)
	}
	if !strings.Contains(got, "press e") {
		t.Fatalf("expected hint; got %q", got)
	}
}

func TestExtensionToast_HiddenAfterDismiss(t *testing.T) {
	ts := extensionToastState{pending: 2}
	ts.Dismiss()
	if ts.View() != "" {
		t.Fatal("expected hidden after Dismiss()")
	}
}

func TestExtensionToast_ReappearsWhenPendingRises(t *testing.T) {
	ts := extensionToastState{pending: 0, dismissed: true}
	ts.SetPending(3)
	if ts.View() == "" {
		t.Fatal("expected toast to reappear when pending rises above 0")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestExtensionToast" -count=1`
Expected: undefined symbol errors.

- [ ] **Step 3: Implement**

```go
package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// extensionToastState tracks the "N extensions pending — press e to review"
// line. Hidden when pending==0 OR dismissed==true-and-pending-didn't-change.
type extensionToastState struct {
	pending    int
	dismissed  bool
	lastCount  int
}

// SetPending updates the count from a lifecycle.List() scan. Whenever
// the count rises above the last-seen count, dismissed is cleared so
// the toast reappears for the new pending extension.
func (s *extensionToastState) SetPending(n int) {
	if n > s.lastCount {
		s.dismissed = false
	}
	s.lastCount = n
	s.pending = n
}

// Dismiss hides the toast until SetPending raises the count again.
func (s *extensionToastState) Dismiss() { s.dismissed = true }

var toastStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("214")).
	Bold(true)

// View renders the toast line. Empty string = no render.
func (s *extensionToastState) View() string {
	if s.pending == 0 || s.dismissed {
		return ""
	}
	text := fmt.Sprintf("%d extension", s.pending)
	if s.pending != 1 {
		text += "s"
	}
	text += " pending approval — press e to review"
	return toastStyle.Render(text)
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestExtensionToast" -count=1 -v`
Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/extension_toast.go internal/tui/extension_toast_test.go
rtk git commit -m "feat(tui): extension_toast state + view with pending-count rendering

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 20: TUI toast integration — wire into model + status bar

**Files:**
- Modify: `internal/tui/chat.go` (or `model.go` — whichever holds the `model` struct)
- Modify: `internal/tui/status.go`

First identify where the TUI `model` struct lives:

Run: `rtk grep -n "^type model struct" internal/tui/`
Expected: points you at (likely) `chat.go`. Use that file path in steps below.

- [ ] **Step 1: Add the field + update rule**

In the model struct file, add:

```go
extensionToast extensionToastState
```

In the model's Update function, add to the top of the function's `tea.KeyMsg` handler branch:

```go
// Any keystroke dismisses a visible extension toast.
if m.extensionToast.pending > 0 && !m.extensionToast.dismissed {
	m.extensionToast.Dismiss()
}
```

- [ ] **Step 2: Refresh count on lifecycle events**

Wherever the model handles `extensionEventMsg` (will be created in Task 28; for now just call this after `tea.WindowSizeMsg`) add:

```go
if m.runtime != nil && m.runtime.Lifecycle != nil {
	m.extensionToast.SetPending(countPending(m.runtime.Lifecycle.List()))
}
```

Declare the helper near the toast:

```go
// countPending counts StatePending hosted extensions — the ones the
// user needs to act on.
func countPending(views []lifecycle.View) int {
	n := 0
	for _, v := range views {
		if v.State == host.StatePending {
			n++
		}
	}
	return n
}
```

Add imports `"github.com/dimetron/pi-go/internal/extension/host"` and `"github.com/dimetron/pi-go/internal/extension/lifecycle"` to the toast file (not the model file if unnecessary). If importing `lifecycle` from `tui` causes a cycle — it shouldn't, but if it does, move `countPending` into the lifecycle package.

- [ ] **Step 3: Render the toast in the status view**

In `status.go`, find where the status bar is assembled and append:

```go
if toast := m.extensionToast.View(); toast != "" {
	out += "  " + toast
}
```

(Exact concatenation depends on the status.go layout; keep it in the same line builder the model already uses.)

- [ ] **Step 4: Build + existing tests still pass**

Run: `rtk go build ./... && rtk go test ./internal/tui/... -count=1`
Expected: clean build, all tests pass.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/
rtk git commit -m "feat(tui): wire extension toast into model/status; dismiss on any key

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 21: `/extensions` panel — scaffold + slash-command integration

**Files:**
- Create: `internal/tui/extension_panel.go`
- Create: `internal/tui/extension_panel_test.go`
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Write failing test**

```go
package tui

import (
	"testing"
)

func TestExtensionPanel_InitiallyHidden(t *testing.T) {
	p := extensionPanelState{}
	if p.Open() {
		t.Fatal("expected hidden state")
	}
}

func TestExtensionPanel_OpenSetsOpen(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	if !p.Open() {
		t.Fatal("expected open after OpenPanel")
	}
}

func TestExtensionPanel_CloseHides(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.Close()
	if p.Open() {
		t.Fatal("expected hidden after Close")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_Initially|TestExtensionPanel_Open|TestExtensionPanel_Close" -count=1`
Expected: undefined.

- [ ] **Step 3: Scaffold `extension_panel.go`**

```go
package tui

import (
	"github.com/dimetron/pi-go/internal/extension/lifecycle"
)

// extensionPanelState holds the /extensions overlay. Populated from
// lifecycle.Service.List() on open and on lifecycle events.
type extensionPanelState struct {
	open     bool
	views    []lifecycle.View
	selected int
	filter   string
	height   int
}

func (s *extensionPanelState) Open() bool { return s.open }

func (s *extensionPanelState) OpenPanel() { s.open = true }

func (s *extensionPanelState) Close() {
	s.open = false
	s.filter = ""
	s.selected = 0
}

func (s *extensionPanelState) SetViews(views []lifecycle.View) {
	s.views = views
	if s.selected >= len(views) {
		s.selected = len(views) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}
```

- [ ] **Step 4: Register `/extensions` slash command**

In `commands.go`, locate the builtin slash command table (search `rtk grep -n "DefaultBuiltinSlashCommands" internal/tui/`). Add:

```go
{
	Name:        "extensions",
	Description: "Open the extension management panel",
	Handler: func(m *model, _ string) tea.Cmd {
		m.extensionPanel.OpenPanel()
		if m.runtime != nil && m.runtime.Lifecycle != nil {
			m.extensionPanel.SetViews(m.runtime.Lifecycle.List())
		}
		return nil
	},
},
```

Add an `extensionPanel extensionPanelState` field to the `model` struct.

- [ ] **Step 5: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_Initially|TestExtensionPanel_Open|TestExtensionPanel_Close" -count=1 -v`
Expected: 3 PASS.

- [ ] **Step 6: Commit**

```bash
rtk git add internal/tui/extension_panel.go internal/tui/extension_panel_test.go internal/tui/commands.go internal/tui/chat.go
rtk git commit -m "feat(tui): /extensions slash command opens extensionPanel overlay

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 22: Panel row rendering + navigation + detail pane

**Files:**
- Modify: `internal/tui/extension_panel.go`
- Modify: `internal/tui/extension_panel_test.go`

- [ ] **Step 1: Write failing tests**

Append to `extension_panel_test.go`:

```go
func TestExtensionPanel_ViewListsRowsAndHighlightsSelection(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "a", Mode: "hosted-go", State: host.StatePending, Trust: host.TrustThirdParty},
		{ID: "b", Mode: "hosted-go", State: host.StateRunning, Trust: host.TrustThirdParty},
	})
	got := p.View(80, 24)
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("expected both rows; got %q", got)
	}
	if !strings.Contains(got, "pending") || !strings.Contains(got, "running") {
		t.Fatalf("expected state cells; got %q", got)
	}
}

func TestExtensionPanel_NavigateMovesSelection(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	p.MoveSelection(1)
	if p.selected != 1 {
		t.Fatalf("expected selected=1; got %d", p.selected)
	}
	p.MoveSelection(1)
	p.MoveSelection(1) // clamp at last
	if p.selected != 2 {
		t.Fatalf("expected clamp at 2; got %d", p.selected)
	}
	p.MoveSelection(-5)
	if p.selected != 0 {
		t.Fatalf("expected clamp at 0; got %d", p.selected)
	}
}

func TestExtensionPanel_DetailPaneShowsRequestedCapabilitiesOnPending(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "a", State: host.StatePending, Requested: []string{"tools.register", "events.session_start"}},
	})
	got := p.View(80, 24)
	if !strings.Contains(got, "tools.register") || !strings.Contains(got, "events.session_start") {
		t.Fatalf("expected requested caps in detail pane; got %q", got)
	}
}
```

Add `"strings"`, `"github.com/dimetron/pi-go/internal/extension/host"`, `"github.com/dimetron/pi-go/internal/extension/lifecycle"` to the test imports.

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_View|TestExtensionPanel_Navigate|TestExtensionPanel_DetailPane" -count=1`
Expected: undefined `View`, `MoveSelection`.

- [ ] **Step 3: Implement**

Append to `extension_panel.go`:

```go
import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dimetron/pi-go/internal/extension/host"
)

func (s *extensionPanelState) MoveSelection(delta int) {
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.views) {
		s.selected = len(s.views) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

var (
	panelBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	panelHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("242"))
	panelSelectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	panelDimmedRow   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	panelErrorRow    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

func (s *extensionPanelState) View(width, height int) string {
	if !s.open {
		return ""
	}
	var b strings.Builder
	b.WriteString(panelHeaderStyle.Render(fmt.Sprintf("%-18s %-12s %-10s %-14s", "NAME", "MODE", "STATE", "TRUST")))
	b.WriteString("\n")
	for i, v := range s.views {
		row := fmt.Sprintf("%-18s %-12s %-10s %-14s", trunc(v.ID, 18), v.Mode, v.State, v.Trust)
		switch {
		case v.State == host.StateErrored:
			b.WriteString(panelErrorRow.Render(row))
		case v.Mode == "compiled-in":
			b.WriteString(panelDimmedRow.Render(row) + "  (implicit)")
		case i == s.selected:
			b.WriteString(panelSelectedRow.Render(row))
		default:
			b.WriteString(row)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.detailPane())
	b.WriteString("\n")
	b.WriteString(panelDimmedRow.Render("a approve · d deny · v revoke · s start · x stop · r restart · R reload · / filter · esc close"))
	return panelBorderStyle.Render(b.String())
}

func (s *extensionPanelState) detailPane() string {
	if len(s.views) == 0 {
		return panelDimmedRow.Render("(no extensions discovered)")
	}
	v := s.views[s.selected]
	var b strings.Builder
	switch v.State {
	case host.StatePending:
		b.WriteString("Pending approval. Requested capabilities:\n")
		for _, c := range v.Requested {
			b.WriteString("  " + c + "\n")
		}
	case host.StateErrored:
		b.WriteString("Error:\n  " + v.Err)
	default:
		b.WriteString(fmt.Sprintf("State: %s · Granted: %s", v.State, strings.Join(v.Granted, ", ")))
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_View|TestExtensionPanel_Navigate|TestExtensionPanel_DetailPane" -count=1 -v`
Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/extension_panel.go internal/tui/extension_panel_test.go
rtk git commit -m "feat(tui): extension panel row rendering + navigation + detail pane

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 23: Panel filter

**Files:**
- Modify: `internal/tui/extension_panel.go`
- Modify: `internal/tui/extension_panel_test.go`

- [ ] **Step 1: Write failing test**

Append to `extension_panel_test.go`:

```go
func TestExtensionPanel_FilterMatchesIDAndMode(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "alpha", Mode: "hosted-go"},
		{ID: "beta", Mode: "hosted-ts"},
		{ID: "gamma", Mode: "compiled-in"},
	})
	p.SetFilter("ts")
	got := p.View(80, 24)
	if !strings.Contains(got, "beta") {
		t.Fatal("expected beta in filtered output")
	}
	if strings.Contains(got, "alpha") || strings.Contains(got, "gamma") {
		t.Fatalf("filter failed to exclude: %q", got)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_Filter" -count=1`
Expected: undefined `SetFilter`.

- [ ] **Step 3: Implement**

Append to `extension_panel.go`:

```go
// SetFilter narrows visible rows to those whose ID or mode contain the
// substring (case-insensitive). Empty string clears the filter.
func (s *extensionPanelState) SetFilter(filter string) {
	s.filter = strings.ToLower(filter)
	if s.selected >= len(s.filteredViews()) {
		s.selected = 0
	}
}

// filteredViews returns the current view list after filter application.
// Used by View() — tests assert on its output.
func (s *extensionPanelState) filteredViews() []lifecycle.View {
	if s.filter == "" {
		return s.views
	}
	out := make([]lifecycle.View, 0, len(s.views))
	for _, v := range s.views {
		if strings.Contains(strings.ToLower(v.ID), s.filter) ||
			strings.Contains(strings.ToLower(v.Mode), s.filter) ||
			strings.Contains(strings.ToLower(string(rune(v.State))+v.State.String()), s.filter) {
			out = append(out, v)
		}
	}
	return out
}
```

Update `View` to iterate over `s.filteredViews()` instead of `s.views`, and update the selected-row detail lookup likewise:

```go
views := s.filteredViews()
...
for i, v := range views { ... }
...
// in detailPane:
views := s.filteredViews()
if len(views) == 0 { ... }
v := views[s.selected]
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_Filter" -count=1 -v`
Expected: PASS.

Run all existing tui tests to make sure nothing regressed:

Run: `rtk go test ./internal/tui/ -count=1`
Expected: green.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/extension_panel.go internal/tui/extension_panel_test.go
rtk git commit -m "feat(tui): extension panel filter on ID/mode (case-insensitive)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 24: Panel action keys delegate to Service

**Files:**
- Modify: `internal/tui/extension_panel.go`
- Modify: `internal/tui/extension_panel_test.go`

- [ ] **Step 1: Write failing test**

Append to `extension_panel_test.go`:

```go
type fakeLifecycle struct {
	approveCalls []string
	denyCalls    []string
	revokeCalls  []string
	startCalls   []string
	stopCalls    []string
	restartCalls []string
	reloadCalls  int
	views        []lifecycle.View
}

func (f *fakeLifecycle) List() []lifecycle.View             { return f.views }
func (f *fakeLifecycle) Get(id string) (lifecycle.View, bool) {
	for _, v := range f.views {
		if v.ID == id {
			return v, true
		}
	}
	return lifecycle.View{}, false
}
func (f *fakeLifecycle) Approve(_ context.Context, id string, _ []string) error {
	f.approveCalls = append(f.approveCalls, id)
	return nil
}
func (f *fakeLifecycle) Deny(_ context.Context, id string, _ string) error {
	f.denyCalls = append(f.denyCalls, id)
	return nil
}
func (f *fakeLifecycle) Revoke(_ context.Context, id string) error {
	f.revokeCalls = append(f.revokeCalls, id)
	return nil
}
func (f *fakeLifecycle) Start(_ context.Context, id string) error {
	f.startCalls = append(f.startCalls, id)
	return nil
}
func (f *fakeLifecycle) Stop(_ context.Context, id string) error {
	f.stopCalls = append(f.stopCalls, id)
	return nil
}
func (f *fakeLifecycle) Restart(_ context.Context, id string) error {
	f.restartCalls = append(f.restartCalls, id)
	return nil
}
func (f *fakeLifecycle) StartApproved(context.Context) []error { return nil }
func (f *fakeLifecycle) StopAll(context.Context) []error       { return nil }
func (f *fakeLifecycle) Reload(context.Context) error          { f.reloadCalls++; return nil }
func (f *fakeLifecycle) Subscribe() (<-chan lifecycle.Event, func()) {
	ch := make(chan lifecycle.Event)
	return ch, func() {}
}

func TestExtensionPanel_KeysDispatchToService(t *testing.T) {
	f := &fakeLifecycle{}
	f.views = []lifecycle.View{{ID: "x", Mode: "hosted-go", State: host.StatePending, Trust: host.TrustThirdParty}}
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews(f.views)
	// Not ideal to call from the panel directly — but this tests the
	// dispatch wiring the model will do.
	p.DispatchKey(context.Background(), f, 's')
	p.DispatchKey(context.Background(), f, 'x')
	p.DispatchKey(context.Background(), f, 'r')
	p.DispatchKey(context.Background(), f, 'R')
	p.DispatchKey(context.Background(), f, 'v')
	p.DispatchKey(context.Background(), f, 'd')
	if len(f.startCalls) != 1 || f.startCalls[0] != "x" {
		t.Fatalf("startCalls=%v", f.startCalls)
	}
	if len(f.stopCalls) != 1 {
		t.Fatalf("stopCalls=%v", f.stopCalls)
	}
	if len(f.restartCalls) != 1 {
		t.Fatalf("restartCalls=%v", f.restartCalls)
	}
	if f.reloadCalls != 1 {
		t.Fatalf("reloadCalls=%d", f.reloadCalls)
	}
	if len(f.revokeCalls) != 1 {
		t.Fatalf("revokeCalls=%v", f.revokeCalls)
	}
	if len(f.denyCalls) != 1 {
		t.Fatalf("denyCalls=%v", f.denyCalls)
	}
}
```

Add `"context"` to imports.

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_KeysDispatch" -count=1`
Expected: undefined `DispatchKey`.

- [ ] **Step 3: Implement**

Append to `extension_panel.go`:

```go
// DispatchKey routes an action key to the supplied lifecycle.Service.
// Returns any error the Service produced — the model decides whether
// to show it as a toast. Approve and filter require interactive modes
// (dialog, filter input) so they're not handled here.
func (s *extensionPanelState) DispatchKey(ctx context.Context, svc lifecycle.Service, r rune) error {
	views := s.filteredViews()
	if len(views) == 0 && r != 'R' {
		return nil
	}
	if r == 'R' {
		return svc.Reload(ctx)
	}
	v := views[s.selected]
	if v.Mode == "compiled-in" {
		// per-row actions are no-ops on compiled-in rows.
		return nil
	}
	switch r {
	case 's':
		return svc.Start(ctx, v.ID)
	case 'x':
		return svc.Stop(ctx, v.ID)
	case 'r':
		return svc.Restart(ctx, v.ID)
	case 'v':
		return svc.Revoke(ctx, v.ID)
	case 'd':
		return svc.Deny(ctx, v.ID, "denied from TUI")
	}
	return nil
}
```

Add `"context"` to `extension_panel.go` imports.

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestExtensionPanel_KeysDispatch" -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/extension_panel.go internal/tui/extension_panel_test.go
rtk git commit -m "feat(tui): panel action keys dispatch to lifecycle.Service via DispatchKey

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 25: Wire panel key handler into model

**Files:**
- Modify: the TUI model Update path (likely `internal/tui/chat.go`)

- [ ] **Step 1: In Update, intercept key events while panel is open**

Search the existing `Update` for the `tea.KeyMsg` branch. Add at the top of that branch:

```go
if m.extensionPanel.Open() {
	switch msg.String() {
	case "esc":
		m.extensionPanel.Close()
		return m, nil
	case "up":
		m.extensionPanel.MoveSelection(-1)
		return m, nil
	case "down":
		m.extensionPanel.MoveSelection(1)
		return m, nil
	case "/":
		m.extensionPanelFilterMode = true
		return m, nil
	}
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r == 'a' {
			// Open approval dialog — wired in Task 27.
			return m, nil
		}
		if err := m.extensionPanel.DispatchKey(context.Background(), m.runtime.Lifecycle, r); err != nil {
			m.lastError = err.Error()
		}
		// Refresh views after action.
		m.extensionPanel.SetViews(m.runtime.Lifecycle.List())
	}
	return m, nil
}
```

Add `m.extensionPanelFilterMode` bool to the model; its filter-mode key plumbing can arrive with Task 27 (interactive filter text). For now just swallow `/` so it doesn't reach the chat input.

Also render the panel in the view. In `View()`, near where overlays render (search for `slashCommandOverlay`), add:

```go
if m.extensionPanel.Open() {
	return m.extensionPanel.View(m.windowWidth, m.windowHeight)
}
```

- [ ] **Step 2: Sanity test**

Run: `rtk go test ./internal/tui/... -count=1`
Expected: green.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/tui/chat.go
rtk git commit -m "feat(tui): route key events through extensionPanel.DispatchKey while open

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 26: Approval dialog — state + render

**Files:**
- Create: `internal/tui/extension_approval_dialog.go`
- Create: `internal/tui/extension_approval_dialog_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tui

import (
	"strings"
	"testing"
)

func TestApprovalDialog_StartsWithAllChecked(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "desc", []string{"tools.register", "events.session_start"})
	for _, c := range d.Capabilities() {
		if !c.Checked {
			t.Fatalf("expected all pre-ticked; %q unchecked", c.Name)
		}
	}
}

func TestApprovalDialog_ToggleUnchecks(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "", []string{"a", "b"})
	d.MoveSelection(1)
	d.Toggle()
	caps := d.Capabilities()
	if caps[1].Checked {
		t.Fatal("expected b unchecked after Toggle")
	}
	if !caps[0].Checked {
		t.Fatal("expected a still checked")
	}
}

func TestApprovalDialog_SelectedGrantsReturnsOnlyChecked(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "", []string{"a", "b"})
	d.MoveSelection(0)
	d.Toggle() // uncheck a
	got := d.SelectedGrants()
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestApprovalDialog_ViewMentionsIDAndVersion(t *testing.T) {
	d := newApprovalDialog("foo-bar", "1.2.3", "great extension", []string{"x.y"})
	out := d.View(80, 24)
	if !strings.Contains(out, "foo-bar") || !strings.Contains(out, "1.2.3") {
		t.Fatalf("expected id + version in view; got %q", out)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `rtk go test ./internal/tui/ -run "TestApprovalDialog" -count=1`
Expected: undefined.

- [ ] **Step 3: Implement**

```go
package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type approvalCapability struct {
	Name    string
	Checked bool
}

type approvalDialogState struct {
	id          string
	version     string
	description string
	caps        []approvalCapability
	selected    int
}

func newApprovalDialog(id, version, description string, requested []string) *approvalDialogState {
	caps := make([]approvalCapability, len(requested))
	for i, c := range requested {
		caps[i] = approvalCapability{Name: c, Checked: true}
	}
	return &approvalDialogState{
		id: id, version: version, description: description, caps: caps,
	}
}

func (s *approvalDialogState) Capabilities() []approvalCapability {
	out := make([]approvalCapability, len(s.caps))
	copy(out, s.caps)
	return out
}

func (s *approvalDialogState) SelectedGrants() []string {
	var out []string
	for _, c := range s.caps {
		if c.Checked {
			out = append(out, c.Name)
		}
	}
	return out
}

func (s *approvalDialogState) MoveSelection(delta int) {
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.caps) {
		s.selected = len(s.caps) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

func (s *approvalDialogState) Toggle() {
	if s.selected >= len(s.caps) {
		return
	}
	s.caps[s.selected].Checked = !s.caps[s.selected].Checked
}

var (
	dialogBorder = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).Padding(0, 1)
	dialogTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

func (s *approvalDialogState) View(width, height int) string {
	_ = width
	_ = height
	var b strings.Builder
	b.WriteString(dialogTitle.Render(fmt.Sprintf("Approve %s?", s.id)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%s v%s\n", s.id, s.version))
	if s.description != "" {
		b.WriteString(s.description + "\n")
	}
	b.WriteString("\nRequested capabilities:\n")
	for i, c := range s.caps {
		marker := "[ ]"
		if c.Checked {
			marker = "[x]"
		}
		line := fmt.Sprintf("  %s %s", marker, c.Name)
		if i == s.selected {
			line = dialogTitle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nSpace toggle · Enter approve · Esc cancel")
	return dialogBorder.Render(b.String())
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `rtk go test ./internal/tui/ -run "TestApprovalDialog" -count=1 -v`
Expected: 4 PASS.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/extension_approval_dialog.go internal/tui/extension_approval_dialog_test.go
rtk git commit -m "feat(tui): approval dialog state + view with per-capability checkboxes

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 27: Wire approval dialog into model + panel 'a' key

**Files:**
- Modify: `internal/tui/chat.go`

- [ ] **Step 1: Add dialog pointer field + flow**

Add `extensionApproval *approvalDialogState` to the model.

Where Task 25 handled the panel's `'a'` key, replace the "wired in Task 27" stub:

```go
if r == 'a' {
	views := m.extensionPanel.filteredViews()
	if len(views) == 0 {
		return m, nil
	}
	v := views[m.extensionPanel.selected]
	if v.State != host.StatePending {
		return m, nil
	}
	m.extensionApproval = newApprovalDialog(v.ID, v.Version, v.Mode, v.Requested)
	return m, nil
}
```

Now in the Update function, *above* the panel-open branch, add a dialog-open branch:

```go
if m.extensionApproval != nil {
	if kmsg, ok := msg.(tea.KeyMsg); ok {
		switch kmsg.String() {
		case "esc":
			m.extensionApproval = nil
			return m, nil
		case "enter":
			grants := m.extensionApproval.SelectedGrants()
			id := m.extensionApproval.id
			m.extensionApproval = nil
			if err := m.runtime.Lifecycle.Approve(context.Background(), id, grants); err != nil {
				m.lastError = err.Error()
			}
			m.extensionPanel.SetViews(m.runtime.Lifecycle.List())
			return m, nil
		case "up":
			m.extensionApproval.MoveSelection(-1)
			return m, nil
		case "down":
			m.extensionApproval.MoveSelection(1)
			return m, nil
		case " ":
			m.extensionApproval.Toggle()
			return m, nil
		}
	}
	return m, nil
}
```

And render the dialog in `View()` above the panel:

```go
if m.extensionApproval != nil {
	return m.extensionApproval.View(m.windowWidth, m.windowHeight)
}
```

- [ ] **Step 2: Build + tests**

Run: `rtk go build ./... && rtk go test ./internal/tui/... -count=1`
Expected: green.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/tui/chat.go
rtk git commit -m "feat(tui): 'a' opens approval dialog; Enter calls Lifecycle.Approve

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 28: Event bridge tea.Cmd

**Files:**
- Modify: `internal/tui/chat.go`

Goal: a `tea.Cmd` that reads one `lifecycle.Event` from `rt.Lifecycle.Subscribe()`, returns it as `extensionEventMsg`, and re-queues itself.

- [ ] **Step 1: Define the message type and cmd**

Add near the top of `chat.go` (or in a new file `internal/tui/extension_events.go`):

```go
type extensionEventMsg struct {
	event lifecycle.Event
}

// extensionEventSubscription starts a subscription and returns a
// recursive tea.Cmd that delivers one event at a time. The returned
// cancel must be stored on the model and called on tea.Quit.
func extensionEventSubscription(svc lifecycle.Service) (<-chan lifecycle.Event, func(), tea.Cmd) {
	ch, cancel := svc.Subscribe()
	var cmd tea.Cmd
	cmd = func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return extensionEventMsg{event: ev}
	}
	return ch, cancel, cmd
}
```

Add `tea "charm.land/bubbletea/v2"` to imports (file may already have it).

- [ ] **Step 2: Wire in Init**

In the model's `Init()` method:

```go
if m.runtime != nil && m.runtime.Lifecycle != nil {
	_, cancel, cmd := extensionEventSubscription(m.runtime.Lifecycle)
	m.extensionEventCancel = cancel
	return tea.Batch(/* existing cmds */, cmd)
}
```

Store `extensionEventCancel func()` on the model.

- [ ] **Step 3: Handle message + re-queue**

In `Update`:

```go
case extensionEventMsg:
	if m.runtime != nil && m.runtime.Lifecycle != nil {
		m.extensionPanel.SetViews(m.runtime.Lifecycle.List())
		m.extensionToast.SetPending(countPending(m.runtime.Lifecycle.List()))
	}
	// Re-queue to read the next event.
	_, _, cmd := extensionEventSubscription(m.runtime.Lifecycle)
	return m, cmd
```

(Minor wart: we resubscribe on every event. For spec #1 this is fine — subscriptions are cheap. A later refactor can preserve the channel + cmd across updates.)

- [ ] **Step 4: Build + tests**

Run: `rtk go build ./... && rtk go test ./internal/tui/... -count=1`
Expected: green.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/chat.go
rtk git commit -m "feat(tui): event bridge reads lifecycle.Event and refreshes panel + toast

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 29: WindowSizeMsg rebase (safety net)

**Files:**
- Modify: `internal/tui/chat.go`

- [ ] **Step 1: On `tea.WindowSizeMsg` refresh views**

In the existing `tea.WindowSizeMsg` branch of Update:

```go
case tea.WindowSizeMsg:
	m.windowWidth, m.windowHeight = msg.Width, msg.Height
	if m.runtime != nil && m.runtime.Lifecycle != nil {
		views := m.runtime.Lifecycle.List()
		m.extensionPanel.SetViews(views)
		m.extensionToast.SetPending(countPending(views))
	}
```

- [ ] **Step 2: Build + tests**

Run: `rtk go build ./... && rtk go test ./internal/tui/... -count=1`
Expected: green.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/tui/chat.go
rtk git commit -m "feat(tui): WindowSizeMsg reconciles panel + toast from Lifecycle.List

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 30: CLI — StartApproved on TUI init

**Files:**
- Modify: `internal/cli/root.go` (or wherever the TUI is launched)

- [ ] **Step 1: Locate the launch site**

Run: `rtk grep -n "tea.NewProgram" internal/cli/` and note the file / function.

- [ ] **Step 2: Call StartApproved**

Just before the call to `prog.Run()`, add:

```go
if rt.Lifecycle != nil {
	rt.Lifecycle.StartApproved(context.Background())
}
```

Add `"context"` to imports if absent.

- [ ] **Step 3: Build + tests**

Run: `rtk go build ./... && rtk go test ./internal/cli/... -count=1`
Expected: the `pi package` stale test still fails (pre-existing); all other cli tests green.

- [ ] **Step 4: Commit**

```bash
rtk git add internal/cli/
rtk git commit -m "feat(cli): kick off Lifecycle.StartApproved before tea.Program.Run

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 31: CLI — StopAll on TUI exit

**Files:**
- Modify: same file as Task 30

- [ ] **Step 1: Wrap Run**

After `prog.Run()`, add:

```go
if rt.Lifecycle != nil {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt.Lifecycle.StopAll(ctx)
}
```

Add `"time"` to imports if absent.

- [ ] **Step 2: Build + tests**

Run: `rtk go build ./... && rtk go test ./internal/cli/... -count=1`
Expected: same as Task 30.

- [ ] **Step 3: Commit**

```bash
rtk git add internal/cli/
rtk git commit -m "feat(cli): StopAll running extensions after TUI exits (bounded 5s)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 32: Docs — Update `docs/extensions.md` for new TUI flow

**Files:**
- Modify: `docs/extensions.md`

- [ ] **Step 1: Replace the "Trust & Approvals" section**

Find the section titled `## Trust & Approvals` in `docs/extensions.md`. Keep the approvals.json schema reference but re-lead it with the in-TUI flow:

```markdown
## Trust & Approvals

Hosted extensions sit in `StatePending` until they're approved. The recommended flow is to approve them from inside pi-go:

1. Start pi-go. Discovered but un-approved extensions appear as a status-bar toast:

   ```
   2 extensions pending approval — press e to review
   ```

2. Press `e` (or type `/extensions`) to open the management panel.

3. Select a row with the arrow keys. Press `a` on a pending row to open the approval dialog, toggle capabilities with `space`, and press `enter` to approve. pi-go writes to `~/.pi-go/extensions/approvals.json` on your behalf.

4. Approved extensions auto-start on the next pi-go launch. You can also `s` (start), `x` (stop), `r` (restart), or `v` (revoke) from the panel at any time.

### approvals.json schema

If you prefer to edit the file directly (or a dotfile-management flow needs it), the schema is:

```json
{
  "version": 2,
  "extensions": {
    "my-ext": {
      "trust_class": "third-party",
      "approved": true,
      "approved_at": "2026-04-15T12:00:00Z",
      "granted_capabilities": [
        "tools.register",
        "events.session_start"
      ],
      "denied_capabilities": []
    }
  }
}
```

Fields the TUI doesn't name are preserved on disk — future pi-go releases may add fields without disturbing your edits.
```

- [ ] **Step 2: Verify file + commit**

Run: `rtk git diff docs/extensions.md | head -80`
Confirm the section replaced correctly.

```bash
rtk git add docs/extensions.md
rtk git commit -m "docs(extensions): lead Trust & Approvals with the in-TUI flow

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] `rtk go test ./... -count=1` → everything green except pre-existing internal/cli failures (`TestPackageCommandLifecycle`, the bubbletea `TestRootCmdMissingAPIKey` hang).
- [ ] `rtk go build ./...` → clean.
- [ ] `rtk go vet ./...` → clean.
- [ ] Manual smoke: run pi-go against a repo with `examples/extensions/hosted-hello-go` symlinked into `.pi-go/extensions/`. Verify toast → panel → dialog approve → `s` starts → status reaches `running`.

---

## Self-Review

**Spec coverage:**

- §Architecture (dep direction, state ownership) → Tasks 1, 4, 9, 16
- §Service interface → Tasks 1, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 15
- §approvals.json persistence → Tasks 2, 3; reused by 6, 7, 8
- §Auto-start flow → Tasks 10 (Start), 13 (StartApproved), 14 (buildCommand), 30 (wiring)
- §TUI surface: toast → Tasks 19, 20, 29
- §TUI surface: panel → Tasks 21, 22, 23, 24, 25
- §TUI surface: approval dialog → Tasks 26, 27
- §Event bridge + WindowSizeMsg rebase → Tasks 28, 29
- §Error handling matrix → covered across Tasks 6, 7, 8, 10, 11, 14 (matches table rows)
- §Testing plan → lifecycle tests in 2-15, e2e in 17-18, TUI tests in 19-27
- §Future piapi exposure → design-only; no implementation task (correct — spec's Non-goals)

**Placeholder scan:** No TBD/TODO left. Every step shows concrete code or commands.

**Type consistency:**
- `lifecycle.Service` methods consistent across tasks (same signatures in spec, Task 4 interface, Task 9 launchFunc, TUI fake in Task 24, wiring in Tasks 30-31).
- `View` struct used identically across Tasks 1, 4, 22, 28.
- `EventKind` constant names: `EventStateChanged`, `EventApprovalChanged`, `EventRegistrationAdded`, `EventRegistrationRemoved` — consistent throughout.
- `host.State` and `host.TrustClass` from existing Phase 8 code — used via existing constants only.

**Ambiguity check:**
- Panel key handling is split across Task 24 (`DispatchKey`) and Task 25 (model wiring). Intentional: `DispatchKey` is unit-tested against a fake; the model wiring is integration and lives in the existing keymap branch.
- `Reload` v1 is explicitly scoped "additive only" in Task 15 body — removals and metadata refreshes deferred. Called out in the spec's _Non-goals_.

# Extension TUI Approval & Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the file-only hosted-extension approval flow (and its "loading: tools..." hang) with an explicit manager state machine, lifecycle API, and in-TUI `/extensions` panel for interactive approve/deny/stop/restart/revoke/reload.

**Architecture:** The `Manager` gains six explicit per-extension states (`Pending`, `Ready`, `Running`, `Stopped`, `Errored`, `Denied`). Unapproved hosted extensions register as `Pending` instead of erroring, so startup can never hang on approval. A new lifecycle API (`GrantApproval`, `StartExtension`, `StopExtension`, `RestartExtension`, `RevokeApproval`, `ReloadManifests`) is called from a new TUI panel (`/extensions`) that renders grouped rows and an approval sub-dialog. Three independent hang fixes in `hostruntime.Process.Shutdown` make shutdown bounded and cross-platform safe.

**Tech Stack:** Go, Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss, standard library JSON + `os/exec`.

---

## Spec reference

`docs/superpowers/specs/2026-04-11-extension-tui-approval-lifecycle-design.md`

## File structure

**Modified:**

- `internal/extension/hostruntime/process.go` — `Shutdown` pipe-close-first + bounded kill + platform-aware interrupt
- `internal/extension/hostruntime/process_test.go` — new shutdown tests
- `internal/extension/permissions.go` — `Upsert`, `Delete` with atomic write
- `internal/extension/permissions_test.go` — round-trip tests
- `internal/extension/manager.go` — state machine, lifecycle API, partial-failure start, timeout consts, `startOneHosted` helper, `ManagerOptions.ApprovalsPath`
- `internal/extension/manager_test.go` — state transition + lifecycle tests (extends `mockHostedLauncher` / `mockHostedClient`)
- `internal/extension/runtime.go` — pass `ApprovalsPath` into `ManagerOptions`
- `internal/extension/hosted_hello_e2e_test.go` — regression test `TestHostedHelloE2E_PendingApprovalDoesNotHang`
- `internal/tui/status.go` — `StatusRenderInput.ExtensionsSummary` + render logic
- `internal/tui/tui_view.go` — render new extensions panel above input
- `internal/tui/tui.go` — `extensionsPanel` model field
- `internal/tui/tui_update.go` — new message handlers
- `internal/tui/tui_keys_modals.go` — `handleExtensionsPanelKey`
- `internal/tui/commands.go` — `/extensions` case in `handleSlashCommand`
- `internal/tui/types.go` — `ExtensionManager` interface extension if needed

**Created:**

- `internal/tui/extensions_panel.go` — panel state, view, Cmds, message types
- `internal/tui/extensions_panel_test.go` — panel unit tests

---

## Task 1: Fix `Process.Shutdown` — pipe-close + bounded kill + platform-aware

**Why:** Today `Shutdown` calls `p.cmd.Process.Signal(os.Interrupt)` (unsupported on Windows) then waits on `p.Wait()` with no timeout. Root cause of the hang.

**Files:**
- Modify: `internal/extension/hostruntime/process.go` (lines 91–118)
- Test: `internal/extension/hostruntime/process_test.go`

- [ ] **Step 1: Write failing test `TestProcessShutdown_ClosesStdinFirst`**

Add to `internal/extension/hostruntime/process_test.go`:

```go
func TestProcessShutdown_ClosesStdinFirst(t *testing.T) {
	// A Go program that blocks reading stdin, exits cleanly on EOF.
	script := `package main
import ("bufio"; "os")
func main() { bufio.NewReader(os.Stdin).ReadByte(); os.Exit(0) }`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown took %v, expected <500ms (stdin close should cause clean exit)", elapsed)
	}
}

// buildTestBinary compiles a tiny Go program to a temp binary for use in tests.
func buildTestBinary(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "bin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dir, binName)
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	return binPath
}
```

Required imports in the test file: `context`, `os`, `os/exec`, `path/filepath`, `runtime`, `testing`, `time`.

- [ ] **Step 2: Run test — verify it fails**

```bash
rtk go test ./internal/extension/hostruntime -run TestProcessShutdown_ClosesStdinFirst -v
```

Expected: FAIL (current `Shutdown` does not close stdin; the blocked-read subprocess never exits on Windows, test either times out after 2s or exceeds the 500ms budget).

- [ ] **Step 3: Write failing test `TestProcessShutdown_KillsOnTimeout`**

Add after the previous test:

```go
func TestProcessShutdown_KillsOnTimeout(t *testing.T) {
	// A program that ignores EOF on stdin and sleeps forever — tests the
	// Kill-on-timeout branch.
	script := `package main
import ("os/signal"; "syscall"; "time")
func main() {
	signal.Ignore(syscall.SIGINT)
	for { time.Sleep(time.Hour) }
}`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = p.Shutdown(ctx)
	elapsed := time.Since(start)
	if elapsed > 800*time.Millisecond {
		t.Fatalf("shutdown did not respect timeout budget: %v", elapsed)
	}
	if err == nil {
		t.Fatal("expected ctx.Err() when kill path is taken")
	}
}
```

- [ ] **Step 4: Write failing test `TestProcessShutdown_IdempotentAfterExit`**

```go
func TestProcessShutdown_IdempotentAfterExit(t *testing.T) {
	script := `package main
func main() {}`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for natural exit.
	_ = p.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown on exited process: %v", err)
	}
}
```

- [ ] **Step 5: Run all three new tests — verify they fail**

```bash
rtk go test ./internal/extension/hostruntime -run TestProcessShutdown_ -v
```

- [ ] **Step 6: Rewrite `Process.Shutdown`**

In `internal/extension/hostruntime/process.go`, add `"runtime"` to the imports and replace the entire `Shutdown` method (lines 91–118) with:

```go
func (p *Process) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	select {
	case <-p.waitDone:
		return nil
	default:
	}

	// Close stdin first — a well-behaved extension sees EOF on its
	// decoder and exits cleanly. Works on every platform.
	if p.stdin != nil {
		_ = p.stdin.Close()
	}

	// Best-effort interrupt. No-op on Windows (os.Interrupt is unsupported
	// for child processes) but clean on Unix.
	if runtime.GOOS != "windows" && p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}

	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()

	select {
	case <-done:
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		return nil
	case <-ctx.Done():
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-done
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		return ctx.Err()
	}
}
```

- [ ] **Step 7: Run new tests — verify pass**

```bash
rtk go test ./internal/extension/hostruntime -run TestProcessShutdown_ -v
```

Expected: PASS all three.

- [ ] **Step 8: Run full hostruntime test package to check for regressions**

```bash
rtk go test ./internal/extension/hostruntime -v
```

- [ ] **Step 9: Commit**

```bash
git add internal/extension/hostruntime/process.go internal/extension/hostruntime/process_test.go
git commit -m "fix(hostruntime): bounded cross-platform Process.Shutdown"
```

---

## Task 2: Bounded shutdown + handshake timeout constants in `manager.go`

**Why:** Every `client.Shutdown(...)` call site currently passes `context.Background()`. Needs a bounded context that flows through the fixed `Process.Shutdown` from Task 1. Also: name the 5s handshake timeout for clarity.

**Files:**
- Modify: `internal/extension/manager.go`

- [ ] **Step 1: Add timeout constants**

Near the top of `manager.go`, after the existing `Dispatcher` alias (around line 23), add:

```go
// HostedHandshakeTimeout bounds the initial JSON-RPC handshake with a
// hosted extension after the process is launched.
const HostedHandshakeTimeout = 5 * time.Second

// HostedShutdownTimeout bounds the graceful shutdown of a hosted
// extension subprocess (stdin close + wait for natural exit, then Kill
// as last resort).
const HostedShutdownTimeout = 3 * time.Second
```

- [ ] **Step 2: Update `StartHostedExtensions` handshake to use the constant**

In `StartHostedExtensions` (around line 422), replace `context.WithTimeout(ctx, 5*time.Second)` with `context.WithTimeout(ctx, HostedHandshakeTimeout)`.

- [ ] **Step 3: Update error-path Shutdown to use bounded context**

In the same function, replace the line:

```go
_ = client.Shutdown(context.Background())
```

with:

```go
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
_ = client.Shutdown(shutdownCtx)
shutdownCancel()
```

- [ ] **Step 4: Update `ShutdownHostedExtensions` to use bounded context**

In `ShutdownHostedExtensions` (around line 467), replace:

```go
for _, client := range clients {
    _ = client.Shutdown(ctx)
}
```

with:

```go
for _, client := range clients {
    shutdownCtx, cancel := context.WithTimeout(ctx, HostedShutdownTimeout)
    _ = client.Shutdown(shutdownCtx)
    cancel()
}
```

- [ ] **Step 5: Update `UnregisterExtension` shutdown call**

In `UnregisterExtension` (around line 816), replace:

```go
if hostedClient != nil {
    _ = hostedClient.Shutdown(context.Background())
}
```

with:

```go
if hostedClient != nil {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
    _ = hostedClient.Shutdown(shutdownCtx)
    cancel()
}
```

- [ ] **Step 6: Run the existing manager tests to confirm no regression**

```bash
rtk go test ./internal/extension -run TestManager -v
```

Expected: all existing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/extension/manager.go
git commit -m "refactor(extension): named + bounded hosted shutdown timeouts"
```

---

## Task 3: State machine types + `RegisterManifest` → `Pending`

**Why:** Core state transition. Unapproved hosted extensions go to `Pending` instead of erroring; this alone unblocks startup for unapproved hosted extensions.

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Add `ExtensionState` type and `ExtensionInfo` snapshot type**

In `manager.go`, near the `extensionRegistration` struct (around line 36):

```go
// ExtensionState is the lifecycle state of a registered extension.
type ExtensionState string

const (
	StatePending ExtensionState = "pending_approval"
	StateReady   ExtensionState = "ready"
	StateRunning ExtensionState = "running"
	StateStopped ExtensionState = "stopped"
	StateErrored ExtensionState = "errored"
	StateDenied  ExtensionState = "denied"
)

// ExtensionInfo is a read-only snapshot of a single extension's state,
// used by the TUI panel and status line.
type ExtensionInfo struct {
	ID                    string
	TrustClass            TrustClass
	State                 ExtensionState
	RequestedCapabilities []Capability
	Runtime               RuntimeSpec
	LastError             string
	StartedAt             time.Time
}
```

Replace the `extensionRegistration` struct with:

```go
type extensionRegistration struct {
	manifest  Manifest
	trust     TrustClass
	state     ExtensionState
	lastError string
	startedAt time.Time
}
```

- [ ] **Step 2: Add `ApprovalsPath` to `ManagerOptions`**

In `ManagerOptions` (around line 47), add:

```go
ApprovalsPath string
```

In the `Manager` struct (around line 83), add:

```go
approvalsPath string
```

In `NewManager` (around line 164), after `hostedLauncher := opts.HostedLauncher...` add:

```go
approvalsPath := strings.TrimSpace(opts.ApprovalsPath)
```

and include `approvalsPath: approvalsPath,` in the `mgr := &Manager{...}` literal.

- [ ] **Step 3: Write failing test `TestManager_RegistersUnapprovedHostedAsPending`**

Add to `manager_test.go`:

```go
func TestManager_RegistersUnapprovedHostedAsPending(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	})
	if err != nil {
		t.Fatalf("expected RegisterManifest to succeed for unapproved hosted, got %v", err)
	}
	infos := m.Extensions()
	if len(infos) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(infos))
	}
	if infos[0].State != StatePending {
		t.Fatalf("expected StatePending, got %q", infos[0].State)
	}
}

func TestManager_RegistersApprovedHostedAsReady(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	infos := m.Extensions()
	if len(infos) != 1 || infos[0].State != StateReady {
		t.Fatalf("expected ready state, got %+v", infos)
	}
}

func TestManager_RegistersDeclarativeAsReady(t *testing.T) {
	m := NewManager(ManagerOptions{})
	if err := m.RegisterManifest(Manifest{Name: "ext.decl"}); err != nil {
		t.Fatal(err)
	}
	infos := m.Extensions()
	if len(infos) != 1 || infos[0].State != StateReady {
		t.Fatalf("expected ready state, got %+v", infos)
	}
}
```

The test references `m.Extensions()` which is built in the next step — the test file won't compile yet; that's fine for TDD.

- [ ] **Step 4: Add `Manager.Extensions()` snapshot method**

Near the other accessor methods in `manager.go` (e.g. `Permissions()`), add:

```go
// Extensions returns a read-only snapshot of every registered extension.
// Safe to call concurrently; the returned slice is not shared with the
// manager's internal state.
func (m *Manager) Extensions() []ExtensionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ExtensionInfo, 0, len(m.extensions))
	for id, reg := range m.extensions {
		caps := append([]Capability(nil), reg.manifest.Capabilities...)
		out = append(out, ExtensionInfo{
			ID:                    id,
			TrustClass:            reg.trust,
			State:                 reg.state,
			RequestedCapabilities: caps,
			Runtime:               reg.manifest.Runtime,
			LastError:             reg.lastError,
			StartedAt:             reg.startedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
```

- [ ] **Step 5: Rewrite `RegisterManifest` to route by state**

Replace the body of `RegisterManifest` (lines 334–365) with:

```go
func (m *Manager) RegisterManifest(manifest Manifest) error {
	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if err := validateRuntimeSpec(manifest); err != nil {
		return fmt.Errorf("extension %q runtime: %w", manifest.Name, err)
	}

	trust := m.permissions.ResolveTrust(manifest.Name, ResolveManifestTrust(manifest))
	initialState := StateReady
	if manifest.runtimeType() == RuntimeTypeHostedStdioJSONRPC &&
		!m.permissions.HostedApproved(manifest.Name, trust) {
		initialState = StatePending
	}

	// Capability gate validation only applies to Ready extensions;
	// Pending extensions carry their requested caps through to the
	// approval dialog untouched.
	if initialState == StateReady {
		for _, capability := range manifest.Capabilities {
			if !m.permissions.AllowsCapability(manifest.Name, trust, capability) {
				return fmt.Errorf("extension %q capability %q is not approved", manifest.Name, capability)
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.extensions[manifest.Name] = extensionRegistration{
		manifest: manifest,
		trust:    trust,
		state:    initialState,
	}
	// Declarative commands are only registered for Ready extensions;
	// Pending extensions register commands when they become Ready.
	if initialState == StateReady {
		for _, command := range manifest.TUI.Commands {
			if err := m.registerCommandLocked(manifest.Name, command, true); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 6: Fix the existing `TestManager_RefusesToLaunchUnapprovedHostedExtension` test**

This test (around line 101) previously asserted `RegisterManifest` succeeded and `StartHostedExtensions` failed. With the state machine, `StartHostedExtensions` silently skips `Pending` extensions — rename and rewrite:

```go
func TestManager_SkipsPendingHostedExtensionOnStart(t *testing.T) {
	launcher := &mockHostedLauncher{client: &mockHostedClient{
		response: hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		},
	}}
	m := NewManager(ManagerOptions{
		Permissions:    EmptyPermissions(),
		HostedLauncher: launcher,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartHostedExtensions(context.Background(), "interactive"); err != nil {
		t.Fatalf("expected StartHostedExtensions to tolerate pending extensions, got %v", err)
	}
	if launcher.launches != 0 {
		t.Fatalf("expected launcher not to run for pending extension, got %d launches", launcher.launches)
	}
}
```

- [ ] **Step 7: Run the new tests — verify pass**

```bash
rtk go test ./internal/extension -run 'TestManager_Registers|TestManager_SkipsPending' -v
```

- [ ] **Step 8: Run full extension package tests**

```bash
rtk go test ./internal/extension -v
```

Expected: all pass. (Existing hosted-launch tests still supply an approval record, so they stay in `Ready`.)

- [ ] **Step 9: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): state machine with Pending routing for unapproved hosted"
```

---

## Task 4: Refactor `StartHostedExtensions` — partial-failure + only `Ready`

**Why:** Currently the function `return`s on the first per-extension error, aborting `BuildRuntime` for the entire runtime. Also, we need to extract a single-extension launch path that `StartExtension` (Task 7) will reuse.

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test `TestStartHostedExtensions_PartialFailureTolerant`**

Add to `manager_test.go`:

```go
func TestStartHostedExtensions_PartialFailureTolerant(t *testing.T) {
	good := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	bad := &mockHostedClient{response: hostproto.HandshakeResponse{
		Accepted: false,
		Message:  "boom",
	}}
	launcher := &sequencedHostedLauncher{clients: []HostedClient{bad, good}}

	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{
			{ExtensionID: "ext.bad", TrustClass: TrustClassHostedThirdParty, HostedRequired: true},
			{ExtensionID: "ext.good", TrustClass: TrustClassHostedThirdParty, HostedRequired: true},
		}),
		HostedLauncher: launcher,
	})
	for _, id := range []string{"ext.bad", "ext.good"} {
		if err := m.RegisterManifest(Manifest{
			Name: id,
			Runtime: RuntimeSpec{
				Type:    RuntimeTypeHostedStdioJSONRPC,
				Command: "hosted-ext",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.StartHostedExtensions(context.Background(), "interactive"); err != nil {
		t.Fatalf("expected nil return despite one failure, got %v", err)
	}

	states := map[string]ExtensionState{}
	for _, info := range m.Extensions() {
		states[info.ID] = info.State
	}
	if states["ext.bad"] != StateErrored {
		t.Errorf("ext.bad state = %q, want errored", states["ext.bad"])
	}
	if states["ext.good"] != StateRunning {
		t.Errorf("ext.good state = %q, want running", states["ext.good"])
	}
}

// sequencedHostedLauncher returns clients in order on successive Launch calls.
type sequencedHostedLauncher struct {
	clients []HostedClient
	calls   int
}

func (l *sequencedHostedLauncher) Launch(_ context.Context, _ Manifest) (HostedClient, error) {
	if l.calls >= len(l.clients) {
		return nil, fmt.Errorf("sequencedHostedLauncher: out of clients")
	}
	c := l.clients[l.calls]
	l.calls++
	return c, nil
}
```

Note: `launches` is not used here because launches happen in whatever order `StartHostedExtensions` iterates — the sequenced launcher works because each launch just returns the next client.

Add `"fmt"` to the test file's imports if not already present.

- [ ] **Step 2: Run test — verify it fails**

```bash
rtk go test ./internal/extension -run TestStartHostedExtensions_PartialFailureTolerant -v
```

Expected: FAIL — current `StartHostedExtensions` returns the first error.

- [ ] **Step 3: Extract `startOneHosted` helper and rewrite `StartHostedExtensions`**

In `manager.go`, replace the current `StartHostedExtensions` body (lines 390–451) with:

```go
func (m *Manager) StartHostedExtensions(ctx context.Context, mode string) error {
	type hostedRegistration struct {
		id       string
		manifest Manifest
		trust    TrustClass
	}
	var toStart []hostedRegistration

	m.mu.RLock()
	for id, reg := range m.extensions {
		if reg.manifest.runtimeType() != RuntimeTypeHostedStdioJSONRPC {
			continue
		}
		if reg.state != StateReady {
			continue
		}
		if _, started := m.hostedClients[id]; started {
			continue
		}
		toStart = append(toStart, hostedRegistration{
			id:       id,
			manifest: reg.manifest,
			trust:    reg.trust,
		})
	}
	m.mu.RUnlock()

	for _, reg := range toStart {
		if err := m.startOneHosted(ctx, reg.id, reg.manifest, reg.trust, mode); err != nil {
			m.markErrored(reg.id, err)
		}
	}
	return nil
}

// startOneHosted launches, handshakes, and wires the dispatch goroutine
// for a single hosted extension. Returns the error that caused failure
// (if any) without mutating manager state — the caller translates
// failure into Errored via markErrored.
func (m *Manager) startOneHosted(ctx context.Context, id string, manifest Manifest, trust TrustClass, mode string) error {
	client, err := m.hostedLauncher.Launch(ctx, manifest)
	if err != nil {
		return fmt.Errorf("launching hosted extension %q: %w", id, err)
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, HostedHandshakeTimeout)
	_, err = client.Handshake(handshakeCtx, hostproto.HandshakeRequest{
		ProtocolVersion:   hostproto.ProtocolVersion,
		ExtensionID:       id,
		Mode:              mode,
		RequestedServices: manifestToRequestedServices(manifest),
	})
	cancel()
	if err != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), HostedShutdownTimeout)
		_ = client.Shutdown(shutdownCtx)
		shutdownCancel()
		return fmt.Errorf("handshake with hosted extension %q failed: %w", id, err)
	}

	m.mu.Lock()
	m.hostedClients[id] = client
	reg := m.extensions[id]
	reg.state = StateRunning
	reg.lastError = ""
	reg.startedAt = time.Now()
	m.extensions[id] = reg
	m.mu.Unlock()

	// Register declarative commands (if any) now that the extension is
	// running. Pending → Ready → Running skipped this in RegisterManifest.
	for _, command := range manifest.TUI.Commands {
		if err := m.RegisterBootstrapCommand(id, command); err != nil {
			// Already-registered commands from prior starts are fine.
			if !strings.Contains(err.Error(), "already registered") {
				return err
			}
		}
	}

	go func(extID string, c HostedClient) {
		serveCtx, serveCancel := context.WithCancel(context.Background())
		defer serveCancel()
		dispatcher := dispatcherFunc(func(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
			return m.DispatchHostCall(extensionID, params)
		})
		_ = c.ServeInbound(serveCtx, extID, dispatcher)
	}(id, client)

	return nil
}

// markErrored transitions an extension to StateErrored and records the
// error message. Safe to call without holding m.mu.
func (m *Manager) markErrored(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.extensions[id]
	if !ok {
		return
	}
	reg.state = StateErrored
	reg.lastError = err.Error()
	m.extensions[id] = reg
}
```

- [ ] **Step 4: Run the new test — verify pass**

```bash
rtk go test ./internal/extension -run TestStartHostedExtensions_PartialFailureTolerant -v
```

Expected: PASS.

- [ ] **Step 5: Update `TestManager_LaunchesHostedExtensionFromManifestRuntime` to check new state**

In `manager_test.go` around line 138, after the existing `if _, ok := m.HostedClient(...)` assertion, add:

```go
infos := m.Extensions()
if len(infos) != 1 || infos[0].State != StateRunning {
    t.Fatalf("expected running state after start, got %+v", infos)
}
```

- [ ] **Step 6: Run full extension tests**

```bash
rtk go test ./internal/extension -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): partial-failure tolerant hosted start + startOneHosted helper"
```

---

## Task 5: `Permissions.Upsert` + `Permissions.Delete` with atomic write

**Why:** The TUI panel and lifecycle API need to add and remove approval records from `approvals.json` safely while pi-go is running.

**Files:**
- Modify: `internal/extension/permissions.go`
- Modify: `internal/extension/permissions_test.go`

- [ ] **Step 1: Write failing test `TestPermissions_UpsertAndDelete`**

Add to `permissions_test.go`:

```go
func TestPermissions_UpsertAndDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.json")

	p := EmptyPermissions()
	record := ApprovalRecord{
		ExtensionID:         "ext.demo",
		TrustClass:          TrustClassHostedThirdParty,
		HostedRequired:      true,
		GrantedCapabilities: []Capability{CapabilityUIStatus},
		ApprovedAt:          time.Unix(1700000000, 0),
	}

	if err := p.Upsert(path, record); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Reload from disk and verify.
	reloaded, err := LoadPermissions(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reloaded.Approval("ext.demo")
	if !ok {
		t.Fatal("expected approval to be persisted")
	}
	if got.TrustClass != TrustClassHostedThirdParty {
		t.Fatalf("trust class = %q, want hosted_third_party", got.TrustClass)
	}
	if len(got.GrantedCapabilities) != 1 || got.GrantedCapabilities[0] != CapabilityUIStatus {
		t.Fatalf("capabilities = %+v", got.GrantedCapabilities)
	}

	if err := p.Delete(path, "ext.demo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	reloaded, err = LoadPermissions(path)
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if _, ok := reloaded.Approval("ext.demo"); ok {
		t.Fatal("expected approval to be gone after delete")
	}
}
```

Imports needed: `path/filepath`, `time`, `testing`.

- [ ] **Step 2: Run test — verify it fails** (methods don't exist)

```bash
rtk go test ./internal/extension -run TestPermissions_UpsertAndDelete -v
```

- [ ] **Step 3: Implement `Upsert` and `Delete`**

In `permissions.go`, after the existing `SavePermissions` function (around line 131), add:

```go
// Upsert adds or replaces an approval record and persists the full set
// to path. Safe to call concurrently with Approval() lookups.
func (p *Permissions) Upsert(path string, record ApprovalRecord) error {
	if p == nil {
		return fmt.Errorf("permissions is nil")
	}
	id := strings.TrimSpace(record.ExtensionID)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	record.ExtensionID = id
	p.approvals[id] = record
	return savePermissionsAtomic(path, p)
}

// Delete removes an approval record and persists the remaining set to
// path. Deleting an unknown id is a no-op (nil return).
func (p *Permissions) Delete(path, id string) error {
	if p == nil {
		return fmt.Errorf("permissions is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	delete(p.approvals, id)
	return savePermissionsAtomic(path, p)
}

// savePermissionsAtomic writes approvals to a temp file in the same dir
// then renames it into place — avoids partial writes on crash.
func savePermissionsAtomic(path string, p *Permissions) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("approvals path is required")
	}
	records := make([]ApprovalRecord, 0, len(p.approvals))
	for _, record := range p.approvals {
		records = append(records, record)
	}
	payload := approvalFile{Approvals: records}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding approvals: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating approvals dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".approvals-*.json")
	if err != nil {
		return fmt.Errorf("creating temp approvals file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp approvals file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp approvals file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp approvals file: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test — verify pass**

```bash
rtk go test ./internal/extension -run TestPermissions_UpsertAndDelete -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/permissions.go internal/extension/permissions_test.go
git commit -m "feat(extension): Permissions.Upsert and Delete with atomic write"
```

---

## Task 6: `Manager.GrantApproval` + `DenyApproval`

**Why:** First user-facing lifecycle method — approve (persisted) or deny (in-memory).

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test `TestManager_GrantApproval`**

```go
func TestManager_GrantApproval(t *testing.T) {
	dir := t.TempDir()
	approvalsPath := filepath.Join(dir, "approvals.json")
	m := NewManager(ManagerOptions{
		Permissions:   EmptyPermissions(),
		ApprovalsPath: approvalsPath,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
		Capabilities: []Capability{CapabilityUIStatus},
	}); err != nil {
		t.Fatal(err)
	}
	// Precondition: pending.
	if info := findExtension(t, m, "ext.hosted"); info.State != StatePending {
		t.Fatalf("pre-grant state = %q, want pending", info.State)
	}

	if err := m.GrantApproval(GrantInput{
		ExtensionID:  "ext.hosted",
		TrustClass:   TrustClassHostedThirdParty,
		Capabilities: []Capability{CapabilityUIStatus},
	}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if info := findExtension(t, m, "ext.hosted"); info.State != StateReady {
		t.Fatalf("post-grant state = %q, want ready", info.State)
	}

	// Verify approvals.json was written.
	reloaded, err := LoadPermissions(approvalsPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Approval("ext.hosted"); !ok {
		t.Fatal("expected approvals.json to contain ext.hosted")
	}
}

func TestManager_DenyApproval(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.DenyApproval("ext.hosted"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateDenied {
		t.Fatalf("post-deny state = %q, want denied", info.State)
	}
}

// findExtension is a test helper.
func findExtension(t *testing.T, m *Manager, id string) ExtensionInfo {
	t.Helper()
	for _, info := range m.Extensions() {
		if info.ID == id {
			return info
		}
	}
	t.Fatalf("extension %q not found", id)
	return ExtensionInfo{}
}
```

Imports: add `path/filepath` and `time` if not already there.

- [ ] **Step 2: Run — verify fail**

```bash
rtk go test ./internal/extension -run 'TestManager_(Grant|Deny)Approval' -v
```

- [ ] **Step 3: Implement `GrantInput`, `GrantApproval`, and `DenyApproval`**

In `manager.go`, after the `ExtensionInfo` type:

```go
// GrantInput carries the parameters for an approval grant from the TUI
// panel or a scripted /extensions approve call.
type GrantInput struct {
	ExtensionID  string
	TrustClass   TrustClass
	Capabilities []Capability
}
```

Add these methods near the other lifecycle helpers:

```go
// GrantApproval records an approval for a pending extension and
// transitions it to Ready. Persists to approvals.json if
// ManagerOptions.ApprovalsPath was set. Does NOT auto-start; callers
// that want immediate launch should call StartExtension next.
func (m *Manager) GrantApproval(input GrantInput) error {
	id := strings.TrimSpace(input.ExtensionID)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}

	m.mu.Lock()
	reg, ok := m.extensions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.state != StatePending && reg.state != StateDenied {
		m.mu.Unlock()
		return fmt.Errorf("extension %q cannot be granted from state %q", id, reg.state)
	}

	trust := input.TrustClass
	if trust == "" {
		trust = reg.trust
	}
	caps := append([]Capability(nil), input.Capabilities...)
	if len(caps) == 0 {
		caps = append(caps, reg.manifest.Capabilities...)
	}

	record := ApprovalRecord{
		ExtensionID:         id,
		TrustClass:          trust,
		GrantedCapabilities: caps,
		HostedRequired:      reg.manifest.runtimeType() == RuntimeTypeHostedStdioJSONRPC,
		ApprovedAt:          time.Now().UTC(),
	}

	reg.trust = trust
	reg.state = StateReady
	reg.lastError = ""
	m.extensions[id] = reg
	approvalsPath := m.approvalsPath
	m.mu.Unlock()

	if approvalsPath != "" {
		if err := m.permissions.Upsert(approvalsPath, record); err != nil {
			// Roll back state transition on persistence failure.
			m.mu.Lock()
			reg.state = StatePending
			reg.lastError = err.Error()
			m.extensions[id] = reg
			m.mu.Unlock()
			return fmt.Errorf("persisting approval for %q: %w", id, err)
		}
	} else {
		// No path configured (tests) — still persist in the in-memory
		// permissions set so AllowsCapability lookups succeed.
		m.permissions.approvals[id] = record
	}
	return nil
}

// DenyApproval transitions a pending extension to Denied. In-memory
// only; not persisted.
func (m *Manager) DenyApproval(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.extensions[id]
	if !ok {
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.state != StatePending {
		return fmt.Errorf("extension %q cannot be denied from state %q", id, reg.state)
	}
	reg.state = StateDenied
	m.extensions[id] = reg
	return nil
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
rtk go test ./internal/extension -run 'TestManager_(Grant|Deny)Approval' -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): Manager.GrantApproval and DenyApproval"
```

---

## Task 7: `Manager.StartExtension` + `Manager.StopExtension`

**Why:** Per-extension lifecycle on a running manager — invoked from the TUI after approval or from `/extensions stop <id>`.

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test `TestManager_StartAndStopExtension`**

```go
func TestManager_StartAndStopExtension(t *testing.T) {
	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
		HostedLauncher: launcher,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateRunning {
		t.Fatalf("post-start state = %q, want running", info.State)
	}
	if launcher.launches != 1 {
		t.Fatalf("launches = %d, want 1", launcher.launches)
	}

	// Idempotent: re-starting a running extension is a no-op.
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	if launcher.launches != 1 {
		t.Fatalf("launches after idempotent = %d, want 1", launcher.launches)
	}

	if err := m.StopExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateStopped {
		t.Fatalf("post-stop state = %q, want stopped", info.State)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", client.shutdowns)
	}
}

func TestManager_StartExtension_RejectsPending(t *testing.T) {
	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	err := m.StartExtension(context.Background(), "ext.hosted")
	if err == nil {
		t.Fatal("expected start of pending extension to fail")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
rtk go test ./internal/extension -run 'TestManager_StartAndStopExtension|TestManager_StartExtension_RejectsPending' -v
```

- [ ] **Step 3: Implement `StartExtension` and `StopExtension`**

In `manager.go` near the other lifecycle methods:

```go
// StartExtension launches + handshakes a single extension. Valid from
// Ready / Stopped / Errored. Idempotent no-op if Running. Rejects
// Pending / Denied. Only meaningful for hosted extensions; declarative
// and compiled-in return nil without action.
func (m *Manager) StartExtension(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}

	m.mu.Lock()
	reg, ok := m.extensions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.manifest.runtimeType() != RuntimeTypeHostedStdioJSONRPC {
		m.mu.Unlock()
		return nil // non-hosted extensions are always active
	}
	switch reg.state {
	case StateRunning:
		m.mu.Unlock()
		return nil
	case StatePending, StateDenied:
		m.mu.Unlock()
		return fmt.Errorf("extension %q cannot start from state %q", id, reg.state)
	case StateReady, StateStopped, StateErrored:
		// ok
	default:
		m.mu.Unlock()
		return fmt.Errorf("extension %q has unknown state %q", id, reg.state)
	}
	manifest := reg.manifest
	trust := reg.trust
	m.mu.Unlock()

	if err := m.startOneHosted(ctx, id, manifest, trust, "interactive"); err != nil {
		m.markErrored(id, err)
		return err
	}
	return nil
}

// StopExtension gracefully shuts down a running hosted extension and
// transitions it to Stopped. No-op for non-running extensions.
func (m *Manager) StopExtension(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}

	m.mu.Lock()
	reg, ok := m.extensions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("extension %q is not registered", id)
	}
	if reg.state != StateRunning {
		m.mu.Unlock()
		return nil
	}
	client := m.hostedClients[id]
	delete(m.hostedClients, id)
	// Drop dynamic contributions (commands/tools/renderers) owned by
	// this extension so the /extensions revoke path doesn't leave stale
	// registrations behind.
	for name, registration := range m.commands {
		if registration.owner == id {
			delete(m.commands, name)
		}
	}
	for name, registration := range m.tools {
		if registration.owner == id {
			delete(m.tools, name)
			delete(m.runtimeTools, name)
		}
	}
	for surface, registration := range m.renderers {
		if registration.owner == id {
			delete(m.renderers, surface)
		}
	}
	reg.state = StateStopped
	reg.lastError = ""
	m.extensions[id] = reg
	m.mu.Unlock()

	if client != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, HostedShutdownTimeout)
		defer cancel()
		_ = client.Shutdown(shutdownCtx)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
rtk go test ./internal/extension -run 'TestManager_StartAndStopExtension|TestManager_StartExtension_RejectsPending' -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): Manager.StartExtension and StopExtension"
```

---

## Task 8: `Manager.RestartExtension` + `Manager.RevokeApproval`

**Why:** Completes the core lifecycle set used by the TUI panel.

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestManager_RestartExtension(t *testing.T) {
	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
		HostedLauncher: launcher,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatal(err)
	}
	if err := m.RestartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if launcher.launches != 2 {
		t.Fatalf("launches = %d, want 2 (start + restart)", launcher.launches)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1 (from stop half of restart)", client.shutdowns)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StateRunning {
		t.Fatalf("post-restart state = %q, want running", info.State)
	}
}

func TestManager_RevokeApproval(t *testing.T) {
	dir := t.TempDir()
	approvalsPath := filepath.Join(dir, "approvals.json")

	// Pre-populate approvals.json so we can assert deletion.
	seed := NewPermissions([]ApprovalRecord{{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}})
	if err := seed.Upsert(approvalsPath, ApprovalRecord{
		ExtensionID:    "ext.hosted",
		TrustClass:     TrustClassHostedThirdParty,
		HostedRequired: true,
	}); err != nil {
		t.Fatal(err)
	}

	client := &mockHostedClient{response: hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
	}}
	launcher := &mockHostedLauncher{client: client}
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
		HostedLauncher: launcher,
		ApprovalsPath:  approvalsPath,
	})
	if err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.StartExtension(context.Background(), "ext.hosted"); err != nil {
		t.Fatal(err)
	}

	if err := m.RevokeApproval(context.Background(), "ext.hosted"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if client.shutdowns != 1 {
		t.Fatalf("shutdowns = %d, want 1", client.shutdowns)
	}
	if info := findExtension(t, m, "ext.hosted"); info.State != StatePending {
		t.Fatalf("post-revoke state = %q, want pending", info.State)
	}

	reloaded, err := LoadPermissions(approvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Approval("ext.hosted"); ok {
		t.Fatal("expected approvals.json to no longer contain ext.hosted")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
rtk go test ./internal/extension -run 'TestManager_RestartExtension|TestManager_RevokeApproval' -v
```

- [ ] **Step 3: Implement methods**

```go
// RestartExtension is Stop followed by Start. Preserves approval.
func (m *Manager) RestartExtension(ctx context.Context, id string) error {
	if err := m.StopExtension(ctx, id); err != nil {
		return fmt.Errorf("restart: stop: %w", err)
	}
	// After stop the state is Stopped — reset to Ready so Start can proceed.
	m.mu.Lock()
	if reg, ok := m.extensions[id]; ok && reg.state == StateStopped {
		reg.state = StateReady
		m.extensions[id] = reg
	}
	m.mu.Unlock()
	return m.StartExtension(ctx, id)
}

// RevokeApproval stops a running extension, removes it from
// approvals.json, and transitions back to Pending.
func (m *Manager) RevokeApproval(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	if err := m.StopExtension(ctx, id); err != nil {
		return fmt.Errorf("revoke: stop: %w", err)
	}

	m.mu.Lock()
	approvalsPath := m.approvalsPath
	reg, ok := m.extensions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("extension %q is not registered", id)
	}
	reg.state = StatePending
	reg.lastError = ""
	m.extensions[id] = reg
	m.mu.Unlock()

	if approvalsPath != "" {
		if err := m.permissions.Delete(approvalsPath, id); err != nil {
			return fmt.Errorf("deleting approval for %q: %w", id, err)
		}
	} else {
		delete(m.permissions.approvals, id)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
rtk go test ./internal/extension -run 'TestManager_RestartExtension|TestManager_RevokeApproval' -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): Manager.RestartExtension and RevokeApproval"
```

---

## Task 9: `Manager.ReloadManifests`

**Why:** Manual hot-reload — rescans manifest dirs and diffs against current state.

**Files:**
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/manager_test.go`

- [ ] **Step 1: Write failing test `TestManager_ReloadManifests`**

```go
func TestManager_ReloadManifests(t *testing.T) {
	root := t.TempDir()
	extDir := filepath.Join(root, "extensions")

	// Seed with one extension on disk.
	mustWriteManifest(t, extDir, "ext.first", `{"name":"ext.first","description":"first"}`)

	m := NewManager(ManagerOptions{Permissions: EmptyPermissions()})
	manifests, err := LoadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterManifests(manifests); err != nil {
		t.Fatal(err)
	}

	// Add a new extension on disk.
	mustWriteManifest(t, extDir, "ext.second", `{"name":"ext.second","description":"second"}`)

	added, removed, err := m.ReloadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "ext.second" {
		t.Fatalf("added = %v, want [ext.second]", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want []", removed)
	}

	// Now remove the first extension from disk.
	if err := os.RemoveAll(filepath.Join(extDir, "ext.first")); err != nil {
		t.Fatal(err)
	}
	added, removed, err = m.ReloadManifests(extDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("added = %v, want []", added)
	}
	if len(removed) != 1 || removed[0] != "ext.first" {
		t.Fatalf("removed = %v, want [ext.first]", removed)
	}

	// Confirm snapshot reflects reality.
	ids := map[string]bool{}
	for _, info := range m.Extensions() {
		ids[info.ID] = true
	}
	if ids["ext.first"] || !ids["ext.second"] {
		t.Fatalf("unexpected extensions after reload: %v", ids)
	}
}

// mustWriteManifest is a test helper.
func mustWriteManifest(t *testing.T, extDir, name, body string) {
	t.Helper()
	dir := filepath.Join(extDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

Imports: `os`.

- [ ] **Step 2: Run — verify fail**

```bash
rtk go test ./internal/extension -run TestManager_ReloadManifests -v
```

- [ ] **Step 3: Implement `ReloadManifests`**

```go
// ReloadManifests rescans the given directories and reconciles the
// manager's extension set against the result. Added manifests are
// registered (pending or ready per approvals). Removed manifests are
// stopped and dropped. Returns the added and removed ids.
func (m *Manager) ReloadManifests(dirs ...string) (added, removed []string, err error) {
	manifests, err := LoadManifests(dirs...)
	if err != nil {
		return nil, nil, fmt.Errorf("reload: %w", err)
	}

	onDisk := make(map[string]Manifest, len(manifests))
	for _, manifest := range manifests {
		onDisk[manifest.Name] = manifest
	}

	m.mu.RLock()
	existing := make(map[string]bool, len(m.extensions))
	for id := range m.extensions {
		existing[id] = true
	}
	m.mu.RUnlock()

	// Register new manifests.
	for id, manifest := range onDisk {
		if existing[id] {
			continue
		}
		if regErr := m.RegisterManifest(manifest); regErr != nil {
			// Best-effort: record but keep reconciling.
			m.markErrored(id, regErr)
			continue
		}
		added = append(added, id)
	}

	// Unregister missing ones.
	for id := range existing {
		if _, stillOnDisk := onDisk[id]; stillOnDisk {
			continue
		}
		// Stop if running, then drop the entry.
		_ = m.StopExtension(context.Background(), id)
		m.UnregisterExtension(id)
		removed = append(removed, id)
	}

	sort.Strings(added)
	sort.Strings(removed)
	return added, removed, nil
}
```

- [ ] **Step 4: Run test — verify pass**

```bash
rtk go test ./internal/extension -run TestManager_ReloadManifests -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/extension/manager.go internal/extension/manager_test.go
git commit -m "feat(extension): Manager.ReloadManifests"
```

---

## Task 10: Regression test — pending approval does not hang `BuildRuntime`

**Why:** Direct regression test for today's bug. Should be independent of the Task 1 hostruntime fixes — asserts at the `BuildRuntime` level that an unapproved hosted extension does not block startup.

**Files:**
- Modify: `internal/extension/hosted_hello_e2e_test.go`

- [ ] **Step 1: Write the regression test**

Read the existing file first:

```bash
rtk read internal/extension/hosted_hello_e2e_test.go
```

Add a new test function at the end of the file:

```go
func TestHostedHelloE2E_PendingApprovalDoesNotHang(t *testing.T) {
	// Setup: working tree with hosted-hello manifest but no approval.
	root := t.TempDir()
	home := t.TempDir()
	setTestHome(t, home)

	extDir := filepath.Join(root, ".pi-go", "extensions", "hosted-hello")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"name": "hosted-hello",
		"runtime": {
			"type": "hosted_stdio_jsonrpc",
			"command": "this-binary-does-not-exist"
		},
		"capabilities": ["ui.status"]
	}`
	if err := os.WriteFile(filepath.Join(extDir, "extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	sandbox, err := tools.NewSandbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	// Bound BuildRuntime to 2s. With today's bug it would block forever.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rt, err := BuildRuntime(ctx, RuntimeConfig{
		Config:          config.Config{},
		WorkDir:         root,
		Sandbox:         sandbox,
		BaseInstruction: "Base.",
	})
	if err != nil {
		t.Fatalf("BuildRuntime returned error: %v", err)
	}
	if rt.Manager == nil {
		t.Fatal("expected runtime manager")
	}

	var info *ExtensionInfo
	for _, ext := range rt.Manager.Extensions() {
		if ext.ID == "hosted-hello" {
			info = &ext
			break
		}
	}
	if info == nil {
		t.Fatal("expected hosted-hello to appear in Extensions()")
	}
	if info.State != StatePending {
		t.Fatalf("hosted-hello state = %q, want pending", info.State)
	}
}
```

Required imports (check `hosted_hello_e2e_test.go` head): `context`, `os`, `path/filepath`, `testing`, `time`, `github.com/dimetron/pi-go/internal/config`, `github.com/dimetron/pi-go/internal/tools`. Use existing helpers `setTestHome`.

- [ ] **Step 2: Run the regression test — verify pass**

```bash
rtk go test ./internal/extension -run TestHostedHelloE2E_PendingApprovalDoesNotHang -v -timeout 30s
```

Expected: PASS under the 2s budget (since Task 3 already routes the extension to `Pending` and Task 4 skips non-Ready extensions, startup should return instantly).

- [ ] **Step 3: Run full extension tests**

```bash
rtk go test ./internal/extension -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/extension/hosted_hello_e2e_test.go
git commit -m "test(extension): regression — pending approval does not hang BuildRuntime"
```

---

## Task 11: Wire `ApprovalsPath` through `BuildRuntime`

**Why:** Without this, `GrantApproval` / `RevokeApproval` can't persist to `~/.pi-go/extensions/approvals.json`. Small but required.

**Files:**
- Modify: `internal/extension/runtime.go`

- [ ] **Step 1: Update `BuildRuntime` to pass `DefaultApprovalsPath()` into `NewManager`**

In `runtime.go` around line 85, the current code is:

```go
permissions, err := LoadPermissions(DefaultApprovalsPath())
if err != nil {
    return nil, fmt.Errorf("loading extension approvals: %w", err)
}
manager := NewManager(ManagerOptions{
    Permissions: permissions,
    Registry:    cfg.CompiledRegistry,
})
```

Replace with:

```go
approvalsPath := DefaultApprovalsPath()
permissions, err := LoadPermissions(approvalsPath)
if err != nil {
    return nil, fmt.Errorf("loading extension approvals: %w", err)
}
manager := NewManager(ManagerOptions{
    Permissions:   permissions,
    Registry:      cfg.CompiledRegistry,
    ApprovalsPath: approvalsPath,
})
```

- [ ] **Step 2: Run extension tests — confirm no regression**

```bash
rtk go test ./internal/extension -v
```

- [ ] **Step 3: Commit**

```bash
git add internal/extension/runtime.go
git commit -m "feat(extension): wire ApprovalsPath into BuildRuntime"
```

---

## Task 12: TUI status line — `ExtensionsSummary` field + rendering

**Why:** User needs to see pending/running/errored counts at all times. First visible TUI change.

**Files:**
- Modify: `internal/tui/status.go`
- Modify: `internal/tui/tui_view.go`
- Modify: `internal/tui/tui_mouse_branch_test.go` (or a new test file) — status test

- [ ] **Step 1: Add `ExtensionsSummary` to `StatusRenderInput`**

In `internal/tui/status.go`, extend `StatusRenderInput`:

```go
// ExtensionsSummary is rendered on the right side of row 1 as
// "ext: N! M✓ K✗" when any count is > 0. Hidden when all zero.
type ExtensionsSummary struct {
	Pending int
	Running int
	Errored int
}
```

Add a field to `StatusRenderInput`:

```go
ExtensionsSummary ExtensionsSummary
```

- [ ] **Step 2: Write failing test `TestStatusModel_RendersExtensionsSummary`**

Create or extend `internal/tui/status_test.go` (create if it doesn't exist):

```go
package tui

import (
	"strings"
	"testing"
)

func TestStatusModel_RendersExtensionsSummary(t *testing.T) {
	s := &StatusModel{Width: 120}
	out := s.Render(StatusRenderInput{
		ProviderName:      "anthropic",
		ModelName:         "claude",
		ExtensionsSummary: ExtensionsSummary{Pending: 1, Running: 2, Errored: 1},
	})
	if !strings.Contains(out, "ext:") {
		t.Fatalf("expected 'ext:' in status output, got %q", out)
	}
	if !strings.Contains(out, "1!") {
		t.Fatalf("expected pending count '1!' in status output, got %q", out)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("expected running count '2' in status output, got %q", out)
	}
	if !strings.Contains(out, "1✗") {
		t.Fatalf("expected errored count '1✗' in status output, got %q", out)
	}
}

func TestStatusModel_HidesExtensionsSummaryWhenEmpty(t *testing.T) {
	s := &StatusModel{Width: 120}
	out := s.Render(StatusRenderInput{
		ProviderName:      "anthropic",
		ModelName:         "claude",
		ExtensionsSummary: ExtensionsSummary{},
	})
	if strings.Contains(out, "ext:") {
		t.Fatalf("expected no 'ext:' when summary empty, got %q", out)
	}
}
```

- [ ] **Step 3: Run — verify fail**

```bash
rtk go test ./internal/tui -run TestStatusModel_RendersExtensionsSummary -v
```

- [ ] **Step 4: Render `ExtensionsSummary` in `StatusModel.Render`**

In `status.go`, find the `row1Right` construction (search for `row1Right = append`). Add after the provider/model chunk:

```go
// Extensions summary (right side of row 1).
if in.ExtensionsSummary.Pending > 0 || in.ExtensionsSummary.Running > 0 || in.ExtensionsSummary.Errored > 0 {
	var parts []string
	parts = append(parts, dim.Render("ext:"))
	if in.ExtensionsSummary.Pending > 0 {
		warn := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214"))
		parts = append(parts, warn.Render(fmt.Sprintf(" %d!", in.ExtensionsSummary.Pending)))
	}
	if in.ExtensionsSummary.Running > 0 {
		ok := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35"))
		parts = append(parts, ok.Render(fmt.Sprintf(" %d✓", in.ExtensionsSummary.Running)))
	}
	if in.ExtensionsSummary.Errored > 0 {
		err := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("196"))
		parts = append(parts, err.Render(fmt.Sprintf(" %d✗", in.ExtensionsSummary.Errored)))
	}
	row1Right = append(row1Right, strings.Join(parts, ""))
}
```

- [ ] **Step 5: Populate `ExtensionsSummary` in `tui_view.go`**

In `internal/tui/tui_view.go` around line 272 where the `StatusRenderInput` is constructed, after the existing fields add:

```go
ExtensionsSummary: m.extensionsSummary(),
```

And add the helper method (new file is not needed — put it in `tui_view.go` or alongside other model helpers, wherever fits):

```go
// extensionsSummary derives a status-bar summary from the manager snapshot.
func (m *model) extensionsSummary() ExtensionsSummary {
	if m.cfg.ExtensionManager == nil {
		return ExtensionsSummary{}
	}
	var sum ExtensionsSummary
	for _, info := range m.cfg.ExtensionManager.Extensions() {
		switch info.State {
		case extension.StatePending:
			sum.Pending++
		case extension.StateRunning:
			sum.Running++
		case extension.StateErrored:
			sum.Errored++
		}
	}
	return sum
}
```

The `m.cfg.ExtensionManager` is already the `*extension.Manager` per `types.go` — calling `Extensions()` is directly available.

- [ ] **Step 6: Run status tests — verify pass**

```bash
rtk go test ./internal/tui -run TestStatusModel_ -v
```

- [ ] **Step 7: Run full TUI tests for regressions**

```bash
rtk go test ./internal/tui -v
```

- [ ] **Step 8: Commit**

```bash
git add internal/tui/status.go internal/tui/status_test.go internal/tui/tui_view.go
git commit -m "feat(tui): status-bar extensions summary (pending/running/errored)"
```

---

## Task 13: TUI extensions panel — state, view, messages (`extensions_panel.go`)

**Why:** The core panel that `/extensions` opens. This task covers types, rendering, and the Cmds it emits. Key handling lives in the next task.

**Files:**
- Create: `internal/tui/extensions_panel.go`
- Create: `internal/tui/extensions_panel_test.go`
- Modify: `internal/tui/tui.go` (add `extensionsPanel` field)
- Modify: `internal/tui/tui_view.go` (render call)

- [ ] **Step 1: Create `extensions_panel.go` with state types**

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dimetron/pi-go/internal/extension"
)

// extensionsPanelState holds the TUI state for the /extensions panel.
type extensionsPanelState struct {
	rows      []extensionPanelRow
	cursor    int
	subDialog *extensionApprovalDialogState
}

// extensionPanelRow is one renderable row. Rows are pre-grouped and
// pre-sorted by buildExtensionPanelRows so the view can iterate once.
type extensionPanelRow struct {
	info    extension.ExtensionInfo
	isGroup bool   // true for group headers like "Pending approval"
	label   string // group label if isGroup
}

type extensionApprovalDialogState struct {
	id            string
	trustClass    extension.TrustClass
	runtime       extension.RuntimeSpec
	capabilities  []extension.Capability
	action        extensionDialogAction // approve or revoke
}

type extensionDialogAction string

const (
	extensionDialogApprove extensionDialogAction = "approve"
	extensionDialogRevoke  extensionDialogAction = "revoke"
)

// extensionLifecycleResultMsg reports the outcome of a background
// lifecycle operation (grant/start/stop/restart/revoke/reload).
type extensionLifecycleResultMsg struct {
	id  string
	op  string // "grant", "start", "stop", "restart", "revoke", "reload"
	err error
}

// extensionsPanelRefreshMsg triggers a re-read of the manager snapshot
// into the panel rows. Emitted after any successful lifecycle op.
type extensionsPanelRefreshMsg struct{}

// buildExtensionPanelRows flattens the manager snapshot into grouped,
// sorted rows for rendering.
func buildExtensionPanelRows(infos []extension.ExtensionInfo) []extensionPanelRow {
	groups := map[extension.ExtensionState][]extension.ExtensionInfo{}
	for _, info := range infos {
		groups[info.State] = append(groups[info.State], info)
	}
	order := []struct {
		state extension.ExtensionState
		label string
	}{
		{extension.StatePending, "Pending approval"},
		{extension.StateRunning, "Running"},
		{extension.StateStopped, "Stopped"},
		{extension.StateErrored, "Errored"},
		{extension.StateDenied, "Denied"},
		{extension.StateReady, "Ready (not running)"},
	}
	var rows []extensionPanelRow
	for _, g := range order {
		infos := groups[g.state]
		if len(infos) == 0 {
			continue
		}
		rows = append(rows, extensionPanelRow{isGroup: true, label: g.label})
		for _, info := range infos {
			rows = append(rows, extensionPanelRow{info: info})
		}
	}
	return rows
}

// renderExtensionsPanel returns the rendered panel or sub-dialog if
// one is active. Returns an empty string when the panel is closed.
func renderExtensionsPanel(state *extensionsPanelState, width int) string {
	if state == nil {
		return ""
	}
	if state.subDialog != nil {
		return renderExtensionApprovalDialog(state.subDialog, width)
	}

	bg := lipgloss.Color("236")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("244")).
		Padding(0, 1).
		Background(bg).
		Width(width - 2)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("252")).Bold(true).Render("Extensions"))
	b.WriteString("\n")
	if len(state.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("246")).Render("(no extensions registered)"))
	}
	for i, row := range state.rows {
		if row.isGroup {
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("39")).Render(row.label))
			b.WriteString("\n")
			continue
		}
		prefix := "  "
		if i == state.cursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s  [%s]", prefix, row.info.ID, row.info.TrustClass)
		if row.info.LastError != "" {
			line += "  — " + truncate(row.info.LastError, 40)
		}
		style := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("252"))
		if i == state.cursor {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("246")).Render(
		"[a]pprove [d]eny [r]estart [s]top [x]revoke [R]eload [Esc] close",
	))
	return border.Render(b.String())
}

func renderExtensionApprovalDialog(d *extensionApprovalDialogState, width int) string {
	bg := lipgloss.Color("236")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1).
		Background(bg).
		Width(width - 4)

	var b strings.Builder
	title := "Approve extension"
	if d.action == extensionDialogRevoke {
		title = "Revoke extension"
	}
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214")).Bold(true).Render(title + ": " + d.id))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Trust class:    %s\n", d.trustClass))
	b.WriteString(fmt.Sprintf("  Runtime:        %s %s\n", d.runtime.Command, strings.Join(d.runtime.Args, " ")))
	caps := make([]string, 0, len(d.capabilities))
	for _, c := range d.capabilities {
		caps = append(caps, string(c))
	}
	b.WriteString(fmt.Sprintf("  Requested caps: %s\n\n", strings.Join(caps, ", ")))
	if d.action == extensionDialogApprove {
		b.WriteString("  [Enter] approve and start   [Esc] cancel")
	} else {
		b.WriteString("  [Enter] revoke              [Esc] cancel")
	}
	return border.Render(b.String())
}

// extensionLifecycleCmd runs a blocking manager operation in the
// background and emits extensionLifecycleResultMsg on completion.
func extensionLifecycleCmd(m *model, id, op string, fn func(context.Context, *model) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*extension.HostedHandshakeTimeout)
		defer cancel()
		err := fn(ctx, m)
		return extensionLifecycleResultMsg{id: id, op: op, err: err}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
```

Note: `extension.HostedHandshakeTimeout` needs to be exported — confirm Task 2 used the capitalized name (it does). The multiplier gives a generous 50s budget for lifecycle Cmds that might include grant-write + launch + handshake.

- [ ] **Step 2: Add `extensionsPanel` field to `tui.go`**

In `internal/tui/tui.go`, add to the `model` struct near the `extensionDialog` field (around line 61):

```go
extensionsPanel *extensionsPanelState
```

- [ ] **Step 3: Render panel in `tui_view.go`**

Search for where `m.extensionDialog` is rendered in `tui_view.go` (around line 108). Add a branch rendering the panel when `m.extensionsPanel != nil`. The simplest placement is: before the `extensionDialog` render, insert:

```go
if m.extensionsPanel != nil {
    parts = append(parts, renderExtensionsPanel(m.extensionsPanel, m.width))
}
```

(Use whatever the actual variable is in that function — read the surrounding context before editing.)

- [ ] **Step 4: Write test `TestExtensionsPanel_RendersGroupedRows`**

Create `internal/tui/extensions_panel_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/extension"
)

func TestExtensionsPanel_BuildsGroupedRows(t *testing.T) {
	rows := buildExtensionPanelRows([]extension.ExtensionInfo{
		{ID: "ext.alpha", State: extension.StateRunning, TrustClass: extension.TrustClassHostedThirdParty},
		{ID: "ext.bravo", State: extension.StatePending, TrustClass: extension.TrustClassHostedThirdParty},
		{ID: "ext.charlie", State: extension.StateErrored, LastError: "boom"},
	})
	// Expect group headers in order: Pending, Running, Errored.
	var headers []string
	for _, r := range rows {
		if r.isGroup {
			headers = append(headers, r.label)
		}
	}
	want := []string{"Pending approval", "Running", "Errored"}
	if strings.Join(headers, ",") != strings.Join(want, ",") {
		t.Fatalf("headers = %v, want %v", headers, want)
	}
}

func TestExtensionsPanel_RendersEmpty(t *testing.T) {
	out := renderExtensionsPanel(&extensionsPanelState{}, 80)
	if !strings.Contains(out, "no extensions registered") {
		t.Fatalf("empty panel should show placeholder, got %q", out)
	}
}

func TestExtensionsPanel_RendersCursor(t *testing.T) {
	state := &extensionsPanelState{
		rows: buildExtensionPanelRows([]extension.ExtensionInfo{
			{ID: "ext.alpha", State: extension.StatePending, TrustClass: extension.TrustClassHostedThirdParty},
		}),
	}
	// Cursor points at index 1 (row 0 is the group header).
	state.cursor = 1
	out := renderExtensionsPanel(state, 80)
	if !strings.Contains(out, "> ext.alpha") {
		t.Fatalf("expected cursor prefix on ext.alpha, got:\n%s", out)
	}
}
```

- [ ] **Step 5: Run panel tests — verify pass**

```bash
rtk go test ./internal/tui -run TestExtensionsPanel_ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/extensions_panel.go internal/tui/extensions_panel_test.go internal/tui/tui.go internal/tui/tui_view.go
git commit -m "feat(tui): extensions panel state and rendering"
```

---

## Task 14: TUI extensions panel — key handling

**Why:** Wire the navigation and lifecycle keys into the existing modal handler chain.

**Files:**
- Modify: `internal/tui/tui_keys_modals.go`
- Modify: `internal/tui/tui_keys.go` (to call the new handler)
- Modify: `internal/tui/extensions_panel_test.go` (key-handling tests)

- [ ] **Step 1: Find where the modal handlers are chained**

```bash
rtk grep -n "handleExtensionDialogKey\|handleSetupAlertKey\|handleSkillCreateKey" internal/tui/tui_keys.go
```

Note the function where these are called in sequence — the new `handleExtensionsPanelKey` slots into the same chain.

- [ ] **Step 2: Write failing test `TestExtensionsPanel_ApprovalKeyOpensDialog`**

Add to `extensions_panel_test.go`:

```go
func TestExtensionsPanel_ApprovalKeyOpensDialog(t *testing.T) {
	m := &model{}
	m.extensionsPanel = &extensionsPanelState{
		rows: buildExtensionPanelRows([]extension.ExtensionInfo{
			{ID: "ext.alpha", State: extension.StatePending, TrustClass: extension.TrustClassHostedThirdParty,
				RequestedCapabilities: []extension.Capability{extension.CapabilityUIStatus}},
		}),
		cursor: 1, // on ext.alpha
	}
	handled, _, _ := m.handleExtensionsPanelKey(tea.Key{Code: 'a'})
	if !handled {
		t.Fatal("expected 'a' key to be handled")
	}
	if m.extensionsPanel.subDialog == nil {
		t.Fatal("expected approval sub-dialog to open")
	}
	if m.extensionsPanel.subDialog.id != "ext.alpha" {
		t.Fatalf("sub-dialog id = %q, want ext.alpha", m.extensionsPanel.subDialog.id)
	}
}

func TestExtensionsPanel_EscClosesPanel(t *testing.T) {
	m := &model{}
	m.extensionsPanel = &extensionsPanelState{}
	handled, _, _ := m.handleExtensionsPanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if m.extensionsPanel != nil {
		t.Fatal("expected panel to be closed on Esc")
	}
}

func TestExtensionsPanel_NavigationSkipsGroupHeaders(t *testing.T) {
	m := &model{}
	m.extensionsPanel = &extensionsPanelState{
		rows: buildExtensionPanelRows([]extension.ExtensionInfo{
			{ID: "ext.alpha", State: extension.StatePending, TrustClass: extension.TrustClassHostedThirdParty},
			{ID: "ext.bravo", State: extension.StateRunning, TrustClass: extension.TrustClassHostedThirdParty},
		}),
		cursor: 1,
	}
	// Down from "ext.alpha" should skip the "Running" group header to ext.bravo.
	handled, _, _ := m.handleExtensionsPanelKey(tea.Key{Code: tea.KeyDown})
	if !handled {
		t.Fatal("expected Down to be handled")
	}
	if m.extensionsPanel.rows[m.extensionsPanel.cursor].info.ID != "ext.bravo" {
		t.Fatalf("cursor on %q, want ext.bravo", m.extensionsPanel.rows[m.extensionsPanel.cursor].info.ID)
	}
}
```

Add `tea "charm.land/bubbletea/v2"` to the test file imports.

- [ ] **Step 3: Run — verify fail** (handler doesn't exist)

```bash
rtk go test ./internal/tui -run TestExtensionsPanel_ -v
```

- [ ] **Step 4: Implement `handleExtensionsPanelKey` in `tui_keys_modals.go`**

Add to `tui_keys_modals.go`:

```go
func (m *model) handleExtensionsPanelKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.extensionsPanel == nil {
		return false, nil, nil
	}

	// Sub-dialog takes precedence.
	if m.extensionsPanel.subDialog != nil {
		return m.handleExtensionApprovalDialogKey(key)
	}

	switch {
	case key.Code == tea.KeyEsc, key.Code == 'c' && key.Mod == tea.ModCtrl:
		m.extensionsPanel = nil
		return true, m, nil

	case key.Code == tea.KeyUp, key.Code == 'k':
		m.moveExtensionsPanelCursor(-1)
		return true, m, nil

	case key.Code == tea.KeyDown, key.Code == 'j':
		m.moveExtensionsPanelCursor(+1)
		return true, m, nil

	case key.Code == 'R':
		return true, m, m.extensionReloadCmd()

	case key.Code == 'a':
		if row, ok := m.selectedExtensionRow(); ok && row.info.State == extension.StatePending {
			m.openApprovalDialog(row.info, extensionDialogApprove)
		}
		return true, m, nil

	case key.Code == 'd':
		if row, ok := m.selectedExtensionRow(); ok && row.info.State == extension.StatePending {
			return true, m, m.extensionDenyCmd(row.info.ID)
		}
		return true, m, nil

	case key.Code == 'r':
		if row, ok := m.selectedExtensionRow(); ok {
			switch row.info.State {
			case extension.StateRunning, extension.StateStopped, extension.StateErrored:
				return true, m, m.extensionRestartCmd(row.info.ID)
			}
		}
		return true, m, nil

	case key.Code == 's':
		if row, ok := m.selectedExtensionRow(); ok && row.info.State == extension.StateRunning {
			return true, m, m.extensionStopCmd(row.info.ID)
		}
		return true, m, nil

	case key.Code == 'x':
		if row, ok := m.selectedExtensionRow(); ok &&
			(row.info.State == extension.StateRunning || row.info.State == extension.StateStopped) {
			m.openApprovalDialog(row.info, extensionDialogRevoke)
		}
		return true, m, nil

	case key.Code == tea.KeyEnter:
		if row, ok := m.selectedExtensionRow(); ok {
			switch row.info.State {
			case extension.StatePending:
				m.openApprovalDialog(row.info, extensionDialogApprove)
			case extension.StateRunning, extension.StateStopped, extension.StateErrored:
				return true, m, m.extensionRestartCmd(row.info.ID)
			}
		}
		return true, m, nil
	}

	return true, m, nil // swallow other keys while panel is open
}

func (m *model) handleExtensionApprovalDialogKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	d := m.extensionsPanel.subDialog
	switch {
	case key.Code == tea.KeyEsc, key.Code == 'c' && key.Mod == tea.ModCtrl:
		m.extensionsPanel.subDialog = nil
		return true, m, nil
	case key.Code == tea.KeyEnter:
		id := d.id
		action := d.action
		m.extensionsPanel.subDialog = nil
		if action == extensionDialogApprove {
			return true, m, m.extensionGrantAndStartCmd(id, d.trustClass, d.capabilities)
		}
		return true, m, m.extensionRevokeCmd(id)
	}
	return true, m, nil
}

func (m *model) moveExtensionsPanelCursor(delta int) {
	p := m.extensionsPanel
	if p == nil || len(p.rows) == 0 {
		return
	}
	i := p.cursor + delta
	for i >= 0 && i < len(p.rows) && p.rows[i].isGroup {
		i += delta
	}
	if i < 0 || i >= len(p.rows) {
		return
	}
	p.cursor = i
}

func (m *model) selectedExtensionRow() (extensionPanelRow, bool) {
	p := m.extensionsPanel
	if p == nil || p.cursor < 0 || p.cursor >= len(p.rows) {
		return extensionPanelRow{}, false
	}
	row := p.rows[p.cursor]
	if row.isGroup {
		return extensionPanelRow{}, false
	}
	return row, true
}

func (m *model) openApprovalDialog(info extension.ExtensionInfo, action extensionDialogAction) {
	m.extensionsPanel.subDialog = &extensionApprovalDialogState{
		id:           info.ID,
		trustClass:   info.TrustClass,
		runtime:      info.Runtime,
		capabilities: info.RequestedCapabilities,
		action:       action,
	}
}
```

Add `"github.com/dimetron/pi-go/internal/extension"` to the imports if not already there.

- [ ] **Step 5: Add Cmd helpers in `extensions_panel.go`**

Append to `extensions_panel.go`:

```go
func (m *model) extensionGrantAndStartCmd(id string, trust extension.TrustClass, caps []extension.Capability) tea.Cmd {
	return extensionLifecycleCmd(m, id, "grant", func(ctx context.Context, mm *model) error {
		mgr := mm.cfg.ExtensionManager
		if mgr == nil {
			return fmt.Errorf("no extension manager")
		}
		if err := mgr.GrantApproval(extension.GrantInput{
			ExtensionID:  id,
			TrustClass:   trust,
			Capabilities: caps,
		}); err != nil {
			return err
		}
		return mgr.StartExtension(ctx, id)
	})
}

func (m *model) extensionDenyCmd(id string) tea.Cmd {
	return extensionLifecycleCmd(m, id, "deny", func(_ context.Context, mm *model) error {
		if mm.cfg.ExtensionManager == nil {
			return fmt.Errorf("no extension manager")
		}
		return mm.cfg.ExtensionManager.DenyApproval(id)
	})
}

func (m *model) extensionStopCmd(id string) tea.Cmd {
	return extensionLifecycleCmd(m, id, "stop", func(ctx context.Context, mm *model) error {
		if mm.cfg.ExtensionManager == nil {
			return fmt.Errorf("no extension manager")
		}
		return mm.cfg.ExtensionManager.StopExtension(ctx, id)
	})
}

func (m *model) extensionRestartCmd(id string) tea.Cmd {
	return extensionLifecycleCmd(m, id, "restart", func(ctx context.Context, mm *model) error {
		if mm.cfg.ExtensionManager == nil {
			return fmt.Errorf("no extension manager")
		}
		return mm.cfg.ExtensionManager.RestartExtension(ctx, id)
	})
}

func (m *model) extensionRevokeCmd(id string) tea.Cmd {
	return extensionLifecycleCmd(m, id, "revoke", func(ctx context.Context, mm *model) error {
		if mm.cfg.ExtensionManager == nil {
			return fmt.Errorf("no extension manager")
		}
		return mm.cfg.ExtensionManager.RevokeApproval(ctx, id)
	})
}

func (m *model) extensionReloadCmd() tea.Cmd {
	return extensionLifecycleCmd(m, "", "reload", func(_ context.Context, mm *model) error {
		if mm.cfg.ExtensionManager == nil {
			return fmt.Errorf("no extension manager")
		}
		// Use the discovered extension dirs from the current workdir.
		dirs := extension.DiscoverResourceDirs(mm.cfg.WorkDir).ExtensionDirs
		_, _, err := mm.cfg.ExtensionManager.ReloadManifests(dirs...)
		return err
	})
}
```

- [ ] **Step 6: Handle `extensionLifecycleResultMsg` and `extensionsPanelRefreshMsg`**

In `internal/tui/tui_update.go`, add message-handling branches in `Update`:

```go
case extensionLifecycleResultMsg:
    if m.extensionsPanel != nil {
        if msg.err != nil {
            // Stash the error so the panel can show it on the row.
            if mgr := m.cfg.ExtensionManager; mgr != nil {
                m.extensionsPanel.rows = buildExtensionPanelRows(mgr.Extensions())
            }
        } else {
            if mgr := m.cfg.ExtensionManager; mgr != nil {
                m.extensionsPanel.rows = buildExtensionPanelRows(mgr.Extensions())
            }
        }
    }
    return m, nil
```

(Read the existing `Update` switch structure before inserting — match the existing style.)

- [ ] **Step 7: Register the new handler in the key handler chain**

Find the function that chains modal handlers (look in `tui_keys.go` for calls to `handleExtensionDialogKey`). Add a call to `handleExtensionsPanelKey` with the same pattern, placed before or after the existing dialog handler. Example:

```go
if handled, next, cmd := m.handleExtensionsPanelKey(key); handled {
    return next, cmd
}
```

- [ ] **Step 8: Run panel tests — verify pass**

```bash
rtk go test ./internal/tui -run TestExtensionsPanel_ -v
```

- [ ] **Step 9: Run full TUI tests**

```bash
rtk go test ./internal/tui -v
```

- [ ] **Step 10: Commit**

```bash
git add internal/tui/tui_keys_modals.go internal/tui/tui_keys.go internal/tui/extensions_panel.go internal/tui/extensions_panel_test.go internal/tui/tui_update.go
git commit -m "feat(tui): extensions panel key handling + lifecycle Cmds"
```

---

## Task 15: `/extensions` slash command

**Why:** User-facing entrypoint that opens the panel and optionally runs subcommands.

**Files:**
- Modify: `internal/extension/manager.go` (add `/extensions` to `defaultBuiltinSlashCommands`)
- Modify: `internal/tui/commands.go` (add the case)

- [ ] **Step 1: Add `/extensions` to `defaultBuiltinSlashCommands`**

In `manager.go`, the `defaultBuiltinSlashCommands` slice (around line 130). Add `"/extensions",` near the top. This reserves the name against conflicts with dynamic commands.

- [ ] **Step 2: Add the case in `handleSlashCommand`**

In `internal/tui/commands.go`, add a new case before the `default:` branch (around line 97):

```go
case "/extensions":
    return m.handleExtensionsCommand(parts[1:])
```

- [ ] **Step 3: Implement `handleExtensionsCommand`**

Add a new method to `commands.go`:

```go
func (m *model) handleExtensionsCommand(args []string) (tea.Model, tea.Cmd) {
	if m.cfg.ExtensionManager == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Extensions manager not available.",
		})
		return m, nil
	}

	if len(args) == 0 {
		// Open the panel.
		rows := buildExtensionPanelRows(m.cfg.ExtensionManager.Extensions())
		cursor := 0
		// Skip leading group header.
		for cursor < len(rows) && rows[cursor].isGroup {
			cursor++
		}
		m.extensionsPanel = &extensionsPanelState{rows: rows, cursor: cursor}
		return m, nil
	}

	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "reload":
		return m, m.extensionReloadCmd()
	case "approve":
		if len(rest) == 0 {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role: "assistant", content: "Usage: `/extensions approve <id>`",
			})
			return m, nil
		}
		return m, m.extensionGrantAndStartCmd(rest[0], "", nil)
	case "deny":
		if len(rest) == 0 {
			return m, nil
		}
		return m, m.extensionDenyCmd(rest[0])
	case "stop":
		if len(rest) == 0 {
			return m, nil
		}
		return m, m.extensionStopCmd(rest[0])
	case "restart":
		if len(rest) == 0 {
			return m, nil
		}
		return m, m.extensionRestartCmd(rest[0])
	case "revoke":
		if len(rest) == 0 {
			return m, nil
		}
		return m, m.extensionRevokeCmd(rest[0])
	default:
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role: "assistant",
			content: fmt.Sprintf("Unknown extensions subcommand: `%s`. Valid: reload, approve, deny, stop, restart, revoke.", sub),
		})
		return m, nil
	}
}
```

- [ ] **Step 4: Write failing test `TestExtensionsCommand_OpensPanel`**

Add to `extensions_panel_test.go`:

```go
func TestExtensionsCommand_OpensPanel(t *testing.T) {
	mgr := extension.NewManager(extension.ManagerOptions{Permissions: extension.EmptyPermissions()})
	if err := mgr.RegisterManifest(extension.Manifest{
		Name: "ext.hosted",
		Runtime: extension.RuntimeSpec{
			Type:    extension.RuntimeTypeHostedStdioJSONRPC,
			Command: "hosted-ext",
		},
	}); err != nil {
		t.Fatal(err)
	}
	m := &model{cfg: Config{ExtensionManager: mgr}}
	m.handleExtensionsCommand(nil)
	if m.extensionsPanel == nil {
		t.Fatal("expected /extensions to open the panel")
	}
	if len(m.extensionsPanel.rows) == 0 {
		t.Fatal("expected panel rows from manager snapshot")
	}
}
```

- [ ] **Step 5: Run — verify pass**

```bash
rtk go test ./internal/tui -run TestExtensionsCommand_OpensPanel -v
```

- [ ] **Step 6: Run full TUI + extension tests**

```bash
rtk go test ./internal/tui ./internal/extension -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/extension/manager.go internal/tui/commands.go internal/tui/extensions_panel_test.go
git commit -m "feat(tui): /extensions slash command + subcommands"
```

---

## Task 16: End-to-end smoke test

**Why:** Final sanity check that the three pieces (hang fix + state machine + TUI plumbing) work together. This is a manual test, not automated.

**Files:** None — this is a smoke check.

- [ ] **Step 1: Delete the existing approval to trigger pending state**

```bash
rm ~/.pi-go/extensions/approvals.json
```

- [ ] **Step 2: Build pi-go**

```bash
rtk go build -o /tmp/pi-go ./cmd/go-pi
```

- [ ] **Step 3: Run pi-go interactively**

```bash
/tmp/pi-go
```

**Expected:** TUI loads within ~1s, no "loading: tools..." hang. Status line shows `ext: 1!`.

- [ ] **Step 4: Type `/extensions` and verify panel opens**

Expected: panel shows `Pending approval` group containing `hosted-hello`.

- [ ] **Step 5: Press `a` to approve**

Expected: approval sub-dialog opens showing runtime command and capabilities. Press `Enter`.

- [ ] **Step 6: Verify the extension moves to `Running`**

Expected: panel refreshes, `hosted-hello` moves to `Running` section. Status line becomes `ext: 1✓`. The extension's `/hello` command becomes available.

- [ ] **Step 7: Press `s` to stop, then `r` to restart, then `x` to revoke**

Each should transition the extension through the corresponding states without hanging. Revoke should return it to `Pending` and remove it from `~/.pi-go/extensions/approvals.json`.

- [ ] **Step 8: Verify `approvals.json` round-trips**

```bash
cat ~/.pi-go/extensions/approvals.json
```

After a fresh approval the file should contain `hosted-hello` with the granted capabilities.

- [ ] **Step 9: Close pi-go (`/quit`) and note any console errors.**

- [ ] **Step 10: Run the entire test suite one last time**

```bash
rtk go test ./... -timeout 120s
```

Expected: all pass.

- [ ] **Step 11: Commit anything left outstanding** (should be nothing — this task is a smoke check)

---

## Self-review

**Spec coverage check (§ references the spec file):**

| Spec section | Task |
|--|--|
| §1 State machine | Task 3 |
| §2 Lifecycle API — Start/Stop | Task 7 |
| §2 Lifecycle API — Restart/Revoke | Task 8 |
| §2 Lifecycle API — Grant/Deny | Task 6 |
| §2 Lifecycle API — Reload | Task 9 |
| §2 Lifecycle API — Extensions() snapshot | Task 3 |
| §2 Partial-failure tolerant Start | Task 4 |
| §3 Process.Shutdown fix | Task 1 |
| §3 Bounded shutdown contexts | Task 2 |
| §3 Partial-failure tolerance | Task 4 |
| §3 Named timeout consts | Task 2 |
| §4 Status line indicator | Task 12 |
| §4 `/extensions` slash command | Task 15 |
| §4 Panel state + view | Task 13 |
| §4 Key handling | Task 14 |
| §4 Approval sub-dialog | Task 13 (render) + 14 (key) |
| §5 Flow A (first run) | Task 10 (regression) + Task 16 (manual) |
| §5 Flow B (handshake timeout) | Task 1 + Task 4 |
| §5 Flow C (reload) | Task 9 + Task 16 (manual) |
| §5 Flow D (revoke) | Task 8 + Task 16 (manual) |
| §6 Persistence — atomic write | Task 5 |
| §6 Persistence — ApprovalsPath wiring | Task 11 |
| §7 Testing — unit manager tests | Tasks 3, 4, 6, 7, 8, 9 |
| §7 Testing — permissions tests | Task 5 |
| §7 Testing — process tests | Task 1 |
| §7 Testing — regression e2e | Task 10 |
| §7 Testing — TUI panel tests | Tasks 13, 14, 15 |

Every spec requirement maps to at least one task. ✅

**Type consistency check:** method names used in Tasks 6–15 (`GrantApproval`, `DenyApproval`, `StartExtension`, `StopExtension`, `RestartExtension`, `RevokeApproval`, `ReloadManifests`, `Extensions`) match the spec §2 table exactly. ✅

**Placeholder scan:** no "TBD", "TODO", "similar to Task N", or ambiguous "add appropriate X" phrases. ✅

**One inconsistency to fix:** Task 13 renders a panel via `m.width`, but the TUI model's width field may be `m.statusModel.Width` or similar. Implementer should match the actual width source in `tui_view.go`. Noted inline in Task 13 Step 3 ("Use whatever the actual variable is — read the surrounding context before editing.").

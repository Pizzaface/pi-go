# Hosted Showcase Go Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:
> executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a hosted Go extension at `examples/extensions/hosted-showcase-go/` that demonstrates every working Spec
#1 API pattern: multi-tool registration, session_start event handling, streaming updates, and system introspection.

**Architecture:** Single `main.go` using `piext.Run()` entrypoint. Registers 4 tools (`ext_info`, `ext_echo`,
`ext_sysinfo`, `ext_rpc_ping`) and a `session_start` event handler. Each tool exercises a distinct API pattern. Module
uses `replace` directives for local `piapi`/`piext` packages.

**Tech Stack:** Go, `pkg/piapi`, `pkg/piext`, `invopop/jsonschema` (via `piext.SchemaFromStruct`)

---

### Task 1: Scaffold module and pi.toml

**Files:**

- Create: `examples/extensions/hosted-showcase-go/pi.toml`
- Create: `examples/extensions/hosted-showcase-go/go.mod`

- [ ] **Step 1: Create pi.toml**

```toml
name = "hosted-showcase-go"
version = "0.1.0"
description = "Showcase hosted-go extension; demonstrates multi-tool registration, event handling, streaming updates, and system introspection."
runtime = "hosted"
command = ["go", "run", "."]
requested_capabilities = [
    "tools.register",
    "events.session_start",
    "events.tool_execute",
]
```

- [ ] **Step 2: Create go.mod**

```
module github.com/pizzaface/go-pi/examples/extensions/hosted-showcase-go

go 1.22

require (
	github.com/pizzaface/go-pi/pkg/piapi v0.0.0
	github.com/pizzaface/go-pi/pkg/piext v0.0.0
)

replace (
	github.com/pizzaface/go-pi/pkg/piapi => ../../../pkg/piapi
	github.com/pizzaface/go-pi/pkg/piext => ../../../pkg/piext
)
```

- [ ] **Step 3: Add module to go.work**

Add `./examples/extensions/hosted-showcase-go` to the `use` block in `go.work` at the repo root. The block currently
looks like:

```
use (
	.
	./pkg/piapi
	./pkg/piext
	./examples/extensions/hosted-hello-go
)
```

Add the new line so it becomes:

```
use (
	.
	./pkg/piapi
	./pkg/piext
	./examples/extensions/hosted-hello-go
	./examples/extensions/hosted-showcase-go
)
```

- [ ] **Step 4: Commit**

```bash
git add examples/extensions/hosted-showcase-go/pi.toml examples/extensions/hosted-showcase-go/go.mod go.work
git commit -m "feat(extensions): scaffold hosted-showcase-go module and pi.toml"
```

---

### Task 2: Implement main.go with Metadata, session_start handler, and ext_info tool

**Files:**

- Create: `examples/extensions/hosted-showcase-go/main.go`

This task creates the file with the entrypoint, metadata, session_start handler, and the first tool. Subsequent tasks
add tools to this same file.

- [ ] **Step 1: Create main.go with metadata, session_start, and ext_info**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:        "hosted-showcase-go",
	Version:     "0.1.0",
	Description: "Showcase hosted-go extension; demonstrates multi-tool registration, event handling, streaming updates, and system introspection.",
	RequestedCapabilities: []string{
		"tools.register",
		"events.session_start",
		"events.tool_execute",
	},
}

// sessionStartTime is set by the session_start handler; read by ext_info.
var sessionStartTime atomic.Value // stores time.Time

func register(pi piapi.API) error {
	// Subscribe to session_start.
	if err := pi.On(piapi.EventSessionStart, func(evt piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		sessionStartTime.Store(time.Now())
		se, _ := evt.(piapi.SessionStartEvent)
		fmt.Fprintln(piext.Log(), "hosted-showcase-go: session_start reason="+se.Reason)
		return piapi.EventResult{}, nil
	}); err != nil {
		return err
	}

	// Tool 1: ext_info — no parameters, returns extension metadata + runtime state.
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "ext_info",
		Label:       "Extension Info",
		Description: "Returns metadata and runtime state of the hosted-showcase-go extension: name, version, capabilities, Go version, PID, and uptime.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			uptime := "unknown (session_start not received)"
			if t, ok := sessionStartTime.Load().(time.Time); ok {
				uptime = time.Since(t).Round(time.Millisecond).String()
			}
			caps := strings.Join(Metadata.RequestedCapabilities, ", ")
			text := fmt.Sprintf(
				"Extension: %s v%s\nDescription: %s\nCapabilities: %s\nGo: %s\nPID: %d\nUptime: %s",
				Metadata.Name, Metadata.Version, Metadata.Description,
				caps, runtime.Version(), os.Getpid(), uptime,
			)
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: text}},
			}, nil
		},
	}); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-showcase-go: fatal:", err)
	}
}
```

- [ ] **Step 2: Run go mod tidy to generate go.sum**

Run from the extension directory:

```bash
cd examples/extensions/hosted-showcase-go && go mod tidy
```

Expected: `go.sum` is created with entries for `jsonschema` and its transitive deps (same as `hosted-hello-go/go.sum`).

- [ ] **Step 3: Verify it compiles**

```bash
cd examples/extensions/hosted-showcase-go && go build -o /dev/null .
```

Expected: exit 0, no errors.

- [ ] **Step 4: Commit**

```bash
git add examples/extensions/hosted-showcase-go/main.go examples/extensions/hosted-showcase-go/go.sum
git commit -m "feat(extensions): hosted-showcase-go with session_start + ext_info tool"
```

---

### Task 3: Add ext_echo tool

**Files:**

- Modify: `examples/extensions/hosted-showcase-go/main.go`

- [ ] **Step 1: Add ext_echo tool registration**

Insert the following block in `main.go` inside the `register` function, after the `ext_info` registration block and
before the final `return nil`:

```go
    // Tool 2: ext_echo — demonstrates complex schema with required/optional fields.
type echoArgs struct {
Message   string `json:"message"   jsonschema:"description=Text to echo back,required"`
Repeat    int    `json:"repeat"    jsonschema:"description=Number of repetitions,minimum=1,maximum=100"`
Uppercase bool   `json:"uppercase" jsonschema:"description=Whether to uppercase the output"`
}
if err := pi.RegisterTool(piapi.ToolDescriptor{
Name:        "ext_echo",
Label:       "Echo",
Description: "Echoes a message back with optional repetition and uppercasing. Demonstrates complex JSON Schema parameters with required and optional fields.",
Parameters:  piext.SchemaFromStruct(echoArgs{}),
Execute: func (_ context.Context, call piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
var a echoArgs
if len(call.Args) > 0 {
if err := json.Unmarshal(call.Args, &a); err != nil {
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: "invalid arguments: " + err.Error()}},
IsError: true,
}, nil
}
}
if a.Message == "" {
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: "message is required"}},
IsError: true,
}, nil
}
if a.Repeat < 1 {
a.Repeat = 1
}
if a.Repeat > 100 {
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: "repeat must be between 1 and 100"}},
IsError: true,
}, nil
}
msg := a.Message
if a.Uppercase {
msg = strings.ToUpper(msg)
}
lines := make([]string, a.Repeat)
for i := range lines {
lines[i] = msg
}
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: strings.Join(lines, "\n")}},
}, nil
},
}); err != nil {
return err
}
```

Note: `echoArgs` is declared as a local type inside `register`. The `strings` import is already present from Task 2.

- [ ] **Step 2: Verify it compiles**

```bash
cd examples/extensions/hosted-showcase-go && go build -o /dev/null .
```

Expected: exit 0, no errors.

- [ ] **Step 3: Commit**

```bash
git add examples/extensions/hosted-showcase-go/main.go
git commit -m "feat(extensions): add ext_echo tool with schema validation"
```

---

### Task 4: Add ext_sysinfo tool

**Files:**

- Modify: `examples/extensions/hosted-showcase-go/main.go`

- [ ] **Step 1: Add ext_sysinfo tool registration**

Insert the following block in `main.go` inside the `register` function, after the `ext_echo` registration block and
before the final `return nil`:

```go
    // Tool 3: ext_sysinfo — demonstrates system introspection via Go stdlib.
if err := pi.RegisterTool(piapi.ToolDescriptor{
Name:        "ext_sysinfo",
Label:       "System Info",
Description: "Returns system information: hostname, OS, architecture, Go version, CPUs, PID, working directory, and executable path.",
Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
Execute: func (_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
hostname, _ := os.Hostname()
wd, _ := os.Getwd()
exe, _ := os.Executable()
text := fmt.Sprintf(
"Hostname: %s\nOS: %s\nArch: %s\nGo: %s\nCPUs: %d\nPID: %d\nWorkDir: %s\nExecutable: %s",
hostname, runtime.GOOS, runtime.GOARCH, runtime.Version(),
runtime.NumCPU(), os.Getpid(), wd, exe,
)
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: text}},
}, nil
},
}); err != nil {
return err
}
```

No new imports needed — `os`, `runtime`, `fmt` are already imported from Task 2.

- [ ] **Step 2: Verify it compiles**

```bash
cd examples/extensions/hosted-showcase-go && go build -o /dev/null .
```

Expected: exit 0, no errors.

- [ ] **Step 3: Commit**

```bash
git add examples/extensions/hosted-showcase-go/main.go
git commit -m "feat(extensions): add ext_sysinfo tool with system introspection"
```

---

### Task 5: Add ext_rpc_ping tool with streaming updates

**Files:**

- Modify: `examples/extensions/hosted-showcase-go/main.go`

- [ ] **Step 1: Add ext_rpc_ping tool registration**

Insert the following block in `main.go` inside the `register` function, after the `ext_sysinfo` registration block and
before the final `return nil`:

```go
    // Tool 4: ext_rpc_ping — demonstrates streaming progress via UpdateFunc.
type pingArgs struct {
Count int `json:"count" jsonschema:"description=Number of pings (1-20),minimum=1,maximum=20"`
}
if err := pi.RegisterTool(piapi.ToolDescriptor{
Name:        "ext_rpc_ping",
Label:       "RPC Ping",
Description: "Measures internal operation timing and streams progress updates after each iteration. Demonstrates UpdateFunc streaming callbacks.",
Parameters:  piext.SchemaFromStruct(pingArgs{}),
Execute: func (_ context.Context, call piapi.ToolCall, onUpdate piapi.UpdateFunc) (piapi.ToolResult, error) {
var a pingArgs
if len(call.Args) > 0 {
if err := json.Unmarshal(call.Args, &a); err != nil {
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: "invalid arguments: " + err.Error()}},
IsError: true,
}, nil
}
}
if a.Count < 1 {
a.Count = 3
}
if a.Count > 20 {
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: "count must be between 1 and 20"}},
IsError: true,
}, nil
}

durations := make([]time.Duration, a.Count)
for i := 0; i < a.Count; i++ {
start := time.Now()
// Trivial operation to measure Go-side timing.
runtime.Gosched()
d := time.Since(start)
durations[i] = d

if onUpdate != nil {
onUpdate(piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: fmt.Sprintf("Ping %d/%d: %s", i+1, a.Count, d)}},
})
}
}

var min, max, total time.Duration
min = durations[0]
for _, d := range durations {
total += d
if d < min {
min = d
}
if d > max {
max = d
}
}
avg := total / time.Duration(a.Count)
text := fmt.Sprintf("Pings: %d\nMin: %s\nMax: %s\nAvg: %s\nTotal: %s", a.Count, min, max, avg, total)
return piapi.ToolResult{
Content: []piapi.ContentPart{{Type: "text", Text: text}},
}, nil
},
}); err != nil {
return err
}
```

The `time` import is already present from Task 2.

- [ ] **Step 2: Verify it compiles**

```bash
cd examples/extensions/hosted-showcase-go && go build -o /dev/null .
```

Expected: exit 0, no errors.

- [ ] **Step 3: Commit**

```bash
git add examples/extensions/hosted-showcase-go/main.go
git commit -m "feat(extensions): add ext_rpc_ping tool with streaming updates"
```

---

### Task 6: Add E2E test and approvals fixture

**Files:**

- Modify: `internal/extension/testdata/approvals_granted_hello.json`
- Create: `internal/extension/e2e_hosted_showcase_go_test.go`

- [ ] **Step 1: Add hosted-showcase-go to the approvals fixture**

The file `internal/extension/testdata/approvals_granted_hello.json` already has entries for `hosted-hello-go` and
`hosted-hello-ts`. Add a `hosted-showcase-go` entry to the `"extensions"` object:

```json
    "hosted-showcase-go": {
"trust_class": "third-party",
"first_party": false,
"approved": true,
"approved_at": "2026-04-17T00:00:00Z",
"granted_capabilities": [
"tools.register",
"events.session_start",
"events.tool_execute"
]
}
```

Insert it after the `hosted-hello-ts` entry (before the closing `}` of `"extensions"`).

- [ ] **Step 2: Create E2E test**

Create `internal/extension/e2e_hosted_showcase_go_test.go`:

```go
package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/host"
)

// TestE2E_HostedShowcaseGo exercises discovery → gate approval →
// BuildRuntime → LaunchHosted for the hosted-showcase-go example.
// It asserts the extension reaches StateRunning after handshake.
func TestE2E_HostedShowcaseGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hosted-showcase-go E2E under -short")
	}
	projectRoot, err := repoRoot()
	if err != nil {
		t.Skipf("locate repo root: %v", err)
	}
	exampleDir := filepath.Join(projectRoot, "examples", "extensions", "hosted-showcase-go")
	if _, err := os.Stat(filepath.Join(exampleDir, "main.go")); err != nil {
		t.Skipf("hosted-showcase-go example missing: %v", err)
	}

	tmp := t.TempDir()
	extsDir := filepath.Join(tmp, ".go-pi", "extensions")
	if err := os.MkdirAll(extsDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(extsDir, "hosted-showcase-go")
	if err := os.Symlink(exampleDir, target); err != nil {
		t.Skipf("symlink unsupported (Windows without admin?): %v", err)
	}

	approvalsSrc := filepath.Join("testdata", "approvals_granted_hello.json")
	approvalsData, err := os.ReadFile(approvalsSrc)
	if err != nil {
		t.Fatalf("read approvals fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extsDir, "approvals.json"), approvalsData, 0644); err != nil {
		t.Fatalf("write approvals: %v", err)
	}

	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := BuildRuntime(ctx, RuntimeConfig{WorkDir: tmp})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	reg := rt.Manager.Get("hosted-showcase-go")
	if reg == nil {
		t.Fatalf("expected hosted-showcase-go registration; got %v", extIDs(rt))
	}
	if reg.State != host.StateReady {
		t.Fatalf("expected StateReady after approved discovery; got %s", reg.State)
	}

	handler := extapi.NewHostedHandler(rt.Manager, reg)
	if err := host.LaunchHosted(ctx, reg, rt.Manager, []string{"go", "run", "."}, handler.Handle); err != nil {
		t.Fatalf("LaunchHosted: %v", err)
	}
	// Let Go start the child, finish compilation, and send its handshake.
	time.Sleep(1500 * time.Millisecond)

	reg = rt.Manager.Get("hosted-showcase-go")
	if reg.State != host.StateRunning {
		t.Fatalf("expected StateRunning after handshake; got %s (err=%v)", reg.State, reg.Err)
	}

	rt.Manager.Shutdown(ctx)

	reg = rt.Manager.Get("hosted-showcase-go")
	if reg.State != host.StateStopped {
		t.Fatalf("expected StateStopped after Shutdown; got %s", reg.State)
	}
}
```

This follows the exact same pattern as `e2e_hosted_go_test.go`. The helper functions `repoRoot()` and `extIDs()` are
already defined in that file and available within the same package.

- [ ] **Step 3: Verify existing tests still pass**

```bash
cd internal/extension && go test -short -count=1 ./...
```

Expected: all short tests pass (E2E tests skip under `-short`).

- [ ] **Step 4: Run the full E2E test**

```bash
cd internal/extension && go test -run TestE2E_HostedShowcaseGo -count=1 -v -timeout 60s
```

Expected: test passes — extension reaches `StateRunning` then `StateStopped`.

- [ ] **Step 5: Commit**

```bash
git add internal/extension/testdata/approvals_granted_hello.json internal/extension/e2e_hosted_showcase_go_test.go
git commit -m "test(extensions): add E2E test for hosted-showcase-go"
```

---

### Task 7: Final verification

**Files:** None (verification only).

- [ ] **Step 1: Verify full build from repo root**

```bash
go build ./...
```

Expected: exit 0, all packages including `examples/extensions/hosted-showcase-go` compile.

- [ ] **Step 2: Run all extension E2E tests**

```bash
cd internal/extension && go test -run TestE2E -count=1 -v -timeout 120s
```

Expected: `TestE2E_CompiledIn`, `TestE2E_HostedGo`, and `TestE2E_HostedShowcaseGo` all pass (TS test may skip if Node
not available).

- [ ] **Step 3: Verify the extension runs standalone**

```bash
cd examples/extensions/hosted-showcase-go && echo '{"jsonrpc":"2.0","id":1,"method":"pi.extension/handshake","params":{"protocol_version":"2.1","extension_id":"hosted-showcase-go","extension_version":"0.1.0","requested_services":[{"service":"tools","version":1,"methods":["register"]},{"service":"events","version":1,"methods":["session_start","tool_execute"]}]}}' | timeout 5 go run . 2>/dev/null || true
```

Expected: the process starts, sends a handshake response on stdout, registers tools, then exits (or times out after 5s
since no shutdown signal is sent — that's fine for a smoke test).

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

// buildTestRuntime constructs a minimal Runtime with one compiled-in
// extension that has a single registered tool and a lifecycle hook pointing
// at that tool.
func buildTestRuntime(t *testing.T, hookEvent string, fb *testbridge.FakeBridge) *Runtime {
	t.Helper()

	manager := host.NewManager(nil)

	meta := piapi.Metadata{
		Name:    "testhook",
		Version: "0.1.0",
	}
	reg := &host.Registration{
		ID:       "testhook",
		Mode:     "compiled-in",
		Trust:    host.TrustCompiledIn,
		Metadata: meta,
	}
	if err := manager.Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var bridge extapi.SessionBridge = extapi.NoopBridge{}
	if fb != nil {
		bridge = fb
	}
	api := extapi.NewCompiled(reg, manager, bridge)
	reg.API = api

	var order []string
	_ = api.RegisterTool(piapi.ToolDescriptor{
		Name:        "hook_tool",
		Description: "test hook tool",
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			order = append(order, "hook_tool")
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: "hook ran"}},
			}, nil
		},
	})

	hooks := []HookConfig{
		{
			ExtensionID: "testhook",
			Event:       hookEvent,
			Command:     "hook_tool",
			Tools:       nil, // no filter = always matches
		},
	}

	rt := &Runtime{
		Extensions:     []*host.Registration{reg},
		LifecycleHooks: hooks,
		Bridge:         bridge,
	}

	_ = order // captured by closure above
	return rt
}

func TestRunLifecycleHooks_FiresInOrder(t *testing.T) {
	var fired []string

	manager := host.NewManager(nil)
	meta := piapi.Metadata{Name: "ext1", Version: "0.1.0"}
	reg := &host.Registration{
		ID:       "ext1",
		Mode:     "compiled-in",
		Trust:    host.TrustCompiledIn,
		Metadata: meta,
	}
	if err := manager.Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	api := extapi.NewCompiled(reg, manager, extapi.NoopBridge{})
	reg.API = api

	_ = api.RegisterTool(piapi.ToolDescriptor{
		Name:        "tool_a",
		Description: "tool a",
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			fired = append(fired, "a")
			return piapi.ToolResult{}, nil
		},
	})
	_ = api.RegisterTool(piapi.ToolDescriptor{
		Name:        "tool_b",
		Description: "tool b",
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			fired = append(fired, "b")
			return piapi.ToolResult{}, nil
		},
	})

	rt := &Runtime{
		Extensions: []*host.Registration{reg},
		LifecycleHooks: []HookConfig{
			{ExtensionID: "ext1", Event: "startup", Command: "tool_a"},
			{ExtensionID: "ext1", Event: "startup", Command: "tool_b"},
		},
	}

	if err := rt.RunLifecycleHooks(context.Background(), "startup", nil); err != nil {
		t.Fatalf("RunLifecycleHooks: %v", err)
	}

	if len(fired) != 2 || fired[0] != "a" || fired[1] != "b" {
		t.Fatalf("expected [a b], got %v", fired)
	}
}

func TestRunLifecycleHooks_BeforeTurnAppendsEntry(t *testing.T) {
	fb := &testbridge.FakeBridge{}

	rt := buildTestRuntime(t, "before_turn", fb)

	if err := rt.RunLifecycleHooks(context.Background(), "before_turn", map[string]any{"key": "val"}); err != nil {
		t.Fatalf("RunLifecycleHooks: %v", err)
	}

	var found bool
	for _, c := range fb.Calls {
		if c.Method == "AppendEntry" {
			found = true
			if c.Args["kind"] != "hook/before_turn" {
				t.Errorf("expected kind=hook/before_turn, got %v", c.Args["kind"])
			}
		}
	}
	if !found {
		t.Fatal("expected AppendEntry to be called for before_turn hook")
	}
}

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

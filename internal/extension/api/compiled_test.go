package api

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func newTestAPI(t *testing.T) (*compiledAPI, *host.Manager) {
	t.Helper()
	g, err := host.NewGate(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := host.NewManager(g)
	reg := &host.Registration{ID: "hello", Mode: "compiled-in", Trust: host.TrustCompiledIn,
		Metadata: piapi.Metadata{Name: "hello", Version: "0.1"}}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	api := NewCompiled(reg, m).(*compiledAPI)
	return api, m
}

func TestCompiled_RegisterToolLands(t *testing.T) {
	api, _ := newTestAPI(t)
	err := api.RegisterTool(piapi.ToolDescriptor{
		Name: "greet", Description: "say hi",
		Execute: func(ctx context.Context, c piapi.ToolCall, u piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "hi"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := api.Tools()["greet"]; !ok {
		t.Fatal("expected greet registered")
	}
}

func TestCompiled_SendMessageNotImplemented(t *testing.T) {
	api, _ := newTestAPI(t)
	err := api.SendMessage(piapi.CustomMessage{}, piapi.SendOptions{})
	if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Fatalf("expected ErrNotImplementedSentinel; got %v", err)
	}
}

func TestCompiled_OnSessionStartSubscribes(t *testing.T) {
	api, mgr := newTestAPI(t)
	called := false
	err := api.On(piapi.EventSessionStart, func(evt piapi.Event, ctx piapi.Context) (piapi.EventResult, error) {
		called = true
		return piapi.EventResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(piapi.SessionStartEvent{Reason: "startup"})
	mgr.Dispatcher().Dispatch(context.Background(), piapi.EventSessionStart, payload)
	if !called {
		t.Fatal("expected handler to have been called")
	}
}

func TestCompiled_OnOtherEventErrors(t *testing.T) {
	api, _ := newTestAPI(t)
	err := api.On("something_else", func(piapi.Event, piapi.Context) (piapi.EventResult, error) {
		return piapi.EventResult{}, nil
	})
	if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Fatalf("expected ErrNotImplementedSentinel; got %v", err)
	}
}

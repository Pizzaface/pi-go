package api

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	testbridge "github.com/pizzaface/go-pi/internal/extension/api/testing"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
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
	api := NewCompiled(reg, m, NoopBridge{}).(*compiledAPI)
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

	wantMethods := []string{"AppendEntry", "SendCustomMessage", "SendUserMessage", "SetSessionTitle", "GetSessionTitle", "SetEntryLabel"}
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

func TestCompiled_SendUserMessageAcceptsSteerWithTrigger(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	reg := &host.Registration{ID: "e", Metadata: piapi.Metadata{Name: "e", Version: "0.0"}}
	api := NewCompiled(reg, host.NewManager(nil), fb)

	err := api.SendUserMessage(
		piapi.UserMessage{Content: []piapi.ContentPart{{Type: "text", Text: "abort"}}},
		piapi.SendOptions{DeliverAs: "steer", TriggerTurn: true},
	)
	if err != nil {
		t.Fatalf("steer with TriggerTurn=true should succeed; got %v", err)
	}
	if len(fb.Calls) != 1 || fb.Calls[0].Method != "SendUserMessage" {
		t.Fatalf("expected single SendUserMessage call; got %+v", fb.Calls)
	}
}

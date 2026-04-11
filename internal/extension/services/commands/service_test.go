package commands

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
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
	var rpcErr *services.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeInvalidParams {
		t.Errorf("code = %d", rpcErr.Code)
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

func TestService_UnregisterRejectsEmptyName(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "unregister",
		Version:     1,
		Payload:     json.RawMessage(`{"name":""}`),
	})
	if err == nil {
		t.Fatal("expected error for empty name")
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

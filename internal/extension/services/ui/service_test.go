package ui

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
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
	if sink.statuses[0].Color != "yellow" {
		t.Errorf("color = %q, want yellow", sink.statuses[0].Color)
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
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeInvalidParams)
	}
}

func TestService_StatusRejectsInvalidJSON(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "status",
		Version:     1,
		Payload:     json.RawMessage(`not json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
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

func TestService_UnknownMethod(t *testing.T) {
	svc := New(&fakeSink{})
	_, err := svc.Dispatch(services.Call{
		ExtensionID: "ext.demo",
		Method:      "mystery",
		Version:     1,
	})
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	var rpcErr *services.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeMethodNotFound {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeMethodNotFound)
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

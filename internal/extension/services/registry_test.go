package services

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

type stubService struct {
	name     string
	version  int
	methods  []string
	dispatch func(Call) (json.RawMessage, error)
}

func (s stubService) Name() string      { return s.name }
func (s stubService) Version() int      { return s.version }
func (s stubService) Methods() []string { return s.methods }
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

func newTestRegistry(t *testing.T, svcs ...Service) *Registry {
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
	r := newTestRegistry(t)
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
	r := newTestRegistry(t, svc)
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
	r := newTestRegistry(t, svc)
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
	r := newTestRegistry(t, svc)
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

func TestRegistry_DispatchWrapsPlainError(t *testing.T) {
	svc := stubService{
		name: "ui", version: 1, methods: []string{"status"},
		dispatch: func(Call) (json.RawMessage, error) {
			return nil, errors.New("boom")
		},
	}
	r := newTestRegistry(t, svc)
	_, err := r.Dispatch("ext.demo", hostproto.HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
	}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %v", err)
	}
	if rpcErr.Code != hostproto.ErrCodeServiceError {
		t.Errorf("code = %d, want %d", rpcErr.Code, hostproto.ErrCodeServiceError)
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

func TestRegistry_RegisterNilFails(t *testing.T) {
	r := NewRegistry(capGate{})
	if err := r.Register(nil); err == nil {
		t.Fatal("expected nil register to fail")
	}
}

func TestRegistry_HostServices(t *testing.T) {
	r := newTestRegistry(t,
		stubService{name: "ui", version: 1, methods: []string{"status", "clear_status"}},
		stubService{name: "commands", version: 1, methods: []string{"register"}},
	)
	catalog := r.HostServices()
	if len(catalog) != 2 {
		t.Fatalf("catalog size = %d, want 2", len(catalog))
	}
	// Sorted alphabetically.
	if catalog[0].Service != "commands" || catalog[1].Service != "ui" {
		t.Errorf("catalog not sorted: %+v", catalog)
	}
}

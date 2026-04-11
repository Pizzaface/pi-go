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
	if rpcErr.RPCCode() != hostproto.ErrCodeInvalidParams {
		t.Errorf("RPCCode() = %d", rpcErr.RPCCode())
	}
}

func TestToRPCError_PassesThroughTypedError(t *testing.T) {
	original := NewRPCError(hostproto.ErrCodeCapabilityDenied, "nope")
	wrapped := ToRPCError(original)
	if wrapped == nil {
		t.Fatal("ToRPCError returned nil for non-nil input")
	}
	if wrapped.Code != hostproto.ErrCodeCapabilityDenied {
		t.Errorf("code = %d", wrapped.Code)
	}
}

func TestToRPCError_WrapsPlainError(t *testing.T) {
	plain := errors.New("something broke")
	wrapped := ToRPCError(plain)
	if wrapped == nil {
		t.Fatal("ToRPCError returned nil")
	}
	if wrapped.Code != hostproto.ErrCodeServiceError {
		t.Errorf("code = %d, want %d", wrapped.Code, hostproto.ErrCodeServiceError)
	}
	if wrapped.Message != "something broke" {
		t.Errorf("message = %q", wrapped.Message)
	}
}

func TestToRPCError_NilReturnsNil(t *testing.T) {
	if ToRPCError(nil) != nil {
		t.Error("ToRPCError(nil) should return nil")
	}
}

func TestSessionContext_Fields(t *testing.T) {
	sc := &SessionContext{SessionID: "sess-1", ExtensionID: "ext.demo", SessionsDir: "/tmp/sessions"}
	if sc.ExtensionID != "ext.demo" {
		t.Errorf("ExtensionID = %q", sc.ExtensionID)
	}
	if sc.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", sc.SessionID)
	}
	if sc.SessionsDir != "/tmp/sessions" {
		t.Errorf("SessionsDir = %q", sc.SessionsDir)
	}
}

// mockService verifies the Service interface can be implemented.
type mockService struct{}

func (mockService) Name() string      { return "mock" }
func (mockService) Version() int      { return 1 }
func (mockService) Methods() []string { return []string{"ping"} }
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

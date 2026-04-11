// Package services implements the v2 extension platform service registry.
// Each service lives in its own subpackage under this directory and
// implements the Service interface. The Registry dispatches incoming
// host_call requests to the appropriate service by name.
package services

import (
	"encoding/json"
	"fmt"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// Service is the host-side interface every v2 extension service
// implements. Services are stateless from the registry's point of view;
// any per-session state lives behind SessionContext.
type Service interface {
	// Name returns the service name as it appears in a host_call
	// envelope (e.g. "ui", "commands"). Must be stable and unique.
	Name() string

	// Version returns the current service version. A call requesting a
	// higher version than the service provides is rejected with
	// ErrCodeServiceUnsupported.
	Version() int

	// Methods returns the set of method names this service exposes.
	// Used for handshake HostServices catalog and for the registry's
	// capability gate.
	Methods() []string

	// Dispatch runs a single call and returns the response payload.
	// A nil error means success; a non-nil error should be an *RPCError
	// for well-typed failures or a plain error (wrapped as
	// ErrCodeServiceError) otherwise.
	Dispatch(call Call) (json.RawMessage, error)
}

// Call is a single host_call dispatched to a service.
type Call struct {
	ExtensionID string
	Method      string
	Version     int
	Payload     json.RawMessage
	Session     *SessionContext
}

// SessionContext carries the active session identity and any resources
// services need to read or mutate session-scoped state. It is created
// fresh per host_call so services cannot hold onto it.
type SessionContext struct {
	SessionID   string
	SessionsDir string
	ExtensionID string
}

// RPCError is a typed error carrying a JSON-RPC error code. Service
// implementations should return *RPCError for known failure modes; any
// other error is wrapped by the registry as ErrCodeServiceError.
type RPCError struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// RPCCode returns the JSON-RPC error code. This lets callers that do
// not import the services package (e.g. hostruntime) extract the code
// through a small interface.
func (e *RPCError) RPCCode() int { return e.Code }

// NewRPCError constructs an *RPCError with the given code and message.
func NewRPCError(code int, message string) error {
	return &RPCError{Code: code, Message: message}
}

// ToRPCError converts any error into an *RPCError. Returns e directly
// if it already is one, otherwise wraps it with ErrCodeServiceError.
func ToRPCError(err error) *RPCError {
	if err == nil {
		return nil
	}
	if rpcErr, ok := err.(*RPCError); ok {
		return rpcErr
	}
	return &RPCError{Code: hostproto.ErrCodeServiceError, Message: err.Error()}
}

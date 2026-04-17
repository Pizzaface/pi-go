package piapi

import "errors"

// ErrNotImplementedSentinel is returned via errors.Is by ErrNotImplemented.
var ErrNotImplementedSentinel = errors.New("piapi: not implemented")

// ErrNotImplemented is returned by API methods whose implementation is
// deferred to a later spec. The Spec field identifies which spec
// adds the implementation.
type ErrNotImplemented struct {
	Method string
	Spec   string
}

func (e ErrNotImplemented) Error() string {
	return "piapi: " + e.Method + " not implemented (deferred to spec " + e.Spec + ")"
}

func (e ErrNotImplemented) Is(target error) bool {
	return target == ErrNotImplementedSentinel
}

// ErrCapabilityDeniedSentinel is returned via errors.Is by ErrCapabilityDenied.
var ErrCapabilityDeniedSentinel = errors.New("piapi: capability denied")

// ErrCapabilityDenied is returned when a host_call or event subscription
// is rejected because the extension was not granted the capability.
type ErrCapabilityDenied struct {
	Capability string
	Reason     string
}

func (e ErrCapabilityDenied) Error() string {
	s := "piapi: capability denied: " + e.Capability
	if e.Reason != "" {
		s += " (" + e.Reason + ")"
	}
	return s
}

func (e ErrCapabilityDenied) Is(target error) bool {
	return target == ErrCapabilityDeniedSentinel
}

// Spec #5 errors.

var ErrInvalidKindSentinel = errors.New("piapi: invalid kind")

type ErrInvalidKind struct{ Kind string }

func (e ErrInvalidKind) Error() string      { return "piapi: invalid kind: " + e.Kind }
func (e ErrInvalidKind) Is(t error) bool    { return t == ErrInvalidKindSentinel }

var ErrIncoherentOptionsSentinel = errors.New("piapi: incoherent options")

type ErrIncoherentOptions struct{ Reason string }

func (e ErrIncoherentOptions) Error() string   { return "piapi: incoherent options: " + e.Reason }
func (e ErrIncoherentOptions) Is(t error) bool { return t == ErrIncoherentOptionsSentinel }

var ErrEntryNotFoundSentinel = errors.New("piapi: entry not found")

type ErrEntryNotFound struct{ ID string }

func (e ErrEntryNotFound) Error() string   { return "piapi: entry not found: " + e.ID }
func (e ErrEntryNotFound) Is(t error) bool { return t == ErrEntryNotFoundSentinel }

var ErrBranchNotFoundSentinel = errors.New("piapi: branch not found")

type ErrBranchNotFound struct{ ID string }

func (e ErrBranchNotFound) Error() string   { return "piapi: branch not found: " + e.ID }
func (e ErrBranchNotFound) Is(t error) bool { return t == ErrBranchNotFoundSentinel }

var ErrSessionNotFoundSentinel = errors.New("piapi: session not found")

type ErrSessionNotFound struct{ ID string }

func (e ErrSessionNotFound) Error() string   { return "piapi: session not found: " + e.ID }
func (e ErrSessionNotFound) Is(t error) bool { return t == ErrSessionNotFoundSentinel }

var ErrSessionControlUnsupportedInCLISentinel = errors.New("piapi: session control unsupported in CLI")

type ErrSessionControlUnsupportedInCLI struct{ Method string }

func (e ErrSessionControlUnsupportedInCLI) Error() string {
	return "piapi: " + e.Method + " unsupported in CLI (run the TUI to use session control)"
}
func (e ErrSessionControlUnsupportedInCLI) Is(t error) bool {
	return t == ErrSessionControlUnsupportedInCLISentinel
}

var ErrSessionControlInEventHandlerSentinel = errors.New("piapi: session control called from event handler")

type ErrSessionControlInEventHandler struct{ Method string }

func (e ErrSessionControlInEventHandler) Error() string {
	return "piapi: " + e.Method + " must not be called from an event handler; use a command handler"
}
func (e ErrSessionControlInEventHandler) Is(t error) bool {
	return t == ErrSessionControlInEventHandlerSentinel
}

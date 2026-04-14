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

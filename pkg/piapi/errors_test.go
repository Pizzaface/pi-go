package piapi

import (
	"errors"
	"testing"
)

func TestErrNotImplemented_Is(t *testing.T) {
	err := ErrNotImplemented{Method: "RegisterCommand", Spec: "#2"}
	if !errors.Is(err, ErrNotImplementedSentinel) {
		t.Fatal("errors.Is should match sentinel")
	}
	if err.Error() == "" {
		t.Fatal("Error() should include method and spec")
	}
}

func TestErrCapabilityDenied_Is(t *testing.T) {
	err := ErrCapabilityDenied{Capability: "tools.register", Reason: "not approved"}
	if !errors.Is(err, ErrCapabilityDeniedSentinel) {
		t.Fatal("errors.Is should match sentinel")
	}
}

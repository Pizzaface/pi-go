package piapi_test

import (
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// TestAPIInterface_HasUnregisterAndReady is a compile-time assertion that
// the API interface advertises UnregisterTool and Ready.
func TestAPIInterface_HasUnregisterAndReady(t *testing.T) {
	var _ interface {
		UnregisterTool(string) error
		Ready() error
	} = (piapi.API)(nil)
	_ = t
}

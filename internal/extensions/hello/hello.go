// Package hello is the canonical compiled-in extension fixture. It
// registers a single "greet" tool and subscribes to session_start. Its
// package init() appends the entry to the compiled registry; importing
// the package (even via blank import) is enough to activate it.
package hello

import (
	"context"

	"github.com/pizzaface/go-pi/internal/extension/compiled"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// Metadata describes the compiled-in hello extension.
var Metadata = piapi.Metadata{
	Name:        "hello",
	Version:     "0.1",
	Description: "Compiled-in hello fixture; registers greet tool.",
}

// Register wires the extension to the supplied API. Exported so tests
// can reference it; the registry below is the normal entry point.
func Register(pi piapi.API) error {
	if err := pi.On(piapi.EventSessionStart, func(piapi.Event, piapi.Context) (piapi.EventResult, error) {
		return piapi.EventResult{}, nil
	}); err != nil {
		return err
	}
	return pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "greet",
		Label:       "Greet",
		Description: "Returns a friendly greeting.",
		Parameters:  nil,
		Execute: func(context.Context, piapi.ToolCall, piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: "hi"}},
			}, nil
		},
	})
}

func init() {
	compiled.Append(compiled.Entry{
		Name:     Metadata.Name,
		Register: Register,
		Metadata: Metadata,
	})
}

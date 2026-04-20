// Hosted-collide is a fixture that attempts to register the tool name
// "greet" — the same name used by hosted-hello-go. When both are
// approved and loaded together, the host must reject the second
// registration with a CollisionError while leaving the first intact.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:                  "hosted-collide",
	Version:               "0.0.1",
	Description:           "Collision fixture: also tries to register 'greet'",
	RequestedCapabilities: []string{"tools.register", "events.tool_execute"},
}

func register(pi piapi.API) error {
	err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "greet",
		Description: "I will collide.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	})
	if err == nil {
		return errors.New("collision fixture: expected RegisterTool to fail")
	}
	fmt.Fprintln(piext.Log(), "collision fixture: rejected as expected:", err)
	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-collide: fatal:", err)
	}
}

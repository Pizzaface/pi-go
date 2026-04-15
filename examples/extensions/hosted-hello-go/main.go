// Hosted-hello-go is the canonical Go-language hosted extension fixture.
// It registers a single "greet" tool and subscribes to session_start.
//
// Run standalone:
//
//	go run .
//
// The process speaks JSON-RPC 2.0 over stdio per pi-go's hostproto v2.1.
// pi-go invokes it via the loader/host pipeline once approved.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dimetron/pi-go/pkg/piapi"
	"github.com/dimetron/pi-go/pkg/piext"
)

// Metadata is package-level so a future runtime/loader can read it via
// reflection if desired; piext.Run reads it explicitly below.
var Metadata = piapi.Metadata{
	Name:        "hosted-hello-go",
	Version:     "0.1.0",
	Description: "Canonical hosted-go extension fixture; registers greet tool.",
	RequestedCapabilities: []string{
		"tools.register",
		"events.session_start",
		"events.tool_execute",
	},
}

type greetArgs struct {
	Name string `json:"name" jsonschema:"description=Name to greet"`
}

func register(pi piapi.API) error {
	if err := pi.On(piapi.EventSessionStart, func(evt piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		fmt.Fprintln(piext.Log(), "hosted-hello-go: session_start", evt.EventName())
		return piapi.EventResult{}, nil
	}); err != nil {
		return err
	}
	return pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "greet",
		Label:       "Greet",
		Description: "Returns a friendly greeting.",
		Parameters:  piext.SchemaFromStruct(greetArgs{}),
		Execute: func(_ context.Context, call piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			var a greetArgs
			if len(call.Args) > 0 {
				if err := json.Unmarshal(call.Args, &a); err != nil {
					return piapi.ToolResult{}, err
				}
			}
			if a.Name == "" {
				a.Name = "world"
			}
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: "Hello, " + a.Name + "!"}},
			}, nil
		},
	})
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-hello-go: fatal:", err)
	}
}

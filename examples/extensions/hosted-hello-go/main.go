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
	"os"

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
	if err := pi.RegisterTool(piapi.ToolDescriptor{
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
	}); err != nil {
		return err
	}

	// Spec #5 probe: when PI_SPEC5_PROBE=1 the extension fires AppendEntry
	// and a log.append call immediately during Register so the E2E test can
	// assert the FakeBridge captured them without needing to invoke a tool.
	if os.Getenv("PI_SPEC5_PROBE") == "1" {
		if err := pi.AppendEntry("probe", map[string]any{"hi": true}); err != nil {
			return fmt.Errorf("spec5_probe AppendEntry: %w", err)
		}
		fmt.Fprintln(piext.Log(), "spec5_probe: hello from log.append")
	}
	return nil
}

func main() {
	meta := Metadata
	if os.Getenv("PI_SPEC5_PROBE") == "1" {
		// Request spec #5 capabilities so the host includes them in the
		// granted-services handshake response.
		meta.RequestedCapabilities = append(
			append([]string(nil), meta.RequestedCapabilities...),
			"session.append_entry",
			"log.append",
		)
	}
	if err := piext.Run(meta, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-hello-go: fatal:", err)
	}
}

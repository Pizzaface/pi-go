// Hosted-slow-ready-go sleeps 300ms before calling RegisterTool so that
// an overly-aggressive quiescence window would miss its registration. It
// then calls pi.Ready() — the host must rely on this explicit signal
// rather than inactivity heuristics.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:                  "hosted-slow-ready-go",
	Version:               "0.0.1",
	Description:           "Sleeps before registering, calls Ready() at end",
	RequestedCapabilities: []string{"tools.register", "events.tool_execute"},
}

func register(pi piapi.API) error {
	time.Sleep(300 * time.Millisecond)
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "slow_greet",
		Description: "Registered after a 300ms delay.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: "slow hello"}},
			}, nil
		},
	}); err != nil {
		return err
	}
	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-slow-ready-go: fatal:", err)
	}
}

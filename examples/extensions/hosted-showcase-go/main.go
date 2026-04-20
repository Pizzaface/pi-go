package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:        "hosted-showcase-go",
	Version:     "0.1.0",
	Description: "Showcase hosted-go extension; demonstrates multi-tool registration, event handling, streaming updates, and system introspection.",
	RequestedCapabilities: []string{
		"tools.register",
		"events.session_start",
		"events.tool_execute",
	},
}

// sessionStartTime is set by the session_start handler; read by ext_info.
var sessionStartTime atomic.Value // stores time.Time

func register(pi piapi.API) error {
	// Subscribe to session_start.
	if err := pi.On(piapi.EventSessionStart, func(evt piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		sessionStartTime.Store(time.Now())
		se, _ := evt.(piapi.SessionStartEvent)
		fmt.Fprintln(piext.Log(), "hosted-showcase-go: session_start reason="+se.Reason)
		return piapi.EventResult{}, nil
	}); err != nil {
		return err
	}

	// Tool 1: ext_info — no parameters, returns extension metadata + runtime state.
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "ext_info",
		Label:       "Extension Info",
		Description: "Returns metadata and runtime state of the hosted-showcase-go extension: name, version, capabilities, Go version, PID, and uptime.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			uptime := "unknown (session_start not received)"
			if t, ok := sessionStartTime.Load().(time.Time); ok {
				uptime = time.Since(t).Round(time.Millisecond).String()
			}
			caps := strings.Join(Metadata.RequestedCapabilities, ", ")
			text := fmt.Sprintf(
				"Extension: %s v%s\nDescription: %s\nCapabilities: %s\nGo: %s\nPID: %d\nUptime: %s",
				Metadata.Name, Metadata.Version, Metadata.Description,
				caps, runtime.Version(), os.Getpid(), uptime,
			)
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: text}},
			}, nil
		},
	}); err != nil {
		return err
	}

	// Tool 2: ext_echo — demonstrates complex schema with required/optional fields.
	type echoArgs struct {
		Message   string `json:"message"   jsonschema:"description=Text to echo back,required"`
		Repeat    int    `json:"repeat"    jsonschema:"description=Number of repetitions,minimum=1,maximum=100"`
		Uppercase bool   `json:"uppercase" jsonschema:"description=Whether to uppercase the output"`
	}
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "ext_echo",
		Label:       "Echo",
		Description: "Echoes a message back with optional repetition and uppercasing. Demonstrates complex JSON Schema parameters with required and optional fields.",
		Parameters:  piext.SchemaFromStruct(echoArgs{}),
		Execute: func(_ context.Context, call piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			var a echoArgs
			if len(call.Args) > 0 {
				if err := json.Unmarshal(call.Args, &a); err != nil {
					return piapi.ToolResult{
						Content: []piapi.ContentPart{{Type: "text", Text: "invalid arguments: " + err.Error()}},
						IsError: true,
					}, nil
				}
			}
			if a.Message == "" {
				return piapi.ToolResult{
					Content: []piapi.ContentPart{{Type: "text", Text: "message is required"}},
					IsError: true,
				}, nil
			}
			if a.Repeat < 1 {
				a.Repeat = 1
			}
			if a.Repeat > 100 {
				return piapi.ToolResult{
					Content: []piapi.ContentPart{{Type: "text", Text: "repeat must be between 1 and 100"}},
					IsError: true,
				}, nil
			}
			msg := a.Message
			if a.Uppercase {
				msg = strings.ToUpper(msg)
			}
			lines := make([]string, a.Repeat)
			for i := range lines {
				lines[i] = msg
			}
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: strings.Join(lines, "\n")}},
			}, nil
		},
	}); err != nil {
		return err
	}

	// Tool 3: ext_sysinfo — demonstrates system introspection via Go stdlib.
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "ext_sysinfo",
		Label:       "System Info",
		Description: "Returns system information: hostname, OS, architecture, Go version, CPUs, PID, working directory, and executable path.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			hostname, _ := os.Hostname()
			wd, _ := os.Getwd()
			exe, _ := os.Executable()
			text := fmt.Sprintf(
				"Hostname: %s\nOS: %s\nArch: %s\nGo: %s\nCPUs: %d\nPID: %d\nWorkDir: %s\nExecutable: %s",
				hostname, runtime.GOOS, runtime.GOARCH, runtime.Version(),
				runtime.NumCPU(), os.Getpid(), wd, exe,
			)
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: text}},
			}, nil
		},
	}); err != nil {
		return err
	}

	// Tool 4: ext_rpc_ping — demonstrates streaming progress via UpdateFunc.
	type pingArgs struct {
		Count int `json:"count" jsonschema:"description=Number of pings (1-20),minimum=1,maximum=20"`
	}
	if err := pi.RegisterTool(piapi.ToolDescriptor{
		Name:        "ext_rpc_ping",
		Label:       "RPC Ping",
		Description: "Measures internal operation timing and streams progress updates after each iteration. Demonstrates UpdateFunc streaming callbacks.",
		Parameters:  piext.SchemaFromStruct(pingArgs{}),
		Execute: func(_ context.Context, call piapi.ToolCall, onUpdate piapi.UpdateFunc) (piapi.ToolResult, error) {
			var a pingArgs
			if len(call.Args) > 0 {
				if err := json.Unmarshal(call.Args, &a); err != nil {
					return piapi.ToolResult{
						Content: []piapi.ContentPart{{Type: "text", Text: "invalid arguments: " + err.Error()}},
						IsError: true,
					}, nil
				}
			}
			if a.Count < 1 {
				a.Count = 3
			}
			if a.Count > 20 {
				return piapi.ToolResult{
					Content: []piapi.ContentPart{{Type: "text", Text: "count must be between 1 and 20"}},
					IsError: true,
				}, nil
			}

			durations := make([]time.Duration, a.Count)
			for i := 0; i < a.Count; i++ {
				start := time.Now()
				// Trivial operation to measure Go-side timing.
				runtime.Gosched()
				d := time.Since(start)
				durations[i] = d

				if onUpdate != nil {
					onUpdate(piapi.ToolResult{
						Content: []piapi.ContentPart{{Type: "text", Text: fmt.Sprintf("Ping %d/%d: %s", i+1, a.Count, d)}},
					})
				}
			}

			var min, max, total time.Duration
			min = durations[0]
			for _, d := range durations {
				total += d
				if d < min {
					min = d
				}
				if d > max {
					max = d
				}
			}
			avg := total / time.Duration(a.Count)
			text := fmt.Sprintf("Pings: %d\nMin: %s\nMax: %s\nAvg: %s\nTotal: %s", a.Count, min, max, avg, total)
			return piapi.ToolResult{
				Content: []piapi.ContentPart{{Type: "text", Text: text}},
			}, nil
		},
	}); err != nil {
		return err
	}

	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-showcase-go: fatal:", err)
	}
}

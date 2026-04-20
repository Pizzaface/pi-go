package piext

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// captureOutboundCall reads one JSON-RPC request from hostIn, replies with an
// empty success result, and returns the decoded outbound envelope.
func captureOutboundCall(t *testing.T, hostIn io.Reader, hostOut io.Writer) map[string]any {
	t.Helper()
	buf := make([]byte, 8192)
	n, err := hostIn.Read(buf)
	if err != nil {
		t.Fatalf("read outbound: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf[:n], &env); err != nil {
		t.Fatalf("decode outbound: %v", err)
	}
	resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": env["id"], "result": map[string]any{}})
	if _, err := hostOut.Write(append(resp, '\n')); err != nil {
		t.Fatalf("write resp: %v", err)
	}
	return env
}

func TestRPCAPI_UnregisterTool_SendsRPC(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	defer transport.Close()

	api := newRPCAPI(transport, piapi.Metadata{Name: "ext-a"}, []GrantedService{
		{Service: "tools", Version: 1, Methods: []string{"register", "unregister"}},
	})

	// First: drain the RegisterTool RPC.
	registerDone := make(chan map[string]any, 1)
	go func() { registerDone <- captureOutboundCall(t, hostIn, hostOut) }()
	if err := api.RegisterTool(piapi.ToolDescriptor{
		Name: "greet", Description: "x",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	<-registerDone

	// Now the actual assertion: UnregisterTool emits tools.unregister.
	unregDone := make(chan map[string]any, 1)
	go func() { unregDone <- captureOutboundCall(t, hostIn, hostOut) }()
	if err := api.UnregisterTool("greet"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	env := <-unregDone

	if got := env["method"]; got != "pi.extension/host_call" {
		t.Fatalf("method = %v; want pi.extension/host_call", got)
	}
	params, _ := env["params"].(map[string]any)
	if params["service"] != "tools" || params["method"] != "unregister" {
		t.Fatalf("wrong service/method: %+v", params)
	}
	payload, _ := params["payload"].(map[string]any)
	if payload["name"] != "greet" {
		t.Fatalf("payload name = %v; want greet", payload["name"])
	}
}

func TestRPCAPI_Ready_SendsRPC(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	defer transport.Close()

	// No grants — Ready() must not require one.
	api := newRPCAPI(transport, piapi.Metadata{Name: "ext-a"}, nil)

	done := make(chan map[string]any, 1)
	go func() { done <- captureOutboundCall(t, hostIn, hostOut) }()
	if err := api.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	env := <-done

	params, _ := env["params"].(map[string]any)
	if params["service"] != "ext" || params["method"] != "ready" {
		t.Fatalf("wrong service/method: %+v", params)
	}
}
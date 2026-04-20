package piext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestRPCAPI_RegisterTool_SendsHostCall(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	api := newRPCAPI(transport, piapi.Metadata{Name: "t", Version: "0.1"}, []GrantedService{
		{Service: "tools", Version: 1, Methods: []string{"register"}},
	})

	go func() {
		buf := make([]byte, 4096)
		n, _ := hostIn.Read(buf)
		var req map[string]any
		_ = json.Unmarshal(buf[:n], &req)
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		_, _ = hostOut.Write(append(resp, '\n'))
	}()

	err := api.RegisterTool(piapi.ToolDescriptor{
		Name: "greet", Description: "greet", Parameters: json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("RegisterTool err: %v", err)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
	_ = transport.Close()
}

func TestRPCAPI_NotImplementedStubs(t *testing.T) {
	transport := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{})
	api := newRPCAPI(transport, piapi.Metadata{Name: "t", Version: "0.1"}, nil)

	err := api.RegisterCommand("x", piapi.CommandDescriptor{})
	if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Fatalf("RegisterCommand: got %v; want ErrNotImplemented", err)
	}
}

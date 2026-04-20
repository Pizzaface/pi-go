package piext

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestRun_Handshake(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()

	meta := piapi.Metadata{
		Name:                  "test",
		Version:               "0.1.0",
		RequestedCapabilities: []string{"tools.register"},
	}
	registerCalled := make(chan struct{})
	go func() {
		_ = runInternal(extIn, extOut, meta, func(pi piapi.API) error {
			close(registerCalled)
			return nil
		})
	}()

	buf := make([]byte, 4096)
	n, _ := hostIn.Read(buf)
	var req map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf[:n]), &req); err != nil {
		t.Fatalf("handshake not JSON: %v", err)
	}
	params := req["params"].(map[string]any)
	if params["protocol_version"] != "2.1" {
		t.Fatalf("protocol_version=%v; want 2.1", params["protocol_version"])
	}
	if params["extension_id"] != "test" {
		t.Fatalf("extension_id=%v; want test", params["extension_id"])
	}

	resp := `{"jsonrpc":"2.0","id":` + toString(req["id"]) + `,"result":{"protocol_version":"2.1","granted_services":[{"service":"tools","version":1,"methods":["register"]}],"host_services":[],"dispatchable_events":[]}}` + "\n"
	_, _ = hostOut.Write([]byte(resp))

	select {
	case <-registerCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("register callback not invoked within 2s")
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func toString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

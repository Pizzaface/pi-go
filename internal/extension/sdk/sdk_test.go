package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func TestClient_SendRequest_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	client := NewClient(io.NopCloser(&bytes.Buffer{}), &buf)

	id, err := client.sendRequest(hostproto.MethodHandshake, map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	if id != 1 {
		t.Errorf("first id = %d, want 1", id)
	}

	var req hostproto.RPCRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Method != hostproto.MethodHandshake {
		t.Errorf("Method = %q", req.Method)
	}
	if req.ID != 1 {
		t.Errorf("ID = %d", req.ID)
	}
}

func TestClient_NextID_Increments(t *testing.T) {
	var buf bytes.Buffer
	client := NewClient(io.NopCloser(&bytes.Buffer{}), &buf)
	id1, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	id2, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	id3, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	if id1 != 1 || id2 != 2 || id3 != 3 {
		t.Errorf("ids = %d, %d, %d, want 1, 2, 3", id1, id2, id3)
	}
}

func TestClient_ReadLoop_DispatchesResponse(t *testing.T) {
	// Use io.Pipe so the decoder can block waiting for input.
	r, w := io.Pipe()
	client := NewClient(io.NopCloser(r), io.Discard)

	// Prime a pending response waiter.
	waiter := client.waitFor(42)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	// Host writes a response.
	go func() {
		resp := hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      42,
			Result:  json.RawMessage(`{"ok":true}`),
		}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(append(data, '\n'))
	}()

	select {
	case got := <-waiter:
		if got.err != nil {
			t.Fatalf("waiter err: %v", got.err)
		}
		if string(got.result) != `{"ok":true}` {
			t.Errorf("result = %s", string(got.result))
		}
	case <-ctx.Done():
		t.Fatal("waiter did not receive response")
	}
}

func TestClient_ReadLoop_DispatchesError(t *testing.T) {
	r, w := io.Pipe()
	client := NewClient(io.NopCloser(r), io.Discard)

	waiter := client.waitFor(7)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	go func() {
		resp := hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      7,
			Error: &hostproto.RPCError{
				Code:    hostproto.ErrCodeInvalidParams,
				Message: "bad payload",
			},
		}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(append(data, '\n'))
	}()

	select {
	case got := <-waiter:
		if got.err == nil {
			t.Fatal("expected error")
		}
		if got.err.Error() != "rpc error -32602: bad payload" {
			t.Errorf("error = %q", got.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("waiter did not receive response")
	}
}
